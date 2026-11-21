//go:build integration

package audioout_test

import (
	"testing"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/audioout"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
)

// The whole route an app takes on a desktop host: the bus request, the
// grant, and a stream on the sound server. It needs a reachable PulseAudio
// (or PipeWire pulse) server and skips without one; the source is silence,
// so a run makes no sound.
func TestOpenPlaysThroughThePulseDevice(t *testing.T) {
	probe, err := pulse.NewClient(pulse.ClientApplicationName("boxer-test-probe"))
	if err != nil {
		t.Skipf("no audio server reachable: %v", err)
	}
	probe.Close()

	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := audioout.NewService(bus, zerolog.Nop(), audioout.PulseDevice{})
	require.NoError(t, err)
	defer svc.Close()

	format := pcm.Format{SampleRate: 48000, Channels: 2}
	src, err := pcm.NewSynthSource(format, format.DurationToFrames(5*time.Second), pcm.Silence())
	require.NoError(t, err)
	s, st := audioout.Open(t.Context(), bus.NewClient("test.player", playerCaps()), src)
	defer func() { require.NoError(t, s.Close()) }()
	require.True(t, st.Audible(), st.Reason)
	require.Equal(t, "pulse", st.Device)

	s.Play()
	require.Equal(t, sink.StatePlaying, s.State())
	time.Sleep(600 * time.Millisecond)
	pos := s.Position()
	require.Greater(t, pos, format.DurationToFrames(300*time.Millisecond), "position did not advance")
	require.Less(t, pos, format.DurationToFrames(900*time.Millisecond), "position ran ahead of the clock")
	require.True(t, svc.Records()[0].Opened)
}
