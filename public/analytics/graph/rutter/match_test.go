package rutter

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

// lineGraph is a w×h lattice whose edges are straight polylines between
// node coordinates, two-way, with the polyline index and the length
// metric a matcher needs.
type lineGraph struct {
	g      *Graph
	x, y   []float64
	lines  Polylines
	index  *Index
	length Metric
	ends   [][2]int32 // per polyline: first and last node
	arcs   [][2]int32 // per polyline: forward and backward arc
}

func newLineGraph(t *testing.T, w, h int, spacing float64) (lg lineGraph) {
	t.Helper()
	n := w * h
	lg.x = make([]float64, n)
	lg.y = make([]float64, n)
	for r := range h {
		for c := range w {
			lg.x[r*w+c] = float64(c) * spacing
			lg.y[r*w+c] = float64(r) * spacing
		}
	}
	var tail, head, edge []int32
	var lengths []uint32
	lg.lines.First = []int32{0}
	add := func(a, b int) {
		e := int32(len(lg.ends))
		lg.ends = append(lg.ends, [2]int32{int32(a), int32(b)})
		l := uint32(math.Round(math.Hypot(lg.x[b]-lg.x[a], lg.y[b]-lg.y[a])))
		tail = append(tail, int32(a), int32(b))
		head = append(head, int32(b), int32(a))
		edge = append(edge, e, e)
		lengths = append(lengths, l, l)
		lg.lines.X = append(lg.lines.X, float32(lg.x[a]), float32(lg.x[b]))
		lg.lines.Y = append(lg.lines.Y, float32(lg.y[a]), float32(lg.y[b]))
		lg.lines.First = append(lg.lines.First, int32(len(lg.lines.X)))
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
		}
	}
	var err error
	lg.g, err = BuildE(int32(n), tail, head, edge)
	require.NoError(t, err)
	lg.length = lg.g.MetricFromInput(lengths)
	lg.index, err = NewIndexE(lg.lines, spacing)
	require.NoError(t, err)
	lg.arcs = make([][2]int32, len(lg.ends))
	for i := range lg.arcs {
		lg.arcs[i] = [2]int32{-1, -1}
	}
	for v := range lg.g.NumNodes() {
		first, last := lg.g.Out(v)
		for a := first; a < last; a++ {
			e := lg.g.Edge(a)
			if lg.ends[e][0] == v {
				lg.arcs[e][0] = a
			} else {
				lg.arcs[e][1] = a
			}
		}
	}
	return
}

func (lg *lineGraph) matcher(opts MatchOptions) *Matcher {
	return NewMatcher(lg.g, lg.index, lg.length,
		func(p int32) (int32, int32) { return lg.ends[p][0], lg.ends[p][1] },
		func(p int32) (int32, int32) { return lg.arcs[p][0], lg.arcs[p][1] },
		opts)
}

func TestCandidatesAreNearestFirstWithinRadius(t *testing.T) {
	lg := newLineGraph(t, 5, 5, 100)
	c := lg.index.Candidates(150, 3, 60, 4, nil, nil)
	require.NotEmpty(t, c)
	for i := 1; i < len(c); i++ {
		require.LessOrEqual(t, c[i-1].Dist, c[i].Dist)
	}
	require.InDelta(t, 3, c[0].Dist, 1e-6, "the bottom row's segment between x=100 and x=200")
	require.LessOrEqual(t, len(c), 4)
	for _, s := range c {
		require.LessOrEqual(t, s.Dist, 60.0)
	}
	require.Empty(t, lg.index.Candidates(150, 3, 60, 4, func(int32) bool { return false }, nil))
}

// A noisy walk along a known path over the lattice: the matcher must put
// nearly every observation on the polyline walked, and the arcs between
// consecutive matches must form a walk.
func TestMatchRecoversAWalk(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	lg := newLineGraph(t, 12, 12, 100)
	// The path: east along the bottom row, then north up a column.
	var truth []int32 // polyline per observation
	var ox, oy []float64
	observe := func(p int32, f float64) {
		a, b := lg.ends[p][0], lg.ends[p][1]
		x := lg.x[a] + f*(lg.x[b]-lg.x[a]) + rng.NormFloat64()*6
		y := lg.y[a] + f*(lg.y[b]-lg.y[a]) + rng.NormFloat64()*6
		ox, oy = append(ox, x), append(oy, y)
		truth = append(truth, p)
	}
	findLine := func(a, b int32) int32 {
		for i, e := range lg.ends {
			if (e[0] == a && e[1] == b) || (e[0] == b && e[1] == a) {
				return int32(i)
			}
		}
		t.Fatalf("no polyline %d-%d", a, b)
		return -1
	}
	for c := range 8 {
		p := findLine(int32(c), int32(c+1))
		for _, f := range []float64{0.2, 0.5, 0.8} {
			observe(p, f)
		}
	}
	for r := range 8 {
		p := findLine(int32(r*12+8), int32((r+1)*12+8))
		for _, f := range []float64{0.3, 0.7} {
			observe(p, f)
		}
	}
	m := lg.matcher(MatchOptions{Radius: 60, Sigma: 8, Beta: 25})
	got := m.Match(ox, oy, nil)
	require.Len(t, got, len(truth))
	right := 0
	for i := range got {
		require.GreaterOrEqual(t, got[i].Polyline, int32(0), "observation %d broke the sequence", i)
		if got[i].Polyline == truth[i] {
			right++
		}
	}
	require.GreaterOrEqual(t, right, len(truth)-2, "the walk's polylines")
	// Consecutive matches on different polylines are joined by arcs that
	// leave the first polyline's end and reach the next's; two polylines
	// that share a node need none.
	for i := 1; i < len(got); i++ {
		if got[i].Polyline == got[i-1].Polyline {
			require.Empty(t, got[i].Arcs)
			continue
		}
		prevEnds := lg.ends[got[i-1].Polyline]
		nextEnds := lg.ends[got[i].Polyline]
		if len(got[i].Arcs) == 0 {
			shared := false
			for _, a := range prevEnds {
				for _, b := range nextEnds {
					shared = shared || a == b
				}
			}
			require.True(t, shared, "observation %d: adjacent polylines without arcs must share a node", i)
			continue
		}
		at := lg.g.Tail(got[i].Arcs[0])
		require.Contains(t, prevEnds[:], at)
		for _, a := range got[i].Arcs {
			require.Equal(t, at, lg.g.Tail(a))
			at = lg.g.Head(a)
		}
		require.Contains(t, nextEnds[:], at)
	}
	// An observation far from everything breaks the sequence and the rest
	// matches again.
	ox2 := append(append([]float64{}, ox[:5]...), 5000)
	oy2 := append(append([]float64{}, oy[:5]...), 5000)
	ox2 = append(ox2, ox[5:]...)
	oy2 = append(oy2, oy[5:]...)
	got2 := m.Match(ox2, oy2, nil)
	require.EqualValues(t, -1, got2[5].Polyline)
	require.GreaterOrEqual(t, got2[6].Polyline, int32(0))
	require.Empty(t, got2[6].Arcs, "a fresh start carries no arcs")
	require.Empty(t, m.Match(nil, nil, nil))
}
