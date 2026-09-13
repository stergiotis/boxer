package engine

import (
	"context"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
)

// DirectionE selects which adjacency a sweep follows.
type DirectionE uint8

const (
	// DirectionOut follows arcs s→d from the frontier to its out-neighbours.
	DirectionOut DirectionE = iota
	// DirectionIn follows arcs in reverse, from the frontier to its
	// in-neighbours.
	DirectionIn
	// DirectionBoth follows both, treating the graph as undirected.
	DirectionBoth
)

// SweepE names the traversal form an edge map ran.
type SweepE uint8

const (
	// SweepPush iterated the frontier's edges (serial).
	SweepPush SweepE = iota
	// SweepPull iterated every candidate destination's in-edges (parallel
	// over destinations).
	SweepPull
)

// DefaultDenseFraction is Ligra's threshold: the sweep pulls once the
// frontier's out-edges exceed this fraction of the arcs.
const DefaultDenseFraction = 1.0 / 20

// EdgeMapOptions tunes one edge map.
type EdgeMapOptions struct {
	// Direction selects the adjacency followed. Zero is DirectionOut.
	Direction DirectionE
	// DenseFraction overrides [DefaultDenseFraction]; a value of 1 or more
	// never pulls, a value of 0 always pulls.
	DenseFraction float64
	// ForcePull or ForcePush fix the sweep regardless of the frontier size.
	ForcePull bool
	ForcePush bool
}

// EdgeFuncs are the per-edge callbacks of an edge map, after Ligra.
//
// Update is applied along an edge from frontier vertex s to d and reports
// whether d joins the next frontier. Under a pull sweep Update runs for every
// in-edge of d whose source is in the frontier, in ascending source order,
// and must write only state indexed by d; it may read source state that the
// sweep does not write. Under a push sweep it runs serially in ascending
// (s, d) order.
//
// Cond reports whether d is still a candidate; nil means every vertex is.
// A pull sweep stops walking d's in-edges as soon as Cond(d) turns false
// after an Update, which is what makes a pull BFS linear in the visited edges.
type EdgeFuncs struct {
	Update func(s, d int32) bool
	Cond   func(d int32) bool
}

// EdgeMap applies f along the edges leaving frontier under opts and returns
// the next frontier and which sweep ran. The next frontier is a fresh
// [Subset]; the input is untouched.
func (e *Engine) EdgeMap(g *csr.Graph, frontier *Subset, f EdgeFuncs, opts EdgeMapOptions) (next *Subset, sweep SweepE) {
	n := g.NumVertices()
	next = NewSubset(n)
	if frontier.IsEmpty() {
		return next, SweepPush
	}
	pull := e.choosePull(g, frontier, opts)
	if pull {
		e.pull(g, frontier, f, opts.Direction, next)
		return next, SweepPull
	}
	e.push(g, frontier, f, opts.Direction, next)
	return next, SweepPush
}

func (e *Engine) choosePull(g *csr.Graph, frontier *Subset, opts EdgeMapOptions) bool {
	if opts.ForcePull {
		return true
	}
	if opts.ForcePush {
		return false
	}
	frac := opts.DenseFraction
	if frac == 0 {
		frac = DefaultDenseFraction
	}
	if frac >= 1 {
		return false
	}
	// Frontier out-edge count, in the sweep direction.
	var outEdges int64
	for _, s := range frontier.Sparse() {
		outEdges += e.degree(g, s, opts.Direction)
	}
	arcs := int64(g.NumArcs())
	if opts.Direction == DirectionBoth && g.IsDirected() {
		arcs *= 2
	}
	return float64(outEdges) > frac*float64(arcs)
}

func (e *Engine) degree(g *csr.Graph, v int32, dir DirectionE) int64 {
	switch dir {
	case DirectionIn:
		return int64(g.InDegree(v))
	case DirectionBoth:
		if g.IsDirected() {
			return int64(g.OutDegree(v)) + int64(g.InDegree(v))
		}
		return int64(g.OutDegree(v))
	default:
		return int64(g.OutDegree(v))
	}
}

// push runs serially over the frontier's edges in ascending (s, d) order.
func (e *Engine) push(g *csr.Graph, frontier *Subset, f EdgeFuncs, dir DirectionE, next *Subset) {
	n := g.NumVertices()
	if cap(e.next) < n {
		e.next = make([]bool, n)
	}
	mark := e.next[:n]
	clear(mark)
	buf := make([]int32, 0, 64)
	for _, s := range frontier.Sparse() {
		for _, d := range Forward(g, s, dir, &buf) {
			if f.Cond != nil && !f.Cond(d) {
				continue
			}
			if f.Update(s, d) {
				mark[d] = true
			}
		}
	}
	next.SetDense(mark)
}

// Forward returns the neighbours of s in the sweep direction, ascending.
// For DirectionBoth on a directed graph the two rows are merged into *buf,
// which grows to fit and is kept for the next call; otherwise the graph's
// own row is returned and buf is untouched.
func Forward(g *csr.Graph, s int32, dir DirectionE, buf *[]int32) []int32 {
	switch dir {
	case DirectionIn:
		return g.In(s)
	case DirectionBoth:
		if !g.IsDirected() {
			return g.Out(s)
		}
		*buf = MergeSorted(g.Out(s), g.In(s), (*buf)[:0])
		return *buf
	default:
		return g.Out(s)
	}
}

// Backward returns the vertices whose sweep-direction edge reaches d — the
// sources a pull over d must consult. buf is used as in [Forward].
func Backward(g *csr.Graph, d int32, dir DirectionE, buf *[]int32) []int32 {
	switch dir {
	case DirectionIn:
		return g.Out(d)
	case DirectionBoth:
		if !g.IsDirected() {
			return g.Out(d)
		}
		*buf = MergeSorted(g.Out(d), g.In(d), (*buf)[:0])
		return *buf
	default:
		return g.In(d)
	}
}

// MergeSorted appends the sorted union of two ascending slot lists to buf
// and returns it.
func MergeSorted(a, b, buf []int32) []int32 {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			buf = append(buf, a[i])
			i++
		case a[i] > b[j]:
			buf = append(buf, b[j])
			j++
		default:
			buf = append(buf, a[i])
			i++
			j++
		}
	}
	buf = append(buf, a[i:]...)
	buf = append(buf, b[j:]...)
	return buf
}

// ContextDone reports whether ctx is cancelled or past its deadline, without
// blocking.
func ContextDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// pull runs over every candidate destination, split across workers by
// contiguous chunks; each destination's sources are consulted in ascending
// order, and the walk stops when Cond(d) turns false.
func (e *Engine) pull(g *csr.Graph, frontier *Subset, f EdgeFuncs, dir DirectionE, next *Subset) {
	n := g.NumVertices()
	inF := frontier.Dense()
	if cap(e.next) < n {
		e.next = make([]bool, n)
	}
	mark := e.next[:n]
	clear(mark)
	e.ParallelFor(n, func(_, lo, hi int) {
		buf := make([]int32, 0, 64)
		for d := int32(lo); d < int32(hi); d++ {
			if f.Cond != nil && !f.Cond(d) {
				continue
			}
			for _, s := range Backward(g, d, dir, &buf) {
				if !inF[s] {
					continue
				}
				if f.Update(s, d) {
					mark[d] = true
				}
				if f.Cond != nil && !f.Cond(d) {
					break
				}
			}
		}
	})
	next.SetDense(mark)
}
