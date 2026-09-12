package algo

import "github.com/stergiotis/boxer/public/analytics/graph/csr"

// Degrees returns the out- and in-degree of every slot. For an undirected
// graph the two are the same values.
func Degrees(g *csr.Graph) (out, in []int32) {
	n := g.NumVertices()
	out = make([]int32, n)
	in = make([]int32, n)
	for v := range n {
		out[v] = g.OutDegree(int32(v))
		in[v] = g.InDegree(int32(v))
	}
	return
}
