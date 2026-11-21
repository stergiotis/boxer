package hostboot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/config/env/envtest"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audioout"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/thestack/imzero2/hostaudio"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

type namedDevice struct{ name string }

func (inst namedDevice) Name() string { return inst.name }
func (inst namedDevice) OpenSink(src pcm.SourceI, _ string) (sink.SinkI, error) {
	return sink.NewNull(src, nil), nil
}

// The audio-output service is opt-in, opens on the device the options name,
// and answers an app that declared the subject (ADR-0372 §SD2).
func TestBoot_AudioOutServesTheConfiguredDevice(t *testing.T) {
	opts := minimalOptions(t, newTestRegistry(t))
	opts.Services.AudioOut = true
	opts.AudioDevice = namedDevice{name: "fixture"}
	rt, err := Boot(context.Background(), opts)
	require.NoError(t, err)
	defer rt.Close(t.Context())

	require.NotNil(t, rt.AudioOut)
	assert.Equal(t, "fixture", rt.AudioOut.Device().Name())

	src, err := pcm.NewSynthSource(pcm.Format{SampleRate: 48000, Channels: 1}, 480, pcm.Silence())
	require.NoError(t, err)
	cli := rt.Bus.NewClient("hostboot.test.player", []app.SubjectFilter{
		{Pattern: audioout.SubjectOpen, Direction: app.CapDirectionPub, Reason: "test"},
	})
	s, st := audioout.Open(t.Context(), cli, src)
	require.NoError(t, s.Close())
	assert.True(t, st.Audible(), st.Reason)
	assert.Equal(t, "fixture", st.Device)
}

func TestBoot_AudioOutDefaultsToThePulseDevice(t *testing.T) {
	opts := minimalOptions(t, newTestRegistry(t))
	opts.Services.AudioOut = true
	rt, err := Boot(context.Background(), opts)
	require.NoError(t, err)
	defer rt.Close(t.Context())
	require.NotNil(t, rt.AudioOut)
	assert.Equal(t, audioout.PulseDevice{}.Name(), rt.AudioOut.Device().Name())
	assert.True(t, AllServices().AudioOut, "the carousel's hosts offer an output")
}

// With a carrier configured the device follows whether the process has an
// audio lane to it: the lane where the client opened its end, this machine's
// sound server where no lane exists.
func TestBoot_AudioOutFollowsTheLaneBehindACarrier(t *testing.T) {
	envtest.Set(t, imzero2env.HeadlessListen, "127.0.0.1:0")
	opts := minimalOptions(t, newTestRegistry(t))
	opts.Services.AudioOut = true
	rt, err := Boot(context.Background(), opts)
	require.NoError(t, err)
	defer rt.Close(t.Context())
	require.NotNil(t, rt.AudioOut)
	assert.Equal(t, "pulse", rt.AudioOut.Device().Name(), "no lane: FIFOs unavailable, or no client launched")

	lane := hostaudio.Attach(func([]byte) {})
	defer lane.Detach()
	lane.Connected()
	assert.Equal(t, "lane", rt.AudioOut.Device().Name(), "the client opened its end")

	src, err := pcm.NewSynthSource(pcm.Format{SampleRate: 48000, Channels: 1}, 480, pcm.Silence())
	require.NoError(t, err)
	cli := rt.Bus.NewClient("hostboot.test.player", []app.SubjectFilter{
		{Pattern: audioout.SubjectOpen, Direction: app.CapDirectionPub, Reason: "test"},
	})
	s, st := audioout.Open(t.Context(), cli, src)
	defer func() { require.NoError(t, s.Close()) }()
	assert.True(t, st.Audible(), st.Reason)
	assert.Equal(t, "lane", st.Device)
	yes, reason := audioout.Audible(s)
	assert.False(t, yes, "granted, and nobody listens yet")
	assert.Contains(t, reason, "no viewer")
}
