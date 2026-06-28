package play

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// waitLaneReady polls demand until the lane has settled on wantSQL.
func waitLaneReady(t *testing.T, lane *nodeLane, wantSQL string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rec, _, sql, loading, _ := lane.demand(wantSQL)
		if rec != nil {
			rec.Release()
		}
		if !loading && sql == wantSQL {
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

func (inst *gatedExecutor) execute(ctx context.Context, sql string, alloc memory.Allocator) (rec arrow.RecordBatch, schema *arrow.Schema, err error) {
	inst.mu.Lock()
	inst.calls++
	inst.mu.Unlock()
	<-inst.gate
	if ctx.Err() != nil {
		err = ctx.Err()
		return
	}
	rec = inst.build(sql)
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
	lane := newNodeLane(clientExecutor{client: NewClient(ClientConfig{URL: srv.URL}, srv.Client())}, memory.NewGoAllocator())
	defer lane.close()

	rec, _, _, loading, _ := lane.demand("SELECT n FROM t")
	require.Nil(t, rec, "first demand is non-blocking — no result yet")
	require.True(t, loading)
	if rec != nil {
		rec.Release()
	}

	waitLaneReady(t, lane, "SELECT n FROM t")
	rec, _, sql, loading, err := lane.demand("SELECT n FROM t")
	require.NoError(t, err)
	require.False(t, loading)
	require.NotNil(t, rec)
	defer rec.Release()
	require.Equal(t, int64(3), rec.NumRows())
	require.Equal(t, "SELECT n FROM t", sql)
	require.Equal(t, 1, *hits)

	r2, _, _, _, _ := lane.demand("SELECT n FROM t")
	if r2 != nil {
		r2.Release()
	}
	require.Equal(t, 1, *hits, "unchanged SQL must be a memo hit")
}

func TestNodeLaneReExecutesOnSqlChange(t *testing.T) {
	srv, hits := arrowServer(t, []int64{7})
	defer srv.Close()
	lane := newNodeLane(clientExecutor{client: NewClient(ClientConfig{URL: srv.URL}, srv.Client())}, memory.NewGoAllocator())
	defer lane.close()

	r, _, _, _, _ := lane.demand("SELECT 1")
	if r != nil {
		r.Release()
	}
	waitLaneReady(t, lane, "SELECT 1")
	require.Equal(t, 1, *hits)

	r, _, _, _, _ = lane.demand("SELECT 2")
	if r != nil {
		r.Release()
	}
	waitLaneReady(t, lane, "SELECT 2")
	rec, _, sql, _, _ := lane.demand("SELECT 2")
	if rec != nil {
		rec.Release()
	}
	require.Equal(t, "SELECT 2", sql)
	require.Equal(t, 2, *hits, "a changed SQL re-executes")
}

// Non-blocking + last-good: while a new run is in flight, demand returns the
// prior result immediately (no flicker).
func TestNodeLaneNonBlockingAndLastGood(t *testing.T) {
	g := &gatedExecutor{gate: make(chan struct{}), build: func(string) arrow.RecordBatch { return int64Rec("n", 1) }}
	lane := newNodeLane(g, memory.NewGoAllocator())
	defer lane.close()

	rec, _, _, loading, _ := lane.demand("sql1")
	require.Nil(t, rec)
	require.True(t, loading)

	g.gate <- struct{}{} // release sql1's execute
	waitLaneReady(t, lane, "sql1")

	// supersede with sql2 (gated): the last-good (sql1) is returned while loading.
	r1, _, _, _, _ := lane.demand("sql2")
	if r1 != nil {
		r1.Release()
	}
	rec, _, sql, loading, _ := lane.demand("sql2")
	require.NotNil(t, rec, "last-good retained during supersede")
	require.Equal(t, "sql1", sql, "still sql1's result while sql2 loads")
	require.True(t, loading)
	rec.Release()

	g.gate <- struct{}{} // release sql2's execute
	waitLaneReady(t, lane, "sql2")
	rec, _, sql, loading, _ = lane.demand("sql2")
	require.NotNil(t, rec)
	rec.Release()
	require.Equal(t, "sql2", sql)
	require.False(t, loading)
}

// A changed SQL while a run is in flight cancels (supersedes) it; the superseded
// result is discarded and the new one wins.
func TestNodeLaneSupersedesInFlight(t *testing.T) {
	g := &gatedExecutor{gate: make(chan struct{}), build: func(string) arrow.RecordBatch { return int64Rec("n", 9) }}
	lane := newNodeLane(g, memory.NewGoAllocator())
	defer lane.close()

	r, _, _, _, _ := lane.demand("sql1") // in flight (gated)
	if r != nil {
		r.Release()
	}
	r, _, _, _, _ = lane.demand("sql2") // cancels sql1 in flight, starts sql2
	if r != nil {
		r.Release()
	}

	// Release both executes (order-independent): sql1's is cancelled and
	// discarded, sql2's wins.
	g.gate <- struct{}{}
	g.gate <- struct{}{}
	waitLaneReady(t, lane, "sql2")

	rec, _, sql, _, _ := lane.demand("sql2")
	require.NotNil(t, rec)
	rec.Release()
	require.Equal(t, "sql2", sql)
	require.Equal(t, 2, g.callCount(), "both sql1 (cancelled) and sql2 started")
}
