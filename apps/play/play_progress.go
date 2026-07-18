package play

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// play_progress.go is ADR-0115 plane A / S3: live query progress from
// ClickHouse's in-band HTTP progress headers. With
// send_progress_in_http_headers=1 the server streams
// `X-ClickHouse-Progress: {...}` lines INSIDE the still-open
// response-header block, one every http_headers_progress_interval_ms,
// and only then finishes the block and starts the body. Go's net/http
// delivers headers as one completed block, so the stock client would
// surface every tick at once — after the query finished. This transport
// reads the header section line by line off the raw connection, firing
// the sink per progress line while the query still runs, then hands the
// remaining stream to the caller as an ordinary *http.Response.
//
// Scope: plain http:// endpoints (play's ClickHouse endpoints — local
// servers and the loopback introspection plane). Anything else keeps
// the stock client and degrades to completion-time delivery; progress
// is transient glass state, so degradation loses nothing durable.

const (
	// progressIntervalMs is the server-side tick spacing requested for
	// progress headers. 250 ms reads as live without header spam on
	// minute-long queries (~240 lines/min).
	progressIntervalMs = 250
	// progressDialTimeout bounds the TCP connect; established connections
	// are governed by the caller's ctx (the lane timeout).
	progressDialTimeout = 5 * time.Second
	// progressMaxHeaderBytes caps the incrementally-read header section —
	// the same order as net/http's default — so a misbehaving server
	// cannot grow the buffer unboundedly.
	progressMaxHeaderBytes = 1 << 20
)

// progressHeaderKey is X-ClickHouse-Progress in the canonical form
// textproto normalises header keys to.
var progressHeaderKey = textproto.CanonicalMIMEHeaderKey("X-ClickHouse-Progress")

// newProgressClient returns an *http.Client whose transport surfaces
// in-band progress headers live, or nil when rawURL is not a plain
// http:// endpoint (the caller then keeps its stock client).
func newProgressClient(rawURL string, onProgress func(Summary)) *http.Client {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "http" || u.Host == "" {
		return nil
	}
	return &http.Client{Transport: &progressTransport{onProgress: onProgress}}
}

// progressTransport is the incremental-header RoundTripper. One request
// per connection (it writes `Connection: close`): the query path builds
// a fresh client per run, and ClickHouse queries are heavyweight enough
// that keep-alive would buy noise, not latency.
type progressTransport struct {
	onProgress func(Summary)
}

func (inst *progressTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	host := req.URL.Host
	if req.URL.Port() == "" {
		host = net.JoinHostPort(host, "80")
	}
	conn, err := net.DialTimeout("tcp", host, progressDialTimeout)
	if err != nil {
		err = eh.Errorf("progress transport: dial %q: %w", host, err)
		return
	}
	// The ctx watcher closes the connection on cancellation — mid-header
	// or mid-body — which fails the blocking read; done retires it when
	// the body is closed.
	done := make(chan struct{})
	go func() {
		select {
		case <-req.Context().Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	closeAll := func() {
		_ = conn.Close()
		close(done)
	}

	err = writeRequest(conn, req)
	if err != nil {
		closeAll()
		err = eh.Errorf("progress transport: write request: %w", err)
		return
	}

	br := bufio.NewReader(conn)
	statusCode, status, header, err := readHeaderStreaming(br, inst.onProgress)
	if err != nil {
		closeAll()
		if ctxErr := req.Context().Err(); ctxErr != nil {
			err = ctxErr
		} else {
			err = eh.Errorf("progress transport: read response: %w", err)
		}
		return
	}

	// Hand the remaining stream over as the body: we already own the
	// framing, so decode it here and present a plain reader. The header
	// keeps X-ClickHouse-Summary and friends; Transfer-Encoding is
	// consumed by the decode and dropped.
	var bodyReader io.Reader
	switch {
	case strings.EqualFold(header.Get("Transfer-Encoding"), "chunked"):
		header.Del("Transfer-Encoding")
		bodyReader = httputil.NewChunkedReader(br)
	case header.Get("Content-Length") != "":
		n, pErr := strconv.ParseInt(header.Get("Content-Length"), 10, 64)
		if pErr != nil {
			closeAll()
			err = eh.Errorf("progress transport: bad content-length %q", header.Get("Content-Length"))
			return
		}
		bodyReader = io.LimitReader(br, n)
	default:
		// Connection: close semantics — the body runs to EOF.
		bodyReader = br
	}
	resp = &http.Response{
		Status:        status,
		StatusCode:    statusCode,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          &connBody{r: bodyReader, close: closeAll},
		ContentLength: -1,
		Request:       req,
	}
	return
}

// writeRequest emits the HTTP/1.1 request by hand: request line, Host,
// the caller's headers, framing, and the (small — it is SQL text) body.
func writeRequest(w io.Writer, req *http.Request) (err error) {
	var body []byte
	if req.Body != nil {
		body, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s HTTP/1.1\r\n", req.Method, req.URL.RequestURI())
	fmt.Fprintf(&b, "Host: %s\r\n", req.URL.Host)
	for k, vs := range req.Header {
		for _, v := range vs {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	fmt.Fprintf(&b, "Content-Length: %d\r\n", len(body))
	b.WriteString("Connection: close\r\n\r\n")
	if _, err = io.WriteString(w, b.String()); err != nil {
		return
	}
	_, err = w.Write(body)
	return
}

// readHeaderStreaming parses the status line, then consumes header lines
// one at a time as the server emits them, firing onProgress for each
// X-ClickHouse-Progress value — this mid-block delivery is the entire
// point of the hand-rolled transport.
func readHeaderStreaming(br *bufio.Reader, onProgress func(Summary)) (statusCode int, status string, header http.Header, err error) {
	statusLine, err := readHeaderLine(br)
	if err != nil {
		return
	}
	proto, rest, ok := strings.Cut(statusLine, " ")
	if !ok || !strings.HasPrefix(proto, "HTTP/1.") {
		err = eh.Errorf("malformed status line %q", statusLine)
		return
	}
	codeStr, _, _ := strings.Cut(rest, " ")
	statusCode, err = strconv.Atoi(codeStr)
	if err != nil {
		err = eh.Errorf("malformed status code in %q", statusLine)
		return
	}
	status = rest

	header = make(http.Header, 16)
	total := len(statusLine)
	for {
		var line string
		line, err = readHeaderLine(br)
		if err != nil {
			return
		}
		if line == "" {
			return
		}
		total += len(line)
		if total > progressMaxHeaderBytes {
			err = eh.Errorf("response header section exceeds %d bytes", progressMaxHeaderBytes)
			return
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			err = eh.Errorf("malformed header line %q", line)
			return
		}
		key = textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == progressHeaderKey {
			if onProgress != nil {
				onProgress(parseSummaryHeader(value))
			}
			continue // ticks are consumed, not accumulated into the header map
		}
		header.Add(key, value)
	}
}

// readHeaderLine reads one CRLF-terminated line.
func readHeaderLine(br *bufio.Reader) (line string, err error) {
	line, err = br.ReadString('\n')
	if err != nil {
		return
	}
	line = strings.TrimRight(line, "\r\n")
	return
}

// connBody is the response body: the decoded stream plus the one Close
// that tears the connection and its ctx watcher down.
type connBody struct {
	r     io.Reader
	close func()
}

func (inst *connBody) Read(p []byte) (int, error) { return inst.r.Read(p) }
func (inst *connBody) Close() error {
	inst.close()
	return nil
}

// activeProgress returns the live tick of the lane the result panels
// observe — the intermediate lane when an intermediate node is observed
// (mirroring activeSnapshot's selection without issuing a demand), the
// `main` lane otherwise. Render-thread-only.
func (inst *PlayApp) activeProgress() (p Summary, fresh bool) {
	split := inst.currentSplit
	if inst.observedNode != "" && inst.observedNode != split.Sink && len(split.Nodes) > 0 {
		return inst.intermediateLane.progressView()
	}
	return inst.graph.MainProgress()
}

// formatProgressLine renders one tick for the status bar and the loading
// empty-state: rows (with a percentage when the server knows the total),
// bytes read, peak memory, elapsed.
func formatProgressLine(p Summary) string {
	var b strings.Builder
	b.WriteString(humanCount(p.ReadRows))
	if p.TotalRowsToRead > 0 {
		fmt.Fprintf(&b, " / %s rows (%d%%)", humanCount(p.TotalRowsToRead),
			min(100, p.ReadRows*100/p.TotalRowsToRead))
	} else {
		b.WriteString(" rows")
	}
	b.WriteString(" · ")
	b.WriteString(humanBytes(p.ReadBytes))
	b.WriteString(" read")
	if p.MemoryUsage > 0 {
		b.WriteString(" · mem ")
		b.WriteString(humanBytes(p.MemoryUsage))
	}
	if p.ElapsedNs > 0 {
		b.WriteString(" · ")
		b.WriteString((time.Duration(p.ElapsedNs) * time.Nanosecond).Round(100 * time.Millisecond).String())
	}
	return b.String()
}

// humanCount renders a row count with K/M/B suffixes (counts, unlike
// bytes, conventionally use decimal thousands).
func humanCount(n uint64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return strconv.FormatUint(n, 10)
	}
}
