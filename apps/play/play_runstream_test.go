package play

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// okServerWithSummary answers 200 with body and the given
// X-ClickHouse-Summary, so a test can stage what the server reported about
// the result size.
func okServerWithSummary(t *testing.T, body []byte, summary string) (srv *httptest.Server, hits *int) {
	t.Helper()
	n := 0
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		w.Header().Set("X-ClickHouse-Summary", summary)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// runAndWait executes sql on the store and blocks until the run settles.
func runAndWait(t *testing.T, store *QueryStore, sql string) {
	t.Helper()
	store.Execute(sql, nil, "")
	require.Eventually(t, func() bool { return !store.IsLoading() },
		2*time.Second, 2*time.Millisecond, "run never finished")
}

func timeRef() time.Time { return time.Now().Add(-time.Second) }

func TestReadResultRowCap(t *testing.T) {
	cases := []struct {
		name       string
		sql        string
		wantRows   uint64
		wantBreaks bool
	}{
		{"no settings", "SELECT 1", 0, false},
		{"cap alone", "SET max_result_rows=100; SELECT 1", 100, false},
		{"cap and break mode", "SET max_result_rows=100, result_overflow_mode='break'; SELECT 1", 100, true},
		{"separate SET statements", "SET max_result_rows=250; SET result_overflow_mode='break'; SELECT 1", 250, true},
		{"throw mode is not a silent cap", "SET max_result_rows=100, result_overflow_mode='throw'; SELECT 1", 100, false},
		{"param settings are unrelated", "SET param_a=1; SELECT {a:UInt64}", 0, false},
		{"non-numeric cap ignored", "SET max_result_rows='lots'; SELECT 1", 0, false},
		{"unparseable", "NOT SQL ((", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := readResultRowCap(tc.sql)
			assert.Equal(t, tc.wantRows, got.MaxResultRows)
			assert.Equal(t, tc.wantBreaks, got.Breaks)
		})
	}
}

// The cap play reads off the buffer is JUDGED by the engine adapter, which
// is the only party that sees the response counters — queryengine.RowCap
// owns that verdict and tests it. What is play's to get right is the
// reading, above, and the end-to-end wiring, below.

// TestQueryStoreTruncationSurfaces drives the whole path: a run whose own
// prelude declares a break-mode cap, against a server that reports hitting
// it, must come back marked — the store's row count alone would read as a
// complete answer.
func TestQueryStoreTruncationSurfaces(t *testing.T) {
	body := emptyArrowStream(t)
	srv, _ := okServerWithSummary(t, body, `{"result_rows":"100","read_rows":"100"}`)
	c := NewClient(ClientConfig{URL: srv.URL}, srv.Client())
	store := NewQueryStore(c, nil, 10, "test")
	t.Cleanup(store.Close)

	runAndWait(t, store, "SET max_result_rows=100, result_overflow_mode='break'; SELECT 1")
	reason := store.Truncation()
	assert.NotEmpty(t, reason, "a capped result must say so")
	assert.Contains(t, reason, "max_result_rows=100")

	// The same server, a buffer that declared no cap: nothing to report.
	runAndWait(t, store, "SELECT 1")
	assert.Empty(t, store.Truncation())
}

func TestQuerySummaryLineNamesTheCap(t *testing.T) {
	app := &PlayApp{queryFSM: newQueryFSM()}
	app.queryFSM.Mirror(queryStateRunning)
	app.queryFSM.Mirror(queryStateRows)

	plain := app.querySummaryLine(100, 0, Summary{}, timeRef(), nil, "")
	assert.Contains(t, plain, "100 rows")
	assert.NotContains(t, plain, "capped")

	capped := app.querySummaryLine(100, 0, Summary{}, timeRef(), nil, "reached max_result_rows=100")
	assert.Contains(t, capped, "capped")
	assert.Contains(t, capped, "max_result_rows=100")
}

// scriptedStream replays a fixed frame sequence, so a test can stage the
// stream shapes a real engine produces without standing one up.
type scriptedStream struct {
	frames []runstream.Frame[[]byte]
	pos    int
	closed int
}

func (inst *scriptedStream) Next() (f runstream.Frame[[]byte], ok bool) {
	if inst.pos >= len(inst.frames) {
		return
	}
	f = inst.frames[inst.pos]
	inst.pos++
	ok = true
	return
}

func (inst *scriptedStream) Err() (err error) { return }

func (inst *scriptedStream) Close() (err error) {
	inst.closed++
	return
}

func dataFrames(seqStart int, chunks ...string) (out []runstream.Frame[[]byte]) {
	for i, c := range chunks {
		out = append(out, runstream.DataFrame(runstream.Seq(seqStart+i), []byte(c)))
	}
	return
}

// TestResultStreamFindsTheTerminalPastUnreadBytes is the bug this file's
// drain exists for. Arrow's IPC decoder stops at its end-of-stream marker
// rather than at end of body, so frames after that point — including the
// terminal — are still sitting in the stream. A reader that never asked for
// them concludes the producer died, and every successful run reads as
// incomplete.
func TestResultStreamFindsTheTerminalPastUnreadBytes(t *testing.T) {
	frames := dataFrames(1, "decoded", "trailing", "more trailing")
	frames = append(frames, runstream.TerminalFrame[[]byte](9, runstream.Complete()))
	rs, err := openResultStream(&scriptedStream{frames: frames})
	require.NoError(t, err)

	// Read only the first chunk, as a decoder that stops early would.
	buf := make([]byte, len("decoded"))
	n, rErr := rs.Read(buf)
	require.NoError(t, rErr)
	require.Equal(t, "decoded", string(buf[:n]))

	term, tErr := rs.terminal()
	require.NoError(t, tErr, "the terminal is past the unread bytes, not absent")
	assert.Equal(t, runstream.TerminalComplete, term.State)
}

// TestResultStreamWithoutATerminalIsIncomplete: the safety net still holds
// once the drain has run — draining looks for a terminal, it does not
// invent one.
func TestResultStreamWithoutATerminalIsIncomplete(t *testing.T) {
	rs, err := openResultStream(&scriptedStream{frames: dataFrames(1, "half an answer")})
	require.NoError(t, err)
	_, tErr := rs.terminal()
	assert.ErrorIs(t, tErr, runstream.ErrIncomplete)
}

// TestOpenResultStreamSurfacesTheServerDiagnostic: a rejected statement ends
// before any bytes arrive, and catching that terminal at open is what shows
// the user ClickHouse's own message instead of the Arrow decoder complaining
// about a missing IPC header.
func TestOpenResultStreamSurfacesTheServerDiagnostic(t *testing.T) {
	boom := eh.Errorf("clickhouse http 400: Code: 47. DB::Exception: Unknown identifier: x")
	src := &scriptedStream{frames: []runstream.Frame[[]byte]{
		runstream.TerminalFrame[[]byte](1, runstream.Failed(boom)),
	}}
	rs, err := openResultStream(src)
	require.Error(t, err)
	assert.Nil(t, rs)
	assert.Contains(t, err.Error(), "DB::Exception")
	assert.Equal(t, 1, src.closed, "a stream nobody will read must not be left open")
}

// TestResultStreamSkipsProgressFrames: progress is advisory and carries no
// bytes, so it must not appear in the decoded result or end it.
func TestResultStreamSkipsProgressFrames(t *testing.T) {
	frames := []runstream.Frame[[]byte]{
		runstream.ProgressFrame[[]byte](1, runstream.Progress{ReadRows: 5}),
		runstream.DataFrame(2, []byte("rows")),
		runstream.ProgressFrame[[]byte](3, runstream.Progress{ReadRows: 9}),
		runstream.TerminalFrame[[]byte](4, runstream.Complete()),
	}
	rs, err := openResultStream(&scriptedStream{frames: frames})
	require.NoError(t, err)
	body, rErr := io.ReadAll(rs)
	require.NoError(t, rErr)
	assert.Equal(t, "rows", string(body))
	term, tErr := rs.terminal()
	require.NoError(t, tErr)
	assert.Equal(t, runstream.TerminalComplete, term.State)
}
