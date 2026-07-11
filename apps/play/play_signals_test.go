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
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
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

	sig, bound, unfilled := app.resolveRunSignals("SET param_y = 1; SELECT {x:UInt64}, {y:UInt64}, {z:UInt64}")
	require.Equal(t, map[string]string{"param_x": "41"}, sig,
		"x resolves from the store; y is pinned by its SET; z has no store value")
	require.Equal(t, map[string]bool{"y": true}, bound)
	require.Equal(t, []string{"z"}, unfilled,
		"z is referenced but neither SET-bound nor signal-written (5e)")

	sigBad, boundBad, unfilledBad := app.resolveRunSignals("this is not sql")
	require.Nil(t, sigBad)
	require.Nil(t, boundBad)
	require.Nil(t, unfilledBad, "the raw-fallback path reports nothing unfilled — the server decides")
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

// sigWith returns a live store snapshot carrying the selection signal — the
// slice-5b replacement for the retired playSignals test stub: panel tests
// exercise the same env implementation production uses.
func sigWith(selection int64) SignalEnvI {
	g := newQueryGraph(nil, nil)
	g.setSignalRaw(signalSelection, strconv.FormatInt(selection, 10))
	return g.signals()
}

// sigNone returns an empty live store snapshot (no signals set) — the
// replacement for the retired emptySignals stub.
func sigNone() SignalEnvI {
	return newQueryGraph(nil, nil).signals()
}

// encodeSignalValue covers the emit-value vocabulary; unsupported types
// report ok=false (the emitter drops them).
func TestEncodeSignalValue(t *testing.T) {
	for _, tc := range []struct {
		in  any
		raw string
		ok  bool
	}{
		{"s", "s", true},
		{int(3), "3", true},
		{int64(-4), "-4", true},
		{uint64(5), "5", true},
		{float64(1.5), "1.5", true},
		{true, "1", true},
		{false, "0", true},
		{struct{}{}, "", false},
	} {
		raw, ok := encodeSignalValue(tc.in)
		require.Equal(t, tc.ok, ok)
		require.Equal(t, tc.raw, raw)
	}
}

// syncSelectionClamp (slice 5b, replacing the selectedRow field clamp) resets
// an absent or out-of-range selection to row 0 and leaves an in-range one
// untouched — no store-revision churn on the steady state.
func TestSyncSelectionClamp(t *testing.T) {
	app := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "")
	rec := int64Rec("n", 1, 2, 3)
	defer rec.Release()

	app.frameSig = app.graph.signals() // absent selection
	app.syncSelectionClamp(rec)
	got, found := readSelection(app.graph.signals())
	require.True(t, found)
	require.Equal(t, int64(0), got, "absent selection clamps to row 0 (auto-select the first row)")

	app.graph.setSignalRaw(signalSelection, "7") // out of range for 3 rows
	app.frameSig = app.graph.signals()
	app.syncSelectionClamp(rec)
	got, _ = readSelection(app.graph.signals())
	require.Equal(t, int64(0), got, "out-of-range selection clamps to row 0")

	app.graph.setSignalRaw(signalSelection, "2") // in range
	app.frameSig = app.graph.signals()
	rev := app.graph.signals().Revision()
	app.syncSelectionClamp(rec)
	require.Equal(t, rev, app.graph.signals().Revision(), "an in-range selection writes nothing")
	got, _ = readSelection(app.graph.signals())
	require.Equal(t, int64(2), got)
}

// The 5c Map seam end to end: the emitted viewport compiles into vp_* URL
// params against a STABLE SQL text; a pan changes only the params, and the
// lane's (SQL, params) key re-executes. This is the ADR-0096 SD6 contract
// ("pan/zoom become typed param mutations") verified on the wire.
func TestMapViewportSeamEndToEnd(t *testing.T) {
	srv, got := captureServer(t)
	defer srv.Close()
	client := NewClient(ClientConfig{URL: srv.URL}, srv.Client())
	g := newQueryGraph(nil, nil)
	d := NewMapDriver(nil, client)
	d.lane.close()
	d.lane = newNodeLane(clientExecutor{client: client}, memory.NewGoAllocator(), 0)
	defer d.lane.close()

	demandSettled := func(wantHits int) {
		require.Eventually(t, func() bool {
			params := resolveSignalNames(d.templateReads, nil, g.signals())
			if !hasViewportParams(params) {
				return false
			}
			v := d.lane.demand(compiledNode{SQL: d.template, Params: params})
			if v.rec != nil {
				v.rec.Release()
			}
			return !v.loading && len(got()) == wantHits
		}, 2*time.Second, time.Millisecond)
	}

	// First settle: London.
	d.updateViewport(51.3, 51.7, -0.6, 0.3, 320, 240, graphEmitter{graph: g})
	firstSQL := d.template
	demandSettled(1)
	qs := got()
	b1, _ := bboxFromLatLon(51.3, 51.7, -0.6, 0.3)
	require.Equal(t, strconv.FormatUint(uint64(b1.minX), 10), qs[0].Get("param_vp_min_x"),
		"the viewport rides the param_* URL channel")
	require.Equal(t, "320", qs[0].Get("param_vp_w"))

	// Pan to Paris: the SQL text must be unchanged; only the params move.
	d.updateViewport(48.6, 49.1, 1.9, 2.9, 320, 240, graphEmitter{graph: g})
	require.Equal(t, firstSQL, d.template, "a pan never changes the SQL text")
	demandSettled(2)
	qs = got()
	b2, _ := bboxFromLatLon(48.6, 49.1, 1.9, 2.9)
	require.Equal(t, strconv.FormatUint(uint64(b2.minX), 10), qs[1].Get("param_vp_min_x"),
		"the pan re-executed with the new viewport params")
}

// --- Slice-5e regression tests: signal provenance, the Signals chrome's row
// model, the unfilled-input Run gate (D3), and the `main` live toggle (D2). ---

// Every write records who last CHANGED the value; deduplicated re-sets update
// neither value nor provenance; an unstamped write records as "app".
func TestSignalStoreTracksWriterAndRevision(t *testing.T) {
	g := newQueryGraph(nil, nil)
	graphEmitter{graph: g, writer: "table"}.Emit("x", int64(1))
	rows := g.signalRows()
	require.Len(t, rows, 1)
	require.Equal(t, signalRow{Name: "x", Raw: "1", Writer: "table", Rev: 1}, rows[0])

	graphEmitter{graph: g, writer: "projection"}.Emit("x", int64(1))
	require.Equal(t, uint64(1), g.signalRows()[0].Rev, "unchanged value ⇒ no revision bump")
	require.Equal(t, "table", g.signalRows()[0].Writer, "unchanged value ⇒ provenance keeps the changer")

	graphEmitter{graph: g}.as("projection").Emit("x", int64(2))
	require.Equal(t, signalRow{Name: "x", Raw: "2", Writer: "projection", Rev: 2}, g.signalRows()[0])

	g.setSignalRaw("y", "raw")
	rows = g.signalRows()
	require.Len(t, rows, 2)
	require.Equal(t, "app", rows[1].Writer, "unstamped writes record as app")
	require.Equal(t, "x", rows[0].Name, "rows are name-sorted")
}

// deleteSignal frees the name and bumps the revision (referencing nodes go
// stale/unfilled); deleting an absent name is revision-free.
func TestDeleteSignalRemovesAndBumps(t *testing.T) {
	g := newQueryGraph(nil, nil)
	g.setSignalRaw("x", "1")
	rev := g.signals().Revision()

	g.deleteSignal("x")
	_, held := g.signals().Get("x")
	require.False(t, held)
	require.Empty(t, g.signalRows())
	require.Equal(t, rev+1, g.signals().Revision(), "deletion is a store change")

	g.deleteSignal("x")
	require.Equal(t, rev+1, g.signals().Revision(), "deleting an absent name is a no-op")
}

// stampProbePanel is a minimal PanelI whose Render emits — for asserting the
// dispatcher's writer stamping.
type stampProbePanel struct{}

func (stampProbePanel) ID() PanelID             { return "probe" }
func (stampProbePanel) Channels() []ChannelSpec { return []ChannelSpec{{ID: chMain, Required: true}} }
func (stampProbePanel) AcceptForChannel(ChannelID, *arrow.Schema, SignalEnvI) (ChannelClaim, string) {
	return true, ""
}
func (stampProbePanel) Render(_ map[ChannelID]ChannelResult, emit SignalEmitterI) {
	emit.Emit("stamped", int64(7))
}

// dispatchPanel stamps the panel's ID onto an unstamped store emitter, so
// panel writes carry provenance without any per-call-site plumbing.
func TestDispatchPanelStampsEmitterWriter(t *testing.T) {
	g := newQueryGraph(nil, nil)
	reject := dispatchPanel(stampProbePanel{}, map[ChannelID]channelInput{chMain: {}}, graphEmitter{graph: g})
	require.Empty(t, reject)
	rows := g.signalRows()
	require.Len(t, rows, 1)
	require.Equal(t, "probe", rows[0].Writer)
}

// collectSlotTypes keeps every occurrence's type (no dedup by name): the
// cross-node conflict signal the chrome warns on.
func TestCollectSlotTypesConflicts(t *testing.T) {
	pr, err := nanopass.Parse("SELECT {x:Int64}, {x:String}, {x:Int64}, {y:UInt8}")
	require.NoError(t, err)
	types := collectSlotTypes(pr)
	require.Equal(t, []string{"Int64", "String"}, types["x"], "distinct types in occurrence order")
	require.Equal(t, []string{"UInt8"}, types["y"])
}

// The chrome row model: held ∪ referenced, with pinned (D1), unfilled (D3),
// provenance, and the reserved-type conflict check.
func TestCollectSignalChromeRows(t *testing.T) {
	app := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "")
	sql := "SET param_a = 1;\nSELECT {a:Int64}, {b:UInt32}, {c:String}, {vp_w:String}"
	app.sql = sql
	app.formattedFor = sql // the debounce has settled — the type table may parse
	slots, vals, err := extractSlotsAndParams(sql)
	require.NoError(t, err)
	app.refreshParamSlotsFromParse(slots, vals)
	app.graph.setSignalRawFrom("b", "7", signalWriterMap)
	app.frameSig = app.graph.signals()

	rows := app.collectSignalChrome()
	byName := make(map[string]signalChromeRow, len(rows))
	for _, r := range rows {
		byName[r.Name] = r
	}
	require.Len(t, rows, 4)

	a := byName["a"]
	require.True(t, a.Pinned, "SET-bound ⇒ pinned (D1)")
	require.False(t, a.Unfilled, "a SET fills the input")
	require.False(t, a.Held)
	require.Equal(t, []string{"Int64"}, a.Types)

	b := byName["b"]
	require.True(t, b.Held)
	require.Equal(t, "7", b.Raw)
	require.Equal(t, signalWriterMap, b.Writer)
	require.False(t, b.Unfilled)
	require.False(t, b.Conflict)

	cRow := byName["c"]
	require.True(t, cRow.Unfilled, "referenced, neither bound nor held")

	vpw := byName["vp_w"]
	require.True(t, vpw.Conflict, "buffer String vs reserved UInt32")
	require.ElementsMatch(t, []string{"String", "UInt32"}, vpw.Types)
	require.True(t, vpw.Unfilled)
}

// unfilledInputs / hasUnboundSlots read the debounced caches + frame
// snapshot: bound names and held names are filled; the rest are not.
func TestUnfilledInputsFromCaches(t *testing.T) {
	app := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "")
	app.paramSlots = []paramSlot{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	app.paramSyncedValues = map[string]string{"a": "1"}
	app.graph.setSignalRaw("b", "2")
	app.frameSig = app.graph.signals()

	require.Equal(t, []string{"c"}, app.unfilledInputs())
	require.True(t, app.hasUnboundSlots(), "b and c are unbound (a SET pins only a)")

	app.paramSyncedValues = map[string]string{"a": "1", "b": "2", "c": "3"}
	require.Empty(t, app.unfilledInputs())
	require.False(t, app.hasUnboundSlots())
}

// The live toggle's decision, gate by gate (D2): only a signal move on an
// unchanged, fully-filled, non-observing, settled buffer re-runs.
func TestShouldAutoRunGates(t *testing.T) {
	mk := func() *PlayApp {
		app := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "")
		app.sql = "SELECT {x:Int64} AS v"
		app.lastSentSql = "SELECT {x:Int64} AS v"
		app.paramSlots = []paramSlot{{Name: "x", Type: "Int64"}}
		app.lastSentSigParams = map[string]string{"param_x": "1"}
		app.graph.setSignalRaw("x", "2") // moved since the run
		app.frameSig = app.graph.signals()
		app.liveMain = true
		return app
	}
	require.True(t, mk().shouldAutoRun(), "baseline: live + diverged + filled + unchanged buffer")

	off := mk()
	off.liveMain = false
	require.False(t, off.shouldAutoRun(), "toggle off")

	pending := mk()
	pending.requestRun = true
	require.False(t, pending.shouldAutoRun(), "a run is already requested")

	neverRan := mk()
	neverRan.lastSentSql = ""
	require.False(t, neverRan.shouldAutoRun(), "live means re-run, not first-run")

	edited := mk()
	edited.sql += " -- edited"
	require.False(t, edited.shouldAutoRun(), "buffer edits stay Run-gated")

	observing := mk()
	observing.currentSplit = splitResult{Sink: "main"}
	observing.observedNode = "cte1"
	require.False(t, observing.shouldAutoRun(), "observed intermediates already re-drive on their lane")

	unfilled := mk()
	unfilled.paramSlots = append(unfilled.paramSlots, paramSlot{Name: "z", Type: "String"})
	require.False(t, unfilled.shouldAutoRun(), "an unfilled input blocks exactly as it blocks a manual Run")

	settled := mk()
	settled.lastSentSigParams = map[string]string{"param_x": "2"}
	require.False(t, settled.shouldAutoRun(), "no divergence ⇒ no run")
}

// executeRun refuses a doomed request (unfilled input, D3) with an actionable
// reason, and runs it once the input is written.
func TestExecuteRunBlockedOnUnfilledThenRuns(t *testing.T) {
	srv, got := captureServer(t)
	defer srv.Close()
	client := NewClient(ClientConfig{URL: srv.URL}, srv.Client())
	app := NewPlayApp(client, newLiveQueryGraph(client, memory.NewGoAllocator(), 10), "")
	defer app.graph.close()
	app.sql = "SELECT {x:Int64} AS v"
	app.frameSig = app.graph.signals()

	app.executeRun(false)
	require.Contains(t, app.runBlockedReason, "{x}")
	require.Empty(t, got(), "no request goes to the server")
	require.Empty(t, app.lastSentSql, "a blocked run records nothing as sent")

	app.graph.setSignalRawFrom("x", "3", signalWriterEditor)
	app.frameSig = app.graph.signals()
	app.executeRun(false)
	require.Empty(t, app.runBlockedReason)
	require.Eventually(t, func() bool { return len(got()) == 1 }, 2*time.Second, time.Millisecond)
	require.Equal(t, "3", got()[0].Get("param_x"))
	require.Equal(t, map[string]string{"param_x": "3"}, app.lastSentSigParams)
}

// The live loop end to end: run, move a referenced signal, the toggle fires
// the same Run path with the new value, and the divergence clears.
func TestAutoRunLoopOnSignalDivergence(t *testing.T) {
	srv, got := captureServer(t)
	defer srv.Close()
	client := NewClient(ClientConfig{URL: srv.URL}, srv.Client())
	app := NewPlayApp(client, newLiveQueryGraph(client, memory.NewGoAllocator(), 10), "")
	defer app.graph.close()
	app.sql = "SELECT {x:Int64} AS v"
	app.paramSlots = []paramSlot{{Name: "x", Type: "Int64"}} // the debounced cache
	app.liveMain = true

	app.graph.setSignalRawFrom("x", "1", signalWriterEditor)
	app.frameSig = app.graph.signals()
	app.executeRun(false) // the first run is manual — live means re-run
	require.Eventually(t, func() bool { return len(got()) == 1 && !app.graph.MainLoading() },
		2*time.Second, time.Millisecond)
	require.False(t, app.shouldAutoRun(), "freshly run, nothing diverged")

	app.graph.setSignalRawFrom("x", "2", signalWriterEditor)
	app.frameSig = app.graph.signals()
	require.True(t, app.shouldAutoRun(), "a referenced signal moved")
	app.executeRun(true)
	require.Eventually(t, func() bool { return len(got()) == 2 && !app.graph.MainLoading() },
		2*time.Second, time.Millisecond)
	require.Equal(t, "2", got()[1].Get("param_x"), "the re-run ships the moved value")

	app.frameSig = app.graph.signals()
	require.False(t, app.shouldAutoRun(), "the divergence cleared with the run")
}
