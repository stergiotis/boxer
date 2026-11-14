package lanesink

import (
	"sync"
	"sync/atomic"
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
	// kept ahead by at least, and deviceLead what it is kept ahead by on top
	// of that: a device must not run dry between two reports, and it places
	// a chunk behind what it has scheduled only while some of that is left —
	// the viewer page starts a stream 30 ms ahead and wants 10 ms left. With
	// a device that reports every 20 ms the two are a floor of 80 ms under
	// any target; at 50 ms the page re-anchored a stream several times a
	// second through the headless carrier, on loopback.
	reportHeadroom = 2.5
	deviceLead     = 30 * time.Millisecond
	// quantum is the least a top-up sends; less is left for the next one.
	quantum = 5 * time.Millisecond
	// maxChunk is the most one chunk message carries.
	maxChunk = 500 * time.Millisecond
	// drainGrace is how long past the moment the device should have played
	// its last frames a stream waits for it to say so, before it ends
	// without the report.
	drainGrace = 500 * time.Millisecond
	// stallAfter is how long a playing stream with frames in flight may go
	// without a report before what is in flight is taken for lost, and
	// stallTick how often a device that is not synchronous looks. A device
	// with frames to play reports many times a second; one whose page the
	// browser has slowed still reports about once a second.
	stallAfter = 2 * time.Second
	stallTick  = 250 * time.Millisecond
)

// Options configure a [Device].
type Options struct {
	// Target is how much audio the device is kept ahead by; zero is the
	// [Target] variable's value, 150 ms unless set. It is the lane's
	// latency, and what covers the time between two of the device's reports;
	// it is never less than two and a half report intervals and 30 ms, and
	// where reports come slowly it grows with them, to four seconds at most.
	Target time.Duration
	// Sync makes a transport call top its stream up on the calling
	// goroutine. A host whose goroutines run only inside its exports — a
	// browser tab — sets it. Elsewhere a goroutine of the device starts the
	// reads, so no transport call starts one; a call made while a read is
	// under way still waits for it, as with any engine (package playout).
	Sync bool
	// Clock supplies wall time; nil is [sink.RealClock].
	Clock sink.ClockI
	// OnReport, when set, is called with every played report a stream took,
	// for a host that traces the lane. It runs with the stream locked: it
	// must not wait and must not call into the stream.
	OnReport func(r Report)
}

// Report is one played report as its stream saw it: what the remote device
// said next to what this side had assumed of it.
type Report struct {
	Stream     uint32
	Epoch      uint32
	DeviceRate uint32
	// Reported is the frames of the epoch the device says it has played,
	// and Estimated what this side took it to have played just before, from
	// the report before and the time since. Both are anchored at the
	// arrival of a report, so their difference shows how unevenly reports
	// arrive, not how long they take.
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

	// state and rate are what the remote device last announced. They are
	// written under mu and read without it, so that asking whether anybody
	// listens never waits.
	state atomic.Uint32
	rate  atomic.Uint32

	// announcing serialises announcements, each of which is held across the
	// reads its top-ups make; mu is never held across a read.
	announcing sync.Mutex

	mu      sync.Mutex
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
// frames at. It never waits.
func (inst *Device) State() (state DeviceStateE, sampleRate uint32) {
	return DeviceStateE(inst.state.Load()), inst.rate.Load()
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
	switch state, rate := inst.State(); state {
	case DeviceRunning:
		// Nothing is read here: a stream that was just opened is not playing.
		out.mu.Lock()
		out.attachLocked(rate)
		out.mu.Unlock()
	case DeviceSuspended:
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
// continues from its position. A running device at a rate no device has is
// taken for none.
func (inst *Device) announce(state DeviceStateE, rate uint32) {
	if state == DeviceRunning && (rate < MinDeviceRate || rate > MaxDeviceRate) {
		state, rate = DeviceAbsent, 0
	}
	inst.announcing.Lock()
	defer inst.announcing.Unlock()
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return
	}
	inst.state.Store(uint32(state))
	inst.rate.Store(rate)
	streams := inst.snapshotLocked()
	inst.mu.Unlock()
	if state == DeviceSuspended && len(streams) > 0 {
		inst.send(AppendWant(nil))
	}
	for _, s := range streams {
		s.mu.Lock()
		if !s.closed {
			s.detachLocked()
			if state == DeviceRunning {
				s.attachLocked(rate)
				s.fillLocked()
			}
		}
		s.mu.Unlock()
	}
}

// snapshotLocked lists the open streams; mu held.
func (inst *Device) snapshotLocked() (streams []*Sink) {
	streams = make([]*Sink, 0, len(inst.streams))
	for _, s := range inst.streams {
		streams = append(streams, s)
	}
	return streams
}

// Close ends every stream on the remote device and moves it back onto the
// device-less clock, and stops the device's goroutine. Sinks stay their
// owners' to close. Idempotent.
func (inst *Device) Close() {
	inst.announcing.Lock()
	defer inst.announcing.Unlock()
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return
	}
	inst.closed = true
	inst.state.Store(uint32(DeviceAbsent))
	inst.rate.Store(0)
	streams := inst.snapshotLocked()
	inst.mu.Unlock()
	for _, s := range streams {
		s.mu.Lock()
		if !s.closed && s.eng != nil {
			inst.send(AppendClose(nil, s.id))
			s.detachLocked()
		}
		s.mu.Unlock()
	}
	if inst.done != nil {
		close(inst.done)
	}
}

// work tops streams up off the caller's goroutine on a device that is not
// synchronous, and looks for stalled ones.
func (inst *Device) work() {
	tick := time.NewTicker(stallTick)
	defer tick.Stop()
	for {
		select {
		case <-inst.done:
			return
		case <-tick.C:
			inst.checkStalls()
			continue
		case <-inst.kick:
		}
		inst.mu.Lock()
		streams := inst.snapshotLocked()
		inst.mu.Unlock()
		for _, s := range streams {
			s.mu.Lock()
			s.fillLocked()
			s.mu.Unlock()
		}
	}
}

// checkStalls restarts every playing stream whose device has frames in
// flight and has said nothing about them for too long. A chunk's frame index
// lets the device play past a chunk it missed once a later one arrives; when
// everything in flight is lost no later one does, the device has nothing to
// report, and nothing would be sent again. The stream then starts a new
// epoch at the last frame the device confirmed, opened anew. On a carrier
// that cannot lose a message — a tab's — nothing calls this.
func (inst *Device) checkStalls() {
	inst.mu.Lock()
	streams := inst.snapshotLocked()
	inst.mu.Unlock()
	for _, s := range streams {
		s.mu.Lock()
		s.restartIfStalledLocked()
		s.mu.Unlock()
	}
}

func (inst *Device) forget(id uint32) {
	inst.mu.Lock()
	delete(inst.streams, id)
	inst.mu.Unlock()
}

// mark says which source frame the stream had delivered once a given number
// of device frames of the epoch were sent.
type mark struct {
	dev int64
	src int64
}

// Sink is one stream of a [Device]. While the remote device runs it is a
// playout engine whose frames go out as chunks; otherwise it is the
// device-less clock, so a playing sink keeps advancing with nobody
// listening (ADR-0372 §SD9, proposed). The switch carries the position, the
// state, the rate, the volume and whether playback had ended across.
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
	// when it said so — or, for a stream with nothing in flight, when the
	// first frames went out.
	devRate  uint32
	epoch    uint32
	sent     int64
	reported int64
	reportAt time.Time
	// gap is the interval between the device's reports as last seen, rising
	// at once and falling slowly; the target follows it.
	gap time.Duration
	// draining is set once the source gave no more frames: the stream stays
	// playing until the device has played what it holds, and ends then.
	draining bool
	// marks map device frames of the epoch to source frames, one per chunk
	// still in flight, behind base, the mapping at the last frame the device
	// reported. They turn a played count into a source position whatever
	// rate each chunk was made at.
	base  mark
	marks []mark

	scratch []float32
	msg     []byte
}

var _ sink.SinkI = (*Sink)(nil)

// transfer puts to into from's transport state.
func transfer(from sink.SinkI, to sink.SinkI) {
	// Position first: it settles a device-less sink, so that the state and
	// the ended flag read after it describe the same instant.
	pos := from.Position()
	state := from.State()
	ended := from.Ended()
	_ = to.SetRate(from.Rate())
	_ = to.SetVolume(from.Volume())
	switch state {
	case sink.StatePlaying:
		to.Play()
	case sink.StatePaused:
		to.Play()
		to.Pause()
	}
	if ended {
		end(to)
		return
	}
	// After the state: playing a sink that stands at its end restarts it.
	_ = to.SeekFrame(pos)
}

// end puts a stopped sink into the state playback leaves at the end of the
// source: stopped there, and ended.
func end(to sink.SinkI) {
	if eng, ok := to.(*playout.Engine); ok {
		eng.Lock()
		_ = eng.SeekFrameLocked(eng.Frames())
		eng.EndLocked()
		eng.Unlock()
		return
	}
	// A device-less sink ends by running into its end: left playing there,
	// it does on the next look.
	to.Play()
	_ = to.SeekFrame(to.Frames())
	_ = to.Position()
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
	inst.gap = 0
	inst.newEpochLocked()
	inst.msg = AppendOpen(inst.msg[:0], inst.id, inst.epoch, rate, uint8(inst.channels))
	inst.dev.send(inst.msg)
}

// newEpochLocked starts the stream's flow control over at the engine's
// position: nothing sent, nothing in flight; mu held, attached.
func (inst *Sink) newEpochLocked() {
	inst.epoch++
	inst.sent, inst.reported = 0, 0
	inst.reportAt = inst.dev.clock.Now()
	inst.draining = false
	inst.marks = inst.marks[:0]
	inst.eng.Lock()
	inst.base = mark{dev: 0, src: inst.eng.DeliveredEndLocked()}
	inst.eng.SetLagSourceLocked(0)
	inst.eng.Unlock()
}

// detachLocked moves the transport back onto the device-less clock; mu held.
// Nothing is sent: the remote device is gone, or has voided the stream.
func (inst *Sink) detachLocked() {
	if inst.eng == nil || inst.closed {
		return
	}
	null := sink.NewNull(inst.src, inst.dev.clock)
	transfer(inst.eng, null)
	_ = inst.eng.Close()
	inst.cur, inst.eng = null, nil
	inst.draining = false
}

// played takes the device's report for an epoch.
func (inst *Sink) played(epoch uint32, frames int64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.eng == nil || inst.closed || epoch != inst.epoch {
		return
	}
	now := inst.dev.clock.Now()
	estimated := inst.playedNowLocked(now)
	// The interval counts only while the device had frames to play: one that
	// ran dry has nothing to report, and its silence is not a slow report.
	// reportAt is then the report before, or the moment a stream with
	// nothing in flight was first sent frames.
	if inst.sent > inst.reported {
		if gap := now.Sub(inst.reportAt); gap > inst.gap {
			inst.gap = gap
		} else {
			inst.gap -= (inst.gap - gap) / 4
		}
	}
	inst.reported = max(inst.reported, min(frames, inst.sent))
	inst.reportAt = now
	for len(inst.marks) > 0 && inst.marks[0].dev <= inst.reported {
		inst.base = inst.marks[0]
		inst.marks = inst.marks[1:]
	}
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

// sourceAtLocked is the source frame the stream had delivered when dev
// device frames of the epoch were sent, read off the marks.
func (inst *Sink) sourceAtLocked(dev int64) (src int64) {
	prev := inst.base
	if dev <= prev.dev {
		return prev.src
	}
	for _, m := range inst.marks {
		if dev <= m.dev {
			return prev.src + (dev-prev.dev)*(m.src-prev.src)/(m.dev-prev.dev)
		}
		prev = m
	}
	return prev.src
}

// targetFramesLocked is how many device frames the stream is kept ahead by.
func (inst *Sink) targetFramesLocked() (frames int64) {
	target := max(inst.dev.target, time.Duration(float64(inst.gap)*reportHeadroom)+deviceLead)
	target = min(target, maxTarget)
	return int64(target.Seconds() * float64(inst.devRate))
}

// fillLocked tops the stream up to its target from what the device reported,
// and ends it once a source that gave no more frames has been played out;
// mu held. It reads the source.
func (inst *Sink) fillLocked() {
	eng := inst.eng
	if eng == nil || inst.closed {
		return
	}
	devRate := float64(inst.devRate)
	least := max(int64(quantum.Seconds()*devRate), 1)
	most := max(int64(maxChunk.Seconds()*devRate), least)
	eng.Lock()
	defer eng.Unlock()
	for eng.StateLocked() == sink.StatePlaying && !inst.draining {
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
			// The source has ended or failed. The stream ends when the
			// device has played what it holds, not now.
			inst.draining = true
			break
		}
		if inst.sent == inst.reported {
			// Nothing was in flight: the device starts on these frames now,
			// and the time it sat idle is not time it took to report.
			inst.reportAt = inst.dev.clock.Now()
		}
		first := inst.sent
		inst.sent += int64(n)
		inst.marks = append(inst.marks, mark{dev: inst.sent, src: eng.DeliveredEndLocked()})
		inst.msg = AppendChunk(inst.msg[:0], inst.id, inst.epoch, uint64(first), buf[:n*inst.channels])
		inst.dev.send(inst.msg)
	}
	inst.settleEngineLocked()
}

// settleEngineLocked ends a drained stream and restates to the engine which
// source frames the device holds unplayed; mu and the engine's lock held.
func (inst *Sink) settleEngineLocked() {
	eng := inst.eng
	now := inst.dev.clock.Now()
	playedNow := inst.playedNowLocked(now)
	if inst.draining && eng.StateLocked() == sink.StatePlaying {
		// Ended when the device said it played the last frame, or, should
		// that report never come, a while after it must have.
		due := inst.reportAt.Add(time.Duration(float64(inst.sent-inst.reported) / float64(inst.devRate) * float64(time.Second)))
		if inst.reported >= inst.sent || now.Sub(due) > drainGrace {
			eng.EndLocked()
			inst.draining = false
			return
		}
	}
	eng.SetLagSourceLocked(eng.DeliveredEndLocked() - inst.sourceAtLocked(playedNow))
}

// settleLocked is what a getter does first on an attached stream: a drained
// one ends even when no report and no top-up comes; mu held.
func (inst *Sink) settleLocked() {
	if inst.eng == nil || !inst.draining {
		return
	}
	inst.eng.Lock()
	inst.settleEngineLocked()
	inst.eng.Unlock()
}

// restartIfStalledLocked starts a new epoch at the last confirmed frame when
// the device has been silent about frames in flight for too long; mu held.
func (inst *Sink) restartIfStalledLocked() {
	if inst.eng == nil || inst.closed || inst.sent <= inst.reported {
		return
	}
	if inst.eng.State() != sink.StatePlaying {
		return
	}
	limit := max(stallAfter, 3*inst.gap)
	if inst.dev.clock.Now().Sub(inst.reportAt) < limit {
		return
	}
	// Opened again rather than flushed: the open itself may be what was
	// lost, and a device that knows the stream replaces it.
	if inst.cur.SeekFrame(inst.sourceAtLocked(inst.reported)) != nil {
		return
	}
	inst.newEpochLocked()
	inst.msg = AppendOpen(inst.msg[:0], inst.id, inst.epoch, inst.devRate, uint8(inst.channels))
	inst.dev.send(inst.msg)
	inst.fillLocked()
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
// when none does. It never waits.
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

// Play implements [sink.SinkI]. On a remote device, playing a stream that
// stands at its end starts a new epoch, so the device drops what it may
// still hold of the old one.
func (inst *Sink) Play() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return
	}
	inst.settleLocked()
	if inst.eng != nil && inst.cur.State() == sink.StateStopped && inst.cur.Position() >= inst.Frames() {
		_ = inst.seekLocked(0)
	}
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
	inst.settleLocked()
	return inst.cur.State()
}

// Position implements [sink.SinkI].
func (inst *Sink) Position() (frame int64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.settleLocked()
	return inst.cur.Position()
}

// SeekFrame implements [sink.SinkI]: on a remote device it also drops what
// the device holds, so the new position is heard at once.
func (inst *Sink) SeekFrame(frame int64) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	err = inst.seekLocked(frame)
	if err != nil || inst.eng == nil {
		return err
	}
	// On a synchronous device the top-up is left to the device's next
	// report, which a flush brings within one report interval: a seek is
	// what a person drags, and each would otherwise read a target's worth
	// on the caller.
	if !inst.dev.sync {
		inst.kickLocked()
	}
	return nil
}

// seekLocked moves the transport and, attached, starts a new epoch on the
// remote device; mu held.
func (inst *Sink) seekLocked(frame int64) (err error) {
	err = inst.cur.SeekFrame(frame)
	if err != nil || inst.eng == nil {
		return err
	}
	inst.newEpochLocked()
	inst.msg = AppendFlush(inst.msg[:0], inst.id, inst.epoch)
	inst.dev.send(inst.msg)
	return nil
}

// Rate implements [sink.SinkI].
func (inst *Sink) Rate() (rate float64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.cur.Rate()
}

// SetRate implements [sink.SinkI]. Frames already sent play at the rate they
// were made at, so the change is heard one target late.
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

// SetVolume implements [sink.SinkI]: a gain on the frames sent from now on,
// so the change is heard one target late.
func (inst *Sink) SetVolume(v float64) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.cur.SetVolume(v)
}

// Ended implements [sink.SinkI]. On a remote device playback has ended once
// the device played the source's last frames, not once they were sent.
func (inst *Sink) Ended() (ended bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.settleLocked()
	return inst.cur.Ended()
}

// Close implements [sink.SinkI]: it ends the stream on the remote device,
// which stops what it holds of it.
func (inst *Sink) Close() (err error) {
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return nil
	}
	if inst.eng != nil {
		inst.msg = AppendClose(inst.msg[:0], inst.id)
		inst.dev.send(inst.msg)
		// Back onto the clock first: a closed sink holds the position that
		// was audible, and a closed engine reads as what it had sent.
		inst.detachLocked()
	}
	inst.closed = true
	err = inst.cur.Close()
	inst.mu.Unlock()
	inst.dev.forget(inst.id)
	return err
}
