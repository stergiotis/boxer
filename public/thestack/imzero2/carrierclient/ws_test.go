package carrierclient

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The frame codec is the one hand-written piece of this package (ADR-0154 SD5
// takes the protobuf generated and the WebSocket by hand), so it carries the
// tests. Each case drives a real [wsConn] over an in-memory pipe against a
// hand-built server side, so the bytes on the wire are what is asserted rather
// than a round-trip through our own encoder.

// serverFrame writes one *unmasked* frame, as a server must (RFC 6455 §5.1).
func serverFrame(t *testing.T, w io.Writer, fin bool, opcode byte, payload []byte) {
	t.Helper()
	head := []byte{opcode}
	if fin {
		head[0] |= 0x80
	}
	n := len(payload)
	switch {
	case n <= 125:
		head = append(head, byte(n))
	case n <= 0xFFFF:
		head = append(head, 126)
		head = binary.BigEndian.AppendUint16(head, uint16(n))
	default:
		head = append(head, 127)
		head = binary.BigEndian.AppendUint64(head, uint64(n))
	}
	_, err := w.Write(append(head, payload...))
	require.NoError(t, err)
}

// readClientFrame parses one frame the client wrote, undoing the mask.
func readClientFrame(t *testing.T, r *bufio.Reader) (opcode byte, payload []byte) {
	t.Helper()
	var b [2]byte
	_, err := io.ReadFull(r, b[:])
	require.NoError(t, err)
	opcode = b[0] & 0x0F
	require.NotZero(t, b[1]&0x80, "client frames must be masked")
	length := uint64(b[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		_, err = io.ReadFull(r, ext[:])
		require.NoError(t, err)
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		_, err = io.ReadFull(r, ext[:])
		require.NoError(t, err)
		length = binary.BigEndian.Uint64(ext[:])
	}
	var mask [4]byte
	_, err = io.ReadFull(r, mask[:])
	require.NoError(t, err)
	payload = make([]byte, length)
	_, err = io.ReadFull(r, payload)
	require.NoError(t, err)
	for i := range payload {
		payload[i] ^= mask[i&3]
	}
	return opcode, payload
}

func pipeConn(t *testing.T) (client *wsConn, server net.Conn) {
	t.Helper()
	c, s := net.Pipe()
	t.Cleanup(func() { _ = c.Close(); _ = s.Close() })
	return &wsConn{conn: c, br: bufio.NewReader(c)}, s
}

func TestWriteBinaryMasksAndSizesFrames(t *testing.T) {
	// The three length encodings, since the boundary arithmetic is the easiest
	// thing to get wrong and a wrong length desynchronizes the whole stream.
	for _, size := range []int{0, 125, 126, 0xFFFF, 0x10000} {
		client, server := pipeConn(t)
		payload := make([]byte, size)
		for i := range payload {
			payload[i] = byte(i)
		}
		wrote := make(chan error, 1)
		go func() { wrote <- client.writeBinary(payload) }()

		br := bufio.NewReader(server)
		opcode, got := readClientFrame(t, br)
		assert.Equal(t, opBinary, opcode, "size %d", size)
		if size == 0 {
			assert.Empty(t, got, "zero-length frame carries no payload")
		} else {
			assert.Equal(t, payload, got, "size %d round-trips", size)
		}
		// Join the writer before the pipe is torn down, so a late failure is
		// reported against this test rather than panicking the run.
		require.NoError(t, <-wrote, "size %d", size)
	}
}

func TestReadBinaryReassemblesFragments(t *testing.T) {
	client, server := pipeConn(t)
	go func() {
		serverFrame(t, server, false, opBinary, []byte("head"))
		serverFrame(t, server, false, opContinuation, []byte("-mid"))
		serverFrame(t, server, true, opContinuation, []byte("-tail"))
	}()
	got, err := client.readBinary(time.Now().Add(2 * time.Second))
	require.NoError(t, err)
	assert.Equal(t, "head-mid-tail", string(got))
}

func TestReadBinaryAnswersPingAndKeepsReading(t *testing.T) {
	// A ping between data frames must be answered inline: the carrier's
	// heartbeat keeps an idle connection alive, and a driver that sits waiting
	// for a tree would otherwise be dropped mid-wait.
	client, server := pipeConn(t)
	go func() {
		serverFrame(t, server, true, opPing, []byte("hb"))
		serverFrame(t, server, true, opBinary, []byte("payload"))
	}()
	done := make(chan []byte, 1)
	go func() {
		got, err := client.readBinary(time.Now().Add(2 * time.Second))
		assert.NoError(t, err)
		done <- got
	}()
	br := bufio.NewReader(server)
	opcode, echoed := readClientFrame(t, br)
	assert.Equal(t, opPong, opcode, "ping is answered with a pong")
	assert.Equal(t, "hb", string(echoed), "the pong echoes the ping payload")
	assert.Equal(t, "payload", string(<-done))
}

func TestReadBinaryReportsCloseAsEOF(t *testing.T) {
	// Surfacing close as io.EOF lets a caller's read loop end the same way on a
	// polite close and on a dropped connection.
	client, server := pipeConn(t)
	go func() {
		serverFrame(t, server, true, opClose, nil)
		// Drain the courtesy echo. Without a reader here the echo would sit
		// until its deadline — which is the behaviour echoClose exists to
		// bound, and is asserted separately below.
		opcode, _ := readClientFrame(t, bufio.NewReader(server))
		assert.Equal(t, opClose, opcode)
	}()
	_, err := client.readBinary(time.Now().Add(2 * time.Second))
	assert.ErrorIs(t, err, io.EOF)
}

func TestReadBinaryRejectsOversizedFrame(t *testing.T) {
	// A malformed length must fail rather than become an allocation.
	client, server := pipeConn(t)
	go func() {
		head := []byte{0x80 | opBinary, 127}
		head = binary.BigEndian.AppendUint64(head, maxFrame+1)
		_, err := server.Write(head)
		assert.NoError(t, err)
	}()
	_, err := client.readBinary(time.Now().Add(2 * time.Second))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds the size bound")
}

func TestReadBinaryRejectsTextFrame(t *testing.T) {
	// The carrier speaks binary only; a text frame means we reached something
	// else — most likely the viewer page's HTTP port.
	client, server := pipeConn(t)
	go func() { serverFrame(t, server, true, opText, []byte("hello")) }()
	_, err := client.readBinary(time.Now().Add(2 * time.Second))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected text frame")
}

func TestCloseEchoDoesNotWedgeTheReader(t *testing.T) {
	// A peer that sends close and then stops reading must not be able to block
	// us indefinitely: readBinary still returns EOF, bounded by echoClose's
	// write deadline rather than waiting on the peer.
	client, server := pipeConn(t)
	go func() { serverFrame(t, server, true, opClose, nil) }()
	start := time.Now()
	_, err := client.readBinary(time.Now().Add(30 * time.Second))
	assert.ErrorIs(t, err, io.EOF)
	assert.Less(t, time.Since(start), closeEchoTimeout*2,
		"the unread echo is bounded, not waited on")
}

func TestAcceptKeyMatchesRFCVector(t *testing.T) {
	// RFC 6455 §1.3's worked example. This pins the magic GUID: a wrong
	// character in it fails every handshake with a mismatch that reads like a
	// server fault, which is exactly how it was first hit here.
	sum := sha1.Sum([]byte("dGhlIHNhbXBsZSBub25jZQ==" + wsGUID))
	assert.Equal(t, "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=", base64.StdEncoding.EncodeToString(sum[:]))
}

func TestDialRejectsNonWsScheme(t *testing.T) {
	_, err := wsDial("wss://127.0.0.1:8089/", time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only ws:// is supported")
}
