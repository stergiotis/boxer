package rutter

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

// gridGraph is a w×h lattice with two-way arcs and random planar jitter,
// the shape a road network has for the order to work on.
func gridGraph(t *testing.T, rng *rand.Rand, w, h int) (g *Graph, x, y []float64, metric Metric) {
	t.Helper()
	n := w * h
	x = make([]float64, n)
	y = make([]float64, n)
	for r := range h {
		for c := range w {
			v := r*w + c
			x[v] = float64(c) + rng.Float64()*0.3
			y[v] = float64(r) + rng.Float64()*0.3
		}
	}
	var tail, head, edge []int32
	var in []uint32
	e := int32(0)
	add := func(a, b int) {
		wgt := uint32(1 + rng.IntN(50))
		if rng.IntN(25) == 0 {
			wgt = Inf
		}
		tail = append(tail, int32(a), int32(b))
		head = append(head, int32(b), int32(a))
		edge = append(edge, e, e)
		in = append(in, wgt, wgt)
		e++
	}
	for r := range h {
		for c := range w {
			v := r*w + c
			if c+1 < w {
				add(v, v+1)
			}
			if r+1 < h {
				add(v, v+w)
			}
			// A few diagonals and a parallel arc now and then.
			if c+1 < w && r+1 < h && rng.IntN(4) == 0 {
				add(v, v+w+1)
			}
			if c+1 < w && rng.IntN(10) == 0 {
				add(v, v+1)
			}
		}
	}
	g, err := BuildE(int32(n), tail, head, edge)
	require.NoError(t, err)
	return g, x, y, g.MetricFromInput(in)
}

func requireOrderIsPermutation(t *testing.T, o Order) {
	t.Helper()
	seen := make([]bool, len(o.Rank))
	for v, r := range o.Rank {
		require.False(t, seen[r])
		seen[r] = true
		require.EqualValues(t, v, o.Node[r])
	}
}

func TestInertialFlowOrderIsAPermutationAndSeparates(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	g, x, y, _ := gridGraph(t, rng, 30, 20)
	o := InertialFlowOrder(g, x, y, OrderOptions{})
	requireOrderIsPermutation(t, o)
	c := NewCCH(g, o)
	// Every parent has a higher rank and is an upward neighbour.
	for v := range g.NumNodes() {
		p := c.Parent(v)
		if p < 0 {
			continue
		}
		require.Greater(t, o.Rank[p], o.Rank[v])
		require.GreaterOrEqual(t, c.upArc(v, p), int32(0))
	}
	// A nested-dissection order of a 600-node grid keeps the tree short
	// and the fill-in bounded; both are the order's quality rather than its
	// correctness, so the bounds are the identity order's, which a grid
	// makes a chain of the whole graph.
	rank := make([]int32, g.NumNodes())
	for i := range rank {
		rank[i] = int32(i)
	}
	identity := NewCCH(g, mustOrder(t, rank))
	require.EqualValues(t, g.NumNodes(), identity.Height())
	require.Less(t, c.Height(), identity.Height()/4)
	require.Less(t, c.NumUpArcs(), identity.NumUpArcs()/2)
	require.Equal(t, o, mustOrder(t, o.Rank))
}

func mustOrder(t *testing.T, rank []int32) Order {
	t.Helper()
	o, err := OrderFromRanksE(rank)
	require.NoError(t, err)
	return o
}

func TestOrderFromRanksRefusesNonPermutations(t *testing.T) {
	_, err := OrderFromRanksE([]int32{0, 0})
	require.Error(t, err)
	_, err = OrderFromRanksE([]int32{0, 2})
	require.Error(t, err)
}

func TestDisconnectedGraphOrders(t *testing.T) {
	// Two islands and an isolated node.
	tail := []int32{0, 1, 3, 4}
	head := []int32{1, 2, 4, 5}
	edge := []int32{0, 1, 2, 3}
	g, err := BuildE(7, tail, head, edge)
	require.NoError(t, err)
	x := []float64{0, 1, 2, 10, 11, 12, 20}
	y := []float64{0, 0, 0, 0, 0, 0, 0}
	o := InertialFlowOrder(g, x, y, OrderOptions{LeafSize: 1})
	requireOrderIsPermutation(t, o)
	c := NewCCH(g, o)
	w := c.Customize(Metric{1, 1, 1, 1})
	q := NewCCHQuery(c)
	d, ok := q.Run(w, 0, 2)
	require.True(t, ok)
	require.EqualValues(t, 2, d)
	_, ok = q.Run(w, 0, 5)
	require.False(t, ok)
	_, ok = q.Run(w, 6, 0)
	require.False(t, ok)
}

// requireWalk checks arcs form a walk from s to t whose weights sum to d.
func requireWalk(t *testing.T, g *Graph, w Metric, arcs []int32, s, target int32, d uint32) {
	t.Helper()
	at := s
	sum := uint32(0)
	for _, a := range arcs {
		require.Equal(t, at, g.Tail(a))
		at = g.Head(a)
		sum = addSat(sum, w[a])
	}
	require.Equal(t, target, at)
	require.Equal(t, d, sum)
}

func TestCCHAgainstDijkstra(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	for round := range 6 {
		g, x, y, w := gridGraph(t, rng, 8+rng.IntN(30), 8+rng.IntN(30))
		o := InertialFlowOrder(g, x, y, OrderOptions{LeafSize: int32(1 + rng.IntN(40))})
		requireOrderIsPermutation(t, o)
		c := NewCCH(g, o)
		cw := c.Customize(w)
		q := NewCCHQuery(c)
		dij := NewDijkstra(g)
		n := int(g.NumNodes())
		for range 200 {
			s, tt := int32(rng.IntN(n)), int32(rng.IntN(n))
			want, wantOK := dij.OneToOne(w, []Source{{Node: s}}, tt)
			got, ok := q.Run(cw, s, tt)
			require.Equal(t, wantOK, ok, "round %d %d→%d", round, s, tt)
			require.Equal(t, want, got, "round %d %d→%d", round, s, tt)
			if ok {
				requireWalk(t, g, w, q.Path(cw, nil), s, tt, got)
			}
		}
		// A second metric over the same hierarchy.
		w2 := make(Metric, len(w))
		for i := range w2 {
			w2[i] = uint32(1 + rng.IntN(9))
		}
		cw2 := c.Customize(w2)
		for range 50 {
			s, tt := int32(rng.IntN(n)), int32(rng.IntN(n))
			want, _ := dij.OneToOne(w2, []Source{{Node: s}}, tt)
			got, _ := q.Run(cw2, s, tt)
			require.Equal(t, want, got)
		}
		// Snapped ends: two sources and two targets with offsets.
		for range 50 {
			src := []Source{{Node: int32(rng.IntN(n)), Dist: uint32(rng.IntN(30))}, {Node: int32(rng.IntN(n)), Dist: uint32(rng.IntN(30))}}
			dst := []Source{{Node: int32(rng.IntN(n)), Dist: uint32(rng.IntN(30))}, {Node: int32(rng.IntN(n)), Dist: uint32(rng.IntN(30))}}
			want := Inf
			for _, d := range dst {
				if dd, ok := dij.OneToOne(w, src, d.Node); ok {
					want = min(want, addSat(dd, d.Dist))
				}
			}
			got, _ := q.RunSources(cw, src, dst)
			require.Equal(t, want, got)
		}
	}
}

func TestManyToManyEqualsPairwise(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 10))
	g, x, y, w := gridGraph(t, rng, 25, 25)
	o := InertialFlowOrder(g, x, y, OrderOptions{})
	c := NewCCH(g, o)
	cw := c.Customize(w)
	q := NewCCHQuery(c)
	n := int(g.NumNodes())
	sources := make([]int32, 17)
	targets := make([]int32, 23)
	for i := range sources {
		sources[i] = int32(rng.IntN(n))
	}
	for i := range targets {
		targets[i] = int32(rng.IntN(n))
	}
	out := make([]uint32, len(sources)*len(targets))
	q.ManyToMany(cw, sources, targets, out)
	dij := NewDijkstra(g)
	for i, s := range sources {
		for j, tt := range targets {
			want, _ := dij.OneToOne(w, []Source{{Node: s}}, tt)
			require.Equal(t, want, out[i*len(targets)+j], "%d→%d", s, tt)
		}
	}
}
