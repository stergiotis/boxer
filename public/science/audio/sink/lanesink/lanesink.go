package lanesink

import (
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/science/audio/sink/playout"
)

const (
	// defaultTarget is how much audio the device is kept ahead by when
	// neither the options nor [Target] name a usable figure.
	defaultTarget = 150 * time.Millisecond
	// maxTarget bounds the target when it grows with slow reports.
	maxTarget = 4 * time.Second
	// reportHeadroom is how many report intervals of audio the device is
	// kept ahead by: it must not run dry between two reports.
	reportHeadroom = 2.5
	// quantum is the least a top-up sends; less is left for the next one.
	quantum = 5 * time.Millisecond
	// maxChunk is the most one chunk message carries.
	maxChunk = 500 * time.Millisecond
)

// Options configure a [Device].
type Options struct {
	// Target is how much audio the device is kept ahead by; zero is the
	// [Target] variable's value, 150 ms unless set. It is the lane's
	// latency, and what covers the time between two of the device's reports;
	// where reports come slower than it allows for, the target grows with
	// them.
	Target time.Duration
	// OnReport, when set, is called with every played report a stream took,
	// for a host that traces the lane. It runs with the stream locked: it
	// must not wait and must not call into the stream.
	OnReport func(r Report)
	// Sync makes a transport call top its stream up on the calling
	// goroutine. A host whose goroutines run only inside its exports — a
	// browser tab — sets it. Elsewhere a goroutine of the device does the
	// reads, so no transport call waits on a source.
	Sync bool
	// Clock supplies wall time; nil is [sink.RealClock].
	Clock sink.ClockI
}

// Report is one played report as its stream saw it: what the remote device
// said next to what this side had assumed of it.
type Report struct {
	Stream     uint32
	Epoch      uint32
	DeviceRate uint32
	// Reported is the frames of the epoch the device says it has played,
	// and Estimated what this side took it to have played just before, from
	// the report before and the time since. Their difference is how far the
	// position readout was from the device, in device frames.
	Reported  int64
	Estimated int64
	// Sent is the frames of the epoch sent so far, and Target how many the
	// stream is kept ahead by.
	Sent   int64
	Target int64
	// Playing reports whether the stream was playing. A playing stream whose
	// Reported has reached Sent has run dry.
	Playing bool
}

// Device is the Go end of the audio lane: it opens sinks, and keeps each
// playing one a target ahead of what the remote device reports as played.
type Device struct {
	send     func(msg []byte)
	target   time.Duration
	sync     bool
	clock    sink.ClockI
	onReport func(r Report)

	mu      sync.Mutex
	state   DeviceStateE
	rate    uint32
	streams map[uint32]*Sink
	nextID  uint32
	closed  bool
	// kick wakes the worker of a device that is not synchronous.
	kick chan struct{}
	done chan struct{}
}

// NewDevice returns a device with no remote end yet. send carries one lane
// message, without its wire prefix, toward the remote device; it is called
// from any goroutine, must not wait, and must not keep msg.
func NewDevice(send func(msg []byte), opts Options) (inst *Device) {
	if opts.Target <= 0 {
		opts.Target = Target.Get()
	}
	if opts.Target <= 0 {
		opts.Target = defaultTarget
	}
	if opts.Clock == nil {
		opts.Clock = sink.RealClock{}
	}
	inst = &Device{
		send:     send,
		target:   opts.Target,
		sync:     opts.Sync,
		clock:    opts.Clock,
		onReport: opts.OnReport,
		streams:  make(map[uint32]*Sink),
		nextID:   1,
	}
	if !opts.Sync {
		inst.kick = make(chan struct{}, 1)
		inst.done = make(chan struct{})
		go inst.work()
	}
	return inst
}

// Name names the device kind.
func (inst *Device) Name() (name string) { return "lane" }

// State is what the remote device last announced, and the rate it takes
// frames at.
func (inst *Device) State() (state DeviceStateE, sampleRate uint32) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.state, inst.rate
}

// OpenSink opens a stream over src. It plays through the remote device
// while one runs and on the device-less clock otherwise. label is unused: the
// remote device shows no stream names. The sink does not own src.
func (inst *Device) OpenSink(src pcm.SourceI, label string) (s sink.SinkI, err error) {
	if src == nil {
		return nil, eh.New("nil source")
	}
	format := src.Format()
	if err = format.Validate(); err != nil {
		return nil, err
	}
	if format.Channels > 2 {
		return nil, eb.Build().Uint16("channels", format.Channels).Errorf("only mono and stereo sources are played")
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return nil, eh.New("the audio lane is closed")
	}
	out := &Sink{dev: inst, id: inst.nextID, src: src, channels: int(format.Channels)}
	out.cur = sink.NewNull(src, inst.clock)
	inst.nextID++
	inst.streams[out.id] = out
	if inst.state == DeviceRunning {
		out.mu.Lock()
		out.attachLocked(inst.rate)
		out.mu.Unlock()
	} else if inst.state == DeviceSuspended {
		inst.send(AppendWant(nil))
	}
	return out, nil
}

// Receive takes one lane message from the remote device, without its wire
// prefix. It is called off the render path: a played report tops its stream
// up, which reads the stream's source.
func (inst *Device) Receive(msg []byte) (err error) {
	m, err := Decode(msg)
	if err != nil {
		return err
	}
	switch m.Kind {
	case KindDevice:
		inst.announce(m.State, m.SampleRate)
	case KindPlayed:
		inst.mu.Lock()
		s := inst.streams[m.Stream]
		inst.mu.Unlock()
		if s != nil {
			s.played(m.Epoch, int64(min(m.Frames, 1<<62)))
		}
	default:
		return eb.Build().Uint8("kind", uint8(m.Kind)).Errorf("a lane message a device does not send")
	}
	return nil
}

// announce takes a device announcement: every stream of an earlier one is
// void, and with a running device every open stream is opened on it and
// continues from its position.
func (inst *Device) announce(state DeviceStateE, rate uint32) {
	if state == DeviceRunning && rate == 0 {
		state = DeviceAbsent
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return
	}
	inst.state, inst.rate = state, rate
	if state == DeviceSuspended && len(inst.streams) > 0 {
		inst.send(AppendWant(nil))
	}
	for _, s := range inst.streams {
		s.mu.Lock()
		s.detachLocked()
		if state == DeviceRunning {
			s.attachLocked(rate)
			s.fillLocked()
		}
		s.mu.Unlock()
	}
}

// Close detaches every stream, which then free-run, and stops the device's
// goroutine. Sinks stay their owners' to close. Idempotent.
func (inst *Device) Close() {
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return
	}
	inst.closed = true
	inst.state = DeviceAbsent
	for _, s := range inst.streams {
		s.mu.Lock()
		s.detachLocked()
		s.mu.Unlock()
	}
	inst.mu.Unlock()
	if inst.done != nil {
		close(inst.done)
	}
}

// work tops streams up off the caller's goroutine on a device that is not
// synchronous.
func (inst *Device) work() {
	for {
		select {
		case <-inst.done:
			return
		case <-inst.kick:
		}
		inst.mu.Lock()
		streams := make([]*Sink, 0, len(inst.streams))
		for _, s := range inst.streams {
			streams = append(streams, s)
		}
		inst.mu.Unlock()
		for _, s := range streams {
			s.mu.Lock()
			s.fillLocked()
			s.mu.Unlock()
		}
	}
}

func (inst *Device) forget(id uint32) {
	inst.mu.Lock()
	delete(inst.streams, id)
	inst.mu.Unlock()
}

// Sink is one stream of a [Device]. While the remote device runs it is a
// playout engine whose frames go out as chunks; otherwise it is the
// device-less clock, so a playing sink keeps advancing with nobody
// listening (ADR-0372 §SD9, proposed). The switch carries the position, the
// state, the rate and the volume across.
type Sink struct {
	dev      *Device
	id       uint32
	src      pcm.SourceI
	channels int

	mu sync.Mutex
	// cur is the transport in force: eng while attached, a sink.Null
	// otherwise.
	cur    sink.SinkI
	eng    *playout.Engine
	closed bool

	// The flow control of the attached stream, in device frames of the
	// current epoch: what was sent, what the device reported as played, and
	// when it said so.
	devRate  uint32
	epoch    uint32
	sent     int64
	reported int64
	reportAt time.Time
	// gap is the interval between the device's reports as last seen, rising
	// at once and falling slowly; the target follows it.
	gap time.Duration

	scratch []float32
	msg     []byte
}

var _ sink.SinkI = (*Sink)(nil)

// transfer puts to into from's transport state.
func transfer(from sink.SinkI, to sink.SinkI) {
	_ = to.SetRate(from.Rate())
	_ = to.SetVolume(from.Volume())
	_ = to.SeekFrame(from.Position())
	switch from.State() {
	case sink.StatePlaying:
		to.Play()
	case sink.StatePaused:
		to.Play()
		to.Pause()
	}
}

// attachLocked opens the stream on the remote device at its rate and moves
// the transport onto an engine; mu held. A source the engine refuses stays
// on the device-less clock.
func (inst *Sink) attachLocked(rate uint32) {
	if inst.closed || inst.eng != nil {
		return
	}
	eng, err := playout.New(inst.src, playout.Options{Clock: inst.dev.clock, DeviceRate: rate})
	if err != nil {
		return
	}
	old := inst.cur
	transfer(old, eng)
	_ = old.Close()
	inst.cur, inst.eng = eng, eng
	inst.devRate = rate
	inst.epoch++
	inst.sent, inst.reported, inst.gap = 0, 0, 0
	inst.reportAt = inst.dev.clock.Now()
	inst.msg = AppendOpen(inst.msg[:0], inst.id, inst.epoch, rate, uint8(inst.channels))
	inst.dev.send(inst.msg)
}

// detachLocked moves the transport back onto the device-less clock; mu held.
// Nothing is sent: the remote device is gone, or has voided the stream.
func (inst *Sink) detachLocked() {
	if inst.eng == nil {
		return
	}
	null := sink.NewNull(inst.src, inst.dev.clock)
	if !inst.closed {
		transfer(inst.eng, null)
	}
	_ = inst.eng.Close()
	inst.cur, inst.eng = null, nil
	if inst.closed {
		_ = null.Close()
	}
}

// played takes the device's report for an epoch.
func (inst *Sink) played(epoch uint32, frames int64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.eng == nil || epoch != inst.epoch {
		return
	}
	now := inst.dev.clock.Now()
	estimated := inst.playedNowLocked(now)
	// The interval counts only while the device had frames to play: one that
	// ran dry has nothing to report, and its silence is not a slow report.
	if inst.sent > inst.reported {
		if gap := now.Sub(inst.reportAt); gap > inst.gap {
			inst.gap = gap
		} else {
			inst.gap -= (inst.gap - gap) / 4
		}
	}
	inst.reported = max(inst.reported, min(frames, inst.sent))
	inst.reportAt = now
	if inst.dev.onReport != nil {
		inst.dev.onReport(Report{
			Stream: inst.id, Epoch: inst.epoch, DeviceRate: inst.devRate,
			Reported: inst.reported, Estimated: estimated, Sent: inst.sent,
			Target:  inst.targetFramesLocked(),
			Playing: inst.eng.State() == sink.StatePlaying,
		})
	}
	inst.fillLocked()
}

// playedNowLocked estimates how many frames the device has played by now:
// what it reported, advanced by the time since, never past what was sent.
func (inst *Sink) playedNowLocked(now time.Time) (frames int64) {
	frames = inst.reported
	if elapsed := now.Sub(inst.reportAt); elapsed > 0 && inst.sent > inst.reported {
		frames += int64(elapsed.Seconds() * float64(inst.devRate))
	}
	return min(frames, inst.sent)
}

// targetFramesLocked is how many device frames the stream is kept ahead by.
func (inst *Sink) targetFramesLocked() (frames int64) {
	target := max(inst.dev.target, time.Duration(float64(inst.gap)*reportHeadroom))
	target = min(target, maxTarget)
	return int64(target.Seconds() * float64(inst.devRate))
}

// fillLocked tops the stream up to its target from what the device reported;
// mu held. It reads the source.
func (inst *Sink) fillLocked() {
	eng := inst.eng
	if eng == nil || inst.closed {
		return
	}
	devRate := float64(inst.devRate)
	least := int64(quantum.Seconds() * devRate)
	most := int64(maxChunk.Seconds() * devRate)
	eng.Lock()
	defer eng.Unlock()
	for eng.StateLocked() == sink.StatePlaying {
		// Counted against the report, not against an estimate of now: what
		// the estimate adds would be sent twice once the report catches up.
		want := inst.targetFramesLocked() - (inst.sent - inst.reported)
		if want < least {
			break
		}
		want = min(want, most)
		need := int(want) * inst.channels
		if cap(inst.scratch) < need {
			inst.scratch = make([]float32, need)
		}
		buf := inst.scratch[:need]
		n := eng.ProduceLocked(buf, int(want))
		if n == 0 {
			eng.EndLocked()
			break
		}
		inst.sent += int64(n)
		inst.msg = AppendChunk(inst.msg[:0], inst.id, inst.epoch, buf[:n*inst.channels])
		inst.dev.send(inst.msg)
	}
	eng.SetLagLocked(inst.sent - inst.playedNowLocked(inst.dev.clock.Now()))
}

// kickLocked tops the stream up after a transport call: here on a
// synchronous device, on the device's goroutine otherwise; mu held.
func (inst *Sink) kickLocked() {
	if inst.eng == nil {
		return
	}
	if inst.dev.sync {
		inst.fillLocked()
		return
	}
	select {
	case inst.dev.kick <- struct{}{}:
	default:
	}
}

// Audible reports whether a remote device plays the sink now, and why not
// when none does.
func (inst *Sink) Audible() (yes bool, reason string) {
	state, _ := inst.dev.State()
	switch state {
	case DeviceRunning:
		return true, ""
	case DeviceSuspended:
		return false, "the browser has not enabled sound yet: click or press a key in the page"
	}
	return false, "no viewer is listening"
}

// Format implements [sink.SinkI].
func (inst *Sink) Format() (format pcm.Format) { return inst.src.Format() }

// Frames implements [sink.SinkI].
func (inst *Sink) Frames() (frames int64) { return max(inst.src.Frames(), 0) }

// Play implements [sink.SinkI].
func (inst *Sink) Play() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.cur.Play()
	inst.kickLocked()
}

// Pause implements [sink.SinkI]. What the remote device already holds, one
// target at most, still plays.
func (inst *Sink) Pause() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.cur.Pause()
}

// State implements [sink.SinkI].
func (inst *Sink) State() (state sink.StateE) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.cur.State()
}

// Position implements [sink.SinkI].
func (inst *Sink) Position() (frame int64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.cur.Position()
}

// SeekFrame implements [sink.SinkI]: on a remote device it also drops what
// the device holds, so the new position is heard at once.
func (inst *Sink) SeekFrame(frame int64) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	err = inst.cur.SeekFrame(frame)
	if err != nil || inst.eng == nil {
		return err
	}
	inst.epoch++
	inst.sent, inst.reported = 0, 0
	inst.reportAt = inst.dev.clock.Now()
	inst.msg = AppendFlush(inst.msg[:0], inst.id, inst.epoch)
	inst.dev.send(inst.msg)
	inst.eng.Lock()
	inst.eng.SetLagLocked(0)
	inst.eng.Unlock()
	inst.kickLocked()
	return nil
}

// Rate implements [sink.SinkI].
func (inst *Sink) Rate() (rate float64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.cur.Rate()
}

// SetRate implements [sink.SinkI]. Frames already sent play at the rate they
// were made at.
func (inst *Sink) SetRate(rate float64) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.cur.SetRate(rate)
}

// Volume implements [sink.SinkI].
func (inst *Sink) Volume() (v float64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.cur.Volume()
}

// SetVolume implements [sink.SinkI]: a gain on the frames sent from now on.
func (inst *Sink) SetVolume(v float64) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.cur.SetVolume(v)
}

// Ended implements [sink.SinkI].
func (inst *Sink) Ended() (ended bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.cur.Ended()
}

// Close implements [sink.SinkI]: it ends the stream on the remote device.
func (inst *Sink) Close() (err error) {
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return nil
	}
	inst.closed = true
	attached := inst.eng != nil
	err = inst.cur.Close()
	if attached {
		inst.msg = AppendClose(inst.msg[:0], inst.id)
		inst.dev.send(inst.msg)
	}
	inst.mu.Unlock()
	inst.dev.forget(inst.id)
	return err
}
