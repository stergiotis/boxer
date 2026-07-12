package play

import (
	"sort"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
)

// play_bindings.go is ADR-0097 slice 6c: per-panel node binding. A panel tab
// can be bound to any split node — the tab's frame view then carries that
// node's result (from a per-node lane, the observed-intermediate machinery
// generalized from one to N) instead of the active result. Bindings are
// presentation-side: Run still executes the fused sink, and the status bar
// keeps tracking `main`. Selection stays coherent across differently-bound
// panels via two companion signals the dispatcher stamps on every selection
// write: `selection_node` (which node the row cursor indexes) and
// `selection_id` (the row's leeway id value, when the result carries one —
// the stable-key beachhead for SQL cross-filtering). The Detail tab follows
// `selection_node` by default, so clicking anywhere retargets it correctly.

// signalSelectionNode names the split node the `selection` cursor indexes —
// written alongside every panel selection emit (the dispatcher stamps it).
// Consumers see `selection` only when it indexes their own node (the
// node-scoped gate in dispatchPanel).
const signalSelectionNode SignalID = "selection_node"

// signalSelectionID carries the selected row's leeway id (`id:id:…` column)
// when the clicked result has one. It is a value, not a row ordinal, so a
// query can cross-filter on `{selection_id:UInt64}` regardless of which
// node — or ordering — produced the click. It tracks the LAST leeway
// selection: clicking a row of an id-less result leaves it unchanged.
const signalSelectionID SignalID = "selection_id"

// bindTab binds a panel tab's primary channel to a split node. Rebinding
// replaces; the lane for the node is created on the next frame's demand.
func (inst *PlayApp) bindTab(tabID string, node NodeID) {
	if inst.tabBindings == nil {
		inst.tabBindings = make(map[string]NodeID, 4)
	}
	inst.tabBindings[tabID] = node
	inst.gcBoundLanes()
}

// unbindTab removes a tab's binding; the tab renders the active result again.
func (inst *PlayApp) unbindTab(tabID string) {
	delete(inst.tabBindings, tabID)
	inst.gcBoundLanes()
}

// clearBindings drops every binding (the Graph view's reset).
func (inst *PlayApp) clearBindings() {
	for k := range inst.tabBindings {
		delete(inst.tabBindings, k)
	}
	inst.gcBoundLanes()
}

// gcBoundLanes closes lanes no binding references any more. Lanes are per
// NODE (two tabs bound to one node share a lane and its memo).
func (inst *PlayApp) gcBoundLanes() {
	for node, lane := range inst.boundLanes {
		referenced := false
		for _, n := range inst.tabBindings {
			if n == node {
				referenced = true
				break
			}
		}
		if !referenced {
			lane.close()
			delete(inst.boundLanes, node)
		}
	}
}

// forgetBoundLanes drops every bound lane's memo (the executeRun force — a
// fresh Run re-executes bound nodes against possibly-changed data, exactly
// as the observed-intermediate lane forgets).
func (inst *PlayApp) forgetBoundLanes() {
	for _, lane := range inst.boundLanes {
		lane.forget()
	}
}

// closeBoundLanes tears the lanes down (Close).
func (inst *PlayApp) closeBoundLanes() {
	for node, lane := range inst.boundLanes {
		lane.close()
		delete(inst.boundLanes, node)
	}
}

// selectionNodeRaw returns the raw `selection_node` value ("" when unset).
func (inst *PlayApp) selectionNodeRaw() string {
	if inst.frameSig == nil {
		return ""
	}
	p, ok := inst.frameSig.Get(signalSelectionNode)
	if !ok {
		return ""
	}
	return p.Raw
}

// demandBoundNodes drives one lane per DISTINCT bound node against the frame
// snapshot and resolves every panel tab's node for this frame. Dangling
// bindings (the split has no such node — a rename or a different buffer)
// stay registered but resolve to the active result, and revive when the
// name returns (slice-6c lifetime decision). Returns the release for the
// retained lane views; call it at end of frame.
func (inst *PlayApp) demandBoundNodes() (release func()) {
	inst.boundViews = make(map[NodeID]laneView, len(inst.tabBindings))
	inst.resolvedNodes = make(map[string]NodeID, len(inst.tabBindings)+1)
	release = func() {
		for _, v := range inst.boundViews {
			if v.rec != nil {
				v.rec.Release()
			}
		}
	}

	active := inst.activeNodeID()
	split := inst.currentSplit
	// Distinct bound nodes present in the current split, demanded once each.
	for tabID, nodeID := range inst.tabBindings {
		node, ok := findSplitNode(split, nodeID)
		if !ok || nodeID == active {
			// Dangling (inert) or redundant (bound to the active node):
			// the tab renders the active result.
			if nodeID == active {
				inst.resolvedNodes[tabID] = active
			}
			continue
		}
		inst.resolvedNodes[tabID] = nodeID
		if _, demanded := inst.boundViews[nodeID]; demanded {
			continue
		}
		lane, has := inst.boundLanes[nodeID]
		if !has {
			if inst.boundLanes == nil {
				inst.boundLanes = make(map[NodeID]*nodeLane, 4)
			}
			lane = newNodeLane(clientExecutor{client: inst.client, opts: newExecOptions("bound-" + string(nodeID))}, inst.graph.alloc, 0)
			inst.boundLanes[nodeID] = lane
		}
		inst.boundViews[nodeID] = lane.demand(compiledNode{
			SQL:    fuseNode(split, nodeID),
			Params: resolveSignalNames(node.Reads, inst.lastRunBound, inst.frameSig),
		})
	}

	// Detail follows the selection's node by default (an explicit Detail
	// binding, handled above, wins). Follow resolves only to nodes that are
	// visible this frame — the active node or a currently-bound one.
	if _, explicit := inst.tabBindings["detail"]; !explicit {
		if raw := inst.selectionNodeRaw(); raw != "" && raw != string(active) {
			if _, hasView := inst.boundViews[NodeID(raw)]; hasView {
				inst.resolvedNodes["detail"] = NodeID(raw)
			}
		}
	}
	return
}

// resolvedTabNode is the node a panel tab renders this frame: its binding
// (or follow-resolution), else the active node.
func (inst *PlayApp) resolvedTabNode(tabID string) NodeID {
	if node, ok := inst.resolvedNodes[tabID]; ok {
		return node
	}
	return inst.activeNodeID()
}

// frameFor returns the tab's frame view: the base (active-result) frame,
// with the result fields swapped to the bound node's lane view when the tab
// resolves to a non-active node. Per-node loading/error ride along, so the
// tab's own empty/loading/error states present per binding (the slice-6c
// staleness decision — bound lanes are live by construction: they recompile
// against every frame's signals and supersede in flight on change).
func (inst *PlayApp) frameFor(tabID string, base *TabFrame) (f TabFrame) {
	f = *base
	node, ok := inst.resolvedNodes[tabID]
	if !ok || node == inst.activeNodeID() {
		return
	}
	v, has := inst.boundViews[node]
	if !has {
		return
	}
	f.Rec = v.rec
	f.Schema = v.schema
	f.NumRows = 0
	if v.rec != nil {
		f.NumRows = v.rec.NumRows()
	}
	f.Loading = v.loading
	f.Err = v.err
	f.Executed = v.executedAt
	f.Elapsed = v.elapsed
	f.Summary = v.summary
	return
}

// boundTabTitle decorates a bound tab's dock title with its node —
// "Table · by_kind" — so a rebound pane is never mistaken for the active
// result. Dock identity is the DockID, so a varying title is safe.
func (inst *PlayApp) boundTabTitle(spec *TabSpec) string {
	node, ok := inst.tabBindings[spec.ID]
	if !ok {
		return spec.Title
	}
	if _, valid := findSplitNode(inst.currentSplit, node); !valid {
		return spec.Title + " · (" + string(node) + " absent)"
	}
	return spec.Title + " · " + string(node)
}

// bindingSummary is the Graph view's one-line inventory of active bindings.
func (inst *PlayApp) bindingSummary() string {
	if len(inst.tabBindings) == 0 {
		return ""
	}
	parts := make([]string, 0, len(inst.tabBindings))
	for tab, node := range inst.tabBindings {
		parts = append(parts, tab+"→"+string(node))
	}
	sort.Strings(parts)
	return "bindings — " + strings.Join(parts, " · ")
}

// nodeScopedSelection gates the `selection` signal to one node: a panel
// bound to node X must not highlight a row cursor that indexes node Y. An
// unset selection_node (bootstrap, history-seeded state) reads as matching,
// preserving pre-6c behaviour.
type nodeScopedSelection struct {
	SignalEnvI
	node NodeID
}

func (inst nodeScopedSelection) Get(id SignalID) (p env.Param, ok bool) {
	p, ok = inst.SignalEnvI.Get(id)
	if !ok || id != signalSelection {
		return
	}
	if src, has := inst.SignalEnvI.Get(signalSelectionNode); has && src.Raw != "" && src.Raw != string(inst.node) {
		p = env.Param{}
		ok = false
	}
	return
}

// selectionStamper decorates a panel's emitter: every `selection` write also
// records which node the cursor indexes and, when the clicked result carries
// a leeway id column, the selected row's id value. Panels stay unaware —
// the dispatcher installs it (like the writer stamp).
type selectionStamper struct {
	inner SignalEmitterI
	node  NodeID
	rec   arrow.RecordBatch
}

func (inst selectionStamper) Emit(id SignalID, value any) {
	inst.inner.Emit(id, value)
	if id != signalSelection {
		return
	}
	inst.inner.Emit(signalSelectionNode, string(inst.node))
	row, isRow := value.(int64)
	if !isRow {
		return
	}
	if raw, found := leewayIdValue(inst.rec, row); found {
		inst.inner.Emit(signalSelectionID, raw)
	}
}

// leewayIdValue extracts the row's primary leeway id — the first `id:id:…`
// column — as its raw string, when the record has one.
func leewayIdValue(rec arrow.RecordBatch, row int64) (raw string, found bool) {
	if rec == nil || row < 0 || row >= rec.NumRows() {
		return
	}
	schema := rec.Schema()
	for i := 0; i < schema.NumFields(); i++ {
		if !strings.HasPrefix(schema.Field(i).Name, "id:id:") {
			continue
		}
		col := rec.Column(i)
		if col.IsNull(int(row)) {
			return
		}
		raw = col.ValueStr(int(row))
		found = raw != ""
		return
	}
	return
}
