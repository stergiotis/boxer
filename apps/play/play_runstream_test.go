package play

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
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
			assert.Equal(t, tc.wantRows, got.maxResultRows)
			assert.Equal(t, tc.wantBreaks, got.breaks)
		})
	}
}

func TestResultRowCapTerminalFor(t *testing.T) {
	capped := resultRowCap{maxResultRows: 100, breaks: true}

	// Under the cap: whole.
	assert.Equal(t, runstream.TerminalComplete, capped.terminalFor(Summary{ResultRows: 99}).State)

	// At or over it: reported as possibly a prefix. ClickHouse stops at a
	// block boundary, so `over` is normal, and a result that is complete at
	// exactly the cap is indistinguishable from one cut there — R9 says take
	// the loud reading.
	for _, rows := range []uint64{100, 128} {
		term := capped.terminalFor(Summary{ResultRows: rows})
		assert.Equal(t, runstream.TerminalTruncated, term.State, "rows=%d", rows)
		assert.Contains(t, term.Reason, "max_result_rows=100")
	}

	// throw mode raises server-side instead of truncating, so nothing here
	// should claim a cap.
	throws := resultRowCap{maxResultRows: 100, breaks: false}
	assert.Equal(t, runstream.TerminalComplete, throws.terminalFor(Summary{ResultRows: 500}).State)

	// No declared cap: nothing to report, however many rows came back.
	var none resultRowCap
	assert.Equal(t, runstream.TerminalComplete, none.terminalFor(Summary{ResultRows: 1 << 20}).State)
}

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
