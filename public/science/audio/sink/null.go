package sink

import (
	"math"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

// Null is a [SinkI] with no output device (ADR-0208 §SD6): it reports where
// playback would be, and plays nothing.
//
// The position is computed rather than ticked. Playback records an anchor —
// a frame and the clock reading at which playback was at that frame — and
// every observation projects it forward by the elapsed span times the rate.
// There is therefore no goroutine and no timer: a [ManualClock] makes a
// twelve-hour playback a single [ManualClock.Advance], and the end of the
// source is detected inside [Null.Position], [Null.State] and [Null.Ended]
// rather than announced by a callback. Because each observation projects
// from the anchor instead of accumulating increments, polling the position
// per rendered frame costs it no precision: a twelve-hour position is exact
// however often it was asked for on the way. The reverse of that is that the
// position follows the clock in both directions — a [ManualClock] moved
// backwards moves the playhead back, floored at the anchor.
//
// It reads no samples, so it holds the format and the frame count of the
// source and not the source itself; the constructor still takes one, so
// swapping in the M3 pulse sink is a change of constructor and nothing else.
// The volume is stored and returned and has no further effect.
type Null struct {
	clock  ClockI
	format pcm.Format
	frames int64

	mu sync.Mutex
	// anchorFrame is the position at anchorTime; while state is
	// StatePlaying the audible position runs ahead of it.
	anchorFrame int64
	anchorTime  time.Time
	rate        float64
	volume      float64
	state       StateE
	ended       bool
	closed      bool
}

var _ SinkI = (*Null)(nil)

// NewNull returns a stopped sink at frame 0, rate 1 and full volume. A nil
// clock means [RealClock]; a nil source means an empty one, which ends as
// soon as it is played.
func NewNull(src pcm.SourceI, clock ClockI) (inst *Null) {
	if clock == nil {
		clock = RealClock{}
	}
	var format pcm.Format
	var frames int64
	if src != nil {
		format = src.Format()
		frames = src.Frames()
	}
	if frames < 0 {
		frames = 0
	}
	return &Null{
		clock:  clock,
		format: format,
		frames: frames,
		rate:   1,
		volume: VolumeMaxIncl,
	}
}

// Format implements [SinkI].
func (inst *Null) Format() (format pcm.Format) { return inst.format }

// Frames implements [SinkI].
func (inst *Null) Frames() (frames int64) { return inst.frames }

// Play implements [SinkI]. On a closed sink it does nothing.
func (inst *Null) Play() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return
	}
	pos := inst.settleLocked()
	if inst.state == StatePlaying {
		return
	}
	if pos >= inst.frames {
		pos = 0
	}
	inst.anchorFrame = pos
	inst.anchorTime = inst.clock.Now()
	inst.state = StatePlaying
	inst.ended = false
}

// Pause implements [SinkI]. On a closed sink it does nothing.
func (inst *Null) Pause() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return
	}
	pos := inst.settleLocked()
	if inst.state != StatePlaying {
		return
	}
	inst.anchorFrame = pos
	inst.anchorTime = inst.clock.Now()
	inst.state = StatePaused
}

// State implements [SinkI]. A closed sink is stopped.
func (inst *Null) State() (state StateE) {
	inst.mu.Lock()
	inst.settleLocked()
	state = inst.state
	inst.mu.Unlock()
	return state
}

// Position implements [SinkI]. A closed sink holds the position it was
// closed at.
func (inst *Null) Position() (frame int64) {
	inst.mu.Lock()
	frame = inst.settleLocked()
	inst.mu.Unlock()
	return frame
}

// SeekE implements [SinkI]. It is an error on a closed sink.
//
// Seeking to Frames() is a position at the end, not the end of playback:
// [Null.Ended] stays false, because only playback running into the end sets
// it. A sink left playing there does run into it, and ends on the next
// observation whether or not the clock has moved.
func (inst *Null) SeekE(frame int64) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return eb.Build().Int64("frame", frame).Errorf("seek on a closed sink")
	}
	if frame < 0 {
		frame = 0
	}
	if frame > inst.frames {
		frame = inst.frames
	}
	inst.anchorFrame = frame
	inst.anchorTime = inst.clock.Now()
	inst.ended = false
	return nil
}

// Rate implements [SinkI].
func (inst *Null) Rate() (rate float64) {
	inst.mu.Lock()
	rate = inst.rate
	inst.mu.Unlock()
	return rate
}

// SetRateE implements [SinkI]. It is an error on a closed sink, and the rate
// is left unchanged.
func (inst *Null) SetRateE(rate float64) (err error) {
	if math.IsNaN(rate) || rate <= RateMinExcl || rate > RateMaxIncl {
		return eb.Build().
			Float64("rate", rate).
			Float64("minExcl", RateMinExcl).
			Float64("maxIncl", RateMaxIncl).
			Errorf("playback rate out of range")
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return eb.Build().Float64("rate", rate).Errorf("set rate on a closed sink")
	}
	// Re-anchor at the position the old rate reached, so the change of slope
	// does not move the playhead.
	inst.anchorFrame = inst.settleLocked()
	inst.anchorTime = inst.clock.Now()
	inst.rate = rate
	return nil
}

// Volume implements [SinkI].
func (inst *Null) Volume() (v float64) {
	inst.mu.Lock()
	v = inst.volume
	inst.mu.Unlock()
	return v
}

// SetVolumeE implements [SinkI]. It is an error on a closed sink, and the
// volume is left unchanged.
func (inst *Null) SetVolumeE(v float64) (err error) {
	if math.IsNaN(v) || v < VolumeMinIncl || v > VolumeMaxIncl {
		return eb.Build().
			Float64("volume", v).
			Float64("minIncl", VolumeMinIncl).
			Float64("maxIncl", VolumeMaxIncl).
			Errorf("volume out of range")
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return eb.Build().Float64("volume", v).Errorf("set volume on a closed sink")
	}
	inst.volume = v
	return nil
}

// Ended implements [SinkI]. A sink closed while playing at the end of the
// source stays ended.
func (inst *Null) Ended() (ended bool) {
	inst.mu.Lock()
	inst.settleLocked()
	ended = inst.ended
	inst.mu.Unlock()
	return ended
}

// CloseE implements [SinkI]. It settles the position one last time and then
// stops the transport: [Null.Play] and [Null.Pause] become no-ops,
// [Null.SeekE], [Null.SetRateE] and [Null.SetVolumeE] return an error, and
// the getters keep answering with the frozen values.
func (inst *Null) CloseE() (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return nil
	}
	inst.anchorFrame = inst.settleLocked()
	inst.state = StateStopped
	inst.closed = true
	return nil
}

// settleLocked returns the audible position, and stops the sink at the end
// of the source when playback has reached it. It is the only place the
// StatePlaying → StateStopped transition happens, which is why every getter
// runs it.
func (inst *Null) settleLocked() (frame int64) {
	if inst.state != StatePlaying {
		return inst.anchorFrame
	}
	now := inst.clock.Now()
	frame = inst.projectLocked(now)
	if frame >= inst.frames {
		inst.anchorFrame = inst.frames
		inst.anchorTime = now
		inst.state = StateStopped
		inst.ended = true
		return inst.frames
	}
	return frame
}

// projectLocked computes where playback started at the anchor would be at t.
func (inst *Null) projectLocked(t time.Time) (frame int64) {
	delta := inst.elapsedFramesLocked(t.Sub(inst.anchorTime))
	// Clamp before adding: delta is unbounded above, anchorFrame + delta
	// need not fit in int64.
	if room := inst.frames - inst.anchorFrame; delta >= room {
		return inst.frames
	}
	return inst.anchorFrame + delta
}

// elapsedFramesLocked converts an elapsed wall-clock span into frames at the
// current rate, saturating rather than wrapping. A negative span — reachable
// only with a [ManualClock] moved backwards, since [RealClock] readings are
// monotonic — is zero, which floors the projection at the anchor.
func (inst *Null) elapsedFramesLocked(elapsed time.Duration) (frames int64) {
	if elapsed <= 0 {
		return 0
	}
	// Scale the span, then convert exactly: pcm.Format.DurationToFrames does
	// the frames-per-second arithmetic in integers, which a
	// span-times-rate-times-sampleRate product in float64 loses precision on
	// well inside the twelve-hour case.
	scaled := float64(elapsed) * inst.rate
	if scaled >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	frames = inst.format.DurationToFrames(time.Duration(scaled))
	if frames < 0 {
		// Overflow inside the conversion (only reachable at an absurd sample
		// rate); the caller clamps to Frames() either way.
		return math.MaxInt64
	}
	return frames
}
