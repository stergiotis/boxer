package hostaudio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink/lanesink"
)

func TestMessagesGoNowhereUntilBothEndsAreThere(t *testing.T) {
	Send([]byte{1})
	Deliver([]byte{2})

	var sent, got [][]byte
	l := Attach(func(msg []byte) { sent = append(sent, append([]byte(nil), msg...)) })
	release := SetReceiver(func(msg []byte) { got = append(got, append([]byte(nil), msg...)) })
	Send([]byte{3})
	Deliver([]byte{4})
	assert.Equal(t, [][]byte{{3}}, sent)
	assert.Equal(t, [][]byte{{4}}, got)

	l.Detach()
	release()
	Send([]byte{5})
	Deliver([]byte{6})
	assert.Len(t, sent, 1)
	assert.Len(t, got, 1)
}

// An end that goes away does not take a later one's place with it.
func TestALaterLaneOrReceiverIsNotRemovedByAnEarlierOnesRelease(t *testing.T) {
	var a, b int
	first := Attach(func([]byte) { a++ })
	second := Attach(func([]byte) { b++ })
	defer second.Detach()
	first.Detach()
	Send([]byte{1})
	assert.Equal(t, 0, a)
	assert.Equal(t, 1, b)

	var x, y int
	releaseFirst := SetReceiver(func([]byte) { x++ })
	releaseSecond := SetReceiver(func([]byte) { y++ })
	defer releaseSecond()
	releaseFirst()
	Deliver([]byte{1})
	assert.Equal(t, 0, x)
	assert.Equal(t, 1, y)
}

// A host asks whether it has a lane before it picks its device: none
// attached is no at once, a connected one is yes, and one the client never
// opens is no once the client has had its time.
func TestAwait(t *testing.T) {
	assert.False(t, Await(time.Second), "no lane")

	l := Attach(func([]byte) {})
	defer l.Detach()
	start := time.Now()
	assert.False(t, Await(60*time.Millisecond), "attached, never opened by the client")
	assert.GreaterOrEqual(t, time.Since(start), 40*time.Millisecond, "the client is given its time")
	start = time.Now()
	assert.False(t, Await(60*time.Millisecond))
	assert.Less(t, time.Since(start), 40*time.Millisecond, "and only once")

	late := Attach(func([]byte) {})
	defer late.Detach()
	go func() {
		time.Sleep(20 * time.Millisecond)
		late.Connected()
		late.Connected()
	}()
	assert.True(t, Await(5*time.Second), "connected while waited for")
	assert.True(t, Await(0))
}

// The device made here is on the process's lane in both directions: an
// announcement handed in reaches it, and what it sends leaves by the lane.
func TestNewLaneDeviceIsWiredBothWays(t *testing.T) {
	var kinds []lanesink.KindE
	l := Attach(func(msg []byte) {
		m, err := lanesink.Decode(msg)
		require.NoError(t, err)
		kinds = append(kinds, m.Kind)
	})
	defer l.Detach()
	var refused []error
	dev, closeDev := NewLaneDevice(lanesink.Options{Sync: true}, func(err error) { refused = append(refused, err) })

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

	// Closed, it is no longer where arrivals go.
	closeDev()
	Deliver([]byte{0x7f})
	assert.Len(t, refused, 1)
}
