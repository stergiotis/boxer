package rutter

import "iter"

// Dijkstra is a label-setting search over a [Graph] with its scratch held
// between runs (ADR-0256 §SD2): labels and parent arcs stamped by
// generation so a reset costs nothing, and a 4-ary heap. It is the oracle
// the hierarchy is tested against and the engine for one-to-all under a
// cut-off. Single-goroutine.
type Dijkstra struct {
	g       *Graph
	dist    []uint32
	parent  []int32 // the arc that set dist, or -1 at the source
	stamp   []uint32
	gen     uint32
	heap    *heap4
	settled []int32
}

// NewDijkstra allocates the scratch for g.
func NewDijkstra(g *Graph) (inst *Dijkstra) {
	n := g.NumNodes()
	inst = &Dijkstra{
		g:      g,
		dist:   make([]uint32, n),
		parent: make([]int32, n),
		stamp:  make([]uint32, n),
		heap:   newHeap4(n),
	}
	return
}

// Source is one start of a search with its offset: a point snapped onto an
// arc starts at both ends of it with the remaining cost of each.
type Source struct {
	Node int32
	Dist uint32
}

func (inst *Dijkstra) begin(sources []Source) {
	inst.gen++
	if inst.gen == 0 {
		clear(inst.stamp)
		inst.gen = 1
	}
	inst.heap.reset()
	inst.settled = inst.settled[:0]
	for _, s := range sources {
		if s.Dist == Inf {
			continue
		}
		if inst.stamp[s.Node] == inst.gen && inst.dist[s.Node] <= s.Dist {
			continue
		}
		inst.stamp[s.Node] = inst.gen
		inst.dist[s.Node] = s.Dist
		inst.parent[s.Node] = -1
		inst.heap.push(s.Node, s.Dist)
	}
}

func (inst *Dijkstra) label(v int32) (d uint32, ok bool) {
	if inst.stamp[v] != inst.gen {
		return Inf, false
	}
	return inst.dist[v], true
}

// step settles the heap's minimum and relaxes its row; ok is false when the
// heap is empty.
func (inst *Dijkstra) step(w Metric) (v int32, d uint32, ok bool) {
	if inst.heap.len() == 0 {
		return -1, Inf, false
	}
	v, d = inst.heap.pop()
	inst.settled = append(inst.settled, v)
	first, last := inst.g.Out(v)
	for a := first; a < last; a++ {
		wa := w[a]
		if wa == Inf {
			continue
		}
		nd := addSat(d, wa)
		if nd == Inf {
			continue
		}
		u := inst.g.head[a]
		if inst.stamp[u] == inst.gen && inst.dist[u] <= nd {
			continue
		}
		inst.stamp[u] = inst.gen
		inst.dist[u] = nd
		inst.parent[u] = a
		inst.heap.push(u, nd)
	}
	return v, d, true
}

// OneToOne is the distance from the sources to target under w, with the
// search stopped when the target settles. ok is false when it is not
// reachable.
func (inst *Dijkstra) OneToOne(w Metric, sources []Source, target int32) (d uint32, ok bool) {
	inst.begin(sources)
	for {
		v, dv, more := inst.step(w)
		if !more {
			return Inf, false
		}
		if v == target {
			return dv, true
		}
	}
}

// OneToAll settles every node within cutoff of the sources and yields each
// with its distance, in settling order. The labels stay readable through
// [Dijkstra.Dist] until the next search.
func (inst *Dijkstra) OneToAll(w Metric, sources []Source, cutoff uint32) iter.Seq2[int32, uint32] {
	return func(yield func(int32, uint32) bool) {
		inst.begin(sources)
		for inst.heap.len() > 0 && inst.heap.peek() <= cutoff {
			v, d, _ := inst.step(w)
			if !yield(v, d) {
				return
			}
		}
	}
}

// OneToMany fills out[i] with the distance to targets[i], [Inf] where
// unreachable, stopping once every target has settled.
func (inst *Dijkstra) OneToMany(w Metric, sources []Source, targets []int32, out []uint32) {
	inst.begin(sources)
	remaining := len(targets)
	want := make(map[int32]int32, len(targets))
	for i, t := range targets {
		out[i] = Inf
		want[t] = int32(i)
	}
	for remaining > 0 {
		v, d, more := inst.step(w)
		if !more {
			return
		}
		if i, isTarget := want[v]; isTarget {
			if out[i] == Inf {
				remaining--
			}
			out[i] = d
		}
	}
}

// Dist is the label of v after the last search, [Inf] when v was never
// reached. After a search cut short — a cut-off, a settled target — a label
// of a node that did not settle is tentative; [Dijkstra.Settled] lists the
// final ones.
func (inst *Dijkstra) Dist(v int32) uint32 {
	d, _ := inst.label(v)
	return d
}

// Path appends the arcs of the tree path into v, source first, to out. A
// node the last search did not reach appends nothing.
func (inst *Dijkstra) Path(v int32, out []int32) []int32 {
	if inst.stamp[v] != inst.gen {
		return out
	}
	start := len(out)
	for {
		a := inst.parent[v]
		if a < 0 {
			break
		}
		out = append(out, a)
		v = inst.g.Tail(a)
	}
	for i, j := start, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Settled is every node the last search settled, in order. Shared; do not
// modify.
func (inst *Dijkstra) Settled() []int32 { return inst.settled }
