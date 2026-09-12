package algo

import (
	"context"
	"math/rand/v2"
	"runtime"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
	"github.com/stretchr/testify/require"
)

// rmat draws an R-MAT edge list (Chakrabarti, Zhan & Faloutsos 2004) with
// the Graph500 probabilities, seeded.
func rmat(scale, edgesPerVertex int, seed uint64) (src, dst []uint64) {
	n := 1 << scale
	m := n * edgesPerVertex
	rng := rand.New(rand.NewPCG(seed, seed*3+1))
	src = make([]uint64, m)
	dst = make([]uint64, m)
	const a, b, c = 0.57, 0.19, 0.19
	for i := range m {
		var u, v uint64
		for bit := scale - 1; bit >= 0; bit-- {
			r := rng.Float64()
			switch {
			case r < a:
			case r < a+b:
				v |= 1 << bit
			case r < a+b+c:
				u |= 1 << bit
			default:
				u |= 1 << bit
				v |= 1 << bit
			}
		}
		src[i], dst[i] = u, v
	}
	return
}

func rmatGraph(t testing.TB, scale, epv int, directed bool) *csr.Graph {
	src, dst := rmat(scale, epv, 42)
	g, err := csr.BuildE(src, dst, nil, csr.Options{Directed: directed})
	require.NoError(t, err)
	return g
}

// TestResultsAreIndependentOfWorkerCount is the ADR-0226 §SD2 contract: the
// bits of every result are the same at one worker and at many.
func TestResultsAreIndependentOfWorkerCount(t *testing.T) {
	ctx := context.Background()
	counts := []int{1, 3, runtime.GOMAXPROCS(0)}
	for _, directed := range []bool{false, true} {
		g := rmatGraph(t, 12, 8, directed)
		type run struct {
			bfs BFSResult
			pr  PageRankResult
			bc  BetweennessResult
			bcS BetweennessResult
			tri TriangleResult
		}
		var ref run
		for i, w := range counts {
			e := engine.New(w)
			var cur run
			var err error
			cur.bfs, err = BFS(ctx, e, g, []int32{0, 5}, BFSOptions{Direction: engine.DirectionBoth})
			require.NoError(t, err)
			cur.pr = PageRank(ctx, e, g, PageRankOptions{Iterations: 15})
			cur.bc, err = Betweenness(ctx, e, g, BetweennessOptions{Sources: []int32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40}})
			require.NoError(t, err)
			cur.bcS, err = Betweenness(ctx, e, g, BetweennessOptions{ExactMaxVertices: 100, Pivots: 50, Seed: 9})
			require.NoError(t, err)
			cur.tri = Triangles(ctx, e, g, TriangleOptions{})
			if i == 0 {
				ref = cur
				continue
			}
			require.Equal(t, ref.bfs.Depth, cur.bfs.Depth, "workers=%d", w)
			require.Equal(t, ref.bfs.Parent, cur.bfs.Parent, "workers=%d", w)
			require.Equal(t, ref.pr.Rank, cur.pr.Rank, "workers=%d", w)
			require.Equal(t, ref.pr.Delta, cur.pr.Delta, "workers=%d", w)
			require.Equal(t, ref.bc.Score, cur.bc.Score, "workers=%d", w)
			require.Equal(t, ref.bcS.Score, cur.bcS.Score, "workers=%d", w)
			require.Equal(t, ref.tri.Total, cur.tri.Total, "workers=%d", w)
		}
	}
}
