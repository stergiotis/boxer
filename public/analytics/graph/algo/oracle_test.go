package algo

import (
	"math"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
)

// adjacency matrices for brute-force oracles; small n only.
type adj struct {
	n   int
	out [][]bool // out[s][d]
}

func adjOf(g *csr.Graph) adj {
	n := g.NumVertices()
	a := adj{n: n, out: make([][]bool, n)}
	for v := range n {
		a.out[v] = make([]bool, n)
		for _, d := range g.Out(int32(v)) {
			a.out[v][d] = true
		}
	}
	return a
}

func (a adj) sym() adj {
	b := adj{n: a.n, out: make([][]bool, a.n)}
	for v := range a.n {
		b.out[v] = make([]bool, a.n)
		for w := range a.n {
			b.out[v][w] = a.out[v][w] || a.out[w][v]
		}
	}
	return b
}

func (a adj) transpose() adj {
	b := adj{n: a.n, out: make([][]bool, a.n)}
	for v := range a.n {
		b.out[v] = make([]bool, a.n)
		for w := range a.n {
			b.out[v][w] = a.out[w][v]
		}
	}
	return b
}

// bfsOracle returns hop distances (-1 unreached) and shortest-path counts
// from s.
func (a adj) bfsOracle(s int) (dist []int, sigma []float64) {
	dist = make([]int, a.n)
	sigma = make([]float64, a.n)
	for i := range dist {
		dist[i] = -1
	}
	dist[s] = 0
	sigma[s] = 1
	queue := []int{s}
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		for w := range a.n {
			if !a.out[v][w] {
				continue
			}
			if dist[w] == -1 {
				dist[w] = dist[v] + 1
				queue = append(queue, w)
			}
			if dist[w] == dist[v]+1 {
				sigma[w] += sigma[v]
			}
		}
	}
	return
}

func (a adj) betweennessOracle() []float64 {
	bc := make([]float64, a.n)
	dist := make([][]int, a.n)
	sigma := make([][]float64, a.n)
	for s := range a.n {
		dist[s], sigma[s] = a.bfsOracle(s)
	}
	for s := range a.n {
		for t := range a.n {
			if s == t || dist[s][t] < 0 {
				continue
			}
			for v := range a.n {
				if v == s || v == t || dist[s][v] < 0 || dist[v][t] < 0 {
					continue
				}
				if dist[s][v]+dist[v][t] == dist[s][t] {
					bc[v] += sigma[s][v] * sigma[v][t] / sigma[s][t]
				}
			}
		}
	}
	return bc
}

func (a adj) reach() [][]bool {
	r := make([][]bool, a.n)
	for v := range a.n {
		r[v] = make([]bool, a.n)
		d, _ := a.bfsOracle(v)
		for w := range a.n {
			r[v][w] = d[w] >= 0
		}
	}
	return r
}

// samePartition reports whether two labelings induce the same partition.
func samePartition(x, y []int32) bool {
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		for j := range x {
			if (x[i] == x[j]) != (y[i] == y[j]) {
				return false
			}
		}
	}
	return true
}

func (a adj) pageRankOracle(damping float64, iters int) []float64 {
	n := a.n
	rank := make([]float64, n)
	for i := range rank {
		rank[i] = 1 / float64(n)
	}
	outdeg := make([]int, n)
	for v := range n {
		for w := range n {
			if a.out[v][w] {
				outdeg[v]++
			}
		}
	}
	for range iters {
		next := make([]float64, n)
		var dangling float64
		for v := range n {
			if outdeg[v] == 0 {
				dangling += rank[v]
			}
		}
		for d := range n {
			s := 0.0
			for v := range n {
				if a.out[v][d] {
					s += rank[v] / float64(outdeg[v])
				}
			}
			next[d] = (1-damping)/float64(n) + damping*(s+dangling/float64(n))
		}
		rank = next
	}
	return rank
}

// corenessOracle peels by definition: the k-core is the largest subgraph
// with minimum degree k.
func (a adj) corenessOracle() []int32 {
	core := make([]int32, a.n)
	for k := 1; ; k++ {
		alive := make([]bool, a.n)
		for i := range alive {
			alive[i] = true
		}
		for changed := true; changed; {
			changed = false
			for v := range a.n {
				if !alive[v] {
					continue
				}
				deg := 0
				for w := range a.n {
					if w != v && alive[w] && a.out[v][w] {
						deg++
					}
				}
				if deg < k {
					alive[v] = false
					changed = true
				}
			}
		}
		any := false
		for v := range a.n {
			if alive[v] {
				core[v] = int32(k)
				any = true
			}
		}
		if !any {
			return core
		}
	}
}

func (a adj) trianglesOracle() (total uint64, per []uint32) {
	per = make([]uint32, a.n)
	for u := range a.n {
		for v := u + 1; v < a.n; v++ {
			if !a.out[u][v] {
				continue
			}
			for w := v + 1; w < a.n; w++ {
				if a.out[u][w] && a.out[v][w] {
					total++
					per[u]++
					per[v]++
					per[w]++
				}
			}
		}
	}
	return
}

// cliquesOracle enumerates subsets; n must be small.
func (a adj) cliquesOracle() map[uint32]bool {
	isClique := func(mask uint32) bool {
		for u := range a.n {
			if mask&(1<<u) == 0 {
				continue
			}
			for v := u + 1; v < a.n; v++ {
				if mask&(1<<v) != 0 && !a.out[u][v] {
					return false
				}
			}
		}
		return true
	}
	cl := map[uint32]bool{}
	for mask := uint32(1); mask < 1<<a.n; mask++ {
		if isClique(mask) {
			cl[mask] = true
		}
	}
	maximal := map[uint32]bool{}
	for mask := range cl {
		isMax := true
		for v := range a.n {
			if mask&(1<<v) == 0 && cl[mask|1<<v] {
				isMax = false
				break
			}
		}
		if isMax {
			maximal[mask] = true
		}
	}
	return maximal
}

func approxEqual(a, b []float64, tol float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > tol*math.Max(1, math.Abs(b[i])) {
			return false
		}
	}
	return true
}
