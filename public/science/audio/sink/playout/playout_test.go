package playout

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
)

// pull is the device side of a test: one request for outFrames device
// frames, ending playback when the source gives none, as a device does.
func pull(inst *Engine, out []float32, outFrames int) (produced int) {
	inst.Lock()
	defer inst.Unlock()
	if inst.StateLocked() != sink.StatePlaying {
		return 0
	}
	produced = inst.ProduceLocked(out, outFrames)
	if produced == 0 {
		inst.EndLocked()
	}
	return produced
}

func newPlaying(t require.TestingT, src pcm.SourceI, opts Options) (inst *Engine) {
	inst, err := New(src, opts)
	require.NoError(t, err)
	inst.Play()
	return inst
}

// maxJump is the largest sample-to-sample step in an interleaved buffer's
// channel ch, the crude discontinuity detector: a sine's step is bounded by
// its slope, a glitch is not.
func maxJump(samples []float32, channels, ch int, prev float32) (jump float32, last float32) {
	last = prev
	for i := ch; i < len(samples); i += channels {
		if d := float32(math.Abs(float64(samples[i] - last))); d > jump {
			jump = d
		}
		last = samples[i]
	}
	return jump, last
}

func TestProduceStaysContinuousAcrossARateExcursion(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	src, err := pcm.NewSynthSource(format, 48000*120, pcm.Sine(format, 440, 0.8))
	require.NoError(t, err)
	inst := newPlaying(t, src, Options{})

	// A 440 Hz sine at 48 kHz moves at most 2π·440/48000·0.8 ≈ 0.046 per
	// frame; at rate 1.5 that is 0.069. Anything past 0.12 is a glitch.
	const limit = 0.12
	sizes := []int{1024, 960, 2048, 1000, 1536, 512}
	rates := []float64{1, 1, 1.5, 1.5, 1.5, 1, 1, 1, 0.75, 1, 1, 1}
	var prev float32
	first := true
	for step := range 600 {
		rate := rates[(step/50)%len(rates)]
		if inst.Rate() != rate {
			require.NoError(t, inst.SetRate(rate))
		}
		out := make([]float32, sizes[step%len(sizes)]*2)
		n := pull(inst, out, len(out)/2)
		require.Positive(t, n)
		got := out[:n*2]
		if first {
			prev = got[0]
			first = false
		}
		jump, last := maxJump(got, 2, 0, prev)
		require.Lessf(t, jump, float32(limit), "step %d rate %v size %d: discontinuity %.3f (frac=%.3f cursor=%d)", step, rate, len(out)/2, jump, inst.frac, inst.cursor)
		prev = last
	}
}

// offsetSource records the offset of every read so a test can tell whether
// the engine reads its source strictly forwards — the property an
// ffmpeg-backed source needs, since any other offset restarts the decoder
// (ADR-0208 SD5).
type offsetSource struct {
	pcm.SourceI
	offsets []int64
	lens    []int
}

func (inst *offsetSource) ReadFramesAt(ctx context.Context, off int64, dst []float32) (n int, err error) {
	n, err = inst.SourceI.ReadFramesAt(ctx, off, dst)
	inst.offsets = append(inst.offsets, off)
	inst.lens = append(inst.lens, n)
	return n, err
}

func (inst *offsetSource) backwardReads() (n int) {
	for i := 1; i < len(inst.offsets); i++ {
		if inst.offsets[i] != inst.offsets[i-1]+int64(inst.lens[i-1]) {
			n++
		}
	}
	return n
}

func TestProduceIsSequentialAcrossARateExcursion(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	base, err := pcm.NewSynthSource(format, 48000*120, pcm.Sine(format, 440, 0.8))
	require.NoError(t, err)
	src := &offsetSource{SourceI: base}
	inst := newPlaying(t, src, Options{})
	// Request sizes a server actually hands out, and rates whose products
	// with them are not whole numbers — the case a single size hides.
	sizes := []int{1024, 960, 2048, 1000, 1536, 512}
	for step := range 400 {
		switch step {
		case 100:
			require.NoError(t, inst.SetRate(1.31))
		case 200:
			require.NoError(t, inst.SetRate(0.83))
		case 300:
			require.NoError(t, inst.SetRate(1))
		}
		out := make([]float32, sizes[step%len(sizes)]*2)
		require.Positive(t, pull(inst, out, len(out)/2))
	}
	require.Zero(t, src.backwardReads(), "every read must continue where the previous one ended, at any rate and after a rate change")
	require.Zero(t, inst.frac, "back at rate 1 the fractional position snaps to zero, so playback is again bit-exact")
}

// A device that states its own rate gets the source resampled to it: the
// whole source comes out as its duration's worth of device frames, read
// forwards and without a discontinuity.
func TestProduceAtADeviceRate(t *testing.T) {
	format := pcm.Format{SampleRate: 44100, Channels: 2}
	const seconds = 3
	base, err := pcm.NewSynthSource(format, 44100*seconds, pcm.Sine(format, 440, 0.8))
	require.NoError(t, err)
	src := &offsetSource{SourceI: base}
	inst := newPlaying(t, src, Options{DeviceRate: 48000})
	require.Equal(t, uint32(48000), inst.DeviceRate())

	sizes := []int{480, 1024, 333}
	var total int
	var prev float32
	first := true
	for step := 0; ; step++ {
		out := make([]float32, sizes[step%len(sizes)]*2)
		n := pull(inst, out, len(out)/2)
		if n == 0 {
			break
		}
		got := out[:n*2]
		if first {
			prev, first = got[0], false
		}
		jump, last := maxJump(got, 2, 0, prev)
		require.Less(t, jump, float32(0.06), "step %d: discontinuity", step)
		prev = last
		total += n
	}
	// The interpolation needs a neighbour past the last frame it reads, so
	// the tail of the source, a frame or two, is not played.
	require.InDelta(t, 48000*seconds, total, 4)
	require.Zero(t, src.backwardReads())
	require.True(t, inst.Ended())
	require.Equal(t, sink.StateStopped, inst.State())
	require.Equal(t, inst.Frames(), inst.Position())
}

// The position trails what was delivered by what the device says it holds,
// advances with the clock in between, and never passes what was delivered.
func TestPositionTrailsDeliveredByTheLag(t *testing.T) {
	format := pcm.Format{SampleRate: 1000, Channels: 1}
	src, err := pcm.NewSynthSource(format, 100000, pcm.Silence())
	require.NoError(t, err)
	clock := sink.NewManualClock(time.Unix(0, 0))
	inst := newPlaying(t, src, Options{Clock: clock})
	out := make([]float32, 500)

	require.Equal(t, 500, pull(inst, out, 500))
	inst.Lock()
	inst.SetLagLocked(200)
	inst.Unlock()
	require.Equal(t, int64(300), inst.Position(), "500 delivered, 200 of them unplayed")

	clock.Advance(100 * time.Millisecond)
	require.Equal(t, int64(400), inst.Position())
	clock.Advance(time.Second)
	require.Equal(t, int64(500), inst.Position(), "never ahead of what was delivered")

	// A later report of a larger lag does not move the readout back.
	inst.Lock()
	inst.SetLagLocked(400)
	inst.Unlock()
	require.Equal(t, int64(500), inst.Position())

	// At twice the rate a device frame spans two source frames.
	require.NoError(t, inst.SeekFrame(1000))
	require.NoError(t, inst.SetRate(2))
	require.Equal(t, 500, pull(inst, out, 500))
	inst.Lock()
	inst.SetLagLocked(100)
	inst.Unlock()
	require.Equal(t, int64(1000+1000-200), inst.Position())
}

// With a device rate the lag is counted in device frames.
func TestPositionAtADeviceRate(t *testing.T) {
	format := pcm.Format{SampleRate: 24000, Channels: 1}
	src, err := pcm.NewSynthSource(format, 240000, pcm.Silence())
	require.NoError(t, err)
	clock := sink.NewManualClock(time.Unix(0, 0))
	inst := newPlaying(t, src, Options{Clock: clock, DeviceRate: 48000})
	out := make([]float32, 4800)
	require.Equal(t, 4800, pull(inst, out, 4800))
	inst.Lock()
	inst.SetLagLocked(2400)
	inst.Unlock()
	// 4800 device frames are 2400 source frames; half of them are unplayed.
	require.InDelta(t, 1200, inst.Position(), 2)
	clock.Advance(10 * time.Millisecond)
	require.InDelta(t, 1440, inst.Position(), 2)
}

// TestTransportInvariants drives an engine and a device that pulls whenever
// time passes through a random command sequence, and checks what every
// consumer of a sink relies on: the position stays inside the source, Ended
// and State agree, Ended pins the position to the end, and the position only
// moves backwards through a seek or a restarting play.
func TestTransportInvariants(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		format := pcm.Format{
			SampleRate: rapid.SampledFrom([]uint32{8000, 22050, 44100, 48000}).Draw(rt, "sampleRate"),
			Channels:   rapid.Uint16Range(1, 2).Draw(rt, "channels"),
		}
		deviceRate := rapid.SampledFrom([]uint32{0, 44100, 48000}).Draw(rt, "deviceRate")
		frames := rapid.Int64Range(0, 4*int64(format.SampleRate)).Draw(rt, "frames")
		src, err := pcm.NewSynthSource(format, frames, pcm.Silence())
		require.NoError(rt, err)
		clock := sink.NewManualClock(time.Unix(0, 0))
		inst, err := New(src, Options{Clock: clock, DeviceRate: deviceRate})
		require.NoError(rt, err)
		lag := rapid.Int64Range(0, 4096).Draw(rt, "lag")
		inst.Lock()
		inst.SetLagLocked(lag)
		inst.Unlock()
		out := make([]float32, 2048*int(format.Channels))

		commands := []string{"play", "pause", "advance", "seek", "rate"}
		prev := inst.Position()
		mayDrop := false
		for range rapid.IntRange(1, 64).Draw(rt, "steps") {
			switch rapid.SampledFrom(commands).Draw(rt, "cmd") {
			case "play":
				mayDrop = mayDrop || prev >= frames
				inst.Play()
			case "pause":
				inst.Pause()
			case "advance":
				clock.Advance(time.Duration(rapid.Int64Range(0, int64(300*time.Millisecond)).Draw(rt, "d")))
				pull(inst, out, rapid.IntRange(1, 2048).Draw(rt, "want"))
			case "seek":
				require.NoError(rt, inst.SeekFrame(rapid.Int64Range(-1024, 2*frames+1024).Draw(rt, "frame")))
				mayDrop = true
			case "rate":
				require.NoError(rt, inst.SetRate(rapid.Float64Range(sink.RateMinExcl+0.01, sink.RateMaxIncl).Draw(rt, "rate")))
			}
			pos := inst.Position()
			state := inst.State()
			ended := inst.Ended()
			require.GreaterOrEqual(rt, pos, int64(0))
			require.LessOrEqual(rt, pos, frames)
			if ended {
				require.Equal(rt, sink.StateStopped, state)
				require.Equal(rt, frames, pos)
			}
			if !mayDrop {
				require.GreaterOrEqual(rt, pos, prev, "the position went back without a seek or a restart")
			}
			prev, mayDrop = pos, false
		}
	})
}

func TestWaitLockedIsReleasedByPlayAndByClose(t *testing.T) {
	format := pcm.Format{SampleRate: 1000, Channels: 1}
	src, err := pcm.NewSynthSource(format, 100, pcm.Silence())
	require.NoError(t, err)
	inst, err := New(src, Options{})
	require.NoError(t, err)

	wait := func() (done chan sink.StateE) {
		done = make(chan sink.StateE, 1)
		go func() {
			inst.Lock()
			for inst.StateLocked() != sink.StatePlaying && !inst.ClosedLocked() {
				inst.WaitLocked()
			}
			done <- inst.StateLocked()
			inst.Unlock()
		}()
		return done
	}
	done := wait()
	select {
	case <-done:
		t.Fatal("the wait returned on a stopped engine")
	case <-time.After(30 * time.Millisecond):
	}
	inst.Play()
	require.Equal(t, sink.StatePlaying, <-done)

	inst.Pause()
	done = wait()
	require.NoError(t, inst.Close())
	require.Equal(t, sink.StateStopped, <-done)
	require.Error(t, inst.SeekFrame(0), "a closed engine refuses a seek")
	require.Error(t, inst.SetRate(1))
	require.Error(t, inst.SetVolume(1))
	require.NoError(t, inst.Close(), "close is idempotent")
}

func TestRangesAndGain(t *testing.T) {
	format := pcm.Format{SampleRate: 1000, Channels: 1}
	src, err := pcm.NewSynthSource(format, 1000, func(int64, int) float32 { return 0.5 })
	require.NoError(t, err)
	inst := newPlaying(t, src, Options{})
	require.Error(t, inst.SetRate(sink.RateMinExcl))
	require.Error(t, inst.SetRate(sink.RateMaxIncl+0.1))
	require.Error(t, inst.SetRate(math.NaN()))
	require.Error(t, inst.SetVolume(-0.1))
	require.Error(t, inst.SetVolume(1.1))
	require.NoError(t, inst.SetVolume(0.5))
	out := make([]float32, 10)
	require.Equal(t, 10, pull(inst, out, 10))
	require.InDelta(t, 0.25, out[0], 1e-6)

	_, err = New(nil, Options{})
	require.Error(t, err)
}
