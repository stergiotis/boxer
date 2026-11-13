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

	// primeBuffers is how many server buffers of frames the callback
	// delivers after the start before it may wait; the server asks for one
	// before it plays.
	primeBuffers = 2
	// startCloseWait bounds how long Close waits for a start in flight.
	startCloseWait = 250 * time.Millisecond
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
	// resumed wakes the pull callback, which waits on it whenever the sink
	// is not playing. Nothing but the one start ever touches the stream:
	// corking it and uncorking it again makes the server send a Started
	// event the library delivers with a blocking send that nobody receives
	// after the first Start, and its read loop parks for good
	// (jfreymuth/pulse#52); and a second Start waits for a Started the
	// server does not always send. The callback blocks on this instead, as
	// oto's PulseAudio backend does; the stream stays running and the
	// server, starved, reports an underflow. Lazily made under mu so a Sink
	// built without Open still reads.
	resumed *sync.Cond
	// started is set once the stream's one start has been asked for, and
	// startDone is closed when the library's Start returned from it.
	started   bool
	startDone chan struct{}
	// primeLeft is how many frames the callback still owes the server
	// before it may wait: after the start the server plays nothing until it
	// holds a buffer's worth, and the library's Start waits for that. They
	// are silence where the source has nothing to give.
	primeLeft int64
	// underflowFrozen latches the stream's underflow report the first time
	// the callback is left to wait, because from then on the report also
	// counts the underflows the waits cause.
	underflowFrozen bool
	underflowValue  bool

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

	// scratch holds held source frames that start at cursor: the lookahead
	// the interpolation needs past the frames it consumes. Reads append past
	// them, so the source is read strictly forwards at any rate — an
	// ffmpeg-backed source restarts its process on any other offset.
	scratch []float32
	held    int
}

var _ sink.SinkI = (*Sink)(nil)

// Open connects to the server and opens a corked playback stream over src.
// The sink does not own src.
func Open(src pcm.SourceI, opts Options) (inst *Sink, err error) {
	if src == nil {
		return nil, eh.New("nil source")
	}
	format := src.Format()
	err = format.Validate()
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

// Play implements [sink.SinkI]: a stopped sink plays from its cursor (from 0
// when it had reached the end), a paused one resumes. It returns at once:
// the first Play starts the stream on a goroutine of its own, and every
// later one only releases the waiting callback.
func (inst *Sink) Play() {
	inst.mu.Lock()
	if inst.closed || inst.state == sink.StatePlaying {
		inst.mu.Unlock()
		return
	}
	if inst.state != sink.StatePaused {
		if inst.ended || inst.cursor >= inst.frames {
			inst.cursor, inst.frac, inst.held = 0, 0, 0
		}
		inst.startPos, inst.delivered = inst.cursor, 0
		inst.lastPos = inst.cursor
	}
	// After a pause the server buffer has drained, so the position lags the
	// truth by up to one buffer until the interpolation catches up.
	inst.ended = false
	inst.state = sink.StatePlaying
	inst.deliveredAt = inst.clock.Now()
	start := !inst.started && inst.stream != nil
	if start {
		inst.started = true
		inst.startDone = make(chan struct{})
		inst.primeLeft = primeBuffers * inst.bufFrames
	}
	inst.condLocked().Broadcast()
	inst.mu.Unlock()
	if start {
		go inst.startStream()
	}
}

// startStream is the stream's one start. The library's Start returns when
// the server reports the stream started, which it does once it holds a
// buffer of frames; the callback delivers those whatever the transport does
// meanwhile (primeLeft). Nothing waits for this goroutine but Close, for a
// bounded time: a report that never arrives leaves it waiting and the
// stream playing.
func (inst *Sink) startStream() {
	defer close(inst.startDone)
	inst.stream.Start()
}

// condLocked returns the resume condition, making it on first use; mu held.
func (inst *Sink) condLocked() *sync.Cond {
	if inst.resumed == nil {
		inst.resumed = sync.NewCond(&inst.mu)
	}
	return inst.resumed
}

// Pause implements [sink.SinkI]: the pull callback waits from its next call
// on. The stream is not corked (see resumed), so what the server already
// holds, one latency at most, still plays.
func (inst *Sink) Pause() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed || inst.state != sink.StatePlaying {
		return
	}
	inst.pausedPos = inst.positionLocked(inst.clock.Now())
	inst.state = sink.StatePaused
	inst.freezeUnderflowLocked()
}

// freezeUnderflowLocked keeps the stream's underflow report as it is now;
// mu held. It is called before the callback is first left to wait.
func (inst *Sink) freezeUnderflowLocked() {
	if inst.underflowFrozen || inst.stream == nil {
		return
	}
	inst.underflowFrozen = true
	inst.underflowValue = inst.stream.Underflow()
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

// SeekFrame implements [sink.SinkI]: moves the cursor, which the callback
// reads from on its next call. The stream is left alone, so while playing
// the server still plays what it holds of the old position, one latency at
// most, before the new one is heard.
func (inst *Sink) SeekFrame(frame int64) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
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

// Rate implements [sink.SinkI].
func (inst *Sink) Rate() (rate float64) {
	inst.mu.Lock()
	rate = inst.rate
	inst.mu.Unlock()
	return rate
}

// SetRate implements [sink.SinkI]: the resampling ratio from the next
// callback on; pitch follows.
func (inst *Sink) SetRate(rate float64) (err error) {
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
	if rate == 1 {
		// Back at unity the fraction is a sub-frame offset nobody hears;
		// dropping it returns playback to the bit-exact direct path.
		inst.frac = 0
	}
	return nil
}

// Volume implements [sink.SinkI].
func (inst *Sink) Volume() (v float64) {
	inst.mu.Lock()
	v = inst.volume
	inst.mu.Unlock()
	return v
}

// SetVolume implements [sink.SinkI]: a software gain in [0, 1].
func (inst *Sink) SetVolume(v float64) (err error) {
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

// Underflow reports whether the server ran dry while the source was too slow
// for the requested latency. A pause and the end of the source starve the
// server by design, and the library's report cannot be cleared without
// restarting the stream, so the report stays at the value it had when
// playback was first paused or first ended.
func (inst *Sink) Underflow() (yes bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.underflowFrozen {
		return inst.underflowValue
	}
	if inst.stream == nil {
		return false
	}
	return inst.stream.Underflow()
}

// Close implements [sink.SinkI]: closes the stream and the connection.
func (inst *Sink) Close() (err error) {
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return nil
	}
	inst.closed = true
	inst.state = sink.StateStopped
	// A waiting callback must return before the stream's close round trip
	// can be answered: the library's read loop may be waiting to hand it
	// the next buffer request.
	inst.condLocked().Broadcast()
	startDone := inst.startDone
	inst.mu.Unlock()
	if startDone != nil {
		// A start in flight talks to the server on this connection; give it
		// the time of a round trip before the stream goes away under it.
		select {
		case <-startDone:
		case <-time.After(startCloseWait):
		}
	}
	inst.stream.Close()
	inst.client.Close()
	return nil
}

// read is the stream's pull callback: it fills out with interleaved frames
// and returns the number of float32 samples written. It returns only with
// frames, or with pulse.EndOfData once the sink is closed: while the sink is
// not playing it waits, so the stream never goes idle and is never started a
// second time. After the start it first delivers primeLeft frames, silence
// where the sink is not playing or the source has ended or failed, because
// the server plays nothing, and reports no start, before it holds a buffer.
//
// The source is read strictly forwards: the interpolation's lookahead frames
// stay in scratch across calls instead of being read again, so a decoder
// whose random access is a process restart is never restarted by playback.
func (inst *Sink) read(out []float32) (n int, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	channels := int(inst.format.Channels)
	outFrames := len(out) / channels
	for {
		if inst.closed {
			return 0, pulse.EndOfData
		}
		if outFrames == 0 {
			return 0, nil
		}
		if inst.state == sink.StatePlaying {
			produced := inst.produceLocked(out, outFrames)
			if produced > 0 {
				inst.primeLeft = max(inst.primeLeft-int64(produced), 0)
				return produced * channels, nil
			}
			inst.endLocked()
			continue
		}
		if inst.primeLeft > 0 {
			k := int(min(int64(outFrames), inst.primeLeft))
			clear(out[:k*channels])
			inst.primeLeft -= int64(k)
			return k * channels, nil
		}
		inst.condLocked().Wait()
	}
}

// produceLocked fills out with up to outFrames frames from the cursor on, at
// the sink's rate and volume, and returns how many it wrote; mu held. Zero
// means the source has ended or failed.
func (inst *Sink) produceLocked(out []float32, outFrames int) (produced int) {
	channels := int(inst.format.Channels)
	ctx := context.Background()
	if inst.rate == 1 && inst.frac == 0 {
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
		need := int(framesNeeded(outFrames, inst.frac, inst.rate))
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
		produced, consumed, inst.frac = resampleLinear(inst.scratch[:inst.held*channels], channels, inst.frac, inst.rate, out[:outFrames*channels])
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
func (inst *Sink) dropHeldLocked(k int) {
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

// endLocked marks the end of playback: the source ran out or failed; mu
// held. A failing source ends playback like a finished one, and the position
// readout shows where. The stream keeps running and the callback waits.
func (inst *Sink) endLocked() {
	inst.cursor = inst.frames
	inst.state = sink.StateStopped
	inst.ended = true
	inst.lastPos = inst.frames
	inst.freezeUnderflowLocked()
}
