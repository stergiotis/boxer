package hostaudio

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink/lanesink"
)

func TestMessagesGoNowhereUntilBothEndsAreThere(t *testing.T) {
	defer SetSender(nil)
	defer SetReceiver(nil)
	Send([]byte{1})
	Deliver([]byte{2})

	var sent, got [][]byte
	SetSender(func(msg []byte) { sent = append(sent, append([]byte(nil), msg...)) })
	SetReceiver(func(msg []byte) { got = append(got, append([]byte(nil), msg...)) })
	Send([]byte{3})
	Deliver([]byte{4})
	assert.Equal(t, [][]byte{{3}}, sent)
	assert.Equal(t, [][]byte{{4}}, got)

	SetSender(nil)
	SetReceiver(nil)
	Send([]byte{5})
	Deliver([]byte{6})
	assert.Len(t, sent, 1)
	assert.Len(t, got, 1)
}

// The device made here is on the process's lane in both directions: an
// announcement handed in reaches it, and what it sends leaves by the sender.
func TestNewLaneDeviceIsWiredBothWays(t *testing.T) {
	defer SetSender(nil)
	defer SetReceiver(nil)
	var kinds []lanesink.KindE
	SetSender(func(msg []byte) {
		m, err := lanesink.Decode(msg)
		require.NoError(t, err)
		kinds = append(kinds, m.Kind)
	})
	var refused []error
	dev := NewLaneDevice(lanesink.Options{Sync: true}, func(err error) { refused = append(refused, err) })
	defer dev.Close()

	src, err := pcm.NewSynthSource(pcm.Format{SampleRate: 48000, Channels: 1}, 48000, pcm.Silence())
	require.NoError(t, err)
	s, err := dev.OpenSink(src, "test")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	Deliver(lanesink.AppendDevice(nil, lanesink.DeviceRunning, 48000))
	state, rate := dev.State()
	assert.Equal(t, lanesink.DeviceRunning, state)
	assert.Equal(t, uint32(48000), rate)
	assert.Equal(t, []lanesink.KindE{lanesink.KindOpen}, kinds, "the open stream is opened on the announced device")

	Deliver([]byte{0x7f})
	assert.Len(t, refused, 1)
}
