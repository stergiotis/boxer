//go:build wasip1

package browserhost

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The tab's HTTP: a wasip1 module has no sockets, so every outbound request
// crosses to the host as bytes and comes back as bytes (ADR-0077 SD9's
// "custom http host import"), against the page's origin, which is where a
// same-origin proxy to ClickHouse lives.
//
// Two ways across (ADR-0263 Update 2026-10-03). A request made on a goroutine
// other than the one running the current export — a query lane, a tile
// loader — is started with http_start and waited for off the render path:
// the host performs it with fetch() and wakes the module when it settles,
// and the goroutine polls http_ready between sleeps, so frames keep coming
// while it is out. A request on the export's own goroutine (setup, or a
// call made inside a frame) cannot wait that way — the browser runs the
// fetch only once the export returns, which it would not — so it keeps the
// synchronous http_fetch, which blocks the tab for its duration. A host
// without asynchronous HTTP answers http_start with 0, and the synchronous
// path is taken then too.
//
// Wire (little-endian u32 lengths): request = method, url, headers as
// "Name: value\n" lines, body. Response = status (0 = the host failed to
// perform it and the "headers" carry the message), headers, body.

//go:wasmimport env http_fetch
func hostHttpFetch(req unsafe.Pointer, n uint32) uint32

//go:wasmimport env http_take
func hostHttpTake(dst unsafe.Pointer, capacity uint32) uint32

// http_start begins a request and returns its handle, 0 when the host
// cannot. http_ready answers 0 while it is out and the reply's length once
// it is in; http_collect copies that reply out and forgets the handle;
// http_abort forgets it without a reply.
//
//go:wasmimport env http_start
func hostHttpStart(req unsafe.Pointer, n uint32) uint32

//go:wasmimport env http_ready
func hostHttpReady(handle uint32) uint32

//go:wasmimport env http_collect
func hostHttpCollect(handle uint32, dst unsafe.Pointer, capacity uint32) uint32

//go:wasmimport env http_abort
func hostHttpAbort(handle uint32)

// asyncPoll is how long a goroutine waiting on a request sleeps between
// asking the host; the host wakes the module when a request settles, so this
// bounds only how soon after that frame the goroutine notices.
const asyncPoll = 2 * time.Millisecond

// exportGoroutine is the goroutine running the current export (setup or
// frame); a request on it takes the synchronous path.
var exportGoroutine atomic.Uint64

// exportStartedNs is when the running export began, 0 between exports.
var exportStartedNs atomic.Int64

// exportStall is how long an export may run while a request waits before the
// wait looks at what the export's goroutine is doing, and how often it looks
// again. The browser finishes a fetch only once the export returns. An export
// that runs long is usually busy — other goroutines decode results inside a
// frame's yield rounds — and the wait goes on; but one whose goroutine is
// blocked (a channel, a lock) is, with near certainty, waiting for the very
// request in flight, and would wait forever. Only then is the request redone
// synchronously, which ends the wait; the first time, every goroutine's
// stack is logged so the blocking site can be found.
const exportStall = 250 * time.Millisecond

var stallLogged atomic.Bool

// beginExport records the calling goroutine as the export's and when it began;
// endExport marks the export finished.
func beginExport() {
	exportGoroutine.Store(goroutineID())
	exportStartedNs.Store(time.Now().UnixNano())
}

func endExport() { exportStartedNs.Store(0) }

// goroutineID reads the calling goroutine's id off its stack header
// ("goroutine N [...]"). Taken once per export and once per request.
func goroutineID() uint64 {
	var b [64]byte
	n := runtime.Stack(b[:], false)
	f := bytes.Fields(b[:n])
	if len(f) < 2 {
		return 0
	}
	id, _ := strconv.ParseUint(string(f[1]), 10, 64)
	return id
}

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

// exchange sends one framed request and returns the framed reply: started
// asynchronously and waited for when the caller is not the export's
// goroutine, synchronously otherwise.
func exchange(ctx context.Context, in []byte) (out []byte, err error) {
	if goroutineID() != exportGoroutine.Load() {
		if h := hostHttpStart(unsafe.Pointer(&in[0]), uint32(len(in))); h != 0 {
			var stalled bool
			out, stalled, err = await(ctx, h)
			if !stalled {
				return
			}
		}
	}
	return fetchSync(in)
}

// fetchSync performs the request with the host's synchronous call.
func fetchSync(in []byte) (out []byte, err error) {
	n := hostHttpFetch(unsafe.Pointer(&in[0]), uint32(len(in)))
	if n == 0 {
		return nil, eh.Errorf("host http: the host returned nothing")
	}
	out = make([]byte, n)
	out = out[:hostHttpTake(unsafe.Pointer(&out[0]), n)]
	return
}

// await waits for request h to settle, sleeping between polls so the frames
// that run while it is out are the ones that let the host finish it. stalled
// says the running export outlasted exportStall meanwhile: the request was
// abandoned, and the caller redoes it synchronously.
func await(ctx context.Context, h uint32) (out []byte, stalled bool, err error) {
	t := time.NewTimer(asyncPoll)
	defer t.Stop()
	var lastLook time.Time
	for {
		if n := hostHttpReady(h); n != 0 {
			out = make([]byte, n)
			out = out[:hostHttpCollect(h, unsafe.Pointer(&out[0]), n)]
			return
		}
		if s := exportStartedNs.Load(); s != 0 && time.Since(time.Unix(0, s)) > exportStall && time.Since(lastLook) > exportStall {
			lastLook = time.Now()
			buf := make([]byte, 1<<16)
			buf = buf[:runtime.Stack(buf, true)]
			if exportBlocked(buf, exportGoroutine.Load()) {
				hostHttpAbort(h)
				if stallLogged.CompareAndSwap(false, true) {
					log.Warn().Str("goroutines", string(buf)).
						Msg("host http: the export blocked while a request was in flight; redone synchronously (ADR-0263 Update 2026-10-03)")
				}
				return nil, true, nil
			}
		}
		select {
		case <-ctx.Done():
			hostHttpAbort(h)
			return nil, false, eh.Errorf("host http: %w", ctx.Err())
		case <-t.C:
			t.Reset(asyncPoll)
		}
	}
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
	out, err := exchange(req.Context(), in)
	if err != nil {
		return nil, err
	}
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
