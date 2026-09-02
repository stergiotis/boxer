package play

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stretchr/testify/require"
)

// waitLaneReady polls demand until the lane has settled on wantSQL.
func waitLaneReady(t *testing.T, lane *nodeLane, wantSQL string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		view := lane.demand(compiledNode{SQL: wantSQL})
		if view.rec != nil {
			view.rec.Release()
		}
		if !view.loading && view.sql == wantSQL {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("lane not ready on %q after 3s", wantSQL)
}

// gatedExecutor blocks each execute on a shared gate, then honours ctx
// cancellation — letting a test drive completion and supersession deterministically.
type gatedExecutor struct {
	gate  chan struct{}
	build func(sql string) arrow.RecordBatch
	mu    sync.Mutex
	calls int
}

func (inst *gatedExecutor) execute(ctx context.Context, c compiledNode, alloc memory.Allocator) (rec arrow.RecordBatch, schema *arrow.Schema, summary Summary, err error) {
	inst.mu.Lock()
	inst.calls++
	inst.mu.Unlock()
	<-inst.gate
	if ctx.Err() != nil {
		err = ctx.Err()
		return
	}
	rec = inst.build(c.SQL)
	schema = rec.Schema()
	return
}

func (inst *gatedExecutor) callCount() int {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.calls
}

// demand triggers async execution; the result lands after completion, and an
// unchanged SQL is a memo hit (one wire hit).
func TestNodeLaneExecutesOnDemand(t *testing.T) {
	srv, hits := arrowServer(t, []int64{1, 2, 3})
	defer srv.Close()
	lane := newNodeLane(clientExecutor{client: NewClient(ClientConfig{URL: srv.URL}, srv.Client())}, memory.NewGoAllocator(), 0)
	defer lane.close()

	view := lane.demand(compiledNode{SQL: "SELECT n FROM t"})
	require.Nil(t, view.rec, "first demand is non-blocking — no result yet")
	require.True(t, view.loading)

	waitLaneReady(t, lane, "SELECT n FROM t")
	view = lane.demand(compiledNode{SQL: "SELECT n FROM t"})
	require.NoError(t, view.err)
	require.False(t, view.loading)
	require.NotNil(t, view.rec)
	defer view.rec.Release()
	require.Equal(t, int64(3), view.rec.NumRows())
	require.Equal(t, "SELECT n FROM t", view.sql)
	require.NotZero(t, view.fingerprint)
	require.Equal(t, 1, *hits)

	v2 := lane.demand(compiledNode{SQL: "SELECT n FROM t"})
	if v2.rec != nil {
		v2.rec.Release()
	}
	require.Equal(t, 1, *hits, "unchanged SQL must be a memo hit")
	require.Equal(t, view.fingerprint, v2.fingerprint, "memo hit serves the same content")
}

func TestNodeLaneReExecutesOnSqlChange(t *testing.T) {
	srv, hits := arrowServer(t, []int64{7})
	defer srv.Close()
	lane := newNodeLane(clientExecutor{client: NewClient(ClientConfig{URL: srv.URL}, srv.Client())}, memory.NewGoAllocator(), 0)
	defer lane.close()

	v := lane.demand(compiledNode{SQL: "SELECT 1"})
	if v.rec != nil {
		v.rec.Release()
	}
	waitLaneReady(t, lane, "SELECT 1")
	require.Equal(t, 1, *hits)

	v = lane.demand(compiledNode{SQL: "SELECT 2"})
	if v.rec != nil {
		v.rec.Release()
	}
	waitLaneReady(t, lane, "SELECT 2")
	v = lane.demand(compiledNode{SQL: "SELECT 2"})
	if v.rec != nil {
		v.rec.Release()
	}
	require.Equal(t, "SELECT 2", v.sql)
	require.Equal(t, 2, *hits, "a changed SQL re-executes")
}

// Non-blocking + last-good: while a new run is in flight, demand returns the
// prior result immediately (no flicker).
func TestNodeLaneNonBlockingAndLastGood(t *testing.T) {
	g := &gatedExecutor{gate: make(chan struct{}), build: func(string) arrow.RecordBatch { return int64Rec("n", 1) }}
	lane := newNodeLane(g, memory.NewGoAllocator(), 0)
	defer lane.close()

	view := lane.demand(compiledNode{SQL: "sql1"})
	require.Nil(t, view.rec)
	require.True(t, view.loading)

	g.gate <- struct{}{} // release sql1's execute
	waitLaneReady(t, lane, "sql1")

	// supersede with sql2 (gated): the last-good (sql1) is returned while loading.
	v1 := lane.demand(compiledNode{SQL: "sql2"})
	if v1.rec != nil {
		v1.rec.Release()
	}
	view = lane.demand(compiledNode{SQL: "sql2"})
	require.NotNil(t, view.rec, "last-good retained during supersede")
	require.Equal(t, "sql1", view.sql, "still sql1's result while sql2 loads")
	require.True(t, view.loading)
	view.rec.Release()

	g.gate <- struct{}{} // release sql2's execute
	waitLaneReady(t, lane, "sql2")
	view = lane.demand(compiledNode{SQL: "sql2"})
	require.NotNil(t, view.rec)
	view.rec.Release()
	require.Equal(t, "sql2", view.sql)
	require.False(t, view.loading)
}

// A changed SQL while a run is in flight cancels (supersedes) it; the superseded
// result is discarded and the new one wins.
func TestNodeLaneSupersedesInFlight(t *testing.T) {
	g := &gatedExecutor{gate: make(chan struct{}), build: func(string) arrow.RecordBatch { return int64Rec("n", 9) }}
	lane := newNodeLane(g, memory.NewGoAllocator(), 0)
	defer lane.close()

	v := lane.demand(compiledNode{SQL: "sql1"}) // in flight (gated)
	if v.rec != nil {
		v.rec.Release()
	}
	v = lane.demand(compiledNode{SQL: "sql2"}) // cancels sql1 in flight, starts sql2
	if v.rec != nil {
		v.rec.Release()
	}

	// Release both executes (order-independent): sql1's is cancelled and
	// discarded, sql2's wins.
	g.gate <- struct{}{}
	g.gate <- struct{}{}
	waitLaneReady(t, lane, "sql2")

	view := lane.demand(compiledNode{SQL: "sql2"})
	require.NotNil(t, view.rec)
	view.rec.Release()
	require.Equal(t, "sql2", view.sql)
	require.Equal(t, 2, g.callCount(), "both sql1 (cancelled) and sql2 started")
}

// forget discards an in-flight completion: a run that lands AFTER forget must
// not restore the memo, so the next demand of the same SQL re-executes (review
// finding: the completion used to undo the force and the re-fetch memo-hit).
func TestNodeLaneForgetDiscardsInFlightCompletion(t *testing.T) {
	g := &gatedExecutor{gate: make(chan struct{}), build: func(string) arrow.RecordBatch { return int64Rec("n", 1) }}
	lane := newNodeLane(g, memory.NewGoAllocator(), 0)
	defer lane.close()

	v := lane.demand(compiledNode{SQL: "SELECT 1"}) // kicks a run, held on the gate
	require.Nil(t, v.rec)
	require.True(t, v.loading)
	require.Eventually(t, func() bool { return g.callCount() == 1 },
		2*time.Second, time.Millisecond, "run goroutine must reach the executor")

	lane.forget() // the force, clicked mid-flight

	close(g.gate) // the in-flight run completes AFTER forget, before any demand
	require.Never(t, func() bool {
		lane.mu.Lock()
		defer lane.mu.Unlock()
		// A landed completion would restore this. Compare against key(),
		// not the bare SQL: the memo key is a length-prefixed encoding of
		// the (SQL, params) pair, not the SQL text.
		return lane.servedKey == compiledNode{SQL: "SELECT 1"}.key()
	}, 150*time.Millisecond, 5*time.Millisecond,
		"the in-flight completion must be discarded after forget")

	v = lane.demand(compiledNode{SQL: "SELECT 1"}) // same SQL — must re-execute, not memo-hit
	if v.rec != nil {
		v.rec.Release()
	}
	require.Eventually(t, func() bool { return g.callCount() == 2 },
		2*time.Second, time.Millisecond, "post-forget demand must re-execute the unchanged SQL")
	waitLaneReady(t, lane, "SELECT 1")
}

// The fingerprint tracks content, not SQL text: a forced re-fetch of the same
// SQL yields a NEW fingerprint when the data changed and the SAME one when it
// did not — the observers' repack/re-map guard (ADR-0097 SD4 early cutoff).
func TestNodeLaneFingerprintTracksContentAcrossForget(t *testing.T) {
	val := int64(1)
	var mu sync.Mutex
	exec := &gatedExecutor{gate: make(chan struct{}), build: func(string) arrow.RecordBatch {
		mu.Lock()
		defer mu.Unlock()
		return int64Rec("n", val)
	}}
	close(exec.gate) // never block — this test exercises memo/fingerprint, not gating
	lane := newNodeLane(exec, memory.NewGoAllocator(), 0)
	defer lane.close()

	lane.demand(compiledNode{SQL: "SELECT n"})
	waitLaneReady(t, lane, "SELECT n")
	v1 := lane.demand(compiledNode{SQL: "SELECT n"})
	if v1.rec != nil {
		v1.rec.Release()
	}

	lane.forget() // re-fetch, same data
	lane.demand(compiledNode{SQL: "SELECT n"})
	waitLaneReady(t, lane, "SELECT n")
	v2 := lane.demand(compiledNode{SQL: "SELECT n"})
	if v2.rec != nil {
		v2.rec.Release()
	}
	require.Equal(t, v1.fingerprint, v2.fingerprint, "identical bytes ⇒ identical fingerprint (no repack)")

	mu.Lock()
	val = 2 // the source "changed"
	mu.Unlock()
	lane.forget() // re-fetch, new data under the SAME SQL
	lane.demand(compiledNode{SQL: "SELECT n"})
	waitLaneReady(t, lane, "SELECT n")
	v3 := lane.demand(compiledNode{SQL: "SELECT n"})
	if v3.rec != nil {
		v3.rec.Release()
	}
	require.NotEqual(t, v1.fingerprint, v3.fingerprint, "changed bytes ⇒ changed fingerprint (repack fires)")
}

// Flipping the demand back to the SQL the memo already serves — while a
// superseding run is in flight — must serve the memo and cancel the run, not
// re-execute (review finding: A→B→A re-ran A although its memo was current, a
// minimality deviation; on the Map, pan-away-and-back re-ran the raster).
func TestNodeLaneFlipBackServesMemoWithoutReExecuting(t *testing.T) {
	g := &gatedExecutor{gate: make(chan struct{}), build: func(string) arrow.RecordBatch { return int64Rec("n", 1) }}
	lane := newNodeLane(g, memory.NewGoAllocator(), 0)
	defer lane.close()

	v := lane.demand(compiledNode{SQL: "A"})
	require.Nil(t, v.rec)
	g.gate <- struct{}{} // release A's execute
	waitLaneReady(t, lane, "A")

	v = lane.demand(compiledNode{SQL: "B"}) // supersede toward B (its execute is held on the gate)
	if v.rec != nil {
		v.rec.Release()
	}
	require.True(t, v.loading)

	view := lane.demand(compiledNode{SQL: "A"}) // flip back while B is in flight
	require.NotNil(t, view.rec, "the memo for A is current — served immediately")
	view.rec.Release()
	require.Equal(t, "A", view.sql)
	require.False(t, view.loading, "the flip-back must not restart A")

	// B's goroutine reaches the executor (call counted), then the closed gate
	// releases it; its completion carries a stale generation and is discarded.
	require.Eventually(t, func() bool { return g.callCount() == 2 },
		2*time.Second, time.Millisecond, "A and B each executed once — A was NOT re-executed on flip-back")
	close(g.gate)
	require.Never(t, func() bool {
		lane.mu.Lock()
		defer lane.mu.Unlock()
		return lane.servedKey != compiledNode{SQL: "A"}.key()
	}, 150*time.Millisecond, 5*time.Millisecond, "B's cancelled completion must not land")
}

// abort is a cancel, not a force: the run stops, and the per-frame demand that
// follows must NOT restart it — otherwise the Map's Cancel would be a re-run
// button, since the panel re-demands the same pair every frame while the camera
// is still. A changed demand does start again, and so does forget.
func TestNodeLaneAbortStopsWithoutRestarting(t *testing.T) {
	g := &gatedExecutor{gate: make(chan struct{}), build: func(string) arrow.RecordBatch { return int64Rec("n", 1) }}
	lane := newNodeLane(g, memory.NewGoAllocator(), 0)
	defer lane.close()

	v := lane.demand(compiledNode{SQL: "A"})
	require.Nil(t, v.rec)
	require.True(t, v.loading)
	// A parks on the gate, so it is genuinely in flight when the Cancel lands.
	require.Eventually(t, func() bool { return g.callCount() == 1 },
		2*time.Second, time.Millisecond)

	lane.abort()
	close(g.gate) // release A: its ctx is cancelled and its completion discarded

	view := lane.demand(compiledNode{SQL: "A"}) // the frame after the click
	require.False(t, view.loading, "an aborted fetch stays aborted")
	require.Nil(t, view.rec)
	require.Never(t, func() bool {
		v := lane.demand(compiledNode{SQL: "A"}) // every subsequent frame
		if v.rec != nil {
			v.rec.Release()
		}
		return v.loading
	}, 150*time.Millisecond, 5*time.Millisecond, "the unchanged demand must not re-arm the lane")
	require.Equal(t, 1, g.callCount(), "the cancelled run is the only execution")

	// A changed demand (a pan) is a new pair, so it runs.
	v = lane.demand(compiledNode{SQL: "B"})
	require.True(t, v.loading)
	waitLaneReady(t, lane, "B")
	require.Equal(t, 2, g.callCount())

	// And forget re-arms the lane the abort left converged (Refresh after a
	// Cancel), even for a pair the memo now serves.
	lane.forget()
	v = lane.demand(compiledNode{SQL: "B"})
	if v.rec != nil {
		v.rec.Release()
	}
	require.True(t, v.loading)
	waitLaneReady(t, lane, "B")
	require.Equal(t, 3, g.callCount())
}

// A closed lane drops demands instead of resurrecting: a straggler frame during
// Unmount must not start a query nothing will consume (review finding —
// QueryStore had this guard, the lane didn't).
func TestNodeLaneClosedDropsDemandsAndForgets(t *testing.T) {
	g := &gatedExecutor{gate: make(chan struct{}), build: func(string) arrow.RecordBatch { return int64Rec("n", 1) }}
	lane := newNodeLane(g, memory.NewGoAllocator(), 0)
	lane.close()

	view := lane.demand(compiledNode{SQL: "SELECT 1"})
	require.Nil(t, view.rec)
	require.False(t, view.loading)
	require.Equal(t, 0, g.callCount(), "no execution starts on a closed lane")

	lane.forget() // no-op on a closed lane — must not cancel or re-arm anything
	view = lane.demand(compiledNode{SQL: "SELECT 1"})
	require.False(t, view.loading)
	require.Equal(t, 0, g.callCount())
	lane.close() // idempotent
}

// A node the split does not hold has no executable form, and the lane must not
// send one. [fuseNode] returns "" for such an id; shipping what it used to
// return — the SET prelude on its own — reached the server as an empty body and
// came back as `syntax error: 1:0: mismatched input '<EOF>'`, a parse error
// naming neither the node nor the buffer. The rejection is the chokepoint: it
// covers the call sites that resolve the node first AND the one that compiles a
// user-written `ts*` input.
func TestNodeLaneRefusesEmptySQL(t *testing.T) {
	g := &gatedExecutor{gate: make(chan struct{}), build: func(string) arrow.RecordBatch { return nil }}
	lane := newNodeLane(g, memory.NewGoAllocator(), 0)
	defer lane.close()

	view := lane.demand(compiledNode{SQL: "", NodeID: "flows"})
	require.Error(t, view.err)
	require.Equal(t, "flows", ebtest.Fields(t, view.err)["nodeID"], "the error names the node, not the SQL")
	require.Contains(t, view.err.Error(), "not in this buffer")
	require.False(t, view.loading)
	require.Nil(t, view.rec)
	require.Zero(t, g.callCount(), "nothing reaches the executor")

	// Whitespace is the same nothing.
	view = lane.demand(compiledNode{SQL: "  \n\t", NodeID: "nodes"})
	require.Error(t, view.err)
	require.Zero(t, g.callCount())
}
