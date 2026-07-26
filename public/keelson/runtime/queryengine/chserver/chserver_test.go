package chserver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/chhttp"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// capture records what the fake server was asked for, so a test can assert
// on the wire rather than on the adapter's internals.
type capture struct {
	mu     sync.Mutex
	method string
	body   string
	query  url.Values
	header http.Header
}

func (inst *capture) get() (method string, body string, query url.Values, header http.Header) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.method, inst.body, inst.query, inst.header
}

// newServer stands up a fake ClickHouse HTTP endpoint. respond writes the
// answer; the request is captured first.
func newServer(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) (endpoint string, cap *capture) {
	t.Helper()
	cap = &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		cap.mu.Lock()
		cap.method, cap.body, cap.query, cap.header = r.Method, string(raw), r.URL.Query(), r.Header.Clone()
		cap.mu.Unlock()
		respond(w, r)
	}))
	t.Cleanup(srv.Close)
	endpoint = srv.URL
	return
}

func newEngine(t *testing.T, endpoint string) (eng *Engine) {
	t.Helper()
	eng, err := New(Config{Endpoint: endpoint, User: "u", Password: "p"})
	require.NoError(t, err)
	return
}

func TestNewRequiresEndpoint(t *testing.T) {
	t.Parallel()
	_, err := New(Config{})
	assert.Error(t, err)
}

func TestDeliverPostsTheStatementAndItsBindings(t *testing.T) {
	t.Parallel()
	endpoint, cap := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apache.arrow.stream")
		_, _ = w.Write([]byte("rows"))
	})
	eng := newEngine(t, endpoint)

	st, res, err := eng.Deliver(context.Background(), queryengine.Request{
		SQL:      "SELECT {n:UInt64}",
		RunID:    "play-main-box-7-1",
		Format:   "ArrowStream",
		Params:   map[string]string{"n": "42"},
		Settings: map[string]string{"log_comment": "{}", "replace_running_query": "1"},
	})
	require.NoError(t, err)
	defer func() { _ = st.Close() }()

	method, body, query, header := cap.get()
	assert.Equal(t, "POST", method)
	assert.Equal(t, "SELECT {n:UInt64}", body, "the adapter ships the statement verbatim; it does not rewrite SQL")
	assert.Equal(t, "42", query.Get(chhttp.ParamPrefix+"n"), "bindings are named once, and prefixed here")
	assert.Equal(t, "play-main-box-7-1", query.Get("query_id"), "the run id is the join key everything else uses (R7)")
	assert.Equal(t, "ArrowStream", query.Get("default_format"))
	assert.Equal(t, "{}", query.Get("log_comment"), "settings ride through verbatim")
	assert.Equal(t, "1", query.Get("replace_running_query"))
	assert.Equal(t, "u", header.Get("X-ClickHouse-User"))
	assert.Equal(t, "p", header.Get("X-ClickHouse-Key"))
	assert.Equal(t, "application/vnd.apache.arrow.stream", res.ContentType)

	bodyBytes, term, err := queryengine.Collect(st)
	require.NoError(t, err)
	assert.Equal(t, "rows", string(bodyBytes))
	assert.Equal(t, runstream.TerminalComplete, term.State)
}

func TestDeliverRejectsSettingsTheAdapterOwns(t *testing.T) {
	t.Parallel()
	endpoint, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {})
	eng := newEngine(t, endpoint)

	for _, key := range []string{"query_id", "default_format", "param_n", "send_progress_in_http_headers"} {
		_, _, err := eng.Deliver(context.Background(), queryengine.Request{
			SQL:      "SELECT 1",
			Settings: map[string]string{key: "x"},
		})
		assert.Error(t, err, "setting %q behind the adapter's back would give the run two answers", key)
	}
}

func TestDeliverRefusesInputTables(t *testing.T) {
	t.Parallel()
	endpoint, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {})
	eng := newEngine(t, endpoint)

	_, _, err := eng.Deliver(context.Background(), queryengine.Request{
		SQL:    "SELECT * FROM t",
		Inputs: map[string][]byte{"t": []byte("arrow")},
	})
	// Refusing beats dropping: a silently discarded input becomes "unknown
	// table t" against the server, with nothing saying the data never left.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "in-memory input tables")
}

func TestDeliverServerErrorIsAFailedTerminalNotAnError(t *testing.T) {
	t.Parallel()
	endpoint, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Code: 47. DB::Exception: Unknown identifier: x"))
	})
	eng := newEngine(t, endpoint)

	st, _, err := eng.Deliver(context.Background(), queryengine.Request{SQL: "SELECT x"})
	// The run happened and ended badly. That is an outcome, and outcomes
	// live in the terminal frame — which is what lets a consumer stop
	// branching on which engine ran the query.
	require.NoError(t, err)
	defer func() { _ = st.Close() }()

	_, term, cErr := queryengine.Collect(st)
	require.NoError(t, cErr)
	assert.Equal(t, runstream.TerminalFailed, term.State)
	require.Error(t, term.Err)
	assert.Contains(t, term.Err.Error(), "clickhouse http 400", "the shape play's probe classifier keys on")
	assert.Contains(t, term.Err.Error(), "DB::Exception")
}

func TestDeliverUnreachableServerIsAFailedTerminal(t *testing.T) {
	t.Parallel()
	eng, err := New(Config{Endpoint: "http://127.0.0.1:1/query"})
	require.NoError(t, err)
	st, _, err := eng.Deliver(context.Background(), queryengine.Request{SQL: "SELECT 1"})
	require.NoError(t, err, "a transport that never connected is still how the run ended")
	defer func() { _ = st.Close() }()
	_, term, cErr := queryengine.Collect(st)
	require.NoError(t, cErr)
	assert.Equal(t, runstream.TerminalFailed, term.State)
}

func TestDeliverReportsTheSummaryAndTruncation(t *testing.T) {
	t.Parallel()
	endpoint, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(chhttp.HeaderSummary, `{"read_rows":"1000","result_rows":"10","elapsed_ns":"5"}`)
		_, _ = w.Write([]byte("ten rows"))
	})
	eng := newEngine(t, endpoint)

	st, res, err := eng.Deliver(context.Background(), queryengine.Request{
		SQL: "SELECT * FROM t",
		Cap: queryengine.RowCap{MaxResultRows: 10, Breaks: true},
	})
	require.NoError(t, err)
	defer func() { _ = st.Close() }()

	assert.Equal(t, uint64(1000), res.Summary.ReadRows)
	assert.Equal(t, uint64(10), res.Summary.ResultRows)

	_, term, cErr := queryengine.Collect(st)
	require.NoError(t, cErr)
	assert.Equal(t, runstream.TerminalTruncated, term.State,
		"a result that stopped at its own declared cap is reported as possibly a prefix")
}

func TestDeliverChunksALargeBody(t *testing.T) {
	t.Parallel()
	payload := strings.Repeat("x", (queryengine.DefaultChunkSize*2)+7)
	endpoint, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	})
	eng := newEngine(t, endpoint)

	st, _, err := eng.Deliver(context.Background(), queryengine.Request{SQL: "SELECT 1"})
	require.NoError(t, err)
	defer func() { _ = st.Close() }()

	dataFrames := 0
	var seqs []runstream.Seq
	var col runstream.Collector[[]byte]
	for {
		f, ok := st.Next()
		if !ok {
			break
		}
		seqs = append(seqs, f.Seq)
		if f.Kind == runstream.KindData {
			dataFrames++
		}
		require.NoError(t, col.Push(f), "the collector rejects a producer that misnumbers its frames")
	}
	assert.Greater(t, dataFrames, 1, "a body larger than a chunk arrives as more than one frame")
	for i := 1; i < len(seqs); i++ {
		assert.Greater(t, seqs[i], seqs[i-1], "sequence numbers strictly increase")
	}
	term, err := col.Terminal()
	require.NoError(t, err)
	assert.Equal(t, runstream.TerminalComplete, term.State)
}

func TestDeliverCancelledContext(t *testing.T) {
	t.Parallel()
	endpoint, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {})
	eng := newEngine(t, endpoint)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	st, _, err := eng.Deliver(ctx, queryengine.Request{SQL: "SELECT 1"})
	require.NoError(t, err)
	defer func() { _ = st.Close() }()
	_, term, cErr := queryengine.Collect(st)
	require.NoError(t, cErr)
	assert.Equal(t, runstream.TerminalFailed, term.State)
}

func TestKillAddressesTheRunById(t *testing.T) {
	t.Parallel()
	endpoint, cap := newServer(t, func(w http.ResponseWriter, r *http.Request) {})
	eng := newEngine(t, endpoint)

	err := eng.Kill(context.Background(), "play-main-box-7-1")
	require.NoError(t, err)
	_, body, _, _ := cap.get()
	assert.Contains(t, body, "KILL QUERY")
	assert.Contains(t, body, "query_id='play-main-box-7-1'")
}

func TestKillRefusesAnUnsafeId(t *testing.T) {
	t.Parallel()
	endpoint, cap := newServer(t, func(w http.ResponseWriter, r *http.Request) {})
	eng := newEngine(t, endpoint)

	// The id is interpolated into a statement, so the charset check is the
	// whole defence — consumers reject rather than escape.
	err := eng.Kill(context.Background(), "x' OR 1=1 --")
	require.Error(t, err)
	_, body, _, _ := cap.get()
	assert.Empty(t, body, "nothing may reach the server built around an id that was never checked")
}

func TestKillReportsTransportFailure(t *testing.T) {
	t.Parallel()
	eng, err := New(Config{Endpoint: "http://127.0.0.1:1/query"})
	require.NoError(t, err)
	assert.Error(t, eng.Kill(context.Background(), "play-main-box-7-1"),
		"a kill that never reached the server is not a kill")
}

func TestParseSummaryTolerance(t *testing.T) {
	t.Parallel()
	assert.Equal(t, queryengine.Summary{}, ParseSummary(""), "an absent header is not a failure")
	assert.Equal(t, queryengine.Summary{}, ParseSummary("not json"), "a malformed header reports nothing")
	got := ParseSummary(`{"read_rows":"7","memory_usage":"9"}`)
	assert.Equal(t, uint64(7), got.ReadRows)
	assert.Equal(t, uint64(9), got.MemoryUsage)
	assert.Zero(t, got.ResultRows, "a field the server did not report stays zero, meaning unreported")
}
