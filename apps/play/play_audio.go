package play

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/science/audio/peaks"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/science/audio/sink/pulsesink"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/waveform"
)

// play_audio.go is ADR-0245 §SD6's play half: the `audio/wav` gloss's block
// face — a static waveform with play and pause — and the one now-playing
// session every pane shares.
//
// Not the full waveform.Player (ADR-0208): zoom, regions and lanes need a
// track, and a track needs a reopenable staged source, a background peaks
// build and a window cache — the right weight for one open recording and the
// wrong one for a page of heroes. A WAV held in a cell needs none of it: the
// native reader takes an io.ReaderAt, so the cell's bytes are read where
// they are.

const (
	// audioMaxBytes bounds a cell offered for decode; past it the face says
	// so, like the text limit (§SD6).
	audioMaxBytes = 64 << 20
	// audioOverviewColumns is what a recording is reduced to and all that is
	// retained of it — about the widest face at one column per pixel.
	audioOverviewColumns = 640
	// audioControlsHeight is the transport row under the waveform.
	audioControlsHeight float32 = 24
	// audioControlsGap separates the transport row's items.
	audioControlsGap float32 = 6
	// audioDetailHeight is the block face's height in Detail and on the
	// leeway card, where nothing else decides it.
	audioDetailHeight float32 = 120
	// audioFaceWidth is its width there: those panes lay a face out before
	// they know their own width, as they do an image's box.
	audioFaceWidth float32 = 400
	// audioRepaintHz keeps the playhead moving while something plays.
	audioRepaintHz = 30
)

// audioArtifact is one recording, reduced: what the artifact caches retain.
type audioArtifact struct {
	overview *peaks.Overview
	info     gloss.WavInfo
	reason   string
}

// buildAudioArtifact reads a recording from a cell's bytes and reduces it.
// raw may alias Arrow memory: it is read here and not retained.
func buildAudioArtifact(raw string) (a *audioArtifact) {
	a = &audioArtifact{}
	if len(raw) > audioMaxBytes {
		a.reason = fmt.Sprintf("%s is over the %s a recording is read at", humanize.IBytes(uint64(len(raw))), humanize.IBytes(audioMaxBytes))
		return
	}
	// A strings.Reader is an io.ReaderAt over the string itself: no copy.
	f, err := wavfile.NewReaderE(strings.NewReader(raw), int64(len(raw)))
	if err != nil {
		a.reason = "not a readable WAVE file: " + err.Error()
		return
	}
	format := f.Format()
	a.info = gloss.WavInfo{
		SampleRate: format.SampleRate, Channels: format.Channels, Bits: f.BitsPerSample(),
		Frames: f.Frames(), Duration: format.FramesToDuration(f.Frames()), Truncated: f.IsTruncated(),
	}
	a.overview, err = peaks.OverviewE(context.Background(), f, audioOverviewColumns)
	if err != nil {
		a.overview = nil
		a.reason = "unable to read the samples: " + err.Error()
	}
	return
}

// audioSession is the one recording play is playing. Its bytes are a COPY of
// the cell's: the source outlives the frame, and the Arrow view does not.
type audioSession struct {
	key string
	// result is the result the cell belonged to; the session ends with it.
	result ResultID

	mu        sync.Mutex
	sink      sink.SinkI
	file      *wavfile.File
	deviceErr string
	opening   bool
	closed    bool
}

// audioState is a session's state as a face reads it, once per frame.
type audioState struct {
	active    bool // this cell is the session's
	opening   bool
	playing   bool
	fraction  float64
	position  time.Duration
	deviceErr string
}

// audioKey names a cell: the pane, the column and the Arrow row, plus the
// ordinal where a pane shows several values of one column.
func audioKey(pane string, col int, row int64, ord int) string {
	return fmt.Sprintf("%s/%d/%d/%d", pane, col, row, ord)
}

// audioStateOf reports the session's state for one cell.
func (inst *PlayApp) audioStateOf(key string) (st audioState) {
	s := inst.audio
	if s == nil || s.key != key {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st.active, st.opening, st.deviceErr = true, s.opening, s.deviceErr
	if s.sink == nil {
		return
	}
	st.playing = s.sink.State() == sink.StatePlaying
	if frames := s.sink.Frames(); frames > 0 {
		pos := s.sink.Position()
		st.fraction = min(float64(pos)/float64(frames), 1)
		st.position = s.sink.Format().FramesToDuration(pos)
	}
	return
}

// audioToggle plays or pauses one cell's recording. Starting a recording
// stops whichever was playing: one session, shared by every pane (§SD6). raw
// is called only when a new session starts, and its result is copied.
func (inst *PlayApp) audioToggle(key string, raw func() string) {
	if s := inst.audio; s != nil && s.key == key {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.sink == nil {
			return // still opening
		}
		switch {
		case s.sink.State() == sink.StatePlaying:
			s.sink.Pause()
		case s.sink.Ended():
			_ = s.sink.SeekE(0)
			s.sink.Play()
		default:
			s.sink.Play()
		}
		return
	}
	inst.audioStop()
	data := strings.Clone(raw())
	s := &audioSession{key: key, result: inst.frameResult, opening: true}
	inst.audio = s
	// Off the render thread: opening the device talks to the sound server.
	go s.open(data, inst.audioNoDevice)
}

// open reads the copy and opens the output. A host with no sound server gets
// the device-less clock and a reason on screen rather than a failed open
// (ADR-0208 §SD6) — the playhead still moves, so the pane still says what
// playing would look like.
func (inst *audioSession) open(data string, noDevice bool) {
	// Nothing here holds the lock while it talks to the sound server: the
	// faces read the session's state every frame.
	var out sink.SinkI
	deviceErr := ""
	f, err := wavfile.NewReaderE(strings.NewReader(data), int64(len(data)))
	switch {
	case err != nil:
		deviceErr = "not a readable WAVE file: " + err.Error()
	case noDevice:
		deviceErr = "no output device (disabled)"
	default:
		dev, oerr := pulsesink.OpenE(f, pulsesink.Options{AppName: "boxer play"})
		if oerr != nil {
			deviceErr = "no output device: " + oerr.Error()
		} else {
			out = dev
		}
	}
	if out == nil && f != nil {
		out = sink.NewNull(f, nil)
	}

	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.opening = false
	if inst.closed {
		// Stopped while opening: the session is gone, so is what it opened.
		if out != nil {
			_ = out.CloseE()
		}
		if f != nil {
			_ = f.CloseE()
		}
		return
	}
	inst.file, inst.sink, inst.deviceErr = f, out, deviceErr
	if out != nil {
		out.Play()
	}
}

// audioSeek moves the session's playhead when the cell is the one playing.
func (inst *PlayApp) audioSeek(key string, fraction float64) {
	s := inst.audio
	if s == nil || s.key != key {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sink != nil {
		_ = s.sink.SeekE(int64(fraction * float64(s.sink.Frames())))
	}
}

// audioStop ends the session, if there is one.
func (inst *PlayApp) audioStop() {
	s := inst.audio
	if s == nil {
		return
	}
	inst.audio = nil
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.sink != nil {
		_ = s.sink.CloseE()
		s.sink = nil
	}
	if s.file != nil {
		_ = s.file.CloseE()
		s.file = nil
	}
}

// audioFollowResult notes the frame's result and stops a session that
// belongs to another: a recording must not keep playing from a result that
// was replaced.
func (inst *PlayApp) audioFollowResult(result ResultID) {
	inst.frameResult = result
	if inst.audio != nil && inst.audio.result != result {
		inst.audioStop()
	}
}

// renderAudioFace draws one recording's block face in a box of (w, h): the
// waveform, and under it the transport — play or pause, the position over the
// length, and why there is no sound when there is none. It reports a click on
// the waveform that a card should take as its own.
//
// scope must be unique per cell on screen; raw is called only when the user
// starts this recording.
func (inst *PlayApp) renderAudioFace(ids *c.WidgetIdStack, scope string, key string, a *audioArtifact, w, h float32, raw func() string) (clicked bool) {
	st := inst.audioStateOf(key)
	progress := -1.0
	if st.active && !st.opening {
		progress = st.fraction
	}
	for range c.IdScope(ids.PrepareStr(scope)) {
		c.UiSetItemSpacing(0, 0)
		res := waveform.RenderThumbnail(waveform.ThumbnailInput{
			Ids: ids, Key: "wave", Overview: a.overview, W: w, H: max(h-audioControlsHeight, 8), Progress: progress,
		})
		if res.Clicked {
			clicked = true
			if st.active {
				inst.audioSeek(key, res.Fraction)
			}
		}
		for range c.HorizontalTop().KeepIter() {
			c.UiSetItemSpacing(audioControlsGap, 0)
			glyph := icons.PhPlay
			if st.playing {
				glyph = icons.PhPause
			}
			for range c.EnabledUi(!st.opening).KeepIter() {
				if c.Button(ids.PrepareStr("toggle"), c.Atoms().Text(glyph).Keep()).Small().Frame(false).
					SendResp().HasPrimaryClicked() {
					inst.audioToggle(key, raw)
				}
			}
			readout := gloss.FormatClock(a.info.Duration)
			if st.active {
				readout = gloss.FormatClock(st.position) + " / " + readout
			}
			c.LabelAtoms(c.Atoms().BeginRichText(readout).Small().Monospace().End().Keep()).Selectable(false).Send()
			switch {
			case st.deviceErr != "":
				for range c.HoverText(st.deviceErr).KeepIter() {
					c.LabelAtoms(c.Atoms().BeginRichText(icons.PhSpeakerSlash).Small().Weak().End().Keep()).Selectable(false).Send()
				}
			case a.info.Truncated:
				for range c.HoverText("the header promises more than the file holds").KeepIter() {
					c.LabelAtoms(c.Atoms().BeginRichText(icons.PhWarning).Small().Weak().End().Keep()).Selectable(false).Send()
				}
			}
		}
	}
	if st.playing || st.opening {
		c.RequestRepaintAfter(1.0 / audioRepaintHz)
	}
	return
}
