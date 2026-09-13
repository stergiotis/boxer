package algo

import (
	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
)

// undirectedView is the symmetric, loop-free adjacency the undirected
// algorithms (k-core, triangles, cliques) walk. For an undirected graph it
// aliases the container's rows minus self-loops; for a directed graph it
// merges each vertex's in- and out-rows.
type undirectedView struct {
	start []int32
	list  []int32
}

func newUndirectedView(g *csr.Graph) (u undirectedView) {
	n := g.NumVertices()
	u.start = make([]int32, n+1)
	u.list = make([]int32, 0, g.NumArcs())
	buf := make([]int32, 0, 64)
	for v := range n {
		row := g.Out(int32(v))
		if g.IsDirected() {
			buf = engine.MergeSorted(g.Out(int32(v)), g.In(int32(v)), buf[:0])
			row = buf
		}
		for _, d := range row {
			if d != int32(v) {
				u.list = append(u.list, d)
			}
		}
		u.start[v+1] = int32(len(u.list))
	}
	return
}

func (u undirectedView) nbr(v int32) []int32 { return u.list[u.start[v]:u.start[v+1]] }

func (u undirectedView) degree(v int32) int32 { return u.start[v+1] - u.start[v] }

// intersectSorted writes a ∩ b into buf and returns it.
func intersectSorted(a, b, buf []int32) []int32 {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			i++
		case a[i] > b[j]:
			j++
		default:
			buf = append(buf, a[i])
			i++
			j++
		}
	}
	return buf
}

// intersectCount returns |a ∩ b|.
func intersectCount(a, b []int32) (c int) {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			i++
		case a[i] > b[j]:
			j++
		default:
			c++
			i++
			j++
		}
	}
	return
}
