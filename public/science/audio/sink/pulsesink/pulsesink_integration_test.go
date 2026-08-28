//go:build integration

package pulsesink_test

import (
	"testing"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/science/audio/sink/pulsesink"
)

// The lane needs a reachable PulseAudio (or PipeWire pulse) server; without
// one the tests skip. The source is silence so a run makes no sound.
func requireServer(t *testing.T) {
	t.Helper()
	c, err := pulse.NewClient(pulse.ClientApplicationName("boxer-test-probe"))
	if err != nil {
		t.Skipf("no audio server reachable: %v", err)
	}
	c.Close()
}

func openSilence(t *testing.T, seconds int) (s *pulsesink.Sink, format pcm.Format) {
	t.Helper()
	format = pcm.Format{SampleRate: 48000, Channels: 2}
	src, err := pcm.NewSynthSourceE(format, format.DurationToFrames(time.Duration(seconds)*time.Second), pcm.Silence())
	require.NoError(t, err)
	s, err = pulsesink.OpenE(src, pulsesink.Options{AppName: "boxer-test", Latency: 40 * time.Millisecond})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.CloseE()) })
	return s, format
}

func TestPlayAdvancesPauseHolds(t *testing.T) {
	requireServer(t)
	s, format := openSilence(t, 10)
	require.Equal(t, sink.StateStopped, s.State())
	require.Equal(t, int64(0), s.Position())

	s.Play()
	require.Equal(t, sink.StatePlaying, s.State())
	time.Sleep(600 * time.Millisecond)
	pos := s.Position()
	// Between 0.3 s and 0.9 s of a 0.6 s wait: the buffer lag and the
	// scheduler both eat into the low end.
	require.Greater(t, pos, format.DurationToFrames(300*time.Millisecond), "position did not advance")
	require.Less(t, pos, format.DurationToFrames(900*time.Millisecond), "position ran ahead of the clock")

	s.Pause()
	require.Equal(t, sink.StatePaused, s.State())
	held := s.Position()
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, held, s.Position(), "a paused position must not move")

	s.Play()
	time.Sleep(200 * time.Millisecond)
	require.Greater(t, s.Position(), held, "resume did not continue")
	require.False(t, s.Underflow(), "silence at 40 ms latency should not underflow")
}

func TestSeekFlushesAndEndStops(t *testing.T) {
	requireServer(t)
	s, format := openSilence(t, 2)
	s.Play()
	time.Sleep(150 * time.Millisecond)
	target := format.DurationToFrames(1700 * time.Millisecond)
	require.NoError(t, s.SeekE(target))
	pos := s.Position()
	require.GreaterOrEqual(t, pos, target, "position after a seek starts at the target")
	require.Less(t, pos, target+format.DurationToFrames(150*time.Millisecond))

	// 300 ms of audio remain; the stream must end on its own.
	deadline := time.Now().Add(3 * time.Second)
	for !s.Ended() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	require.True(t, s.Ended(), "playback did not end")
	require.Equal(t, sink.StateStopped, s.State())
	require.Equal(t, s.Frames(), s.Position())

	// Play after the end restarts from 0.
	s.Play()
	time.Sleep(100 * time.Millisecond)
	require.Less(t, s.Position(), format.DurationToFrames(500*time.Millisecond))
	require.False(t, s.Ended())
}

func TestRateAndVolume(t *testing.T) {
	requireServer(t)
	s, format := openSilence(t, 10)
	require.NoError(t, s.SetVolumeE(0.25))
	require.Equal(t, 0.25, s.Volume())
	require.Error(t, s.SetRateE(0))
	require.NoError(t, s.SetRateE(2))
	s.Play()
	time.Sleep(600 * time.Millisecond)
	pos := s.Position()
	// At 2× the position covers roughly twice the wall time.
	require.Greater(t, pos, format.DurationToFrames(700*time.Millisecond))
	require.Less(t, pos, format.DurationToFrames(1700*time.Millisecond))
}
