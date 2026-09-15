package play

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/analytics/graph/knn"
	"github.com/stergiotis/boxer/public/semistructured/leeway/card"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
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
	var nodes graphview.NodeColumns
	var edges graphview.EdgeColumns
	buildProjectionDeclaration(res, -1, true, &nodes, &edges)
	require.NoError(t, nodes.Validate())
	require.NoError(t, edges.Validate())
	require.Len(t, nodes.Ids, n)
	require.Equal(t, int(g.Graph.NumEdges()), len(edges.From))
	for s, id := range nodes.Ids {
		require.Equal(t, uint64(s)+1, id)
		auras := nodes.AuraIds[nodes.AuraOffsets[s]:nodes.AuraOffsets[s+1]]
		if cl.Label[s] >= 0 && cl.Probability[s] >= projectionNoiseAuraFloor {
			require.Equal(t, []string{"cluster " + string(rune('1'+cl.Label[s]))}, auras)
		} else {
			require.Empty(t, auras)
		}
	}
	for i := range edges.From {
		require.Greater(t, edges.Strength[i], float32(0))
		require.Less(t, edges.From[i], edges.To[i])
	}
	// Colour by feature 0, over the same columns: the buckets span the
	// palette, and no aura column is declared.
	buildProjectionDeclaration(res, 0, false, &nodes, &edges)
	require.NoError(t, nodes.Validate())
	require.NotEqual(t, nodes.Color[0], nodes.Color[n-1])
	require.Nil(t, nodes.AuraOffsets)
}

func TestProjectionFeatureSetNames(t *testing.T) {
	require.Equal(t, "shape", projectionFeatureShape.String())
	require.Equal(t, "structure", projectionFeatureStructure.String())
	require.Equal(t, "?", projectionFeatureSetE(9).String())
	for _, fs := range projectionFeatureSets {
		require.Contains(t, fs.Doc(), fs.String())
	}
	require.Equal(t, projectionFeatureShape, projectionParams{}.FeatureSet, "the zero value is the shape set the lane always had")
}
