package graphview

import (
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// graph is the retained struct-of-arrays state the per-frame declaration is
// reconciled against (ADR-0224 §SD1). Nodes live in slots, and the slot
// order is the declaration's row order (ADR-0232 §SD3): a declaration in the
// same order as last frame's costs a linear compare, one that adds, drops or
// reorders ids is a topology change that permutes the retained per-id state
// — positions and widget-side holds — into the new order. Edges are rebuilt
// from the declaration on a topology change — they carry no widget-owned
// state — as slot pairs plus an undirected CSR adjacency for the attraction
// pass, and only their attributes are copied otherwise.
//
// The declaration reaches the reconcile as columns whichever form the caller
// used: the row form is rewritten into the declN / declE scratch first, its
// zero-as-default fields resolved to the columnar unset value, so the two
// forms share one reconcile and one retained state.
type graph struct {
	ids    []uint64
	slot   map[uint64]int32
	x, y   []float32
	label  []string
	col    []color.Color
	radius []float32 // NaN when unset: the style default
	donut  []Donut
	// opacity is resolved to 1 when unset; noPick takes the pointer away
	// from the node entirely (ADR-0224 §SD14).
	opacity     []float32
	noPick      []bool
	labelAlways []bool
	pull        []Pull
	// anyPull is recomputed every reconcile: with no pull declared the step
	// skips its pass entirely, so an unpulled graph is unchanged to the bit.
	anyPull bool
	// fixed is recomputed every frame: the force step leaves a fixed node
	// alone. Its sources are a declared pin, a widget-side hold and the drag.
	fixed   []bool
	pinDecl []bool // pinned in this frame's declaration
	pinX    []float32
	pinY    []float32
	held    []bool // held where the user dropped it (Options.PinOnDrag / PinNode)

	eFrom, eTo []int32
	eId        []uint64
	eLabel     []string
	eCol       []color.Color
	eWidth     []float32 // NaN when unset: the style default
	eLen       []float32 // ideal-length multiplier, resolved to 1 when unset
	eStr       []float32 // attraction multiplier, resolved to 1 when unset
	eOpacity   []float32 // resolved to 1 when unset (ADR-0224 §SD14)
	eNoPick    []bool
	eOrder     []uint8 // index among parallel edges of the same ordered pair

	adjStart []int32 // n+1 offsets into adjList
	adjList  []int32 // undirected neighbours, one entry per edge end
	adjEdge  []int32 // the edge index behind each adjList entry
	inDeg    []int32
	edgeIdx  map[EdgeRef]int32 // first edge index per ref, rebuilt with the edges

	frame       uint32
	topoHash    uint64
	pinnedCount uint32 // declared or widget-side pins, as of the last applyPins
	// posVer counts the changes to the slot set, a position or a radius —
	// everything the pick grid is built from. Every writer bumps it.
	posVer uint32
	// forceVer counts the declared changes the force step reads but the
	// topology hash does not cover — a pull, an edge length or strength —
	// so a PauseOnSettle hold wakes for them.
	forceVer uint32

	// rowSlot maps each row of this frame's declaration to its slot, valid
	// until the next reconcile; the aura table is built from it.
	rowSlot []int32

	// declN and declE hold the row form's columns (columns.go).
	declN NodeColumns
	declE EdgeColumns

	// scratch
	pairCount map[[2]int32]uint8
	newSlots  []int32
	slot2     map[uint64]int32 // the next slot map while the layout changes
	x2, y2    []float32        // the permuted positions
	held2     []bool
	csrCursor []int32
	mark      []bool // per-slot scratch for the placement passes
}

func (g *graph) n() int { return len(g.ids) }

// reconcile applies one frame's row-shaped declaration: the row form is
// rewritten into columns and reconciled as such.
func (g *graph) reconcile(nodes []NodeSpec, edges []EdgeSpec) (created []int32, topoChanged bool) {
	g.declN.fromSpecs(nodes)
	g.declE.fromSpecs(edges)
	return g.reconcileColumns(&g.declN, &g.declE)
}

// reconcileColumns applies one frame's declaration. It returns the slots
// created this frame (for placement) and whether the topology — the node
// set, its order, or the edge set — differs from the previous frame's. When
// the ids are last frame's in last frame's order the slots stay and the
// attributes are copied in place; otherwise the slots are laid out afresh
// in declaration order, each surviving id carrying its position and hold
// into its new slot, and the created indices are final. When the topology
// is unchanged the edge structure is kept and only the edge attributes are
// copied.
func (g *graph) reconcileColumns(nc *NodeColumns, ec *EdgeColumns) (created []int32, topoChanged bool) {
	if g.slot == nil {
		g.slot = make(map[uint64]int32, len(nc.Ids))
		g.slot2 = make(map[uint64]int32, len(nc.Ids))
		g.pairCount = make(map[[2]int32]uint8, len(ec.From))
		g.edgeIdx = make(map[EdgeRef]int32, len(ec.From))
	}
	g.frame++
	g.anyPull = false
	g.newSlots = g.newSlots[:0]
	rows := len(nc.Ids)
	g.rowSlot = growTo(g.rowSlot, rows)

	// The common case: the same ids in the same order. A duplicate id makes
	// the row count exceed the slot count, so it falls through to the
	// general path, where the later row wins.
	var hash uint64
	same := rows == len(g.ids)
	for i, id := range nc.Ids {
		hash += mix64(id)
		if same && g.ids[i] != id {
			same = false
		}
	}
	if same {
		for i := range rows {
			g.rowSlot[i] = int32(i)
		}
	} else {
		g.relayout(nc.Ids)
	}
	created = g.newSlots
	for i := range rows {
		g.setNode(g.rowSlot[i], nc, i)
	}

	// Edges: the hash covers the edges between known ids, in either
	// direction distinctly; a re-added id changes the node set even when
	// the hash agrees, so created counts as a change too.
	for i := range ec.From {
		_, okF := g.slot[ec.From[i]]
		_, okT := g.slot[ec.To[i]]
		if okF && okT {
			hash += mix64(ec.From[i] ^ mix64(ec.To[i]^mix64(colU64(ec.Id, i))))
		}
	}
	topoChanged = !same || hash != g.topoHash || len(created) > 0
	g.topoHash = hash
	if topoChanged {
		g.rebuildEdges(ec)
	} else {
		j := 0
		for i := range ec.From {
			_, okF := g.slot[ec.From[i]]
			_, okT := g.slot[ec.To[i]]
			if !okF || !okT {
				continue
			}
			g.eLabel[j] = colStr(ec.Label, i)
			g.eCol[j] = colColor(ec.Color, i)
			g.eWidth[j] = colF32(ec.Width, i)
			if l, st := lengthOr1(colF32(ec.Length, i)), strengthOr1(colF32(ec.Strength, i)); l != g.eLen[j] || st != g.eStr[j] {
				g.eLen[j], g.eStr[j] = l, st
				g.forceVer++
			}
			g.eOpacity[j] = opacityOr1(colF32(ec.Opacity, i))
			g.eNoPick[j] = colBool(ec.NoPick, i)
			j++
		}
	}
	return
}

// relayout assigns the slots afresh in declaration order — the unique ids
// in first-row order — carrying every surviving id's position and hold into
// its new slot, and records the created slots and each row's slot. The
// old slot map becomes the scratch for next time.
func (g *graph) relayout(ids []uint64) {
	next := g.slot2
	clear(next)
	n := 0
	for i, id := range ids {
		if s, dup := next[id]; dup {
			g.rowSlot[i] = s
			continue
		}
		s := int32(n)
		next[id] = s
		g.rowSlot[i] = s
		n++
	}
	g.x2 = growTo(g.x2, n)
	g.y2 = growTo(g.y2, n)
	g.held2 = growTo(g.held2, n)
	for i, id := range ids {
		s := g.rowSlot[i]
		if int(s) >= n {
			continue
		}
		if old, known := g.slot[id]; known {
			g.x2[s], g.y2[s], g.held2[s] = g.x[old], g.y[old], g.held[old]
			continue
		}
		g.x2[s], g.y2[s], g.held2[s] = 0, 0, false
	}
	// Each created slot once, in slot order: a duplicate row of a new id
	// shares its first row's slot.
	seenRow := int32(-1)
	for i, id := range ids {
		s := g.rowSlot[i]
		if s <= seenRow {
			continue
		}
		seenRow = s
		if _, known := g.slot[id]; !known {
			g.newSlots = append(g.newSlots, s)
		}
	}
	g.slot, g.slot2 = next, g.slot
	g.x, g.x2 = g.x2, g.x
	g.y, g.y2 = g.y2, g.y
	g.held, g.held2 = g.held2, g.held
	g.ids = growTo(g.ids, n)
	for id, s := range g.slot {
		g.ids[s] = id
	}
	g.label = growTo(g.label, n)
	g.col = growTo(g.col, n)
	g.radius = growTo(g.radius, n)
	g.donut = growTo(g.donut, n)
	g.opacity = growTo(g.opacity, n)
	g.noPick = growTo(g.noPick, n)
	g.labelAlways = growTo(g.labelAlways, n)
	g.pull = growTo(g.pull, n)
	g.fixed = growTo(g.fixed, n)
	clear(g.fixed)
	g.pinDecl = growTo(g.pinDecl, n)
	g.pinX = growTo(g.pinX, n)
	g.pinY = growTo(g.pinY, n)
	// Every attribute below is set by setNode this frame; the change
	// detectors it runs against must not read a previous occupant's value
	// as this slot's, so the slots start unset.
	for s := range n {
		g.radius[s] = nan32
		g.pull[s] = Pull{}
	}
	g.posVer++
}

// setNode copies row i's attributes into slot s.
func (g *graph) setNode(s int32, nc *NodeColumns, i int) {
	g.label[s] = colStr(nc.Label, i)
	g.col[s] = colColor(nc.Color, i)
	if r := colF32(nc.Radius, i); !sameF32(g.radius[s], r) {
		g.radius[s] = r
		g.posVer++
	}
	g.donut[s] = donutAt(nc, i)
	g.opacity[s] = opacityOr1(colF32(nc.Opacity, i))
	g.noPick[s] = colBool(nc.NoPick, i)
	g.labelAlways[s] = colBool(nc.LabelAlways, i)
	pl := pullAt(nc, i)
	if g.pull[s] != pl {
		g.pull[s] = pl
		g.forceVer++
	}
	if !pl.IsZero() {
		g.anyPull = true
	}
	px, py := colF32(nc.PinX, i), colF32(nc.PinY, i)
	if g.pinDecl[s] = !isNaN32(px) && !isNaN32(py); g.pinDecl[s] {
		g.pinX[s], g.pinY[s] = px, py
	} else {
		g.pinX[s], g.pinY[s] = 0, 0
	}
}

// donutAt is row i's donut: its slice of the values, colours and total.
func donutAt(nc *NodeColumns, i int) (d Donut) {
	if nc.DonutOffsets == nil {
		return
	}
	lo, hi := nc.DonutOffsets[i], nc.DonutOffsets[i+1]
	if lo >= hi {
		return
	}
	d.Values = nc.DonutValues[lo:hi:hi]
	if nc.DonutColors != nil {
		d.Colors = nc.DonutColors[lo:hi:hi]
	}
	if t := colF32(nc.DonutTotal, i); !isNaN32(t) {
		d.Total = t
	}
	return
}

// pullAt is row i's pull: an axis pulls when its target and strength are
// both declared and the strength is not zero; the target of an axis that
// does not pull is kept as declared, 0 when absent, so the retained value
// is the row form's to the bit.
func pullAt(nc *NodeColumns, i int) (p Pull) {
	x, y := colF32(nc.PullX, i), colF32(nc.PullY, i)
	sx, sy := colF32(nc.PullStrengthX, i), colF32(nc.PullStrengthY, i)
	if !isNaN32(x) {
		p.X = x
	}
	if !isNaN32(y) {
		p.Y = y
	}
	if !isNaN32(x) && !isNaN32(sx) {
		p.StrengthX = sx
	}
	if !isNaN32(y) && !isNaN32(sy) {
		p.StrengthY = sy
	}
	return
}

// rebuildEdges rebuilds the edge arrays, the parallel-edge orders, the
// in-degrees and the undirected CSR adjacency from the declaration.
func (g *graph) rebuildEdges(ec *EdgeColumns) {
	n := len(g.ids)
	g.eFrom = g.eFrom[:0]
	g.eTo = g.eTo[:0]
	g.eId = g.eId[:0]
	g.eLabel = g.eLabel[:0]
	g.eCol = g.eCol[:0]
	g.eWidth = g.eWidth[:0]
	g.eLen = g.eLen[:0]
	g.eStr = g.eStr[:0]
	g.eOpacity = g.eOpacity[:0]
	g.eNoPick = g.eNoPick[:0]
	g.eOrder = g.eOrder[:0]
	clear(g.pairCount)
	clear(g.edgeIdx)
	g.inDeg = growTo(g.inDeg, n)
	clear(g.inDeg)
	deg := growTo(g.adjStart, n+1)
	clear(deg)
	for i := range ec.From {
		from, okF := g.slot[ec.From[i]]
		to, okT := g.slot[ec.To[i]]
		if !okF || !okT {
			continue
		}
		key := [2]int32{from, to}
		order := g.pairCount[key]
		g.pairCount[key] = order + 1
		id := colU64(ec.Id, i)
		g.eFrom = append(g.eFrom, from)
		g.eTo = append(g.eTo, to)
		g.eId = append(g.eId, id)
		g.eLabel = append(g.eLabel, colStr(ec.Label, i))
		g.eCol = append(g.eCol, colColor(ec.Color, i))
		g.eWidth = append(g.eWidth, colF32(ec.Width, i))
		g.eLen = append(g.eLen, lengthOr1(colF32(ec.Length, i)))
		g.eStr = append(g.eStr, strengthOr1(colF32(ec.Strength, i)))
		g.eOpacity = append(g.eOpacity, opacityOr1(colF32(ec.Opacity, i)))
		g.eNoPick = append(g.eNoPick, colBool(ec.NoPick, i))
		g.eOrder = append(g.eOrder, order)
		ref := EdgeRef{From: ec.From[i], To: ec.To[i], Id: id}
		if _, dup := g.edgeIdx[ref]; !dup {
			g.edgeIdx[ref] = int32(len(g.eFrom) - 1)
		}
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
	g.adjEdge = growTo(g.adjEdge, int(acc))
	g.csrCursor = growTo(g.csrCursor, n)
	cur := g.csrCursor
	copy(cur, g.adjStart[:n])
	for i := range g.eFrom {
		f, t := g.eFrom[i], g.eTo[i]
		if f == t {
			continue
		}
		g.adjList[cur[f]] = t
		g.adjEdge[cur[f]] = int32(i)
		cur[f]++
		g.adjList[cur[t]] = f
		g.adjEdge[cur[t]] = int32(i)
		cur[t]++
	}
}

// edgeRef names edge i by its endpoint ids and declared id.
func (g *graph) edgeRef(i int32) EdgeRef {
	return EdgeRef{From: g.ids[g.eFrom[i]], To: g.ids[g.eTo[i]], Id: g.eId[i]}
}

// findEdge returns the index of the first edge matching ref, or -1.
func (g *graph) findEdge(ref EdgeRef) int32 {
	if i, ok := g.edgeIdx[ref]; ok {
		return i
	}
	return -1
}

// lengthOr1 resolves a declared ideal-length multiplier: unset, and any
// value that is not positive — a zero ideal length has no meaning — is 1.
func lengthOr1(v float32) float32 {
	if v > 0 {
		return v
	}
	return 1
}

// strengthOr1 resolves a declared attraction multiplier: unset is 1, a
// declared value is the value, and a negative one — a spring that pushes —
// is 0 (ADR-0232 §SD4).
func strengthOr1(v float32) float32 {
	switch {
	case isNaN32(v):
		return 1
	case v < 0:
		return 0
	}
	return v
}

// opacityOr1 resolves a declared opacity (ADR-0224 §SD14, ADR-0232 §SD4).
// NaN is the unset value and paints as declared, as does anything at or
// above 1; a value in between fades, and a declared 0 paints nothing. The
// row form has no 0 to declare — its zero is the unset value and reaches
// here as NaN — which is why fully transparent has no spelling there.
func opacityOr1(v float32) float32 {
	switch {
	case isNaN32(v) || v >= 1:
		return 1
	case v < 0:
		return 0
	}
	return v
}

// allSlots returns every slot index, in the shared newSlots scratch.
func (g *graph) allSlots() []int32 {
	g.newSlots = g.newSlots[:0]
	for i := range g.ids {
		g.newSlots = append(g.newSlots, int32(i))
	}
	return g.newSlots
}

// applyPins moves every declared-pinned node to its pin and recomputes
// fixed, reporting whether any node moved. dragSlot, when non-negative, is
// the node under the user's drag: it keeps the dragged position this frame
// so the pin does not snap it back mid-gesture, and it is fixed like the
// others.
func (g *graph) applyPins(dragSlot int32) (moved bool) {
	g.pinnedCount = 0
	for i := range g.ids {
		if g.pinDecl[i] && int32(i) != dragSlot {
			if g.x[i] != g.pinX[i] || g.y[i] != g.pinY[i] {
				moved = true
			}
			g.x[i], g.y[i] = g.pinX[i], g.pinY[i]
		}
		pinned := g.pinDecl[i] || g.held[i]
		if pinned {
			g.pinnedCount++
		}
		g.fixed[i] = pinned || int32(i) == dragSlot
	}
	if moved {
		g.posVer++
	}
	return
}

// isPinned reports whether slot s is fixed by a pin or a hold, as opposed to
// only by the drag in flight.
func (g *graph) isPinned(s int) bool { return g.pinDecl[s] || g.held[s] }

// neighbors yields the undirected neighbour slots of slot s.
func (g *graph) neighbors(s int32) []int32 {
	return g.adjList[g.adjStart[s]:g.adjStart[s+1]]
}

// radiusOr is slot i's declared radius, or the default when unset.
func (g *graph) radiusOr(i int, defaultRadius float32) float32 {
	if r := g.radius[i]; !isNaN32(r) {
		return r
	}
	return defaultRadius
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
		r := g.radiusOr(i, defaultRadius)
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
