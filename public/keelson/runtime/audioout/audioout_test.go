package audioout_test

import (
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audioout"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
)

// fakeDevice opens a null sink it can recognise, or fails.
type fakeDevice struct {
	err    error
	labels []string
	opened []sink.SinkI
}

func (inst *fakeDevice) Name() string { return "fake" }

func (inst *fakeDevice) OpenSink(src pcm.SourceI, label string) (sink.SinkI, error) {
	inst.labels = append(inst.labels, label)
	if inst.err != nil {
		return nil, inst.err
	}
	s := sink.NewNull(src, nil)
	inst.opened = append(inst.opened, s)
	return s, nil
}

func playerCaps() []app.SubjectFilter {
	return []app.SubjectFilter{
		{Pattern: audioout.SubjectOpen, Direction: app.CapDirectionPub, Reason: "test: play a recording"},
	}
}

func source(t *testing.T) pcm.SourceI {
	t.Helper()
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	src, err := pcm.NewSynthSource(format, 48000, pcm.Silence())
	require.NoError(t, err)
	return src
}

func served(t *testing.T, dev audioout.DeviceI) (bus *inprocbus.Inst, svc *audioout.Service) {
	t.Helper()
	bus = inprocbus.NewInst(zerolog.Nop())
	svc, err := audioout.NewService(bus, zerolog.Nop(), dev)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	return bus, svc
}

func TestOpenReturnsTheDevicesSink(t *testing.T) {
	dev := &fakeDevice{}
	bus, svc := served(t, dev)
	src := source(t)

	s, st := audioout.Open(bus.NewClient("test.player", playerCaps()), src)
	require.True(t, st.Audible(), st.Reason)
	require.Equal(t, "fake", st.Device)
	require.Len(t, dev.opened, 1)
	require.Same(t, dev.opened[0], s)
	require.Equal(t, []string{"test.player"}, dev.labels, "the device is told which app the stream is for")

	recs := svc.Records()
	require.Len(t, recs, 1)
	require.Equal(t, app.AppIdT("test.player"), recs[0].From)
	require.True(t, recs[0].Opened)
	require.Equal(t, audioout.OpenRequest{SampleRate: 48000, Channels: 2, Frames: 48000}, recs[0].Request)
}

// An app that did not declare the subject is refused by the bus before the
// service sees anything, and still gets a sink that moves a playhead.
func TestOpenWithoutTheCapabilityGetsTheNullSink(t *testing.T) {
	dev := &fakeDevice{}
	bus, svc := served(t, dev)

	s, st := audioout.Open(bus.NewClient("test.undeclared", nil), source(t))
	require.False(t, st.Audible())
	require.NotEmpty(t, st.Reason)
	require.Empty(t, st.Device)
	require.IsType(t, &sink.Null{}, s)
	require.Empty(t, dev.opened)
	require.Empty(t, svc.Records())
}

func TestOpenWithNoServiceGetsTheNullSink(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	s, st := audioout.Open(bus.NewClient("test.player", playerCaps()), source(t))
	require.False(t, st.Audible())
	require.IsType(t, &sink.Null{}, s)
}

func TestADeviceThatDoesNotOpenIsRecordedWithItsReason(t *testing.T) {
	dev := &fakeDevice{err: errors.New("no sound server")}
	bus, svc := served(t, dev)

	s, st := audioout.Open(bus.NewClient("test.player", playerCaps()), source(t))
	require.False(t, st.Audible())
	require.Contains(t, st.Reason, "no sound server")
	require.IsType(t, &sink.Null{}, s)
	recs := svc.Records()
	require.Len(t, recs, 1)
	require.False(t, recs[0].Opened)
	require.Equal(t, "no sound server", recs[0].Error)
}

func TestOpenWithoutASourceOrABus(t *testing.T) {
	s, st := audioout.Open(nil, source(t))
	require.False(t, st.Audible())
	require.NotNil(t, s)
	s, st = audioout.Open(inprocbus.NewInst(zerolog.Nop()).NewClient("test.player", playerCaps()), nil)
	require.False(t, st.Audible())
	require.NotNil(t, s)
}

// A token is good for one stream: asking the service directly and replaying
// its reply opens nothing more.
func TestEachOpenIsItsOwnGrant(t *testing.T) {
	dev := &fakeDevice{}
	bus, svc := served(t, dev)
	cli := bus.NewClient("test.player", playerCaps())
	a, _ := audioout.Open(cli, source(t))
	b, _ := audioout.Open(cli, source(t))
	require.NotSame(t, a, b)
	require.Len(t, dev.opened, 2)
	require.Len(t, svc.Records(), 2)
}

func TestNewServiceRefusesNil(t *testing.T) {
	_, err := audioout.NewService(nil, zerolog.Nop(), &fakeDevice{})
	require.Error(t, err)
	_, err = audioout.NewService(inprocbus.NewInst(zerolog.Nop()), zerolog.Nop(), nil)
	require.Error(t, err)
}

type mutedSink struct{ sink.SinkI }

func (mutedSink) Audible() (bool, string) { return false, "nobody is listening" }

// Audible asks a sink that knows; a device-less sink is silent, and a sink
// that does not say is taken to play.
func TestAudible(t *testing.T) {
	null := sink.NewNull(source(t), nil)
	yes, reason := audioout.Audible(null)
	require.False(t, yes)
	require.NotEmpty(t, reason)

	yes, reason = audioout.Audible(mutedSink{null})
	require.False(t, yes)
	require.Equal(t, "nobody is listening", reason)

	type plain struct{ sink.SinkI }
	yes, _ = audioout.Audible(plain{null})
	require.True(t, yes)
	yes, _ = audioout.Audible(nil)
	require.False(t, yes)
}

// latencyDevice is a fakeDevice that takes a request's latency.
type latencyDevice struct {
	fakeDevice
	latencies []time.Duration
}

func (inst *latencyDevice) OpenSinkWithLatency(src pcm.SourceI, label string, latency time.Duration) (sink.SinkI, error) {
	inst.latencies = append(inst.latencies, latency)
	return inst.OpenSink(src, label)
}

// A latency the app names reaches a device that can take it and is recorded;
// a device that cannot is opened as it always is.
func TestOpenWithALatency(t *testing.T) {
	dev := &latencyDevice{}
	bus, svc := served(t, dev)
	client := bus.NewClient("test.player", playerCaps())

	_, st := audioout.OpenWith(client, source(t), audioout.Options{Latency: 300 * time.Millisecond})
	require.True(t, st.Audible(), st.Reason)
	require.Equal(t, []time.Duration{300 * time.Millisecond}, dev.latencies)
	require.Equal(t, 300*time.Millisecond, svc.Records()[0].Request.Latency)

	_, st = audioout.Open(client, source(t))
	require.True(t, st.Audible(), st.Reason)
	require.Len(t, dev.latencies, 1, "no latency asked, the plain open")
	require.Len(t, dev.opened, 2)

	plain := &fakeDevice{}
	bus, _ = served(t, plain)
	_, st = audioout.OpenWith(bus.NewClient("test.player", playerCaps()), source(t), audioout.Options{Latency: time.Second})
	require.True(t, st.Audible(), st.Reason)
	require.Len(t, plain.opened, 1)
}
