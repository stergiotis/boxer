package rutter

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

// A small directed graph with a parallel arc and a self-loop:
//
//	0 →(e0, 5)→ 1 →(e1, 2)→ 2
//	0 →(e2, 9)→ 2      1 →(e3, 1)→ 2 (parallel to e1, cheaper)
//	2 →(e4, 1)→ 3      3 →(e5)→ 3 (loop, dropped)
//	1 →(e6, Inf)→ 3 (present, forbidden)
func smallGraph(t *testing.T) (g *Graph, w Metric) {
	t.Helper()
	tail := []int32{0, 1, 0, 1, 2, 3, 1}
	head := []int32{1, 2, 2, 2, 3, 3, 3}
	edge := []int32{0, 1, 2, 3, 4, 5, 6}
	g, err := BuildE(4, tail, head, edge)
	require.NoError(t, err)
	w = g.MetricFromInput([]uint32{5, 2, 9, 1, 1, 7, Inf})
	return
}

func TestBuildKeepsParallelArcsAndDropsLoops(t *testing.T) {
	g, w := smallGraph(t)
	require.EqualValues(t, 4, g.NumNodes())
	require.EqualValues(t, 6, g.NumArcs())
	require.EqualValues(t, -1, g.InputArc(5))
	first, last := g.Out(1)
	require.EqualValues(t, 3, last-first)
	heads := []int32{}
	edges := []int32{}
	for a := first; a < last; a++ {
		heads = append(heads, g.Head(a))
		edges = append(edges, g.Edge(a))
		require.EqualValues(t, 1, g.Tail(a))
	}
	require.Equal(t, []int32{2, 2, 3}, heads)
	require.Equal(t, []int32{1, 3, 6}, edges)
	require.Equal(t, []uint32{2, 1, Inf}, []uint32(w[first:last]))
	// Reverse row of 2: arcs from 0, 1, 1 — tails ascending.
	rf, rl := g.In(2)
	tails := []int32{}
	for p := rf; p < rl; p++ {
		tails = append(tails, g.InTail(p))
		require.EqualValues(t, 2, g.Head(g.InArc(p)))
	}
	require.Equal(t, []int32{0, 1, 1}, tails)
}

func TestBuildRefusesSlotsOutsideTheGraph(t *testing.T) {
	_, err := BuildE(2, []int32{0}, []int32{2}, []int32{0})
	require.Error(t, err)
	_, err = BuildE(2, []int32{0, 1}, []int32{1}, []int32{0, 1})
	require.Error(t, err)
}

func TestFromRowsRoundTrips(t *testing.T) {
	g, _ := smallGraph(t)
	h, err := FromRowsE(g.FirstOut(), g.Heads(), g.Edges())
	require.NoError(t, err)
	require.Equal(t, g.NumArcs(), h.NumArcs())
	for v := range g.NumNodes() {
		gf, gl := g.In(v)
		hf, hl := h.In(v)
		require.Equal(t, g.inArc[gf:gl], h.inArc[hf:hl])
	}
	_, err = FromRowsE([]int32{0, 2}, []int32{0}, []int32{0})
	require.Error(t, err)
}

func TestHeapOrdersAndDecreasesKeys(t *testing.T) {
	h := newHeap4(8)
	h.push(3, 30)
	h.push(1, 10)
	h.push(5, 50)
	h.push(7, 70)
	h.push(5, 5)  // decrease
	h.push(1, 99) // ignored: larger
	v, k := h.pop()
	require.EqualValues(t, 5, v)
	require.EqualValues(t, 5, k)
	v, k = h.pop()
	require.EqualValues(t, 1, v)
	require.EqualValues(t, 10, k)
	require.Equal(t, 2, h.len())
	h.reset()
	require.Equal(t, 0, h.len())
	require.EqualValues(t, -1, h.pos[3])
}

func TestDijkstraSmallGraph(t *testing.T) {
	g, w := smallGraph(t)
	d := NewDijkstra(g)
	dist, ok := d.OneToOne(w, []Source{{Node: 0}}, 3)
	require.True(t, ok)
	require.EqualValues(t, 7, dist) // 0→1 (5), 1→2 (1), 2→3 (1)
	path := d.Path(3, nil)
	require.Len(t, path, 3)
	require.Equal(t, []int32{0, 3, 4}, []int32{g.Edge(path[0]), g.Edge(path[1]), g.Edge(path[2])})
	_, ok = d.OneToOne(w, []Source{{Node: 3}}, 0)
	require.False(t, ok)
	// One-to-all under a cut-off settles 0, 1 and 2 (7 is past 6).
	settled := map[int32]uint32{}
	for v, dv := range d.OneToAll(w, []Source{{Node: 0}}, 6) {
		settled[v] = dv
	}
	require.Equal(t, map[int32]uint32{0: 0, 1: 5, 2: 6}, settled)
	require.Equal(t, []int32{0, 1, 2}, d.Settled())
	out := make([]uint32, 3)
	d.OneToMany(w, []Source{{Node: 0}}, []int32{3, 2, 0}, out)
	require.Equal(t, []uint32{7, 6, 0}, out)
	// Two sources with offsets: a point snapped mid-arc.
	dist, ok = d.OneToOne(w, []Source{{Node: 0, Dist: 100}, {Node: 2, Dist: 4}}, 3)
	require.True(t, ok)
	require.EqualValues(t, 5, dist)
}

// randomGraph is a sparse directed graph with parallel arcs and a few
// forbidden ones, for the oracle comparisons.
func randomGraph(t *testing.T, rng *rand.Rand, n, m int) (g *Graph, w Metric) {
	t.Helper()
	tail := make([]int32, m)
	head := make([]int32, m)
	edge := make([]int32, m)
	in := make([]uint32, m)
	for i := range m {
		tail[i] = int32(rng.IntN(n))
		head[i] = int32(rng.IntN(n))
		edge[i] = int32(i)
		in[i] = uint32(1 + rng.IntN(100))
		if rng.IntN(20) == 0 {
			in[i] = Inf
		}
	}
	g, err := BuildE(int32(n), tail, head, edge)
	require.NoError(t, err)
	return g, g.MetricFromInput(in)
}

// bruteForce is Bellman–Ford from s, the reference for Dijkstra.
func bruteForce(g *Graph, w Metric, s int32) (dist []uint32) {
	n := int(g.NumNodes())
	dist = make([]uint32, n)
	for i := range dist {
		dist[i] = Inf
	}
	dist[s] = 0
	for range n {
		changed := false
		for v := range g.NumNodes() {
			if dist[v] == Inf {
				continue
			}
			first, last := g.Out(v)
			for a := first; a < last; a++ {
				nd := addSat(dist[v], w[a])
				if nd < dist[g.Head(a)] {
					dist[g.Head(a)] = nd
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	return
}

func TestDijkstraAgainstBellmanFord(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for round := range 20 {
		n := 5 + rng.IntN(60)
		g, w := randomGraph(t, rng, n, n*3)
		d := NewDijkstra(g)
		s := int32(rng.IntN(n))
		want := bruteForce(g, w, s)
		for v, dv := range d.OneToAll(w, []Source{{Node: s}}, Inf-1) {
			require.Equal(t, want[v], dv, "round %d node %d", round, v)
		}
		for v := range g.NumNodes() {
			require.Equal(t, want[v], d.Dist(v), "round %d node %d", round, v)
			// The tree path is a walk whose weights sum to the label.
			path := d.Path(v, nil)
			sum := uint32(0)
			at := s
			for _, a := range path {
				require.Equal(t, at, g.Tail(a))
				at = g.Head(a)
				sum = addSat(sum, w[a])
			}
			if want[v] != Inf {
				require.Equal(t, v, at)
				require.Equal(t, want[v], sum)
			}
		}
	}
}

func TestIndexAgainstLinearScan(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	lines := Polylines{First: []int32{0}}
	for range 200 {
		k := 2 + rng.IntN(4)
		x, y := rng.Float64()*1000, rng.Float64()*1000
		for range k {
			lines.X = append(lines.X, float32(x))
			lines.Y = append(lines.Y, float32(y))
			x += rng.Float64()*40 - 20
			y += rng.Float64()*40 - 20
		}
		lines.First = append(lines.First, int32(len(lines.X)))
	}
	idx, err := NewIndexE(lines, 50)
	require.NoError(t, err)
	for range 300 {
		qx, qy := rng.Float64()*1100-50, rng.Float64()*1100-50
		radius := 30 + rng.Float64()*100
		bestD := math.Inf(1)
		bestI := int32(-1)
		for i := range int32(lines.NumPolylines()) {
			m := idx.measure(i, qx, qy)
			if m.Dist < bestD {
				bestD, bestI = m.Dist, i
			}
		}
		s, ok := idx.Nearest(qx, qy, radius)
		if bestD > radius {
			require.False(t, ok)
			continue
		}
		require.True(t, ok)
		require.Equal(t, bestI, s.Polyline)
		require.InDelta(t, bestD, s.Dist, 1e-9)
		require.GreaterOrEqual(t, s.Fraction, 0.0)
		require.LessOrEqual(t, s.Fraction, 1.0)
	}
	// The closest point on a two-vertex line is the projection, and the
	// fraction is where along it.
	one := Polylines{First: []int32{0, 2}, X: []float32{0, 10}, Y: []float32{0, 0}}
	idx, err = NewIndexE(one, 5)
	require.NoError(t, err)
	s, ok := idx.Nearest(2.5, 3, 10)
	require.True(t, ok)
	require.InDelta(t, 2.5, s.X, 1e-12)
	require.InDelta(t, 0, s.Y, 1e-12)
	require.InDelta(t, 3, s.Dist, 1e-12)
	require.InDelta(t, 0.25, s.Fraction, 1e-12)
	require.InDelta(t, 10, one.Length(0), 1e-12)
	// A filter skips the nearest when it is not admitted.
	two := Polylines{First: []int32{0, 2, 4}, X: []float32{0, 10, 0, 10}, Y: []float32{0, 0, 5, 5}}
	idx, err = NewIndexE(two, 5)
	require.NoError(t, err)
	s, ok = idx.NearestWhere(5, 1, 10, func(i int32) bool { return i == 1 })
	require.True(t, ok)
	require.EqualValues(t, 1, s.Polyline)
	require.InDelta(t, 4, s.Dist, 1e-6)
	_, err = NewIndexE(Polylines{First: []int32{0, 0}}, 5)
	require.Error(t, err)
}
