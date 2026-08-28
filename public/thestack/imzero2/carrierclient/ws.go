package carrierclient

import (
	"os"
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ws.go is a minimal RFC 6455 *client*, scoped to what the imzero2 remote
// carrier needs: the opening handshake, binary frames both ways, and the
// control frames a peer may send at any time (ping, close).
//
// Hand-rolled rather than taken as a dependency, unlike the protobuf on the
// other side of this package (ADR-0154 SD5). The two cases differ: the wire
// schema is a versioned contract this repo also generates Rust from, so a
// hand-written codec could silently drift from it, whereas RFC 6455 client
// framing has been frozen since 2011 and there is nothing to drift from. The
// scope is loopback — the carrier refuses a non-loopback bind without the auth
// and TLS of ADR-0082 — so the parts of the RFC that earn a library
// (permessage-deflate, TLS, proxies, streaming very large frames) are out.
//
// Not implemented, deliberately: extensions, fragmentation on send (every
// message goes out as one frame), and `wss://`.

const (
	opContinuation byte = 0x0
	opText         byte = 0x1
	opBinary       byte = 0x2
	opClose        byte = 0x8
	opPing         byte = 0x9
	opPong         byte = 0xA

	// wsGUID is the RFC 6455 §1.3 handshake constant. Pinned by
	// TestAcceptKeyMatchesRFCVector — a single wrong character here fails every
	// connection with an accept-key mismatch and looks like a server fault.
	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	// closeEchoTimeout bounds the courtesy close frame. See echoClose.
	closeEchoTimeout = 2 * time.Second

	// maxFrame bounds an inbound payload. A tree snapshot of a dense app is
	// tens of KB and a video access unit is smaller; 64 MiB is far above both
	// and keeps a malformed length from becoming an allocation.
	maxFrame = 64 << 20
)

// wsConn is one client connection. Not safe for concurrent use by multiple
// writers; [Client] serializes its own sends.
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader
}

// wsDial performs the opening handshake against a ws:// URL.
func wsDial(rawURL string, timeout time.Duration) (c *wsConn, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, eb.Build().Str("url", rawURL).Errorf("unable to parse websocket url: %w", err)
	}
	if u.Scheme != "ws" {
		// wss would need a TLS dial and certificate policy; the carrier is
		// loopback-only until ADR-0082 lands, so this stays a clear refusal
		// rather than a silent plaintext downgrade.
		return nil, eb.Build().Str("scheme", u.Scheme).Errorf("only ws:// is supported")
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":80"
	}
	conn, err := net.DialTimeout("tcp", host, timeout)
	if err != nil {
		return nil, eb.Build().Str("host", host).Errorf("unable to dial carrier: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = conn.Close()
		}
	}()

	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return nil, eh.Errorf("unable to generate websocket nonce: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(nonce[:])
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if err = conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, eh.Errorf("unable to set handshake deadline: %w", err)
	}
	if _, err = io.WriteString(conn, req); err != nil {
		return nil, eh.Errorf("unable to send websocket handshake: %w", err)
	}

	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		return nil, eh.Errorf("unable to read handshake response: %w", err)
	}
	if !strings.HasPrefix(status, "HTTP/1.1 101") {
		// The carrier serves the viewer page on the same port and answers a
		// non-upgrade GET with 200; say so, since pointing the client at the
		// page port instead of the socket is the likely mistake.
		return nil, eb.Build().Str("status", strings.TrimSpace(status)).
			Errorf("carrier did not upgrade the connection")
	}
	var accept string
	for {
		var line string
		line, err = br.ReadString('\n')
		if err != nil {
			return nil, eh.Errorf("unable to read handshake headers: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, found := strings.Cut(line, ":")
		if found && strings.EqualFold(strings.TrimSpace(name), "Sec-WebSocket-Accept") {
			accept = strings.TrimSpace(value)
		}
	}
	sum := sha1.Sum([]byte(key + wsGUID)) //nolint:gosec // RFC 6455 handshake, not a security primitive
	if want := base64.StdEncoding.EncodeToString(sum[:]); accept != want {
		return nil, eb.Build().Str("got", accept).Str("want", want).
			Errorf("websocket accept key mismatch")
	}
	if err = conn.SetDeadline(time.Time{}); err != nil {
		return nil, eh.Errorf("unable to clear handshake deadline: %w", err)
	}
	ok = true
	return &wsConn{conn: conn, br: br}, nil
}

// writeBinary sends one payload as a single masked binary frame. Client frames
// MUST be masked (RFC 6455 §5.3); the mask is not a security property, and the
// server rejects an unmasked frame.
func (inst *wsConn) writeBinary(payload []byte) (err error) {
	return inst.writeFrame(opBinary, payload)
}

func (inst *wsConn) writeFrame(opcode byte, payload []byte) (err error) {
	var head []byte
	head = append(head, 0x80|opcode) // FIN + opcode
	n := len(payload)
	switch {
	case n <= 125:
		head = append(head, 0x80|byte(n))
	case n <= 0xFFFF:
		head = append(head, 0x80|126)
		head = binary.BigEndian.AppendUint16(head, uint16(n))
	default:
		head = append(head, 0x80|127)
		head = binary.BigEndian.AppendUint64(head, uint64(n))
	}
	var mask [4]byte
	if _, err = rand.Read(mask[:]); err != nil {
		return eh.Errorf("unable to generate frame mask: %w", err)
	}
	head = append(head, mask[:]...)
	// One write for header and payload together: a frame is meaningless split,
	// and a single call keeps it that way on the wire whatever the transport
	// does with partial writes.
	frame := make([]byte, len(head)+n)
	copy(frame, head)
	for i := range n {
		frame[len(head)+i] = payload[i] ^ mask[i&3]
	}
	if _, err = inst.conn.Write(frame); err != nil {
		return eh.Errorf("unable to write frame: %w", err)
	}
	return nil
}

// idleTimeoutError marks a read deadline that expired between frames: nothing
// of the next frame was consumed, so the connection remains usable. It is a
// timeout for os.IsTimeout, so callers that treat any expiry as failure keep
// working, while Client.Idle can tell it from a mid-frame stall.
type idleTimeoutError struct{}

func (idleTimeoutError) Error() string   { return "read deadline expired between frames" }
func (idleTimeoutError) Timeout() bool   { return true }
func (idleTimeoutError) Temporary() bool { return true }

var errIdleTimeout error = idleTimeoutError{}

// readBinary returns the next binary message, answering ping frames and
// reassembling fragments on the way. Control frames the peer sends between
// fragments are handled where they arrive, as the RFC requires.
//
// A close frame surfaces as [io.EOF] so a caller's read loop ends the same way
// it would on a dropped connection.
func (inst *wsConn) readBinary(deadline time.Time) (payload []byte, err error) {
	if err = inst.conn.SetReadDeadline(deadline); err != nil {
		return nil, eh.Errorf("unable to set read deadline: %w", err)
	}
	var assembled []byte
	var assembling bool
	for {
		if !assembling {
			// Wait for the next frame without consuming it: a deadline that
			// expires here leaves the stream at a frame boundary, so the
			// connection is still usable — the case an idle pause relies on.
			// bufio keeps whatever it buffered across the failed Peek.
			if _, err = inst.br.Peek(2); err != nil {
				if os.IsTimeout(err) {
					return nil, errIdleTimeout
				}
				return nil, err // io.EOF passes through for the caller's loop
			}
		}
		var b [2]byte
		if _, err = io.ReadFull(inst.br, b[:]); err != nil {
			return nil, err // a timeout here is mid-frame: the stream is unusable
		}
		fin := b[0]&0x80 != 0
		opcode := b[0] & 0x0F
		masked := b[1]&0x80 != 0
		length := uint64(b[1] & 0x7F)
		switch length {
		case 126:
			var ext [2]byte
			if _, err = io.ReadFull(inst.br, ext[:]); err != nil {
				return nil, eh.Errorf("unable to read 16-bit frame length: %w", err)
			}
			length = uint64(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err = io.ReadFull(inst.br, ext[:]); err != nil {
				return nil, eh.Errorf("unable to read 64-bit frame length: %w", err)
			}
			length = binary.BigEndian.Uint64(ext[:])
		}
		if length > maxFrame {
			return nil, eb.Build().Uint64("length", length).Uint64("max", maxFrame).
				Errorf("inbound frame exceeds the size bound")
		}
		var mask [4]byte
		if masked {
			// A server MUST NOT mask, but unmasking is two lines and a
			// non-conforming peer is not worth a hard failure here.
			if _, err = io.ReadFull(inst.br, mask[:]); err != nil {
				return nil, eh.Errorf("unable to read frame mask: %w", err)
			}
		}
		buf := make([]byte, length)
		if _, err = io.ReadFull(inst.br, buf); err != nil {
			return nil, eh.Errorf("unable to read frame payload: %w", err)
		}
		if masked {
			for i := range buf {
				buf[i] ^= mask[i&3]
			}
		}

		switch opcode {
		case opPing:
			// Answer immediately; the carrier's heartbeat keeps an idle
			// connection alive and a missed pong would eventually drop us.
			if err = inst.writeFrame(opPong, buf); err != nil {
				return nil, err
			}
		case opPong:
			// Unsolicited pongs are legal and carry nothing we need.
		case opClose:
			// Echo the close, but never let it wedge the reader: a peer that
			// has stopped reading would otherwise block this write for as long
			// as its receive buffer stays full. The connection is finished
			// either way, so a failed courtesy echo changes nothing.
			inst.echoClose(buf)
			return nil, io.EOF
		case opBinary, opContinuation:
			if opcode == opBinary && assembling {
				return nil, eh.Errorf("new data frame arrived while a fragment was open")
			}
			assembled = append(assembled, buf...)
			assembling = !fin
			if fin {
				return assembled, nil
			}
		case opText:
			// The carrier speaks binary only; a text frame means we are
			// talking to something else.
			return nil, eb.Build().Str("text", truncate(string(buf), 120)).
				Errorf("unexpected text frame from the carrier")
		default:
			return nil, eb.Build().Int("opcode", int(opcode)).Errorf("unknown websocket opcode")
		}
	}
}

// echoClose sends a close frame under a short deadline, ignoring failure.
func (inst *wsConn) echoClose(payload []byte) {
	_ = inst.conn.SetWriteDeadline(time.Now().Add(closeEchoTimeout))
	_ = inst.writeFrame(opClose, payload)
	_ = inst.conn.SetWriteDeadline(time.Time{})
}

func (inst *wsConn) close() (err error) {
	inst.echoClose(nil)
	return inst.conn.Close()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
