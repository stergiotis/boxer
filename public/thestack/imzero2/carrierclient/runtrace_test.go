package carrierclient

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// fakeCarrier answers every tree request on server with snap until the pipe
// closes. It is lenient where the codec tests are strict: it runs past the end
// of the test, when a failed read only means the client hung up.
func fakeCarrier(t *testing.T, server net.Conn, snap *TreeSnapshot) {
	t.Helper()
	reply, err := proto.Marshal(&SessionControl{
		Control: &SessionControl_TreeSnapshot{TreeSnapshot: snap},
	})
	require.NoError(t, err)
	reply = append([]byte{prefixSession}, reply...)
	go func() {
		br := bufio.NewReader(server)
		for {
			payload, e := readMaskedFrame(br)
			if e != nil {
				return
			}
			if len(payload) == 0 || payload[0] != prefixSession {
				continue
			}
			ctl := &SessionControl{}
			if proto.Unmarshal(payload[1:], ctl) != nil {
				return
			}
			if _, ok := ctl.GetControl().(*SessionControl_TreeRequest); !ok {
				continue
			}
			head := []byte{0x80 | opBinary}
			if n := len(reply); n <= 125 {
				head = append(head, byte(n))
			} else {
				head = append(head, 126)
				head = binary.BigEndian.AppendUint16(head, uint16(n))
			}
			if _, e = server.Write(append(head, reply...)); e != nil {
				return
			}
		}
	}()
}

func readMaskedFrame(r *bufio.Reader) (payload []byte, err error) {
	var b [2]byte
	if _, err = io.ReadFull(r, b[:]); err != nil {
		return nil, err
	}
	length := uint64(b[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	var mask [4]byte
	if _, err = io.ReadFull(r, mask[:]); err != nil {
		return nil, err
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i&3]
	}
	return payload, nil
}

func TestRunTraceSettlesAfterWaitAndRead(t *testing.T) {
	// settleMs applies to any step: a wait or read that succeeds at once still
	// pauses for what the frame is doing before the next step runs.
	ws, server := pipeConn(t)
	fakeCarrier(t, server, &TreeSnapshot{Nodes: []*TreeNode{
		{Id: 1, Role: "button", Name: "Run"},
		{Id: 2, Role: "label", Value: "rows 42"},
	}})
	c := &Client{ws: ws, log: zerolog.Nop()}
	for _, st := range []Step{
		{Do: "wait", Name: "Run", SettleMs: 300},
		{Do: "read", Role: "label", ValueContains: "rows", Pattern: `rows (?P<n>\d+)`, SettleMs: 300},
	} {
		start := time.Now()
		require.NoError(t, RunTrace(c, []Step{st}, RunOptions{Timeout: 2 * time.Second, Logger: zerolog.Nop()}))
		assert.GreaterOrEqual(t, time.Since(start), 300*time.Millisecond, st.Do)
	}
}

func TestRunTraceDryRunSkipsWait(t *testing.T) {
	// A dry run sends no input, so a wait on what that input would have
	// produced must not poll until the timeout and fail the run.
	ws, server := pipeConn(t)
	fakeCarrier(t, server, &TreeSnapshot{Nodes: []*TreeNode{{Id: 1, Role: "button", Name: "Run"}}})
	c := &Client{ws: ws, log: zerolog.Nop()}
	start := time.Now()
	err := RunTrace(c, []Step{{Do: "wait", Name: "Result"}}, RunOptions{
		DryRun: true, Timeout: time.Second, Logger: zerolog.Nop(),
	})
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 500*time.Millisecond)
}
