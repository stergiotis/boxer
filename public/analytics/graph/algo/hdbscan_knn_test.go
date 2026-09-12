package algo_test

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/analytics/graph/knn"
	"github.com/stretchr/testify/require"
)

// End to end over the producer: the neighbour graph's distance form and
// core distances into HDBSCAN, on planted blobs with outliers in four
// dimensions (ADR-0227 §SD3). Over a neighbour graph an outlier is not
// isolated — it has K neighbours — and it is the only bridge between blobs,
// so it can sit inside a blob's subtree and take the label with a low
// probability where the complete-graph form calls it noise; the test reads
// the probability, as a consumer should.
func TestHDBSCANOverTheNeighbourGraph(t *testing.T) {
	const c, per, d, outliers = 4, 60, 4, 6
	rng := rand.New(rand.NewPCG(21, 22))
	n := c*per + outliers
	x := make([]float32, 0, n*d)
	blob := make([]int, 0, n)
	ids := make([]uint64, 0, n)
	for b := range c {
		for range per {
			for j := range d {
				v := rng.NormFloat64()
				if j == b {
					v += 15
				}
				x = append(x, float32(v))
			}
			blob = append(blob, b)
			ids = append(ids, uint64(len(ids)+1))
		}
	}
	for o := range outliers {
		for j := range d {
			x = append(x, float32(-40-10*o+3*j))
		}
		blob = append(blob, -1)
		ids = append(ids, uint64(len(ids)+1))
	}
	r, err := knn.Build(context.Background(), nil, x, d, ids, knn.Options{K: 10})
	require.NoError(t, err)
	dg, err := r.DistanceGraph()
	require.NoError(t, err)
	h, err := algo.HDBSCAN(context.Background(), dg, r.CoreDist, algo.HDBSCANOptions{MinClusterSize: 10})
	require.NoError(t, err)
	require.Equal(t, c, h.NumClusters)
	labelOfBlob := map[int]int32{}
	for s := range dg.NumVertices() {
		row := int(r.Rows[s])
		b := blob[row]
		if b < 0 {
			require.True(t, h.Label[s] < 0 || h.Probability[s] < 0.1, "outlier row %d: label %d, probability %v", row, h.Label[s], h.Probability[s])
			continue
		}
		require.GreaterOrEqual(t, h.Label[s], int32(0), "row %d", row)
		require.Greater(t, h.Probability[s], float32(0.1), "row %d", row)
		if prev, ok := labelOfBlob[b]; ok {
			require.Equal(t, prev, h.Label[s])
		} else {
			labelOfBlob[b] = h.Label[s]
		}
	}
	require.Len(t, labelOfBlob, c)
}

// BenchmarkHDBSCAN runs over the neighbour graph of ten thousand rows of
// sixteen features, the Projection lane's cap (ADR-0227 §SD3).
func BenchmarkHDBSCAN(b *testing.B) {
	const n, d, k = 10_000, 16, 15
	rng := rand.New(rand.NewPCG(5, 6))
	x := make([]float32, n*d)
	for i := range x {
		x[i] = float32(rng.NormFloat64())
	}
	ids := make([]uint64, n)
	for i := range ids {
		ids[i] = uint64(i)
	}
	r, err := knn.Build(context.Background(), nil, x, d, ids, knn.Options{K: k})
	if err != nil {
		b.Fatal(err)
	}
	dg, err := r.DistanceGraph()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := algo.HDBSCAN(context.Background(), dg, r.CoreDist, algo.HDBSCANOptions{MinClusterSize: 10}); err != nil {
			b.Fatal(err)
		}
	}
}
