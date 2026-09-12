package graphview

import (
	"cmp"
	"math"
	"slices"
)

// RadialParams tunes the radial layout (ADR-0225 §SD6): rings by undirected
// hop distance from a set of centres. Centers names the nodes at the
// middle; ids the declaration does not carry are ignored, and none at all
// takes the node of highest degree, smallest id on a tie. RingDist is the
// spacing between rings in world units; zero takes 60. A change re-runs
// the layout on the next Render.
type RadialParams struct {
	Centers  []uint64
	RingDist float32
}

func (inst RadialParams) withDefaults() RadialParams {
	if inst.RingDist <= 0 {
		inst.RingDist = 60
	}
	return inst
}

// equal reports whether two parameter sets lay out the same.
func (inst RadialParams) equal(o RadialParams) bool {
	return inst.RingDist == o.RingDist && slices.Equal(inst.Centers, o.Centers)
}

// clone copies the parameters so a retained copy does not alias the
// caller's slice.
func (inst RadialParams) clone() RadialParams {
	inst.Centers = slices.Clone(inst.Centers)
	return inst
}

// radialSystemGap is the clear space between two radial systems, in ring
// distances.
const radialSystemGap = 1

// layoutRadial places every node on the ring of its hop distance from the
// centres. One centre sits at its system's origin; several sit on ring one
// under a virtual root, spread by their subtrees. Angles follow the classic
// radial tree drawing: a breadth-first tree from the centres, children in
// id order, each subtree allotted a sector proportional to its leaf count
// within its parent's, the node at the middle of its sector. A component
// the centres do not reach becomes a system of its own around its node of
// highest degree, packed to the right of the last. The result is a
// function of the topology and the parameters alone.
func layoutRadial(g *graph, p RadialParams) {
	n := g.n()
	if n == 0 {
		return
	}
	// Undirected neighbours in id order, so the tree is a function of the
	// topology and not of the declaration order.
	adjStart := make([]int32, n+1)
	copy(adjStart, g.adjStart[:n+1])
	adj := make([]int32, len(g.adjList))
	copy(adj, g.adjList[:adjStart[n]])
	for s := 0; s < n; s++ {
		nb := adj[adjStart[s]:adjStart[s+1]]
		slices.SortFunc(nb, func(a, b int32) int { return cmp.Compare(g.ids[a], g.ids[b]) })
	}
	degree := func(s int32) int32 { return adjStart[s+1] - adjStart[s] }
	// The node of highest degree among the unvisited, smallest id on a tie.
	visited := make([]bool, n)
	bestUnvisited := func() int32 {
		best := int32(-1)
		for s := int32(0); s < int32(n); s++ {
			if visited[s] {
				continue
			}
			if best < 0 || degree(s) > degree(best) || (degree(s) == degree(best) && g.ids[s] < g.ids[best]) {
				best = s
			}
		}
		return best
	}

	// The first system's centres are the declared ones, in declaration
	// order, each once; with none known the whole graph takes the best node.
	var centers []int32
	seen := make(map[int32]struct{}, len(p.Centers))
	for _, id := range p.Centers {
		if s, ok := g.slot[id]; ok {
			if _, dup := seen[s]; !dup {
				seen[s] = struct{}{}
				centers = append(centers, s)
			}
		}
	}
	if len(centers) == 0 {
		centers = append(centers, bestUnvisited())
	}

	parent := make([]int32, n)
	depth := make([]int32, n)
	order := make([]int32, 0, n) // breadth-first order, per system appended
	kids := make([][]int32, n)
	leaves := make([]int32, n)
	sector0 := make([]float64, n) // sector start
	sector := make([]float64, n)  // sector width
	rw := radialWalker{adjStart: adjStart, adj: adj, visited: visited, parent: parent, depth: depth, kids: kids}

	offsetX := float32(0)
	first := true
	for {
		if !first {
			c := bestUnvisited()
			if c < 0 {
				break
			}
			centers = centers[:0]
			centers = append(centers, c)
		}
		first = false
		start := len(order)
		order = rw.walk(centers, order)
		sys := order[start:]
		// Leaf counts, children first: the order is breadth-first, so the
		// reverse visits every child before its parent.
		for i := len(sys) - 1; i >= 0; i-- {
			s := sys[i]
			if len(kids[s]) == 0 {
				leaves[s] = 1
			} else {
				leaves[s] = 0
				for _, k := range kids[s] {
					leaves[s] += leaves[k]
				}
			}
		}
		// Sectors: a virtual root over the centres shares the full turn by
		// leaves; every node shares its sector among its children the
		// same way.
		total := int32(0)
		for _, c := range centers {
			total += leaves[c]
		}
		a := 0.0
		for _, c := range centers {
			sector0[c] = a
			sector[c] = 2 * math.Pi * float64(leaves[c]) / float64(total)
			a += sector[c]
		}
		for _, s := range sys {
			a := sector0[s]
			for _, k := range kids[s] {
				sector0[k] = a
				sector[k] = sector[s] * float64(leaves[k]) / float64(leaves[s])
				a += sector[k]
			}
		}
		// Rings: one centre at the origin, several on ring one.
		ringOff := int32(0)
		if len(centers) > 1 {
			ringOff = 1
		}
		maxRing := int32(0)
		for _, s := range sys {
			maxRing = max(maxRing, depth[s]+ringOff)
		}
		radius := float32(maxRing) * p.RingDist
		cx := offsetX + radius
		for _, s := range sys {
			r := float32(depth[s]+ringOff) * p.RingDist
			mid := sector0[s] + sector[s]/2
			g.x[s] = cx + r*float32(math.Cos(mid))
			g.y[s] = r * float32(math.Sin(mid))
		}
		offsetX = cx + radius + radialSystemGap*p.RingDist
	}
	g.posVer++
}

// radialWalker is the breadth-first walk shared by the systems.
type radialWalker struct {
	adjStart []int32
	adj      []int32
	visited  []bool
	parent   []int32
	depth    []int32
	kids     [][]int32
}

// walk visits everything reachable from the centres, recording parent,
// depth and children, and appends the visit order to out.
func (w *radialWalker) walk(centers []int32, out []int32) []int32 {
	head := len(out)
	for _, c := range centers {
		if w.visited[c] {
			continue
		}
		w.visited[c] = true
		w.parent[c] = -1
		w.depth[c] = 0
		out = append(out, c)
	}
	for head < len(out) {
		s := out[head]
		head++
		for _, nb := range w.adj[w.adjStart[s]:w.adjStart[s+1]] {
			if w.visited[nb] {
				continue
			}
			w.visited[nb] = true
			w.parent[nb] = s
			w.depth[nb] = w.depth[s] + 1
			w.kids[s] = append(w.kids[s], nb)
			out = append(out, nb)
		}
	}
	return out
}
