package play

import (
	"context"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// play_graph_lane.go is ADR-0097 slice 3b: the suspending async execution lane
// for a self-executed node (SD5). demand() never blocks the render thread: it
// returns the last-good result immediately and converges the lane toward
// executing the requested compiled SQL — a changed SQL supersedes the in-flight
// run (generation-tagged, latest wins) and the last-good result is retained
// until the new one lands (no flicker). The `main` node keeps its QueryStore
// lane (Run-triggered, with history); this lane is for demand-triggered
// self-executed nodes (the splitter's, once they execute separately in slice 3d)
// and is exercised by tests until then.

type nodeLane struct {
	exec    nodeExecutorI
	alloc   memory.Allocator
	timeout time.Duration // per-execution timeout (0 = none); the Map's remote sources need ~60s

	mu        sync.Mutex
	result    *nodeResult // last-good (owned); retained across a successful supersede
	servedSQL string      // the SQL `result` was computed for
	wantSQL   string      // the SQL currently desired (latest demanded)
	loading   bool
	gen       uint64 // bumped on each start; a stale (superseded) completion is discarded
	cancel    context.CancelFunc
}

func newNodeLane(exec nodeExecutorI, alloc memory.Allocator, timeout time.Duration) (inst *nodeLane) {
	if alloc == nil {
		alloc = memory.NewGoAllocator()
	}
	inst = &nodeLane{exec: exec, alloc: alloc, timeout: timeout}
	return
}

// demand requests the node's result for compiledSQL, non-blocking. It returns a
// retained snapshot of the last-good result (caller MUST Release rec, nil-safe)
// and ensures the lane converges to executing compiledSQL. While a run is in
// flight, the prior result is returned (last-good) with loading=true.
func (inst *nodeLane) demand(compiledSQL string) (rec arrow.RecordBatch, schema *arrow.Schema, sql string, loading bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	fresh := inst.result != nil && inst.servedSQL == compiledSQL && !inst.loading
	if !fresh && inst.wantSQL != compiledSQL {
		inst.startLocked(compiledSQL)
	}

	if inst.result != nil {
		if inst.result.rec != nil {
			inst.result.rec.Retain()
		}
		rec = inst.result.rec
		schema = inst.result.schema
		sql = inst.result.sql
		err = inst.result.err
	}
	loading = inst.loading
	return
}

// forget clears the memo so the next demand re-executes even for an unchanged
// SQL (the Timeline's "Run bands" force — re-fetch against a changed source
// table). The last-good result is retained until the re-run lands.
func (inst *nodeLane) forget() {
	inst.mu.Lock()
	inst.servedSQL = ""
	inst.wantSQL = ""
	inst.mu.Unlock()
}

// startLocked supersedes any in-flight run and kicks a new one. Caller holds mu.
func (inst *nodeLane) startLocked(sql string) {
	inst.wantSQL = sql
	if inst.cancel != nil {
		inst.cancel()
	}
	inst.gen++
	gen := inst.gen
	var ctx context.Context
	var cancel context.CancelFunc
	if inst.timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), inst.timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	inst.cancel = cancel
	inst.loading = true
	go inst.run(ctx, gen, sql)
}

func (inst *nodeLane) run(ctx context.Context, gen uint64, sql string) {
	rec, schema, err := inst.exec.execute(ctx, sql, inst.alloc)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if gen != inst.gen {
		// Superseded (or closed) while in flight: discard this result so the
		// last-good / newer run wins.
		if rec != nil {
			rec.Release()
		}
		return
	}
	inst.loading = false
	inst.cancel = nil
	prev := inst.result
	inst.result = &nodeResult{rec: rec, schema: schema, sql: sql, fingerprint: fingerprintRecord(rec), err: err}
	inst.servedSQL = sql
	if prev != nil && prev.rec != nil {
		prev.rec.Release()
	}
}

// close cancels any in-flight run and releases the held result.
func (inst *nodeLane) close() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.cancel != nil {
		inst.cancel()
	}
	inst.gen++ // discard any in-flight completion
	if inst.result != nil && inst.result.rec != nil {
		inst.result.rec.Release()
	}
	inst.result = nil
}
