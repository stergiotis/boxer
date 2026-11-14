package pulsesink

import (
	"testing"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/science/audio/sink/playout"
)

// What the callback reads — continuity across a rate change, strictly
// forward reads — is the engine's and is tested in package playout. What is
// tested here is the callback's own contract toward the stream.

// The callback never ends the stream while the sink is open: it waits while
// the sink is not playing, and after the start it first delivers the frames
// the server needs before it plays, silence where there is nothing else.
func TestReadWaitsWhileNotPlayingAndPrimesWithSilence(t *testing.T) {
	format := pcm.Format{SampleRate: 1000, Channels: 2}
	src, err := pcm.NewSynthSource(format, 50, pcm.Sine(format, 100, 0.8))
	require.NoError(t, err)
	eng, err := playout.New(src, playout.Options{})
	require.NoError(t, err)
	// A sink with no stream: Play starts nothing, the callback is called by
	// hand.
	inst := &Sink{eng: eng, channels: int(format.Channels)}

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
	inst.eng.Lock()
	inst.primeLeft = 25
	inst.eng.Unlock()
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
	require.NoError(t, inst.Close())
	r = returns(done)
	require.Equal(t, 0, r.n)
	require.ErrorIs(t, r.err, pulse.EndOfData)
}
