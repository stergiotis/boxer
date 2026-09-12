package play

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/analytics/graph/knn"
	"github.com/stergiotis/boxer/public/semistructured/leeway/card"
	"github.com/stretchr/testify/require"
)

func TestBucketIndexEqualWidth(t *testing.T) {
	const n = 8
	// Endpoints and midpoint.
	if got := bucketIndex(0, 0, 1, n); got != 0 {
		t.Errorf("min -> %d, want 0", got)
	}
	if got := bucketIndex(1, 0, 1, n); got != n-1 {
		t.Errorf("max -> %d, want %d", got, n-1)
	}
	if got := bucketIndex(0.5, 0, 1, n); got != 4 {
		t.Errorf("mid -> %d, want 4", got)
	}
	// Out-of-range clamps both ways.
	if got := bucketIndex(-5, 0, 1, n); got != 0 {
		t.Errorf("below min -> %d, want 0", got)
	}
	if got := bucketIndex(5, 0, 1, n); got != n-1 {
		t.Errorf("above max -> %d, want %d", got, n-1)
	}
}

// The off-by-one fix: with equal-width bins the top bucket covers a real 1/n
// slice of the range, not just the exact maximum. Under the old `*(n-1)`
// divisor, v=0.9 landed in bucket 6 and only v=1.0 reached bucket 7.
func TestBucketIndexTopBucketNotDegenerate(t *testing.T) {
	const n = 8
	if got := bucketIndex(0.9, 0, 1, n); got != 7 {
		t.Errorf("0.9 -> bucket %d, want 7 (top bucket must catch more than the exact max)", got)
	}
	// Every value in [0.875, 1.0] should map to the top bucket.
	for _, v := range []float64{0.876, 0.95, 0.999, 1.0} {
		if got := bucketIndex(v, 0, 1, n); got != n-1 {
			t.Errorf("%.3f -> bucket %d, want %d", v, got, n-1)
		}
	}
}

func TestBucketIndexDegenerate(t *testing.T) {
	if got := bucketIndex(5, 3, 3, 8); got != 0 {
		t.Errorf("zero-span -> %d, want 0", got)
	}
	if got := bucketIndex(5, 0, 10, 1); got != 0 {
		t.Errorf("n<=1 -> %d, want 0", got)
	}
}

func TestSubsampleFeaturesIdentity(t *testing.T) {
	feats := make([]card.EntityFeatures, 5)
	sampled, coordRow := subsampleFeatures(feats, 100)
	if len(sampled) != 5 || len(coordRow) != 5 {
		t.Fatalf("len sampled=%d coordRow=%d, want 5/5", len(sampled), len(coordRow))
	}
	for i := range coordRow {
		if coordRow[i] != int64(i) {
			t.Errorf("coordRow[%d]=%d, want identity", i, coordRow[i])
		}
	}
}

func TestSubsampleFeaturesCapped(t *testing.T) {
	const n, max = 1000, 100
	feats := make([]card.EntityFeatures, n)
	sampled, coordRow := subsampleFeatures(feats, max)
	if len(sampled) != max || len(coordRow) != max {
		t.Fatalf("len sampled=%d coordRow=%d, want %d", len(sampled), len(coordRow), max)
	}
	// First and last original rows must be retained, mapping monotonic.
	if coordRow[0] != 0 {
		t.Errorf("coordRow[0]=%d, want 0", coordRow[0])
	}
	if coordRow[max-1] != int64(n-1) {
		t.Errorf("coordRow[last]=%d, want %d", coordRow[max-1], n-1)
	}
	for i := 1; i < max; i++ {
		if coordRow[i] <= coordRow[i-1] {
			t.Errorf("coordRow not strictly increasing at %d: %d <= %d", i, coordRow[i], coordRow[i-1])
		}
	}
}

// The declaration built from a run: one node per slot with the cluster as
// its aura unless its membership is weak, one edge per neighbour pair with
// the membership as its strength, and the colour-by fill bucketed.
func TestBuildProjectionDeclaration(t *testing.T) {
	const n, d = 40, 3
	x := make([]float32, n*d)
	ids := make([]uint64, n)
	for i := range n {
		ids[i] = uint64(i) + 1
		for j := range d {
			v := float32(i%2) * 20 // two tight groups
			if j == i%d {
				v += float32(i) * 0.01
			}
			x[i*d+j] = v
		}
	}
	g, err := knn.Build(context.Background(), nil, x, d, ids, knn.Options{K: 5})
	require.NoError(t, err)
	dg, err := g.DistanceGraph()
	require.NoError(t, err)
	cl, err := algo.HDBSCAN(context.Background(), dg, g.CoreDist, algo.HDBSCANOptions{MinClusterSize: 5})
	require.NoError(t, err)
	res := &projectionResult{graph: g, clusters: cl, rows: make([]int64, n)}
	for f := range card.NumFeatures {
		res.featureColumns[f] = make([]float64, n)
		for s := range n {
			res.featureColumns[f][s] = float64(s)
		}
	}
	nodes, edges := buildProjectionDeclaration(res, -1, true)
	require.Len(t, nodes, n)
	require.Equal(t, int(g.Graph.NumEdges()), len(edges))
	for s, node := range nodes {
		require.Equal(t, uint64(s)+1, node.Id)
		if cl.Label[s] >= 0 && cl.Probability[s] >= projectionNoiseAuraFloor {
			require.Equal(t, []string{"cluster " + string(rune('1'+cl.Label[s]))}, node.Auras)
		} else {
			require.Empty(t, node.Auras)
		}
	}
	for _, e := range edges {
		require.Greater(t, e.Strength, float32(0))
		require.Less(t, e.From, e.To)
	}
	// Colour by feature 0: the buckets span the palette.
	coloured, _ := buildProjectionDeclaration(res, 0, false)
	require.NotEqual(t, coloured[0].Color, coloured[n-1].Color)
	require.Empty(t, coloured[0].Auras)
}
