package chserver

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// progressTestServer accepts one connection, reads the request, writes
// script(conn) and closes. It returns the base URL. httptest cannot serve
// these tests: the whole point is a response-header block that stays open,
// which needs the raw socket.
func progressTestServer(t *testing.T, script func(conn net.Conn)) (baseURL string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, aErr := ln.Accept()
		if aErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Drain the request head (and its small body) before scripting the
		// response.
		br := bufio.NewReader(conn)
		for {
			line, rErr := br.ReadString('\n')
			if rErr != nil {
				return
			}
			if line == "\r\n" || line == "\n" {
				break
			}
		}
		script(conn)
	}()
	baseURL = "http://" + ln.Addr().String() + "/"
	return
}

// TestProgressTransportStreamsMidBlock pins the entire point of the
// hand-rolled transport: progress ticks reach the sink WHILE the header
// block is still open. The server refuses to finish the response until the
// client has observed both ticks — a causal proof, no timing flakes.
func TestProgressTransportStreamsMidBlock(t *testing.T) {
	ticks := make(chan runstream.Progress, 8)
	proceed := make(chan struct{})
	baseURL := progressTestServer(t, func(conn net.Conn) {
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\n")
		_, _ = io.WriteString(conn, "X-ClickHouse-Progress: {\"read_rows\":\"10\",\"read_bytes\":\"80\",\"total_rows_to_read\":\"100\",\"memory_usage\":\"1024\"}\r\n")
		_, _ = io.WriteString(conn, "X-ClickHouse-Progress: {\"read_rows\":\"50\",\"read_bytes\":\"400\",\"total_rows_to_read\":\"100\",\"memory_usage\":\"2048\"}\r\n")
		<-proceed // only complete the block once the client saw both ticks
		_, _ = io.WriteString(conn, "X-ClickHouse-Summary: {\"read_rows\":\"100\",\"read_bytes\":\"800\"}\r\n")
		_, _ = io.WriteString(conn, "Content-Type: text/plain\r\nContent-Length: 5\r\n\r\nhello")
	})

	client := &http.Client{Transport: &progressTransport{onProgress: func(p runstream.Progress) { ticks <- p }}}
	req, err := http.NewRequest("POST", baseURL, strings.NewReader("SELECT 1"))
	require.NoError(t, err)

	type doResult struct {
		resp *http.Response
		err  error
	}
	doCh := make(chan doResult, 1)
	go func() {
		resp, dErr := client.Do(req)
		doCh <- doResult{resp, dErr}
	}()

	first := <-ticks
	require.EqualValues(t, 10, first.ReadRows)
	require.EqualValues(t, 1024, first.MemoryUsage)
	second := <-ticks
	require.EqualValues(t, 50, second.ReadRows)
	select {
	case r := <-doCh:
		t.Fatalf("Do returned before the header block completed: %+v", r)
	default: // good — the response is still open
	}
	close(proceed)

	r := <-doCh
	require.NoError(t, r.err)
	defer func() { _ = r.resp.Body.Close() }()
	require.Equal(t, http.StatusOK, r.resp.StatusCode)
	require.Contains(t, r.resp.Header.Get("X-ClickHouse-Summary"), "\"read_rows\":\"100\"")
	require.Empty(t, r.resp.Header.Get(progressHeaderKey), "ticks are consumed, not accumulated")
	body, err := io.ReadAll(r.resp.Body)
	require.NoError(t, err)
	require.Equal(t, "hello", string(body))
}

func TestProgressTransportChunkedBody(t *testing.T) {
	baseURL := progressTestServer(t, func(conn net.Conn) {
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\n")
		_, _ = io.WriteString(conn, "X-ClickHouse-Progress: {\"read_rows\":\"1\"}\r\n")
		_, _ = io.WriteString(conn, "Transfer-Encoding: chunked\r\n\r\n")
		_, _ = io.WriteString(conn, "5\r\nhello\r\n6\r\n world\r\n0\r\n\r\n")
	})
	client := &http.Client{Transport: &progressTransport{onProgress: func(runstream.Progress) {}}}
	resp, err := client.Post(baseURL, "text/plain", strings.NewReader("q"))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "hello world", string(body))
	require.Empty(t, resp.Header.Get("Transfer-Encoding"), "framing is decoded by the transport")
}

func TestProgressTransportCancelMidHeaders(t *testing.T) {
	stall := make(chan struct{})
	t.Cleanup(func() { close(stall) })
	baseURL := progressTestServer(t, func(conn net.Conn) {
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\n")
		_, _ = io.WriteString(conn, "X-ClickHouse-Progress: {\"read_rows\":\"1\"}\r\n")
		<-stall // never completes the block
	})
	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Transport: &progressTransport{onProgress: func(runstream.Progress) { cancel() }}}
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL, strings.NewReader("q"))
	require.NoError(t, err)
	start := time.Now()
	_, err = client.Do(req) //nolint:bodyclose // the request fails; there is no body
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, time.Since(start), 3*time.Second, "cancellation must not wait out the stall")
}

func TestNewProgressClientSchemeGate(t *testing.T) {
	t.Parallel()
	require.NotNil(t, newProgressClient("http://127.0.0.1:8123/", func(runstream.Progress) {}))
	require.Nil(t, newProgressClient("https://ch.example/", func(runstream.Progress) {}),
		"TLS endpoints keep the stock client")
	require.Nil(t, newProgressClient("::not-a-url::", func(runstream.Progress) {}))
}

// TestDeliverSurfacesInBandProgress is the seam itself: a request that asks
// for progress gets the streaming transport, and its ticks reach the
// caller's sink before the result stream exists — which is exactly why they
// are a callback and not progress frames.
func TestDeliverSurfacesInBandProgress(t *testing.T) {
	ticks := make(chan runstream.Progress, 4)
	proceed := make(chan struct{})
	baseURL := progressTestServer(t, func(conn net.Conn) {
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\n")
		_, _ = io.WriteString(conn, "X-ClickHouse-Progress: {\"read_rows\":\"42\",\"total_rows_to_read\":\"84\"}\r\n")
		<-proceed
		_, _ = io.WriteString(conn, "Content-Length: 4\r\n\r\ndone")
	})
	eng, err := New(Config{Endpoint: baseURL})
	require.NoError(t, err)

	type delivery struct {
		st  queryengine.StreamI
		err error
	}
	done := make(chan delivery, 1)
	go func() {
		st, _, dErr := eng.Deliver(context.Background(), queryengine.Request{
			SQL:        "SELECT 1",
			OnProgress: func(p runstream.Progress) { ticks <- p },
		})
		done <- delivery{st, dErr}
	}()

	tick := <-ticks
	require.EqualValues(t, 42, tick.ReadRows)
	require.EqualValues(t, 84, tick.TotalRowsToRead)
	select {
	case d := <-done:
		t.Fatalf("Deliver returned before the header block completed: %+v", d)
	default: // good — the tick is live, not a replay
	}
	close(proceed)

	d := <-done
	require.NoError(t, d.err)
	defer func() { _ = d.st.Close() }()
	body, term, cErr := queryengine.Collect(d.st)
	require.NoError(t, cErr)
	require.Equal(t, "done", string(body))
	require.Equal(t, runstream.TerminalComplete, term.State)
}
