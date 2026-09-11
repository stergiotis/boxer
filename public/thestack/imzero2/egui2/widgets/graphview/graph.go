package graphview

import (
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// graph is the retained struct-of-arrays state the per-frame declaration is
// reconciled against (ADR-0224 §SD1). Nodes live in slots; a slot survives
// as long as its id is re-declared, so positions the layout or the user set
// persist. Edges are rebuilt from the declaration every frame — they carry
// no widget-owned state — as slot pairs plus an undirected CSR adjacency for
// the attraction pass.
type graph struct {
	ids    []uint64
	slot   map[uint64]int32
	x, y   []float32
	label  []string
	col    []color.Color
	radius []float32
	donut  []Donut
	seen   []uint32 // frame stamp of the last declaration that named the slot
	// fixed is recomputed every frame: the force step leaves a fixed node
	// alone. Its sources are a declared pin, a widget-side hold and the drag.
	fixed   []bool
	pinDecl []bool // Pinned in this frame's declaration
	pinX    []float32
	pinY    []float32
	held    []bool // held where the user dropped it (Options.PinOnDrag / PinNode)

	eFrom, eTo []int32
	eLabel     []string
	eCol       []color.Color
	eWidth     []float32
	eOrder     []uint8 // index among parallel edges of the same ordered pair

	adjStart []int32 // n+1 offsets into adjList
	adjList  []int32 // undirected neighbours, one entry per edge end
	inDeg    []int32

	frame       uint32
	topoHash    uint64
	pinnedCount uint32 // declared or widget-side pins, as of the last applyPins

	// scratch
	pairCount map[[2]int32]uint8
	newSlots  []int32
	pending   []int32 // indices into the declaration of ids not yet known
	csrCursor []int32
	mark      []bool // per-slot scratch for the placement passes
}

func (g *graph) n() int { return len(g.ids) }

// reconcile applies one frame's declaration. It returns the slots created
// this frame (for placement) and whether the topology — the node set or the
// edge set — differs from the previous frame's. Known ids are updated in
// place, vanished ids are swap-removed, and only then are the new ids
// appended, so the created indices are final. When the topology is
// unchanged the edge structure is kept and only the edge attributes are
// copied.
func (g *graph) reconcile(nodes []NodeSpec, edges []EdgeSpec) (created []int32, topoChanged bool) {
	if g.slot == nil {
		g.slot = make(map[uint64]int32, len(nodes))
		g.pairCount = make(map[[2]int32]uint8, len(edges))
	}
	g.frame++
	g.newSlots = g.newSlots[:0]
	g.pending = g.pending[:0]
	var hash uint64
	for i := range nodes {
		sp := &nodes[i]
		hash += mix64(sp.Id)
		s, ok := g.slot[sp.Id]
		if !ok {
			g.pending = append(g.pending, int32(i))
			continue
		}
		g.seen[s] = g.frame
		g.setNode(s, sp)
	}
	// Drop slots the declaration no longer names. Swap-remove from the back
	// so every index below the cursor stays valid.
	for s := len(g.ids) - 1; s >= 0; s-- {
		if g.seen[s] != g.frame {
			g.removeSlot(s)
		}
	}
	for _, i := range g.pending {
		sp := &nodes[i]
		if s, dup := g.slot[sp.Id]; dup {
			// The same id twice in one declaration: the later spec wins.
			g.setNode(s, sp)
			continue
		}
		s := int32(len(g.ids))
		g.slot[sp.Id] = s
		g.ids = append(g.ids, sp.Id)
		g.x = append(g.x, 0)
		g.y = append(g.y, 0)
		g.label = append(g.label, "")
		g.col = append(g.col, color.Color{})
		g.radius = append(g.radius, 0)
		g.donut = append(g.donut, Donut{})
		g.seen = append(g.seen, g.frame)
		g.fixed = append(g.fixed, false)
		g.pinDecl = append(g.pinDecl, false)
		g.pinX = append(g.pinX, 0)
		g.pinY = append(g.pinY, 0)
		g.held = append(g.held, false)
		g.newSlots = append(g.newSlots, s)
		g.setNode(s, sp)
	}
	created = g.newSlots

	// Edges: the hash covers the edges between known ids, in either
	// direction distinctly; a re-added id changes the node set even when
	// the hash agrees, so created counts as a change too.
	for i := range edges {
		e := &edges[i]
		_, okF := g.slot[e.From]
		_, okT := g.slot[e.To]
		if okF && okT {
			hash += mix64(e.From ^ mix64(e.To))
		}
	}
	topoChanged = hash != g.topoHash || len(created) > 0
	g.topoHash = hash
	if topoChanged {
		g.rebuildEdges(edges)
	} else {
		j := 0
		for i := range edges {
			e := &edges[i]
			_, okF := g.slot[e.From]
			_, okT := g.slot[e.To]
			if !okF || !okT {
				continue
			}
			g.eLabel[j] = e.Label
			g.eCol[j] = e.Color
			g.eWidth[j] = e.Width
			j++
		}
	}
	return
}

// setNode copies a spec's per-frame attributes into slot s.
func (g *graph) setNode(s int32, sp *NodeSpec) {
	g.label[s] = sp.Label
	g.col[s] = sp.Color
	g.radius[s] = sp.Radius
	g.donut[s] = sp.Donut
	g.pinDecl[s] = sp.Pinned
	g.pinX[s] = sp.PinX
	g.pinY[s] = sp.PinY
}

// rebuildEdges rebuilds the edge arrays, the parallel-edge orders, the
// in-degrees and the undirected CSR adjacency from the declaration.
func (g *graph) rebuildEdges(edges []EdgeSpec) {
	n := len(g.ids)
	g.eFrom = g.eFrom[:0]
	g.eTo = g.eTo[:0]
	g.eLabel = g.eLabel[:0]
	g.eCol = g.eCol[:0]
	g.eWidth = g.eWidth[:0]
	g.eOrder = g.eOrder[:0]
	clear(g.pairCount)
	g.inDeg = growTo(g.inDeg, n)
	clear(g.inDeg)
	deg := growTo(g.adjStart, n+1)
	clear(deg)
	for i := range edges {
		e := &edges[i]
		from, okF := g.slot[e.From]
		to, okT := g.slot[e.To]
		if !okF || !okT {
			continue
		}
		key := [2]int32{from, to}
		order := g.pairCount[key]
		g.pairCount[key] = order + 1
		g.eFrom = append(g.eFrom, from)
		g.eTo = append(g.eTo, to)
		g.eLabel = append(g.eLabel, e.Label)
		g.eCol = append(g.eCol, e.Color)
		g.eWidth = append(g.eWidth, e.Width)
		g.eOrder = append(g.eOrder, order)
		g.inDeg[to]++
		if from != to {
			deg[from]++
			deg[to]++
		}
	}
	// CSR from degrees: adjStart[i] = sum of degrees below i.
	var acc int32
	for i := 0; i < n; i++ {
		d := deg[i]
		deg[i] = acc
		acc += d
	}
	deg[n] = acc
	g.adjStart = deg
	g.adjList = growTo(g.adjList, int(acc))
	g.csrCursor = growTo(g.csrCursor, n)
	cur := g.csrCursor
	copy(cur, g.adjStart[:n])
	for i := range g.eFrom {
		f, t := g.eFrom[i], g.eTo[i]
		if f == t {
			continue
		}
		g.adjList[cur[f]] = t
		cur[f]++
		g.adjList[cur[t]] = f
		cur[t]++
	}
}

// allSlots returns every slot index, in the shared newSlots scratch.
func (g *graph) allSlots() []int32 {
	g.newSlots = g.newSlots[:0]
	for i := range g.ids {
		g.newSlots = append(g.newSlots, int32(i))
	}
	return g.newSlots
}

// removeSlot drops slot s by moving the last slot into its place.
func (g *graph) removeSlot(s int) {
	last := len(g.ids) - 1
	delete(g.slot, g.ids[s])
	if s != last {
		g.ids[s] = g.ids[last]
		g.x[s] = g.x[last]
		g.y[s] = g.y[last]
		g.label[s] = g.label[last]
		g.col[s] = g.col[last]
		g.radius[s] = g.radius[last]
		g.donut[s] = g.donut[last]
		g.seen[s] = g.seen[last]
		g.fixed[s] = g.fixed[last]
		g.pinDecl[s] = g.pinDecl[last]
		g.pinX[s] = g.pinX[last]
		g.pinY[s] = g.pinY[last]
		g.held[s] = g.held[last]
		g.slot[g.ids[s]] = int32(s)
	}
	g.ids = g.ids[:last]
	g.x = g.x[:last]
	g.y = g.y[:last]
	g.label = g.label[:last]
	g.col = g.col[:last]
	g.radius = g.radius[:last]
	g.donut = g.donut[:last]
	g.seen = g.seen[:last]
	g.fixed = g.fixed[:last]
	g.pinDecl = g.pinDecl[:last]
	g.pinX = g.pinX[:last]
	g.pinY = g.pinY[:last]
	g.held = g.held[:last]
}

// applyPins moves every declared-pinned node to its pin and recomputes
// fixed. dragSlot, when non-negative, is the node under the user's drag: it
// keeps the dragged position this frame so the pin does not snap it back
// mid-gesture, and it is fixed like the others.
func (g *graph) applyPins(dragSlot int32) {
	g.pinnedCount = 0
	for i := range g.ids {
		if g.pinDecl[i] && int32(i) != dragSlot {
			g.x[i], g.y[i] = g.pinX[i], g.pinY[i]
		}
		pinned := g.pinDecl[i] || g.held[i]
		if pinned {
			g.pinnedCount++
		}
		g.fixed[i] = pinned || int32(i) == dragSlot
	}
}

// isPinned reports whether slot s is fixed by a pin or a hold, as opposed to
// only by the drag in flight.
func (g *graph) isPinned(s int) bool { return g.pinDecl[s] || g.held[s] }

// neighbors yields the undirected neighbour slots of slot s.
func (g *graph) neighbors(s int32) []int32 {
	return g.adjList[g.adjStart[s]:g.adjStart[s+1]]
}

// bounds returns the axis-aligned box around every node centre, padded by
// each node's radius. ok is false for an empty graph.
func (g *graph) bounds(defaultRadius float32) (minX, minY, maxX, maxY float32, ok bool) {
	if len(g.ids) == 0 {
		return
	}
	minX, minY = g.x[0], g.y[0]
	maxX, maxY = minX, minY
	for i := range g.ids {
		r := g.radius[i]
		if r <= 0 {
			r = defaultRadius
		}
		minX = min(minX, g.x[i]-r)
		maxX = max(maxX, g.x[i]+r)
		minY = min(minY, g.y[i]-r)
		maxY = max(maxY, g.y[i]+r)
	}
	ok = true
	return
}

// growTo returns s resliced to n, reallocating only when the capacity is
// short; the contents are unspecified.
func growTo[T any](s []T, n int) []T {
	if cap(s) < n {
		s = make([]T, n)
	}
	return s[:n]
}

// mix64 is splitmix64's finaliser: a bijective scrambler that turns an id
// into well-spread bits for deterministic placement and order-independent
// topology hashing. It is not a hash of arbitrary bytes, which is what xxh3
// is for.
func mix64(v uint64) uint64 {
	v ^= v >> 30
	v *= 0xbf58476d1ce4e5b9
	v ^= v >> 27
	v *= 0x94d049bb133111eb
	v ^= v >> 31
	return v
}

// unit01 maps a scrambled id to [0, 1).
func unit01(v uint64) float32 {
	return float32(v>>40) / float32(1<<24)
}
