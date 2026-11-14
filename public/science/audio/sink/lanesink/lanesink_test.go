package lanesink

import (
	"context"
	"encoding/binary"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
)

// remote is the far end of the lane as a test plays it, behaving as the
// viewer page does: it takes the messages the device sends, plays what it
// holds at its rate when the clock moves, and reports a stream only when its
// played count changed. Frames a lost chunk carried are counted as played
// once it reaches them. It never calls into the device from send.
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
	// drop makes send lose that many of the next chunk messages.
	drop int
	// underruns counts the advances in which a stream with a chunk behind it
	// had less to play than the time asked for.
	underruns int
}

type remoteStream struct {
	epoch    uint32
	channels int
	// end is the epoch frame index up to which chunks arrived; holes are the
	// index ranges in between that never did.
	end      int64
	holes    [][2]int64
	played   int64
	reported int64
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
		r.streams[m.Stream] = &remoteStream{epoch: m.Epoch, channels: int(m.Channels), reported: -1}
	case KindChunk:
		s := r.streams[m.Stream]
		if s == nil || m.Epoch != s.epoch {
			return
		}
		if r.drop > 0 {
			r.drop--
			return
		}
		r.chunks++
		require.Zero(r.t, len(m.Samples)%(2*s.channels), "a chunk holds whole frames")
		first := int64(m.Frames)
		require.GreaterOrEqual(r.t, first, s.end, "chunks do not overlap")
		if first > s.end {
			s.holes = append(s.holes, [2]int64{s.end, first})
		}
		for i := 0; i+1 < len(m.Samples); i += 2 {
			s.samples = append(s.samples, int16(binary.LittleEndian.Uint16(m.Samples[i:])))
		}
		s.end = first + int64(len(m.Samples)/(2*s.channels))
	case KindFlush:
		r.flushes = append(r.flushes, m)
		s := r.streams[m.Stream]
		require.NotNil(r.t, s)
		*s = remoteStream{epoch: m.Epoch, channels: s.channels, reported: -1}
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

// inFlight is how many frames a stream holds unplayed.
func (s *remoteStream) inFlight() (frames int64) {
	frames = s.end - s.played
	for _, h := range s.holes {
		if h[1] > s.played {
			frames -= h[1] - max(h[0], s.played)
		}
	}
	return frames
}

// advance moves the clock by d, plays that much of every stream, and reports
// the streams whose count changed.
func (r *remote) advance(d time.Duration) {
	r.clock.Advance(d)
	budget := int64(d.Seconds() * float64(r.rate))
	for id, s := range r.streams {
		if s.inFlight() < budget && s.end > 0 {
			r.underruns++
		}
		left := budget
		for left > 0 && s.played < s.end {
			stop := s.end
			skipped := false
			for _, h := range s.holes {
				if h[0] <= s.played && s.played < h[1] {
					s.played, skipped = h[1], true
					break
				}
				if h[0] > s.played {
					stop = min(stop, h[0])
				}
			}
			if skipped {
				continue
			}
			n := min(left, stop-s.played)
			s.played += n
			left -= n
		}
		// A hole directly ahead costs no time.
		for _, h := range s.holes {
			if h[0] == s.played {
				s.played = h[1]
			}
		}
		if s.played != s.reported {
			s.reported = s.played
			require.NoError(r.t, r.dev.Receive(AppendPlayed(nil, id, s.epoch, uint64(s.played))))
		}
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

var targetFrames = int64(defaultTarget.Seconds() * testRate)

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
	require.Equal(t, targetFrames, rs.end, "the first top-up is one target")
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
	least := int64(quantum.Seconds() * testRate)

	for range 500 {
		r.advance(20 * time.Millisecond)
		require.LessOrEqual(t, rs.inFlight(), targetFrames)
		require.Greater(t, rs.inFlight(), targetFrames-least-1, "each report is answered with a top-up")
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
	require.LessOrEqual(t, s.Position(), rs.end)
}

func TestASeekFlushesTheDevice(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	src := &countingSource{SourceI: ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, 30*testRate)}
	s := open(t, r, src)
	r.announce(DeviceRunning, testRate)
	s.Play()
	r.advance(20 * time.Millisecond)
	before := r.opens[0].Epoch

	reads := src.reads.Load()
	require.NoError(t, s.SeekFrame(5*testRate))
	require.Len(t, r.flushes, 1)
	require.Greater(t, r.flushes[0].Epoch, before)
	require.Equal(t, int64(5*testRate), s.Position())
	require.Equal(t, reads, src.reads.Load(), "on a synchronous device a seek reads nothing on the caller")

	// A report still on its way for the old epoch changes nothing.
	require.NoError(t, r.dev.Receive(AppendPlayed(nil, s.id, before, 1<<40)))
	rs := r.only()
	require.Zero(t, rs.end)
	require.Equal(t, int64(5*testRate), s.Position())

	// The device's report after the flush brings the top-up.
	r.advance(20 * time.Millisecond)
	require.Equal(t, r.flushes[0].Epoch, rs.epoch)
	require.Equal(t, targetFrames, rs.end)
	require.Equal(t, rampAt(5*testRate, 0), rs.samples[0], "the chunks after the flush are from the new position")

	// Sixty seeks, one second of dragging, read nothing more until a report.
	reads = src.reads.Load()
	for i := range 60 {
		require.NoError(t, s.SeekFrame(int64(i)*1000))
	}
	require.Equal(t, reads, src.reads.Load())
}

// countingSource counts the reads made of it.
type countingSource struct {
	pcm.SourceI
	reads atomic.Int64
}

func (inst *countingSource) ReadFramesAt(ctx context.Context, off int64, dst []float32) (n int, err error) {
	inst.reads.Add(1)
	return inst.SourceI.ReadFramesAt(ctx, off, dst)
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
	require.Equal(t, int64(defaultTarget.Seconds()*44100), r.only().end)
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
	require.LessOrEqual(t, rs.inFlight(), int64(maxTarget.Seconds()*testRate))

	// Back at a fast report the target comes down again.
	for range 200 {
		r.advance(20 * time.Millisecond)
	}
	require.Less(t, rs.inFlight(), int64(testRate/2))
}

// A device with nothing to play reports nothing, and that silence is not a
// slow report: a stream played after a quiet spell is topped up to its
// target, not to seconds.
func TestIdleTimeDoesNotRaiseTheTarget(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, 600*testRate))
	r.announce(DeviceRunning, testRate)
	// Attached and stopped for five seconds, then played.
	r.clock.Advance(5 * time.Second)
	s.Play()
	rs := r.only()
	for range 10 {
		r.advance(20 * time.Millisecond)
		require.LessOrEqual(t, rs.inFlight(), targetFrames)
	}
	require.InDelta(t, 10*testRate/50, s.Position(), 0.03*testRate, "and the readout starts with the device")

	// Paused until the device has run dry, quiet for three seconds, resumed.
	s.Pause()
	for range 20 {
		r.advance(20 * time.Millisecond)
	}
	require.Zero(t, rs.inFlight())
	r.clock.Advance(3 * time.Second)
	s.Play()
	for range 10 {
		r.advance(20 * time.Millisecond)
		require.LessOrEqual(t, rs.inFlight(), targetFrames)
	}
}

// A chunk that never arrives leaves a hole the device plays past once a
// later one shows it: it counts the missing frames as played, so the two
// sides agree again on what is in flight and the stream carries on.
func TestALostChunkDoesNotShrinkTheWindow(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, 600*testRate))
	r.announce(DeviceRunning, testRate)
	s.Play()
	for range 10 {
		r.advance(20 * time.Millisecond)
	}
	rs := r.only()
	least := int64(quantum.Seconds() * testRate)
	// Less than a target is lost, so later chunks still come.
	r.drop = 3
	for range 100 {
		r.advance(20 * time.Millisecond)
	}
	require.Zero(t, r.drop)
	require.Len(t, rs.holes, 1)
	require.Greater(t, rs.inFlight(), targetFrames-least-1, "the window is whole again")
	require.LessOrEqual(t, rs.inFlight(), targetFrames)
	require.Equal(t, sink.StatePlaying, s.State())
	require.InDelta(t, rs.played, s.Position(), 0.03*testRate, "the readout follows the device past the hole")
}

// When everything in flight is lost no later chunk shows the hole and the
// device has nothing to report. The stream restarts at the last frame the
// device confirmed instead of staying silent.
func TestAStreamWhoseFlightIsLostRestarts(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, 600*testRate))
	r.announce(DeviceRunning, testRate)
	s.Play()
	for range 10 {
		r.advance(20 * time.Millisecond)
	}
	rs := r.only()
	r.drop = 1000
	for range 30 {
		r.advance(20 * time.Millisecond)
	}
	chunks, confirmed := r.chunks, rs.played
	for range 20 {
		r.advance(20 * time.Millisecond)
	}
	require.Equal(t, chunks, r.chunks, "nothing arrives, and the device reports nothing")
	r.drop = 0

	// Not yet: a device may simply be slow to report.
	r.dev.checkStalls()
	require.Len(t, r.opens, 1)

	r.clock.Advance(stallAfter)
	r.dev.checkStalls()
	require.Len(t, r.opens, 2, "opened again, in a new epoch: the open may be what was lost")
	require.Greater(t, r.opens[1].Epoch, r.opens[0].Epoch)
	rs = r.only()
	require.Equal(t, targetFrames, rs.end)
	require.Equal(t, rampAt(confirmed, 0), rs.samples[0], "from the last frame the device confirmed")
	for range 50 {
		r.advance(20 * time.Millisecond)
	}
	require.Equal(t, sink.StatePlaying, s.State())
	require.InDelta(t, confirmed+testRate, s.Position(), 0.03*testRate)

	// A stream that is paused, or has nothing in flight, is left alone.
	s.Pause()
	r.clock.Advance(10 * stallAfter)
	r.dev.checkStalls()
	require.Len(t, r.opens, 2)
}

func TestPauseStopsTheTopUps(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, 30*testRate))
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
}

// Playback has ended when the device played the last frames, not when they
// were sent: until then the stream is playing and its position is short of
// the end.
func TestTheEndIsReachedWhenTheDeviceHasPlayedIt(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	const frames = testRate / 2
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, frames))
	r.announce(DeviceRunning, testRate)
	s.Play()
	rs := r.only()
	for rs.end < frames {
		r.advance(20 * time.Millisecond)
	}
	require.Equal(t, int64(frames), rs.end, "every frame of the source was sent, once")
	require.Greater(t, rs.inFlight(), int64(0))
	require.False(t, s.Ended(), "frames the device still holds are not played yet")
	require.Equal(t, sink.StatePlaying, s.State())
	require.Less(t, s.Position(), int64(frames))

	for range 20 {
		r.advance(20 * time.Millisecond)
	}
	require.True(t, s.Ended())
	require.Equal(t, sink.StateStopped, s.State())
	require.Equal(t, int64(frames), s.Position())

	// Play at the end starts over in a new epoch, so the device drops
	// whatever it might still hold of the old one.
	flushes := len(r.flushes)
	s.Play()
	require.Len(t, r.flushes, flushes+1)
	require.Equal(t, sink.StatePlaying, s.State())
	require.False(t, s.Ended())
	require.Less(t, s.Position(), int64(frames/4))
	require.Equal(t, rampAt(0, 0), rs.samples[0])
}

// A device that never says it played the last frames does not keep the
// stream playing for ever.
func TestTheEndIsReachedWithoutTheLastReport(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	const frames = testRate / 2
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, frames))
	r.announce(DeviceRunning, testRate)
	s.Play()
	for r.only().end < frames {
		r.advance(20 * time.Millisecond)
	}
	r.clock.Advance(100 * time.Millisecond)
	require.False(t, s.Ended())
	r.clock.Advance(2 * time.Second)
	require.True(t, s.Ended())
	require.Equal(t, int64(frames), s.Position())
}

// The switch between the clock and the device carries the end of playback
// and a pause at the end, in both directions.
func TestTheSwitchCarriesEndedAndPausedAtTheEnd(t *testing.T) {
	const frames = testRate
	format := pcm.Format{SampleRate: testRate, Channels: 1}

	t.Run("ended on the clock, then a device", func(t *testing.T) {
		r := newRemote(t, Options{Sync: true})
		s := open(t, r, ramp(t, format, frames))
		s.Play()
		r.clock.Advance(3 * time.Second)
		require.True(t, s.Ended())
		r.announce(DeviceRunning, testRate)
		require.True(t, s.Ended())
		require.Equal(t, sink.StateStopped, s.State())
		require.Equal(t, int64(frames), s.Position())
		require.Zero(t, r.chunks)
	})
	t.Run("ended on a device, then none", func(t *testing.T) {
		r := newRemote(t, Options{Sync: true})
		s := open(t, r, ramp(t, format, frames))
		r.announce(DeviceRunning, testRate)
		s.Play()
		for range 100 {
			r.advance(20 * time.Millisecond)
		}
		require.True(t, s.Ended())
		r.announce(DeviceAbsent, 0)
		require.True(t, s.Ended())
		require.Equal(t, int64(frames), s.Position())
	})
	t.Run("paused at the end", func(t *testing.T) {
		r := newRemote(t, Options{Sync: true})
		s := open(t, r, ramp(t, format, frames))
		s.Play()
		s.Pause()
		require.NoError(t, s.SeekFrame(frames))
		r.announce(DeviceRunning, testRate)
		require.Equal(t, sink.StatePaused, s.State())
		require.Equal(t, int64(frames), s.Position(), "not restarted from the beginning")
		r.announce(DeviceAbsent, 0)
		require.Equal(t, sink.StatePaused, s.State())
		require.Equal(t, int64(frames), s.Position())
	})
}

// A change of rate does not move the readout: the frames the device holds
// keep the span of the source they were made with.
func TestARateChangeDoesNotMoveThePosition(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, 600*testRate))
	r.announce(DeviceRunning, testRate)
	require.NoError(t, s.SetRate(4))
	s.Play()
	for range 50 {
		r.advance(20 * time.Millisecond)
	}
	before := s.Position()
	require.NoError(t, s.SetRate(0.5))
	require.Equal(t, before, s.Position(), "no jump at the change")
	// The device still holds frames made at four times the speed; the
	// readout does not stand still while they play, and never goes back.
	prev := before
	for range 20 {
		r.advance(20 * time.Millisecond)
		pos := s.Position()
		require.GreaterOrEqual(t, pos, prev)
		prev = pos
	}
	require.Greater(t, prev, before)
	require.Less(t, prev, before+int64(4*0.4*testRate)+int64(testRate), "nor does it leap by the old rate's lag")
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

// A rate no device has is not taken for a device: a figure a page reports
// must not end playback or size a buffer.
func TestAnImplausibleDeviceRateIsNoDevice(t *testing.T) {
	for _, rate := range []uint32{0, 1, 100, 199, 7999, 384001, 0xFFFFFFFF} {
		r := newRemote(t, Options{Sync: true})
		s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, 10*testRate))
		s.Play()
		r.announce(DeviceRunning, rate)
		state, _ := r.dev.State()
		require.Equal(t, DeviceAbsent, state, "rate %d", rate)
		require.Empty(t, r.opens)
		require.Equal(t, sink.StatePlaying, s.State())
		require.False(t, s.Ended())
	}
}

func TestCloseEndsTheStreamOnTheDevice(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, testRate))
	other := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 2}, testRate))
	r.announce(DeviceRunning, testRate)
	require.Len(t, r.opens, 2)
	require.NotEqual(t, r.opens[0].Stream, r.opens[1].Stream)

	s.Play()
	r.advance(100 * time.Millisecond)
	pos := s.Position()
	require.NoError(t, s.Close())
	require.Equal(t, []uint32{s.id}, r.closes)
	require.NoError(t, s.Close(), "close is idempotent")
	require.Error(t, s.SeekFrame(0))

	// The device forgot it: a report for it, and an announcement, pass it
	// by, and it keeps the position it was closed at.
	require.NoError(t, r.dev.Receive(AppendPlayed(nil, s.id, 1, 10)))
	r.announce(DeviceRunning, testRate)
	require.Len(t, r.opens, 3, "only the open stream is opened on the new device")
	require.Equal(t, other.id, r.opens[2].Stream)
	require.Equal(t, pos, s.Position())

	// Closing the device ends what is open on the remote one.
	r.dev.Close()
	require.Equal(t, []uint32{s.id, other.id}, r.closes)
	require.Equal(t, sink.StateStopped, other.State())
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
	require.Error(t, r.dev.Receive(AppendProbe(nil)), "nor a probe")
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
		require.Zero(t, m.Frames, "the first chunk starts the epoch")
	case <-time.After(2 * time.Second):
		t.Fatal("no chunk followed Play")
	}
}

// slowSource takes its time over a read, as a decoder does.
type slowSource struct {
	pcm.SourceI
	delay   time.Duration
	reading atomic.Bool
}

func (inst *slowSource) ReadFramesAt(ctx context.Context, off int64, dst []float32) (n int, err error) {
	inst.reading.Store(true)
	time.Sleep(inst.delay)
	n, err = inst.SourceI.ReadFramesAt(ctx, off, dst)
	inst.reading.Store(false)
	return n, err
}

// Whether anybody listens is asked every frame, and must not wait behind an
// announcement that is reading a source.
func TestAudibleDoesNotWaitBehindARead(t *testing.T) {
	dev := NewDevice(func([]byte) {}, Options{})
	defer dev.Close()
	src := &slowSource{SourceI: ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, 10*testRate), delay: 300 * time.Millisecond}
	out, err := dev.OpenSink(src, "")
	require.NoError(t, err)
	defer func() { _ = out.Close() }()
	out.Play()
	done := make(chan struct{})
	go func() {
		_ = dev.Receive(AppendDevice(nil, DeviceRunning, testRate))
		close(done)
	}()
	require.Eventually(t, src.reading.Load, 2*time.Second, time.Millisecond)
	start := time.Now()
	yes, _ := out.(*Sink).Audible()
	require.True(t, yes)
	state, _ := dev.State()
	require.Equal(t, DeviceRunning, state)
	require.Less(t, time.Since(start), 100*time.Millisecond)
	<-done
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
	require.Equal(t, targetFrames, last.Target)
	require.Greater(t, last.Sent, last.Reported)
}

func TestWireRoundTrip(t *testing.T) {
	m, err := Decode(AppendOpen(nil, 7, 3, 44100, 2))
	require.NoError(t, err)
	require.Equal(t, Message{Kind: KindOpen, Stream: 7, Epoch: 3, SampleRate: 44100, Channels: 2, Format: SampleFormatS16LE}, m)

	m, err = Decode(AppendChunk(nil, 7, 3, 1<<33, []float32{0, 1, -1, 2, -2, 0.5}))
	require.NoError(t, err)
	require.Equal(t, KindChunk, m.Kind)
	require.Equal(t, uint64(1<<33), m.Frames, "the chunk's first frame")
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
	m, err = Decode(AppendProbe(nil))
	require.NoError(t, err)
	require.Equal(t, Message{Kind: KindProbe}, m)
	m, err = Decode(AppendDevice(nil, DeviceRunning, 48000))
	require.NoError(t, err)
	require.Equal(t, Message{Kind: KindDevice, State: DeviceRunning, SampleRate: 48000}, m)
	m, err = Decode(AppendPlayed(nil, 7, 4, 1<<33))
	require.NoError(t, err)
	require.Equal(t, Message{Kind: KindPlayed, Stream: 7, Epoch: 4, Frames: 1 << 33}, m)

	for _, short := range [][]byte{{byte(KindOpen), 1}, {byte(KindChunk)}, {byte(KindChunk), 0, 0, 0, 0, 0, 0, 0, 0}, {byte(KindPlayed), 0, 0, 0, 0}, {0x7f}} {
		_, err = Decode(short)
		require.Error(t, err)
	}
	require.Equal(t, "running", DeviceRunning.String())
}

// Whatever target is asked for, the device is kept two and a half report
// intervals and the lead a page starts a stream with ahead: less, and a
// page that reports every 20 ms runs so close to dry that it cannot place a
// chunk behind what it has.
func TestTheTargetHasAFloor(t *testing.T) {
	r := newRemote(t, Options{Sync: true, Target: 10 * time.Millisecond})
	s := open(t, r, ramp(t, pcm.Format{SampleRate: testRate, Channels: 1}, 60*testRate))
	r.announce(DeviceRunning, testRate)
	s.Play()
	rs := r.only()
	for range 50 {
		r.advance(20 * time.Millisecond)
	}
	floor := int64((20*time.Millisecond*5/2 + deviceLead).Seconds() * testRate)
	least := int64(quantum.Seconds() * testRate)
	require.Zero(t, r.underruns)
	require.LessOrEqual(t, rs.inFlight(), floor)
	require.Greater(t, rs.inFlight(), floor-least-1)
}

// A stream opened with a latency above the device's target is kept that far
// ahead; one that asks for less keeps the device's target.
func TestAStreamsLatencyRaisesItsOwnTarget(t *testing.T) {
	r := newRemote(t, Options{Sync: true})
	format := pcm.Format{SampleRate: testRate, Channels: 1}
	deep, err := r.dev.OpenSinkWithLatency(ramp(t, format, 60*testRate), "test", 400*time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(func() { _ = deep.Close() })
	shallow, err := r.dev.OpenSinkWithLatency(ramp(t, format, 60*testRate), "test", 20*time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(func() { _ = shallow.Close() })
	r.announce(DeviceRunning, testRate)
	deep.Play()
	shallow.Play()
	for range 50 {
		r.advance(20 * time.Millisecond)
	}
	least := int64(quantum.Seconds() * testRate)
	asked := int64((400 * time.Millisecond).Seconds() * testRate)
	require.Zero(t, r.underruns)
	for id, rs := range r.streams {
		want := targetFrames
		if id == deep.(*Sink).id {
			want = asked
		}
		require.LessOrEqual(t, rs.inFlight(), want)
		require.Greater(t, rs.inFlight(), want-least-1)
	}
}
