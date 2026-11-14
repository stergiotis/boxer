package hostboot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audioout"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

type namedDevice struct{ name string }

func (inst namedDevice) Name() string { return inst.name }
func (inst namedDevice) OpenSink(src pcm.SourceI, _ string) (sink.SinkI, error) {
	return sink.NewNull(src, nil), nil
}

// The audio-output service is opt-in, opens on the device the options name,
// and answers an app that declared the subject (ADR-0372 §SD2, proposed).
func TestBoot_AudioOutServesTheConfiguredDevice(t *testing.T) {
	opts := minimalOptions(t, newTestRegistry(t))
	opts.Services.AudioOut = true
	opts.AudioDevice = namedDevice{name: "fixture"}
	rt, err := Boot(context.Background(), opts)
	require.NoError(t, err)
	defer rt.Close()

	require.NotNil(t, rt.AudioOut)
	assert.Equal(t, "fixture", rt.AudioOut.Device().Name())

	src, err := pcm.NewSynthSource(pcm.Format{SampleRate: 48000, Channels: 1}, 480, pcm.Silence())
	require.NoError(t, err)
	cli := rt.Bus.NewClient("hostboot.test.player", []app.SubjectFilter{
		{Pattern: audioout.SubjectOpen, Direction: app.CapDirectionPub, Reason: "test"},
	})
	s, st := audioout.Open(cli, src)
	require.NoError(t, s.Close())
	assert.True(t, st.Audible(), st.Reason)
	assert.Equal(t, "fixture", st.Device)
}

func TestBoot_AudioOutDefaultsToThePulseDevice(t *testing.T) {
	opts := minimalOptions(t, newTestRegistry(t))
	opts.Services.AudioOut = true
	rt, err := Boot(context.Background(), opts)
	require.NoError(t, err)
	defer rt.Close()
	require.NotNil(t, rt.AudioOut)
	assert.Equal(t, audioout.PulseDevice{}.Name(), rt.AudioOut.Device().Name())
	assert.True(t, AllServices().AudioOut, "the carousel's hosts offer an output")
}

// With a remote carrier configured the person is at a viewer's page, so the
// default device is the audio lane rather than this machine's sound server.
func TestBoot_AudioOutDefaultsToTheLaneBehindACarrier(t *testing.T) {
	imzero2env.HeadlessListen.SetForTest(t, "127.0.0.1:0")
	opts := minimalOptions(t, newTestRegistry(t))
	opts.Services.AudioOut = true
	rt, err := Boot(context.Background(), opts)
	require.NoError(t, err)
	defer rt.Close()
	require.NotNil(t, rt.AudioOut)
	assert.Equal(t, "lane", rt.AudioOut.Device().Name())
}
