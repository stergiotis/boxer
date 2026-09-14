package play

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// play_network_source.go owns the two lanes the graph contract is fed from —
// the `edges` and `vertices` CTEs of the user's own split, each demanded on its
// own lane, the kanban `lanes`-CTE mechanism (ADR-0122 §SD6) applied twice.
//
// The source is SHARED by both graph panels (ADR-0227 §SD2). With the Network
// and Graphview tabs both open the CTEs execute once — the second demand is a
// memo hit on the same lane — and a forced re-fetch clears one memo rather than
// two. It also puts the status mirrors in one place, so a failed lane says so
// in both status lines.

// networkSource is the two lanes plus the per-frame status the panels read
// back. Lanes are nil for an unwired host (tests); the panels then show their
// empty state.
type networkSource struct {
	edgesLane    *nodeLane
	verticesLane *nodeLane
	optsLane     *nodeLane

	// The status mirrors let a failed lane say so rather than reading as "no
	// graph". Written by every demand — nil clears, there is no latch.
	edgesLoading    bool
	verticesLoading bool
	edgesErr        error
	verticesErr     error

	// edgesFP / verticesFP are the served results' content fingerprints — the
	// early-cutoff hook of ADR-0097 §SD4. A panel that rebuilds its model per
	// frame keys the cache on them (ADR-0227 §SD8), so a running simulation
	// re-formats no cells and a re-fetch returning identical bytes costs no
	// rebuild. Zero means nothing served.
	edgesFP    uint64
	verticesFP uint64
	optsFP     uint64

	// The three lanes' served SQL and signal values, for the own-signal rule
	// of ADR-0231 §SD8: a rebuild whose inputs diverged only on signals the
	// Graphview tab itself wrote keeps the camera.
	edgesServed, verticesServed, optsServed laneServed
}

// laneServed is what one lane last served a result for: the fused SQL and the
// signal values it was compiled with.
type laneServed struct {
	sql    string
	params map[string]string
}

// netServed is the three graph lanes' served inputs at one moment.
type netServed struct {
	edges, vertices, opts laneServed
}

// served snapshots the three lanes' served inputs.
func (inst *networkSource) served() netServed {
	return netServed{edges: inst.edgesServed, vertices: inst.verticesServed, opts: inst.optsServed}
}

// divergedOnlyOn reports whether every difference between two snapshots is a
// signal value whose name own accepts, with the SQL of every lane unchanged.
// It is false when nothing differs, since then nothing was caused at all.
func (inst netServed) divergedOnlyOn(prev netServed, own func(name string) bool) bool {
	differs := false
	for _, pair := range [][2]laneServed{{inst.edges, prev.edges}, {inst.vertices, prev.vertices}, {inst.opts, prev.opts}} {
		cur, old := pair[0], pair[1]
		if cur.sql != old.sql {
			return false
		}
		for name, v := range cur.params {
			if ov, had := old.params[name]; !had || ov != v {
				if !own(name) {
					return false
				}
				differs = true
			}
		}
		for name := range old.params {
			if _, has := cur.params[name]; !has {
				if !own(name) {
					return false
				}
				differs = true
			}
		}
	}
	return differs
}

// newNetworkSource builds the source. client may be nil, which leaves both
// lanes absent.
func newNetworkSource(client *Client) (inst *networkSource) {
	inst = &networkSource{}
	if client != nil {
		inst.edgesLane = newNodeLane(clientExecutor{client: client, opts: newExecOptions("network-edges")},
			memory.NewGoAllocator(), 0)
		inst.verticesLane = newNodeLane(clientExecutor{client: client, opts: newExecOptions("network-vertices")},
			memory.NewGoAllocator(), 0)
		inst.optsLane = newNodeLane(clientExecutor{client: client, opts: newExecOptions("network-graph-opts")},
			memory.NewGoAllocator(), 0)
	}
	return
}

// forgetLanes clears both lane memos so the next demand re-executes, even for
// an unchanged (SQL, params) pair — the Run hook (executeRun), matching the
// intermediate and bound lanes. Without it a re-Run after a transient failure
// (a wrong endpoint, a server that was down) memo-hits the stored error — its
// key is the SQL, and the endpoint is not part of it — so the graph never
// recovers though the main result does.
func (inst *networkSource) forgetLanes() {
	if inst == nil {
		return
	}
	if inst.edgesLane != nil {
		inst.edgesLane.forget()
	}
	if inst.verticesLane != nil {
		inst.verticesLane.forget()
	}
	if inst.optsLane != nil {
		inst.optsLane.forget()
	}
}

// close tears both lanes down with the app.
func (inst *networkSource) close() {
	if inst == nil {
		return
	}
	if inst.edgesLane != nil {
		inst.edgesLane.close()
	}
	if inst.verticesLane != nil {
		inst.verticesLane.close()
	}
	if inst.optsLane != nil {
		inst.optsLane.close()
	}
}

// demandNetworkEdges compiles the query's `edges` CTE — if it has one — and
// demands it on the shared edges lane, returning the retained result for the
// chEdges channel (the caller MUST Release rec). Mirrors demandKanbanLanes: the
// node comes from the last Run's split, so its signal reads resolve like any
// other node's and a SET-bound name travels inside the fused SQL.
//
// Both graph tabs call this in the same frame; the second call is a memo hit.
func (inst *PlayApp) demandNetworkEdges() (rec arrow.RecordBatch, schema *arrow.Schema) {
	s := inst.netSource
	if s == nil || s.edgesLane == nil {
		return
	}
	node, ok := findSplitNode(inst.currentSplit, networkEdgesNodeID)
	if !ok {
		s.edgesLoading = false
		s.edgesErr = nil
		s.edgesFP = 0
		return
	}
	v := s.edgesLane.demand(compiledNode{
		SQL:    fuseNode(inst.currentSplit, networkEdgesNodeID),
		NodeID: networkEdgesNodeID,
		Params: resolveSignalNamesWithDefaults(node.Reads, inst.lastRunBound, inst.frameSig),
	})
	s.edgesLoading = v.loading
	s.edgesErr = v.err // mirrored every demand — nil clears (no latch)
	s.edgesFP = v.fingerprint
	s.edgesServed = laneServed{sql: v.sql, params: v.params}
	return v.rec, v.schema
}

// demandNetworkVertices is demandNetworkEdges for the optional `vertices` CTE.
func (inst *PlayApp) demandNetworkVertices() (rec arrow.RecordBatch, schema *arrow.Schema) {
	s := inst.netSource
	if s == nil || s.verticesLane == nil {
		return
	}
	node, ok := findSplitNode(inst.currentSplit, networkVerticesNodeID)
	if !ok {
		s.verticesLoading = false
		s.verticesErr = nil
		s.verticesFP = 0
		return
	}
	v := s.verticesLane.demand(compiledNode{
		SQL:    fuseNode(inst.currentSplit, networkVerticesNodeID),
		NodeID: networkVerticesNodeID,
		Params: resolveSignalNamesWithDefaults(node.Reads, inst.lastRunBound, inst.frameSig),
	})
	s.verticesLoading = v.loading
	s.verticesErr = v.err
	s.verticesFP = v.fingerprint
	s.verticesServed = laneServed{sql: v.sql, params: v.params}
	return v.rec, v.schema
}

// demandGraphOpts compiles the optional `graph_opts` CTE and demands it on its
// own lane. Its own lane rather than a read off the vertices result because it
// is a different CTE with a different shape, and because a settings row that
// changes must not re-execute the graph.
func (inst *PlayApp) demandGraphOpts() (rec arrow.RecordBatch, schema *arrow.Schema) {
	s := inst.netSource
	if s == nil || s.optsLane == nil {
		return
	}
	node, ok := findSplitNode(inst.currentSplit, networkGraphOptsNodeID)
	if !ok {
		s.optsFP = 0
		return
	}
	v := s.optsLane.demand(compiledNode{
		SQL:    fuseNode(inst.currentSplit, networkGraphOptsNodeID),
		NodeID: networkGraphOptsNodeID,
		Params: resolveSignalNamesWithDefaults(node.Reads, inst.lastRunBound, inst.frameSig),
	})
	s.optsFP = v.fingerprint
	s.optsServed = laneServed{sql: v.sql, params: v.params}
	return v.rec, v.schema
}

// graphChannelInputs demands both CTEs and packs them as the channel inputs the
// two graph panels dispatch on. The caller MUST release the two records, which
// the returned closure does.
//
// The vertices channel is offered only when the CTE exists (a schema-only view
// still fills it, so an inventory that legitimately returned nothing reads as
// "no vertices" rather than as pending).
func (inst *PlayApp) graphChannelInputs() (inputs map[ChannelID]channelInput, release func()) {
	edgesRec, edgesSchema := inst.demandNetworkEdges()
	vertRec, vertSchema := inst.demandNetworkVertices()
	optsRec, optsSchema := inst.demandGraphOpts()
	release = func() {
		if edgesRec != nil {
			edgesRec.Release()
		}
		if vertRec != nil {
			vertRec.Release()
		}
		if optsRec != nil {
			optsRec.Release()
		}
	}
	inputs = map[ChannelID]channelInput{
		chEdges: {node: networkEdgesNodeID, rec: edgesRec, schema: edgesSchema, sig: inst.frameSig},
	}
	if vertRec != nil || vertSchema != nil {
		inputs[chVertices] = channelInput{node: networkVerticesNodeID, rec: vertRec, schema: vertSchema, sig: inst.frameSig}
	}
	if optsRec != nil || optsSchema != nil {
		inputs[chGraphOpts] = channelInput{node: networkGraphOptsNodeID, rec: optsRec, schema: optsSchema, sig: inst.frameSig}
	}
	return
}

// statusSuffix is the LANE half of a graph panel's status line: what a failed
// lane is reporting, or the pending marker while one is in flight. Shared so
// both tabs say the same thing about the same lanes.
func (inst *networkSource) statusSuffix() string {
	if inst == nil {
		return ""
	}
	switch {
	case inst.edgesErr != nil:
		return fmt.Sprintf(" · edges query failed: %v", inst.edgesErr)
	case inst.verticesErr != nil:
		return fmt.Sprintf(" · vertices query failed: %v", inst.verticesErr)
	case inst.edgesLoading || inst.verticesLoading:
		return " · …"
	}
	return ""
}

// edgesPending reports that the required lane is in flight, which a panel says
// instead of its own reject reason — a graph that has not arrived yet is not a
// graph the query failed to declare.
func (inst *networkSource) edgesPending() bool {
	return inst != nil && inst.edgesLoading
}
