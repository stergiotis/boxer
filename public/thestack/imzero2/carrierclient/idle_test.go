package carrierclient

import (
	"bufio"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdleAnswersPingsAndReturnsWhenTheTimeIsUp(t *testing.T) {
	// A trace's `sleep` must keep servicing the socket: the carrier pings an
	// idle peer and reaps one whose pong never comes.
	ws, server := pipeConn(t)
	c := &Client{ws: ws, log: zerolog.Nop()}
	go func() {
		time.Sleep(50 * time.Millisecond)
		serverFrame(t, server, true, opPing, []byte("hb"))
	}()
	pong := make(chan string, 1)
	go func() {
		br := bufio.NewReader(server)
		opcode, echoed := readClientFrame(t, br)
		assert.Equal(t, opPong, opcode)
		pong <- string(echoed)
	}()
	start := time.Now()
	require.NoError(t, c.Idle(300*time.Millisecond))
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, 300*time.Millisecond, "idles for the whole duration")
	assert.Less(t, elapsed, 2*time.Second)
	assert.Equal(t, "hb", <-pong, "the ping was answered during the pause")
}

func TestIdleReportsAClosedConnection(t *testing.T) {
	ws, server := pipeConn(t)
	c := &Client{ws: ws, log: zerolog.Nop()}
	go serverFrame(t, server, true, opClose, nil)
	err := c.Idle(time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestReadBinaryTimeoutBetweenFramesLeavesTheStreamUsable(t *testing.T) {
	// The idle path relies on an expiry between frames consuming nothing: the
	// next read must still parse the next frame from its first byte.
	ws, server := pipeConn(t)
	_, err := ws.readBinary(time.Now().Add(50 * time.Millisecond))
	require.ErrorIs(t, err, errIdleTimeout)
	assert.True(t, os.IsTimeout(err), "still a timeout for callers that treat any expiry as failure")
	go serverFrame(t, server, true, opBinary, []byte("after"))
	got, err := ws.readBinary(time.Now().Add(2 * time.Second))
	require.NoError(t, err)
	assert.Equal(t, "after", string(got))
}

func TestSettleBeforeOnlyForCapture(t *testing.T) {
	assert.True(t, settleBefore("capture"))
	for _, do := range []string{"click", "sleep", "wait", "note", "type", "drag"} {
		assert.False(t, settleBefore(do), do)
	}
}
