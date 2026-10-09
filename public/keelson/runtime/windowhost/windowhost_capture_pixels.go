package windowhost

import (
	"crypto/rand"
	"encoding/hex"
	"slices"
	"time"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/capture"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Pixel captures (ADR-0281 §SD4, §SD5). A capture of windows takes three
// frames: in the first the host records its outgoing stream and marks where
// each window's emission begins and ends; in the second it sends the granted
// windows' spans in one captureReplay, which the client replays into a
// separate context and rasterizes, and that frame's Sync fetches the pixels;
// in the third the host hands them to the capture service, which encodes
// them (ADR-0281's PEP).

// pixelPhaseE is where a pixel capture stands.
type pixelPhaseE uint8

const (
	pixelPhaseQueued pixelPhaseE = iota
	pixelPhaseRecording
	pixelPhaseReplayPending
	pixelPhaseAwaitingResult
)

// pixelJob is the state a pixel capture carries between frames.
type pixelJob struct {
	keys      []WindowKeyT
	format    uint8
	recheck   func() bool
	phase     pixelPhaseE
	requestId uint64
	// spans are offsets into the frame's recording, per granted window, in
	// the order emitted; spanKeys and spanRects are each span's window and
	// its outer rect.
	spans     [][2]int
	spanKeys  []WindowKeyT
	spanRects []Rect
	// spanWindows locate each window in the replayed stream.
	spanWindows []capture.SpanWindow
	stream      []byte
	// spansDigest names the stream replayed (ADR-0281 §SD4).
	spansDigest string
	// recordedAt is when the recording of the frame ended.
	recordedAt time.Time
}

// The client's capture formats (captureReplay).
const (
	replayFormatPng uint8 = 0
	replayFormatSvg uint8 = 1
	// replayFormatTree is a window tree (ADR-0301).
	replayFormatTree uint8 = 2
)

// RenderPixels queues a render of the windows' pixels for the capture
// service (ADR-0281). Only the windows named are drawn, and only open ones;
// recheck is asked when the spans are chosen, and false fails the render.
// SourceStatus follows the job.
func (inst *Inst) RenderPixels(keys []uint64, recheck func() bool) (job string, err error) {
	return inst.renderReplay(keys, replayFormatPng, recheck)
}

// RenderSvg queues an SVG of one window, drawn from the same replay as a
// pixel capture, so the window's popups are in it.
func (inst *Inst) RenderSvg(key uint64, recheck func() bool) (job string, err error) {
	return inst.renderReplay([]uint64{key}, replayFormatSvg, recheck)
}

// RenderTree queues a window tree of the windows (ADR-0301), drawn from
// the same replay as a pixel capture.
func (inst *Inst) RenderTree(keys []uint64, recheck func() bool) (job string, err error) {
	return inst.renderReplay(keys, replayFormatTree, recheck)
}

func (inst *Inst) renderReplay(keys []uint64, format uint8, recheck func() bool) (job string, err error) {
	if len(keys) == 0 {
		err = eh.Errorf("windowhost: a pixel capture names at least one window")
		return
	}
	wk := make([]WindowKeyT, 0, len(keys))
	for _, k := range keys {
		if _, w := inst.engineOf("", k); w == nil {
			err = eh.Errorf("windowhost: no open window by that key")
			return
		}
		wk = append(wk, WindowKeyT(k))
	}
	inst.caps.mu.Lock()
	defer inst.caps.mu.Unlock()
	if inst.caps.jobs == nil {
		inst.caps.jobs = make(map[string]*captureJob)
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	job = "cap-" + hex.EncodeToString(b[:])
	j := &captureJob{id: job, requested: time.Now(),
		status: opwire.CaptureStatus{Phase: opwire.PhaseRunning},
		pixel:  &pixelJob{keys: wk, format: format, recheck: recheck}}
	inst.caps.jobs[job] = j
	inst.caps.pixelQueue = append(inst.caps.pixelQueue, j)
	return
}

// pixelFrameBegin advances the pixel capture in flight at the start of a
// Frame, before any window is emitted.
func (inst *Inst) pixelFrameBegin(sm *c.StateManager) {
	inst.caps.mu.Lock()
	defer inst.caps.mu.Unlock()
	j := inst.caps.pixelActive
	if j != nil {
		p := j.pixel
		switch p.phase {
		case pixelPhaseAwaitingResult:
			if r, ok := sm.TakeCaptureResult(); ok && r.RequestId == p.requestId {
				inst.finishPixelJobLocked(j, r, sm.GetPixelsPerPoint())
				j = nil
			} else if time.Since(j.requested) > captureTimeout {
				inst.failPixelJobLocked(j, "the client did not answer the capture in time")
				j = nil
			}
		case pixelPhaseReplayPending:
			c.CaptureReplay(p.requestId, p.format, p.stream)
			sm.WantCaptureResult()
			p.phase = pixelPhaseAwaitingResult
			p.stream = nil
		}
	}
	if j == nil && len(inst.caps.pixelQueue) > 0 {
		j = inst.caps.pixelQueue[0]
		inst.caps.pixelQueue = inst.caps.pixelQueue[1:]
		inst.caps.pixelActive = j
	}
	if j != nil && j.pixel.phase == pixelPhaseQueued {
		if f := typed.GetCurrentFffiVar(); f != nil {
			f.BeginRecording()
			j.pixel.phase = pixelPhaseRecording
			inst.caps.nextRequestId++
			j.pixel.requestId = inst.caps.nextRequestId
		}
	}
	if inst.caps.pixelActive != nil {
		// Keep frames coming until the capture lands: a reactive host
		// would otherwise wait on input between the steps.
		c.RequestRepaint()
	}
}

// pixelRecordingPosition is the recording's current length while a
// capture records, or -1.
func (inst *Inst) pixelRecordingPosition() int {
	inst.caps.mu.Lock()
	recording := inst.caps.pixelActive != nil && inst.caps.pixelActive.pixel.phase == pixelPhaseRecording
	inst.caps.mu.Unlock()
	if !recording {
		return -1
	}
	if f := typed.GetCurrentFffiVar(); f != nil {
		return f.RecordingPosition()
	}
	return -1
}

// pixelWindowSpan notes the span a window's emission took, when the window
// is one the capture draws.
func (inst *Inst) pixelWindowSpan(key WindowKeyT, begin int, end int, rect Rect) {
	if begin < 0 || end < begin {
		return
	}
	inst.caps.mu.Lock()
	defer inst.caps.mu.Unlock()
	j := inst.caps.pixelActive
	if j == nil || j.pixel.phase != pixelPhaseRecording || !slices.Contains(j.pixel.keys, key) {
		return
	}
	j.pixel.spans = append(j.pixel.spans, [2]int{begin, end})
	j.pixel.spanKeys = append(j.pixel.spanKeys, key)
	j.pixel.spanRects = append(j.pixel.spanRects, rect)
}

// pixelFrameEnd closes the recording after the windows were emitted and
// keeps the granted windows' spans for the next frame's replay.
func (inst *Inst) pixelFrameEnd() {
	inst.caps.mu.Lock()
	defer inst.caps.mu.Unlock()
	j := inst.caps.pixelActive
	if j == nil || j.pixel.phase != pixelPhaseRecording {
		return
	}
	f := typed.GetCurrentFffiVar()
	if f == nil {
		inst.failPixelJobLocked(j, "no client channel")
		return
	}
	rec := f.EndRecording()
	p := j.pixel
	p.recordedAt = time.Now()
	if p.recheck != nil && !p.recheck() {
		inst.failPixelJobLocked(j, "the grant no longer covers the windows")
		return
	}
	if len(p.spans) == 0 {
		inst.failPixelJobLocked(j, "none of the windows was drawn")
		return
	}
	n := 0
	for _, s := range p.spans {
		n += s[1] - s[0]
	}
	stream := make([]byte, 0, n)
	p.spanWindows = p.spanWindows[:0]
	for k, s := range p.spans {
		r := p.spanRects[k]
		p.spanWindows = append(p.spanWindows, capture.SpanWindow{Window: uint64(p.spanKeys[k]),
			Begin: len(stream), End: len(stream) + s[1] - s[0], Rect: [4]float32{r.MinX, r.MinY, r.W(), r.H()}})
		stream = append(stream, rec[s[0]:s[1]]...)
	}
	sum := blake3.Sum256(stream)
	p.stream, p.spans, p.spansDigest = stream, nil, hex.EncodeToString(sum[:])
	p.spanKeys, p.spanRects = nil, nil
	p.phase = pixelPhaseReplayPending
}

func (inst *Inst) finishPixelJobLocked(j *captureJob, r c.CaptureResultValue, ppp float32) {
	inst.caps.pixelActive = nil
	switch r.Status {
	case c.CaptureStatusCompleted:
	case c.CaptureStatusUnsupported:
		j.status = opwire.CaptureStatus{Phase: opwire.PhaseRefused, Reason: "this host cannot rasterize a capture: " + r.Reason}
		return
	default:
		j.status = opwire.CaptureStatus{Phase: opwire.PhaseFailed, Reason: r.Reason}
		return
	}
	if j.pixel.format == replayFormatTree {
		j.result = capture.SourceResult{Phase: opwire.PhaseCompleted, Tree: r.Data, SpansDigest: j.pixel.spansDigest,
			RecordedAt: j.pixel.recordedAt, Spans: j.pixel.spanWindows}
		j.status = opwire.CaptureStatus{Phase: opwire.PhaseCompleted}
		return
	}
	if j.pixel.format == replayFormatSvg {
		j.result = capture.SourceResult{Phase: opwire.PhaseCompleted, Svg: r.Data, SpansDigest: j.pixel.spansDigest}
		j.status = opwire.CaptureStatus{Phase: opwire.PhaseCompleted}
		return
	}
	if want := int(r.Width) * int(r.Height) * 4; want == 0 || len(r.Data) != want {
		j.status = opwire.CaptureStatus{Phase: opwire.PhaseFailed, Reason: "the capture's pixels do not match its size"}
		return
	}
	if r.UnknownTextures > 0 || r.RefusedUploads > 0 {
		inst.logger.Warn().Uint64("unknownTextures", r.UnknownTextures).Uint64("refusedUploads", r.RefusedUploads).
			Str("job", j.id).Msg("windowhost: a pixel capture has textures it could not draw")
	}
	j.result = capture.SourceResult{Phase: opwire.PhaseCompleted, Rgba: r.Data, Width: int(r.Width), Height: int(r.Height),
		PixelsPerPoint: ppp, SpansDigest: j.pixel.spansDigest}
	j.status = opwire.CaptureStatus{Phase: opwire.PhaseCompleted}
}

func (inst *Inst) failPixelJobLocked(j *captureJob, reason string) {
	inst.caps.pixelActive = nil
	j.status = opwire.CaptureStatus{Phase: opwire.PhaseFailed, Reason: reason}
	if f := typed.GetCurrentFffiVar(); f != nil && j.pixel.phase == pixelPhaseRecording {
		_ = f.EndRecording()
	}
}
