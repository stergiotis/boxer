package algo

import (
	"context"
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// The metric vocabulary of ADR-0232 §SD6 and the seeded rank of §SD7.

func TestMetricNamesRoundTrip(t *testing.T) {
	for _, m := range Metrics() {
		require.True(t, m.IsValid(), "Metrics() yielded an invalid value")
		s := m.String()
		require.NotEmpty(t, s, "metric %d has no spelling", uint8(m))
		got, ok := ParseMetric(s)
		require.True(t, ok, "%q does not parse", s)
		require.Equal(t, m, got)
	}
	require.Empty(t, MetricNone.String())
	require.False(t, MetricNone.IsValid())
	require.Equal(t, KindNone, MetricNone.Kind())
	for _, s := range []string{"", "Degree", "page_rank", "nope", " degree"} {
		m, ok := ParseMetric(s)
		require.False(t, ok, "%q parsed", s)
		require.Equal(t, MetricNone, m)
	}
}

// TestMetricNamesAreUnique guards the one table String and ParseMetric share:
// two metrics with one spelling would make the parser's answer arbitrary.
func TestMetricNamesAreUnique(t *testing.T) {
	seen := make(map[string]MetricE, len(metricNames))
	for _, m := range Metrics() {
		s := m.String()
		prev, dup := seen[s]
		require.False(t, dup, "%q names both %d and %d", s, uint8(prev), uint8(m))
		seen[s] = m
	}
}

func TestMetricKindAndSymmetric(t *testing.T) {
	for _, tc := range []struct {
		m    MetricE
		kind KindE
		sym  MetricE
	}{
		{MetricDegree, KindOrdinal, MetricDegree},
		{MetricInDegree, KindOrdinal, MetricDegree},
		{MetricOutDegree, KindOrdinal, MetricDegree},
		{MetricPageRank, KindOrdinal, MetricPageRank},
		{MetricBetweenness, KindOrdinal, MetricBetweenness},
		{MetricKCore, KindOrdinal, MetricKCore},
		{MetricTriangles, KindOrdinal, MetricTriangles},
		{MetricClustering, KindOrdinal, MetricClustering},
		{MetricClique, KindOrdinal, MetricClique},
		{MetricComponent, KindCategorical, MetricComponent},
		{MetricSCC, KindCategorical, MetricComponent},
		{MetricComponentSize, KindOrdinal, MetricComponentSize},
		{MetricDistance, KindOrdinal, MetricDistance},
		{MetricDistanceIn, KindOrdinal, MetricDistance},
		{MetricDistanceOut, KindOrdinal, MetricDistance},
		{MetricRelevance, KindOrdinal, MetricRelevance},
	} {
		require.Equal(t, tc.kind, tc.m.Kind(), "kind of %s", tc.m)
		require.Equal(t, tc.sym, tc.m.Symmetric(), "symmetric of %s", tc.m)
		// A symmetric form is its own symmetric form: mapping twice is
		// mapping once, so a consumer may apply it without tracking whether
		// it already has.
		require.Equal(t, tc.sym, tc.sym.Symmetric(), "symmetric of %s is not a fixed point", tc.sym)
	}
}

// TestEveryMetricIsDispatched is the completeness test the verification plan
// names: a value added to the enum without a case in Compute returns an empty
// column here, and one added to Compute without an enum entry is not in
// Metrics() and so is never offered.
func TestEveryMetricIsDispatched(t *testing.T) {
	g := rmatGraph(t, 8, 4, true)
	n := g.NumVertices()
	for _, m := range Metrics() {
		opts := ComputeOptions{}
		if m.IsSeeded() {
			opts.Sources = []int32{0}
		}
		c, err := Compute(context.Background(), nil, g, m, opts)
		require.NoError(t, err, "metric %s", m)
		require.Equal(t, m, c.Metric)
		require.Equal(t, m.Kind(), c.Kind)
		require.Len(t, c.Values, n, "metric %s returned no column", m)
	}
}

func TestComputeRefusesUnknownAndUnseeded(t *testing.T) {
	g := rmatGraph(t, 6, 4, true)
	_, err := Compute(context.Background(), nil, g, MetricNone, ComputeOptions{})
	require.Error(t, err)
	_, err = Compute(context.Background(), nil, g, MetricE(200), ComputeOptions{})
	require.Error(t, err)
	for _, m := range Metrics() {
		if !m.IsSeeded() {
			continue
		}
		// An empty source set is an error rather than an empty column: a
		// consumer must be able to tell "nothing selected" from "nothing
		// near the selection".
		_, err = Compute(context.Background(), nil, g, m, ComputeOptions{})
		require.Error(t, err, "metric %s accepted an empty source set", m)
	}
}

// TestComputeMatchesTheDirectCall is ADR-0232 §SD6's "bit-identical to
// calling them", at one worker and at many.
func TestComputeMatchesTheDirectCall(t *testing.T) {
	ctx := context.Background()
	for _, directed := range []bool{false, true} {
		g := rmatGraph(t, 10, 8, directed)
		for _, workers := range []int{1, 4} {
			e := engine.New(workers)
			src := []int32{0, 3, 7}
			opts := ComputeOptions{Sources: src}

			out, in := Degrees(g)
			want := make([]float64, g.NumVertices())
			for v := range g.NumVertices() {
				if directed {
					want[v] = float64(out[v]) + float64(in[v])
				} else {
					want[v] = float64(out[v])
				}
			}
			requireColumn(t, ctx, e, g, MetricDegree, opts, want)
			requireColumn(t, ctx, e, g, MetricInDegree, opts, int32Column(in))
			requireColumn(t, ctx, e, g, MetricOutDegree, opts, int32Column(out))

			requireColumn(t, ctx, e, g, MetricPageRank, opts, PageRank(ctx, e, g, PageRankOptions{}).Rank)
			requireColumn(t, ctx, e, g, MetricKCore, opts, int32Column(KCore(ctx, g).Core))
			requireColumn(t, ctx, e, g, MetricComponent, opts, int32Column(ConnectedComponents(ctx, g).Comp))
			requireColumn(t, ctx, e, g, MetricSCC, opts, int32Column(StronglyConnectedComponents(ctx, g).Comp))
			requireColumn(t, ctx, e, g, MetricComponentSize, opts,
				int32Column(ComponentSizes(ConnectedComponents(ctx, g).Comp)))

			tri := Triangles(ctx, e, g, TriangleOptions{PerVertex: true})
			requireColumn(t, ctx, e, g, MetricTriangles, opts, uint32Column(tri.PerVertex))
			requireColumn(t, ctx, e, g, MetricClustering, opts, LocalClustering(g, tri.PerVertex))

			bc, err := Betweenness(ctx, e, g, BetweennessOptions{})
			require.NoError(t, err)
			requireColumn(t, ctx, e, g, MetricBetweenness, opts, bc.Score)

			rel := PageRank(ctx, e, g, PageRankOptions{Teleport: src})
			requireColumn(t, ctx, e, g, MetricRelevance, opts, rel.Rank)

			for _, m := range []MetricE{MetricDistance, MetricDistanceIn, MetricDistanceOut} {
				r, errB := BFS(ctx, e, g, src, BFSOptions{Direction: bfsDirection(m)})
				require.NoError(t, errB)
				want := make([]float64, len(r.Depth))
				for v, d := range r.Depth {
					if d < 0 {
						want[v] = math.NaN()
					} else {
						want[v] = float64(d)
					}
				}
				requireColumn(t, ctx, e, g, m, opts, want)
			}
		}
	}
}

// requireColumn compares a computed column with the direct call's values
// exactly, NaN counting as equal to NaN so the absent entries compare.
func requireColumn(t *testing.T, ctx context.Context, e *engine.Engine, g *csr.Graph,
	m MetricE, opts ComputeOptions, want []float64) {
	t.Helper()
	c, err := Compute(ctx, e, g, m, opts)
	require.NoError(t, err, "metric %s", m)
	require.Len(t, c.Values, len(want), "metric %s", m)
	for i := range want {
		if math.IsNaN(want[i]) {
			require.True(t, math.IsNaN(c.Values[i]), "metric %s slot %d: want NaN, got %v", m, i, c.Values[i])
			continue
		}
		require.Equal(t, want[i], c.Values[i], "metric %s slot %d", m, i)
	}
}

// TestDistanceLeavesUnreachedAbsent pins §SD6's reading: an unreached vertex
// is NaN, not zero, which a ramp would otherwise place beside the sources.
func TestDistanceLeavesUnreachedAbsent(t *testing.T) {
	// Two components: 0→1 and 2→3.
	g, err := csr.BuildE([]uint64{0, 2}, []uint64{1, 3}, nil, csr.Options{Directed: true})
	require.NoError(t, err)
	c, err := Compute(context.Background(), nil, g, MetricDistance, ComputeOptions{Sources: []int32{0}})
	require.NoError(t, err)
	require.Equal(t, float64(0), c.Values[0])
	require.Equal(t, float64(1), c.Values[1])
	require.True(t, math.IsNaN(c.Values[2]), "unreached slot is not absent")
	require.True(t, math.IsNaN(c.Values[3]), "unreached slot is not absent")
}

func TestLocalClusteringMatchesBruteForce(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		directed := rapid.Bool().Draw(t, "directed")
		g, _, _ := drawGraph(t, directed)
		a := adjOf(g).sym()
		tri := Triangles(context.Background(), nil, g, TriangleOptions{PerVertex: true})
		got := LocalClustering(g, tri.PerVertex)
		require.Len(t, got, a.n)
		for v := range a.n {
			// Neighbours of v in the symmetric, loop-free reading.
			var nbr []int
			for w := range a.n {
				if w != v && a.out[v][w] {
					nbr = append(nbr, w)
				}
			}
			d := len(nbr)
			if d < 2 {
				require.True(t, math.IsNaN(got[v]), "slot %d of degree %d is not absent", v, d)
				continue
			}
			var links int
			for i := range nbr {
				for j := i + 1; j < d; j++ {
					if a.out[nbr[i]][nbr[j]] {
						links++
					}
				}
			}
			want := 2 * float64(links) / (float64(d) * float64(d-1))
			require.InDelta(t, want, got[v], 1e-12, "slot %d", v)
		}
	})
}

func TestCliqueSizesMatchOracle(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		directed := rapid.Bool().Draw(t, "directed")
		g, _, _ := drawGraph(t, directed)
		a := adjOf(g).sym()
		r := MaximalCliques(context.Background(), g, CliqueOptions{})
		require.False(t, r.Truncated)
		got := CliqueSizes(g, r)
		// The oracle enumerates every maximal clique as a bitmask; the
		// largest one containing v is v's score.
		want := make([]int32, a.n)
		for mask := range a.cliquesOracle() {
			size := int32(0)
			for v := range a.n {
				if mask&(1<<v) != 0 {
					size++
				}
			}
			for v := range a.n {
				if mask&(1<<v) != 0 && size > want[v] {
					want[v] = size
				}
			}
		}
		require.Equal(t, want, got)
	})
}

func TestComponentSizesCountEachLabel(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g, _, _ := drawGraph(t, rapid.Bool().Draw(t, "directed"))
		comp := ConnectedComponents(context.Background(), g).Comp
		got := ComponentSizes(comp)
		require.Len(t, got, len(comp))
		for i, c := range comp {
			var want int32
			for _, o := range comp {
				if o == c {
					want++
				}
			}
			require.Equal(t, want, got[i], "slot %d", i)
		}
	})
	require.Nil(t, ComponentSizes(nil))
}

// personalisedOracle is pageRankOracle with a teleport distribution: the
// restart mass and the dangling mass both land on the seed set.
func (a adj) personalisedOracle(damping float64, iters int, seeds []int32) []float64 {
	n := a.n
	tp := make([]float64, n)
	for _, s := range seeds {
		tp[s] = 1
	}
	var count float64
	for _, v := range tp {
		if v != 0 {
			count++
		}
	}
	for i, v := range tp {
		if v != 0 {
			tp[i] = 1 / count
		}
	}
	rank := make([]float64, n)
	copy(rank, tp)
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
			next[d] = (1-damping)*tp[d] + damping*dangling*tp[d] + damping*s
		}
		rank = next
	}
	return rank
}

func TestSeededPageRankMatchesOracle(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g, _, _ := drawGraph(t, true)
		n := g.NumVertices()
		seeds := []int32{int32(rapid.IntRange(0, n-1).Draw(t, "seed"))}
		const iters = 60
		got := PageRank(context.Background(), nil, g, PageRankOptions{Iterations: iters, Teleport: seeds})
		want := adjOf(g).personalisedOracle(0.85, iters, seeds)
		require.True(t, approxEqual(want, got.Rank, 1e-9), "want %v got %v", want, got.Rank)
	})
}

// TestSeedingEveryVertexIsUniform is §SD7's stated identity: a teleport set of
// every slot is uniform teleportation written the long way.
func TestSeedingEveryVertexIsUniform(t *testing.T) {
	g := rmatGraph(t, 9, 6, true)
	n := g.NumVertices()
	all := make([]int32, n)
	for i := range all {
		all[i] = int32(i)
	}
	const iters = 40
	plain := PageRank(context.Background(), nil, g, PageRankOptions{Iterations: iters})
	seeded := PageRank(context.Background(), nil, g, PageRankOptions{Iterations: iters, Teleport: all})
	require.True(t, approxEqual(plain.Rank, seeded.Rank, 1e-12))
}

// TestTeleportOutOfRangeFallsBackToUniform pins the defensive reading the
// option documents: PageRank returns no error, so a set that names nothing
// in range is the uniform rank rather than a blank one.
func TestTeleportOutOfRangeFallsBackToUniform(t *testing.T) {
	g := rmatGraph(t, 8, 4, true)
	n := int32(g.NumVertices())
	plain := PageRank(context.Background(), nil, g, PageRankOptions{})
	for _, tp := range [][]int32{{-1}, {n}, {-5, n + 3}} {
		got := PageRank(context.Background(), nil, g, PageRankOptions{Teleport: tp})
		require.True(t, approxEqual(plain.Rank, got.Rank, 0), "teleport %v did not fall back", tp)
	}
	// A duplicated slot counts once, so it is the same as naming it once.
	one := PageRank(context.Background(), nil, g, PageRankOptions{Teleport: []int32{2}})
	dup := PageRank(context.Background(), nil, g, PageRankOptions{Teleport: []int32{2, 2, 2}})
	require.True(t, approxEqual(one.Rank, dup.Rank, 0))
}

// TestSeededRankConcentratesOnTheSeed is the reading `relevance` is spent on:
// the seed outranks a vertex it cannot reach, which the unseeded rank need
// not do.
func TestSeededRankConcentratesOnTheSeed(t *testing.T) {
	// 0→1→2, and an isolated 3.
	g, err := csr.BuildE([]uint64{0, 1, 3}, []uint64{1, 2, 3}, nil, csr.Options{Directed: true})
	require.NoError(t, err)
	c, err := Compute(context.Background(), nil, g, MetricRelevance,
		ComputeOptions{Sources: []int32{0}, PageRank: PageRankOptions{Iterations: 50}})
	require.NoError(t, err)
	require.Greater(t, c.Values[0], c.Values[1], "the seed does not outrank its successor")
	require.Greater(t, c.Values[1], c.Values[2], "relevance does not decay with distance")
	require.Greater(t, c.Values[2], c.Values[3], "an unreachable vertex is not the least relevant")
}
