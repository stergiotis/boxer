package rutter

import (
	"cmp"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Order is a nested-dissection order of a graph's nodes (ADR-0256 §SD3):
// Rank[v] is the position of node v, Node[r] the node at position r. A
// separator's nodes take the highest positions of the part it split.
type Order struct {
	Rank []int32
	Node []int32
}

// NumNodes is the order's length.
func (inst Order) NumNodes() int32 { return int32(len(inst.Rank)) }

// OrderFromRanksE rebuilds an [Order] from a stored rank array, checking
// that it is a permutation.
func OrderFromRanksE(rank []int32) (o Order, err error) {
	n := len(rank)
	node := make([]int32, n)
	seen := make([]bool, n)
	for v, r := range rank {
		if r < 0 || int(r) >= n || seen[r] {
			err = eb.Build().Int("node", v).Int("rank", int(r)).Errorf("the ranks are not a permutation")
			return
		}
		seen[r] = true
		node[r] = int32(v)
	}
	return Order{Rank: rank, Node: node}, nil
}

// OrderOptions tune [InertialFlowOrder].
type OrderOptions struct {
	// Balance is the fraction of a part's nodes taken as sources and as
	// sinks at each bisection, in (0, 0.5). Zero means 0.25.
	Balance float64
	// LeafSize is the part size below which recursion stops and the part
	// is ordered by degree. Zero means 32.
	LeafSize int32
	// Directions are the projections tried at each bisection, as (a, b)
	// with the projection a·x + b·y. Nil means the four of the paper:
	// west–east, south–north and the two diagonals.
	Directions [][2]float64
}

func (inst OrderOptions) withDefaults() OrderOptions {
	if inst.Balance <= 0 || inst.Balance >= 0.5 {
		inst.Balance = 0.25
	}
	if inst.LeafSize <= 0 {
		inst.LeafSize = 32
	}
	if inst.Directions == nil {
		inst.Directions = [][2]float64{{1, 0}, {0, 1}, {1, 1}, {1, -1}}
	}
	return inst
}

// undirected is the symmetric, deduplicated adjacency the order and the
// hierarchy work on: positions [first[v], first[v+1]) hold v's neighbours
// ascending, and twin[p] is the position of the reverse arc.
type undirected struct {
	first []int32
	adj   []int32
	twin  []int32
}

func newUndirected(g *Graph) (u undirected) {
	n := g.NumNodes()
	// Count both directions, then dedupe per row after a sort.
	deg := make([]int32, n+1)
	for v := range n {
		f, l := g.Out(v)
		deg[v+1] += l - f
		f, l = g.In(v)
		deg[v+1] += l - f
	}
	for v := range n {
		deg[v+1] += deg[v]
	}
	raw := make([]int32, deg[n])
	fill := make([]int32, n)
	copy(fill, deg[:n])
	for v := range n {
		f, l := g.Out(v)
		for a := f; a < l; a++ {
			raw[fill[v]] = g.Head(a)
			fill[v]++
		}
		f, l = g.In(v)
		for p := f; p < l; p++ {
			raw[fill[v]] = g.InTail(p)
			fill[v]++
		}
	}
	u.first = make([]int32, n+1)
	u.adj = make([]int32, 0, len(raw))
	for v := range n {
		row := raw[deg[v]:deg[v+1]]
		slices.Sort(row)
		row = slices.Compact(row)
		u.adj = append(u.adj, row...)
		u.first[v+1] = int32(len(u.adj))
	}
	u.adj = slices.Clip(u.adj)
	u.twin = make([]int32, len(u.adj))
	for v := range n {
		for p := u.first[v]; p < u.first[v+1]; p++ {
			w := u.adj[p]
			row := u.adj[u.first[w]:u.first[w+1]]
			i, _ := slices.BinarySearch(row, v)
			u.twin[p] = u.first[w] + int32(i)
		}
	}
	return
}

func (u undirected) numNodes() int32 { return int32(len(u.first) - 1) }

// dissector holds the scratch of one [InertialFlowOrder] run. Recursion
// works on subsets of the global adjacency, told apart by a stamp per
// node, so no induced subgraph is ever copied.
type dissector struct {
	u    undirected
	x, y []float64
	opts OrderOptions

	rank []int32
	next int32 // the highest unassigned rank + 1; ranks are handed out downward

	active []int32 // stamp: the call id a node belongs to, 0 when ordered
	callID int32

	level  []int32 // BFS level in the flow; -1 unreached
	it     []int32 // current-arc pointer per node
	flow   []int8  // antisymmetric per position, in {-1, 0, 1}
	side   []int32 // stamp: reachable from the sources in the residual graph
	sideID int32
	isSink []int32 // stamp: sink of the current bisection
	sinkID int32

	queue []int32
	proj  []float64
	order []int32
	sep   []int32
	comp  []int32
	stack []int32
}

// InertialFlowOrder computes a nested-dissection order by recursive
// bisection with Inertial Flow separators (Schild & Sommer 2015): at each
// part the nodes are sorted along each direction, the first and last
// Balance fraction are sources and sinks, a unit-capacity max-flow finds
// the cut, and the source-side endpoints of the smallest cut are the
// separator; the remainder's components recurse. x and y are planar
// coordinates per node. Disconnected input is fine: components are
// ordered one after another.
func InertialFlowOrder(g *Graph, x, y []float64, opts OrderOptions) (o Order) {
	opts = opts.withDefaults()
	u := newUndirected(g)
	n := u.numNodes()
	d := &dissector{
		u: u, x: x, y: y, opts: opts,
		rank:   make([]int32, n),
		next:   n,
		active: make([]int32, n),
		level:  make([]int32, n),
		it:     make([]int32, n),
		flow:   make([]int8, len(u.adj)),
		side:   make([]int32, n),
		isSink: make([]int32, n),
	}
	all := make([]int32, n)
	for v := range n {
		all[v] = v
		d.rank[v] = -1
	}
	d.dissect(all)
	o.Rank = d.rank
	o.Node = make([]int32, n)
	for v, r := range o.Rank {
		o.Node[r] = int32(v)
	}
	return
}

// dissect orders the nodes of one part. The slice is the dissector's to
// reuse once the call returns.
func (d *dissector) dissect(nodes []int32) {
	k := int32(len(nodes))
	if k == 0 {
		return
	}
	if k <= d.opts.LeafSize {
		d.orderLeaf(nodes)
		return
	}
	d.callID++
	id := d.callID
	for _, v := range nodes {
		d.active[v] = id
	}
	// Components first: a disconnected part needs no separator.
	comps := d.components(nodes, id)
	if len(comps) > 1 {
		for _, c := range comps {
			d.dissect(c)
		}
		return
	}
	sep, rest := d.bisect(nodes, id)
	if len(sep) == 0 {
		// No cut found — the part is a clique-like knot smaller than any
		// balance allows. Order it as a leaf.
		d.orderLeaf(nodes)
		return
	}
	// The separator takes the top of the range; the parts below it.
	for _, v := range sep {
		d.next--
		d.rank[v] = d.next
		d.active[v] = 0
	}
	d.dissect(rest)
}

// orderLeaf hands out the part's ranks by ascending degree, so the
// well-connected nodes of a small part sit above the rest.
func (d *dissector) orderLeaf(nodes []int32) {
	slices.SortFunc(nodes, func(a, b int32) int {
		da := d.u.first[a+1] - d.u.first[a]
		db := d.u.first[b+1] - d.u.first[b]
		if da != db {
			return cmp.Compare(db, da)
		}
		return cmp.Compare(a, b)
	})
	for _, v := range nodes {
		d.next--
		d.rank[v] = d.next
		d.active[v] = 0
	}
}

// components splits the part's nodes into connected components of the
// induced subgraph. Each returned slice is freshly allocated.
func (d *dissector) components(nodes []int32, id int32) (comps [][]int32) {
	d.sideID++
	seen := d.sideID
	for _, s := range nodes {
		if d.side[s] == seen {
			continue
		}
		comp := []int32{}
		d.queue = append(d.queue[:0], s)
		d.side[s] = seen
		for len(d.queue) > 0 {
			v := d.queue[len(d.queue)-1]
			d.queue = d.queue[:len(d.queue)-1]
			comp = append(comp, v)
			for p := d.u.first[v]; p < d.u.first[v+1]; p++ {
				w := d.u.adj[p]
				if d.active[w] != id || d.side[w] == seen {
					continue
				}
				d.side[w] = seen
				d.queue = append(d.queue, w)
			}
		}
		comps = append(comps, comp)
	}
	return
}

// bisect tries every direction and keeps the smallest separator; ties go
// to the better balance. rest is the part without the separator.
func (d *dissector) bisect(nodes []int32, id int32) (sep, rest []int32) {
	k := len(nodes)
	b := max(1, int(float64(k)*d.opts.Balance))
	d.proj = slices.Grow(d.proj[:0], k)[:k]
	d.order = slices.Grow(d.order[:0], k)[:k]
	bestSep := []int32(nil)
	bestBalance := -1
	for _, dir := range d.opts.Directions {
		for i, v := range nodes {
			d.proj[i] = dir[0]*d.x[v] + dir[1]*d.y[v]
			d.order[i] = int32(i)
		}
		slices.SortFunc(d.order, func(a, b int32) int { return cmp.Compare(d.proj[a], d.proj[b]) })
		sources := make([]int32, b)
		for i := range b {
			sources[i] = nodes[d.order[i]]
		}
		d.sinkID++
		for i := range b {
			d.isSink[nodes[d.order[k-1-i]]] = d.sinkID
		}
		cut := d.maxFlow(nodes, sources, id)
		if cut < 0 {
			continue
		}
		sep, srcSide := d.separator(sources, id)
		balance := min(srcSide-len(sep), k-srcSide)
		if bestSep == nil || len(sep) < len(bestSep) || (len(sep) == len(bestSep) && balance > bestBalance) {
			bestSep = slices.Clone(sep)
			bestBalance = balance
		}
	}
	if bestSep == nil {
		return nil, nil
	}
	d.sideID++
	for _, v := range bestSep {
		d.side[v] = d.sideID
	}
	rest = make([]int32, 0, k-len(bestSep))
	for _, v := range nodes {
		if d.side[v] != d.sideID {
			rest = append(rest, v)
		}
	}
	return bestSep, rest
}

// maxFlow runs Dinic from the sources to the stamped sinks over the part's
// induced subgraph with unit capacities and returns the flow value, or -1
// when a source is a sink.
func (d *dissector) maxFlow(nodes []int32, sources []int32, id int32) (value int) {
	for _, v := range nodes {
		for p := d.u.first[v]; p < d.u.first[v+1]; p++ {
			d.flow[p] = 0
		}
	}
	for _, s := range sources {
		if d.isSink[s] == d.sinkID {
			return -1
		}
	}
	for {
		if !d.bfsLevels(nodes, sources, id) {
			return value
		}
		for _, v := range nodes {
			d.it[v] = d.u.first[v]
		}
		for _, s := range sources {
			for d.augment(s, id) {
				value++
			}
		}
	}
}

// bfsLevels builds the level graph from the sources; false when no sink is
// reachable. Sinks are not expanded.
func (d *dissector) bfsLevels(nodes []int32, sources []int32, id int32) (reached bool) {
	for _, v := range nodes {
		d.level[v] = -1
	}
	d.queue = d.queue[:0]
	for _, s := range sources {
		d.level[s] = 0
		d.queue = append(d.queue, s)
	}
	for head := 0; head < len(d.queue); head++ {
		v := d.queue[head]
		if d.isSink[v] == d.sinkID {
			reached = true
			continue
		}
		for p := d.u.first[v]; p < d.u.first[v+1]; p++ {
			w := d.u.adj[p]
			if d.active[w] != id || d.level[w] >= 0 || d.flow[p] >= 1 {
				continue
			}
			d.level[w] = d.level[v] + 1
			d.queue = append(d.queue, w)
		}
	}
	return
}

// augment pushes one unit from s along the level graph, iteratively with
// a path stack, and returns whether it reached a sink.
func (d *dissector) augment(s int32, id int32) bool {
	d.stack = append(d.stack[:0], s)
	path := d.stack
	for {
		v := path[len(path)-1]
		if d.isSink[v] == d.sinkID {
			break
		}
		advanced := false
		for d.it[v] < d.u.first[v+1] {
			p := d.it[v]
			w := d.u.adj[p]
			if d.active[w] == id && d.level[w] == d.level[v]+1 && d.flow[p] < 1 {
				path = append(path, w)
				advanced = true
				break
			}
			d.it[v]++
		}
		if advanced {
			continue
		}
		// Dead end: retreat, and the parent moves past this arc.
		if len(path) == 1 {
			d.stack = path
			return false
		}
		path = path[:len(path)-1]
		d.it[path[len(path)-1]]++
	}
	for i := 0; i+1 < len(path); i++ {
		p := d.it[path[i]]
		d.flow[p]++
		d.flow[d.u.twin[p]]--
	}
	d.stack = path
	return true
}

// separator is the set of source-side endpoints of the cut after a
// max-flow: the nodes reachable from the sources in the residual graph
// that have an unreachable active neighbour. srcSide is the reachable
// count, separator included.
func (d *dissector) separator(sources []int32, id int32) (sep []int32, srcSide int) {
	d.sideID++
	reach := d.sideID
	d.queue = d.queue[:0]
	for _, s := range sources {
		if d.side[s] != reach {
			d.side[s] = reach
			d.queue = append(d.queue, s)
		}
	}
	for head := 0; head < len(d.queue); head++ {
		v := d.queue[head]
		for p := d.u.first[v]; p < d.u.first[v+1]; p++ {
			w := d.u.adj[p]
			if d.active[w] != id || d.side[w] == reach || d.flow[p] >= 1 {
				continue
			}
			d.side[w] = reach
			d.queue = append(d.queue, w)
		}
	}
	srcSide = len(d.queue)
	d.sep = d.sep[:0]
	for _, v := range d.queue {
		for p := d.u.first[v]; p < d.u.first[v+1]; p++ {
			w := d.u.adj[p]
			if d.active[w] == id && d.side[w] != reach {
				d.sep = append(d.sep, v)
				break
			}
		}
	}
	return d.sep, srcSide
}
