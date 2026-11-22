package pulsesink

import (
	"time"

	"github.com/jfreymuth/pulse"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/science/audio/sink/playout"
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

// Sink plays one source through a PulseAudio playback stream. The transport,
// the resampling and the position are the engine's (package playout); what
// is here is the stream and its one start.
type Sink struct {
	eng      *playout.Engine
	channels int

	client *pulse.Client
	stream *pulse.PlaybackStream
	// bufFrames is the server's target buffer in output frames; the audible
	// position lags the delivered count by this much.
	bufFrames int64

	// The fields below are guarded by the engine's lock, so that a transport
	// call and the pull callback see them together with the engine's state.
	//
	// Nothing but the one start ever touches the stream: corking it and
	// uncorking it again makes the server send a Started event the library
	// delivers with a blocking send that nobody receives after the first
	// Start, and its read loop parks for good (jfreymuth/pulse#52); and a
	// second Start waits for a Started the server does not always send. The
	// callback waits in the engine instead, as oto's PulseAudio backend
	// does; the stream stays running and the server, starved, reports an
	// underflow.

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
	// The stream is opened at the source's rate and the server resamples,
	// so the engine's device rate is the source's.
	eng, err := playout.New(src, playout.Options{Clock: opts.Clock})
	if err != nil {
		return nil, err
	}

	clientOpts := []pulse.ClientOption{pulse.ClientApplicationName(opts.AppName)}
	if opts.Server != "" {
		clientOpts = append(clientOpts, pulse.ClientServerString(opts.Server))
	}
	client, err := pulse.NewClient(clientOpts...)
	if err != nil {
		return nil, eb.Build().Str("server", opts.Server).Errorf("unable to connect to the audio server: %w", err)
	}

	inst = &Sink{eng: eng, channels: int(format.Channels), client: client}
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
	eng.Lock()
	eng.SetLagLocked(inst.bufFrames)
	eng.Unlock()
	return inst, nil
}

// Format implements [sink.SinkI].
func (inst *Sink) Format() (format pcm.Format) { return inst.eng.Format() }

// Frames implements [sink.SinkI].
func (inst *Sink) Frames() (frames int64) { return inst.eng.Frames() }

// Play implements [sink.SinkI]: a stopped sink plays from its cursor (from 0
// when it had reached the end), a paused one resumes. It returns at once:
// the first Play starts the stream on a goroutine of its own, and every
// later one only releases the waiting callback.
func (inst *Sink) Play() {
	inst.eng.Lock()
	if inst.eng.ClosedLocked() || inst.eng.StateLocked() == sink.StatePlaying {
		inst.eng.Unlock()
		return
	}
	start := !inst.started && inst.stream != nil
	if start {
		inst.started = true
		inst.startDone = make(chan struct{})
		inst.primeLeft = primeBuffers * inst.bufFrames
	}
	inst.eng.PlayLocked()
	inst.eng.Unlock()
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

// Pause implements [sink.SinkI]: the pull callback waits from its next call
// on. The stream is not corked (see the Sink's fields), so what the server
// already holds, one latency at most, still plays.
func (inst *Sink) Pause() {
	inst.eng.Lock()
	defer inst.eng.Unlock()
	if inst.eng.PauseLocked() {
		inst.freezeUnderflowLocked()
	}
}

// freezeUnderflowLocked keeps the stream's underflow report as it is now;
// the engine's lock held. It is called before the callback is first left to
// wait.
func (inst *Sink) freezeUnderflowLocked() {
	if inst.underflowFrozen || inst.stream == nil {
		return
	}
	inst.underflowFrozen = true
	inst.underflowValue = inst.streamUnderflowLocked()
}

// streamUnderflowLocked reads the stream's underflow report; the engine's
// lock held. The library guards the report with nothing, and its Start
// clears it after handing the stream its first buffer request, so the pull
// callback — and a Pause or an Underflow on another goroutine — can run
// while Start writes it. It is therefore read only once Start has returned:
// startDone is closed after that, which orders the write before the read.
// Until then the server has not reported the stream started, and the
// report is false.
func (inst *Sink) streamUnderflowLocked() (yes bool) {
	if inst.stream == nil || inst.startDone == nil {
		return false
	}
	select {
	case <-inst.startDone:
		return inst.stream.Underflow()
	default:
		return false
	}
}

// State implements [sink.SinkI].
func (inst *Sink) State() (state sink.StateE) { return inst.eng.State() }

// Position implements [sink.SinkI].
func (inst *Sink) Position() (frame int64) { return inst.eng.Position() }

// SeekFrame implements [sink.SinkI]: moves the cursor, which the callback
// reads from on its next call. The stream is left alone, so while playing
// the server still plays what it holds of the old position, one latency at
// most, before the new one is heard.
func (inst *Sink) SeekFrame(frame int64) (err error) { return inst.eng.SeekFrame(frame) }

// Rate implements [sink.SinkI].
func (inst *Sink) Rate() (rate float64) { return inst.eng.Rate() }

// SetRate implements [sink.SinkI]: the resampling ratio from the next
// callback on; pitch follows.
func (inst *Sink) SetRate(rate float64) (err error) { return inst.eng.SetRate(rate) }

// Volume implements [sink.SinkI].
func (inst *Sink) Volume() (v float64) { return inst.eng.Volume() }

// SetVolume implements [sink.SinkI]: a software gain in [0, 1].
func (inst *Sink) SetVolume(v float64) (err error) { return inst.eng.SetVolume(v) }

// Ended implements [sink.SinkI].
func (inst *Sink) Ended() (ended bool) { return inst.eng.Ended() }

// Underflow reports whether the server ran dry while the source was too slow
// for the requested latency. A pause and the end of the source starve the
// server by design, and the library's report cannot be cleared without
// restarting the stream, so the report stays at the value it had when
// playback was first paused or first ended.
func (inst *Sink) Underflow() (yes bool) {
	inst.eng.Lock()
	defer inst.eng.Unlock()
	if inst.underflowFrozen {
		return inst.underflowValue
	}
	return inst.streamUnderflowLocked()
}

// Close implements [sink.SinkI]: closes the stream and the connection.
func (inst *Sink) Close() (err error) {
	inst.eng.Lock()
	// A waiting callback must return before the stream's close round trip
	// can be answered: the library's read loop may be waiting to hand it
	// the next buffer request. Closing the engine wakes it.
	if !inst.eng.CloseLocked() {
		inst.eng.Unlock()
		return nil
	}
	startDone := inst.startDone
	inst.eng.Unlock()
	if startDone != nil {
		// A start in flight talks to the server on this connection; give it
		// the time of a round trip before the stream goes away under it.
		select {
		case <-startDone:
		case <-time.After(startCloseWait):
		}
	}
	if inst.stream != nil {
		inst.stream.Close()
	}
	if inst.client != nil {
		inst.client.Close()
	}
	return nil
}

// read is the stream's pull callback: it fills out with interleaved frames
// and returns the number of float32 samples written. It returns only with
// frames, or with pulse.EndOfData once the sink is closed: while the sink is
// not playing it waits, so the stream never goes idle and is never started a
// second time. After the start it first delivers primeLeft frames, silence
// where the sink is not playing or the source has ended or failed, because
// the server plays nothing, and reports no start, before it holds a buffer.
func (inst *Sink) read(out []float32) (n int, err error) {
	inst.eng.Lock()
	defer inst.eng.Unlock()
	channels := inst.channels
	outFrames := len(out) / channels
	for {
		if inst.eng.ClosedLocked() {
			return 0, pulse.EndOfData
		}
		if outFrames == 0 {
			return 0, nil
		}
		if inst.eng.StateLocked() == sink.StatePlaying {
			produced := inst.eng.ProduceLocked(out, outFrames)
			if produced > 0 {
				inst.primeLeft = max(inst.primeLeft-int64(produced), 0)
				return produced * channels, nil
			}
			// The stream keeps running past the end of its source, and the
			// callback waits.
			inst.eng.EndLocked()
			inst.freezeUnderflowLocked()
			continue
		}
		if inst.primeLeft > 0 {
			k := int(min(int64(outFrames), inst.primeLeft))
			clear(out[:k*channels])
			inst.primeLeft -= int64(k)
			return k * channels, nil
		}
		inst.eng.WaitLocked()
	}
}
