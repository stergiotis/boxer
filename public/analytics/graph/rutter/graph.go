package rutter

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Inf is the weight of an arc a metric forbids. Sums saturate at it.
const Inf uint32 = math.MaxUint32

// Metric is one weight per arc, parallel to the graph's arcs, in whatever
// unit the caller counts (ADR-0256 §SD1). An arc weighted [Inf] is absent
// under the metric.
type Metric []uint32

// Graph is the routing graph (ADR-0256 §SD1): n nodes as slots, arcs kept
// as declared — parallel arcs included, self-loops dropped — each with the
// edge it came from, forward rows sorted by head and reverse rows sorted by
// tail. Arc indices are the order of the forward rows and are what a
// [Metric] is parallel to.
type Graph struct {
	n int32

	firstOut []int32 // n+1
	head     []int32 // per arc, grouped by tail
	arcEdge  []int32 // per arc

	firstIn []int32 // n+1
	inArc   []int32 // per node's reverse row: the forward arc index
	inTail  []int32 // per node's reverse row: the arc's tail

	// inputArc maps an input arc position to its arc index, or -1 for a
	// dropped self-loop, so a caller's per-input columns map onto arcs.
	inputArc []int32
}

// BuildE reads parallel tail, head and edge columns into a [Graph] over n
// nodes. Every value must be a slot below n; edge is any int32 the caller
// uses to name the arc's origin, and may repeat (a two-way segment is two
// arcs with one edge). A self-loop is dropped, because no shortest path
// uses one and the hierarchy has no place for it.
func BuildE(n int32, tail, head, edge []int32) (g *Graph, err error) {
	if len(tail) != len(head) || len(tail) != len(edge) {
		err = eb.Build().Int("tail", len(tail)).Int("head", len(head)).Int("edge", len(edge)).Errorf("the arc columns differ in length")
		return
	}
	if n < 0 || len(tail) > math.MaxInt32 {
		err = eb.Build().Int("nodes", int(n)).Int("arcs", len(tail)).Errorf("the graph exceeds the int32 offsets")
		return
	}
	m := 0
	for i := range tail {
		if tail[i] < 0 || tail[i] >= n || head[i] < 0 || head[i] >= n {
			err = eb.Build().Int("arc", i).Int("tail", int(tail[i])).Int("head", int(head[i])).Int("nodes", int(n)).Errorf("an arc names a slot outside the graph")
			return
		}
		if tail[i] != head[i] {
			m++
		}
	}
	g = &Graph{n: n}
	g.firstOut = make([]int32, n+1)
	for i := range tail {
		if tail[i] != head[i] {
			g.firstOut[tail[i]+1]++
		}
	}
	for v := range n {
		g.firstOut[v+1] += g.firstOut[v]
	}
	g.head = make([]int32, m)
	g.arcEdge = make([]int32, m)
	g.inputArc = make([]int32, len(tail))
	fill := make([]int32, n)
	copy(fill, g.firstOut[:n])
	// A counting sort by head first would keep rows sorted by head; the
	// rows are sorted per node below instead, because the input order is
	// what the caller's columns are in and a stable two-pass sort is not
	// worth its second copy here.
	for i := range tail {
		if tail[i] == head[i] {
			g.inputArc[i] = -1
			continue
		}
		p := fill[tail[i]]
		fill[tail[i]]++
		g.head[p] = head[i]
		g.arcEdge[p] = edge[i]
		g.inputArc[i] = p
	}
	g.sortRows()
	g.buildReverse()
	return g, nil
}

// sortRows orders each forward row by head, carrying the edge and the
// input mapping along. Rows are short, so an insertion sort per row is
// the cheapest correct choice.
func (g *Graph) sortRows() {
	// The input mapping points at positions; positions move, so the
	// permutation is recorded and the mapping remapped afterwards.
	perm := make([]int32, len(g.head))
	for i := range perm {
		perm[i] = int32(i)
	}
	for v := range g.n {
		lo, hi := g.firstOut[v], g.firstOut[v+1]
		for i := lo + 1; i < hi; i++ {
			h, e, p := g.head[i], g.arcEdge[i], perm[i]
			j := i
			for j > lo && (g.head[j-1] > h || (g.head[j-1] == h && g.arcEdge[j-1] > e)) {
				g.head[j], g.arcEdge[j], perm[j] = g.head[j-1], g.arcEdge[j-1], perm[j-1]
				j--
			}
			g.head[j], g.arcEdge[j], perm[j] = h, e, p
		}
	}
	inv := make([]int32, len(perm))
	for newPos, oldPos := range perm {
		inv[oldPos] = int32(newPos)
	}
	for i, a := range g.inputArc {
		if a >= 0 {
			g.inputArc[i] = inv[a]
		}
	}
}

// buildReverse derives the reverse rows: for each node, the arcs that end
// at it, sorted by tail, each carrying its forward arc index.
func (g *Graph) buildReverse() {
	m := int32(len(g.head))
	g.firstIn = make([]int32, g.n+1)
	for _, h := range g.head {
		g.firstIn[h+1]++
	}
	for v := range g.n {
		g.firstIn[v+1] += g.firstIn[v]
	}
	g.inArc = make([]int32, m)
	g.inTail = make([]int32, m)
	fill := make([]int32, g.n)
	copy(fill, g.firstIn[:g.n])
	// Walking arcs in forward order means tails ascend within each reverse
	// row, so no sort is needed.
	for v := range g.n {
		for a := g.firstOut[v]; a < g.firstOut[v+1]; a++ {
			h := g.head[a]
			p := fill[h]
			fill[h]++
			g.inArc[p] = a
			g.inTail[p] = v
		}
	}
}

// NumNodes is the slot count.
func (g *Graph) NumNodes() int32 { return g.n }

// NumArcs is the arc count after self-loops were dropped.
func (g *Graph) NumArcs() int32 { return int32(len(g.head)) }

// Out is the forward row of v: arc indices [first, last).
func (g *Graph) Out(v int32) (first, last int32) { return g.firstOut[v], g.firstOut[v+1] }

// Head is the head of arc a.
func (g *Graph) Head(a int32) int32 { return g.head[a] }

// Edge is the edge arc a came from.
func (g *Graph) Edge(a int32) int32 { return g.arcEdge[a] }

// In is the reverse row of v: positions [first, last) into [Graph.InArc]
// and [Graph.InTail].
func (g *Graph) In(v int32) (first, last int32) { return g.firstIn[v], g.firstIn[v+1] }

// InArc is the forward arc index at reverse position p.
func (g *Graph) InArc(p int32) int32 { return g.inArc[p] }

// InTail is the tail of the arc at reverse position p.
func (g *Graph) InTail(p int32) int32 { return g.inTail[p] }

// Tail is the tail of arc a, found by binary search over the offsets; a
// caller walking rows knows the tail already and does not need this.
func (g *Graph) Tail(a int32) int32 {
	lo, hi := int32(0), g.n
	for lo < hi {
		mid := (lo + hi) / 2
		if g.firstOut[mid+1] <= a {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// InputArc maps the i-th input arc of [BuildE] to its arc index, or -1 for
// a dropped self-loop.
func (g *Graph) InputArc(i int) int32 { return g.inputArc[i] }

// FirstOut exposes the forward offsets (length n+1). Shared; do not modify.
func (g *Graph) FirstOut() []int32 { return g.firstOut }

// Heads exposes the flat head array. Shared; do not modify.
func (g *Graph) Heads() []int32 { return g.head }

// Edges exposes the flat edge array, parallel to [Graph.Heads]. Shared; do
// not modify.
func (g *Graph) Edges() []int32 { return g.arcEdge }

// FromRowsE rebuilds a graph from the arrays [Graph.FirstOut], [Graph.Heads]
// and [Graph.Edges] exposed, as a caller persisting the topology stores
// them. The rows must be sorted by head as [BuildE] leaves them.
func FromRowsE(firstOut, head, edge []int32) (g *Graph, err error) {
	if len(firstOut) == 0 || len(head) != len(edge) || int(firstOut[len(firstOut)-1]) != len(head) {
		err = eb.Build().Int("offsets", len(firstOut)).Int("heads", len(head)).Int("edges", len(edge)).Errorf("the row arrays do not describe a graph")
		return
	}
	n := int32(len(firstOut) - 1)
	for i := range n {
		if firstOut[i] > firstOut[i+1] {
			err = eb.Build().Int("node", int(i)).Errorf("the offsets are not monotone")
			return
		}
	}
	for _, h := range head {
		if h < 0 || h >= n {
			err = eb.Build().Int("head", int(h)).Int("nodes", int(n)).Errorf("a head outside the graph")
			return
		}
	}
	g = &Graph{n: n, firstOut: firstOut, head: head, arcEdge: edge}
	g.buildReverse()
	return g, nil
}

// MetricFromInput spreads a per-input-arc weight column over the arcs, in
// the order [BuildE] was given them; dropped self-loops are skipped.
func (g *Graph) MetricFromInput(w []uint32) (m Metric) {
	m = make(Metric, len(g.head))
	for i, a := range g.inputArc {
		if a >= 0 {
			m[a] = w[i]
		}
	}
	return m
}

// addSat is a saturating sum; [Inf] absorbs.
func addSat(a, b uint32) uint32 {
	if a == Inf || b == Inf {
		return Inf
	}
	s := uint64(a) + uint64(b)
	if s >= uint64(Inf) {
		return Inf
	}
	return uint32(s)
}
