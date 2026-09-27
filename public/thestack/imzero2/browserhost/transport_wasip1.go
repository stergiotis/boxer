//go:build wasip1

package browserhost

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unsafe"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The tab's HTTP: a wasip1 module has no sockets, so every outbound request
// crosses to the host as bytes and comes back as bytes (ADR-0077 SD9's
// "custom http host import"). The host — the browser worker — performs it
// synchronously against the page's origin, which is where a same-origin
// proxy to ClickHouse lives; a Node host answers "no HTTP here".
//
// Wire (little-endian u32 lengths): request = method, url, headers as
// "Name: value\n" lines, body. Response = status (0 = the host failed to
// perform it and the "headers" carry the message), headers, body.

//go:wasmimport env http_fetch
func hostHttpFetch(req unsafe.Pointer, n uint32) uint32

//go:wasmimport env http_take
func hostHttpTake(dst unsafe.Pointer, capacity uint32) uint32

type hostTransport struct{}

var _ http.RoundTripper = hostTransport{}

// InstallHostTransport makes the host import the process's default HTTP
// transport. It is a process-wide swap, taken deliberately and once by the
// tab binary at start: chclient, play's client and the egress service set
// no Transport of their own, so the default is the one seam through which
// every request of theirs leaves the module. A client that did set a
// Transport would dial and fail, since the module has no sockets.
func InstallHostTransport() {
	http.DefaultTransport = hostTransport{}
}

func putBytes(w *bytes.Buffer, b []byte) {
	var l [4]byte
	binary.LittleEndian.PutUint32(l[:], uint32(len(b)))
	w.Write(l[:])
	w.Write(b)
}

func takeBytes(b []byte) (field []byte, rest []byte, ok bool) {
	if len(b) < 4 {
		return
	}
	n := binary.LittleEndian.Uint32(b[:4])
	if uint32(len(b)-4) < n {
		return
	}
	return b[4 : 4+n], b[4+n:], true
}

func (hostTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	var body []byte
	if req.Body != nil {
		body, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, eh.Errorf("host http: read request body: %w", err)
		}
	}
	var hdr strings.Builder
	for k, vs := range req.Header {
		for _, v := range vs {
			hdr.WriteString(k)
			hdr.WriteString(": ")
			hdr.WriteString(v)
			hdr.WriteByte('\n')
		}
	}
	var frame bytes.Buffer
	putBytes(&frame, []byte(req.Method))
	putBytes(&frame, []byte(req.URL.String()))
	putBytes(&frame, []byte(hdr.String()))
	putBytes(&frame, body)
	in := frame.Bytes()
	n := hostHttpFetch(unsafe.Pointer(&in[0]), uint32(len(in)))
	if n == 0 {
		return nil, eh.Errorf("host http: the host returned nothing")
	}
	out := make([]byte, n)
	got := hostHttpTake(unsafe.Pointer(&out[0]), n)
	out = out[:got]
	if len(out) < 4 {
		return nil, eh.Errorf("host http: short response frame")
	}
	status := binary.LittleEndian.Uint32(out[:4])
	headers, rest, ok := takeBytes(out[4:])
	if !ok {
		return nil, eh.Errorf("host http: malformed response headers")
	}
	rbody, _, ok := takeBytes(rest)
	if !ok {
		return nil, eh.Errorf("host http: malformed response body")
	}
	if status == 0 {
		return nil, eb.Build().Str("url", req.URL.String()).Str("host", string(headers)).Errorf("host http: request failed")
	}
	resp = &http.Response{
		Status:        strconv.Itoa(int(status)) + " " + http.StatusText(int(status)),
		StatusCode:    int(status),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        make(http.Header),
		Body:          io.NopCloser(bytes.NewReader(rbody)),
		ContentLength: int64(len(rbody)),
		Request:       req,
	}
	for line := range strings.SplitSeq(string(headers), "\n") {
		k, v, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		resp.Header.Add(strings.TrimSpace(k), strings.TrimSpace(v))
	}
	return
}
