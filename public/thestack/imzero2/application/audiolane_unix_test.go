//go:build unix

package application

import (
	"bufio"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/hostaudio"
)

func TestAudioLaneCarriesMessagesBothWays(t *testing.T) {
	defer hostaudio.SetReceiver(nil)
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
	hostaudio.SetReceiver(func(msg []byte) { got <- append([]byte(nil), msg...) })
	l.start()

	// What the client does: read one, write the other.
	clientR, err := os.Open(l.down)
	require.NoError(t, err)
	clientW, err := os.OpenFile(l.up, os.O_WRONLY, 0)
	require.NoError(t, err)

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
}

// A client that never opens its ends leaves two goroutines waiting; Close
// releases them.
func TestAudioLaneCloseReleasesAnUnopenedLane(t *testing.T) {
	l, err := newAudioLane(exec.Command("true"))
	require.NoError(t, err)
	l.start()
	time.Sleep(20 * time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- l.Close() }()
	select {
	case err = <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("close did not return")
	}
}
