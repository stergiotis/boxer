package knn

import (
	"context"
	"math"
	"math/rand/v2"
	"sort"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// oracleKNN is the O(n²) neighbour list by (distance, index), the row
// itself excluded.
func oracleKNN(x []float32, d, n, k int, metric MetricE) (idx [][]int32, dist [][]float32) {
	norms := make([]float64, n)
	for i := range n {
		var s float64
		for _, v := range x[i*d : (i+1)*d] {
			s += float64(v) * float64(v)
		}
		norms[i] = math.Sqrt(s)
	}
	distance := func(i, j int) float32 {
		xi, xj := x[i*d:(i+1)*d], x[j*d:(j+1)*d]
		if metric == MetricCosine {
			if norms[i] == 0 || norms[j] == 0 {
				return 1
			}
			var dot float32
			for c := range xi {
				dot += xi[c] * xj[c]
			}
			v := 1 - dot/(float32(norms[i])*float32(norms[j]))
			if v < 0 {
				v = 0
			}
			return v
		}
		var s float32
		for c := range xi {
			t := xi[c] - xj[c]
			s += t * t
		}
		return float32(math.Sqrt(float64(s)))
	}
	idx = make([][]int32, n)
	dist = make([][]float32, n)
	for i := range n {
		type cand struct {
			j int32
			d float32
		}
		cs := make([]cand, 0, n-1)
		for j := range n {
			if j != i {
				cs = append(cs, cand{int32(j), distance(i, j)})
			}
		}
		sort.Slice(cs, func(a, b int) bool {
			if cs[a].d != cs[b].d {
				return cs[a].d < cs[b].d
			}
			return cs[a].j < cs[b].j
		})
		for _, c := range cs[:k] {
			idx[i] = append(idx[i], c.j)
			dist[i] = append(dist[i], c.d)
		}
	}
	return
}

func seqIDs(n int) []uint64 {
	ids := make([]uint64, n)
	for i := range ids {
		ids[i] = uint64(i)
	}
	return ids
}

func TestBuildMatchesOracle(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(2, 12).Draw(t, "n")
		d := rapid.IntRange(1, 4).Draw(t, "d")
		k := rapid.IntRange(1, n-1).Draw(t, "k")
		metric := rapid.SampledFrom([]MetricE{MetricEuclidean, MetricCosine}).Draw(t, "metric")
		// Small integer-valued coordinates make ties and duplicate points common.
		x := make([]float32, n*d)
		for i := range x {
			x[i] = float32(rapid.IntRange(-2, 2).Draw(t, "x"))
		}
		r, err := Build(context.Background(), engine.New(1), x, d, seqIDs(n), Options{K: k, Metric: metric})
		require.NoError(t, err)
		require.Equal(t, k, r.K)
		require.False(t, r.Truncation.Truncated)
		require.Equal(t, n, r.Graph.NumVertices())
		require.False(t, r.Graph.IsDirected())

		oi, od := oracleKNN(x, d, n, k, metric)
		for i := range n {
			// ids are 0..n-1 so slot == row.
			require.Equal(t, int32(i), r.Rows[i])
			require.Equal(t, oi[i], r.Indices[i*k:(i+1)*k], "row %d", i)
			require.Equal(t, od[i], r.Dists[i*k:(i+1)*k], "row %d", i)
			require.Equal(t, od[i][k-1], r.CoreDist[i])
		}
		// Weights in (0, 1], symmetric, and the arc set is the union of the
		// directed neighbour lists.
		for i := range n {
			for a, j := range r.Graph.Out(int32(i)) {
				w := r.Graph.OutWeights(int32(i))[a]
				require.Greater(t, w, float32(0))
				require.LessOrEqual(t, w, float32(1)+1e-6)
				back := r.Graph.OutWeights(j)
				bi := sort.Search(len(r.Graph.Out(j)), func(b int) bool { return r.Graph.Out(j)[b] >= int32(i) })
				require.Equal(t, w, back[bi])
			}
		}
		// The arc set is the fuzzy union of the directed memberships computed
		// from the result's own σ and ρ; a membership that underflows to zero
		// is no arc, as umap-learn eliminates zeros.
		member := func(i int, j int32) float64 {
			for a, jj := range oi[i] {
				if jj == j {
					dd := float64(od[i][a]) - float64(r.Rhos[i])
					if dd <= 0 || r.Sigmas[i] == 0 {
						return 1
					}
					return math.Exp(-dd / float64(r.Sigmas[i]))
				}
			}
			return 0
		}
		arcs := 0
		for i := range n {
			for j := range n {
				if i == j {
					continue
				}
				a, b := member(i, int32(j)), member(j, int32(i))
				w := float32(a + b - a*b)
				if w > 0 {
					require.True(t, r.Graph.HasArc(int32(i), int32(j)), "arc %d→%d", i, j)
					arcs++
					out := r.Graph.Out(int32(i))
					bi := sort.Search(len(out), func(b int) bool { return out[b] >= int32(j) })
					require.InDelta(t, float64(w), float64(r.Graph.OutWeights(int32(i))[bi]), 1e-6, "weight %d→%d", i, j)
				} else {
					require.False(t, r.Graph.HasArc(int32(i), int32(j)), "arc %d→%d", i, j)
				}
			}
		}
		require.Equal(t, arcs, r.Graph.NumArcs())
		// The membership sum hits log₂(k+1) unless the bandwidth was floored.
		target := math.Log2(float64(k + 1))
		for i := range n {
			sigma, rho := float64(r.Sigmas[i]), float64(r.Rhos[i])
			var psum float64
			for _, v := range r.Dists[i*k : (i+1)*k] {
				dd := float64(v) - rho
				if dd > 0 {
					psum += math.Exp(-dd / sigma)
				} else {
					psum += 1
				}
			}
			allAtRho := true
			for _, v := range r.Dists[i*k : (i+1)*k] {
				if float64(v) > rho {
					allAtRho = false
				}
			}
			if allAtRho {
				require.InDelta(t, float64(k), psum, 1e-6)
				continue
			}
			if math.Abs(psum-target) > 1e-3 {
				// Only a floored sigma may miss the target.
				require.Less(t, sigma, 1e-2, "row %d: psum %v target %v sigma %v", i, psum, target, sigma)
			}
		}
		dg, err := r.DistanceGraph()
		require.NoError(t, err)
		require.Equal(t, r.Graph.NumArcs(), dg.NumArcs())
		require.Equal(t, r.Graph.Fingerprint(), dg.Fingerprint())
	})
}

func randomMatrix(n, d int, seed uint64) []float32 {
	rng := rand.New(rand.NewPCG(seed, seed+1))
	x := make([]float32, n*d)
	for i := range x {
		x[i] = float32(rng.NormFloat64())
	}
	return x
}

func TestBuildIsDeterministicAcrossWorkers(t *testing.T) {
	n, d := 3*engine.MinParallel+7, 6
	x := randomMatrix(n, d, 7)
	a, err := Build(context.Background(), engine.New(1), x, d, seqIDs(n), Options{K: 8})
	require.NoError(t, err)
	b, err := Build(context.Background(), engine.New(0), x, d, seqIDs(n), Options{K: 8})
	require.NoError(t, err)
	require.Equal(t, a.Indices, b.Indices)
	require.Equal(t, a.Dists, b.Dists)
	require.Equal(t, a.Sigmas, b.Sigmas)
	require.Equal(t, a.Rhos, b.Rhos)
	require.Equal(t, a.Graph.Fingerprint(), b.Graph.Fingerprint())
	for v := range n {
		require.Equal(t, a.Graph.OutWeights(int32(v)), b.Graph.OutWeights(int32(v)))
	}
}

func TestBuildRowBudget(t *testing.T) {
	n, d := 100, 3
	x := randomMatrix(n, d, 3)
	ids := make([]uint64, n)
	for i := range ids {
		ids[i] = uint64(1000 - i) // descending, so slot order differs from row order
	}
	r, err := Build(context.Background(), nil, x, d, ids, Options{K: 4, MaxRows: 10})
	require.NoError(t, err)
	require.True(t, r.Truncation.Truncated)
	require.Equal(t, algo.LimitRows, r.Truncation.By)
	require.Equal(t, 10, r.Graph.NumVertices())
	// Kept rows are a uniform stride with the first and last row present,
	// and Rows maps each slot to its input row.
	seen := map[int32]bool{}
	for s := range 10 {
		row := r.Rows[s]
		seen[row] = true
		require.Equal(t, ids[row], r.Graph.ID(int32(s)))
	}
	require.True(t, seen[0] && seen[int32(n-1)])
	// Neighbour indices are slots, so they resolve to kept rows.
	for _, s := range r.Indices {
		require.True(t, seen[r.Rows[s]])
	}
}

func TestBuildErrors(t *testing.T) {
	_, err := Build(context.Background(), nil, []float32{1, 2, 3}, 2, []uint64{1, 2}, Options{})
	require.Error(t, err)
	_, err = Build(context.Background(), nil, []float32{1, 2, 3, 4}, 2, []uint64{1, 1}, Options{K: 1})
	require.Error(t, err, "duplicate ids")
	_, err = Build(context.Background(), nil, []float32{1, 2}, 2, []uint64{1}, Options{K: 1})
	require.Error(t, err, "one row")
	_, err = Build(context.Background(), nil, []float32{float32(math.NaN()), 2, 3, 4}, 2, []uint64{1, 2}, Options{K: 1})
	require.Error(t, err, "nan")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	x := randomMatrix(5000, 4, 1)
	_, err = Build(ctx, engine.New(1), x, 4, seqIDs(5000), Options{K: 5})
	require.Error(t, err, "cancelled")
}

func TestKClamps(t *testing.T) {
	x := randomMatrix(4, 2, 9)
	r, err := Build(context.Background(), nil, x, 2, seqIDs(4), Options{K: 50})
	require.NoError(t, err)
	require.Equal(t, 3, r.K)
	require.Equal(t, 4*3, r.Graph.NumArcs())
}
