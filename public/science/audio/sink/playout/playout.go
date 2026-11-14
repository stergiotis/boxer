package playout

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
)

// Options configure [New].
type Options struct {
	// Clock supplies wall time for the position interpolation; nil is
	// [sink.RealClock].
	Clock sink.ClockI
	// DeviceRate is the sample rate the device takes frames at. Zero is the
	// source's own rate, for a device that resamples itself.
	DeviceRate uint32
}

// Engine is the transport over one source: it implements [sink.SinkI] and
// hands a device the frames to play. See the package documentation for the
// two halves of its method set.
type Engine struct {
	format pcm.Format
	frames int64
	src    pcm.SourceI
	clock  sink.ClockI
	// deviceRate is the rate ProduceLocked's frames are at, and srcPerDev
	// the source frames one of them spans at playback rate 1.
	deviceRate uint32
	srcPerDev  float64

	mu     sync.Mutex
	state  sink.StateE
	ended  bool
	closed bool
	// resumed wakes a device waiting in WaitLocked: Play and Close signal it.
	resumed *sync.Cond

	cursor int64   // next source frame ProduceLocked reads
	frac   float64 // fractional part of the resampling position
	rate   float64
	volume float64

	// lag is how many device frames the device holds unplayed, as it last
	// said; the audible position trails what was delivered by that much.
	lag int64

	// Position bookkeeping, all in source frames.
	startPos    int64     // cursor when playback last started
	delivered   int64     // source frames handed to the device since startPos
	deliveredAt time.Time // when the bookkeeping was last anchored
	lastPos     int64     // the last position reported; positions never go back
	pausedPos   int64     // the position frozen by Pause

	// scratch holds held source frames that start at cursor: the lookahead
	// the interpolation needs past the frames it consumes. Reads append past
	// them, so the source is read strictly forwards at any rate — an
	// ffmpeg-backed source restarts its process on any other offset.
	scratch []float32
	held    int
}

var _ sink.SinkI = (*Engine)(nil)

// New returns a stopped engine over src at frame 0, rate 1 and full volume.
// The engine does not own src.
func New(src pcm.SourceI, opts Options) (inst *Engine, err error) {
	if src == nil {
		return nil, eh.New("nil source")
	}
	format := src.Format()
	err = format.Validate()
	if err != nil {
		return nil, err
	}
	if opts.Clock == nil {
		opts.Clock = sink.RealClock{}
	}
	if opts.DeviceRate == 0 {
		opts.DeviceRate = format.SampleRate
	}
	inst = &Engine{
		format:     format,
		frames:     max(src.Frames(), 0),
		src:        src,
		clock:      opts.Clock,
		deviceRate: opts.DeviceRate,
		srcPerDev:  float64(format.SampleRate) / float64(opts.DeviceRate),
		state:      sink.StateStopped,
		rate:       1,
		volume:     sink.VolumeMaxIncl,
	}
	inst.resumed = sync.NewCond(&inst.mu)
	return inst, nil
}

// DeviceRate returns the sample rate of the frames [Engine.ProduceLocked]
// writes.
func (inst *Engine) DeviceRate() (rate uint32) { return inst.deviceRate }

// Format implements [sink.SinkI].
func (inst *Engine) Format() (format pcm.Format) { return inst.format }

// Frames implements [sink.SinkI].
func (inst *Engine) Frames() (frames int64) { return inst.frames }

// Play implements [sink.SinkI]: a stopped engine plays from its cursor (from
// 0 when it had reached the end), a paused one resumes.
func (inst *Engine) Play() {
	inst.mu.Lock()
	inst.PlayLocked()
	inst.mu.Unlock()
}

// Pause implements [sink.SinkI]: ProduceLocked's caller finds the engine not
// playing from its next look on.
func (inst *Engine) Pause() {
	inst.mu.Lock()
	inst.PauseLocked()
	inst.mu.Unlock()
}

// State implements [sink.SinkI].
func (inst *Engine) State() (state sink.StateE) {
	inst.mu.Lock()
	state = inst.state
	inst.mu.Unlock()
	return state
}

// Position implements [sink.SinkI].
func (inst *Engine) Position() (frame int64) {
	inst.mu.Lock()
	frame = inst.PositionLocked()
	inst.mu.Unlock()
	return frame
}

// SeekFrame implements [sink.SinkI]: moves the cursor ProduceLocked reads
// from. What the device already holds is the device's to drop or play out.
func (inst *Engine) SeekFrame(frame int64) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.SeekFrameLocked(frame)
}

// Rate implements [sink.SinkI].
func (inst *Engine) Rate() (rate float64) {
	inst.mu.Lock()
	rate = inst.rate
	inst.mu.Unlock()
	return rate
}

// SetRate implements [sink.SinkI]: the resampling ratio from the next
// ProduceLocked on; pitch follows.
func (inst *Engine) SetRate(rate float64) (err error) {
	if math.IsNaN(rate) || rate <= sink.RateMinExcl || rate > sink.RateMaxIncl {
		return eb.Build().
			Float64("rate", rate).
			Float64("minExcl", sink.RateMinExcl).
			Float64("maxIncl", sink.RateMaxIncl).
			Errorf("playback rate out of range")
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return eb.Build().Float64("rate", rate).Errorf("set rate on a closed sink")
	}
	// Re-anchor so the interpolation does not jump with the new ratio.
	if inst.state == sink.StatePlaying {
		now := inst.clock.Now()
		pos := inst.positionLocked(now)
		inst.startPos, inst.delivered = pos, inst.startPos+inst.delivered-pos
		inst.deliveredAt = now
	}
	inst.rate = rate
	if inst.stepLocked() == 1 {
		// Back at a step of one the fraction is a sub-frame offset nobody
		// hears; dropping it returns playback to the bit-exact direct path.
		inst.frac = 0
	}
	return nil
}

// Volume implements [sink.SinkI].
func (inst *Engine) Volume() (v float64) {
	inst.mu.Lock()
	v = inst.volume
	inst.mu.Unlock()
	return v
}

// SetVolume implements [sink.SinkI]: a software gain in [0, 1].
func (inst *Engine) SetVolume(v float64) (err error) {
	if math.IsNaN(v) || v < sink.VolumeMinIncl || v > sink.VolumeMaxIncl {
		return eb.Build().
			Float64("volume", v).
			Float64("minIncl", sink.VolumeMinIncl).
			Float64("maxIncl", sink.VolumeMaxIncl).
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

// Ended implements [sink.SinkI].
func (inst *Engine) Ended() (ended bool) {
	inst.mu.Lock()
	ended = inst.ended
	inst.mu.Unlock()
	return ended
}

// Close implements [sink.SinkI]: it stops the transport and releases a
// device waiting in WaitLocked. The device's own resources are the device's
// to release.
func (inst *Engine) Close() (err error) {
	inst.mu.Lock()
	inst.CloseLocked()
	inst.mu.Unlock()
	return nil
}

// Lock takes the engine's lock, which every method ending in Locked needs
// held. The methods without that suffix take it themselves.
func (inst *Engine) Lock() { inst.mu.Lock() }

// Unlock releases the engine's lock.
func (inst *Engine) Unlock() { inst.mu.Unlock() }

// ClosedLocked reports whether the engine was closed.
func (inst *Engine) ClosedLocked() (closed bool) { return inst.closed }

// StateLocked is [Engine.State] with the lock held.
func (inst *Engine) StateLocked() (state sink.StateE) { return inst.state }

// EndedLocked is [Engine.Ended] with the lock held.
func (inst *Engine) EndedLocked() (ended bool) { return inst.ended }

// PlayLocked is [Engine.Play] with the lock held. It reports whether the
// call changed the state, which is false on a closed or playing engine.
func (inst *Engine) PlayLocked() (changed bool) {
	if inst.closed || inst.state == sink.StatePlaying {
		return false
	}
	if inst.state != sink.StatePaused {
		if inst.ended || inst.cursor >= inst.frames {
			inst.cursor, inst.frac, inst.held = 0, 0, 0
		}
		inst.startPos, inst.delivered = inst.cursor, 0
		inst.lastPos = inst.cursor
	}
	// After a pause the device has played out what it held, so the position
	// lags the truth by up to one lag until the interpolation catches up.
	inst.ended = false
	inst.state = sink.StatePlaying
	inst.deliveredAt = inst.clock.Now()
	inst.resumed.Broadcast()
	return true
}

// PauseLocked is [Engine.Pause] with the lock held, reporting whether the
// call changed the state.
func (inst *Engine) PauseLocked() (changed bool) {
	if inst.closed || inst.state != sink.StatePlaying {
		return false
	}
	inst.pausedPos = inst.positionLocked(inst.clock.Now())
	inst.state = sink.StatePaused
	return true
}

// SeekFrameLocked is [Engine.SeekFrame] with the lock held.
func (inst *Engine) SeekFrameLocked(frame int64) (err error) {
	if inst.closed {
		return eb.Build().Int64("frame", frame).Errorf("seek on a closed sink")
	}
	frame = max(0, min(frame, inst.frames))
	inst.cursor, inst.frac, inst.held = frame, 0, 0
	inst.startPos, inst.delivered = frame, 0
	inst.lastPos, inst.pausedPos = frame, frame
	inst.ended = false
	inst.deliveredAt = inst.clock.Now()
	return nil
}

// CloseLocked marks the engine closed and wakes a waiting device. It reports
// whether the engine was open.
func (inst *Engine) CloseLocked() (wasOpen bool) {
	if inst.closed {
		return false
	}
	inst.closed = true
	inst.state = sink.StateStopped
	inst.resumed.Broadcast()
	return true
}

// WaitLocked releases the lock until the next Play or Close and takes it
// again, as [sync.Cond.Wait] does. A device that pulls on a goroutine of its
// own waits here while there is nothing to play; the caller checks the state
// again on return.
func (inst *Engine) WaitLocked() { inst.resumed.Wait() }

// SetLagLocked records how many device frames the device holds unplayed. A
// device with a fixed buffer says so once; one that is told what was played
// says so whenever it learns. While playing it also anchors the wall-clock
// interpolation at now, since the figure describes this instant.
func (inst *Engine) SetLagLocked(deviceFrames int64) {
	inst.lag = max(deviceFrames, 0)
	if inst.state == sink.StatePlaying {
		now := inst.clock.Now()
		// Read the position first: it is monotone, so the new anchor cannot
		// move the readout back.
		_ = inst.positionLocked(now)
		inst.deliveredAt = now
	}
}

// PositionLocked is [Engine.Position] with the lock held.
func (inst *Engine) PositionLocked() (frame int64) {
	return inst.positionLocked(inst.clock.Now())
}

// stepLocked is how many source frames one device frame spans.
func (inst *Engine) stepLocked() (step float64) { return inst.rate * inst.srcPerDev }

// positionLocked is the audible frame: delivered minus what the device
// holds, interpolated since the last anchor, monotone, never past delivered.
func (inst *Engine) positionLocked(now time.Time) (frame int64) {
	switch inst.state {
	case sink.StatePaused:
		return inst.pausedPos
	case sink.StateStopped:
		return min(inst.cursor, inst.frames)
	}
	deliveredEnd := inst.startPos + inst.delivered
	base := deliveredEnd - int64(float64(inst.lag)*inst.stepLocked())
	if base < inst.startPos {
		base = inst.startPos
	}
	elapsed := now.Sub(inst.deliveredAt)
	if elapsed > 0 {
		base += inst.format.DurationToFrames(time.Duration(float64(elapsed) * inst.rate))
	}
	frame = min(base, deliveredEnd, inst.frames)
	if frame < inst.lastPos {
		frame = inst.lastPos
	}
	inst.lastPos = frame
	return frame
}

// ProduceLocked fills out with up to outFrames device frames from the cursor
// on, at the engine's rate and volume, and returns how many it wrote. Zero
// means the source has ended or failed; the caller then calls
// [Engine.EndLocked]. It is called only while the engine is playing.
//
// The source is read strictly forwards: the interpolation's lookahead frames
// stay in scratch across calls instead of being read again, so a decoder
// whose random access is a process restart is never restarted by playback.
func (inst *Engine) ProduceLocked(out []float32, outFrames int) (produced int) {
	channels := int(inst.format.Channels)
	outFrames = min(outFrames, len(out)/channels)
	if outFrames <= 0 {
		return 0
	}
	ctx := context.Background()
	step := inst.stepLocked()
	if step == 1 && inst.frac == 0 {
		// Direct path: held lookahead frames go out first, the rest is read
		// from where the last read ended.
		pre := min(inst.held, outFrames)
		copy(out[:pre*channels], inst.scratch[:pre*channels])
		inst.dropHeldLocked(pre)
		produced = pre
		if pre < outFrames {
			got, err := inst.src.ReadFramesAt(ctx, inst.cursor, out[pre*channels:outFrames*channels])
			if err == nil && got > 0 {
				produced += got
				inst.cursor += int64(got)
				inst.delivered += int64(got)
			}
		}
		if produced == 0 {
			return 0
		}
	} else {
		need := int(framesNeeded(outFrames, inst.frac, step))
		if cap(inst.scratch) < need*channels {
			grown := make([]float32, need*channels)
			copy(grown, inst.scratch[:inst.held*channels])
			inst.scratch = grown
		}
		if inst.held < need {
			got, err := inst.src.ReadFramesAt(ctx, inst.cursor+int64(inst.held), inst.scratch[inst.held*channels:need*channels])
			if err == nil {
				inst.held += got
			}
		}
		if inst.held < 2 {
			return 0
		}
		var consumed int64
		produced, consumed, inst.frac = resampleLinear(inst.scratch[:inst.held*channels], channels, inst.frac, step, out[:outFrames*channels])
		if produced == 0 {
			return 0
		}
		inst.dropHeldLocked(int(consumed))
	}
	if inst.volume != 1 {
		gain := float32(inst.volume)
		for i := range out[:produced*channels] {
			out[i] *= gain
		}
	}
	inst.deliveredAt = inst.clock.Now()
	return produced
}

// dropHeldLocked consumes k frames from the front of the lookahead: the
// cursor moves past them and what remains slides to the front.
func (inst *Engine) dropHeldLocked(k int) {
	if k <= 0 {
		return
	}
	channels := int(inst.format.Channels)
	if k >= inst.held {
		inst.cursor += int64(k)
		inst.delivered += int64(k)
		inst.held = 0
		return
	}
	rem := inst.held - k
	copy(inst.scratch[:rem*channels], inst.scratch[k*channels:inst.held*channels])
	inst.held = rem
	inst.cursor += int64(k)
	inst.delivered += int64(k)
}

// EndLocked marks the end of playback: the source ran out or failed. A
// failing source ends playback like a finished one, and the position readout
// shows where.
func (inst *Engine) EndLocked() {
	inst.cursor = inst.frames
	inst.state = sink.StateStopped
	inst.ended = true
	inst.lastPos = inst.frames
}
