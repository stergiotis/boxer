package graphview

import "math"

// pickGrid is a uniform grid over world space holding every node's slot in
// the cell of its centre, in CSR form. It is rebuilt when graph.posVer
// moves — a slot, position or radius changed — and reused otherwise, so a
// graph at rest picks in the cells around the pointer rather than over
// every node (ADR-0224 §SD3). A live force layout moves every frame and
// rebuilds every frame, which is one counting sort: no worse than the scan
// it replaces.
type pickGrid struct {
	ver        uint32
	n          int
	radius     float32 // the style default the build used
	minX, minY float32
	cell       float32
	cols, rows int32
	start      []int32 // cols·rows+1 offsets into items
	items      []int32 // slot indices, grouped by cell
	maxR       float32 // widest node radius in world units
	cellOf     []int32 // scratch: cell per slot
}

// pickGridMaxCellsPerNode bounds the cell count for degenerate bounds — a
// long thin graph would otherwise get a cell per unit of its length.
const pickGridMaxCellsPerNode = 8

// stale reports whether the grid no longer describes the graph.
func (pg *pickGrid) stale(g *graph, defaultRadius float32) bool {
	return pg.ver != g.posVer || pg.n != g.n() || pg.radius != defaultRadius
}

// build sorts every slot into its cell. The cell is sized for about one
// node per cell, and never under twice the widest radius so a query for
// one disc touches few cells.
func (pg *pickGrid) build(g *graph, defaultRadius float32) {
	n := g.n()
	pg.ver, pg.n, pg.radius = g.posVer, n, defaultRadius
	pg.cols, pg.rows = 0, 0
	if n == 0 {
		return
	}
	minX, minY, maxX, maxY, _ := g.bounds(defaultRadius)
	maxR := float32(0)
	for i := range g.ids {
		r := g.radius[i]
		if r <= 0 {
			r = defaultRadius
		}
		maxR = max(maxR, r)
	}
	pg.maxR = maxR
	w, h := maxX-minX, maxY-minY
	cell := float32(math.Sqrt(float64(max(w*h, 1) / float32(n))))
	cell = max(cell, 2*maxR, 1e-3)
	cols := int32(w/cell) + 1
	rows := int32(h/cell) + 1
	for int(cols)*int(rows) > pickGridMaxCellsPerNode*n+64 {
		cell *= 2
		cols = int32(w/cell) + 1
		rows = int32(h/cell) + 1
	}
	pg.minX, pg.minY, pg.cell, pg.cols, pg.rows = minX, minY, cell, cols, rows
	cells := int(cols) * int(rows)
	pg.start = growTo(pg.start, cells+1)
	clear(pg.start)
	pg.cellOf = growTo(pg.cellOf, n)
	for i := range g.ids {
		c := pg.cellIndex(g.x[i], g.y[i])
		pg.cellOf[i] = c
		pg.start[c+1]++
	}
	for c := 0; c < cells; c++ {
		pg.start[c+1] += pg.start[c]
	}
	pg.items = growTo(pg.items, n)
	// Fill from each cell's start, walking the start offsets forward and
	// restoring them after, so no second scratch array is needed.
	for i := range g.ids {
		c := pg.cellOf[i]
		pg.items[pg.start[c]] = int32(i)
		pg.start[c]++
	}
	for c := cells; c > 0; c-- {
		pg.start[c] = pg.start[c-1]
	}
	pg.start[0] = 0
}

// cellCoords maps a world point to its clamped cell column and row.
func (pg *pickGrid) cellCoords(x, y float32) (cx, cy int32) {
	cx = int32((x - pg.minX) / pg.cell)
	cy = int32((y - pg.minY) / pg.cell)
	cx = min(max(cx, 0), pg.cols-1)
	cy = min(max(cy, 0), pg.rows-1)
	return
}

func (pg *pickGrid) cellIndex(x, y float32) int32 {
	cx, cy := pg.cellCoords(x, y)
	return cy*pg.cols + cx
}

// pickNode returns the slot under canvas point (px, py): the nearest node
// whose screen disc — donut ring included, floored at pickMinPx — contains
// it, or -1. Candidates come from the grid cells within the widest disc any
// node can present, in world units at the current zoom; the test on each is
// exact (ADR-0224 §SD3). A node declared NoPick is skipped, so the pointer
// reaches whatever is behind it (§SD14); the grid still holds it, since
// NoPick changes per frame and the grid is keyed on positions.
func (v *View) pickNode(px, py float32) int32 {
	n := v.g.n()
	if n == 0 {
		return -1
	}
	if v.grid.stale(&v.g, v.style.NodeRadius) {
		v.grid.build(&v.g, v.style.NodeRadius)
	}
	pg := &v.grid
	reachPx := max(pg.maxR*v.cam.Zoom+v.style.DonutWidth, pickMinPx)
	reach := reachPx / v.cam.Zoom
	wx, wy := v.cam.ToWorld(px, py)
	// Outside the padded bounds no disc can contain the point.
	if wx < pg.minX-reach || wy < pg.minY-reach ||
		wx > pg.minX+float32(pg.cols)*pg.cell+reach || wy > pg.minY+float32(pg.rows)*pg.cell+reach {
		return -1
	}
	cx0, cy0 := pg.cellCoords(wx-reach, wy-reach)
	cx1, cy1 := pg.cellCoords(wx+reach, wy+reach)
	best, bestD := int32(-1), float32(math.MaxFloat32)
	for cy := cy0; cy <= cy1; cy++ {
		for cx := cx0; cx <= cx1; cx++ {
			c := cy*pg.cols + cx
			for _, i := range pg.items[pg.start[c]:pg.start[c+1]] {
				if v.g.noPick[i] {
					continue
				}
				sx, sy := v.cam.ToScreen(v.g.x[i], v.g.y[i])
				r := max(v.nodeOuterPx(int(i)), pickMinPx)
				dx, dy := px-sx, py-sy
				d2 := dx*dx + dy*dy
				if d2 <= r*r && d2 < bestD {
					best, bestD = i, d2
				}
			}
		}
	}
	return best
}

// edgeBoxMayContain reports whether canvas point (px, py) lies within the
// box of edge i's endpoints on screen, padded by everything the stroke can
// reach past them: the pick tolerance, the arrow head, the node radius the
// stroke is trimmed by (which carries it past the far endpoint when the
// discs overlap), a parallel edge's bulge or a self-loop's height. A false
// answer is certain; a true one is resolved by the geometry.
func (v *View) edgeBoxMayContain(i int32, px, py float32) bool {
	f, t := v.g.eFrom[i], v.g.eTo[i]
	x1, y1 := v.cam.ToScreen(v.g.x[f], v.g.y[f])
	x2, y2 := v.cam.ToScreen(v.g.x[t], v.g.y[t])
	width := v.g.eWidth[i]
	if width <= 0 {
		width = v.style.EdgeWidth
	}
	order := float32(v.g.eOrder[i])
	pad := max(width, pickEdgeTolPx) + v.style.TipSize
	if f == t {
		pad += v.nodeRadius(int(f)) * v.cam.Zoom * (v.style.LoopSize + order)
	} else {
		pad += max(v.nodeRadius(int(f)), v.nodeRadius(int(t)))*v.cam.Zoom + v.style.CurveSize*order*v.cam.Zoom
	}
	return px >= min(x1, x2)-pad && px <= max(x1, x2)+pad &&
		py >= min(y1, y2)-pad && py <= max(y1, y2)+pad
}
