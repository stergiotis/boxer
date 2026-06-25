// Package store provides the concrete Graggle data structure implementing
// all graph interfaces defined in the types package.
package store

import (
	"bytes"
	"fmt"
	"iter"
	"slices"
	"strings"
	"time"

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

	// deleters[id] is the set of patches that tombstoned id. Two patches
	// deleting the same node is the normal convergent-edit case; the
	// tombstone must survive until the LAST deleting patch is unapplied,
	// so UndeleteNode resurrects only when this set empties. Populated by
	// DeleteNode, drained by UndeleteNode, cleared by RemoveNode.
	deleters map[t.NodeID]map[t.PatchHash]struct{}

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

	// Tombstone retention bookkeeping (storage-limitation under GDPR Art
	// 5(1)(e) / FADP Art 6(4); see SweepTombstones).
	//
	// tombstoneAt[id] holds the wall-clock time the node was tombstoned as
	// observed by THIS replica. Populated by DeleteNode via inst.clock(),
	// cleared by UndeleteNode and RemoveNode, and carried in the GRG1
	// snapshot. It is a working copy of the repo's durable retention
	// ledger: full replay re-stamps it to replay time, but the repo
	// re-seeds it from the ledger at Open (see SeedTombstoneStamps), so the
	// pending retention horizon survives crash/restart on the same store
	// (ADR-0079). A fresh clone carries no ledger and starts the horizon at
	// clone time — fleet-wide erasure across clones is ADR-0025's layer.
	tombstoneAt map[t.NodeID]time.Time

	// contentPurged[id] is present iff content was destroyed by
	// SweepTombstones (or, in future, a Forget operation). Distinct from
	// "node never had content recorded": the entry survives even after
	// contents[id] has been dropped, so Patch.Unapply can refuse to
	// resurrect a node whose content can no longer be reconstructed and
	// audits can distinguish purged from missing.
	contentPurged map[t.NodeID]struct{}

	// clock is the time source used by DeleteNode to stamp tombstoneAt.
	// Defaults to time.Now in New(); tests inject a fake clock via
	// SetClock to make sweep behaviour deterministic.
	clock func() time.Time
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
		deleters:          make(map[t.NodeID]map[t.PatchHash]struct{}),
		edges:             t.NewMultiMap(),
		backEdges:         t.NewMultiMap(),
		deletedPartition:  t.NewUnionFind(),
		reasonPseudoEdges: make(map[t.NodeID][]pseudoEdge),
		pseudoEdgeReasons: make(map[pseudoEdge]map[t.NodeID]struct{}),
		dirtyReps:         make(map[t.NodeID]struct{}),
		tombstoneAt:       make(map[t.NodeID]time.Time),
		contentPurged:     make(map[t.NodeID]struct{}),
		clock:             time.Now,
	}
	inst.nodes.Add(t.RootNodeID)
	return inst
}

// SetClock replaces the time source used by DeleteNode to stamp
// tombstoneAt. Intended for deterministic testing of SweepTombstones;
// production callers should leave the default (time.Now). The clock is
// not used outside DeleteNode — SweepTombstones takes its now explicitly.
func (inst *Graggle) SetClock(clock func() time.Time) {
	inst.clock = clock
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
		return eh.Errorf("node %v: %w", id, ErrNodeExists)
	}
	inst.nodes.Add(id)
	inst.contents[id] = content

	for _, up := range upContext {
		if !inst.HasNode(up) {
			return eh.Errorf("up-context node %v: %w", up, ErrNodeMissing)
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
			return eh.Errorf("down-context node %v: %w", down, ErrNodeMissing)
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

// DeleteNode tombstones a node on behalf of the given patch.
//
// Deleting an already-deleted node is not an error: two patches can
// legitimately delete the same node — e.g. two actors editing the same
// line, where LineDiff produces a delete+insert pair on each side, and
// applying both patches must succeed in either order. The deleter is
// recorded either way; the tombstone survives until UndeleteNode has
// removed every recorded deleter, so unapplying one of two convergent
// edits does not resurrect the node out from under the other. (An
// earlier revision made DeleteNode a bare no-op on tombstones — that
// fixed apply-commutativity but left the inverse direction unsound.)
func (inst *Graggle) DeleteNode(id t.NodeID, deleter t.PatchHash) error {
	if inst.deletedNodes.Contains(id) {
		inst.addDeleter(id, deleter)
		return nil
	}
	if !inst.nodes.Contains(id) {
		return eh.Errorf("node %v: %w", id, ErrNodeMissing)
	}
	if id == t.RootNodeID {
		return eh.Errorf("%w", ErrRootImmutable)
	}

	// Move from live to deleted.
	inst.nodes.Remove(id)
	inst.deletedNodes.Add(id)
	inst.addDeleter(id, deleter)

	// Record when the tombstone entered this graggle session, for
	// SweepTombstones' retention-horizon decisions.
	inst.tombstoneAt[id] = inst.clock()

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

// UndeleteNode removes one deleter from a tombstoned node and resurrects
// the node when (and only when) that was the last recorded deleter.
//
// The undeleter must be one of the patches that deleted the node —
// anything else is a sequencing bug at the caller. While other deleters
// remain, the call only shrinks the deleter set: the node stays
// tombstoned because a still-applied patch wants it dead.
//
// Undeletion can split a deleted component into several. Union-find is
// merge-only, so this method snapshots the component, drops every member
// from the partition (along with stale pseudo-edge bookkeeping for the old
// rep), then rebuilds new sub-components via recomputeDeletedComponents.
// Each sub-component is marked dirty under its own rep so resolve-time
// pseudo-edge recomputation handles each independently.
func (inst *Graggle) UndeleteNode(id t.NodeID, undeleter t.PatchHash) error {
	if !inst.deletedNodes.Contains(id) {
		return eh.Errorf("node %v: %w", id, ErrNotDeleted)
	}
	set := inst.deleters[id]
	if _, ok := set[undeleter]; !ok {
		return eh.Errorf("node %v, undeleter %s (deleters: %s): %w", id, undeleter, deleterList(set), ErrWrongUndeleter)
	}
	if len(set) > 1 {
		// Other still-applied patches keep the tombstone alive.
		delete(set, undeleter)
		return nil
	}
	// Last deleter: actual resurrection. The purge check applies only
	// here — removing a non-final deleter never needs the content back.
	if _, purged := inst.contentPurged[id]; purged {
		return eh.Errorf("node %v has been swept; patch is permanent past retention: %w", id, ErrContentPurged)
	}
	delete(inst.deleters, id)

	// Snapshot the original component before any mutation. Members may
	// include id itself.
	rep := inst.deletedPartition.Find(id)
	peers := inst.deletedPartition.Members(rep)

	// Drop pseudo-edge bookkeeping keyed under ANY member of the
	// component, not just its current representative: representatives
	// shift as components merge, and a reason recorded under an earlier
	// rep would survive a rep-keyed drop — leaving a pseudo-edge in the
	// graph that nothing justifies once this undeletion makes the path
	// live again. (Found by the multi-repo state-machine harness: a
	// pull/unrecord sequence left an unjustified root→node pseudo-edge.)
	for _, m := range peers {
		inst.dropReasonsForRep(m)
	}

	// Move id from deleted to live and retag its edges accordingly.
	inst.deletedNodes.Remove(id)
	inst.nodes.Add(id)
	delete(inst.tombstoneAt, id)
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

// addDeleter records that deleter tombstoned id.
func (inst *Graggle) addDeleter(id t.NodeID, deleter t.PatchHash) {
	set := inst.deleters[id]
	if set == nil {
		set = make(map[t.PatchHash]struct{}, 1)
		inst.deleters[id] = set
	}
	set[deleter] = struct{}{}
}

// NodeDeleterCount returns how many patches currently hold id tombstoned.
// Zero for live (or unknown) nodes.
func (inst *Graggle) NodeDeleterCount(id t.NodeID) int {
	return len(inst.deleters[id])
}

// deleterList renders a deleter set for error messages, in deterministic
// order.
func deleterList(set map[t.PatchHash]struct{}) string {
	hs := make([]t.PatchHash, 0, len(set))
	for h := range set {
		hs = append(hs, h)
	}
	slices.SortFunc(hs, func(a, b t.PatchHash) int { return bytes.Compare(a[:], b[:]) })
	parts := make([]string, len(hs))
	for i, h := range hs {
		parts[i] = h.String()
	}
	return strings.Join(parts, ", ")
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
		return eh.Errorf("source node %v: %w", src, ErrNodeMissing)
	}
	if !inst.HasNode(dest) {
		return eh.Errorf("dest node %v: %w", dest, ErrNodeMissing)
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
//
// Removing a live or deleted-kind edge can invalidate derived pseudo-edge
// state: a removed live edge may have been shadowing a pseudo-edge that a
// neighbouring deleted component still justifies, and a removed
// deleted-kind edge can split a deleted component. Every deleted
// component adjacent to either endpoint is therefore marked dirty for
// re-resolution. Pseudo-kind removals are resolution-internal bookkeeping
// and must not re-dirty the components being resolved.
func (inst *Graggle) RemoveEdge(src, dest t.NodeID, kind t.EdgeKindE, patch t.PatchHash) {
	e := t.Edge{Dest: dest, Kind: kind, IntroducedBy: patch}
	inst.edges.Remove(src, e)
	backE := t.Edge{Dest: src, Kind: kind, IntroducedBy: patch}
	inst.backEdges.Remove(dest, backE)
	if kind != t.EdgeKindPseudo {
		inst.markDirtyAroundEdgeRemoval(src, dest)
	}
}

// markDirtyAroundEdgeRemoval flags the deleted components of both
// endpoints and of every deleted neighbour of either endpoint.
func (inst *Graggle) markDirtyAroundEdgeRemoval(src, dest t.NodeID) {
	mark := func(id t.NodeID) {
		if inst.deletedPartition.Contains(id) {
			inst.dirtyReps[inst.deletedPartition.Find(id)] = struct{}{}
		}
	}
	for _, endpoint := range [2]t.NodeID{src, dest} {
		mark(endpoint)
		for _, e := range inst.edges.Get(endpoint) {
			if inst.IsDeleted(e.Dest) {
				mark(e.Dest)
			}
		}
		for _, be := range inst.backEdges.Get(endpoint) {
			if inst.IsDeleted(be.Dest) {
				mark(be.Dest)
			}
		}
	}
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
//
// The dirty set is swapped out before iterating and the loop repeats until
// no new dirt appears, so resolution work that flags further components
// (rather than mutating the map under iteration) is handled in a
// follow-up round instead of being silently discarded.
func (inst *Graggle) ResolvePseudoEdges() {
	for len(inst.dirtyReps) > 0 {
		dirty := inst.dirtyReps
		inst.dirtyReps = make(map[t.NodeID]struct{})
		for dirtyRep := range dirty {
			inst.resolveComponent(dirtyRep)
		}
	}
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

	// Drop reason entries keyed under ANY member, not just the current
	// rep: representatives shift as components merge, and entries keyed
	// under a node that has since stopped being a representative would
	// otherwise never be cleaned up.
	for _, m := range members {
		inst.dropReasonsForRep(m)
	}

	// Recompute connected components via DFS on the full graph (deleted subgraph).
	// This handles the case where undeletion split a component.
	components := inst.recomputeDeletedComponents(members)

	// If the component split (e.g. RemoveEdge dropped a deleted-kind edge
	// during Unapply), the merge-only union-find still holds one merged
	// set; rebuild the partition for these members so each sub-component
	// gets its own representative. Without this, every sub-component
	// below resolves under the same Find() result and the last one
	// overwrites the others' reasonPseudoEdges entry.
	if len(components) > 1 {
		for _, m := range members {
			inst.deletedPartition.Remove(m)
		}
		for _, m := range members {
			inst.deletedPartition.Add(m)
		}
	}

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
// Used by Unapply to clean up nodes added by a patch. The caller must
// ensure the node is not tombstoned (Patch.Unapply pre-flights this);
// the deleter entry is cleared defensively so misuse surfaces through
// the tombstone invariants rather than as stale bookkeeping.
func (inst *Graggle) RemoveNode(id t.NodeID) {
	inst.nodes.Remove(id)
	delete(inst.contents, id)
	delete(inst.tombstoneAt, id)
	delete(inst.contentPurged, id)
	delete(inst.deleters, id)
}

// NodeContent returns the content of a node, or nil if not found.
//
// Note that nil is ambiguous: it can mean "node never had content
// recorded", "node has been swept and content destroyed", or "node has
// empty content". Use NodeContentStatus to disambiguate.
func (inst *Graggle) NodeContent(id t.NodeID) []byte {
	return inst.contents[id]
}

// NodeContentStatus reports whether content is recorded for id and, if
// not, distinguishes "never recorded" from "purged by sweep". The
// distinction matters for data-protection audits: a Purged node was
// previously recorded and then deliberately destroyed, while a Missing
// node simply has no entry.
func (inst *Graggle) NodeContentStatus(id t.NodeID) (status t.NodeContentStatusE) {
	if _, ok := inst.contents[id]; ok {
		status = t.NodeContentStatusPresent
		return
	}
	if _, ok := inst.contentPurged[id]; ok {
		status = t.NodeContentStatusPurged
		return
	}
	status = t.NodeContentStatusMissing
	return
}

// SweepTombstones drops the content bytes of tombstoned nodes whose
// tombstone time is strictly older than (now - horizon). The node
// remains in deletedNodes — the graph topology is preserved so the live
// subgraph stays well-defined — but contentPurged[id] is set and
// contents[id] is freed.
//
// Implements the storage-limitation duty under GDPR Art 5(1)(e) and FADP
// Art 6(4): personal data that is no longer necessary must be destroyed
// or anonymised. The legal framing (the choice between salt-destruction,
// commitment-destruction, encryption-shredding, etc.) is the consuming
// repo's concern; this method is the mechanism that drops content past
// a retention horizon under any of those architectures.
//
// Trade-off: a tombstoned node with purged content can no longer be
// resurrected. Patch.Unapply of the patch that introduced the
// DeleteNode for id will fail with a clear error after the sweep. This
// is the intended trade-off — past the retention horizon, the patch is
// effectively permanent. Callers wanting reversibility within the
// horizon must keep horizon long enough to cover their unrecord
// workflows.
//
// Caveats:
//   - The pending retention horizon is durable across crash/restart on
//     the same store: tombstoneAt is mirrored to the repo's retention
//     ledger and re-seeded at Open, so full replay no longer resets it
//     (ADR-0079). A fresh clone carries no ledger and starts the horizon
//     at clone time; fleet-wide erasure across clones is ADR-0025's layer.
//   - Not safe for concurrent use with mutating graggle operations; the
//     caller's mutex (e.g. PushoutRepo.Mu) applies.
//
// Returns the count of nodes whose content was newly purged, and the
// list of their NodeIDs in deterministic order (sorted via
// t.CompareNodeID) so callers can write structured audit log entries.
func (inst *Graggle) SweepTombstones(now time.Time, horizon time.Duration) (purgedCount int, purgedIDs []t.NodeID) {
	cutoff := now.Add(-horizon)
	for id, when := range inst.tombstoneAt {
		if !when.Before(cutoff) {
			continue
		}
		if _, already := inst.contentPurged[id]; already {
			continue
		}
		if _, hasContent := inst.contents[id]; !hasContent {
			// Marker without content: still flip to Purged so the audit
			// signal survives, but nothing to delete.
			inst.contentPurged[id] = struct{}{}
			purgedIDs = append(purgedIDs, id)
			purgedCount++
			continue
		}
		delete(inst.contents, id)
		inst.contentPurged[id] = struct{}{}
		purgedIDs = append(purgedIDs, id)
		purgedCount++
	}
	if len(purgedIDs) > 1 {
		slices.SortFunc(purgedIDs, t.CompareNodeID)
	}
	return
}

// SeedTombstoneStamps overlays durable retention times onto the in-memory
// tombstoneAt working copy: for every currently-tombstoned node present in
// ledger, its stamp is replaced with the durable value; tombstones absent
// from the ledger keep whatever stamp decode or replay produced (that
// stamp is the fallback). This is how the repo restores replay-stable
// retention horizons at Open without re-stamping — see ADR-0079 and the
// tombstoneAt field doc. Entries in ledger for non-tombstoned nodes are
// ignored.
func (inst *Graggle) SeedTombstoneStamps(ledger map[t.NodeID]time.Time) {
	for _, id := range inst.deletedNodes.Items() {
		if when, ok := ledger[id]; ok {
			inst.tombstoneAt[id] = when
		}
	}
}

// TombstoneStamps returns a copy of the per-node tombstone times. Only
// tombstoned nodes have entries; the repo persists this as the durable
// retention ledger.
func (inst *Graggle) TombstoneStamps() map[t.NodeID]time.Time {
	out := make(map[t.NodeID]time.Time, len(inst.tombstoneAt))
	for id, when := range inst.tombstoneAt {
		out[id] = when
	}
	return out
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
	// Copy deleter sets.
	for id, set := range inst.deleters {
		cp := make(map[t.PatchHash]struct{}, len(set))
		for h := range set {
			cp[h] = struct{}{}
		}
		ng.deleters[id] = cp
	}
	// Copy tombstone retention bookkeeping.
	for id, when := range inst.tombstoneAt {
		ng.tombstoneAt[id] = when
	}
	for id := range inst.contentPurged {
		ng.contentPurged[id] = struct{}{}
	}
	// Propagate the clock so a clone of a graggle with a test clock
	// keeps the test clock; production clones inherit time.Now.
	ng.clock = inst.clock
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
