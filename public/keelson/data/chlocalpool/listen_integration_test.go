//go:build integration

package chlocalpool

// Engine behaviour a long-lived, listening `clickhouse local` would rest on —
// the premises behind reopening ADR-0028's rejected O2. Each test pins one
// claim on the installed binary, so a version bump that breaks it fails here
// rather than in a design built on it.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type listener struct {
	base   string
	token  string
	client *http.Client
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	exited chan struct{}
	err    error
	stderr *lockedBuffer
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (inst *lockedBuffer) Write(p []byte) (n int, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.buf.Write(p)
}

func (inst *lockedBuffer) String() string {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.buf.String()
}

func freePort(tb testing.TB) (port int) {
	tb.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(tb, err)
	port = ln.Addr().(*net.TCPAddr).Port
	require.NoError(tb, ln.Close())
	return
}

// listenerOpts shapes a listener's process. ServerSettings are passed after
// `--` as configuration overrides (e.g. `max_server_memory_usage=…`, which
// the command line does not accept as an option). Every listener is behind
// the header gate of tokenGateConfig with a fresh token, which its client
// sends; Ungated starts one with ClickHouse's default handlers instead.
type listenerOpts struct {
	ServerSettings []string
	Ungated        bool
}

// tokenHeader carries the listener's shared secret. The listener's own
// session logs in from loopback as a passwordless `default`, so a password
// cannot keep other loopback clients out without locking the process out of
// itself; routing can. A request without the header matches no handler and
// gets a 404 on every path.
const tokenHeader = "X-Boxer-Token"

// tokenGateConfig replaces the default HTTP handlers with two that match only
// when tokenHeader carries token: the query handler and /ping.
func tokenGateConfig(token string) string {
	return `<clickhouse>
  <http_handlers>
    <rule>
      <methods>POST</methods>
      <headers><` + tokenHeader + `>` + token + `</` + tokenHeader + `></headers>
      <handler><type>dynamic_query_handler</type><query_param_name>query</query_param_name></handler>
    </rule>
    <rule>
      <url>/ping</url>
      <methods>GET</methods>
      <headers><` + tokenHeader + `>` + token + `</` + tokenHeader + `></headers>
      <handler><type>static</type><response_content>Ok.</response_content></handler>
    </rule>
  </http_handlers>
</clickhouse>
`
}

type tokenTransport struct {
	token string
	base  *http.Transport
}

func (inst tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set(tokenHeader, inst.token)
	return inst.base.RoundTrip(req)
}

// CloseIdleConnections lets http.Client.CloseIdleConnections reach the
// wrapped transport; an idle keep-alive connection holds shutdown.
func (inst tokenTransport) CloseIdleConnections() {
	inst.base.CloseIdleConnections()
}

// startListener runs `clickhouse local --interactive --query "SYSTEM START
// LISTEN HTTP"` with stdin held open by the test. Interactive mode then waits
// on stdin, so the process serves until the pipe closes — its lifetime is
// bound to the parent's, as a one-shot worker's is.
func startListener(tb testing.TB, opts ...listenerOpts) (l *listener) {
	tb.Helper()
	var o listenerOpts
	for _, x := range opts {
		o.ServerSettings = append(o.ServerSettings, x.ServerSettings...)
		o.Ungated = o.Ungated || x.Ungated
	}
	bin := requireBinaryTB(tb)
	port := freePort(tb)
	dir := tb.TempDir()
	args := []string{
		"--path", dir,
		"--logger.console", "0",
		"--listen_host", "127.0.0.1",
		"--http_port", strconv.Itoa(port),
		"--interactive", "--query", "SYSTEM START LISTEN HTTP",
	}
	client := &http.Client{}
	token := ""
	if !o.Ungated {
		var raw [16]byte
		_, err := rand.Read(raw[:])
		require.NoError(tb, err)
		token = hex.EncodeToString(raw[:])
		cfgPath := filepath.Join(dir, "token-gate.xml")
		require.NoError(tb, os.WriteFile(cfgPath, []byte(tokenGateConfig(token)), 0o600))
		args = append(args, "--config-file", cfgPath)
		client.Transport = tokenTransport{token: token, base: &http.Transport{}}
	}
	if len(o.ServerSettings) > 0 {
		args = append(args, "--")
		for _, kv := range o.ServerSettings {
			args = append(args, "--"+kv)
		}
	}
	cmd, err := extbin.ClickHouseLocal.Command(context.Background(), extbin.Opts{Path: bin}, args...)
	require.NoError(tb, err)
	l = &listener{base: "http://127.0.0.1:" + strconv.Itoa(port), token: token, client: client, cmd: cmd, exited: make(chan struct{}), stderr: &lockedBuffer{}}
	cmd.Stdout = io.Discard // interactive mode writes terminal control sequences
	cmd.Stderr = l.stderr
	l.stdin, err = cmd.StdinPipe()
	require.NoError(tb, err)
	require.NoError(tb, cmd.Start())
	go func() {
		l.err = cmd.Wait()
		close(l.exited)
	}()
	tb.Cleanup(func() {
		l.close()
		select {
		case <-l.exited:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-l.exited
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if l.ping() {
			return
		}
		select {
		case <-l.exited:
			tb.Fatalf("listener exited before serving: %v; stderr: %s", l.err, l.stderr.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
	tb.Fatalf("listener did not answer /ping within 10s; stderr: %s", l.stderr.String())
	return
}

// close ends the process the way a parent's exit would. Idle keep-alive
// connections are dropped first: an open one holds shutdown for seconds.
func (inst *listener) close() {
	inst.client.CloseIdleConnections()
	_ = inst.stdin.Close()
}

func (inst *listener) ping() bool {
	c := http.Client{Timeout: time.Second, Transport: inst.client.Transport}
	defer c.CloseIdleConnections()
	resp, err := c.Get(inst.base + "/ping")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// post sends one statement and returns the status and body.
func (inst *listener) post(tb testing.TB, params url.Values, sql string) (status int, body string) {
	tb.Helper()
	status, body, err := inst.postCtx(context.Background(), params, sql)
	require.NoError(tb, err)
	return
}

func (inst *listener) postCtx(ctx context.Context, params url.Values, sql string) (status int, body string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inst.base+"/?"+params.Encode(), strings.NewReader(sql))
	if err != nil {
		return
	}
	resp, err := inst.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	status, body = resp.StatusCode, strings.TrimSpace(string(b))
	return
}

func TestListen_LifetimeBoundToStdin(t *testing.T) {
	l := startListener(t)
	status, body := l.post(t, nil, "SELECT 1")
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, "1", body)

	l.close()
	select {
	case <-l.exited:
	case <-time.After(2 * time.Second):
		t.Fatal("listener kept running after stdin closed")
	}
	assert.NoError(t, l.err, "a stdin EOF is a clean exit")
	assert.False(t, l.ping(), "nothing answers once the process is gone")
}

// Temporary tables are scoped to the HTTP session, which is what an input
// table prelude needs to be invisible to other consumers of one process.
func TestListen_SessionTemporaryTablesAreIsolated(t *testing.T) {
	l := startListener(t)
	a := url.Values{"session_id": {"a"}}
	b := url.Values{"session_id": {"b"}}
	status, body := l.post(t, a, "CREATE TEMPORARY TABLE t AS SELECT 1 AS x")
	require.Equal(t, http.StatusOK, status, body)

	status, body = l.post(t, a, "SELECT x FROM t")
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "1", body)

	status, body = l.post(t, b, "SELECT x FROM t")
	assert.NotEqual(t, http.StatusOK, status)
	assert.Contains(t, body, "UNKNOWN_TABLE")
}

// readonly=2 refuses persistent DDL but allows the temporary tables an input
// prelude creates — the policy that closes the Memory-table leak.
func TestListen_Readonly2RefusesPersistentDDLAllowsTemporary(t *testing.T) {
	l := startListener(t)
	ro := url.Values{"readonly": {"2"}, "session_id": {"ro"}}
	status, body := l.post(t, ro, "CREATE TABLE m (x UInt8) ENGINE = Memory")
	assert.NotEqual(t, http.StatusOK, status)
	assert.Contains(t, body, "READONLY")

	status, body = l.post(t, ro, "CREATE TEMPORARY TABLE t AS SELECT 1 AS x")
	assert.Equal(t, http.StatusOK, status, body)

	status, body = l.post(t, ro, "CREATE TABLE m (x UInt8) ENGINE = Memory SETTINGS readonly = 0")
	assert.NotEqual(t, http.StatusOK, status, "a statement cannot lower readonly")
	assert.Contains(t, body, "READONLY")
	status, body = l.post(t, ro, "SELECT 1 SETTINGS readonly = 0")
	assert.NotEqual(t, http.StatusOK, status, "a statement cannot lower readonly")
	assert.Contains(t, body, "readonly")
}

// The hazard ADR-0028 rejected O2 on still holds without that policy: a
// table one client creates is visible to every other client of the process.
func TestListen_PersistentTablesAreSharedAcrossClients(t *testing.T) {
	l := startListener(t)
	status, body := l.post(t, url.Values{"session_id": {"writer"}}, "CREATE TABLE m (x UInt8) ENGINE = Memory")
	require.Equal(t, http.StatusOK, status, body)

	status, body = l.post(t, url.Values{"session_id": {"reader"}, "readonly": {"2"}},
		"SELECT name FROM system.tables WHERE database = currentDatabase() AND name = 'm'")
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "m", body)
}

func TestListen_PerRequestMemoryCap(t *testing.T) {
	l := startListener(t)
	status, body := l.post(t, url.Values{"max_memory_usage": {strconv.Itoa(10 << 20)}},
		"SELECT groupArray(number) FROM numbers(10000000) FORMAT Null")
	assert.NotEqual(t, http.StatusOK, status)
	assert.Contains(t, body, "MEMORY_LIMIT_EXCEEDED")

	status, body = l.post(t, nil, "SELECT 1")
	assert.Equal(t, http.StatusOK, status, "a refused request leaves the process serving: %s", body)
}

// A client that goes away does not cancel its query by default; with
// readonly and cancel_http_readonly_queries_on_client_close it does.
func TestListen_ClientDisconnect(t *testing.T) {
	l := startListener(t)
	const slow = "SELECT sleepEachRow(1) FROM numbers(3) SETTINGS max_block_size = 1 FORMAT Null"
	running := func(queryID string) bool {
		_, body := l.post(t, nil, "SELECT count() FROM system.processes WHERE query_id = '"+queryID+"'")
		return body == "1"
	}
	disconnect := func(params url.Values) {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		_, _, err := l.postCtx(ctx, params, slow)
		require.Error(t, err, "the client is meant to give up before the query ends")
	}

	t.Run("default keeps running", func(t *testing.T) {
		disconnect(url.Values{"query_id": {"keep"}})
		time.Sleep(500 * time.Millisecond)
		assert.True(t, running("keep"))
		_, _ = l.post(t, nil, "KILL QUERY WHERE query_id = 'keep' SYNC")
	})
	t.Run("readonly cancel on close", func(t *testing.T) {
		disconnect(url.Values{"query_id": {"drop"}, "readonly": {"2"}, "cancel_http_readonly_queries_on_client_close": {"1"}})
		deadline := time.Now().Add(2 * time.Second)
		for running("drop") {
			if time.Now().After(deadline) {
				t.Fatal("query still running 2s after its client disconnected")
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
}

// Over HTTP the status line is sent before a mid-stream failure: the reply is
// 200 with an X-ClickHouse-Exception-Tag header, and the error follows the
// data as a tagged `__exception__` trailer (the shape
// queryengine/chserver/exception.go parses). An Arrow reader that stops at the
// end-of-stream marker never sees it, but the reply is not left looking
// complete either: the connection closes without the final zero-length chunk,
// so the client's body read fails. wait_end_of_query=1 turns it back into a
// 500 with X-ClickHouse-Exception-Code, at the cost of buffering the result.
func TestListen_MidStreamErrorSignalling(t *testing.T) {
	l := startListener(t)
	const failing = "SELECT number, throwIf(number = 50000) AS boom FROM numbers(100000) SETTINGS max_block_size = 1000"
	do := func(params url.Values) (resp *http.Response, body []byte, readErr error) {
		req, err := http.NewRequest(http.MethodPost, l.base+"/?"+params.Encode(), strings.NewReader(failing))
		require.NoError(t, err)
		resp, err = l.client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, readErr = io.ReadAll(resp.Body)
		return
	}

	resp, body, readErr := do(url.Values{"default_format": {"ArrowStream"}})
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.ErrorIs(t, readErr, io.ErrUnexpectedEOF, "the chunked body is left unterminated")
	tag := resp.Header.Get("X-ClickHouse-Exception-Tag")
	require.NotEmpty(t, tag)
	assert.Contains(t, string(body), "FUNCTION_THROW_IF_VALUE_IS_NON_ZERO")
	assert.True(t, bytes.HasSuffix(body, []byte(tag+"\r\n__exception__\r\n")), "body ends with the tagged trailer")

	resp, body, readErr = do(url.Values{"default_format": {"ArrowStream"}, "wait_end_of_query": {"1"}})
	require.NoError(t, readErr)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, "395", resp.Header.Get("X-ClickHouse-Exception-Code"))
	assert.Contains(t, string(body), "FUNCTION_THROW_IF_VALUE_IS_NON_ZERO")
}

// With the token gate, a loopback client without the shared secret reaches
// nothing — not the query handler, not /ping, not the bundled UIs — and the
// gated handler still takes URL settings.
func TestListen_HeaderTokenGatesEveryHandler(t *testing.T) {
	l := startListener(t)

	status, body := l.post(t, url.Values{"readonly": {"2"}, "default_format": {"JSONEachRow"}}, "SELECT 1 AS x")
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, `{"x":1}`, body)

	bare := &http.Client{}
	defer bare.CloseIdleConnections()
	probes := []struct {
		method, path, header string
	}{
		{method: http.MethodPost, path: "/"},
		{method: http.MethodGet, path: "/?query=SELECT%201"},
		{method: http.MethodGet, path: "/ping"},
		{method: http.MethodGet, path: "/play"},
		{method: http.MethodPost, path: "/", header: "wrong"},
	}
	for _, p := range probes {
		req, err := http.NewRequest(p.method, l.base+p.path, strings.NewReader("SELECT 1"))
		require.NoError(t, err)
		if p.header != "" {
			req.Header.Set(tokenHeader, p.header)
		}
		resp, err := bare.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "%s %s header=%q", p.method, p.path, p.header)
	}
}

// Why the gate exists: without it any loopback client is served as the
// passwordless `default` user. A listener's trust boundary is otherwise every
// process on the host, where a pipe worker's is its parent.
func TestListen_UngatedListenerServesAnyLoopbackClient(t *testing.T) {
	l := startListener(t, listenerOpts{Ungated: true})
	bare := &http.Client{}
	defer bare.CloseIdleConnections()
	resp, err := bare.Post(l.base+"/", "text/plain", strings.NewReader("SELECT currentUser()"))
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "default", strings.TrimSpace(string(b)))
}

// The broker's preludes over HTTP. The interface rejects multi-statement
// bodies, so each input table is its own request in the caller's session,
// under readonly=2, and query parameters travel as param_* URL values instead
// of a `SET param_…` prelude.
func TestListen_PreludeOverASession(t *testing.T) {
	arrow, stderr, err := runOutput(t, "SELECT number AS n FROM numbers(4)", "Arrow")
	require.NoError(t, err, stderr)
	path := filepath.Join(t.TempDir(), "in.arrow")
	require.NoError(t, os.WriteFile(path, arrow, 0o600))

	l := startListener(t)
	session := url.Values{"session_id": {"prelude"}, "readonly": {"2"}}
	status, body := l.post(t, session, "SELECT 1; SELECT 2")
	assert.NotEqual(t, http.StatusOK, status)
	assert.Contains(t, body, "Multi-statements are not allowed")

	status, body = l.post(t, session, "CREATE TEMPORARY TABLE input AS SELECT * FROM file('"+path+"', 'Arrow')")
	require.Equal(t, http.StatusOK, status, body)

	query := url.Values{"session_id": {"prelude"}, "readonly": {"2"}, "param_k": {"2"}}
	status, body = l.post(t, query, "SELECT sum(n) FROM input WHERE n >= {k:UInt64}")
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "5", body)
}
