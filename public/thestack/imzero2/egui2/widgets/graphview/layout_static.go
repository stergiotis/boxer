package graphview

import "slices"

// spawnSize is the side of the square random placement fills, egui_graphs'
// SPAWN_SIZE, in world units.
const spawnSize = 250

// placeRandom puts each of the given slots at a position derived from its
// id, so the same graph lays out the same way in every run (ADR-0224 §SD2).
func placeRandom(g *graph, slots []int32) {
	for _, s := range slots {
		h := mix64(g.ids[s])
		g.x[s] = unit01(h) * spawnSize
		g.y[s] = unit01(mix64(h)) * spawnSize
	}
}

// placeNear puts a newly declared node beside an already-placed neighbour
// when it has one, and randomly otherwise. A force layout then pulls it into
// place from somewhere plausible instead of from the origin.
func placeNear(g *graph, slots []int32, spread float32) {
	isNew := make(map[int32]bool, len(slots))
	for _, s := range slots {
		isNew[s] = true
	}
	for _, s := range slots {
		placed := false
		for _, nb := range g.neighbors(s) {
			if isNew[nb] {
				continue
			}
			h := mix64(g.ids[s])
			g.x[s] = g.x[nb] + (unit01(h)-0.5)*spread
			g.y[s] = g.y[nb] + (unit01(mix64(h))-0.5)*spread
			placed = true
			break
		}
		if !placed {
			placeRandom(g, []int32{s})
		}
	}
}

// layoutHierarchical is egui_graphs' tree walk: roots are the nodes with no
// incoming edge, each root's subtree is laid out depth-first with children
// on consecutive columns, and forests pack left to right. Nodes a walk
// cannot reach from a root — cycles — start their own tree afterwards.
// Roots and children are visited in id order so the result is a function of
// the topology alone.
func layoutHierarchical(g *graph, p HierParams) {
	n := g.n()
	if n == 0 {
		return
	}
	order := make([]int32, n)
	for i := range order {
		order[i] = int32(i)
	}
	slices.SortFunc(order, func(a, b int32) int { return cmpU64(g.ids[a], g.ids[b]) })

	// Outgoing adjacency in CSR form, children in id order, so the walk is
	// linear in nodes plus edges and independent of declaration order.
	outStart := make([]int32, n+1)
	for i := range g.eFrom {
		if g.eFrom[i] != g.eTo[i] {
			outStart[g.eFrom[i]+1]++
		}
	}
	for i := 0; i < n; i++ {
		outStart[i+1] += outStart[i]
	}
	outList := make([]int32, outStart[n])
	cursor := make([]int32, n)
	copy(cursor, outStart[:n])
	for i := range g.eFrom {
		f, t := g.eFrom[i], g.eTo[i]
		if f == t {
			continue
		}
		outList[cursor[f]] = t
		cursor[f]++
	}
	for s := 0; s < n; s++ {
		kids := outList[outStart[s]:outStart[s+1]]
		slices.SortFunc(kids, func(a, b int32) int { return cmpU64(g.ids[a], g.ids[b]) })
	}

	visited := make([]bool, n)
	h := hierWalk{g: g, p: p, visited: visited, outStart: outStart, outList: outList}
	nextCol := float32(0)
	for _, s := range order {
		if g.inDeg[s] != 0 || visited[s] {
			continue
		}
		nextCol = h.tree(s, 0, nextCol) + 1
	}
	for _, s := range order {
		if visited[s] {
			continue
		}
		nextCol = h.tree(s, 0, nextCol) + 1
	}
}

type hierWalk struct {
	g        *graph
	p        HierParams
	visited  []bool
	outStart []int32
	outList  []int32
}

// tree lays out the subtree under s starting at row and column startCol and
// returns the rightmost column it used.
func (h *hierWalk) tree(s int32, row int, startCol float32) (maxCol float32) {
	g := h.g
	h.visited[s] = true
	maxCol = startCol
	childCol := startCol
	placedKids := 0
	for _, k := range h.outList[h.outStart[s]:h.outStart[s+1]] {
		if h.visited[k] {
			continue
		}
		h.visited[k] = true
		cm := h.tree(k, row+1, childCol)
		maxCol = max(maxCol, cm)
		childCol = cm + 1
		placedKids++
	}
	col := startCol
	if h.p.CenterParent && placedKids > 0 {
		col = (startCol + maxCol) / 2
	}
	var x, y float32
	switch h.p.Orientation {
	case OrientationLeftRight:
		x = float32(row) * h.p.RowDist
		y = col * h.p.ColDist
	default:
		x = col * h.p.ColDist
		y = float32(row) * h.p.RowDist
	}
	g.x[s], g.y[s] = x, y
	return maxCol
}

func cmpU64(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
