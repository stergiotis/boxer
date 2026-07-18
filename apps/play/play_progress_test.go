package play

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// progressTestServer accepts one connection, reads the request, writes
// script(conn) and closes. It returns the base URL.
func progressTestServer(t *testing.T, script func(conn net.Conn)) string {
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
	return "http://" + ln.Addr().String() + "/"
}

// TestProgressTransportStreamsMidBlock pins the entire point of the
// hand-rolled transport: progress ticks reach the sink WHILE the header
// block is still open. The server refuses to finish the response until
// the client has observed both ticks — a causal proof, no timing flakes.
func TestProgressTransportStreamsMidBlock(t *testing.T) {
	ticks := make(chan Summary, 8)
	proceed := make(chan struct{})
	baseURL := progressTestServer(t, func(conn net.Conn) {
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\n")
		_, _ = io.WriteString(conn, "X-ClickHouse-Progress: {\"read_rows\":\"10\",\"read_bytes\":\"80\",\"total_rows_to_read\":\"100\",\"memory_usage\":\"1024\"}\r\n")
		_, _ = io.WriteString(conn, "X-ClickHouse-Progress: {\"read_rows\":\"50\",\"read_bytes\":\"400\",\"total_rows_to_read\":\"100\",\"memory_usage\":\"2048\"}\r\n")
		<-proceed // only complete the block once the client saw both ticks
		_, _ = io.WriteString(conn, "X-ClickHouse-Summary: {\"read_rows\":\"100\",\"read_bytes\":\"800\"}\r\n")
		_, _ = io.WriteString(conn, "Content-Type: text/plain\r\nContent-Length: 5\r\n\r\nhello")
	})

	client := &http.Client{Transport: &progressTransport{onProgress: func(p Summary) { ticks <- p }}}
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
	client := &http.Client{Transport: &progressTransport{onProgress: func(Summary) {}}}
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
	client := &http.Client{Transport: &progressTransport{onProgress: func(Summary) { cancel() }}}
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL, strings.NewReader("q"))
	require.NoError(t, err)
	start := time.Now()
	_, err = client.Do(req) //nolint:bodyclose // the request fails; there is no body
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, time.Since(start), 3*time.Second, "cancellation must not wait out the stall")
}

func TestNewProgressClientSchemeGate(t *testing.T) {
	require.NotNil(t, newProgressClient("http://127.0.0.1:8123/", func(Summary) {}))
	require.Nil(t, newProgressClient("https://ch.example/", func(Summary) {}), "TLS endpoints keep the stock client")
	require.Nil(t, newProgressClient("::not-a-url::", func(Summary) {}))
}

// fakeProgressExec implements both executor interfaces: each
// executeWithProgress call parks until release closes, exposing its sink
// so the test can fire ticks at controlled moments.
type fakeProgressExec struct {
	mu      sync.Mutex
	sinks   []func(Summary)
	release chan struct{}
}

func (inst *fakeProgressExec) execute(ctx context.Context, c compiledNode, alloc memory.Allocator) (arrow.RecordBatch, *arrow.Schema, Summary, error) {
	return inst.executeWithProgress(ctx, c, alloc, nil)
}

func (inst *fakeProgressExec) executeWithProgress(ctx context.Context, c compiledNode, alloc memory.Allocator, onProgress func(Summary)) (arrow.RecordBatch, *arrow.Schema, Summary, error) {
	inst.mu.Lock()
	inst.sinks = append(inst.sinks, onProgress)
	inst.mu.Unlock()
	select {
	case <-inst.release:
		return nil, nil, Summary{}, nil
	case <-ctx.Done():
		return nil, nil, Summary{}, ctx.Err()
	}
}

func (inst *fakeProgressExec) sink(t *testing.T, i int) func(Summary) {
	t.Helper()
	require.Eventually(t, func() bool {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		return len(inst.sinks) > i
	}, 2*time.Second, time.Millisecond)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.sinks[i]
}

func TestLaneProgressTickAndGate(t *testing.T) {
	exec := &fakeProgressExec{release: make(chan struct{})}
	lane := newNodeLane(exec, nil, 0)
	defer lane.close()

	v := lane.demand(compiledNode{SQL: "SELECT A"})
	require.True(t, v.loading)
	require.False(t, v.progressFresh, "no tick yet")

	exec.sink(t, 0)(Summary{ReadRows: 10, TotalRowsToRead: 100})
	p, fresh := lane.progressView()
	require.True(t, fresh)
	require.EqualValues(t, 10, p.ReadRows)
	v = lane.demand(compiledNode{SQL: "SELECT A"})
	require.True(t, v.progressFresh)
	require.EqualValues(t, 10, v.progress.ReadRows)

	close(exec.release)
	require.Eventually(t, func() bool {
		view := lane.demand(compiledNode{SQL: "SELECT A"})
		defer func() {
			if view.rec != nil {
				view.rec.Release()
			}
		}()
		return !view.loading
	}, 2*time.Second, time.Millisecond)
	_, fresh = lane.progressView()
	require.False(t, fresh, "a landed run shows no stale ticks")
}

func TestLaneProgressSupersededTickDiscarded(t *testing.T) {
	exec := &fakeProgressExec{release: make(chan struct{})}
	defer close(exec.release)
	lane := newNodeLane(exec, nil, 0)
	defer lane.close()

	lane.demand(compiledNode{SQL: "SELECT A"})
	oldSink := exec.sink(t, 0)
	lane.demand(compiledNode{SQL: "SELECT B"}) // supersedes A
	newSink := exec.sink(t, 1)

	oldSink(Summary{ReadRows: 999}) // late tick from the superseded run
	_, fresh := lane.progressView()
	require.False(t, fresh, "a superseded run's tick must not paint the new run's badge")

	newSink(Summary{ReadRows: 5})
	p, fresh := lane.progressView()
	require.True(t, fresh)
	require.EqualValues(t, 5, p.ReadRows)
}

// TestQueryStoreProgressEndToEnd runs the real ExecuteArrowStream against
// the dribble server: the store's Progress() must go fresh while the
// header block is still open, then gate off once the run lands (the body
// is not a valid ArrowStream — the run finishes with an error, which is
// irrelevant to progress gating).
func TestQueryStoreProgressEndToEnd(t *testing.T) {
	proceed := make(chan struct{})
	baseURL := progressTestServer(t, func(conn net.Conn) {
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\n")
		_, _ = io.WriteString(conn, "X-ClickHouse-Progress: {\"read_rows\":\"42\",\"total_rows_to_read\":\"84\"}\r\n")
		<-proceed
		_, _ = io.WriteString(conn, "Content-Length: 0\r\n\r\n")
	})
	store := NewQueryStore(NewClient(ClientConfig{URL: baseURL}, nil), nil, 10, "progress-test")
	defer store.Close()
	store.Execute("SELECT 1", nil)

	require.Eventually(t, func() bool {
		_, fresh := store.Progress()
		return fresh
	}, 2*time.Second, time.Millisecond, "the tick must surface while the run is in flight")
	p, _ := store.Progress()
	require.EqualValues(t, 42, p.ReadRows)

	close(proceed)
	require.Eventually(t, func() bool { return !store.IsLoading() }, 2*time.Second, time.Millisecond)
	_, fresh := store.Progress()
	require.False(t, fresh)
}

func TestFormatProgressLine(t *testing.T) {
	s := formatProgressLine(Summary{ReadRows: 1_946_964_294, ReadBytes: 15_575_714_352,
		TotalRowsToRead: 2_500_000_000, ElapsedNs: 300_006_531, MemoryUsage: 1_145_567})
	require.Contains(t, s, "1.9B / 2.5B rows (77%)")
	require.Contains(t, s, "14.5 GB read")
	require.Contains(t, s, "mem 1.1 MB")
	require.Contains(t, s, "300ms")

	require.Equal(t, "12 rows · 0 B read", formatProgressLine(Summary{ReadRows: 12}))
}
