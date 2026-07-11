package play

import (
	"context"
	"encoding/binary"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// play_graph.go is slice 1 of ADR-0097 (`play` as a reactive query-graph): the
// Node / Signal / Panel contract plus a demand-driven, memoised, revision-based
// runtime for a SINGLE node, proving the runtime laws — minimality, early
// cutoff, and demand — on a mock executor. Live-path status: the queryGraph
// memo runtime (demand/setSignal/beginFrame) is exercised by the law tests and
// stands ready for the many-node scheduler; the LIVE graph flows through the
// facade below (the `main` QueryStore lane) and the nodeLane instances
// (map/bands/intermediate). Early cutoff is live at the lane observers: the
// content fingerprint computed here guards the Map repack and bands re-map.
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

// ChannelClaim is a panel's interpretation of one channel's node output schema,
// opaque to the runtime — Timeline's Mode+slots, Detail's leeway-vs-ad-hoc
// choice. Computed in AcceptForChannel, consumed in Render.
type ChannelClaim any

// SignalEnvI is the read-only view of the graph's signal (unbound-param) values
// at a single consistent revision (ADR-0097 SD4). Panels read it in AcceptForChannel.
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

// ChannelID identifies a typed input channel of a panel (ADR-0097 SD6/SD7,
// amended slice 4): the slot an eligible node fills. Single-input panels declare
// one channel; the Timeline declares events + bands.
type ChannelID string

const (
	chMain   ChannelID = "main"   // the lone channel of single-input panels (Table, Projection, Detail)
	chEvents ChannelID = "events" // the Timeline's foreground marks
	chBands  ChannelID = "bands"  // the Timeline's background bands (slice 4b-2)
)

// ChannelSpec declares one of a panel's input channels. A panel is renderable iff
// all its Required channels are filled.
type ChannelSpec struct {
	ID       ChannelID
	Required bool
	Label    string // human label for the Graph-view channel UI (slice 4c)
}

// ChannelResult is the node result bound to a channel, with the panel's resolved
// per-channel claim. Passed to Render in the filled map.
type ChannelResult struct {
	Node  NodeID
	Rec   arrow.RecordBatch
	Claim ChannelClaim
}

// PanelI is the panel contract (ADR-0097 SD6/SD7, amended slice 4): a panel
// declares typed input channels, each filled by an eligible node. The
// single-channel case is the pre-slice-4 single-node observer, unchanged.
type PanelI interface {
	ID() PanelID
	// Channels declares the panel's input channels in render/assignment order.
	Channels() []ChannelSpec
	// AcceptForChannel is the per-channel capability check (SD6): given a candidate
	// node's output schema for ch and the signal env, return a claim or a
	// human-facing reason (the empty-state text). Eligibility is reason == ""
	// — the dispatcher keys on the reason, and the claim may be any value the
	// panel wants back in Render (including nil). Pure: no side effects, no
	// rendering.
	AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string)
	// Render draws the panel from its filled channels — called when every Required
	// channel is filled (and the panel is visible). May publish signal mutations
	// via emit.
	Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI)
}

// compiledNode is a node's executable form (ADR-0097 slice 5a): the SQL body
// plus the signal values the node reads, resolved from the store at compile
// time. Params are URL entries — keys carry the `param_` prefix, values are
// raw strings — and ride the same channel as SET-bound constants at
// execution, where a SET-bound name shadows a same-named signal (slice-5 D1).
type compiledNode struct {
	SQL    string
	Params map[string]string
}

// key is the memo identity of a compiled node: the SQL plus the sorted
// params. The same SQL under different signal values is a different
// execution — both the runtime memo and the lanes key on this pair.
func (inst compiledNode) key() (out string) {
	if len(inst.Params) == 0 {
		return inst.SQL
	}
	names := make([]string, 0, len(inst.Params))
	for k := range inst.Params {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	b.Grow(len(inst.SQL) + 32*len(names))
	b.WriteString(inst.SQL)
	for _, k := range names {
		b.WriteByte(0x00)
		b.WriteString(k)
		b.WriteByte(0x01)
		b.WriteString(inst.Params[k])
	}
	out = b.String()
	return
}

// nodeExecutorI runs a compiled query and returns its single, concatenated Arrow
// result plus the engine's execution summary. The real impl wraps
// *Client.ExecuteArrowStream; tests use mocks (a zero Summary is fine).
type nodeExecutorI interface {
	execute(ctx context.Context, c compiledNode, alloc memory.Allocator) (rec arrow.RecordBatch, schema *arrow.Schema, summary Summary, err error)
}

// Node is a query node. Compile produces the pushed-down SQL plus the signal
// values it reads from the current signal env (ADR-0097: editor SQL →
// nanopass pipeline → param resolution). In slice 1 Compile is supplied
// directly; the splitter and a real nanopass pipeline land in slice 3.
type Node struct {
	ID      NodeID
	Compile func(sig SignalEnvI) (c compiledNode, err error)
}

// nodeResult is a node's memoised output (ADR-0097 SD1/SD10): the result, the
// compiled (SQL, signal values) pair whose key is the memo identity (5a), a
// content fingerprint for early cutoff (SD4), and the signal revision it was
// computed at. The graph owns rec and releases it when the result is replaced
// or the graph is closed; callers must not release it. A failed execution is
// carried on err (per-node state, SD11), not returned from demand.
type nodeResult struct {
	rec         arrow.RecordBatch
	schema      *arrow.Schema
	sql         string // the compiled SQL half (display/observers)
	key         string // compiledNode.key() — the memo identity
	fingerprint uint64
	revision    uint64
	summary     Summary
	executedAt  time.Time     // when the execution finished (zero = never ran)
	elapsed     time.Duration // wall-clock of the execution
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

	// mainLane is the `main` node's async execution lane (ADR-0097): the
	// Run-triggered node whose SQL is the editor buffer. nil for the bare
	// test runtime (newQueryGraph); set by newLiveQueryGraph. See the live
	// runtime facade at the bottom of this file.
	mainLane *QueryStore
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

// setSignalRaw writes a signal's raw value into the store (ADR-0097 slice
// 5a). name is the bare param name (no `param_` prefix); raw is the value
// string that rides the URL channel at execution. The revision bumps only on
// an actual change (setSignal). Writers: history-restore seeding (D4) and
// the panel emitters (graphEmitter, slice 5b).
func (inst *queryGraph) setSignalRaw(name SignalID, raw string) {
	inst.setSignal(name, env.Param{Name: name, Raw: raw})
}

// encodeSignalValue renders a panel-emitted Go value as the raw string a
// signal stores (the param_* URL channel ships raw text; the referencing
// slot's declared type drives the server-side interpretation). Unsupported
// types report ok=false — the emitter drops the write with a warning rather
// than storing something lossy.
func encodeSignalValue(value any) (raw string, ok bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), true
	case bool:
		if v {
			return "1", true
		}
		return "0", true
	}
	return "", false
}

// graphEmitter is the live SignalEmitterI over the signal store (ADR-0097
// slice 5b): a panel's Emit writes the named value into the store, where it
// is visible from the NEXT frame's snapshot (frame consistency, 5a) — every
// panel in a frame sees one revision, so propagation is uniform rather than
// dependent on tab render order. This is the single Emit path; the per-field
// selectedRow bridge (selectedRowEmitter) retired with it.
type graphEmitter struct {
	graph *queryGraph
}

func (inst graphEmitter) Emit(id SignalID, value any) {
	raw, encodable := encodeSignalValue(value)
	if !encodable {
		log.Warn().Str("signal", string(id)).Type("valueType", value).
			Msg("play: dropping signal emit with unsupported value type")
		return
	}
	inst.graph.setSignalRaw(id, raw)
}

// resolveSignalNames resolves the given slot names against a signal snapshot,
// skipping names in bound (SET-bound prelude constants — a SET pins, slice-5
// D1) and names the store has no value for. The result is URL-keyed
// (`param_`+name → raw), ready for the wire; nil when nothing resolves.
func resolveSignalNames(names []string, bound map[string]bool, sig SignalEnvI) (out map[string]string) {
	if sig == nil {
		return
	}
	for _, name := range names {
		if bound[name] {
			continue
		}
		p, found := sig.Get(name)
		if !found {
			continue
		}
		if out == nil {
			out = make(map[string]string, 4)
		}
		out["param_"+name] = p.Raw
	}
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

	c, cErr := n.Compile(inst.sig)
	if cErr != nil {
		err = eh.Errorf("queryGraph.demand: compile node %q: %w", id, cErr)
		return
	}
	memoKey := c.key()

	prev := inst.results[id]
	if prev != nil && prev.key == memoKey {
		res = prev
		return
	}

	// Stale ⇒ execute. An executor error is carried on the result (SD11), not
	// returned, so the bound panel can render it as an empty-state.
	start := time.Now()
	rec, schema, summary, xErr := inst.exec.execute(ctx, c, inst.alloc)
	next := &nodeResult{
		rec:         rec,
		schema:      schema,
		sql:         c.SQL,
		key:         memoKey,
		fingerprint: fingerprintRecord(rec),
		revision:    inst.sig.revision,
		summary:     summary,
		executedAt:  time.Now(),
		elapsed:     time.Since(start),
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

// close releases every memoised result and closes the main lane (when the
// graph owns one); the graph is unusable afterwards.
func (inst *queryGraph) close() {
	inst.mu.Lock()
	for _, r := range inst.results {
		if r != nil && r.rec != nil {
			r.rec.Release()
		}
	}
	inst.results = make(map[NodeID]*nodeResult, 4)
	lane := inst.mainLane
	inst.mu.Unlock()
	if lane != nil {
		lane.Close()
	}
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

// --- ADR-0097 live runtime: the `main` node's async execution lane ---
//
// `main` is the Run-triggered node — its SQL is the editor buffer, executed when
// the user runs. Its lane reuses the proven QueryStore async / single-flight /
// cancel / history machinery, now OWNED by the graph and reached only through
// this facade. PlayApp holds the graph, not a standalone store, so the panels and
// chrome read `main` through the graph (main is a graph node). Demand-triggered,
// self-executed nodes (the splitter's, slice 3) use the executor + memo path
// above instead.

// newLiveQueryGraph builds the graph for the live app: a clientExecutor over the
// client for self-executed nodes, plus the `main` node's QueryStore lane.
func newLiveQueryGraph(client *Client, alloc memory.Allocator, maxHistory int) (inst *queryGraph) {
	if alloc == nil {
		alloc = memory.NewGoAllocator()
	}
	inst = newQueryGraph(clientExecutor{client: client}, alloc)
	inst.mainLane = NewQueryStore(client, alloc, maxHistory, "main")
	return
}

// RunMain executes the `main` node's SQL (the editor buffer) on its async
// lane. signals carries the URL-keyed signal values resolved for this run
// (slice 5a — the store's values for the buffer's unbound param slots); they
// ride the request URL and are snapshotted into the run's history entry (D4).
func (inst *queryGraph) RunMain(sql string, signals map[string]string) {
	inst.mainLane.Execute(sql, signals)
}

// CancelMain aborts an in-flight `main` execution.
func (inst *queryGraph) CancelMain() { inst.mainLane.Cancel() }

// MainLoading reports whether `main` is executing.
func (inst *queryGraph) MainLoading() bool { return inst.mainLane.IsLoading() }

// MainHistory returns the `main` lane's run history.
func (inst *queryGraph) MainHistory() []HistoryEntry { return inst.mainLane.History() }

// MainSnapshot returns the `main` node's current result + metadata. The caller
// MUST Release the returned record (nil-safe), exactly as for QueryStore.Snapshot.
func (inst *queryGraph) MainSnapshot() (rec arrow.RecordBatch, schema *arrow.Schema, numRows int64, loading bool, elapsed time.Duration, summary Summary, executed time.Time, err error) {
	return inst.mainLane.Snapshot()
}
