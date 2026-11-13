package pulsesink

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
)

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

func TestReadStaysContinuousAcrossARateExcursion(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	src, err := pcm.NewSynthSource(format, 48000*120, pcm.Sine(format, 440, 0.8))
	require.NoError(t, err)
	inst := &Sink{format: format, frames: src.Frames(), src: src, clock: sink.RealClock{}, state: sink.StatePlaying, rate: 1, volume: 1}

	// A 440 Hz sine at 48 kHz moves at most 2π·440/48000·0.8 ≈ 0.046 per
	// frame; at rate 1.5 that is 0.069. Anything past 0.12 is a glitch.
	const limit = 0.12
	sizes := []int{1024, 960, 2048, 1000, 1536, 512}
	rates := []float64{1, 1, 1.5, 1.5, 1.5, 1, 1, 1, 0.75, 1, 1, 1}
	var prev float32
	first := true
	for step := range 600 {
		rate := rates[(step/50)%len(rates)]
		if inst.rate != rate {
			require.NoError(t, inst.SetRate(rate))
		}
		out := make([]float32, sizes[step%len(sizes)]*2)
		n, err := inst.read(out)
		require.NoError(t, err)
		require.Positive(t, n)
		got := out[:n]
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
// the sink reads its source strictly forwards — the property an ffmpeg-backed
// source needs, since any other offset restarts the decoder (ADR-0208 SD5).
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

func TestReadIsSequentialAcrossARateExcursion(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	base, err := pcm.NewSynthSource(format, 48000*120, pcm.Sine(format, 440, 0.8))
	require.NoError(t, err)
	src := &offsetSource{SourceI: base}
	inst := &Sink{format: format, frames: base.Frames(), src: src, clock: sink.RealClock{}, state: sink.StatePlaying, rate: 1, volume: 1}
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
		_, err := inst.read(out)
		require.NoError(t, err)
	}
	require.Zero(t, src.backwardReads(), "every read must continue where the previous one ended, at any rate and after a rate change")
	require.Zero(t, inst.frac, "back at rate 1 the fractional position snaps to zero, so playback is again bit-exact")
}

// The callback never ends the stream while the sink is open: it waits while
// the sink is not playing, and after the start it first delivers the frames
// the server needs before it plays, silence where there is nothing else.
func TestReadWaitsWhileNotPlayingAndPrimesWithSilence(t *testing.T) {
	format := pcm.Format{SampleRate: 1000, Channels: 2}
	src, err := pcm.NewSynthSource(format, 50, pcm.Sine(format, 100, 0.8))
	require.NoError(t, err)
	inst := &Sink{format: format, frames: src.Frames(), src: src, clock: sink.RealClock{}, state: sink.StateStopped, rate: 1, volume: 1}

	type result struct {
		n   int
		err error
	}
	read := func(frames int) (out []float32, done chan result) {
		out = make([]float32, frames*2)
		for i := range out {
			out[i] = 9
		}
		done = make(chan result, 1)
		go func() {
			n, err := inst.read(out)
			done <- result{n, err}
		}()
		return
	}
	waits := func(done chan result) {
		t.Helper()
		select {
		case r := <-done:
			t.Fatalf("the callback returned %d samples while the sink was not playing", r.n)
		case <-time.After(50 * time.Millisecond):
		}
	}
	returns := func(done chan result) (r result) {
		t.Helper()
		select {
		case r = <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("the callback did not return")
		}
		return
	}

	// Stopped: the callback waits, and Play releases it.
	_, done := read(20)
	waits(done)
	inst.Play()
	r := returns(done)
	require.NoError(t, r.err)
	require.Equal(t, 40, r.n)

	// Across the end of the source: what is left, then a wait, not the end
	// of the stream.
	out, done := read(40)
	r = returns(done)
	require.Equal(t, 60, r.n, "the 30 frames left")
	require.NotEqual(t, float32(9), out[0])
	_, done = read(20)
	waits(done)
	require.True(t, inst.Ended())
	require.Equal(t, sink.StateStopped, inst.State())

	// Play at the end starts over.
	inst.Play()
	r = returns(done)
	require.Equal(t, 40, r.n)
	require.False(t, inst.Ended())

	// Paused with frames owed to the server: silence, then a wait.
	inst.Pause()
	inst.mu.Lock()
	inst.primeLeft = 25
	inst.mu.Unlock()
	out, done = read(20)
	r = returns(done)
	require.Equal(t, 40, r.n)
	require.Equal(t, float32(0), out[0])
	require.Equal(t, float32(0), out[39])
	out, done = read(20)
	r = returns(done)
	require.Equal(t, 10, r.n, "the five frames still owed")
	_, done = read(20)
	waits(done)

	// Closing releases the callback and ends the stream.
	inst.mu.Lock()
	inst.closed = true
	inst.condLocked().Broadcast()
	inst.mu.Unlock()
	r = returns(done)
	require.Equal(t, 0, r.n)
	require.ErrorIs(t, r.err, pulse.EndOfData)
}
