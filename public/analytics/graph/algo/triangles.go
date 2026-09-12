package algo

import (
	"context"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
)

// TriangleOptions tunes [Triangles].
type TriangleOptions struct {
	// PerVertex also counts the triangles through each vertex. This part
	// runs serially, since three vertices share every triangle.
	PerVertex bool
}

// TriangleResult holds triangle counts of the graph viewed as undirected.
type TriangleResult struct {
	Total uint64
	// PerVertex is nil unless requested.
	PerVertex []uint32
	Truncation
}

// triangleChunk is the fixed job size of the parallel total; the fold is
// over integers, so the size affects speed only.
const triangleChunk = 1024

// Triangles counts triangles by the order-invariant method (GAP): each edge
// is oriented from the lower-(degree, slot) endpoint to the higher, and a
// triangle is counted once at its lowest vertex by intersecting oriented
// rows.
func Triangles(ctx context.Context, e *engine.Engine, g *csr.Graph, opts TriangleOptions) (r TriangleResult) {
	e = engineOrDefault(e)
	u := newUndirectedView(g)
	n := g.NumVertices()
	less := func(a, b int32) bool {
		da, db := u.degree(a), u.degree(b)
		return da < db || (da == db && a < b)
	}
	// Oriented rows: neighbours ranked above the vertex, ascending.
	oStart := make([]int32, n+1)
	oList := make([]int32, 0, len(u.list)/2)
	for v := range n {
		for _, w := range u.nbr(int32(v)) {
			if less(int32(v), w) {
				oList = append(oList, w)
			}
		}
		oStart[v+1] = int32(len(oList))
	}
	orow := func(v int32) []int32 { return oList[oStart[v]:oStart[v+1]] }
	if opts.PerVertex {
		r.PerVertex = make([]uint32, n)
		buf := make([]int32, 0, 64)
		for v := range n {
			if v&0xFFF == 0 && ctxDone(ctx) {
				r.Truncation = truncatedBy(LimitContext)
				return
			}
			rv := orow(int32(v))
			for _, w := range rv {
				for _, x := range intersectSorted(rv, orow(w), buf[:0]) {
					r.Total++
					r.PerVertex[v]++
					r.PerVertex[w]++
					r.PerVertex[x]++
				}
			}
		}
		return
	}
	chunks := (n + triangleChunk - 1) / triangleChunk
	partial := make([]uint64, chunks)
	e.ChunkedJobs(n, triangleChunk, func(c, lo, hi int) {
		var t uint64
		for v := lo; v < hi; v++ {
			rv := orow(int32(v))
			for _, w := range rv {
				t += uint64(intersectCount(rv, orow(w)))
			}
		}
		partial[c] = t
	})
	for _, p := range partial {
		r.Total += p
	}
	if ctxDone(ctx) {
		r.Truncation = truncatedBy(LimitContext)
	}
	return
}
