package algo

import (
	"context"
	"math"
	"math/rand/v2"
	"sort"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// completeGraph builds the complete distance graph of 2-D points with ids
// 0..n-1 (so slot == index) and the core distance to the k-th neighbour.
type testingT interface {
	require.TestingT
	Helper()
}

func completeGraph(t testingT, pts [][2]float64, k int) (*csr.Graph, []float32, [][]float32) {
	t.Helper()
	n := len(pts)
	dist := make([][]float32, n)
	var src, dst []uint64
	var w []float32
	for i := range n {
		dist[i] = make([]float32, n)
		for j := range n {
			d := float32(math.Hypot(pts[i][0]-pts[j][0], pts[i][1]-pts[j][1]))
			dist[i][j] = d
			if j > i {
				src = append(src, uint64(i))
				dst = append(dst, uint64(j))
				w = append(w, d)
			}
		}
	}
	core := make([]float32, n)
	for i := range n {
		ds := append([]float32(nil), dist[i]...)
		sort.Slice(ds, func(a, b int) bool { return ds[a] < ds[b] })
		if k < n {
			core[i] = ds[k] // ds[0] is the point itself
		} else {
			core[i] = ds[n-1]
		}
	}
	g, err := csr.BuildE(src, dst, w, csr.Options{})
	require.NoError(t, err)
	return g, core, dist
}

// singleLinkageOracle merges the two clusters at the smallest mutual
// reachability distance until one remains, returning the merge heights.
func singleLinkageOracle(dist [][]float32, core []float32) []float32 {
	n := len(dist)
	member := make([]int, n)
	for i := range member {
		member[i] = i
	}
	var heights []float32
	for merges := 0; merges < n-1; merges++ {
		best := float32(math.MaxFloat32)
		bi, bj := -1, -1
		for i := range n {
			for j := i + 1; j < n; j++ {
				if member[i] == member[j] {
					continue
				}
				d := max(dist[i][j], core[i], core[j])
				if d < best {
					best, bi, bj = d, i, j
				}
			}
		}
		heights = append(heights, best)
		old, keep := member[bj], member[bi]
		for i := range member {
			if member[i] == old {
				member[i] = keep
			}
		}
	}
	sort.Slice(heights, func(a, b int) bool { return heights[a] < heights[b] })
	return heights
}

func TestHDBSCANHierarchyMatchesSingleLinkage(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(2, 10).Draw(t, "n")
		k := rapid.IntRange(1, n-1).Draw(t, "k")
		m := rapid.IntRange(2, 4).Draw(t, "m")
		pts := make([][2]float64, n)
		for i := range pts {
			pts[i] = [2]float64{float64(rapid.IntRange(0, 6).Draw(t, "x")), float64(rapid.IntRange(0, 6).Draw(t, "y"))}
		}
		g, core, dist := completeGraph(t, pts, k)
		r, err := HDBSCAN(context.Background(), g, core, HDBSCANOptions{MinClusterSize: m})
		require.NoError(t, err)
		require.False(t, r.Truncation.Truncated)
		require.Equal(t, singleLinkageOracle(dist, core), r.Heights)
		// Labels: dense in [0, K), each cluster at least m strong, noise
		// unlabelled with probability 0, members in (0, 1].
		counts := make([]int, r.NumClusters)
		for i, lb := range r.Label {
			if lb < 0 {
				require.Equal(t, float32(0), r.Probability[i])
				continue
			}
			require.Less(t, int(lb), r.NumClusters)
			counts[lb]++
			require.Greater(t, r.Probability[i], float32(0))
			require.LessOrEqual(t, r.Probability[i], float32(1))
		}
		for lb, c := range counts {
			require.GreaterOrEqual(t, c, m, "cluster %d", lb)
		}
		require.Len(t, r.Stability, r.NumClusters)
		// Determinism.
		r2, _ := HDBSCAN(context.Background(), g, core, HDBSCANOptions{MinClusterSize: m})
		require.Equal(t, r.Label, r2.Label)
	})
}

// blobsAndNoise plants c tight blobs of size per and a few far outliers.
func blobsAndNoise(c, per, outliers int, seed uint64) (pts [][2]float64, blob []int) {
	rng := rand.New(rand.NewPCG(seed, seed+1))
	for b := range c {
		cx, cy := float64(b)*100, float64(b%2)*100
		for range per {
			pts = append(pts, [2]float64{cx + rng.NormFloat64(), cy + rng.NormFloat64()})
			blob = append(blob, b)
		}
	}
	for o := range outliers {
		pts = append(pts, [2]float64{-200 - float64(o)*50, 300 + float64(o)*70})
		blob = append(blob, -1)
	}
	return
}

func TestHDBSCANFindsPlantedClustersAndNoise(t *testing.T) {
	pts, blob := blobsAndNoise(3, 30, 4, 9)
	g, core, _ := completeGraph(t, pts, 5)
	r, err := HDBSCAN(context.Background(), g, core, HDBSCANOptions{MinClusterSize: 5})
	require.NoError(t, err)
	require.Equal(t, 3, r.NumClusters)
	labelOfBlob := map[int]int32{}
	for i, b := range blob {
		if b < 0 {
			require.Equal(t, int32(-1), r.Label[i], "outlier %d is noise", i)
			continue
		}
		require.GreaterOrEqual(t, r.Label[i], int32(0), "blob point %d is clustered", i)
		if prev, ok := labelOfBlob[b]; ok {
			require.Equal(t, prev, r.Label[i], "blob %d is one cluster", b)
		} else {
			labelOfBlob[b] = r.Label[i]
		}
	}
	require.Len(t, labelOfBlob, 3, "blobs map to distinct clusters")
	for _, s := range r.Stability {
		require.Greater(t, s, float32(0))
	}
}

func TestHDBSCANSingleClusterOptionAndErrors(t *testing.T) {
	pts, _ := blobsAndNoise(1, 20, 0, 2)
	g, core, _ := completeGraph(t, pts, 3)
	r, err := HDBSCAN(context.Background(), g, core, HDBSCANOptions{MinClusterSize: 5})
	require.NoError(t, err)
	// One blob and no root selection: the split children compete; whatever
	// they yield, the root itself is never a cluster.
	for _, lb := range r.Label {
		require.Less(t, int(lb), r.NumClusters)
	}
	r, err = HDBSCAN(context.Background(), g, core, HDBSCANOptions{MinClusterSize: 5, AllowSingleCluster: true})
	require.NoError(t, err)
	require.GreaterOrEqual(t, r.NumClusters, 1)

	// An unweighted graph is refused; a mismatched core column is refused.
	ug, _ := csr.BuildE([]uint64{1, 2}, []uint64{2, 3}, nil, csr.Options{})
	_, err = HDBSCAN(context.Background(), ug, nil, HDBSCANOptions{})
	require.Error(t, err)
	_, err = HDBSCAN(context.Background(), g, core[:3], HDBSCANOptions{})
	require.Error(t, err)

	// A cancelled context truncates.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err = HDBSCAN(ctx, g, core, HDBSCANOptions{})
	require.NoError(t, err)
	require.True(t, r.Truncation.Truncated)
	require.Equal(t, LimitContext, r.Truncation.By)
}

func TestHDBSCANDisconnectedComponentsAreClusters(t *testing.T) {
	// Two components with no edge between them, each dense enough.
	var src, dst []uint64
	var w []float32
	for c := range 2 {
		base := uint64(c * 10)
		for i := range 10 {
			for j := i + 1; j < 10; j++ {
				src = append(src, base+uint64(i))
				dst = append(dst, base+uint64(j))
				w = append(w, 1+0.01*float32(i+j))
			}
		}
	}
	g, err := csr.BuildE(src, dst, w, csr.Options{})
	require.NoError(t, err)
	r, err := HDBSCAN(context.Background(), g, nil, HDBSCANOptions{MinClusterSize: 4})
	require.NoError(t, err)
	require.Equal(t, 2, r.NumClusters)
	require.Len(t, r.Heights, 18)
	for i := range 10 {
		require.Equal(t, r.Label[0], r.Label[i])
		require.Equal(t, r.Label[10], r.Label[10+i])
	}
	require.NotEqual(t, r.Label[0], r.Label[10])
}
