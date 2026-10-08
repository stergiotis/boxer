//go:build integration

package pulsesink_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/science/audio/decode"
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
	src, err := pcm.NewSynthSource(format, format.DurationToFrames(time.Duration(seconds)*time.Second), pcm.Silence())
	require.NoError(t, err)
	s, err = pulsesink.Open(src, pulsesink.Options{AppName: "boxer-test", Latency: 40 * time.Millisecond})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
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
	time.Sleep(300 * time.Millisecond)
	// Position is capped at the frames delivered, so a stream the server
	// stopped pulling from freezes within one buffer (40 ms) of held. Ask
	// for well past that.
	require.Greater(t, s.Position(), held+format.DurationToFrames(150*time.Millisecond), "resume did not continue")
	require.False(t, s.Underflow(), "silence at 40 ms latency should not underflow")
}

// Every pause and resume used to cork and uncork the stream, and the uncork
// provoked a Started event that parked the library's read loop for good
// (jfreymuth/pulse#52): audio died and every later round trip took the
// library's one-second timeout. A pause now parks the pull callback instead.
// This holds the stream alive through repeated cycles and closes fast.
func TestPauseResumeCyclesKeepTheStreamAlive(t *testing.T) {
	requireServer(t)
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	src, err := pcm.NewSynthSource(format, format.DurationToFrames(30*time.Second), pcm.Silence())
	require.NoError(t, err)
	s, err := pulsesink.Open(src, pulsesink.Options{AppName: "boxer-test", Latency: 40 * time.Millisecond})
	require.NoError(t, err)
	s.Play()
	time.Sleep(150 * time.Millisecond)
	for i := 0; i < 5; i++ {
		s.Pause()
		// A pause longer than the latency drains the server; the resume
		// after it is the case that parked the read loop.
		time.Sleep(100 * time.Millisecond)
		held := s.Position()
		s.Play()
		time.Sleep(300 * time.Millisecond)
		require.Greater(t, s.Position(), held+format.DurationToFrames(150*time.Millisecond), "cycle %d: the stream stopped pulling after resume", i)
	}
	// A seek while paused leaves the stream alone; playing on picks it up.
	s.Pause()
	time.Sleep(100 * time.Millisecond)
	target := format.DurationToFrames(20 * time.Second)
	require.NoError(t, s.SeekFrame(target))
	require.Equal(t, target, s.Position())
	s.Play()
	time.Sleep(300 * time.Millisecond)
	require.Greater(t, s.Position(), target+format.DurationToFrames(150*time.Millisecond), "resume after a paused seek did not continue")
	require.False(t, s.Underflow(), "the pauses' own underflows are not reported")
	// Closing from paused must release the callback before the close round
	// trip; a blocked read loop shows as the one-second request timeout.
	s.Pause()
	started := time.Now()
	require.NoError(t, s.Close())
	require.Less(t, time.Since(started), 500*time.Millisecond, "close waited on a blocked read loop")
}

func TestSeekFlushesAndEndStops(t *testing.T) {
	requireServer(t)
	s, format := openSilence(t, 2)
	s.Play()
	time.Sleep(150 * time.Millisecond)
	target := format.DurationToFrames(1700 * time.Millisecond)
	require.NoError(t, s.SeekFrame(target))
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
	require.NoError(t, s.SetVolume(0.25))
	require.Equal(t, 0.25, s.Volume())
	require.Error(t, s.SetRate(0))
	require.NoError(t, s.SetRate(2))
	s.Play()
	time.Sleep(600 * time.Millisecond)
	pos := s.Position()
	// At 2× the position covers roughly twice the wall time.
	require.Greater(t, pos, format.DurationToFrames(700*time.Millisecond))
	require.Less(t, pos, format.DurationToFrames(1700*time.Millisecond))
}

// A rate other than 1 must not make the sink read behind the decoder: on an
// ffmpeg-backed source every such read is a process restart, audible as a
// stutter that outlasts the rate change. Skips without ffmpeg.
func TestNonUnityRateNeverRestartsTheDecoder(t *testing.T) {
	requireServer(t)
	if _, ok := extbin.Ffmpeg.Resolve(); !ok {
		t.Skip("ffmpeg not available")
	}
	dir := t.TempDir()
	flac := filepath.Join(dir, "tone.flac")
	// A quiet tone, so a run is not loud; lavfi's sine is a fraction of full scale.
	out, err := extbin.Ffmpeg.CombinedOutput(context.Background(), extbin.Opts{},
		"-y", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=6", "-ac", "2", "-ar", "48000", "-c:a", "flac", flac)
	require.NoError(t, err, string(out))
	src, err := decode.OpenFfmpeg(context.Background(), flac)
	require.NoError(t, err)
	defer func() { _ = src.Close() }()
	s, err := pulsesink.Open(src, pulsesink.Options{AppName: "boxer-test", Latency: 40 * time.Millisecond})
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()
	require.NoError(t, s.SetVolume(0.05))
	require.NoError(t, s.SetRate(1.31))
	s.Play()
	time.Sleep(700 * time.Millisecond)
	require.NoError(t, s.SetRate(1))
	time.Sleep(400 * time.Millisecond)
	s.Pause()
	require.Zero(t, src.Restarts(), "playback at any rate reads the decoder forwards only")
	require.False(t, s.Underflow())
}
