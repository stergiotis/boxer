package windowhost

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Pixel captures (ADR-0281 §SD4, §SD5). A capture of windows takes three
// frames: in the first the host records its outgoing stream and marks where
// each window's emission begins and ends; in the second it sends the granted
// windows' spans in one captureReplay, which the client replays into a
// separate context and rasterizes, and that frame's Sync fetches the pixels;
// in the third the host writes them out.

// pixelPhaseE is where a pixel capture stands.
type pixelPhaseE uint8

const (
	pixelQueued pixelPhaseE = iota
	pixelRecording
	pixelReplayPending
	pixelAwaitingResult
)

// pixelJob is the state a pixel capture carries between frames.
type pixelJob struct {
	keys      []WindowKeyT
	phase     pixelPhaseE
	requestId uint64
	// spans are offsets into the frame's recording, per granted window.
	spans  [][2]int
	stream []byte
}

// OpsCapturePixels queues a capture of the windows' pixels as PNG. Only the
// windows named are drawn; the job's status follows through
// OpsCaptureStatus. Only an open window can be captured.
func (inst *Inst) OpsCapturePixels(keys []uint64) (job string, err error) {
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
	if err = inst.caps.ensureDirLocked(); err != nil {
		return
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	job = "cap-" + hex.EncodeToString(b[:])
	j := &captureJob{id: job, key: wk[0], path: filepath.Join(inst.caps.dir, job+".png"), requested: time.Now(),
		status: opwire.CaptureStatus{Phase: opwire.PhaseRunning, MediaType: "image/png"},
		pixel:  &pixelJob{keys: wk}}
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
		case pixelAwaitingResult:
			if r, ok := sm.TakeCaptureResult(); ok && r.RequestId == p.requestId {
				inst.finishPixelJobLocked(j, r)
				j = nil
			} else if time.Since(j.requested) > captureTimeout {
				inst.failPixelJobLocked(j, "the client did not answer the capture in time")
				j = nil
			}
		case pixelReplayPending:
			c.CaptureReplay(p.requestId, p.stream)
			sm.WantCaptureResult()
			p.phase = pixelAwaitingResult
			p.stream = nil
		}
	}
	if j == nil && len(inst.caps.pixelQueue) > 0 {
		j = inst.caps.pixelQueue[0]
		inst.caps.pixelQueue = inst.caps.pixelQueue[1:]
		inst.caps.pixelActive = j
	}
	if j != nil && j.pixel.phase == pixelQueued {
		if f := typed.GetCurrentFffiVar(); f != nil {
			f.BeginRecording()
			j.pixel.phase = pixelRecording
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
	recording := inst.caps.pixelActive != nil && inst.caps.pixelActive.pixel.phase == pixelRecording
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
func (inst *Inst) pixelWindowSpan(key WindowKeyT, begin int, end int) {
	if begin < 0 || end < begin {
		return
	}
	inst.caps.mu.Lock()
	defer inst.caps.mu.Unlock()
	j := inst.caps.pixelActive
	if j == nil || j.pixel.phase != pixelRecording || !slices.Contains(j.pixel.keys, key) {
		return
	}
	j.pixel.spans = append(j.pixel.spans, [2]int{begin, end})
}

// pixelFrameEnd closes the recording after the windows were emitted and
// keeps the granted windows' spans for the next frame's replay.
func (inst *Inst) pixelFrameEnd() {
	inst.caps.mu.Lock()
	defer inst.caps.mu.Unlock()
	j := inst.caps.pixelActive
	if j == nil || j.pixel.phase != pixelRecording {
		return
	}
	f := typed.GetCurrentFffiVar()
	if f == nil {
		inst.failPixelJobLocked(j, "no client channel")
		return
	}
	rec := f.EndRecording()
	p := j.pixel
	if len(p.spans) == 0 {
		inst.failPixelJobLocked(j, "none of the windows was drawn")
		return
	}
	n := 0
	for _, s := range p.spans {
		n += s[1] - s[0]
	}
	stream := make([]byte, 0, n)
	for _, s := range p.spans {
		stream = append(stream, rec[s[0]:s[1]]...)
	}
	p.stream, p.spans = stream, nil
	p.phase = pixelReplayPending
}

func (inst *Inst) finishPixelJobLocked(j *captureJob, r c.CaptureResultValue) {
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
	if want := int(r.Width) * int(r.Height) * 4; want == 0 || len(r.Rgba) != want {
		j.status = opwire.CaptureStatus{Phase: opwire.PhaseFailed, Reason: "the capture's pixels do not match its size"}
		return
	}
	img := &image.RGBA{Pix: r.Rgba, Stride: int(r.Width) * 4, Rect: image.Rect(0, 0, int(r.Width), int(r.Height))}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		j.status = opwire.CaptureStatus{Phase: opwire.PhaseFailed, Reason: "png: " + err.Error()}
		return
	}
	if err := os.WriteFile(j.path, buf.Bytes(), 0o600); err != nil {
		j.status = opwire.CaptureStatus{Phase: opwire.PhaseFailed, Reason: "write: " + err.Error()}
		return
	}
	if r.UnknownTextures > 0 || r.RefusedUploads > 0 {
		inst.logger.Warn().Uint64("unknownTextures", r.UnknownTextures).Uint64("refusedUploads", r.RefusedUploads).
			Str("job", j.id).Msg("windowhost: a pixel capture has textures it could not draw")
	}
	j.status = opwire.CaptureStatus{Phase: opwire.PhaseCompleted, Path: j.path, MediaType: "image/png", Bytes: int64(buf.Len())}
}

func (inst *Inst) failPixelJobLocked(j *captureJob, reason string) {
	inst.caps.pixelActive = nil
	j.status = opwire.CaptureStatus{Phase: opwire.PhaseFailed, Reason: reason}
	if f := typed.GetCurrentFffiVar(); f != nil && j.pixel.phase == pixelRecording {
		_ = f.EndRecording()
	}
}
