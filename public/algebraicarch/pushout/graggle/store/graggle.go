//go:build llm_generated_opus47

// Package store provides the concrete Graggle data structure implementing
// all graph interfaces defined in the types package.
package store

import (
	"fmt"
	"iter"

	t "github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/types"
)

// Graggle is the core data structure: a directed graph of lines (nodes).
// It generalises a file's linear order into a partial order, enabling
// mathematically correct merging via categorical pushouts.
//
// Deleted nodes are tombstoned ("ghost lines"), never truly removed.
// Pseudo-edges bridge over deleted regions so the live subgraph stays connected.
//
// All state is unexported. External read access goes through the
// GraphReader/Inspectable/Visualizable interfaces (see graggle/types).
// External mutation goes through GraphWriter — direct field access would
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
	g := &Graggle{
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
	g.nodes.Add(t.RootNodeID)
	return g
}

// HasNode returns true if the node exists (live or deleted).
func (g *Graggle) HasNode(id t.NodeID) bool {
	return g.nodes.Contains(id) || g.deletedNodes.Contains(id)
}

// IsLive returns true if the node is alive (not deleted).
func (g *Graggle) IsLive(id t.NodeID) bool {
	return g.nodes.Contains(id)
}

// IsDeleted returns true if the node is a tombstone.
func (g *Graggle) IsDeleted(id t.NodeID) bool {
	return g.deletedNodes.Contains(id)
}

// LiveNodeCount returns the number of live (non-tombstoned) nodes,
// including the root.
func (g *Graggle) LiveNodeCount() int {
	return g.nodes.Len()
}

// DeletedNodeCount returns the number of tombstoned (ghost) nodes.
func (g *Graggle) DeletedNodeCount() int {
	return g.deletedNodes.Len()
}

// AddNode inserts a new live node with the given content and connects it
// between the given context nodes.
// upContext: nodes that should precede this node.
// downContext: nodes that should follow this node.
func (g *Graggle) AddNode(id t.NodeID, content []byte, patch t.PatchHash, upContext, downContext []t.NodeID) error {
	if g.HasNode(id) {
		return fmt.Errorf("node %v already exists", id)
	}
	g.nodes.Add(id)
	g.contents[id] = content

	for _, up := range upContext {
		if !g.HasNode(up) {
			return fmt.Errorf("up-context node %v does not exist", up)
		}
		kind := t.EdgeLive
		if g.IsDeleted(up) {
			kind = t.EdgeDeleted
			// Mark the deleted component dirty so pseudo-edges are recomputed.
			if g.deletedPartition.Contains(up) {
				rep := g.deletedPartition.Find(up)
				g.dirtyReps[rep] = struct{}{}
			}
		}
		g.addEdgeInternal(up, id, kind, patch)
	}
	for _, down := range downContext {
		if !g.HasNode(down) {
			return fmt.Errorf("down-context node %v does not exist", down)
		}
		kind := t.EdgeLive
		if g.IsDeleted(down) {
			kind = t.EdgeDeleted
			if g.deletedPartition.Contains(down) {
				rep := g.deletedPartition.Find(down)
				g.dirtyReps[rep] = struct{}{}
			}
		}
		g.addEdgeInternal(id, down, kind, patch)
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
func (g *Graggle) DeleteNode(id t.NodeID) error {
	if g.deletedNodes.Contains(id) {
		return nil
	}
	if !g.nodes.Contains(id) {
		return fmt.Errorf("node %v does not exist", id)
	}
	if id == t.RootNodeID {
		return fmt.Errorf("cannot delete root node")
	}

	// Move from live to deleted.
	g.nodes.Remove(id)
	g.deletedNodes.Add(id)

	// Re-tag all edges involving this node from Live to Deleted.
	g.retagEdgesForDeletion(id)

	// Add to union-find and merge with adjacent deleted nodes.
	g.deletedPartition.Add(id)
	g.mergeAdjacentDeleted(id)

	// Mark dirty.
	rep := g.deletedPartition.Find(id)
	g.dirtyReps[rep] = struct{}{}

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
func (g *Graggle) UndeleteNode(id t.NodeID) error {
	if !g.deletedNodes.Contains(id) {
		return fmt.Errorf("node %v is not deleted", id)
	}

	// Snapshot the original component before any mutation. Members may
	// include id itself.
	rep := g.deletedPartition.Find(id)
	peers := g.deletedPartition.Members(rep)

	// Drop pseudo-edge bookkeeping rooted at the old rep — the component is
	// about to be partitioned, so the old reasons are no longer valid.
	g.dropReasonsForRep(rep)

	// Move id from deleted to live and retag its edges accordingly.
	g.deletedNodes.Remove(id)
	g.nodes.Add(id)
	g.retagEdgesForUndeletion(id)

	// Remove every former peer from the partition so stale parent pointers
	// can't survive the rebuild. Then re-add the survivors as singletons.
	for _, m := range peers {
		g.deletedPartition.Remove(m)
	}
	survivors := make([]t.NodeID, 0, len(peers))
	for _, m := range peers {
		if m == id {
			continue
		}
		g.deletedPartition.Add(m)
		survivors = append(survivors, m)
	}
	if len(survivors) == 0 {
		return nil
	}

	// Recompute components from the surviving deleted subgraph and union
	// each into a fresh representative. Mark every component dirty.
	components := g.recomputeDeletedComponents(survivors)
	for newRep, comp := range components {
		for _, m := range comp {
			g.deletedPartition.Union(newRep, m)
		}
		g.dirtyReps[g.deletedPartition.Find(newRep)] = struct{}{}
	}

	return nil
}

// dropReasonsForRep removes pseudo-edge bookkeeping (and the corresponding
// graph edges, when the rep was the last reason) rooted at rep.
func (g *Graggle) dropReasonsForRep(rep t.NodeID) {
	pes, ok := g.reasonPseudoEdges[rep]
	if !ok {
		return
	}
	for _, pe := range pes {
		reasons, ok := g.pseudoEdgeReasons[pe]
		if !ok {
			continue
		}
		delete(reasons, rep)
		if len(reasons) == 0 {
			delete(g.pseudoEdgeReasons, pe)
			g.RemoveEdge(pe.Src, pe.Dest, t.EdgePseudo, t.PatchHash{})
		}
	}
	delete(g.reasonPseudoEdges, rep)
}

// AddEdge adds a directed edge between two existing nodes.
func (g *Graggle) AddEdge(src, dest t.NodeID, patch t.PatchHash) error {
	if !g.HasNode(src) {
		return fmt.Errorf("source node %v does not exist", src)
	}
	if !g.HasNode(dest) {
		return fmt.Errorf("dest node %v does not exist", dest)
	}
	kind := t.EdgeLive
	if g.IsDeleted(src) || g.IsDeleted(dest) {
		kind = t.EdgeDeleted
	}
	// If adding a live edge that duplicates an existing pseudo-edge, remove the pseudo-edge.
	if kind == t.EdgeLive {
		for _, e := range g.edges.Get(src) {
			if e.Dest == dest && e.Kind == t.EdgePseudo {
				g.removePseudoEdgeWithReasons(src, dest)
				break
			}
		}
	}
	g.addEdgeInternal(src, dest, kind, patch)
	// If both deleted and adjacent, merge in partition.
	if g.IsDeleted(src) && g.IsDeleted(dest) {
		g.deletedPartition.Union(src, dest)
		rep := g.deletedPartition.Find(src)
		g.dirtyReps[rep] = struct{}{}
	}
	return nil
}

// RemoveEdge removes a specific edge.
func (g *Graggle) RemoveEdge(src, dest t.NodeID, kind t.EdgeKind, patch t.PatchHash) {
	e := t.Edge{Dest: dest, Kind: kind, IntroducedBy: patch}
	g.edges.Remove(src, e)
	backE := t.Edge{Dest: src, Kind: kind, IntroducedBy: patch}
	g.backEdges.Remove(dest, backE)
}

// addEdgeInternal adds an edge and its reverse without validation.
func (g *Graggle) addEdgeInternal(src, dest t.NodeID, kind t.EdgeKind, patch t.PatchHash) {
	e := t.Edge{Dest: dest, Kind: kind, IntroducedBy: patch}
	if !g.edges.Has(src, e) {
		g.edges.Add(src, e)
		backE := t.Edge{Dest: src, Kind: kind, IntroducedBy: patch}
		g.backEdges.Add(dest, backE)
	}
}

// removePseudoEdgeWithReasons removes a pseudo-edge from the graph, cleans
// up its entries in the reason tracking maps, and marks affected components
// dirty so they are recomputed.
func (g *Graggle) removePseudoEdgeWithReasons(src, dest t.NodeID) {
	pe := pseudoEdge{Src: src, Dest: dest}
	g.RemoveEdge(src, dest, t.EdgePseudo, t.PatchHash{})

	// Clean up reason maps and mark affected components dirty.
	if reasons, ok := g.pseudoEdgeReasons[pe]; ok {
		for rep := range reasons {
			g.dirtyReps[rep] = struct{}{}
			if pes, ok2 := g.reasonPseudoEdges[rep]; ok2 {
				for i, p := range pes {
					if p == pe {
						g.reasonPseudoEdges[rep] = append(pes[:i], pes[i+1:]...)
						break
					}
				}
				if len(g.reasonPseudoEdges[rep]) == 0 {
					delete(g.reasonPseudoEdges, rep)
				}
			}
		}
		delete(g.pseudoEdgeReasons, pe)
	}
}

// retagEdgesForDeletion changes Live edges touching id to Deleted
// and removes Pseudo edges (they will be recomputed by ResolvePseudoEdges).
func (g *Graggle) retagEdgesForDeletion(id t.NodeID) {
	// Snapshot edges before mutating — Remove/Add modify the underlying slice.
	// Forward edges from id.
	fwd := append([]t.Edge(nil), g.edges.Get(id)...)
	for _, e := range fwd {
		if e.Kind == t.EdgeLive {
			g.edges.Remove(id, e)
			g.backEdges.Remove(e.Dest, t.Edge{Dest: id, Kind: t.EdgeLive, IntroducedBy: e.IntroducedBy})
			newE := t.Edge{Dest: e.Dest, Kind: t.EdgeDeleted, IntroducedBy: e.IntroducedBy}
			g.edges.Add(id, newE)
			g.backEdges.Add(e.Dest, t.Edge{Dest: id, Kind: t.EdgeDeleted, IntroducedBy: e.IntroducedBy})
		} else if e.Kind == t.EdgePseudo {
			g.removePseudoEdgeWithReasons(id, e.Dest)
		}
	}
	// Backward edges to id (i.e., edges from other nodes pointing to id).
	bwd := append([]t.Edge(nil), g.backEdges.Get(id)...)
	for _, be := range bwd {
		if be.Kind == t.EdgeLive {
			src := be.Dest
			g.backEdges.Remove(id, be)
			g.edges.Remove(src, t.Edge{Dest: id, Kind: t.EdgeLive, IntroducedBy: be.IntroducedBy})
			g.backEdges.Add(id, t.Edge{Dest: src, Kind: t.EdgeDeleted, IntroducedBy: be.IntroducedBy})
			g.edges.Add(src, t.Edge{Dest: id, Kind: t.EdgeDeleted, IntroducedBy: be.IntroducedBy})
		} else if be.Kind == t.EdgePseudo {
			g.removePseudoEdgeWithReasons(be.Dest, id)
		}
	}
}

// retagEdgesForUndeletion changes Deleted edges touching id back to Live.
func (g *Graggle) retagEdgesForUndeletion(id t.NodeID) {
	// Snapshot edges before mutating.
	fwd := append([]t.Edge(nil), g.edges.Get(id)...)
	for _, e := range fwd {
		if e.Kind == t.EdgeDeleted {
			g.edges.Remove(id, e)
			g.backEdges.Remove(e.Dest, t.Edge{Dest: id, Kind: t.EdgeDeleted, IntroducedBy: e.IntroducedBy})
			newKind := t.EdgeLive
			if g.IsDeleted(e.Dest) {
				newKind = t.EdgeDeleted
			}
			g.edges.Add(id, t.Edge{Dest: e.Dest, Kind: newKind, IntroducedBy: e.IntroducedBy})
			g.backEdges.Add(e.Dest, t.Edge{Dest: id, Kind: newKind, IntroducedBy: e.IntroducedBy})
		}
	}
	bwd := append([]t.Edge(nil), g.backEdges.Get(id)...)
	for _, be := range bwd {
		if be.Kind == t.EdgeDeleted {
			src := be.Dest
			g.backEdges.Remove(id, be)
			g.edges.Remove(src, t.Edge{Dest: id, Kind: t.EdgeDeleted, IntroducedBy: be.IntroducedBy})
			newKind := t.EdgeLive
			if g.IsDeleted(src) {
				newKind = t.EdgeDeleted
			}
			g.backEdges.Add(id, t.Edge{Dest: src, Kind: newKind, IntroducedBy: be.IntroducedBy})
			g.edges.Add(src, t.Edge{Dest: id, Kind: newKind, IntroducedBy: be.IntroducedBy})
		}
	}
}

// mergeAdjacentDeleted unions id with adjacent deleted nodes in the partition.
func (g *Graggle) mergeAdjacentDeleted(id t.NodeID) {
	// Check forward neighbors.
	for _, e := range g.edges.Get(id) {
		if g.IsDeleted(e.Dest) {
			g.deletedPartition.Union(id, e.Dest)
		}
	}
	// Check backward neighbors.
	for _, be := range g.backEdges.Get(id) {
		if g.IsDeleted(be.Dest) {
			g.deletedPartition.Union(id, be.Dest)
		}
	}
}

// ResolvePseudoEdges recomputes pseudo-edges for all dirty deleted components.
// For each connected component of deleted nodes, it finds the live "boundary"
// nodes and adds pseudo-edges between them to keep the live subgraph connected.
func (g *Graggle) ResolvePseudoEdges() {
	for dirtyRep := range g.dirtyReps {
		g.resolveComponent(dirtyRep)
	}
	g.dirtyReps = make(map[t.NodeID]struct{})
}

func (g *Graggle) resolveComponent(rep t.NodeID) {
	// Remove old pseudo-edges for this component.
	oldPseudos := g.reasonPseudoEdges[rep]
	for _, pe := range oldPseudos {
		reasons := g.pseudoEdgeReasons[pe]
		delete(reasons, rep)
		if len(reasons) == 0 {
			// No remaining reasons — remove the pseudo-edge.
			delete(g.pseudoEdgeReasons, pe)
			g.RemoveEdge(pe.Src, pe.Dest, t.EdgePseudo, t.PatchHash{})
		}
	}
	delete(g.reasonPseudoEdges, rep)

	// Collect all deleted nodes in this component.
	// After undeletions, the representative may no longer be in the partition.
	if !g.deletedPartition.Contains(rep) {
		return
	}
	members := g.deletedPartition.Members(rep)
	if len(members) == 0 {
		return
	}

	// Recompute connected components via DFS on the full graph (deleted subgraph).
	// This handles the case where undeletion split a component.
	components := g.recomputeDeletedComponents(members)

	for newRep, component := range components {
		// Update partition: all members point to new representative.
		for _, m := range component {
			g.deletedPartition.Add(m)
		}
		for _, m := range component {
			g.deletedPartition.Union(newRep, m)
		}
		actualRep := g.deletedPartition.Find(newRep)

		// Find boundary live nodes: live nodes adjacent to any deleted node in this component.
		boundary := g.findBoundaryNodes(component)

		// For each pair of boundary nodes connected through the deleted component,
		// add a pseudo-edge (unless a live edge already exists).
		var newPseudos []pseudoEdge
		for _, src := range boundary.sources {
			// DFS through deleted nodes from src's neighbors to find reachable boundary dests.
			reachable := g.findReachableBoundary(src, component, boundary.isDest)
			for _, dest := range reachable {
				if src == dest {
					continue
				}
				if g.edges.HasLiveEdgeTo(src, dest) {
					continue
				}
				pe := pseudoEdge{Src: src, Dest: dest}
				newPseudos = append(newPseudos, pe)
				// Add the pseudo-edge to the graph.
				g.addEdgeInternal(src, dest, t.EdgePseudo, t.PatchHash{})
				// Track the reason.
				if g.pseudoEdgeReasons[pe] == nil {
					g.pseudoEdgeReasons[pe] = make(map[t.NodeID]struct{})
				}
				g.pseudoEdgeReasons[pe][actualRep] = struct{}{}
			}
		}
		g.reasonPseudoEdges[actualRep] = newPseudos
	}
}

// boundaryInfo holds live nodes adjacent to a deleted component.
type boundaryInfo struct {
	sources  []t.NodeID // live nodes with forward edges into the component
	dests    []t.NodeID // live nodes with backward edges from the component
	isSource map[t.NodeID]struct{}
	isDest   map[t.NodeID]struct{}
}

func (g *Graggle) findBoundaryNodes(component []t.NodeID) boundaryInfo {
	info := boundaryInfo{
		isSource: make(map[t.NodeID]struct{}),
		isDest:   make(map[t.NodeID]struct{}),
	}

	for _, del := range component {
		// Live predecessors of del are sources.
		for _, be := range g.backEdges.Get(del) {
			if g.IsLive(be.Dest) {
				if _, ok := info.isSource[be.Dest]; !ok {
					info.isSource[be.Dest] = struct{}{}
					info.sources = append(info.sources, be.Dest)
				}
			}
		}
		// Live successors of del are dests.
		for _, e := range g.edges.Get(del) {
			if g.IsLive(e.Dest) {
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
func (g *Graggle) findReachableBoundary(src t.NodeID, component []t.NodeID, destSet map[t.NodeID]struct{}) []t.NodeID {
	componentSet := make(map[t.NodeID]struct{}, len(component))
	for _, m := range component {
		componentSet[m] = struct{}{}
	}

	// Find deleted nodes directly reachable from src.
	var startNodes []t.NodeID
	for _, e := range g.edges.Get(src) {
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

		for _, e := range g.edges.Get(n) {
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
func (g *Graggle) recomputeDeletedComponents(members []t.NodeID) map[t.NodeID][]t.NodeID {
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
			for _, e := range g.edges.Get(n) {
				if _, ok := memberSet[e.Dest]; ok {
					queue = append(queue, e.Dest)
				}
			}
			for _, be := range g.backEdges.Get(n) {
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
func (g *Graggle) LiveChildren(id t.NodeID) iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, e := range g.edges.Get(id) {
			if e.Kind == t.EdgeLive {
				if !yield(e.Dest) {
					return
				}
			} else if e.Kind == t.EdgePseudo && g.IsLive(e.Dest) {
				if !yield(e.Dest) {
					return
				}
			}
		}
	}
}

// LiveParents yields live predecessors of id (following back-edges).
func (g *Graggle) LiveParents(id t.NodeID) iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, be := range g.backEdges.Get(id) {
			if be.Kind == t.EdgeLive {
				if !yield(be.Dest) {
					return
				}
			} else if be.Kind == t.EdgePseudo && g.IsLive(be.Dest) {
				if !yield(be.Dest) {
					return
				}
			}
		}
	}
}

// AllLiveNodes yields all live node IDs in the graggle.
func (g *Graggle) AllLiveNodes() iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, id := range g.nodes.Items() {
			if !yield(id) {
				return
			}
		}
	}
}

// RemoveNode removes a node and its content from the live set.
// Used by Unapply to clean up nodes added by a patch.
func (g *Graggle) RemoveNode(id t.NodeID) {
	g.nodes.Remove(id)
	delete(g.contents, id)
}

// NodeContent returns the content of a node, or nil if not found.
func (g *Graggle) NodeContent(id t.NodeID) []byte {
	return g.contents[id]
}

// --- Inspectable / Visualizable adapter methods ---

// AllDeletedNodes yields all deleted (tombstoned) node IDs.
func (g *Graggle) AllDeletedNodes() iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, id := range g.deletedNodes.Items() {
			if !yield(id) {
				return
			}
		}
	}
}

// ForwardEdges yields all forward edges from src (all kinds).
func (g *Graggle) ForwardEdges(src t.NodeID) iter.Seq[t.Edge] {
	return func(yield func(t.Edge) bool) {
		for _, e := range g.edges.Get(src) {
			if !yield(e) {
				return
			}
		}
	}
}

// ForwardEdgeSources yields all node IDs that have outgoing edges.
func (g *Graggle) ForwardEdgeSources() iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, src := range g.edges.Sources() {
			if !yield(src) {
				return
			}
		}
	}
}

// BackwardEdges yields all backward edges to dest (all kinds).
func (g *Graggle) BackwardEdges(dest t.NodeID) iter.Seq[t.Edge] {
	return func(yield func(t.Edge) bool) {
		for _, be := range g.backEdges.Get(dest) {
			if !yield(be) {
				return
			}
		}
	}
}

// BackwardEdgeSources yields all node IDs that have incoming edges.
func (g *Graggle) BackwardEdgeSources() iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, dest := range g.backEdges.Sources() {
			if !yield(dest) {
				return
			}
		}
	}
}

// HasLiveEdgeTo checks if there is a live edge from src to dest.
func (g *Graggle) HasLiveEdgeTo(src, dest t.NodeID) bool {
	return g.edges.HasLiveEdgeTo(src, dest)
}

// DeletedPartitionContains returns true if id is in the deleted partition.
func (g *Graggle) DeletedPartitionContains(id t.NodeID) bool {
	return g.deletedPartition.Contains(id)
}

// DeletedPartitionFind returns the representative of id's component.
func (g *Graggle) DeletedPartitionFind(id t.NodeID) t.NodeID {
	return g.deletedPartition.Find(id)
}

// DeletedPartitionRepresentatives yields one representative per deleted component.
func (g *Graggle) DeletedPartitionRepresentatives() iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, r := range g.deletedPartition.Representatives() {
			if !yield(r) {
				return
			}
		}
	}
}

// DeletedPartitionMembers yields all members of the component containing rep.
func (g *Graggle) DeletedPartitionMembers(rep t.NodeID) iter.Seq[t.NodeID] {
	return func(yield func(t.NodeID) bool) {
		for _, m := range g.deletedPartition.Members(rep) {
			if !yield(m) {
				return
			}
		}
	}
}

// DirtyRepCount returns the number of dirty component representatives.
func (g *Graggle) DirtyRepCount() int {
	return len(g.dirtyReps)
}

// PseudoEdgeReasonCount returns the number of components justifying pseudo-edge src→dest.
func (g *Graggle) PseudoEdgeReasonCount(src, dest t.NodeID) int {
	pe := pseudoEdge{Src: src, Dest: dest}
	return len(g.pseudoEdgeReasons[pe])
}

// ReasonPseudoEdgesForRep yields [src,dest] pairs of pseudo-edges justified by rep.
func (g *Graggle) ReasonPseudoEdgesForRep(rep t.NodeID) iter.Seq[[2]t.NodeID] {
	return func(yield func([2]t.NodeID) bool) {
		for _, pe := range g.reasonPseudoEdges[rep] {
			if !yield([2]t.NodeID{pe.Src, pe.Dest}) {
				return
			}
		}
	}
}

// AllTrackedPseudoEdges yields [src,dest] pairs of all tracked pseudo-edges.
func (g *Graggle) AllTrackedPseudoEdges() iter.Seq[[2]t.NodeID] {
	return func(yield func([2]t.NodeID) bool) {
		for pe := range g.pseudoEdgeReasons {
			if !yield([2]t.NodeID{pe.Src, pe.Dest}) {
				return
			}
		}
	}
}

// ExportFindBoundaryNodes returns live boundary nodes adjacent to a deleted component.
// sources: live nodes with edges into the component.
// dests: live nodes with edges from the component.
func (g *Graggle) ExportFindBoundaryNodes(component []t.NodeID) (sources, dests []t.NodeID) {
	info := g.findBoundaryNodes(component)
	return info.sources, info.dests
}

// ExportFindReachableBoundary returns boundary dest nodes reachable from src through
// the deleted component.
func (g *Graggle) ExportFindReachableBoundary(src t.NodeID, component, dests []t.NodeID) []t.NodeID {
	destSet := make(map[t.NodeID]struct{}, len(dests))
	for _, d := range dests {
		destSet[d] = struct{}{}
	}
	return g.findReachableBoundary(src, component, destSet)
}

// CloneStore creates a deep copy that satisfies GraphStore.
func (g *Graggle) CloneStore() t.GraphStore {
	return g.Clone()
}

// Clone creates a deep copy of the graggle.
func (g *Graggle) Clone() *Graggle {
	ng := New()
	// Remove the root that New() adds; we'll copy everything.
	ng.nodes = t.NewNodeSet()
	ng.contents = make(map[t.NodeID][]byte)

	for _, id := range g.nodes.Items() {
		ng.nodes.Add(id)
	}
	for id, content := range g.contents {
		c := make([]byte, len(content))
		copy(c, content)
		ng.contents[id] = c
	}
	for _, id := range g.deletedNodes.Items() {
		ng.deletedNodes.Add(id)
	}
	// Copy edges.
	for _, src := range g.edges.Sources() {
		for _, e := range g.edges.Get(src) {
			ng.edges.Add(src, e)
		}
	}
	for _, dest := range g.backEdges.Sources() {
		for _, be := range g.backEdges.Get(dest) {
			ng.backEdges.Add(dest, be)
		}
	}
	// Copy partition.
	ng.deletedPartition = t.NewUnionFind()
	for _, id := range g.deletedNodes.Items() {
		ng.deletedPartition.Add(id)
	}
	for _, id := range g.deletedNodes.Items() {
		if g.deletedPartition.Contains(id) {
			rep := g.deletedPartition.Find(id)
			if ng.deletedPartition.Contains(rep) {
				ng.deletedPartition.Union(id, rep)
			}
		}
	}
	// Copy pseudo-edge reasons.
	ng.reasonPseudoEdges = make(map[t.NodeID][]pseudoEdge)
	for rep, pes := range g.reasonPseudoEdges {
		cp := make([]pseudoEdge, len(pes))
		copy(cp, pes)
		ng.reasonPseudoEdges[rep] = cp
	}
	ng.pseudoEdgeReasons = make(map[pseudoEdge]map[t.NodeID]struct{})
	for pe, reasons := range g.pseudoEdgeReasons {
		m := make(map[t.NodeID]struct{})
		for r := range reasons {
			m[r] = struct{}{}
		}
		ng.pseudoEdgeReasons[pe] = m
	}
	for rep := range g.dirtyReps {
		ng.dirtyReps[rep] = struct{}{}
	}
	return ng
}

// Debug returns a human-readable dump of the graggle.
func (g *Graggle) Debug() string {
	s := fmt.Sprintf("Live nodes (%d):\n", g.nodes.Len())
	for _, id := range g.nodes.Items() {
		s += fmt.Sprintf("  %v: %q\n", id, string(g.contents[id]))
	}
	s += fmt.Sprintf("Deleted nodes (%d):\n", g.deletedNodes.Len())
	for _, id := range g.deletedNodes.Items() {
		s += fmt.Sprintf("  %v: %q\n", id, string(g.contents[id]))
	}
	s += "Edges:\n"
	for _, src := range g.edges.Sources() {
		for _, e := range g.edges.Get(src) {
			s += fmt.Sprintf("  %v --%s--> %v\n", src, e.Kind, e.Dest)
		}
	}
	return s
}