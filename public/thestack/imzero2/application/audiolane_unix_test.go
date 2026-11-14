//go:build unix

package application

import (
	"bufio"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/hostaudio"
)

func TestAudioLaneCarriesMessagesBothWays(t *testing.T) {
	cmd := exec.Command("true")
	l, err := newAudioLane(cmd)
	require.NoError(t, err)
	require.NotNil(t, l)
	named := map[string]string{}
	for _, kv := range cmd.Env {
		for _, name := range []string{audioLaneToDeviceEnv, audioLaneFromDeviceEnv} {
			if v, ok := strings.CutPrefix(kv, name+"="); ok {
				named[name] = v
			}
		}
	}
	assert.Equal(t, l.down, named[audioLaneToDeviceEnv], "the client is told where both fifos are")
	assert.Equal(t, l.up, named[audioLaneFromDeviceEnv])

	got := make(chan []byte, 8)
	release := hostaudio.SetReceiver(func(msg []byte) { got <- append([]byte(nil), msg...) })
	defer release()
	l.start()
	assert.False(t, hostaudio.Await(0), "attached, and the client has not opened its end")

	// What the client does: read one, write the other.
	clientR, err := os.Open(l.down)
	require.NoError(t, err)
	clientW, err := os.OpenFile(l.up, os.O_WRONLY, 0)
	require.NoError(t, err)
	assert.True(t, hostaudio.Await(5*time.Second), "the client reads the lane: it has a carrier")

	hostaudio.Send([]byte{0x05})
	hostaudio.Send([]byte{0x04, 7, 0, 0, 0})
	r := bufio.NewReader(clientR)
	for _, want := range [][]byte{{0x05}, {0x04, 7, 0, 0, 0}} {
		var head [4]byte
		_, err = io.ReadFull(r, head[:])
		require.NoError(t, err)
		msg := make([]byte, binary.LittleEndian.Uint32(head[:]))
		_, err = io.ReadFull(r, msg)
		require.NoError(t, err)
		assert.Equal(t, want, msg)
	}

	announce := []byte{0x81, 0x02, 0x80, 0xbb, 0, 0}
	_, err = clientW.Write(append(binary.LittleEndian.AppendUint32(nil, uint32(len(announce))), announce...))
	require.NoError(t, err)
	select {
	case msg := <-got:
		assert.Equal(t, announce, msg)
	case <-time.After(5 * time.Second):
		t.Fatal("the client's message did not arrive")
	}

	// The client goes away: the device is told there is none.
	require.NoError(t, clientW.Close())
	select {
	case msg := <-got:
		assert.Equal(t, []byte{0x81, 0, 0, 0, 0, 0}, msg, "an absent device")
	case <-time.After(5 * time.Second):
		t.Fatal("the end of the client's stream was not reported")
	}
	require.NoError(t, clientR.Close())
	require.NoError(t, l.Close())
	_, err = os.Stat(l.dir)
	assert.True(t, os.IsNotExist(err), "the fifos are removed")
	require.NoError(t, l.Close(), "close is idempotent")
	assert.False(t, hostaudio.Await(0), "a closed lane is no lane")
}

// An oversized length from the client closes the lane instead of allocating
// what it names.
func TestAudioLaneRefusesAnOversizedMessage(t *testing.T) {
	l, err := newAudioLane(exec.Command("true"))
	require.NoError(t, err)
	got := make(chan []byte, 4)
	release := hostaudio.SetReceiver(func(msg []byte) { got <- append([]byte(nil), msg...) })
	defer release()
	l.start()
	defer func() { require.NoError(t, l.Close()) }()
	clientW, err := os.OpenFile(l.up, os.O_WRONLY, 0)
	require.NoError(t, err)
	defer func() { _ = clientW.Close() }()
	_, err = clientW.Write(binary.LittleEndian.AppendUint32(nil, audioLaneMaxMessage+1))
	require.NoError(t, err)
	select {
	case msg := <-got:
		assert.Equal(t, []byte{0x81, 0, 0, 0, 0, 0}, msg, "the lane is closed, so there is no device")
	case <-time.After(5 * time.Second):
		t.Fatal("the oversized message did not close the lane")
	}
}

// settled waits for the goroutine count to come back to at most want.
func settled(want int) (n int) {
	for range 200 {
		if n = runtime.NumGoroutine(); n <= want {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
	return n
}

// A client that never opens its ends, one that opens and never reads, and a
// lane closed as soon as it started all leave nothing behind: no call here
// waits in open, and Close closes what a goroutine waits on.
func TestAudioLaneCloseLeavesNoGoroutineBehind(t *testing.T) {
	before := runtime.NumGoroutine()

	for range 200 {
		l, err := newAudioLane(exec.Command("true"))
		require.NoError(t, err)
		l.start()
		require.NoError(t, l.Close())
	}
	assert.LessOrEqual(t, settled(before), before, "closed at once, two hundred times")

	l, err := newAudioLane(exec.Command("true"))
	require.NoError(t, err)
	l.start()
	time.Sleep(3 * audioLanePoll)
	require.NoError(t, l.Close())
	assert.LessOrEqual(t, settled(before), before, "the client never opened its ends")

	// A client that opens its end and never reads: the pipe and then the
	// queue fill, and the writer waits in a write.
	l, err = newAudioLane(exec.Command("true"))
	require.NoError(t, err)
	l.start()
	clientR, err := os.Open(l.down)
	require.NoError(t, err)
	require.True(t, hostaudio.Await(5*time.Second))
	chunk := make([]byte, 32<<10)
	for range audioLaneQueue + 16 {
		hostaudio.Send(chunk)
	}
	assert.Len(t, l.queue, audioLaneQueue, "what did not fit was dropped, not waited for")
	done := make(chan error, 1)
	go func() { done <- l.Close() }()
	select {
	case err = <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("close did not return")
	}
	assert.LessOrEqual(t, settled(before), before, "the client stopped reading")
	require.NoError(t, clientR.Close())
}
