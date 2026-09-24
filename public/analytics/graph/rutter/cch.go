package rutter

import "slices"

// CCH is the metric-independent half of a customizable contraction
// hierarchy (ADR-0256 §SD4): the upward arcs the contraction along an
// [Order] yields, each node's lower neighbours as the reverse view of
// them, and the elimination tree. It is built once per topology and order
// and customized per [Metric] by [CCH.Customize].
type CCH struct {
	g     *Graph
	order Order

	upFirst []int32 // n+1, by node
	upHead  []int32 // per up-arc, sorted by rank within a row
	upTail  []int32 // per up-arc

	downFirst []int32 // n+1, by node: the up-arcs that end at it
	downArc   []int32 // up-arc indices, sorted by the tail's rank

	parent []int32 // elimination-tree parent node, -1 at a root
	height int32
}

// NewCCH contracts g along order: in rank order each node's lowest upward
// neighbour becomes its parent and inherits its other upward neighbours
// (the contraction-graph rule, Dibbelt et al. §5), which yields the
// chordal supergraph's upward arcs with no witness search.
func NewCCH(g *Graph, order Order) (inst *CCH) {
	n := g.NumNodes()
	u := newUndirected(g)
	// Upward neighbour sets by rank, grown as contraction inserts.
	up := make([][]int32, n)
	for v := range n {
		rv := order.Rank[v]
		row := u.adj[u.first[v]:u.first[v+1]]
		for _, w := range row {
			if order.Rank[w] > rv {
				up[v] = append(up[v], order.Rank[w])
			}
		}
		slices.Sort(up[v])
	}
	inst = &CCH{g: g, order: order, parent: make([]int32, n)}
	total := 0
	for r := range n {
		v := order.Node[r]
		nb := up[v]
		total += len(nb)
		if len(nb) == 0 {
			inst.parent[v] = -1
			continue
		}
		p := order.Node[nb[0]]
		inst.parent[v] = p
		for _, w := range nb[1:] {
			row := up[p]
			i, found := slices.BinarySearch(row, w)
			if !found {
				up[p] = slices.Insert(row, i, w)
				total++
			}
		}
	}
	inst.upFirst = make([]int32, n+1)
	inst.upHead = make([]int32, 0, total)
	inst.upTail = make([]int32, 0, total)
	for v := range n {
		for _, r := range up[v] {
			inst.upHead = append(inst.upHead, order.Node[r])
			inst.upTail = append(inst.upTail, v)
		}
		inst.upFirst[v+1] = int32(len(inst.upHead))
		up[v] = nil
	}
	inst.buildDown()
	inst.height = inst.treeHeight()
	return
}

// buildDown derives each node's lower neighbours: the up-arcs ending at
// it, sorted by the tail's rank. Walking nodes in rank order appends the
// arcs of each row in tail-rank order, so no sort is needed.
func (inst *CCH) buildDown() {
	n := inst.g.NumNodes()
	inst.downFirst = make([]int32, n+1)
	for _, h := range inst.upHead {
		inst.downFirst[h+1]++
	}
	for v := range n {
		inst.downFirst[v+1] += inst.downFirst[v]
	}
	inst.downArc = make([]int32, len(inst.upHead))
	fill := make([]int32, n)
	copy(fill, inst.downFirst[:n])
	for r := range n {
		v := inst.order.Node[r]
		for a := inst.upFirst[v]; a < inst.upFirst[v+1]; a++ {
			h := inst.upHead[a]
			inst.downArc[fill[h]] = a
			fill[h]++
		}
	}
}

func (inst *CCH) treeHeight() (h int32) {
	n := inst.g.NumNodes()
	depth := make([]int32, n)
	for r := range n {
		v := inst.order.Node[n-1-r] // roots first
		if p := inst.parent[v]; p >= 0 {
			depth[v] = depth[p] + 1
		}
		h = max(h, depth[v]+1)
	}
	return
}

// Graph is the graph the hierarchy was built over.
func (inst *CCH) Graph() *Graph { return inst.g }

// Order is the order it was built along.
func (inst *CCH) Order() Order { return inst.order }

// NumUpArcs is the upward arc count, original arcs and shortcuts together.
func (inst *CCH) NumUpArcs() int32 { return int32(len(inst.upHead)) }

// Height is the elimination tree's height: the longest chain a query walks.
func (inst *CCH) Height() int32 { return inst.height }

// Parent is v's elimination-tree parent, -1 at a root.
func (inst *CCH) Parent(v int32) int32 { return inst.parent[v] }

// upArc finds the up-arc from x to y by the rank of y in x's row, or -1.
func (inst *CCH) upArc(x, y int32) int32 {
	lo, hi := inst.upFirst[x], inst.upFirst[x+1]
	ry := inst.order.Rank[y]
	for lo < hi {
		mid := (lo + hi) / 2
		if inst.order.Rank[inst.upHead[mid]] < ry {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < inst.upFirst[x+1] && inst.upHead[lo] == y {
		return lo
	}
	return -1
}

// Weights is one customized metric over a [CCH] (ADR-0256 §SD4): for each
// up-arc (x, y) the length of the shortest x→y path it stands for and of
// the y→x path, the middle node that formed each where it is a shortcut,
// and the original arc it stands for where it is not. Immutable once
// returned; a caller swapping metrics under running queries publishes a
// new one and lets the old finish.
type Weights struct {
	cch      *CCH
	up, down []uint32
	upMid    []int32
	downMid  []int32
	upOrig   []int32
	downOrig []int32
}

// Customize computes the weights for w: every up-arc takes the minimum of
// the original arcs it covers in each direction, then each arc (x, y) is
// relaxed over its lower triangles (z, x, y) in the rank order of x, so a
// triangle's lower arcs are final when it is read.
func (inst *CCH) Customize(w Metric) (out *Weights) {
	m := len(inst.upHead)
	out = &Weights{
		cch: inst,
		up:  make([]uint32, m), down: make([]uint32, m),
		upMid: make([]int32, m), downMid: make([]int32, m),
		upOrig: make([]int32, m), downOrig: make([]int32, m),
	}
	for a := range m {
		out.up[a], out.down[a] = Inf, Inf
		out.upMid[a], out.downMid[a] = -1, -1
		out.upOrig[a], out.downOrig[a] = -1, -1
	}
	g := inst.g
	rank := inst.order.Rank
	for v := range g.NumNodes() {
		first, last := g.Out(v)
		for a := first; a < last; a++ {
			u := g.Head(a)
			if rank[u] > rank[v] {
				ua := inst.upArc(v, u)
				if w[a] < out.up[ua] {
					out.up[ua], out.upOrig[ua] = w[a], a
				}
			} else {
				ua := inst.upArc(u, v)
				if w[a] < out.down[ua] {
					out.down[ua], out.downOrig[ua] = w[a], a
				}
			}
		}
	}
	n := g.NumNodes()
	for r := range n {
		x := inst.order.Node[r]
		for a := inst.upFirst[x]; a < inst.upFirst[x+1]; a++ {
			y := inst.upHead[a]
			// Lower triangles: z in both down rows, merged by the tail's rank.
			i, iEnd := inst.downFirst[x], inst.downFirst[x+1]
			j, jEnd := inst.downFirst[y], inst.downFirst[y+1]
			for i < iEnd && j < jEnd {
				zx, zy := inst.downArc[i], inst.downArc[j]
				rzx, rzy := rank[inst.upTail[zx]], rank[inst.upTail[zy]]
				switch {
				case rzx < rzy:
					i++
				case rzx > rzy:
					j++
				default:
					z := inst.upTail[zx]
					// x→z→y: down along (z, x), up along (z, y).
					if d := addSat(out.down[zx], out.up[zy]); d < out.up[a] {
						out.up[a], out.upMid[a] = d, z
					}
					// y→z→x: down along (z, y), up along (z, x).
					if d := addSat(out.down[zy], out.up[zx]); d < out.down[a] {
						out.down[a], out.downMid[a] = d, z
					}
					i++
					j++
				}
			}
		}
	}
	return
}

// CCH is the hierarchy the weights were customized over.
func (inst *Weights) CCH() *CCH { return inst.cch }

// unpackUp appends the original arcs of the path up-arc a stands for in
// the x→y direction.
func (inst *Weights) unpackUp(a int32, out []int32) []int32 {
	z := inst.upMid[a]
	if z < 0 {
		if inst.upOrig[a] >= 0 {
			out = append(out, inst.upOrig[a])
		}
		return out
	}
	c := inst.cch
	zx := c.upArc(z, c.upTail[a])
	zy := c.upArc(z, c.upHead[a])
	out = inst.unpackDown(zx, out)
	return inst.unpackUp(zy, out)
}

// unpackDown appends the original arcs of the path up-arc a stands for in
// the y→x direction.
func (inst *Weights) unpackDown(a int32, out []int32) []int32 {
	z := inst.downMid[a]
	if z < 0 {
		if inst.downOrig[a] >= 0 {
			out = append(out, inst.downOrig[a])
		}
		return out
	}
	c := inst.cch
	zx := c.upArc(z, c.upTail[a])
	zy := c.upArc(z, c.upHead[a])
	out = inst.unpackDown(zy, out)
	return inst.unpackUp(zx, out)
}
