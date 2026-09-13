package knn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/graph/engine"
	"github.com/stretchr/testify/require"
)

// The oracle is umap-learn's construction as composed from umap-go's
// exported stages — brute-force k-NN, SmoothKNNDist,
// ComputeMembershipStrengths, FuzzySetUnion of the matrix and its transpose
// — captured once as golden fixtures under testdata (ADR-0230 §SD5), so the
// comparison is against the construction SD1 re-derives and the module is
// no longer a dependency. Each fixture carries its input matrix, so the test
// does not rest on the random source staying stable.
//
// Regenerating: add github.com/nozzle/umap-go to go.mod, restore
// fixture_gen_test.go from history at the commit that removed it, and run
// the package tests with KNN_WRITE_FIXTURES=1. The fixtures in the tree were
// written against umap-go v0.0.0-20260301204052-79bd84384eff.

type umapFixture struct {
	Metric            string       `json:"metric"`
	N                 int          `json:"n"`
	D                 int          `json:"d"`
	K                 int          `json:"k"`
	LocalConnectivity float64      `json:"local_connectivity"`
	X                 []float32    `json:"x"`
	Sigmas            []float64    `json:"sigmas"`
	Rhos              []float64    `json:"rhos"`
	Arcs              [][3]float64 `json:"arcs"` // (i, j, weight) with i < j
}

type fixtureCase struct {
	name   string
	metric MetricE
	lc     float64
	n, d   int
	k      int
	seed   uint64
}

var fixtureCases = []fixtureCase{
	{"euclidean", MetricEuclidean, 1, 300, 8, 10, 11},
	{"cosine", MetricCosine, 1, 300, 8, 10, 11},
	{"euclidean-lc1.5", MetricEuclidean, 1.5, 300, 8, 10, 11},
}

func TestBuildMatchesUMAPFixtures(t *testing.T) {
	for _, fc := range fixtureCases {
		t.Run(fc.name, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("testdata", "umap_"+fc.name+".json"))
			require.NoError(t, err)
			var fx umapFixture
			require.NoError(t, json.Unmarshal(b, &fx))
			require.Equal(t, fc.n*fc.d, len(fx.X))
			r, err := Build(context.Background(), engine.New(1), fx.X, fc.d, seqIDs(fc.n), Options{K: fc.k, Metric: fc.metric, LocalConnectivity: float32(fc.lc)})
			require.NoError(t, err)
			for i := range fc.n {
				require.InDelta(t, fx.Sigmas[i], float64(r.Sigmas[i]), 1e-3*fx.Sigmas[i]+1e-6, "sigma %d", i)
				require.InDelta(t, fx.Rhos[i], float64(r.Rhos[i]), 1e-4*fx.Rhos[i]+1e-6, "rho %d", i)
			}
			for _, arc := range fx.Arcs {
				i, j := int32(arc[0]), int32(arc[1])
				out := r.Graph.Out(i)
				w := r.Graph.OutWeights(i)
				found := false
				for b, s := range out {
					if s == j {
						require.InDelta(t, arc[2], float64(w[b]), 1e-3, "arc %d→%d", i, j)
						found = true
						break
					}
				}
				require.True(t, found, "arc %d→%d missing", i, j)
			}
			require.Equal(t, int64(len(fx.Arcs)), r.Graph.NumEdges())
		})
	}
}
