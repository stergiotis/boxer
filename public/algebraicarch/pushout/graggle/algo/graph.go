//go:build llm_generated_opus47

// Package algo provides pure graph algorithms operating on the GraphReaderI
// interface. It has no knowledge of the concrete Graggle implementation.
package algo

import (
	"slices"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// Tarjan computes strongly connected components of the live subgraph.
// Returns SCCs in reverse topological order (sinks first).
// Multi-vertex SCCs indicate cyclic conflicts.
//
// The implementation is iterative — a recursive Tarjan walks one stack
// frame per graph vertex, which blows the goroutine stack on long files.
// We instead maintain an explicit work stack of frames; lowlink propagation
// happens when a frame is popped (the equivalent of a recursive return).
//
// Precondition: ResolvePseudoEdges must have been called.
func Tarjan(g t.GraphReaderI) [][]t.NodeID {
	index := 0
	var sccStack []t.NodeID
	onStack := make(map[t.NodeID]bool)
	indices := make(map[t.NodeID]int)
	lowlinks := make(map[t.NodeID]int)
	var sccs [][]t.NodeID

	// frame holds the state we'd otherwise carry across a recursive call.
	type frame struct {
		v        t.NodeID
		children []t.NodeID
		i        int
	}
	var work []frame

	visit := func(v t.NodeID) {
		indices[v] = index
		lowlinks[v] = index
		index++
		sccStack = append(sccStack, v)
		onStack[v] = true
		work = append(work, frame{v: v, children: slices.Collect(g.LiveChildren(v))})
	}

	popSCC := func(v t.NodeID) {
		var scc []t.NodeID
		for {
			w := sccStack[len(sccStack)-1]
			sccStack = sccStack[:len(sccStack)-1]
			onStack[w] = false
			scc = append(scc, w)
			if w == v {
				break
			}
		}
		sccs = append(sccs, scc)
	}

	for root := range g.AllLiveNodes() {
		if _, ok := indices[root]; ok {
			continue
		}
		visit(root)
		for len(work) > 0 {
			top := &work[len(work)-1]
			if top.i < len(top.children) {
				w := top.children[top.i]
				top.i++
				if !g.IsLive(w) {
					continue
				}
				if _, ok := indices[w]; !ok {
					visit(w)
					continue
				}
				if onStack[w] && indices[w] < lowlinks[top.v] {
					lowlinks[top.v] = indices[w]
				}
				continue
			}
			// All children processed — equivalent of a recursive return.
			v := top.v
			if lowlinks[v] == indices[v] {
				popSCC(v)
			}
			work = work[:len(work)-1]
			if len(work) > 0 {
				parent := &work[len(work)-1]
				if lowlinks[v] < lowlinks[parent.v] {
					lowlinks[parent.v] = lowlinks[v]
				}
			}
		}
	}

	return sccs
}

// TopoSort returns a topological ordering of the live subgraph.
// Returns nil if the graph contains cycles.
//
// The result is deterministic: the initial zero-in-degree queue is
// seeded in CompareNodeID order — map iteration order would otherwise
// leak into the output whenever the topological order is not unique.
//
// Precondition: ResolvePseudoEdges must have been called.
func TopoSort(g t.GraphReaderI) []t.NodeID {
	// Kahn's algorithm.
	inDegree := make(map[t.NodeID]int)
	for v := range g.AllLiveNodes() {
		if _, ok := inDegree[v]; !ok {
			inDegree[v] = 0
		}
		for w := range g.LiveChildren(v) {
			if g.IsLive(w) {
				inDegree[w]++
			}
		}
	}

	var queue []t.NodeID
	for v, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, v)
		}
	}
	slices.SortFunc(queue, t.CompareNodeID)

	var order []t.NodeID
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		order = append(order, v)
		for w := range g.LiveChildren(v) {
			if !g.IsLive(w) {
				continue
			}
			inDegree[w]--
			if inDegree[w] == 0 {
				queue = append(queue, w)
			}
		}
	}

	if len(order) != len(inDegree) {
		return nil // cycle
	}
	return order
}

// LinearOrder returns a unique linear ordering of the live nodes, or nil
// if the graph is not linearly ordered (i.e., there are conflicts).
// A linear order exists iff the topological sort is unique, meaning every
// consecutive pair has a direct edge between them.
//
// Precondition: ResolvePseudoEdges must have been called.
func LinearOrder(g t.GraphReaderI) []t.NodeID {
	order := TopoSort(g)
	if order == nil {
		return nil // cycles
	}

	// Check uniqueness: each consecutive pair must have a direct edge.
	for i := 0; i < len(order)-1; i++ {
		hasEdge := false
		for w := range g.LiveChildren(order[i]) {
			if w == order[i+1] {
				hasEdge = true
				break
			}
		}
		if !hasEdge {
			return nil // not unique — conflict
		}
	}
	return order
}

// HasConflicts returns true if the live subgraph is not linearly ordered.
//
// Precondition: ResolvePseudoEdges must have been called.
func HasConflicts(g t.GraphReaderI) bool {
	return LinearOrder(g) == nil
}

// ConflictInfo describes a detected conflict.
//
// For "order" conflicts, Nodes[0] is the common parent and Nodes[1:] the
// incomparable children. For "zombie" conflicts, Nodes[0] is the live
// zombie node and Nodes[1:] are its deleted context nodes (parents and/or
// children). "cycle" lists the SCC members and "orphan" the single
// unreachable node.
//
// "order", "cycle", and "orphan" break the linear order; "zombie" does
// not (the node stays positioned via pseudo-edges, it merely lost its
// anchoring context). Invariant 13 in graggle/qc relies on this split:
// LinearOrder()==nil must coincide with at least one linearity-breaking
// conflict being reported.
type ConflictInfo struct {
	Kind  string     // "order" (fork), "cycle", "zombie", "orphan"
	Nodes []t.NodeID // involved nodes
}

// DetectConflicts returns a list of all conflicts in the live subgraph.
//
// Precondition: ResolvePseudoEdges must have been called.
func DetectConflicts(g t.GraphReaderI) []ConflictInfo {
	var conflicts []ConflictInfo

	// Cyclic conflicts: multi-vertex SCCs, plus single-vertex SCCs with
	// a self-edge — Tarjan reports those as singletons, but a self-loop
	// is a cycle and TopoSort refuses to order it (found by the snapshot
	// fuzzer via qc invariant 13: LinearOrder()==nil with an empty
	// conflict list).
	sccs := Tarjan(g)
	for _, scc := range sccs {
		if len(scc) > 1 || hasSelfEdge(g, scc[0]) {
			conflicts = append(conflicts, ConflictInfo{
				Kind:  "cycle",
				Nodes: scc,
			})
		}
	}

	// Order conflicts: nodes with multiple live children that have
	// no ordering between them (forks in the DAG).
	for v := range g.AllLiveNodes() {
		children := slices.Collect(g.LiveChildren(v))
		if len(children) <= 1 {
			continue
		}
		for i := range len(children) {
			for j := i + 1; j < len(children); j++ {
				if !hasPath(g, children[i], children[j]) && !hasPath(g, children[j], children[i]) {
					conflicts = append(conflicts, ConflictInfo{
						Kind:  "order",
						Nodes: []t.NodeID{v, children[i], children[j]},
					})
				}
			}
		}
	}

	// Orphan conflicts: live nodes unreachable from the root through
	// live+pseudo edges. They have no anchored position, so no linear
	// order exists — yet they fork nothing and cycle nowhere, which made
	// the detector incomplete with respect to LinearOrder before this
	// kind existed (HasConflicts()==true with an empty conflict list).
	reachable := make(map[t.NodeID]struct{})
	stack := []t.NodeID{t.RootNodeID}
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := reachable[v]; ok {
			continue
		}
		reachable[v] = struct{}{}
		for w := range g.LiveChildren(v) {
			if g.IsLive(w) {
				stack = append(stack, w)
			}
		}
	}
	for v := range g.AllLiveNodes() {
		if _, ok := reachable[v]; !ok {
			conflicts = append(conflicts, ConflictInfo{
				Kind:  "orphan",
				Nodes: []t.NodeID{v},
			})
		}
	}

	// Zombie conflicts: live nodes whose anchoring context (up or down) was
	// deleted. A zombie's incident edges to the deleted neighbours carry
	// EdgeKindDeleted; pseudo-edges keep the node visible in render but
	// the user should decide whether to re-anchor or remove it. Pseudo-
	// edges always point to live nodes so they cannot mask a true zombie
	// here.
	for v := range g.AllLiveNodes() {
		if v == t.RootNodeID {
			continue
		}
		var deletedCtx []t.NodeID
		for be := range g.BackwardEdges(v) {
			if be.Kind == t.EdgeKindDeleted {
				deletedCtx = append(deletedCtx, be.Dest)
			}
		}
		for e := range g.ForwardEdges(v) {
			if e.Kind == t.EdgeKindDeleted {
				deletedCtx = append(deletedCtx, e.Dest)
			}
		}
		if len(deletedCtx) > 0 {
			conflicts = append(conflicts, ConflictInfo{
				Kind:  "zombie",
				Nodes: append([]t.NodeID{v}, deletedCtx...),
			})
		}
	}

	return conflicts
}

// hasSelfEdge reports whether v has a live/pseudo edge to itself.
func hasSelfEdge(g t.GraphReaderI, v t.NodeID) bool {
	for w := range g.LiveChildren(v) {
		if w == v {
			return true
		}
	}
	return false
}

// hasPath checks if there is a directed path from src to dest in the live subgraph.
func hasPath(g t.GraphReaderI, src, dest t.NodeID) bool {
	visited := make(map[t.NodeID]struct{})
	stack := []t.NodeID{src}
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if v == dest {
			return true
		}
		if _, ok := visited[v]; ok {
			continue
		}
		visited[v] = struct{}{}
		for w := range g.LiveChildren(v) {
			if g.IsLive(w) {
				stack = append(stack, w)
			}
		}
	}
	return false
}
