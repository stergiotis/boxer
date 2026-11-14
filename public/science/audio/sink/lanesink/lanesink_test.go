package lanesink

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
)

// remote is the far end of the lane as a test plays it: it takes the
// messages the device sends, plays what it holds at its rate when the clock
// moves, and reports. It never calls into the device from send.
type remote struct {
	t     *testing.T
	dev   *Device
	clock *sink.ManualClock
	rate  uint32

	streams map[uint32]*remoteStream
	opens   []Message
	flushes []Message
	closes  []uint32
	chunks  int
	wants   int
	// underruns counts the advances in which a stream with a chunk behind it
	// had less to play than the time asked for.
	underruns int
}

type remoteStream struct {
	epoch    uint32
	channels int
	received int64
	played   int64
	samples  []int16
}

func newRemote(t *testing.T, opts Options) (r *remote) {
	r = &remote{t: t, clock: sink.NewManualClock(time.Unix(1000, 0)), streams: map[uint32]*remoteStream{}}
	opts.Clock = r.clock
	r.dev = NewDevice(r.send, opts)
	t.Cleanup(r.dev.Close)
	return r
}

func (r *remote) send(msg []byte) {
	m, err := Decode(msg)
	require.NoError(r.t, err)
	switch m.Kind {
	case KindOpen:
		require.Equal(r.t, SampleFormatS16LE, m.Format)
		r.opens = append(r.opens, m)
		r.streams[m.Stream] = &remoteStream{epoch: m.Epoch, channels: int(m.Channels)}
	case KindChunk:
		s := r.streams[m.Stream]
		require.NotNil(r.t, s, "a chunk for a stream that was not opened")
		if m.Epoch != s.epoch {
			return
		}
		r.chunks++
		require.Zero(r.t, len(m.Samples)%(2*s.channels), "a chunk holds whole frames")
		for i := 0; i+1 < len(m.Samples); i += 2 {
			s.samples = append(s.samples, int16(binary.LittleEndian.Uint16(m.Samples[i:])))
		}
		s.received += int64(len(m.Samples) / (2 * s.channels))
	case KindFlush:
		r.flushes = append(r.flushes, m)
		s := r.streams[m.Stream]
		require.NotNil(r.t, s)
		s.epoch, s.received, s.played, s.samples = m.Epoch, 0, 0, nil
	case KindClose:
		r.closes = append(r.closes, m.Stream)
		delete(r.streams, m.Stream)
	case KindWant:
		r.wants++
	default:
		r.t.Fatalf("the device sent a message of kind %#x", m.Kind)
	}
}

// announce is the remote device (re)announcing itself; its streams are void.
func (r *remote) announce(state DeviceStateE, rate uint32) {
	r.rate = rate
	r.streams = map[uint32]*remoteStream{}
	require.NoError(r.t, r.dev.Receive(AppendDevice(nil, state, rate)))
}

// advance moves the clock by d, plays that much of every stream, and reports.
func (r *remote) advance(d time.Duration) {
	r.clock.Advance(d)
	frames := int64(d.Seconds() * float64(r.rate))
	for id, s := range r.streams {
		avail := s.received - s.played
		if avail < frames && s.received > 0 {
			r.underruns++
		}
		s.played += min(avail, frames)
		require.NoError(r.t, r.dev.Receive(AppendPlayed(nil, id, s.epoch, uint64(s.played))))
	}
}

func (r *remote) only() (s *remoteStream) {
	require.Len(r.t, r.streams, 1)
	for _, v := range r.streams {
		s = v
	}
	return s
}

// ramp is a source whose every sample survives the trip through 16 bits, so
// what arrives can be compared with what was read.
func ramp(t *testing.T, format pcm.Format, frames int64) (src pcm.SourceI) {
	src, err := pcm.NewSynthSource(format, frames, func(frame int64, ch int) float32 {
		return float32(frame%2000+int64(ch)*3) / 32767
	})
	require.NoError(t, err)
	return src
}

func rampAt(frame int64, ch int) int16 { return int16(frame%2000 + int64(ch)*3) }

const testRate = 48000

func open(t *testing.T, r *remote, src pcm.SourceI) (s *Sink) {
	out, err := r.dev.OpenSink(src, "test")
	require.NoError(t, err)
	s = out.(*Sink)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAStreamFreeRunsWithNoDevice(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 2}, 10*testRate))
	yes, reason := s.Audible()
	require.False(t, yes)
	require.Contains(t, reason, "no viewer")
	require.Zero(t, r.wants, "with no device there is nobody to tell")

	s.Play()
	r.clock.Advance(time.Second)
	require.Equal(t, sink.StatePlaying, s.State())
	require.Equal(t, int64(testRate), s.Position())
	require.Empty(t, r.opens, "nothing is sent toward a device that has not announced itself")
	require.Zero(t, r.chunks)

	// A suspended device is announced but does not play; the stream stays
	// on the clock and says why.
	r.announce(DeviceSuspended, testRate)
	require.Empty(t, r.opens)
	require.Equal(t, 1, r.wants, "a suspended device is told that a stream waits for it")
	other := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, testRate))
	require.Equal(t, 2, r.wants, "and again when another stream is opened while it is suspended")
	require.NoError(t, other.Close())
	yes, reason = s.Audible()
	require.False(t, yes)
	require.Contains(t, reason, "click")
}

func TestAnAnnouncementMovesTheStreamOntoTheDeviceAtItsPosition(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	format := pcm.Format{SampleRate: testRate, Channels: 2}
	s := open(t, r, ramp(t, format, 10*testRate))
	require.NoError(t, s.SetVolume(1))
	s.Play()
	r.clock.Advance(time.Second)

	r.announce(DeviceRunning, testRate)
	require.Len(t, r.opens, 1)
	require.Equal(t, uint32(testRate), r.opens[0].SampleRate)
	require.Equal(t, uint8(2), r.opens[0].Channels)
	yes, _ := s.Audible()
	require.True(t, yes)
	require.Equal(t, sink.StatePlaying, s.State())

	rs := r.only()
	target := int64(defaultTarget.Seconds() * testRate)
	require.Equal(t, target, rs.received, "the first top-up is one target")
	require.Equal(t, rampAt(testRate, 0), rs.samples[0], "the stream continues from where the clock had it")
	require.Equal(t, rampAt(testRate, 1), rs.samples[1])
	require.InDelta(t, testRate, s.Position(), 2, "the position does not jump at the switch")
}

// What is in flight never exceeds the target, the remote device never runs
// dry at a report interval the target covers, and the frames arrive in order
// and unchanged.
func TestTheDeviceIsKeptOneTargetAhead(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	format := pcm.Format{SampleRate: testRate, Channels: 2}
	s := open(t, r, ramp(t, format, 30*testRate))
	r.announce(DeviceRunning, testRate)
	s.Play()
	rs := r.only()
	target := int64(defaultTarget.Seconds() * testRate)
	least := int64(quantum.Seconds() * testRate)

	for range 500 {
		r.advance(20 * time.Millisecond)
		inFlight := rs.received - rs.played
		require.LessOrEqual(t, inFlight, target)
		require.Greater(t, inFlight, target-least-1, "each report is answered with a top-up")
	}
	require.Zero(t, r.underruns)
	for i := 0; i < len(rs.samples); i += 2 {
		frame := int64(i / 2)
		if rs.samples[i] != rampAt(frame, 0) || rs.samples[i+1] != rampAt(frame, 1) {
			t.Fatalf("frame %d arrived as (%d, %d)", frame, rs.samples[i], rs.samples[i+1])
		}
	}
	// 10 s played: the position is what the remote device played, within
	// the interpolation of one report.
	require.InDelta(t, rs.played, s.Position(), 0.03*testRate)
	require.LessOrEqual(t, s.Position(), rs.received)
}

func TestASeekFlushesTheDevice(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, 30*testRate))
	r.announce(DeviceRunning, testRate)
	s.Play()
	r.advance(20 * time.Millisecond)
	before := r.opens[0].Epoch

	require.NoError(t, s.SeekFrame(5*testRate))
	require.Len(t, r.flushes, 1)
	require.Greater(t, r.flushes[0].Epoch, before)
	rs := r.only()
	require.Equal(t, r.flushes[0].Epoch, rs.epoch)
	require.Equal(t, rampAt(5*testRate, 0), rs.samples[0], "the chunks after the flush are from the new position")
	require.Equal(t, int64(5*testRate), s.Position())

	// A report still on its way for the old epoch changes nothing.
	sentBefore := rs.received
	require.NoError(t, r.dev.Receive(AppendPlayed(nil, s.id, before, 1<<40)))
	require.Equal(t, sentBefore, rs.received)
	require.Equal(t, int64(5*testRate), s.Position())
}

func TestADeviceThatGoesAwayLeavesTheStreamOnTheClock(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 2}, 60*testRate))
	r.announce(DeviceRunning, testRate)
	s.Play()
	for range 50 {
		r.advance(20 * time.Millisecond)
	}
	pos := s.Position()
	require.InDelta(t, testRate, pos, 0.05*testRate)

	r.announce(DeviceAbsent, 0)
	chunks := r.chunks
	require.Equal(t, sink.StatePlaying, s.State())
	r.clock.Advance(2 * time.Second)
	require.InDelta(t, pos+2*testRate, s.Position(), 4)
	require.Equal(t, chunks, r.chunks, "nothing is sent with nobody listening")

	// Another device, at another rate, takes the stream over where it is.
	r.announce(DeviceRunning, 44100)
	require.Len(t, r.opens, 2)
	require.Equal(t, uint32(44100), r.opens[1].SampleRate)
	require.Greater(t, r.opens[1].Epoch, r.opens[0].Epoch)
	require.Equal(t, int64(defaultTarget.Seconds()*44100), r.only().received)
	require.InDelta(t, pos+2*testRate, s.Position(), 4)
}

// Reports that come slower than the target allows for make the target grow
// until the remote device no longer runs dry between two of them.
func TestSlowReportsRaiseTheTarget(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, 600*testRate))
	r.announce(DeviceRunning, testRate)
	s.Play()
	for range 4 {
		r.advance(time.Second)
	}
	r.underruns = 0
	for range 20 {
		r.advance(time.Second)
	}
	require.Zero(t, r.underruns, "after the first slow reports the device holds enough for the next")
	rs := r.only()
	require.LessOrEqual(t, rs.received-rs.played, int64(maxTarget.Seconds()*testRate))

	// Back at a fast report the target comes down again.
	for range 200 {
		r.advance(20 * time.Millisecond)
	}
	require.Less(t, rs.received-rs.played, int64(testRate/2))
}

func TestPauseStopsTheTopUpsAndTheEndIsReached(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, testRate/2))
	r.announce(DeviceRunning, testRate)
	s.Play()
	r.advance(20 * time.Millisecond)
	s.Pause()
	chunks := r.chunks
	for range 10 {
		r.advance(20 * time.Millisecond)
	}
	require.Equal(t, chunks, r.chunks, "a paused stream is not topped up")
	require.Equal(t, sink.StatePaused, s.State())

	s.Play()
	for range 40 {
		r.advance(20 * time.Millisecond)
	}
	require.True(t, s.Ended())
	require.Equal(t, sink.StateStopped, s.State())
	require.Equal(t, int64(testRate/2), s.Position())
	require.Equal(t, int64(testRate/2), r.only().received, "every frame of the source was sent, once")

	// Play at the end starts over, on the same stream.
	s.Play()
	require.Equal(t, sink.StatePlaying, s.State())
	require.Less(t, s.Position(), int64(testRate/4))
}

func TestAStreamIsResampledToTheDeviceRate(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	src, err := pcm.NewSynthSource(pcm.Format{SampleRate: 24000, Channels: 1}, 24000*20, pcm.Sine(pcm.Format{SampleRate: 24000, Channels: 1}, 440, 0.5))
	require.NoError(t, err)
	s := open(t, r, src)
	r.announce(DeviceRunning, testRate)
	s.Play()
	for range 100 {
		r.advance(20 * time.Millisecond)
	}
	rs := r.only()
	require.InDelta(t, 2*testRate, rs.played, 0.01*testRate, "two seconds of device frames")
	require.InDelta(t, 2*24000, s.Position(), 0.03*24000, "are two seconds of the source")
	require.Zero(t, r.underruns)
}

func TestCloseEndsTheStreamOnTheDevice(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, testRate))
	other := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 2}, testRate))
	r.announce(DeviceRunning, testRate)
	require.Len(t, r.opens, 2)
	require.NotEqual(t, r.opens[0].Stream, r.opens[1].Stream)

	require.NoError(t, s.Close())
	require.Equal(t, []uint32{s.id}, r.closes)
	require.NoError(t, s.Close(), "close is idempotent")
	require.Error(t, s.SeekFrame(0))

	// The device forgot it: a report for it, and an announcement, pass it by.
	require.NoError(t, r.dev.Receive(AppendPlayed(nil, s.id, 1, 10)))
	r.announce(DeviceRunning, testRate)
	require.Len(t, r.opens, 3, "only the open stream is opened on the new device")
	require.Equal(t, other.id, r.opens[2].Stream)
}

func TestOpenRefusesWhatTheLaneDoesNotCarry(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	_, err := r.dev.OpenSink(nil, "")
	require.Error(t, err)
	six, err := pcm.NewSynthSource(pcm.Format{SampleRate: testRate, Channels: 6}, 10, pcm.Silence())
	require.NoError(t, err)
	_, err = r.dev.OpenSink(six, "")
	require.Error(t, err)
	require.Error(t, r.dev.Receive(AppendOpen(nil, 1, 1, testRate, 2)), "a device does not send an open")
	require.Error(t, r.dev.Receive(nil))

	r.dev.Close()
	_, err = r.dev.OpenSink(ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, 10), "")
	require.Error(t, err)
}

// A device that is not synchronous tops up on its own goroutine: Play
// returns without having read the source.
func TestAnAsynchronousDeviceFillsOffTheCaller(t *testing.T) {
	sent := make(chan Message, 64)
	dev := NewDevice(func(msg []byte) {
		m, err := Decode(append([]byte(nil), msg...))
		if err == nil {
			sent <- m
		}
	}, Options{})
	defer dev.Close()
	out, err := dev.OpenSink(ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, 10*testRate), "")
	require.NoError(t, err)
	defer func() { _ = out.Close() }()
	require.NoError(t, dev.Receive(AppendDevice(nil, DeviceRunning, testRate)))
	require.Equal(t, KindOpen, (<-sent).Kind)

	out.Play()
	select {
	case m := <-sent:
		require.Equal(t, KindChunk, m.Kind)
	case <-time.After(2 * time.Second):
		t.Fatal("no chunk followed Play")
	}
}

func TestWireRoundTrip(t *testing.T) {
	m, err := Decode(AppendOpen(nil, 7, 3, 44100, 2))
	require.NoError(t, err)
	require.Equal(t, Message{Kind: KindOpen, Stream: 7, Epoch: 3, SampleRate: 44100, Channels: 2, Format: SampleFormatS16LE}, m)

	m, err = Decode(AppendChunk(nil, 7, 3, []float32{0, 1, -1, 2, -2, 0.5}))
	require.NoError(t, err)
	require.Equal(t, KindChunk, m.Kind)
	require.Equal(t, []byte{0, 0, 0xff, 0x7f, 0x01, 0x80, 0xff, 0x7f, 0x00, 0x80, 0x00, 0x40}, m.Samples, "full scale is ±32767, what lies outside clips")

	m, err = Decode(AppendFlush(nil, 7, 4))
	require.NoError(t, err)
	require.Equal(t, Message{Kind: KindFlush, Stream: 7, Epoch: 4}, m)
	m, err = Decode(AppendClose(nil, 7))
	require.NoError(t, err)
	require.Equal(t, Message{Kind: KindClose, Stream: 7}, m)
	m, err = Decode(AppendWant(nil))
	require.NoError(t, err)
	require.Equal(t, Message{Kind: KindWant}, m)
	m, err = Decode(AppendDevice(nil, DeviceRunning, 48000))
	require.NoError(t, err)
	require.Equal(t, Message{Kind: KindDevice, State: DeviceRunning, SampleRate: 48000}, m)
	m, err = Decode(AppendPlayed(nil, 7, 4, 1<<33))
	require.NoError(t, err)
	require.Equal(t, Message{Kind: KindPlayed, Stream: 7, Epoch: 4, Frames: 1 << 33}, m)

	for _, short := range [][]byte{{byte(KindOpen), 1}, {byte(KindChunk)}, {byte(KindPlayed), 0, 0, 0, 0}, {0x7f}} {
		_, err = Decode(short)
		require.Error(t, err)
	}
	require.Equal(t, "running", DeviceRunning.String())
}

// A host that traces the lane is told, per report, what the device said next
// to what this side had assumed of it.
func TestOnReportCarriesTheEstimateBesideTheReport(t *testing.T) {
	var reports []Report
	r := newRemote(t, Options{Sync: true, OnReport: func(rep Report) { reports = append(reports, rep) }})
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, 30*testRate))
	r.announce(DeviceRunning, testRate)
	s.Play()
	for range 10 {
		r.advance(20 * time.Millisecond)
	}
	require.Len(t, reports, 10)
	last := reports[len(reports)-1]
	require.Equal(t, s.id, last.Stream)
	require.Equal(t, uint32(testRate), last.DeviceRate)
	require.True(t, last.Playing)
	require.Equal(t, int64(10*testRate/50), last.Reported)
	require.Equal(t, last.Reported, last.Estimated, "a device on this side's clock is where it was taken to be")
	require.Equal(t, int64(defaultTarget.Seconds()*testRate), last.Target)
	require.Greater(t, last.Sent, last.Reported)
}
