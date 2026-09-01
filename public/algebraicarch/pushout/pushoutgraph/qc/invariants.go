// Package qc provides structural invariant checking for pushoutgraph graphs.
package qc

import (
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/algo"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
)

// CheckInvariants verifies structural invariants of the pushoutgraph and returns
// all violations found. An empty slice means the pushoutgraph is consistent.
// Call this after ResolvePseudoEdges() for full coverage.
//
// CheckInvariants never mutates g: the idempotence check (invariant 10)
// runs its redundant resolution on a clone, so the checker is safe to call
// on live state from tests and assertions.
func CheckInvariants(g t.InspectableI) []error {
	var errs []error
	errs = append(errs, checkRootNodeInvariant(g)...)
	errs = append(errs, checkNodePartition(g)...)
	errs = append(errs, checkEdgeSymmetry(g)...)
	errs = append(errs, checkEdgeKindConsistency(g)...)
	errs = append(errs, checkDeletedPartitionCoverage(g)...)
	errs = append(errs, checkNoDirtyReps(g)...)
	errs = append(errs, checkPseudoEdgeMinimality(g)...)
	errs = append(errs, checkPseudoEdgeReachability(g)...)
	errs = append(errs, checkPseudoEdgeCompleteness(g)...)
	errs = append(errs, checkPseudoEdgeIdempotence(g)...)
	errs = append(errs, checkReasonTrackingConsistency(g)...)
	errs = append(errs, checkLiveSubgraphConnectivity(g)...)
	errs = append(errs, checkConflictConsistency(g)...)
	errs = append(errs, checkTombstoneDeleters(g)...)
	return errs
}

// Invariant 1: Root node is always live, never deleted.
func checkRootNodeInvariant(g t.InspectableI) []error {
	var errs []error
	if !g.IsLive(t.RootNodeID) {
		errs = append(errs, eh.Errorf("root node not in live set"))
	}
	if g.IsDeleted(t.RootNodeID) {
		errs = append(errs, eh.Errorf("root node in deleted set"))
	}
	return errs
}

// Invariant 2: Every node is in exactly one of live or deleted.
// Every node referenced by an edge must exist.
func checkNodePartition(g t.InspectableI) []error {
	var errs []error

	// No node in both sets.
	for id := range g.AllLiveNodes() {
		if g.IsDeleted(id) {
			errs = append(errs, eb.Build().Stringer("id", id).Errorf("node in both live and deleted sets"))
		}
	}

	// Every edge endpoint must be in one of the sets.
	for src := range g.ForwardEdgeSources() {
		if !g.IsLive(src) && !g.IsDeleted(src) {
			errs = append(errs, eb.Build().Stringer("src", src).Errorf("edge source not in any node set"))
		}
		for e := range g.ForwardEdges(src) {
			if !g.IsLive(e.Dest) && !g.IsDeleted(e.Dest) {
				errs = append(errs, eb.Build().Stringer("dest", e.Dest).Stringer("src", src).Errorf("edge dest not in any node set"))
			}
		}
	}
	return errs
}

// Invariant 3: Every forward edge has a matching back-edge and vice versa.
func checkEdgeSymmetry(g t.InspectableI) []error {
	var errs []error

	// Canonical edge key: always (src, dest, kind, introducedBy) in forward direction.
	type edgeKey struct {
		src, dest    t.NodeID
		kind         t.EdgeKindE
		introducedBy t.PatchHash
	}

	// Collect forward edges as canonical keys.
	fwdSet := make(map[edgeKey]struct{})
	for src := range g.ForwardEdgeSources() {
		for e := range g.ForwardEdges(src) {
			fwdSet[edgeKey{src, e.Dest, e.Kind, e.IntroducedBy}] = struct{}{}
		}
	}

	// Collect backward edges as canonical keys.
	// BackwardEdges(dest) yields Edge{Dest: src, Kind, IntroducedBy},
	// so the canonical forward direction is (be.Dest → dest).
	bwdSet := make(map[edgeKey]struct{})
	for dest := range g.BackwardEdgeSources() {
		for be := range g.BackwardEdges(dest) {
			bwdSet[edgeKey{be.Dest, dest, be.Kind, be.IntroducedBy}] = struct{}{}
		}
	}

	// Forward → backward: every forward edge should have a matching back-edge.
	for k := range fwdSet {
		if _, ok := bwdSet[k]; !ok {
			errs = append(errs, eb.Build().Stringer("src", k.src).Stringer("dest", k.dest).Stringer("kind", k.kind).Errorf("forward edge has no matching back-edge"))
		}
	}

	// Backward → forward: every back-edge should have a matching forward edge.
	for k := range bwdSet {
		if _, ok := fwdSet[k]; !ok {
			errs = append(errs, eb.Build().Stringer("src", k.src).Stringer("dest", k.dest).Stringer("kind", k.kind).Errorf("back-edge has no matching forward edge"))
		}
	}
	return errs
}

// Invariant 4: Edge kinds match the state of their endpoints.
func checkEdgeKindConsistency(g t.InspectableI) []error {
	var errs []error
	for src := range g.ForwardEdgeSources() {
		for e := range g.ForwardEdges(src) {
			srcLive := g.IsLive(src)
			destLive := g.IsLive(e.Dest)
			srcDel := g.IsDeleted(src)
			destDel := g.IsDeleted(e.Dest)

			switch e.Kind {
			case t.EdgeKindLive:
				if !srcLive || !destLive {
					errs = append(errs, eb.Build().Stringer("src", src).Stringer("dest", e.Dest).Bool("srcLive", srcLive).Bool("destLive", destLive).Errorf("live edge endpoints are not both live"))
				}
			case t.EdgeKindDeleted:
				if !srcDel && !destDel {
					errs = append(errs, eb.Build().Stringer("src", src).Stringer("dest", e.Dest).Errorf("deleted edge has no deleted endpoint"))
				}
			case t.EdgeKindPseudo:
				if !srcLive || !destLive {
					errs = append(errs, eb.Build().Stringer("src", src).Stringer("dest", e.Dest).Bool("srcLive", srcLive).Bool("destLive", destLive).Errorf("pseudo-edge endpoints are not both live"))
				}
			}
		}
	}
	return errs
}

// Invariant 5: DeletedPartition covers exactly the deleted nodes.
func checkDeletedPartitionCoverage(g t.InspectableI) []error {
	var errs []error
	for id := range g.AllDeletedNodes() {
		if !g.DeletedPartitionContains(id) {
			errs = append(errs, eb.Build().Stringer("id", id).Errorf("deleted node not in DeletedPartition"))
		}
	}
	for id := range g.AllLiveNodes() {
		if g.DeletedPartitionContains(id) {
			errs = append(errs, eb.Build().Stringer("id", id).Errorf("live node found in DeletedPartition"))
		}
	}
	return errs
}

// Invariant 6: No dirty reps remain after ResolvePseudoEdges.
func checkNoDirtyReps(g t.InspectableI) []error {
	if g.DirtyRepCount() > 0 {
		return []error{eb.Build().Int("dirtyReps", g.DirtyRepCount()).Errorf("dirty reps remain unresolved")}
	}
	return nil
}

// Invariant 7: No pseudo-edge duplicates a live edge.
func checkPseudoEdgeMinimality(g t.InspectableI) []error {
	var errs []error
	for src := range g.ForwardEdgeSources() {
		for e := range g.ForwardEdges(src) {
			if e.Kind == t.EdgeKindPseudo && g.HasLiveEdgeTo(src, e.Dest) {
				errs = append(errs, eb.Build().Stringer("src", src).Stringer("dest", e.Dest).Errorf("pseudo-edge duplicates a live edge"))
			}
		}
	}
	return errs
}

// Invariant 8: Every pseudo-edge is justified by a path through deleted nodes.
func checkPseudoEdgeReachability(g t.InspectableI) []error {
	var errs []error
	for src := range g.ForwardEdgeSources() {
		for e := range g.ForwardEdges(src) {
			if e.Kind != t.EdgeKindPseudo {
				continue
			}
			if !canReachThroughDeleted(g, src, e.Dest) {
				errs = append(errs, eb.Build().Stringer("src", src).Stringer("dest", e.Dest).Errorf("pseudo-edge not justified by path through deleted nodes"))
			}
		}
	}
	return errs
}

// canReachThroughDeleted checks if dest is reachable from src by going through
// at least one deleted node using forward edges.
func canReachThroughDeleted(g t.InspectableI, src, dest t.NodeID) bool {
	visited := make(map[t.NodeID]struct{})
	var stack []t.NodeID
	for e := range g.ForwardEdges(src) {
		if g.IsDeleted(e.Dest) {
			stack = append(stack, e.Dest)
		}
	}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := visited[n]; ok {
			continue
		}
		visited[n] = struct{}{}
		for e := range g.ForwardEdges(n) {
			if e.Dest == dest {
				return true
			}
			if g.IsDeleted(e.Dest) {
				stack = append(stack, e.Dest)
			}
		}
	}
	return false
}

// Invariant 9: Every pair of live boundary nodes connected through a deleted
// component has a pseudo-edge (unless a live edge already connects them).
func checkPseudoEdgeCompleteness(g t.InspectableI) []error {
	var errs []error

	for rep := range g.DeletedPartitionRepresentatives() {
		members := slices.Collect(g.DeletedPartitionMembers(rep))
		if len(members) == 0 {
			continue
		}
		sources, dests := g.ExportFindBoundaryNodes(members)
		for _, src := range sources {
			reachable := g.ExportFindReachableBoundary(src, members, dests)
			for _, dest := range reachable {
				if src == dest {
					continue
				}
				if g.HasLiveEdgeTo(src, dest) {
					continue
				}
				hasPseudo := false
				for e := range g.ForwardEdges(src) {
					if e.Dest == dest && e.Kind == t.EdgeKindPseudo {
						hasPseudo = true
						break
					}
				}
				if !hasPseudo {
					errs = append(errs, eb.Build().Stringer("src", src).Stringer("dest", dest).Stringer("rep", rep).Errorf("missing pseudo-edge for a pair connected through a deleted component"))
				}
			}
		}
	}
	return errs
}

// Invariant 10: Calling ResolvePseudoEdges a second time is a no-op.
//
// The redundant resolution runs on a clone so that CheckInvariants never
// mutates the pushoutgraph under inspection. PushoutGraphs that cannot produce a
// clone skip this check — running it in place would turn the checker into
// a mutator and could mask dirty-rep state for whoever inspects next.
func checkPseudoEdgeIdempotence(g t.InspectableI) []error {
	cloner, ok := g.(interface{ CloneStore() t.GraphStoreI })
	if !ok {
		return nil
	}
	clone, ok := cloner.CloneStore().(t.InspectableI)
	if !ok {
		return nil
	}

	type pe struct{ src, dest t.NodeID }
	before := make(map[pe]struct{})
	for src := range clone.ForwardEdgeSources() {
		for e := range clone.ForwardEdges(src) {
			if e.Kind == t.EdgeKindPseudo {
				before[pe{src, e.Dest}] = struct{}{}
			}
		}
	}

	clone.ResolvePseudoEdges()

	after := make(map[pe]struct{})
	for src := range clone.ForwardEdgeSources() {
		for e := range clone.ForwardEdges(src) {
			if e.Kind == t.EdgeKindPseudo {
				after[pe{src, e.Dest}] = struct{}{}
			}
		}
	}

	var errs []error
	for p := range before {
		if _, ok := after[p]; !ok {
			errs = append(errs, eb.Build().Stringer("src", p.src).Stringer("dest", p.dest).Errorf("pseudo-edge removed by a redundant ResolvePseudoEdges"))
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			errs = append(errs, eb.Build().Stringer("src", p.src).Stringer("dest", p.dest).Errorf("pseudo-edge added by a redundant ResolvePseudoEdges"))
		}
	}
	return errs
}

// Invariant 11: ReasonPseudoEdges and PseudoEdgeReasons are consistent inverses.
func checkReasonTrackingConsistency(g t.InspectableI) []error {
	var errs []error

	// Forward check: every pseudo-edge listed under a rep should have that
	// rep recorded in the inverse PseudoEdgeReasons map.
	for rep := range g.DeletedPartitionRepresentatives() {
		for pe := range g.ReasonPseudoEdgesForRep(rep) {
			if g.PseudoEdgeReasonCount(pe[0], pe[1]) == 0 {
				errs = append(errs, eb.Build().Stringer("rep", rep).Stringer("src", pe[0]).Stringer("dest", pe[1]).Errorf("ReasonPseudoEdges references a pseudo-edge that PseudoEdgeReasons has no entry for"))
			}
		}
	}

	// Every pseudo-edge in the graph should be tracked.
	for src := range g.ForwardEdgeSources() {
		for e := range g.ForwardEdges(src) {
			if e.Kind == t.EdgeKindPseudo {
				pe := [2]t.NodeID{src, e.Dest}
				if g.PseudoEdgeReasonCount(pe[0], pe[1]) == 0 {
					errs = append(errs, eb.Build().Stringer("src", src).Stringer("dest", e.Dest).Errorf("pseudo-edge in the graph is not tracked in PseudoEdgeReasons"))
				}
			}
		}
	}

	// Every tracked pseudo-edge should exist in the graph.
	for pe := range g.AllTrackedPseudoEdges() {
		found := false
		for e := range g.ForwardEdges(pe[0]) {
			if e.Dest == pe[1] && e.Kind == t.EdgeKindPseudo {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, eb.Build().Stringer("src", pe[0]).Stringer("dest", pe[1]).Errorf("PseudoEdgeReasons entry has no pseudo-edge in graph"))
		}
	}

	return errs
}

// Invariant 12: Every live node is reachable from root through live+pseudo edges.
func checkLiveSubgraphConnectivity(g t.InspectableI) []error {
	reachable := make(map[t.NodeID]struct{})
	stack := []t.NodeID{t.RootNodeID}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := reachable[n]; ok {
			continue
		}
		reachable[n] = struct{}{}
		for e := range g.ForwardEdges(n) {
			if (e.Kind == t.EdgeKindLive || e.Kind == t.EdgeKindPseudo) && g.IsLive(e.Dest) {
				stack = append(stack, e.Dest)
			}
		}
	}

	var errs []error
	for id := range g.AllLiveNodes() {
		if _, ok := reachable[id]; !ok {
			errs = append(errs, eb.Build().Stringer("id", id).Errorf("live node unreachable from root"))
		}
	}
	return errs
}

// Invariant 13: LinearOrder and DetectConflicts agree on linearity.
//
// LinearOrder()==nil must coincide with DetectConflicts reporting at least
// one linearity-breaking conflict — kind "order", "cycle", or "orphan".
// Zombie conflicts are excluded: a zombie does not break the linear order.
// The two sides are computed by independent code paths (Kahn + uniqueness
// check vs. Tarjan + fork/anchor scans), so drift between them is caught
// here. The previous formulation compared HasConflicts against
// LinearOrder()==nil, but HasConflicts is *defined* as LinearOrder()==nil,
// so it could never fire.
func checkConflictConsistency(g t.InspectableI) []error {
	isLinear := algo.LinearOrder(g) != nil
	breaking := 0
	for _, c := range algo.DetectConflicts(g) {
		switch c.Kind {
		case "order", "cycle", "orphan":
			breaking++
		}
	}

	var errs []error
	if isLinear && breaking > 0 {
		errs = append(errs, eb.Build().Int("breaking", breaking).Errorf("LinearOrder succeeded but DetectConflicts reports linearity-breaking conflicts"))
	}
	if !isLinear && breaking == 0 {
		errs = append(errs, eh.Errorf("LinearOrder()==nil but DetectConflicts reports no order/cycle/orphan conflict — conflict detector is incomplete for this graph"))
	}
	return errs
}

// Invariant 14: tombstones track their deleters. Every deleted node must
// carry at least one recorded deleting patch (UndeleteNode resurrects on
// the last one), and no live node may carry any.
func checkTombstoneDeleters(g t.InspectableI) []error {
	var errs []error
	for id := range g.AllDeletedNodes() {
		if g.NodeDeleterCount(id) == 0 {
			errs = append(errs, eb.Build().Stringer("id", id).Errorf("deleted node has no recorded deleter"))
		}
	}
	for id := range g.AllLiveNodes() {
		if g.NodeDeleterCount(id) != 0 {
			errs = append(errs, eb.Build().Stringer("nodeID", id).Int("deleters", g.NodeDeleterCount(id)).Errorf("live node has recorded deleters"))
		}
	}
	return errs
}
