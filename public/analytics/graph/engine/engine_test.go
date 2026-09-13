package engine

import (
	"math/rand/v2"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stretchr/testify/require"
)

func randomGraph(t *testing.T, n, m int, seed uint64, directed bool) *csr.Graph {
	rng := rand.New(rand.NewPCG(seed, seed))
	src := make([]uint64, m)
	dst := make([]uint64, m)
	for i := range m {
		src[i] = uint64(rng.IntN(n))
		dst[i] = uint64(rng.IntN(n))
	}
	g, err := csr.BuildE(src, dst, nil, csr.Options{Directed: directed})
	require.NoError(t, err)
	return g
}

func TestReduceFloat64IsIndependentOfWorkers(t *testing.T) {
	n := 10*ReduceChunk + 17
	vals := make([]float64, n)
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range vals {
		vals[i] = rng.Float64() * 1e6
	}
	body := func(lo, hi int) float64 {
		var s float64
		for i := lo; i < hi; i++ {
			s += vals[i]
		}
		return s
	}
	ref := New(1).ReduceFloat64(n, body)
	for _, w := range []int{2, 3, 7, 16} {
		require.Equal(t, ref, New(w).ReduceFloat64(n, body), "workers=%d", w)
	}
}

func TestParallelForCoversRangeOnce(t *testing.T) {
	n := 3*MinParallel + 5
	seen := make([]int32, n)
	New(5).ParallelFor(n, func(_, lo, hi int) {
		for i := lo; i < hi; i++ {
			seen[i]++
		}
	})
	for i, c := range seen {
		require.EqualValues(t, 1, c, "index %d", i)
	}
}

// bfsLevel runs one BFS level with the given sweep forced and returns the
// next frontier and the parents it assigned.
func bfsLevel(e *Engine, g *csr.Graph, frontier *Subset, force EdgeMapOptions) ([]int32, []int32) {
	n := g.NumVertices()
	parent := make([]int32, n)
	for i := range parent {
		parent[i] = -1
	}
	for _, s := range frontier.Sparse() {
		parent[s] = s
	}
	next, _ := e.EdgeMap(g, frontier, EdgeFuncs{
		Update: func(s, d int32) bool {
			if parent[d] != -1 {
				return false
			}
			parent[d] = s
			return true
		},
		Cond: func(d int32) bool { return parent[d] == -1 },
	}, force)
	return append([]int32(nil), next.Sparse()...), parent
}

func TestPushAndPullAgree(t *testing.T) {
	for _, directed := range []bool{false, true} {
		for _, dir := range []DirectionE{DirectionOut, DirectionIn, DirectionBoth} {
			g := randomGraph(t, 5000, 20000, 7, directed)
			frontier := FromSlots(g.NumVertices(), []int32{0, 17, 4242, 999})
			nPush, pPush := bfsLevel(New(1), g, frontier, EdgeMapOptions{Direction: dir, ForcePush: true})
			nPull, pPull := bfsLevel(New(4), g, frontier, EdgeMapOptions{Direction: dir, ForcePull: true})
			require.Equal(t, nPush, nPull, "directed=%v dir=%d", directed, dir)
			require.Equal(t, pPush, pPull, "directed=%v dir=%d", directed, dir)
		}
	}
}

func TestSubsetConversions(t *testing.T) {
	s := FromSlots(10, []int32{5, 1, 5, 9})
	require.Equal(t, 3, s.Len())
	require.Equal(t, []int32{1, 5, 9}, s.Sparse())
	d := s.Dense()
	require.True(t, d[1] && d[5] && d[9])
	require.False(t, d[0] || d[2])
	require.True(t, s.Contains(9))
	require.False(t, s.Contains(8))
	s.SetDense([]bool{true, false, false, false, false, false, false, false, false, true})
	require.Equal(t, []int32{0, 9}, s.Sparse())
}
