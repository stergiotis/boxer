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
// until the new one lands (no flicker). A demand that flips BACK to the SQL the
// memo already serves cancels the in-flight run and serves the memo without
// re-executing (minimality — a forced re-fetch goes through forget instead). A
// closed lane drops demands rather than resurrecting. The `main` node keeps its
// QueryStore lane (Run-triggered, with history); this lane is for
// demand-triggered self-executed nodes (the splitter's, once they execute
// separately in slice 3d) and is exercised by tests until then.

type nodeLane struct {
	exec    nodeExecutorI
	alloc   memory.Allocator
	timeout time.Duration // per-execution timeout (0 = none); the Map's remote sources need ~60s

	mu        sync.Mutex
	result    *nodeResult // last-good (owned); retained across a successful supersede
	servedKey string      // the compiled key `result` was computed for (compiledNode.key)
	wantKey   string      // the compiled key currently desired (latest demanded)
	loading   bool
	gen       uint64 // bumped on each start; a stale (superseded) completion is discarded
	cancel    context.CancelFunc
	// closed marks a torn-down lane: a straggler frame's demand after close
	// (the Unmount window) is dropped instead of starting a query nothing will
	// consume — the QueryStore `closed` guard, mirrored here.
	closed bool
}

func newNodeLane(exec nodeExecutorI, alloc memory.Allocator, timeout time.Duration) (inst *nodeLane) {
	if alloc == nil {
		alloc = memory.NewGoAllocator()
	}
	inst = &nodeLane{exec: exec, alloc: alloc, timeout: timeout}
	return
}

// laneView is a demand-time snapshot of the lane: the last-good result
// (retained for the caller — Release rec, nil-safe), the compiled (SQL,
// params) pair it was computed for — sql/params/key, where key is the memo
// identity and params must be treated as read-only — and the in-flight flag.
// The fingerprint is the early-cutoff hook (ADR-0097 SD4) for the lane's
// observers: repack/re-map only when it changes, so a forced re-fetch that
// returns identical bytes costs no downstream work. params lets an observer
// derive per-result metadata from the served inputs themselves (the Map pins
// its raster to bounds recovered from the served vp_* values, slice 5c).
type laneView struct {
	rec         arrow.RecordBatch
	schema      *arrow.Schema
	sql         string
	params      map[string]string // served signal values (read-only)
	key         string            // compiledNode.key() of the served result
	fingerprint uint64
	summary     Summary
	executedAt  time.Time     // when the served result's execution finished
	elapsed     time.Duration // its wall-clock
	loading     bool
	err         error
}

// demand requests the node's result for the compiled (SQL, signal values)
// pair, non-blocking. It returns a retained snapshot of the last-good result
// (caller MUST Release view.rec, nil-safe) and ensures the lane converges to
// executing c. While a run is in flight, the prior result is returned
// (last-good) with loading=true. The memo keys on compiledNode.key(), so the
// same SQL under different signal values re-executes (slice 5a).
func (inst *nodeLane) demand(c compiledNode) (view laneView) {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.closed {
		// Torn down: drop the demand (empty view — no result, not loading).
		return
	}

	demandKey := c.key()
	memoCurrent := inst.result != nil && inst.servedKey == demandKey
	switch {
	case memoCurrent && inst.loading:
		// Flip-back: the demand returned to the pair the memo already serves
		// while a superseding run is still in flight (A→B→A). Cancel that run
		// and serve the memo — re-executing would break minimality: nothing
		// the memo covers changed (a forced re-fetch goes through forget,
		// which clears servedKey and so never lands here).
		inst.gen++ // the in-flight completion is stale now
		if inst.cancel != nil {
			inst.cancel()
			inst.cancel = nil
		}
		inst.loading = false
		inst.wantKey = demandKey
	case !memoCurrent && inst.wantKey != demandKey:
		inst.startLocked(c, demandKey)
	}

	if inst.result != nil {
		if inst.result.rec != nil {
			inst.result.rec.Retain()
		}
		view.rec = inst.result.rec
		view.schema = inst.result.schema
		view.sql = inst.result.sql
		view.params = inst.result.params
		view.key = inst.result.key
		view.fingerprint = inst.result.fingerprint
		view.summary = inst.result.summary
		view.executedAt = inst.result.executedAt
		view.elapsed = inst.result.elapsed
		view.err = inst.result.err
	}
	view.loading = inst.loading
	return
}

// forget clears the memo AND discards any in-flight run, so the next demand
// re-executes even for an unchanged input (the "Run bands" / Map-Refresh
// force — re-fetch against a changed source table). Without the generation
// bump, an in-flight completion landing after forget would restore servedKey
// and the next demand would memo-hit, silently dropping the force. The
// last-good result is retained until the re-run lands.
func (inst *nodeLane) forget() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return
	}
	inst.servedKey = ""
	inst.wantKey = ""
	inst.gen++ // an in-flight completion is stale now
	if inst.cancel != nil {
		inst.cancel()
		inst.cancel = nil
	}
	inst.loading = false
}

// startLocked supersedes any in-flight run and kicks a new one. Caller holds mu.
func (inst *nodeLane) startLocked(c compiledNode, demandKey string) {
	inst.wantKey = demandKey
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
	go inst.run(ctx, cancel, gen, c, demandKey)
}

func (inst *nodeLane) run(ctx context.Context, cancel context.CancelFunc, gen uint64, c compiledNode, demandKey string) {
	// Release the context on every path (idempotent with the supersede /
	// close cancels): a WithTimeout ctx left uncancelled keeps its timer
	// armed until the deadline — 60s per map fetch (review finding).
	defer cancel()
	start := time.Now()
	rec, schema, summary, err := inst.exec.execute(ctx, c, inst.alloc)
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
	inst.result = &nodeResult{
		rec: rec, schema: schema, sql: c.SQL, params: c.Params, key: demandKey,
		fingerprint: fingerprintRecord(rec),
		summary:     summary,
		executedAt:  time.Now(),
		elapsed:     time.Since(start),
		err:         err,
	}
	inst.servedKey = demandKey
	if prev != nil && prev.rec != nil {
		prev.rec.Release()
	}
}

// close cancels any in-flight run, releases the held result, and marks the
// lane closed so later demands/forgets are dropped. Idempotent.
func (inst *nodeLane) close() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.closed = true
	if inst.cancel != nil {
		inst.cancel()
	}
	inst.gen++ // discard any in-flight completion
	if inst.result != nil && inst.result.rec != nil {
		inst.result.rec.Release()
	}
	inst.result = nil
}
