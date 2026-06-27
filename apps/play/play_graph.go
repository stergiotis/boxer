package play

import (
	"context"
	"encoding/binary"
	"hash/fnv"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// play_graph.go is slice 1 of ADR-0097 (`play` as a reactive query-graph): the
// Node / Signal / Panel contract plus a demand-driven, memoised, revision-based
// runtime for a SINGLE node. It proves the runtime laws — minimality, early
// cutoff, and demand — on a mock executor, without splitting (slice 3) or
// touching the live render path (slice 2). The full Node (DependsOn / Reads /
// nanopass Pipeline), the suspending async scheduler, and the real *Client
// executor adapter land in later slices.
//
// See doc/adr/0097-play-reactive-query-graph.md.

// NodeID identifies a query node in the graph.
type NodeID string

// SignalID is a param name. An unbound `{name:Type}` slot is a signal, and
// signals unify across nodes by name (ADR-0097 SD8): the same name is one shared
// input.
type SignalID = string

// PanelID identifies a panel (a dock tab that observes a node).
type PanelID string

// PanelClaim is a panel's interpretation of its node's output schema, opaque to
// the runtime — Timeline's Mode+slots, Detail's leeway-vs-ad-hoc choice, the
// Map's framebuffer mapping. Computed once in Accept, consumed in Render.
type PanelClaim any

// SignalEnvI is the read-only view of the graph's signal (unbound-param) values
// at a single consistent revision (ADR-0097 SD4). Panels read it in Accept.
type SignalEnvI interface {
	Get(id SignalID) (param env.Param, ok bool)
	Revision() uint64
}

// SignalEmitterI lets a panel write a param's value — the viewof producer/
// consumer duality (ADR-0097 SD8). A widget and a panel write the SAME named
// params; a node that references the param depends on it.
type SignalEmitterI interface {
	Emit(id SignalID, value any)
}

// PanelI is the rigorous panel contract (ADR-0097 interface, SD6/SD7): an
// observer bound to exactly one node, with a typed accept/reject negotiation
// generalising the Timeline Mode/Reject pattern.
type PanelI interface {
	ID() PanelID
	BoundNode() NodeID
	// Accept is the capability check: given the bound node's output schema and
	// the current signal env, return a claim (non-nil ⇒ render) or a human-facing
	// reason (the empty-state text). Pure: no side effects, no rendering.
	Accept(schema *arrow.Schema, sig SignalEnvI) (claim PanelClaim, reason string)
	// Render draws the node's result. Called only when Accept returned a claim
	// and the panel is visible (demand). May publish signal mutations via emit.
	Render(rec arrow.RecordBatch, claim PanelClaim, emit SignalEmitterI)
}

// nodeExecutorI runs a compiled query and returns its single, concatenated Arrow
// result. The real impl wraps *Client.ExecuteArrowStream (slice 2); slice-1 tests
// use a mock.
type nodeExecutorI interface {
	execute(ctx context.Context, sql string, alloc memory.Allocator) (rec arrow.RecordBatch, schema *arrow.Schema, err error)
}

// Node is a query node. Compile produces the pushed-down SQL from the current
// signal env (ADR-0097: editor SQL → nanopass pipeline → param substitution). In
// slice 1 Compile is supplied directly; the splitter and a real nanopass pipeline
// land in slice 3.
type Node struct {
	ID      NodeID
	Compile func(sig SignalEnvI) (sql string, err error)
}

// nodeResult is a node's memoised output (ADR-0097 SD1/SD10): the result, the
// compiled SQL that is its memo key, a content fingerprint for early cutoff
// (SD4), and the signal revision it was computed at. The graph owns rec and
// releases it when the result is replaced or the graph is closed; callers must
// not release it. A failed execution is carried on err (per-node state, SD11),
// not returned from demand.
type nodeResult struct {
	rec         arrow.RecordBatch
	schema      *arrow.Schema
	sql         string
	fingerprint uint64
	revision    uint64
	err         error
}

// signalEnv is an immutable signal snapshot. setSignal copy-on-writes a new one
// and bumps the revision, so a holder of an older snapshot keeps a consistent
// view (glitch-free reads, ADR-0097 SD4).
type signalEnv struct {
	params   map[SignalID]env.Param
	revision uint64
}

func (inst *signalEnv) Get(id SignalID) (param env.Param, ok bool) {
	param, ok = inst.params[id]
	return
}

func (inst *signalEnv) Revision() uint64 { return inst.revision }

// queryGraph is the slice-1 runtime: a demand-driven, memoised set of nodes over
// one executor. Execution is synchronous under the lock (the suspending async
// scheduler is slice 2). All methods are safe for concurrent use.
type queryGraph struct {
	mu       sync.Mutex
	alloc    memory.Allocator
	exec     nodeExecutorI
	nodes    map[NodeID]*Node
	results  map[NodeID]*nodeResult
	demanded map[NodeID]bool
	sig      *signalEnv
}

func newQueryGraph(exec nodeExecutorI, alloc memory.Allocator) (inst *queryGraph) {
	if alloc == nil {
		alloc = memory.NewGoAllocator()
	}
	inst = &queryGraph{
		alloc:    alloc,
		exec:     exec,
		nodes:    make(map[NodeID]*Node, 4),
		results:  make(map[NodeID]*nodeResult, 4),
		demanded: make(map[NodeID]bool, 4),
		sig:      &signalEnv{params: make(map[SignalID]env.Param, 4)},
	}
	return
}

// addNode registers a node; an existing ID is replaced.
func (inst *queryGraph) addNode(n *Node) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.nodes[n.ID] = n
}

// signals returns the current immutable signal snapshot (for a panel's Accept).
func (inst *queryGraph) signals() (sig SignalEnvI) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	sig = inst.sig
	return
}

// setSignal sets a param's value, bumping the revision only when the value
// actually changes (ADR-0097 SD4: minimality starts at the input). Unchanged
// re-sets are no-ops, so a node is not re-run for a signal that did not move.
func (inst *queryGraph) setSignal(id SignalID, p env.Param) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	cur, ok := inst.sig.params[id]
	if ok && cur.Raw == p.Raw && cur.Type == p.Type {
		return
	}
	next := make(map[SignalID]env.Param, len(inst.sig.params)+1)
	for k, v := range inst.sig.params {
		next[k] = v
	}
	next[id] = p
	inst.sig = &signalEnv{params: next, revision: inst.sig.revision + 1}
}

// beginFrame clears the per-frame demand set. Panels call demand during the
// frame; a node nothing demands this frame is never executed (ADR-0097 SD2).
func (inst *queryGraph) beginFrame() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for k := range inst.demanded {
		delete(inst.demanded, k)
	}
}

// demand marks a node observed this frame and returns its memoised result,
// computing it on demand iff stale: a node executes only when its compiled SQL
// (under the current signals) differs from the memoised result's (minimality,
// ADR-0097 SD1). An undemanded node is never reached, hence never executed (SD2).
func (inst *queryGraph) demand(ctx context.Context, id NodeID) (res *nodeResult, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	n := inst.nodes[id]
	if n == nil {
		err = eh.Errorf("queryGraph.demand: unknown node %q", id)
		return
	}
	inst.demanded[id] = true

	sql, cErr := n.Compile(inst.sig)
	if cErr != nil {
		err = eh.Errorf("queryGraph.demand: compile node %q: %w", id, cErr)
		return
	}

	prev := inst.results[id]
	if prev != nil && prev.sql == sql {
		res = prev
		return
	}

	// Stale ⇒ execute. An executor error is carried on the result (SD11), not
	// returned, so the bound panel can render it as an empty-state.
	rec, schema, xErr := inst.exec.execute(ctx, sql, inst.alloc)
	next := &nodeResult{
		rec:         rec,
		schema:      schema,
		sql:         sql,
		fingerprint: fingerprintRecord(rec),
		revision:    inst.sig.revision,
		err:         xErr,
	}
	if prev != nil && prev.rec != nil {
		prev.rec.Release()
	}
	inst.results[id] = next
	res = next
	return
}

// isDemanded reports whether a node was demanded since the last beginFrame
// (inspection/test helper).
func (inst *queryGraph) isDemanded(id NodeID) (ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ok = inst.demanded[id]
	return
}

// close releases every memoised result; the graph is unusable afterwards.
func (inst *queryGraph) close() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, r := range inst.results {
		if r != nil && r.rec != nil {
			r.rec.Release()
		}
	}
	inst.results = make(map[NodeID]*nodeResult, 4)
}

// fingerprintRecord computes a content fingerprint over a record's schema, row
// count, and column buffers. Equal fingerprints mean content-identical results,
// so a re-executed node whose fingerprint is unchanged does not invalidate its
// downstream observers (ADR-0097 SD4 early cutoff). It is a fast non-cryptographic
// hash (FNV-1a), not a collision-proof digest.
func fingerprintRecord(rec arrow.RecordBatch) (fp uint64) {
	if rec == nil {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(rec.Schema().String()))
	var scratch [8]byte
	binary.LittleEndian.PutUint64(scratch[:], uint64(rec.NumRows()))
	_, _ = h.Write(scratch[:])
	ncols := int(rec.NumCols())
	for c := 0; c < ncols; c++ {
		for _, buf := range rec.Column(c).Data().Buffers() {
			if buf != nil {
				_, _ = h.Write(buf.Bytes())
			}
		}
	}
	fp = h.Sum64()
	return
}
