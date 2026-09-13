package algo

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
	"github.com/stretchr/testify/require"
	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/network"
	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"
	"pgregory.net/rapid"
)

// drawGraph generates a small graph; ids are drawn sparse so slots and ids
// differ.
func drawGraph(t *rapid.T, directed bool) (*csr.Graph, []uint64, []uint64) {
	n := rapid.IntRange(1, 9).Draw(t, "n")
	m := rapid.IntRange(1, 3*n).Draw(t, "m")
	src := make([]uint64, m)
	dst := make([]uint64, m)
	for i := range m {
		src[i] = uint64(rapid.IntRange(0, n-1).Draw(t, "s")) * 7
		dst[i] = uint64(rapid.IntRange(0, n-1).Draw(t, "d")) * 7
	}
	g, err := csr.BuildE(src, dst, nil, csr.Options{Directed: directed})
	require.NoError(t, err)
	return g, src, dst
}

func gonumUndirected(g *csr.Graph) *simple.UndirectedGraph {
	u := simple.NewUndirectedGraph()
	for v := range g.NumVertices() {
		u.AddNode(simple.Node(v))
	}
	for v := range g.NumVertices() {
		for _, w := range g.Out(int32(v)) {
			if int32(v) < w || (g.IsDirected() && int32(v) != w) {
				u.SetEdge(u.NewEdge(simple.Node(v), simple.Node(w)))
			}
		}
	}
	return u
}

func gonumDirected(g *csr.Graph) *simple.DirectedGraph {
	d := simple.NewDirectedGraph()
	for v := range g.NumVertices() {
		d.AddNode(simple.Node(v))
	}
	for v := range g.NumVertices() {
		for _, w := range g.Out(int32(v)) {
			if int32(v) != w {
				d.SetEdge(d.NewEdge(simple.Node(v), simple.Node(w)))
			}
		}
	}
	return d
}

func TestBFSMatchesOracle(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		directed := rapid.Bool().Draw(t, "directed")
		g, _, _ := drawGraph(t, directed)
		a := adjOf(g)
		s := int32(rapid.IntRange(0, g.NumVertices()-1).Draw(t, "src"))
		frac := rapid.SampledFrom([]float64{0.0001, engine.DefaultDenseFraction, 2}).Draw(t, "frac")
		r, err := BFS(context.Background(), engine.New(2), g, []int32{s}, BFSOptions{DenseFraction: frac})
		require.NoError(t, err)
		dist, _ := a.bfsOracle(int(s))
		for v := range g.NumVertices() {
			require.EqualValues(t, dist[v], r.Depth[v], "vertex %d", v)
			if dist[v] > 0 {
				p := r.Parent[v]
				require.EqualValues(t, dist[v]-1, r.Depth[p])
				require.True(t, a.out[p][v])
			}
		}
		require.False(t, r.Truncated)
		rIn, _ := BFS(context.Background(), engine.New(1), g, []int32{s}, BFSOptions{Direction: engine.DirectionIn})
		distIn, _ := a.transpose().bfsOracle(int(s))
		for v := range g.NumVertices() {
			require.EqualValues(t, distIn[v], rIn.Depth[v])
		}
		rBoth, _ := BFS(context.Background(), engine.New(3), g, []int32{s}, BFSOptions{Direction: engine.DirectionBoth})
		distBoth, _ := a.sym().bfsOracle(int(s))
		for v := range g.NumVertices() {
			require.EqualValues(t, distBoth[v], rBoth.Depth[v])
		}
	})
}

func TestBFSMaxDepth(t *testing.T) {
	g, _ := csr.BuildE([]uint64{0, 1, 2}, []uint64{1, 2, 3}, nil, csr.Options{Directed: true})
	r, err := BFS(context.Background(), nil, g, []int32{0}, BFSOptions{MaxDepth: 2})
	require.NoError(t, err)
	require.Equal(t, []int32{0, 1, 2, -1}, r.Depth)
	_, err = BFS(context.Background(), nil, g, []int32{9}, BFSOptions{})
	require.Error(t, err)
}

func TestComponentsMatchOracleAndGonum(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		directed := rapid.Bool().Draw(t, "directed")
		g, _, _ := drawGraph(t, directed)
		a := adjOf(g)
		cc := ConnectedComponents(context.Background(), g)
		reach := a.sym().reach()
		for v := range a.n {
			require.LessOrEqual(t, cc.Comp[v], int32(v))
			for w := range a.n {
				require.Equal(t, reach[v][w], cc.Comp[v] == cc.Comp[w], "cc %d %d", v, w)
			}
		}
		gc := topo.ConnectedComponents(gonumUndirected(g))
		require.Equal(t, len(gc), cc.Count)

		scc := StronglyConnectedComponents(context.Background(), g)
		r := a.reach()
		for v := range a.n {
			for w := range a.n {
				require.Equal(t, r[v][w] && r[w][v], scc.Comp[v] == scc.Comp[w], "scc %d %d", v, w)
			}
		}
		if directed {
			gs := topo.TarjanSCC(gonumDirected(g))
			require.Equal(t, len(gs), scc.Count)
		}
		// A sink component completes first: no arc leaves component 0.
		for v := range a.n {
			if scc.Comp[v] != 0 {
				continue
			}
			for w := range a.n {
				if a.out[v][w] {
					require.EqualValues(t, 0, scc.Comp[w])
				}
			}
		}
	})
}

func TestPageRankMatchesOracle(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		directed := rapid.Bool().Draw(t, "directed")
		g, _, _ := drawGraph(t, directed)
		a := adjOf(g)
		iters := rapid.IntRange(1, 30).Draw(t, "iters")
		r := PageRank(context.Background(), engine.New(3), g, PageRankOptions{Iterations: iters})
		require.Equal(t, iters, r.Iterations)
		require.True(t, approxEqual(r.Rank, a.pageRankOracle(0.85, iters), 1e-9))
		var sum float64
		for _, x := range r.Rank {
			sum += x
		}
		require.InDelta(t, 1, sum, 1e-9)
	})
}

func TestPageRankConvergenceAndBudget(t *testing.T) {
	g, _ := csr.BuildE([]uint64{0, 1, 2, 3, 0}, []uint64{1, 2, 3, 0, 2}, nil, csr.Options{Directed: true})
	r := PageRank(context.Background(), nil, g, PageRankOptions{Tolerance: 1e-12, Iterations: 100})
	require.True(t, r.Converged)
	require.False(t, r.Truncated)
	pr := network.PageRank(gonumDirected(g), 0.85, 1e-12)
	for v := range 4 {
		require.InDelta(t, pr[int64(v)], r.Rank[v], 1e-6)
	}
	cut := PageRank(context.Background(), nil, g, PageRankOptions{Tolerance: 1e-300, Iterations: 3})
	require.False(t, cut.Converged)
	require.Equal(t, LimitIterations, cut.By)
	require.Equal(t, 3, cut.Iterations)
}

func TestKCoreMatchesOracle(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		directed := rapid.Bool().Draw(t, "directed")
		g, _, _ := drawGraph(t, directed)
		a := adjOf(g).sym()
		r := KCore(context.Background(), g)
		want := a.corenessOracle()
		require.Equal(t, want, r.Core)
		require.Len(t, r.Order, a.n)
		// Degeneracy ordering: every vertex has at most Degeneracy neighbours
		// later in the order.
		rank := make([]int, a.n)
		for i, v := range r.Order {
			rank[v] = i
		}
		for v := range a.n {
			later := 0
			for w := range a.n {
				if w != v && a.out[v][w] && rank[w] > rank[v] {
					later++
				}
			}
			require.LessOrEqual(t, later, int(r.Degeneracy))
		}
	})
}

func TestTrianglesMatchOracle(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		directed := rapid.Bool().Draw(t, "directed")
		g, _, _ := drawGraph(t, directed)
		a := adjOf(g).sym()
		total, per := a.trianglesOracle()
		r := Triangles(context.Background(), engine.New(2), g, TriangleOptions{PerVertex: true})
		require.Equal(t, total, r.Total)
		require.Equal(t, per, r.PerVertex)
		r2 := Triangles(context.Background(), engine.New(2), g, TriangleOptions{})
		require.Equal(t, total, r2.Total)
		require.Nil(t, r2.PerVertex)
	})
}

func TestBetweennessMatchesOracleAndGonum(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		directed := rapid.Bool().Draw(t, "directed")
		g, _, _ := drawGraph(t, directed)
		a := adjOf(g)
		r, err := Betweenness(context.Background(), engine.New(3), g, BetweennessOptions{})
		require.NoError(t, err)
		require.False(t, r.Sampled)
		require.Equal(t, a.n, r.Sources)
		require.True(t, approxEqual(r.Score, a.betweennessOracle(), 1e-9), "%v vs %v", r.Score, a.betweennessOracle())
		var gb map[int64]float64
		if directed {
			gb = network.Betweenness(gonumDirected(g))
		} else {
			gb = network.Betweenness(gonumUndirected(g))
		}
		for v := range a.n {
			require.InDelta(t, gb[int64(v)], r.Score[v], 1e-9, "vertex %d", v)
		}
		rb, err := Betweenness(context.Background(), engine.New(2), g, BetweennessOptions{Direction: engine.DirectionBoth})
		require.NoError(t, err)
		require.True(t, approxEqual(rb.Score, a.sym().betweennessOracle(), 1e-9))
	})
}

func TestBetweennessSampling(t *testing.T) {
	src := make([]uint64, 0, 400)
	dst := make([]uint64, 0, 400)
	for i := uint64(0); i < 200; i++ {
		src = append(src, i, i)
		dst = append(dst, (i+1)%200, (i*7+3)%200)
	}
	g, _ := csr.BuildE(src, dst, nil, csr.Options{})
	exact, _ := Betweenness(context.Background(), engine.New(4), g, BetweennessOptions{})
	est, err := Betweenness(context.Background(), engine.New(4), g, BetweennessOptions{ExactMaxVertices: 100, Pivots: 60, Seed: 5})
	require.NoError(t, err)
	require.True(t, est.Sampled)
	require.Equal(t, LimitPivots, est.By)
	require.Equal(t, 60, est.Sources)
	var se, ss float64
	for v := range g.NumVertices() {
		se += exact.Score[v]
		ss += est.Score[v]
	}
	// The estimate is unbiased; totals should agree within a loose band.
	require.InDelta(t, se, ss, 0.25*se)
	again, _ := Betweenness(context.Background(), engine.New(1), g, BetweennessOptions{ExactMaxVertices: 100, Pivots: 60, Seed: 5})
	require.Equal(t, est.Score, again.Score)
}

func cliqueSet(g *csr.Graph, r CliqueResult) map[uint32]bool {
	set := map[uint32]bool{}
	for i := range r.Count {
		var mask uint32
		for _, v := range r.Members[r.Start[i]:r.Start[i+1]] {
			mask |= 1 << v
		}
		set[mask] = true
	}
	return set
}

func gonumCliqueSet(cl [][]graph.Node) map[uint32]bool {
	set := map[uint32]bool{}
	for _, c := range cl {
		var mask uint32
		for _, nd := range c {
			mask |= 1 << nd.ID()
		}
		set[mask] = true
	}
	return set
}

func TestMaximalCliquesMatchOracleAndGonum(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		directed := rapid.Bool().Draw(t, "directed")
		g, _, _ := drawGraph(t, directed)
		a := adjOf(g).sym()
		r := MaximalCliques(context.Background(), g, CliqueOptions{})
		require.False(t, r.Truncated)
		require.Equal(t, a.cliquesOracle(), cliqueSet(g, r))
		require.Equal(t, gonumCliqueSet(topo.BronKerbosch(gonumUndirected(g))), cliqueSet(g, r))
		for i := range r.Count {
			m := r.Members[r.Start[i]:r.Start[i+1]]
			for j := 1; j < len(m); j++ {
				require.Less(t, m[j-1], m[j])
			}
		}
		if r.Count > 1 {
			capped := MaximalCliques(context.Background(), g, CliqueOptions{MaxCliques: r.Count - 1})
			require.True(t, capped.Truncated)
			require.Equal(t, LimitOutput, capped.By)
			require.Equal(t, r.Count-1, capped.Count)
			require.Equal(t, r.Members[:capped.Start[capped.Count]], capped.Members)
		}
		big := MaximalCliques(context.Background(), g, CliqueOptions{MinSize: 3})
		for i := range big.Count {
			require.GreaterOrEqual(t, big.Start[i+1]-big.Start[i], int32(3))
		}
	})
}

func TestContextCancellationTruncates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := make([]uint64, 0, 2000)
	dst := make([]uint64, 0, 2000)
	for i := uint64(0); i < 1000; i++ {
		src = append(src, i, i)
		dst = append(dst, (i+1)%1000, (i*13+5)%1000)
	}
	g, _ := csr.BuildE(src, dst, nil, csr.Options{})
	r, _ := BFS(ctx, nil, g, []int32{0}, BFSOptions{})
	require.Equal(t, LimitContext, r.By)
	pr := PageRank(ctx, nil, g, PageRankOptions{})
	require.Equal(t, LimitContext, pr.By)
	require.Equal(t, 0, pr.Iterations)
	bc, _ := Betweenness(ctx, nil, g, BetweennessOptions{})
	require.Equal(t, LimitContext, bc.By)
	require.Equal(t, 0, bc.Sources)
	cc := ConnectedComponents(ctx, g)
	require.Equal(t, LimitContext, cc.By)
	kc := KCore(ctx, g)
	require.Equal(t, LimitContext, kc.By)
	cl := MaximalCliques(ctx, g, CliqueOptions{})
	require.Equal(t, LimitContext, cl.By)
}

func TestDegrees(t *testing.T) {
	g, _ := csr.BuildE([]uint64{0, 0, 1}, []uint64{1, 2, 2}, nil, csr.Options{Directed: true})
	out, in := Degrees(g)
	require.Equal(t, []int32{2, 1, 0}, out)
	require.Equal(t, []int32{0, 1, 2}, in)
}

func TestConnectedComponentsTruncatedStillLabels(t *testing.T) {
	g, err := csr.BuildE([]uint64{1, 2, 4}, []uint64{2, 3, 5}, nil, csr.Options{})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := ConnectedComponents(ctx, g)
	require.True(t, r.Truncation.Truncated)
	require.Len(t, r.Comp, g.NumVertices(), "a truncated result still carries a label per slot")
	for _, c := range r.Comp {
		require.GreaterOrEqual(t, c, int32(0))
	}
}
