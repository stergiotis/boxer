package pulsesink

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/jfreymuth/pulse"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
)

// Options configure the connection and the stream.
type Options struct {
	// AppName is the client name the server shows in its mixer; empty is
	// "boxer".
	AppName string
	// Latency is the stream latency asked of the server; empty is 60 ms.
	// Lower means a quicker seek and more underflow risk.
	Latency time.Duration
	// Server is the PulseAudio server string; empty is the default lookup.
	Server string
	// Clock supplies wall time for the position interpolation; nil is
	// [sink.RealClock].
	Clock sink.ClockI
}

const (
	defaultAppName = "boxer"
	defaultLatency = 60 * time.Millisecond
)

// Sink plays one source through a PulseAudio playback stream.
type Sink struct {
	format pcm.Format
	frames int64
	src    pcm.SourceI
	clock  sink.ClockI

	client *pulse.Client
	stream *pulse.PlaybackStream
	// bufFrames is the server's target buffer in output frames; the audible
	// position lags the delivered count by this much.
	bufFrames int64

	mu     sync.Mutex
	state  sink.StateE
	ended  bool
	closed bool
	// corked is true between Pause and the Resume or Stop that follows: the
	// stream still holds its buffer, so Play resumes rather than restarts.
	corked bool

	cursor int64   // next source frame the callback reads
	frac   float64 // fractional part of the resampling position
	rate   float64
	volume float64

	// Position bookkeeping, all in source frames.
	startPos    int64     // cursor when the stream last started
	delivered   int64     // source frames handed to the server since startPos
	deliveredAt time.Time // when the callback last ran
	lastPos     int64     // the last position reported; positions never go back
	pausedPos   int64     // the position frozen by Pause

	scratch []float32
}

var _ sink.SinkI = (*Sink)(nil)

// OpenE connects to the server and opens a corked playback stream over src.
// The sink does not own src.
func OpenE(src pcm.SourceI, opts Options) (inst *Sink, err error) {
	if src == nil {
		return nil, eh.New("nil source")
	}
	format := src.Format()
	err = format.ValidateE()
	if err != nil {
		return nil, err
	}
	var chanOpt pulse.PlaybackOption
	switch format.Channels {
	case 1:
		chanOpt = pulse.PlaybackMono
	case 2:
		chanOpt = pulse.PlaybackStereo
	default:
		return nil, eb.Build().Uint16("channels", format.Channels).Errorf("only mono and stereo sources are played")
	}
	if opts.AppName == "" {
		opts.AppName = defaultAppName
	}
	if opts.Latency <= 0 {
		opts.Latency = defaultLatency
	}
	if opts.Clock == nil {
		opts.Clock = sink.RealClock{}
	}

	clientOpts := []pulse.ClientOption{pulse.ClientApplicationName(opts.AppName)}
	if opts.Server != "" {
		clientOpts = append(clientOpts, pulse.ClientServerString(opts.Server))
	}
	client, err := pulse.NewClient(clientOpts...)
	if err != nil {
		return nil, eb.Build().Str("server", opts.Server).Errorf("unable to connect to the audio server: %w", err)
	}

	inst = &Sink{
		format: format,
		frames: max(src.Frames(), 0),
		src:    src,
		clock:  opts.Clock,
		client: client,
		state:  sink.StateStopped,
		rate:   1,
		volume: sink.VolumeMaxIncl,
	}
	stream, err := client.NewPlayback(pulse.Float32Reader(inst.read),
		chanOpt,
		pulse.PlaybackSampleRate(int(format.SampleRate)),
		pulse.PlaybackLatency(opts.Latency.Seconds()),
		pulse.PlaybackMediaName("boxer audio"))
	if err != nil {
		client.Close()
		return nil, eb.Build().
			Uint32("sampleRate", format.SampleRate).
			Uint16("channels", format.Channels).
			Errorf("unable to open a playback stream: %w", err)
	}
	inst.stream = stream
	inst.bufFrames = int64(stream.BufferSize())
	return inst, nil
}

// Format implements [sink.SinkI].
func (inst *Sink) Format() (format pcm.Format) { return inst.format }

// Frames implements [sink.SinkI].
func (inst *Sink) Frames() (frames int64) { return inst.frames }

// Play implements [sink.SinkI]: a stopped sink starts (from 0 when it had
// reached the end), a paused one resumes with its buffer intact.
func (inst *Sink) Play() {
	inst.mu.Lock()
	if inst.closed || inst.state == sink.StatePlaying {
		inst.mu.Unlock()
		return
	}
	resume := inst.state == sink.StatePaused && inst.corked
	if !resume {
		if inst.ended || inst.cursor >= inst.frames {
			inst.cursor, inst.frac = 0, 0
		}
		inst.startPos, inst.delivered = inst.cursor, 0
		inst.lastPos = inst.cursor
	}
	inst.ended = false
	inst.state = sink.StatePlaying
	inst.deliveredAt = inst.clock.Now()
	inst.corked = false
	inst.mu.Unlock()
	// Stream calls run unlocked: Start blocks until the server asks for data,
	// and the callback that answers takes the lock.
	if resume {
		inst.stream.Resume()
	} else {
		inst.stream.Stop()
		inst.stream.Start()
	}
}

// Pause implements [sink.SinkI]: corks the stream, keeping its buffer.
func (inst *Sink) Pause() {
	inst.mu.Lock()
	if inst.closed || inst.state != sink.StatePlaying {
		inst.mu.Unlock()
		return
	}
	inst.pausedPos = inst.positionLocked(inst.clock.Now())
	inst.state = sink.StatePaused
	inst.corked = true
	inst.mu.Unlock()
	inst.stream.Pause()
}

// State implements [sink.SinkI].
func (inst *Sink) State() (state sink.StateE) {
	inst.mu.Lock()
	state = inst.state
	inst.mu.Unlock()
	return state
}

// Position implements [sink.SinkI].
func (inst *Sink) Position() (frame int64) {
	inst.mu.Lock()
	frame = inst.positionLocked(inst.clock.Now())
	inst.mu.Unlock()
	return frame
}

// positionLocked is the audible frame: delivered minus the server buffer,
// interpolated since the last callback, monotone, never past delivered.
func (inst *Sink) positionLocked(now time.Time) (frame int64) {
	switch inst.state {
	case sink.StatePaused:
		return inst.pausedPos
	case sink.StateStopped:
		return min(inst.cursor, inst.frames)
	}
	deliveredEnd := inst.startPos + inst.delivered
	base := deliveredEnd - int64(float64(inst.bufFrames)*inst.rate)
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

// SeekE implements [sink.SinkI]: moves the cursor and, when the stream holds
// audio, flushes it so the new position is heard at once.
func (inst *Sink) SeekE(frame int64) (err error) {
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return eb.Build().Int64("frame", frame).Errorf("seek on a closed sink")
	}
	frame = max(0, min(frame, inst.frames))
	inst.cursor, inst.frac = frame, 0
	inst.startPos, inst.delivered = frame, 0
	inst.lastPos, inst.pausedPos = frame, frame
	inst.ended = false
	playing := inst.state == sink.StatePlaying
	inst.deliveredAt = inst.clock.Now()
	inst.corked = false
	inst.mu.Unlock()
	// Pause corks (a running stream only), Stop drops it to idle; Start then
	// flushes the server buffer and restarts from the new cursor.
	inst.stream.Pause()
	inst.stream.Stop()
	if playing {
		inst.stream.Start()
	}
	return nil
}

// Rate implements [sink.SinkI].
func (inst *Sink) Rate() (rate float64) {
	inst.mu.Lock()
	rate = inst.rate
	inst.mu.Unlock()
	return rate
}

// SetRateE implements [sink.SinkI]: the resampling ratio from the next
// callback on; pitch follows.
func (inst *Sink) SetRateE(rate float64) (err error) {
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
		pos := inst.positionLocked(inst.clock.Now())
		inst.startPos, inst.delivered = pos, inst.startPos+inst.delivered-pos
		inst.deliveredAt = inst.clock.Now()
	}
	inst.rate = rate
	return nil
}

// Volume implements [sink.SinkI].
func (inst *Sink) Volume() (v float64) {
	inst.mu.Lock()
	v = inst.volume
	inst.mu.Unlock()
	return v
}

// SetVolumeE implements [sink.SinkI]: a software gain in [0, 1].
func (inst *Sink) SetVolumeE(v float64) (err error) {
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
func (inst *Sink) Ended() (ended bool) {
	inst.mu.Lock()
	ended = inst.ended
	inst.mu.Unlock()
	return ended
}

// Underflow reports whether the server ran dry since the stream last
// started — the source was too slow for the requested latency.
func (inst *Sink) Underflow() (yes bool) { return inst.stream.Underflow() }

// CloseE implements [sink.SinkI]: closes the stream and the connection.
func (inst *Sink) CloseE() (err error) {
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return nil
	}
	inst.closed = true
	inst.state = sink.StateStopped
	inst.mu.Unlock()
	inst.stream.Close()
	inst.client.Close()
	return nil
}

// read is the pull callback, on the stream's goroutine. It returns the
// number of float32 samples written; pulse.EndOfData ends the stream.
func (inst *Sink) read(out []float32) (n int, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed || inst.state != sink.StatePlaying {
		return 0, pulse.EndOfData
	}
	channels := int(inst.format.Channels)
	outFrames := len(out) / channels
	if outFrames == 0 {
		return 0, nil
	}
	ctx := context.Background()
	var produced int
	if inst.rate == 1 && inst.frac == 0 {
		produced, err = inst.src.ReadFramesAtE(ctx, inst.cursor, out[:outFrames*channels])
		if err != nil || produced == 0 {
			return inst.endLocked(err)
		}
		inst.cursor += int64(produced)
		inst.delivered += int64(produced)
	} else {
		need := framesNeeded(outFrames, inst.frac, inst.rate)
		if cap(inst.scratch) < int(need)*channels {
			inst.scratch = make([]float32, int(need)*channels)
		}
		var got int
		got, err = inst.src.ReadFramesAtE(ctx, inst.cursor, inst.scratch[:int(need)*channels])
		if err != nil || got < 2 {
			return inst.endLocked(err)
		}
		var consumed int64
		produced, consumed, inst.frac = resampleLinear(inst.scratch[:got*channels], channels, inst.frac, inst.rate, out[:outFrames*channels])
		if produced == 0 {
			return inst.endLocked(nil)
		}
		inst.cursor += consumed
		inst.delivered += consumed
	}
	if inst.volume != 1 {
		gain := float32(inst.volume)
		for i := range out[:produced*channels] {
			out[i] *= gain
		}
	}
	inst.deliveredAt = inst.clock.Now()
	return produced * channels, nil
}

// endLocked marks the end of playback: the source ran out (io.EOF or a
// short read at its end) or failed. Either way the stream stops.
func (inst *Sink) endLocked(readErr error) (n int, err error) {
	inst.cursor = inst.frames
	inst.state = sink.StateStopped
	inst.ended = true
	inst.corked = false
	inst.lastPos = inst.frames
	_ = readErr // a failing source ends playback like a finished one; the position readout shows where
	return 0, pulse.EndOfData
}
