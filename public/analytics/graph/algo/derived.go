package algo

import "github.com/stergiotis/boxer/public/analytics/graph/csr"

// The metrics below are derived from another algorithm's result rather than
// walked for in their own right (ADR-0232 §SD6). Each is a few lines wherever
// it is written; the point of writing them here is that the metric vocabulary,
// the facts follow-up (ADR-0229 §SD5) and the navigation layer read one
// definition instead of restating it.

// LocalClustering is the local clustering coefficient of every slot: the
// fraction of a vertex's neighbour pairs that are themselves adjacent,
// 2·T(v) / (d(v)·(d(v)−1)) over the graph viewed as undirected, which is the
// same view [Triangles] counts in.
//
// perVertex is [TriangleResult.PerVertex] — a count run with
// [TriangleOptions.PerVertex] set; passing a nil or mis-sized slice returns
// nil. A vertex of degree below two has no neighbour pair and so no
// coefficient: its entry is NaN, not zero, because "undefined" and "no closed
// pair" are different readings and a ramp must not place the first at the
// bottom.
func LocalClustering(g *csr.Graph, perVertex []uint32) (coef []float64) {
	n := g.NumVertices()
	if len(perVertex) != n {
		return nil
	}
	u := newUndirectedView(g)
	coef = make([]float64, n)
	for v := range n {
		d := float64(u.degree(int32(v)))
		if d < 2 {
			coef[v] = nan64
			continue
		}
		coef[v] = 2 * float64(perVertex[v]) / (d * (d - 1))
	}
	return
}

// CliqueSizes is the size of the largest maximal clique containing each slot,
// read off a listing from [MaximalCliques]. A vertex in no listed clique — an
// isolated one, or one whose cliques fell past the listing's output cap —
// scores zero.
//
// Under a truncated listing the result is a lower bound, which is why
// [CliqueResult.Truncation] travels with it through [Compute] and a consumer
// is expected to say so.
func CliqueSizes(g *csr.Graph, r CliqueResult) (size []int32) {
	n := g.NumVertices()
	size = make([]int32, n)
	for i := 0; i+1 < len(r.Start); i++ {
		members := r.Members[r.Start[i]:r.Start[i+1]]
		s := int32(len(members))
		for _, v := range members {
			if v >= 0 && int(v) < n && s > size[v] {
				size[v] = s
			}
		}
	}
	return
}

// ComponentSizes turns a component labelling — [CCResult.Comp] or
// [SCCResult.Comp] — into the size of each slot's own component, which is
// what makes "the big component" a quantity a ramp can carry where the label
// itself is only a name.
func ComponentSizes(comp []int32) (size []int32) {
	if len(comp) == 0 {
		return nil
	}
	count := make(map[int32]int32, 16)
	for _, c := range comp {
		count[c]++
	}
	size = make([]int32, len(comp))
	for i, c := range comp {
		size[i] = count[c]
	}
	return
}
