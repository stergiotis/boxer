package algo

import (
	"context"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
)

// CCResult labels weakly connected components.
type CCResult struct {
	// Comp is the component label of each slot: the smallest slot in its
	// component, so labels are canonical.
	Comp []int32
	// Count is the number of components.
	Count int
	Truncation
}

// ConnectedComponents labels the connected components of the graph viewed
// as undirected (weak components of a directed graph) by union–find with
// path halving and union by size.
func ConnectedComponents(ctx context.Context, g *csr.Graph) (r CCResult) {
	n := g.NumVertices()
	parent := make([]int32, n)
	size := make([]int32, n)
	for v := range n {
		parent[v] = int32(v)
		size[v] = 1
	}
	find := func(v int32) int32 {
		for parent[v] != v {
			parent[v] = parent[parent[v]]
			v = parent[v]
		}
		return v
	}
	union := func(a, b int32) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		if size[ra] < size[rb] {
			ra, rb = rb, ra
		}
		parent[rb] = ra
		size[ra] += size[rb]
	}
	targets := g.OutTargets()
	offsets := g.OutOffsets()
	for v := range n {
		if v&0xFFFF == 0 && ctxDone(ctx) {
			r.Truncation = truncatedBy(LimitContext)
			return
		}
		for _, d := range targets[offsets[v]:offsets[v+1]] {
			if d > int32(v) || (g.IsDirected() && d != int32(v)) {
				union(int32(v), d)
			}
		}
	}
	r.Comp = make([]int32, n)
	fill(r.Comp, -1)
	for v := range n {
		root := find(int32(v))
		if r.Comp[root] == -1 {
			// First slot to reach this root is the smallest in the component.
			r.Comp[root] = int32(v)
			r.Count++
		}
		r.Comp[v] = r.Comp[root]
	}
	return
}

// SCCResult labels strongly connected components.
type SCCResult struct {
	// Comp is the component index of each slot, numbered in the order
	// Tarjan completes them (a sink component first).
	Comp []int32
	// Count is the number of components.
	Count int
	Truncation
}

// StronglyConnectedComponents runs an iterative Tarjan over the out-rows in
// ascending order. On an undirected graph it agrees with
// [ConnectedComponents] up to labelling.
func StronglyConnectedComponents(ctx context.Context, g *csr.Graph) (r SCCResult) {
	n := g.NumVertices()
	const unvisited = -1
	index := make([]int32, n)
	low := make([]int32, n)
	fill(index, unvisited)
	onStack := make([]bool, n)
	r.Comp = make([]int32, n)
	fill(r.Comp, -1)
	stack := make([]int32, 0, 64)
	type frame struct {
		v    int32
		next int32 // position in v's out-row
	}
	call := make([]frame, 0, 64)
	offsets := g.OutOffsets()
	targets := g.OutTargets()
	var counter int32
	for root := range n {
		if index[root] != unvisited {
			continue
		}
		if root&0x3FF == 0 && ctxDone(ctx) {
			r.Truncation = truncatedBy(LimitContext)
			return
		}
		index[root] = counter
		low[root] = counter
		counter++
		stack = append(stack, int32(root))
		onStack[root] = true
		call = append(call, frame{v: int32(root), next: offsets[root]})
		for len(call) > 0 {
			f := &call[len(call)-1]
			v := f.v
			if f.next < offsets[v+1] {
				w := targets[f.next]
				f.next++
				if index[w] == unvisited {
					index[w] = counter
					low[w] = counter
					counter++
					stack = append(stack, w)
					onStack[w] = true
					call = append(call, frame{v: w, next: offsets[w]})
				} else if onStack[w] && index[w] < low[v] {
					low[v] = index[w]
				}
				continue
			}
			// v is finished.
			if low[v] == index[v] {
				comp := int32(r.Count)
				r.Count++
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[w] = false
					r.Comp[w] = comp
					if w == v {
						break
					}
				}
			}
			call = call[:len(call)-1]
			if len(call) > 0 {
				p := call[len(call)-1].v
				if low[v] < low[p] {
					low[p] = low[v]
				}
			}
		}
	}
	return
}
