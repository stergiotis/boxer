//go:build llm_generated_opus47

// Package store provides the concrete Graggle data structure implementing
// all graph interfaces defined in the types package.
package store

import (
	"fmt"
	"iter"

	"github.com/stergiotis/boxer/public/observability/eh"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// Graggle is the core data structure: a directed graph of lines (nodes).
// It generalises a file's linear order into a partial order, enabling
// mathematically correct merging via categorical pushouts.
//
// Deleted nodes are tombstoned ("ghost lines"), never truly removed.
// Pseudo-edges bridge over deleted regions so the live subgraph stays connected.
//
// All state is unexported. External read access goes through the
// GraphReaderI/InspectableI/VisualizableI interfaces (see graggle/types).
// External mutation goes through GraphWriterI — direct field access would
// bypass pseudo-edge bookkeeping and break invariants.
type Graggle struct {
	// Live nodes and their contents.
	nodes    *t.NodeSet
	contents map[t.NodeID][]byte

	// Tombstoned (ghost/deleted) nodes — still in the graph, marked dead.
	deletedNodes *t.NodeSet

	// Forward and backward adjacency lists.
	edges     *t.MultiMap // src -> []Edge
	backEdges *t.MultiMap // dest -> []Edge (reverse)

	// Pseudo-edge bookkeeping.
	deletedPartition *t.UnionFind
	// Maps deleted-component representative -> set of pseudo-edges it justifies.
	reasonPseudoEdges map[t.NodeID][]pseudoEdge
	// Maps pseudo-edge -> set of component reps that justify it.
	pseudoEdgeReasons map[pseudoEdge]map[t.NodeID]struct{}
	// Components whose pseudo-edges need recomputation.
	dirtyReps map[t.NodeID]struct{}
}

// pseudoEdge identifies a pseudo-edge by its endpoints.
type pseudoEdge struct {
	Src, Dest t.NodeID
}

// New creates an empty graggle with only the root node.
func New() *Graggle {
	inst := &Graggle{
		nodes:             t.NewNodeSet(),
		contents:          make(map[t.NodeID][]byte),
		deletedNodes:      t.NewNodeSet(),
		edges:             t.NewMultiMap(),
		backEdges:         t.NewMultiMap(),
		deletedPartition:  t.NewUnionFind(),
		reasonPseudoEdges: make(map[t.NodeID][]pseudoEdge),
		pseudoEdgeReasons: make(map[pseudoEdge]map[t.NodeID]struct{}),
		dirtyReps:         make(map[t.NodeID]struct{}),
	}
	inst.nodes.Add(t.RootNodeID)
	return inst
}

// HasNode returns true if the node exists (live or deleted).
func (inst *Graggle) HasNode(id t.NodeID) bool {
	return inst.nodes.Contains(id) || inst.deletedNodes.Contains(id)
}

// IsLive returns true if the node is alive (not deleted).
func (inst *Graggle) IsLive(id t.NodeID) bool {
	return inst.nodes.Contains(id)
}

// IsDeleted returns true if the node is a tombstone.
func (inst *Graggle) IsDeleted(id t.NodeID) bool {
	return inst.deletedNodes.Contains(id)
}

// LiveNodeCount returns the number of live (non-tombstoned) nodes,
// including the root.
func (inst *Graggle) LiveNodeCount() int {
	return inst.nodes.Len()
}

// DeletedNodeCount returns the number of tombstoned (ghost) nodes.
func (inst *Graggle) DeletedNodeCount() int {
	return inst.deletedNodes.Len()
}

// AddNode inserts a new live node with the given content and connects it
// between the given context nodes.
// upContext: nodes that should precede this node.
// downContext: nodes that should follow this node.
func (inst *Graggle) AddNode(id t.NodeID, content []byte, patch t.PatchHash, upContext, downContext []t.NodeID) error {
	if inst.HasNode(id) {
		return eh.Errorf("node %v already exists", id)
	}
	inst.nodes.Add(id)
	inst.contents[id] = content

	for _, up := range upContext {
		if !inst.HasNode(up) {
			return eh.Errorf("up-context node %v does not exist", up)
		}
		kind := t.EdgeKindLive
		if inst.IsDeleted(up) {
			kind = t.EdgeKindDeleted
			// Mark the deleted component dirty so pseudo-edges are recomputed.
			if inst.deletedPartition.Contains(up) {
				rep := inst.deletedPartition.Find(up)
				inst.dirtyReps[rep] = struct{}{}
			}
		}
		inst.addEdgeInternal(up, id, kind, patch)
	}
	for _, down := range downContext {
		if !inst.HasNode(down) {
			return eh.Errorf("down-context node %v does not exist", down)
		}
		kind := t.EdgeKindLive
		if inst.IsDeleted(down) {
			kind = t.EdgeKindDeleted
			if inst.deletedPartition.Contains(down) {
				rep := inst.deletedPartition.Find(down)
				inst.dirtyReps[rep] = struct{}{}
			}
		}
		inst.addEdgeInternal(id, down, kind, patch)
	}
	return nil
}

// VENDOR DEVIATION: DeleteNode is idempotent on already-deleted nodes
// (returns nil instead of an error). Two patches can legitimately delete
// the same node — e.g. two actors editing the same line, where LineDiff
// produces a delete+insert pair on each side. Applying both patches must
// succeed in either order; the upstream "already deleted" error breaks
// the merge model. Plan to upstream this to hackathon_2026 pushout.

// DeleteNode tombstones a live node: moves it to DeletedNodes, re-tags its
// edges as Deleted, and marks the component dirty for pseudo-edge resolution.
//
// Idempotent: deleting an already-deleted node is a no-op (returns nil).
// Required by the merge model — see the VENDOR DEVIATION note above.
func (inst *Graggle) DeleteNode(id t.NodeID) error {
	if inst.deletedNodes.Contains(id) {
		return nil
	}
	if !inst.nodes.Contains(id) {
		return eh.Errorf("node %v does not exist", id)
	}
	if id == t.RootNodeID {
		return eh.Errorf("cannot delete root node")
	}

	// Move from live to deleted.
	inst.nodes.Remove(id)
	inst.deletedNodes.Add(id)

	// Re-tag all edges involving this node from Live to Deleted.
	inst.retagEdgesForDeletion(id)

	// Add to union-find and merge with adjacent deleted nodes.
	inst.deletedPartition.Add(id)
	inst.mergeAdjacentDeleted(id)

	// Mark dirty.
	rep := inst.deletedPartition.Find(id)
	inst.dirtyReps[rep] = struct{}{}

	return nil
}

// UndeleteNode resurrects a tombstoned node.
//
// Undeletion can split a deleted component into several. Union-find is
// merge-only, so this method snapshots the component, drops every member
// from the partition (along with stale pseudo-edge bookkeeping for the old
// rep), then rebuilds new sub-components via recomputeDeletedComponents.
// Each sub-component is marked dirty under its own rep so resolve-time
// pseudo-edge recomputation handles each independently.
func (inst *Graggle) UndeleteNode(id t.NodeID) error {
	if !inst.deletedNodes.Contains(id) {
		return eh.Errorf("node %v is not deleted", id)
	}

	// Snapshot the original component before any mutation. Members may
	// include id itself.
	rep := inst.deletedPartition.Find(id)
	peers := inst.deletedPartition.Members(rep)

	// Drop pseudo-edge bookkeeping rooted at the old rep — the component is
	// about to be partitioned, so the old reasons are no longer valid.
	inst.dropReasonsForRep(rep)

	// Move id from deleted to live and retag its edges accordingly.
	inst.deletedNodes.Remove(id)
	inst.nodes.Add(id)
	inst.retagEdgesForUndeletion(id)

	// Remove every former peer from the partition so stale parent pointers
	// can't survive the rebuild. Then re-add the survivors as singletons.
	for _, m := range peers {
		inst.deletedPartition.Remove(m)
	}
	survivors := make([]t.NodeID, 0, len(peers))
	for _, m := range peers {
		if m == id {
			continue
		}
		inst.deletedPartition.Add(m)
		survivors = append(survivors, m)
	}
	if len(survivors) == 0 {
		return nil
	}

	// Recompute components from the surviving deleted subgraph and union
	// each into a fresh representative. Mark every component dirty.
	components := inst.recomputeDeletedComponents(survivors)
	for newRep, comp := range components {
		for _, m := range comp {
			inst.deletedPartition.Union(newRep, m)
		}
		inst.dirtyReps[inst.deletedPartition.Find(newRep)] = struct{}{}
	}

	return nil
}

// dropReasonsForRep removes pseudo-edge bookkeeping (and the corresponding
// graph edges, when the rep was the last reason) rooted at rep.
func (inst *Graggle) dropReasonsForRep(rep t.NodeID) {
	pes, ok := inst.reasonPseudoEdges[rep]
	if !ok {
		return
	}
	for _, pe := range pes {
		reasons, ok := inst.pseudoEdgeReasons[pe]
		if !ok {
			continue
		}
		delete(reasons, rep)
		if len(reasons) == 0 {
			delete(inst.pseudoEdgeReasons, pe)
			inst.RemoveEdge(pe.Src, pe.Dest, t.EdgeKindPseudo, t.PatchHash{})
		}
	}
	delete(inst.reasonPseudoEdges, rep)
}

// AddEdge adds a directed edge between two existing nodes.
func (inst *Graggle) AddEdge(src, dest t.NodeID, patch t.PatchHash) error {
	if !inst.HasNode(src) {
		return eh.Errorf("source node %v does not exist", src)
	}
	if !inst.HasNode(dest) {
		return eh.Errorf("dest node %v does not exist", dest)
	}
	kind := t.EdgeKindLive
	if inst.IsDeleted(src) || inst.IsDeleted(dest) {
		kind = t.EdgeKindDeleted
	}
	// If adding a live edge that duplicates an existing pseudo-edge, remove the pseudo-edge.
	if kind == t.EdgeKindLive {
		for _, e := range inst.edges.Get(src) {
			if e.Dest == dest && e.Kind == t.EdgeKindPseudo {
				inst.removePseudoEdgeWithReasons(src, dest)
				break
			}
		}
	}
	inst.addEdgeInternal(src, dest, kind, patch)
	// If both deleted and adjacent, merge in partition.
	if inst.IsDeleted(src) && inst.IsDeleted(dest) {
		inst.deletedPartition.Union(src, dest)
		rep := inst.deletedPartition.Find(src)
		inst.dirtyReps[rep] = struct{}{}
	}
	return nil
}

// RemoveEdge removes a specific edge.
func (inst *Graggle) RemoveEdge(src, dest t.NodeID, kind t.EdgeKindE, patch t.PatchHash) {
	e := t.Edge{Dest: dest, Kind: kind, IntroducedBy: patch}
	inst.edges.Remove(src, e)
	backE := t.Edge{Dest: src, Kind: kind, IntroducedBy: patch}
	inst.backEdges.Remove(dest, backE)
}

// addEdgeInternal adds an edge and its reverse without validation.
func (inst *Graggle) addEdgeInternal(src, dest t.NodeID, kind t.EdgeKindE, patch t.PatchHash) {
	e := t.Edge{Dest: dest, Kind: kind, IntroducedBy: patch}
	if !inst.edges.Has(src, e) {
		inst.edges.Add(src, e)
		backE := t.Edge{Dest: src, Kind: kind, IntroducedBy: patch}
		inst.backEdges.Add(dest, backE)
	}
}

// removePseudoEdgeWithReasons removes a pseudo-edge from the graph, cleans
// up its entries in the reason tracking maps, and marks affected components
// dirty so they are recomputed.
func (inst *Graggle) removePseudoEdgeWithReasons(src, dest t.NodeID) {
	pe := pseudoEdge{Src: src, Dest: dest}
	inst.RemoveEdge(src, dest, t.EdgeKindPseudo, t.PatchHash{})

	// Clean up reason maps and mark affected components dirty.
	if reasons, ok := inst.pseudoEdgeReasons[pe]; ok {
		for rep := range reasons {
			inst.dirtyReps[rep] = struct{}{}
			if pes, ok2 := inst.reasonPseudoEdges[rep]; ok2 {
				for i, p := range pes {
					if p == pe {
						inst.reasonPseudoEdges[rep] = append(pes[:i], pes[i+1:]...)
						break
					}
				}
				if len(inst.reasonPseudoEdges[rep]) == 0 {
					delete(inst.reasonPseudoEdges, rep)
				}
			}
		}
		delete(inst.pseudoEdgeReasons, pe)
	}
}

// retagEdgesForDeletion changes Live edges touching id to Deleted
// and removes Pseudo edges (they will be recomputed by ResolvePseudoEdges).
func (inst *Graggle) retagEdgesForDeletion(id t.NodeID) {
	// Snapshot edges before mutating — Remove/Add modify the underlying slice.
	// Forward edges from id.
	fwd := append([]t.Edge(nil), inst.edges.Get(id)...)
	for _, e := range fwd {
		if e.Kind == t.EdgeKindLive {
			inst.edges.Remove(id, e)
			inst.backEdges.Remove(e.Dest, t.Edge{Dest: id, Kind: t.EdgeKindLive, IntroducedBy: e.IntroducedBy})
			newE := t.Edge{Dest: e.Dest, Kind: t.EdgeKindDeleted, IntroducedBy: e.IntroducedBy}
			inst.edges.Add(id, newE)
			inst.backEdges.Add(e.Dest, t.Edge{Dest: id, Kind: t.EdgeKindDeleted, IntroducedBy: e.IntroducedBy})
		} else if e.Kind == t.EdgeKindPseudo {
			inst.removePseudoEdgeWithReasons(id, e.Dest)
		}
	}
	// Backward edges to id (i.e., edges from other nodes pointing to id).
	bwd := append([]t.Edge(nil), inst.backEdges.Get(id)...)
	for _, be := range bwd {
		if be.Kind == t.EdgeKindLive {
			src := be.Dest
			inst.backEdges.Remove(id, be)
			inst.edges.Remove(src, t.Edge{Dest: id, Kind: t.EdgeKindLive, IntroducedBy: be.IntroducedBy})
			inst.backEdges.Add(id, t.Edge{Dest: src, Kind: t.EdgeKindDeleted, IntroducedBy: be.IntroducedBy})
			inst.edges.Add(src, t.Edge{Dest: id, Kind: t.EdgeKindDeleted, IntroducedBy: be.IntroducedBy})
		} else if be.Kind == t.EdgeKindPseudo {
			inst.removePseudoEdgeWithReasons(be.Dest, id)
		}
	}
}

// retagEdgesForUndeletion changes Deleted edges touching id back to Live.
func (inst *Graggle) retagEdgesForUndeletion(id t.NodeID) {
	// Snapshot edges before mutating.
	fwd := append([]t.Edge(nil), inst.edges.Get(id)...)
	for _, e := range fwd {
		if e.Kind == t.EdgeKindDeleted {
			inst.edges.Remove(id, e)
			inst.backEdges.Remove(e.Dest, t.Edge{Dest: id, Kind: t.EdgeKindDeleted, IntroducedBy: e.IntroducedBy})
			newKind := t.EdgeKindLive
			if inst.IsDeleted(e.Dest) {
				newKind = t.EdgeKindDeleted
			}
			inst.edges.Add(id, t.Edge{Dest: e.Dest, Kind: newKind, IntroducedBy: e.IntroducedBy})
			inst.backEdges.Add(e.Dest, t.Edge{Dest: id, Kind: newKind, IntroducedBy: e.IntroducedBy})
		}
	}
	bwd := append([]t.Edge(nil), inst.backEdges.Get(id)...)
	for _, be := range bwd {
		if be.Kind == t.EdgeKindDeleted {
			src := be.Dest
			inst.backEdges.Remove(id, be)
			inst.edges.Remove(src, t.Edge{Dest: id, Kind: t.EdgeKindDeleted, IntroducedBy: be.IntroducedBy})
			newKind := t.EdgeKindLive
			if inst.IsDeleted(src) {
				newKind = t.EdgeKindDeleted
			}
			inst.backEdges.Add(id, t.Edge{Dest: src, Kind: newKind, IntroducedBy: be.IntroducedBy})
			inst.edges.Add(src, t.Edge{Dest: id, Kind: newKind, IntroducedBy: be.IntroducedBy})
		}
	}
}

// mergeAdjacentDeleted unions id with adjacent deleted nodes in the partition.
func (inst *Graggle) mergeAdjacentDeleted(id t.NodeID) {
	// Check forward neighbors.
	for _, e := range inst.edges.Get(id) {
		if inst.IsDeleted(e.Dest) {
			inst.deletedPartition.Union(id, e.Dest)
		}
	}
	// Check backward neighbors.
	for _, be := range inst.backEdges.Get(id) {
		if inst.IsDeleted(be.Dest) {
			inst.deletedPartition.Union(id, be.Dest)
		}
	}
}

// ResolvePseudoEdges recomputes pseudo-edges for all dirty deleted components.
// For each connected component of deleted nodes, it finds the live "boundary"
// nodes and adds pseudo-edges between them to keep the live subgraph connected.
func (inst *Graggle) ResolvePseudoEdges() {
	for dirtyRep := range inst.dirtyReps {
		inst.resolveComponent(dirtyRep)
	}
	inst.dirtyReps = make(map[t.NodeID]struct{})
}

func (inst *Graggle) resolveComponent(rep t.NodeID) {
	// Remove old pseudo-edges for this component.
	oldPseudos := inst.reasonPseudoEdges[rep]
	for _, pe := range oldPseudos {
		reasons := inst.pseudoEdgeReasons[pe]
		delete(reasons, rep)
		if len(reasons) == 0 {
			// No remaining reasons — remove the pseudo-edge.
			delete(inst.pseudoEdgeReasons, pe)
			inst.RemoveEdge(pe.Src, pe.Dest, t.EdgeKindPseudo, t.PatchHash{})
		}
	}
	delete(inst.reasonPseudoEdges, rep)

	// Collect all deleted nodes in this component.
	// After undeletions, the representative may no longer be in the partition.
	if !inst.deletedPartition.Contains(rep) {
		return
	}
	members := inst.deletedPartition.Members(rep)
	if len(members) == 0 {
		return
	}

	// Recompute connected components via DFS on the full graph (deleted subgraph).
	// This handles the case where undeletion split a component.
	components := inst.recomputeDeletedComponents(members)

	for newRep, component := range components {
		// Update partition: all members point to new representative.
		for _, m := range component {
			inst.deletedPartition.Add(m)
		}
		for _, m := range component {
			inst.deletedPartition.Union(newRep, m)
		}
		actualRep := inst.deletedPartition.Find(newRep)

		// Find boundary live nodes: live nodes adjacent to any deleted node in this component.
		boundary := inst.findBoundaryNodes(component)

		// For each pair of boundary nodes connected through the deleted component,
		// add a pseudo-edge (unless a live edge already exists).
		var newPseudos []pseudoEdge
		for _, src := range boundary.sources {
			// DFS through deleted nodes from src's neighbors to find reachable boundary dests.
			reachable := inst.findReachableBoundary(src, component, boundary.isDest)
			for _, dest := range reachable {
				if src == dest {
					continue
				}
				if inst.edges.HasLiveEdgeTo(src, dest) {
					continue
				}
				pe := pseudoEdge{Src: src, Dest: dest}
				newPseudos = append(newPseudos, pe)
				// Add the pseudo-edge to the graph.
				inst.addEdgeInternal(src, dest, t.EdgeKindPseudo, t.PatchHash{})
				// Track the reason.
				if inst.pseudoEdgeReasons[pe] == nil {
					inst.pseudoEdgeReasons[pe] = make(map[t.NodeID]struct{})
				}
				inst.pseudoEdgeReasons[pe][actualRep] = struct{}{}
			}
		}
		inst.reasonPseudoEdges[actualRep] = newPseudos
	}
}

// boundaryInfo holds live nodes adjacent to a deleted component.
type boundaryInfo struct {
	sources  []t.NodeID // live nodes with forward edges into the component
	dests    []t.NodeID // live nodes with backward edges from the component
	isSource map[t.NodeID]struct{}
	isDest   map[t.NodeID]struct{}
}

func (inst *Graggle) findBoundaryNodes(component []t.NodeID) boundaryInfo {
	info := boundaryInfo{
		isSource: make(map[t.NodeID]struct{}),
		isDest:   make(map[t.NodeID]struct{}),
	}

	for _, del := range component {
		// Live predecessors of del are sources.
		for _, be := range inst.backEdges.Get(del) {
			if inst.IsLive(be.Dest) {
				if _, ok := info.isSource[be.Dest]; !ok {
					info.isSource[be.Dest] = struct{}{}
					info.sources = append(info.sources, be.Dest)
				}
			}
		}
		// Live successors of del are dests.
		for _, e := range inst.edges.Get(del) {
			if inst.IsLive(e.Dest) {
				if _, ok := info.isDest[e.Dest]; !ok {
					info.isDest[e.Dest] = struct{}{}
					info.dests = append(info.dests, e.Dest)
				}
			}
		}
	}
	return info
}

// findReachableBoundary does a DFS from src through deleted nodes to find
// which boundary dest nodes are reachable.
func (inst *Graggle) findReachableBoundary(src t.NodeID, component []t.NodeID, destSet map[t.NodeID]struct{}) []t.NodeID {
	componentSet := make(map[t.NodeID]struct{}, len(component))
	for _, m := range component {
		componentSet[m] = struct{}{}
	}

	// Find deleted nodes directly reachable from src.
	var startNodes []t.NodeID
	for _, e := range inst.edges.Get(src) {
		if _, ok := componentSet[e.Dest]; ok {
			startNodes = append(startNodes, e.Dest)
		}
	}
	if len(startNodes) == 0 {
		return nil
	}

	// BFS/DFS through deleted component.
	visited := make(map[t.NodeID]struct{})
	stack := startNodes
	var reachable []t.NodeID
	seen := make(map[t.NodeID]struct{})

	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := visited[n]; ok {
			continue
		}
		visited[n] = struct{}{}

		for _, e := range inst.edges.Get(n) {
			if _, ok := componentSet[e.Dest]; ok {
				stack = append(stack, e.Dest)
			} else if _, ok := destSet[e.Dest]; ok {
				if _, ok2 := seen[e.Dest]; !ok2 {
					seen[e.Dest] = struct{}{}
					reachable = append(reachable, e.Dest)
				}
			}
		}
	}
	return reachable
}

// recomputeDeletedComponents takes a set of deleted nodes and partitions them
// into connected components via DFS on the full (forward+backward) graph.
func (inst *Graggle) recomputeDeletedComponents(members []t.NodeID) map[t.NodeID][]t.NodeID {
	memberSet := make(map[t.NodeID]struct{}, len(members))
	for _, m := range members {
		memberSet[m] = struct{}{}
	}

	visited := make(map[t.NodeID]struct{})
	components := make(map[t.NodeID][]t.NodeID)

	for _, m := range members {
		if _, ok := visited[m]; ok {
			continue
		}
		// BFS from m through deleted nodes.
		var comp []t.NodeID
		queue := []t.NodeID{m}
		for len(queue) > 0 {
			n := queue[0]
			queue = queue[1:]
			if _, ok := visited[n]; ok {
				continue
			}
			visited[n] = struct{}{}
			comp = append(comp, n)
			// Follow all edges to other deleted members.
			for _, e := range inst.edges.Get(n) {
				if _, ok := memberSet[e.Dest]; ok {
					queue = append(queue, e.Dest)
				}
			}
			for _, be := range inst.backEdges.Get(n) {
				if _, ok := memberSet[be.Dest]; ok {
					queue = append(queue, be.Dest)
				}
			}
		}
		components[m] = comp
	}
	return components
}

// LiveChildren yields live forward neighbors of id (live + pseudo edges to live nodes).
func (inst *Graggle) LiveChildren(id t.NodeID) iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, e := range inst.edges.Get(id) {
			if e.Kind == t.EdgeKindLive {
				if !yield(e.Dest) {
					return
				}
			} else if e.Kind == t.EdgeKindPseudo && inst.IsLive(e.Dest) {
				if !yield(e.Dest) {
					return
				}
			}
		}
	}
}

// LiveParents yields live predecessors of id (following back-edges).
func (inst *Graggle) LiveParents(id t.NodeID) iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, be := range inst.backEdges.Get(id) {
			if be.Kind == t.EdgeKindLive {
				if !yield(be.Dest) {
					return
				}
			} else if be.Kind == t.EdgeKindPseudo && inst.IsLive(be.Dest) {
				if !yield(be.Dest) {
					return
				}
			}
		}
	}
}

// AllLiveNodes yields all live node IDs in the graggle.
func (inst *Graggle) AllLiveNodes() iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, id := range inst.nodes.Items() {
			if !yield(id) {
				return
			}
		}
	}
}

// RemoveNode removes a node and its content from the live set.
// Used by Unapply to clean up nodes added by a patch.
func (inst *Graggle) RemoveNode(id t.NodeID) {
	inst.nodes.Remove(id)
	delete(inst.contents, id)
}

// NodeContent returns the content of a node, or nil if not found.
func (inst *Graggle) NodeContent(id t.NodeID) []byte {
	return inst.contents[id]
}

// --- InspectableI / VisualizableI adapter methods ---

// AllDeletedNodes yields all deleted (tombstoned) node IDs.
func (inst *Graggle) AllDeletedNodes() iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, id := range inst.deletedNodes.Items() {
			if !yield(id) {
				return
			}
		}
	}
}

// ForwardEdges yields all forward edges from src (all kinds).
func (inst *Graggle) ForwardEdges(src t.NodeID) iter.Seq[t.Edge] {
	return func(yield func(t.Edge) bool) {
		for _, e := range inst.edges.Get(src) {
			if !yield(e) {
				return
			}
		}
	}
}

// ForwardEdgeSources yields all node IDs that have outgoing edges.
func (inst *Graggle) ForwardEdgeSources() iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, src := range inst.edges.Sources() {
			if !yield(src) {
				return
			}
		}
	}
}

// BackwardEdges yields all backward edges to dest (all kinds).
func (inst *Graggle) BackwardEdges(dest t.NodeID) iter.Seq[t.Edge] {
	return func(yield func(t.Edge) bool) {
		for _, be := range inst.backEdges.Get(dest) {
			if !yield(be) {
				return
			}
		}
	}
}

// BackwardEdgeSources yields all node IDs that have incoming edges.
func (inst *Graggle) BackwardEdgeSources() iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, dest := range inst.backEdges.Sources() {
			if !yield(dest) {
				return
			}
		}
	}
}

// HasLiveEdgeTo checks if there is a live edge from src to dest.
func (inst *Graggle) HasLiveEdgeTo(src, dest t.NodeID) bool {
	return inst.edges.HasLiveEdgeTo(src, dest)
}

// DeletedPartitionContains returns true if id is in the deleted partition.
func (inst *Graggle) DeletedPartitionContains(id t.NodeID) bool {
	return inst.deletedPartition.Contains(id)
}

// DeletedPartitionFind returns the representative of id's component.
func (inst *Graggle) DeletedPartitionFind(id t.NodeID) t.NodeID {
	return inst.deletedPartition.Find(id)
}

// DeletedPartitionRepresentatives yields one representative per deleted component.
func (inst *Graggle) DeletedPartitionRepresentatives() iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, r := range inst.deletedPartition.Representatives() {
			if !yield(r) {
				return
			}
		}
	}
}

// DeletedPartitionMembers yields all members of the component containing rep.
func (inst *Graggle) DeletedPartitionMembers(rep t.NodeID) iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, m := range inst.deletedPartition.Members(rep) {
			if !yield(m) {
				return
			}
		}
	}
}

// DirtyRepCount returns the number of dirty component representatives.
func (inst *Graggle) DirtyRepCount() int {
	return len(inst.dirtyReps)
}

// PseudoEdgeReasonCount returns the number of components justifying pseudo-edge src→dest.
func (inst *Graggle) PseudoEdgeReasonCount(src, dest t.NodeID) int {
	pe := pseudoEdge{Src: src, Dest: dest}
	return len(inst.pseudoEdgeReasons[pe])
}

// ReasonPseudoEdgesForRep yields [src,dest] pairs of pseudo-edges justified by rep.
func (inst *Graggle) ReasonPseudoEdgesForRep(rep t.NodeID) iter.Seq[[2]t.NodeID] {
	return func(yield func([2]t.NodeID) bool) {
		for _, pe := range inst.reasonPseudoEdges[rep] {
			if !yield([2]t.NodeID{pe.Src, pe.Dest}) {
				return
			}
		}
	}
}

// AllTrackedPseudoEdges yields [src,dest] pairs of all tracked pseudo-edges.
func (inst *Graggle) AllTrackedPseudoEdges() iter.Seq[[2]t.NodeID] {
	return func(yield func([2]t.NodeID) bool) {
		for pe := range inst.pseudoEdgeReasons {
			if !yield([2]t.NodeID{pe.Src, pe.Dest}) {
				return
			}
		}
	}
}

// ExportFindBoundaryNodes returns live boundary nodes adjacent to a deleted component.
// sources: live nodes with edges into the component.
// dests: live nodes with edges from the component.
func (inst *Graggle) ExportFindBoundaryNodes(component []t.NodeID) (sources, dests []t.NodeID) {
	info := inst.findBoundaryNodes(component)
	return info.sources, info.dests
}

// ExportFindReachableBoundary returns boundary dest nodes reachable from src through
// the deleted component.
func (inst *Graggle) ExportFindReachableBoundary(src t.NodeID, component, dests []t.NodeID) []t.NodeID {
	destSet := make(map[t.NodeID]struct{}, len(dests))
	for _, d := range dests {
		destSet[d] = struct{}{}
	}
	return inst.findReachableBoundary(src, component, destSet)
}

// CloneStore creates a deep copy that satisfies GraphStoreI.
func (inst *Graggle) CloneStore() t.GraphStoreI {
	return inst.Clone()
}

// Clone creates a deep copy of the graggle.
func (inst *Graggle) Clone() *Graggle {
	ng := New()
	// Remove the root that New() adds; we'll copy everything.
	ng.nodes = t.NewNodeSet()
	ng.contents = make(map[t.NodeID][]byte)

	for _, id := range inst.nodes.Items() {
		ng.nodes.Add(id)
	}
	for id, content := range inst.contents {
		c := make([]byte, len(content))
		copy(c, content)
		ng.contents[id] = c
	}
	for _, id := range inst.deletedNodes.Items() {
		ng.deletedNodes.Add(id)
	}
	// Copy edges.
	for _, src := range inst.edges.Sources() {
		for _, e := range inst.edges.Get(src) {
			ng.edges.Add(src, e)
		}
	}
	for _, dest := range inst.backEdges.Sources() {
		for _, be := range inst.backEdges.Get(dest) {
			ng.backEdges.Add(dest, be)
		}
	}
	// Copy partition.
	ng.deletedPartition = t.NewUnionFind()
	for _, id := range inst.deletedNodes.Items() {
		ng.deletedPartition.Add(id)
	}
	for _, id := range inst.deletedNodes.Items() {
		if inst.deletedPartition.Contains(id) {
			rep := inst.deletedPartition.Find(id)
			if ng.deletedPartition.Contains(rep) {
				ng.deletedPartition.Union(id, rep)
			}
		}
	}
	// Copy pseudo-edge reasons.
	ng.reasonPseudoEdges = make(map[t.NodeID][]pseudoEdge)
	for rep, pes := range inst.reasonPseudoEdges {
		cp := make([]pseudoEdge, len(pes))
		copy(cp, pes)
		ng.reasonPseudoEdges[rep] = cp
	}
	ng.pseudoEdgeReasons = make(map[pseudoEdge]map[t.NodeID]struct{})
	for pe, reasons := range inst.pseudoEdgeReasons {
		m := make(map[t.NodeID]struct{})
		for r := range reasons {
			m[r] = struct{}{}
		}
		ng.pseudoEdgeReasons[pe] = m
	}
	for rep := range inst.dirtyReps {
		ng.dirtyReps[rep] = struct{}{}
	}
	return ng
}

// Debug returns a human-readable dump of the graggle.
func (inst *Graggle) Debug() string {
	s := fmt.Sprintf("Live nodes (%d):\n", inst.nodes.Len())
	for _, id := range inst.nodes.Items() {
		s += fmt.Sprintf("  %v: %q\n", id, string(inst.contents[id]))
	}
	s += fmt.Sprintf("Deleted nodes (%d):\n", inst.deletedNodes.Len())
	for _, id := range inst.deletedNodes.Items() {
		s += fmt.Sprintf("  %v: %q\n", id, string(inst.contents[id]))
	}
	s += "Edges:\n"
	for _, src := range inst.edges.Sources() {
		for _, e := range inst.edges.Get(src) {
			s += fmt.Sprintf("  %v --%s--> %v\n", src, e.Kind, e.Dest)
		}
	}
	return s
}