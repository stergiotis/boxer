package play

// Slice-5a regression tests (ADR-0097 "Slice 5 (design)"): the signal-store
// substrate — compiled (SQL, params) memo identity, the param_* URL wire
// channel with SET-shadowing (D1), Run-time resolution of unbound slots, the
// signal half of the staleness witness (D2), and the history snapshot/seed
// round-trip (D4).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func TestCompiledNodeKeyDistinguishesParams(t *testing.T) {
	a := compiledNode{SQL: "SELECT {x:UInt64}"}
	b := compiledNode{SQL: "SELECT {x:UInt64}", Params: map[string]string{"param_x": "1"}}
	c2 := compiledNode{SQL: "SELECT {x:UInt64}", Params: map[string]string{"param_x": "2"}}
	require.Equal(t, a.SQL, a.key(), "no params ⇒ the key is the SQL")
	require.NotEqual(t, a.key(), b.key())
	require.NotEqual(t, b.key(), c2.key(), "same SQL under a different signal value is a different execution")

	d := compiledNode{SQL: "s", Params: map[string]string{"param_a": "1", "param_b": "2"}}
	e := compiledNode{SQL: "s", Params: map[string]string{"param_b": "2", "param_a": "1"}}
	require.Equal(t, d.key(), e.key(), "the key is param-order-insensitive")
}

// The lane memo keys on the compiled pair: the same SQL re-executes when a
// signal value moves, and memo-hits when the pair is unchanged.
func TestNodeLaneReExecutesOnSignalChange(t *testing.T) {
	g := &gatedExecutor{gate: make(chan struct{}), build: func(string) arrow.RecordBatch { return int64Rec("n", 1) }}
	close(g.gate) // never block — this test exercises the memo key, not gating
	lane := newNodeLane(g, memory.NewGoAllocator(), 0)
	defer lane.close()

	settle := func(c compiledNode, wantCalls int) {
		require.Eventually(t, func() bool {
			v := lane.demand(c)
			if v.rec != nil {
				v.rec.Release()
			}
			return !v.loading && g.callCount() == wantCalls
		}, 2*time.Second, time.Millisecond)
	}

	c1 := compiledNode{SQL: "SELECT {x:UInt64}", Params: map[string]string{"param_x": "1"}}
	settle(c1, 1)
	settle(c1, 1) // unchanged pair ⇒ memo hit, no new wire call

	c2 := compiledNode{SQL: "SELECT {x:UInt64}", Params: map[string]string{"param_x": "2"}}
	settle(c2, 2) // same SQL, moved signal ⇒ re-execute
}

// captureServer serves a fixed one-column Arrow stream and records each
// request's URL query values.
func captureServer(t *testing.T) (srv *httptest.Server, got func() []url.Values) {
	t.Helper()
	stream := arrowStreamBytes(t, []int64{1})
	var mu sync.Mutex
	var seen []url.Values
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Query())
		mu.Unlock()
		w.Header().Set("X-ClickHouse-Summary", `{"read_rows":"1","read_bytes":"8"}`)
		_, _ = w.Write(stream)
	}))
	got = func() []url.Values {
		mu.Lock()
		defer mu.Unlock()
		out := make([]url.Values, len(seen))
		copy(out, seen)
		return out
	}
	return
}

// Signal values ride the param_* URL channel; a SET-bound name shadows a
// same-named signal (D1: a SET pins a signal into a constant).
func TestExecuteArrowStreamShipsSignalsAndSetShadows(t *testing.T) {
	srv, got := captureServer(t)
	defer srv.Close()
	c := NewClient(ClientConfig{URL: srv.URL}, srv.Client())

	sql := "SET param_y = 'bound'; SELECT {x:UInt64}, {y:String}"
	rdr, closer, _, err := c.ExecuteArrowStream(context.Background(), sql, memory.NewGoAllocator(), nil,
		map[string]string{"param_x": "42", "param_y": "signal"})
	require.NoError(t, err)
	rdr.Release()
	_ = closer.Close()

	qs := got()
	require.Len(t, qs, 1)
	require.Equal(t, "42", qs[0].Get("param_x"), "the signal value rides the param_* URL channel")
	require.Equal(t, "bound", qs[0].Get("param_y"), "the SET-bound constant shadows the same-named signal (D1)")
}

// A run's signal values are snapshotted into its history entry (D4).
func TestQueryStoreSnapshotsSignalsIntoHistory(t *testing.T) {
	srv, got := captureServer(t)
	defer srv.Close()
	store := NewQueryStore(NewClient(ClientConfig{URL: srv.URL}, srv.Client()), memory.NewGoAllocator(), 10, "sigtest")
	defer store.Close()

	store.Execute("SELECT {x:UInt64}", map[string]string{"param_x": "7"})
	require.Eventually(t, func() bool { return !store.IsLoading() }, 2*time.Second, time.Millisecond)

	h := store.History()
	require.Len(t, h, 1)
	require.Equal(t, map[string]string{"param_x": "7"}, h[0].SigParams, "history snapshots the run's signal inputs (D4)")
	qs := got()
	require.Len(t, qs, 1)
	require.Equal(t, "7", qs[0].Get("param_x"))
}

// resolveRunSignals resolves only the UNBOUND slots (a SET pins — D1), only
// for names the store holds; a parse failure resolves nothing (the
// raw-fallback path defers to the server).
func TestResolveRunSignalsResolvesUnboundOnly(t *testing.T) {
	app := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "")
	app.graph.setSignalRaw("x", "41")
	app.graph.setSignalRaw("y", "shadowed") // y is SET-bound below — never consulted
	app.frameSig = app.graph.signals()

	sig, bound := app.resolveRunSignals("SET param_y = 1; SELECT {x:UInt64}, {y:UInt64}, {z:UInt64}")
	require.Equal(t, map[string]string{"param_x": "41"}, sig,
		"x resolves from the store; y is pinned by its SET; z has no store value")
	require.Equal(t, map[string]bool{"y": true}, bound)

	sigBad, boundBad := app.resolveRunSignals("this is not sql")
	require.Nil(t, sigBad)
	require.Nil(t, boundBad)
}

// The staleness witness gains its signal half (D2): a referenced signal that
// moved since the run flips the state to the *Stale twin, and moving back
// clears it — symmetric with a buffer edit and its revert.
func TestObserveQueryStateSignalStaleness(t *testing.T) {
	app := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "")
	app.sql = "SELECT {x:UInt64}"
	app.lastSentSql = "SELECT {x:UInt64}"
	app.paramSlots = []paramSlot{{Name: "x", Type: "UInt64"}}
	app.lastSentSigParams = map[string]string{"param_x": "1"}
	app.graph.setSignalRaw("x", "1")
	app.frameSig = app.graph.signals()
	executed := time.Now()

	require.Equal(t, queryStateRows, app.observeQueryState(false, 5, executed, nil),
		"resolution matches the run ⇒ fresh")

	app.graph.setSignalRaw("x", "2")
	app.frameSig = app.graph.signals()
	require.Equal(t, queryStateRowsStale, app.observeQueryState(false, 5, executed, nil),
		"a moved referenced signal ⇒ stale (D2)")

	app.graph.setSignalRaw("x", "1")
	app.frameSig = app.graph.signals()
	require.Equal(t, queryStateRows, app.observeQueryState(false, 5, executed, nil),
		"moving back clears the staleness (revert symmetry)")
}

// Restoring a history entry seeds the store with the run's signal values (D4).
func TestRestoreHistoryEntrySeedsSignalStore(t *testing.T) {
	app := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "-- initial")
	app.restoreHistoryEntry(HistoryEntry{
		SQL:       "SELECT {x:UInt64}",
		SigParams: map[string]string{"param_x": "7"},
	})
	require.Equal(t, "SELECT {x:UInt64}", app.sql)
	p, found := app.graph.signals().Get("x")
	require.True(t, found, "restore seeds the signal store")
	require.Equal(t, "7", p.Raw)
}

// An observed intermediate resolves its Reads against the frame snapshot and
// ships them on the wire (slice 5a on the intermediate lane).
func TestActiveSnapshotResolvesIntermediateSignals(t *testing.T) {
	srv, got := captureServer(t)
	defer srv.Close()
	client := NewClient(ClientConfig{URL: srv.URL}, srv.Client())
	app := NewPlayApp(client, newLiveQueryGraph(client, memory.NewGoAllocator(), 10), "")
	app.currentSplit = splitResult{
		Nodes: []splitNode{
			{ID: "recent", Kind: splitNodeCTE, SQL: "SELECT n FROM t WHERE n = {x:UInt64}", Reads: []SignalID{"x"}},
			{ID: mainNodeID, Kind: splitNodeStatement, SQL: "WITH recent AS (SELECT n FROM t WHERE n = {x:UInt64}) SELECT * FROM recent"},
		},
		Sink: mainNodeID,
	}
	app.observedNode = "recent"
	app.graph.setSignalRaw("x", "9")
	app.frameSig = app.graph.signals()

	rec, _, _, _, _, _, _, _ := app.activeSnapshot()
	if rec != nil {
		rec.Release()
	}
	require.Eventually(t, func() bool { return len(got()) == 1 }, 2*time.Second, time.Millisecond)
	require.Equal(t, "9", got()[0].Get("param_x"), "the intermediate's Reads resolve from the store and ride the URL")
	app.Close()
}
