package algo

import (
	"context"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
)

// KCoreResult holds the core decomposition of the graph viewed as undirected.
type KCoreResult struct {
	// Core is the coreness of each slot: the largest k such that the vertex
	// is in the k-core.
	Core []int32
	// Degeneracy is the largest coreness.
	Degeneracy int32
	// Order is the degeneracy ordering: the peel order, in which every
	// vertex has at most Degeneracy neighbours later in the order.
	Order []int32
	Truncation
}

// KCore runs the bucket peeling of Batagelj & Zaveršnik (2003) in O(m). Ties
// within a degree bucket resolve in ascending slot order, so the ordering is
// a function of the topology alone.
func KCore(ctx context.Context, g *csr.Graph) (r KCoreResult) {
	u := newUndirectedView(g)
	n := g.NumVertices()
	r.Core = make([]int32, n)
	r.Order = make([]int32, 0, n)
	if n == 0 {
		return
	}
	deg := make([]int32, n)
	var maxDeg int32
	for v := range n {
		deg[v] = u.degree(int32(v))
		maxDeg = max(maxDeg, deg[v])
	}
	// Bucket sort by degree; vert is the ordering, pos its inverse, bin the
	// start of each degree bucket.
	bin := make([]int32, maxDeg+2)
	for _, d := range deg {
		bin[d]++
	}
	start := int32(0)
	for d := range bin {
		c := bin[d]
		bin[d] = start
		start += c
	}
	vert := make([]int32, n)
	pos := make([]int32, n)
	for v := range n {
		d := deg[v]
		pos[v] = bin[d]
		vert[pos[v]] = int32(v)
		bin[d]++
	}
	for d := maxDeg; d > 0; d-- {
		bin[d] = bin[d-1]
	}
	bin[0] = 0
	for i := range n {
		if i&0xFFFF == 0 && engine.ContextDone(ctx) {
			r.Truncation = truncatedBy(LimitContext)
			return
		}
		v := vert[i]
		r.Core[v] = deg[v]
		r.Degeneracy = max(r.Degeneracy, deg[v])
		r.Order = append(r.Order, v)
		for _, w := range u.nbr(v) {
			if deg[w] <= deg[v] {
				continue
			}
			dw := deg[w]
			pw := pos[w]
			pu := bin[dw]
			x := vert[pu]
			if x != w {
				pos[w] = pu
				vert[pw] = x
				pos[x] = pw
				vert[pu] = w
			}
			bin[dw]++
			deg[w]--
		}
	}
	return
}
