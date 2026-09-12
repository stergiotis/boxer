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
	release = func() {
		if edgesRec != nil {
			edgesRec.Release()
		}
		if vertRec != nil {
			vertRec.Release()
		}
	}
	inputs = map[ChannelID]channelInput{
		chEdges: {node: networkEdgesNodeID, rec: edgesRec, schema: edgesSchema, sig: inst.frameSig},
	}
	if vertRec != nil || vertSchema != nil {
		inputs[chVertices] = channelInput{node: networkVerticesNodeID, rec: vertRec, schema: vertSchema, sig: inst.frameSig}
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
