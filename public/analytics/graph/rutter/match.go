package rutter

import (
	"math"
)

// Candidates lists up to max polylines within radius of (x, y) that accept
// admits, nearest first. It is [Index.NearestWhere] keeping every hit
// rather than the best, for a matcher that weighs several.
func (inst *Index) Candidates(x, y, radius float64, max int, accept func(polyline int32) bool, out []Snap) []Snap {
	out = out[:0]
	if len(inst.lines.X) == 0 || max <= 0 {
		return out
	}
	inst.gen++
	if inst.gen == 0 {
		clear(inst.stamp)
		inst.gen = 1
	}
	c0, r0 := inst.cellOf(x-radius, y-radius)
	c1, r1 := inst.cellOf(x+radius, y+radius)
	for r := r0; r <= r1; r++ {
		for c := c0; c <= c1; c++ {
			ci := inst.cellIndex(c, r)
			for p := inst.first[ci]; p < inst.first[ci+1]; p++ {
				i := inst.member[p]
				if inst.stamp[i] == inst.gen {
					continue
				}
				inst.stamp[i] = inst.gen
				if accept != nil && !accept(i) {
					continue
				}
				cand := inst.measure(i, x, y)
				if cand.Dist > radius {
					continue
				}
				// Insert in distance order, keeping the best max.
				pos := len(out)
				for pos > 0 && out[pos-1].Dist > cand.Dist {
					pos--
				}
				if pos >= max {
					continue
				}
				if len(out) < max {
					out = append(out, Snap{})
				}
				copy(out[pos+1:], out[pos:len(out)-1])
				out[pos] = cand
			}
		}
	}
	return out
}

// MatchOptions tune [Matcher.Match].
type MatchOptions struct {
	// Radius is how far from an observation a candidate polyline may lie,
	// in the plane's unit. Zero means 50.
	Radius float64
	// Sigma is the emission's standard deviation, in the plane's unit
	// (Newson & Krumm measured 4.07 m for their GPS). Zero means 10.
	Sigma float64
	// Beta scales the transition: the difference between the straight-line
	// distance and the route distance between consecutive observations is
	// exponentially penalised at this scale. Zero means 20.
	Beta float64
	// MaxCandidates bounds the candidates per observation. Zero means 8.
	MaxCandidates int
	// MaxDetour bounds a transition: a route longer than the straight line
	// by more than this factor plus Radius is not considered. Zero means 4.
	MaxDetour float64
}

func (inst MatchOptions) withDefaults() MatchOptions {
	if inst.Radius <= 0 {
		inst.Radius = 50
	}
	if inst.Sigma <= 0 {
		inst.Sigma = 10
	}
	if inst.Beta <= 0 {
		inst.Beta = 20
	}
	if inst.MaxCandidates <= 0 {
		inst.MaxCandidates = 8
	}
	if inst.MaxDetour <= 0 {
		inst.MaxDetour = 4
	}
	return inst
}

// Matched is one observation's answer: the polyline it lies on, or -1
// where the sequence broke at it, and the arcs walked from the previous
// matched observation to this one (empty at a sequence start or on the
// same polyline).
type Matched struct {
	Snap
	Arcs []int32
}

// Matcher is Newson–Krumm map matching (SIGSPATIAL 2009) over a graph
// whose polylines are the edges' geometry: a hidden Markov model whose
// states are candidate polylines within Radius of each observation,
// emission Gaussian in the distance to the polyline, transition
// exponential in the difference between straight-line and route distance,
// decoded by Viterbi. Route distances come from bounded Dijkstra searches
// under a length metric in the plane's unit.
//
// The caller maps a polyline to its arcs: PolylineArcs(i) returns the
// forward arc (first vertex to last) and the backward arc, either -1 where
// absent, so a candidate is entered and left at the right ends.
type Matcher struct {
	g            *Graph
	index        *Index
	length       Metric
	polylineArcs func(polyline int32) (forward, backward int32)
	tailOf       func(polyline int32) (first, last int32) // the nodes at the polyline's ends
	dijk         *Dijkstra
	opts         MatchOptions
}

// NewMatcher binds a matcher. length is the arc length metric; ends
// returns the nodes at a polyline's first and last vertex; arcs its
// forward and backward arcs.
func NewMatcher(g *Graph, index *Index, length Metric, ends func(polyline int32) (first, last int32), arcs func(polyline int32) (forward, backward int32), opts MatchOptions) (inst *Matcher) {
	return &Matcher{g: g, index: index, length: length, polylineArcs: arcs, tailOf: ends, dijk: NewDijkstra(g), opts: opts.withDefaults()}
}

// state is one candidate of one observation in the lattice.
type state struct {
	snap  Snap
	score float64 // log-probability of the best path ending here
	prev  int32   // index of the previous observation's state, -1 at a start
	arcs  []int32 // arcs from prev to here
}

// Match decodes the observations, given as planar coordinates, and returns
// one [Matched] per observation. An observation with no candidate within
// the radius, or unreachable from every candidate of the previous one,
// breaks the sequence: its Polyline is -1 and the next observation starts
// afresh.
func (inst *Matcher) Match(x, y []float64, accept func(polyline int32) bool) (out []Matched) {
	n := len(x)
	out = make([]Matched, n)
	if n == 0 {
		return
	}
	var prev []state
	var cands []Snap
	lattice := make([][]state, n)
	for i := range n {
		cands = inst.index.Candidates(x[i], y[i], inst.opts.Radius, inst.opts.MaxCandidates, accept, cands)
		cur := make([]state, 0, len(cands))
		for _, c := range cands {
			emit := -0.5 * (c.Dist / inst.opts.Sigma) * (c.Dist / inst.opts.Sigma)
			s := state{snap: c, score: math.Inf(-1), prev: -1}
			if len(prev) == 0 {
				s.score = emit
			} else {
				straight := math.Hypot(x[i]-x[i-1], y[i]-y[i-1])
				for j := range prev {
					d, arcs, ok := inst.routeDistance(prev[j].snap, c, straight)
					if !ok {
						continue
					}
					trans := -math.Abs(d-straight) / inst.opts.Beta
					if sc := prev[j].score + trans + emit; sc > s.score {
						s.score, s.prev, s.arcs = sc, int32(j), arcs
					}
				}
				if s.prev < 0 {
					// Unreachable from the previous lattice column: a fresh
					// start, penalised so a reachable candidate wins.
					s.score = emit - 1e6
				}
			}
			cur = append(cur, s)
		}
		lattice[i] = cur
		prev = cur
	}
	// Backtrack from the best final state, breaking where a state started
	// afresh.
	for i := n - 1; i >= 0; {
		col := lattice[i]
		if len(col) == 0 {
			out[i].Polyline = -1
			i--
			continue
		}
		best := 0
		for j := range col {
			if col[j].score > col[best].score {
				best = j
			}
		}
		for i >= 0 {
			s := col[best]
			out[i] = Matched{Snap: s.snap, Arcs: s.arcs}
			if s.prev < 0 {
				i--
				break
			}
			i--
			col = lattice[i]
			best = int(s.prev)
		}
	}
	return
}

// routeDistance is the length of the shortest walk from a on polyline p
// to b on polyline q, leaving p at either end and entering q at either
// end, with the partial polylines counted; ok is false beyond the detour
// bound. On the same polyline the walk along it is taken where the arcs
// allow.
func (inst *Matcher) routeDistance(a, b Snap, straight float64) (d float64, arcs []int32, ok bool) {
	bound := straight*inst.opts.MaxDetour + inst.opts.Radius*4
	if a.Polyline == b.Polyline {
		fwd, bwd := inst.polylineArcs(a.Polyline)
		delta := b.Fraction - a.Fraction
		if (delta >= 0 && fwd >= 0) || (delta < 0 && bwd >= 0) {
			a := fwd
			if delta < 0 {
				a = bwd
			}
			return math.Abs(delta) * float64(inst.length[a]), nil, true
		}
	}
	first, last := inst.tailOf(a.Polyline)
	fwd, bwd := inst.polylineArcs(a.Polyline)
	var sources []Source
	if fwd >= 0 && inst.length[fwd] != Inf {
		sources = append(sources, Source{Node: last, Dist: scaleDist(inst.length[fwd], 1-a.Fraction)})
	}
	if bwd >= 0 && inst.length[bwd] != Inf {
		sources = append(sources, Source{Node: first, Dist: scaleDist(inst.length[bwd], a.Fraction)})
	}
	bFirst, bLast := inst.tailOf(b.Polyline)
	bFwd, bBwd := inst.polylineArcs(b.Polyline)
	targets := make([]int32, 0, 2)
	tails := make([]uint32, 0, 2)
	if bFwd >= 0 && inst.length[bFwd] != Inf {
		targets = append(targets, bFirst)
		tails = append(tails, scaleDist(inst.length[bFwd], b.Fraction))
	}
	if bBwd >= 0 && inst.length[bBwd] != Inf {
		targets = append(targets, bLast)
		tails = append(tails, scaleDist(inst.length[bBwd], 1-b.Fraction))
	}
	if len(sources) == 0 || len(targets) == 0 {
		return 0, nil, false
	}
	dist := make([]uint32, len(targets))
	inst.dijk.OneToMany(inst.length, sources, targets, dist)
	best := Inf
	bestT := -1
	for t := range targets {
		if dist[t] == Inf {
			continue
		}
		if total := addSat(dist[t], tails[t]); total < best {
			best, bestT = total, t
		}
	}
	if bestT < 0 || float64(best) > bound {
		return 0, nil, false
	}
	// The one-to-many search's labels stand, so the tree path to the
	// chosen end is the walk; it is the arcs between the two polylines.
	arcs = inst.dijk.Path(targets[bestT], nil)
	return float64(best), arcs, true
}

func scaleDist(w uint32, f float64) uint32 {
	if w == Inf {
		return Inf
	}
	return uint32(math.Round(float64(w) * f))
}
