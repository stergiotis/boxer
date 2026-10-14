package graphview

import (
	"math"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/metrics"
)

// paint_batch.go holds the frame's batched geometry and the budget it is held
// to (ADR-0329). Straight edges go out as one segment mesh per stroke width,
// arrow heads as one triangle mesh, node discs as one mesh per (colour,
// radius) and pinned rings as one ring mesh per radius; what lies wholly
// outside the canvas is not sent at all. A picture past the vertex budget
// drops its decorations before its data — the pinned rings, then the arrow
// heads, then a stable 1-in-k sample of the edges — and says so through
// PaintStats.

// PaintStats is what the last paint drew and what it left out.
type PaintStats struct {
	// Nodes and Edges are what was drawn; the Culled counts lay wholly
	// outside the canvas and were not sent.
	Nodes, NodesCulled int
	Edges, EdgesCulled int
	// EdgeStride is 1 when every visible edge was drawn, k when only one in
	// k was, to stay under the budget.
	EdgeStride int
	// NoRings and NoTips report the decorations the budget dropped: the
	// pinned-node rings and the arrow heads.
	NoRings, NoTips bool
	// Vertices is the estimate the frame was judged by, Budget the line.
	Vertices, Budget int
}

// Coarse reports that the budget changed the picture this frame.
func (inst PaintStats) Coarse() bool {
	return inst.EdgeStride > 1 || inst.NoRings || inst.NoTips
}

// PaintStats returns what the last Render or HostedPaint drew.
func (v *View) PaintStats() PaintStats { return v.stats }

// defaultVertexBudget is the widget's budget when the host reports none — a
// host that has not answered fetchPaintBudget yet, or a test.
const defaultVertexBudget = 3_000_000

// hostBudgetShare is the part of the host's vertex budget a graph takes when
// Options.VertexBudget is unset. A graph is rarely alone on screen — hosted
// in a map, the map's own layers share the frame — so it leaves the rest.
const hostBudgetShare = 0.6

// budgetBackoff and budgetRecover move budgetScale: down at once when the
// host reports a frame past its budget (whatever drew it), back up slowly, so
// a picture that sits at the line does not flicker between two levels.
const (
	budgetBackoff  = 0.7
	budgetRecover  = 1.02
	budgetScaleMin = 0.05
)

// vertexBudget is this frame's budget for the graph's own geometry.
func (v *View) vertexBudget() int {
	b := v.Opts.VertexBudget
	if b <= 0 {
		if hb := metrics.Current.PaintBudget; hb > 0 {
			b = int(float64(hb) * hostBudgetShare)
		} else {
			b = defaultVertexBudget
		}
	}
	if v.budgetScale == 0 {
		v.budgetScale = 1
	}
	if metrics.Current.PaintOverBudget() {
		v.budgetScale = max(v.budgetScale*budgetBackoff, budgetScaleMin)
	} else if v.budgetScale < 1 {
		v.budgetScale = min(v.budgetScale*budgetRecover, 1)
	}
	return int(float64(b) * float64(v.budgetScale))
}

// discSegments mirrors the host's segment count for a mesh disc of radius r
// pixels (rust/imzero2 paint_budget.rs), so the estimate tracks what the host
// tessellates.
func discSegments(r float32) int {
	n := math.Ceil(float64(max(r, 0)) * 2 * math.Pi / 2.5)
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 6
	}
	return min(max(int(n), 6), 64)
}

// discVertices is the host's vertex count for one mesh disc.
func discVertices(r, outlineW float32, filled bool) int {
	ringed := outlineW > 0
	if ringed {
		r += outlineW / 2
	}
	n := discSegments(r)
	switch {
	case filled && ringed:
		return 1 + 4*n
	case filled:
		return 1 + 2*n
	case ringed:
		return 3 * n
	}
	return 0
}

// segBatch is one PaintSegments call: the straight edges of one stroke width.
type segBatch struct {
	width              float32
	x0s, y0s, x1s, y1s []float32
	cols               color.Colors
	edge               []int32 // the edge each segment draws, for thinning
}

func (b *segBatch) reset() {
	b.x0s, b.y0s, b.x1s, b.y1s = b.x0s[:0], b.y0s[:0], b.x1s[:0], b.y1s[:0]
	b.cols, b.edge = b.cols[:0], b.edge[:0]
}

// ringBatch is one ring-marker call: the pinned rings of one radius.
type ringBatch struct {
	radius float32
	xs, ys []float32
}

// widthKey quantises a stroke width to a quarter pixel, so continuously
// weighted edges still share a handful of batches.
func widthKey(w float32) int32 { return int32(math.Round(float64(w) * 4)) }

// ringKey quantises a ring radius to half a pixel.
func ringKey(r float32) int32 { return int32(math.Round(float64(r) * 2)) }

// beginBatches empties the frame's batches, keeping their storage.
func (v *View) beginBatches() {
	for i := range v.segs {
		v.segs[i].reset()
	}
	v.segs = v.segs[:0]
	clear(v.segIdx)
	v.tipXs, v.tipYs, v.tipCols, v.tipEdge = v.tipXs[:0], v.tipYs[:0], v.tipCols[:0], v.tipEdge[:0]
	for i := range v.rings {
		v.rings[i].xs, v.rings[i].ys = v.rings[i].xs[:0], v.rings[i].ys[:0]
	}
	v.rings = v.rings[:0]
	clear(v.ringIdx)
}

// addSegment appends one straight edge to the batch of its width.
func (v *View) addSegment(edge int, geo *edgeGeo, col color.Color) {
	k := widthKey(geo.width)
	bi, ok := v.segIdx[k]
	if !ok {
		bi = int32(len(v.segs))
		v.segIdx[k] = bi
		if cap(v.segs) > len(v.segs) {
			v.segs = v.segs[:len(v.segs)+1]
		} else {
			v.segs = append(v.segs, segBatch{})
		}
		v.segs[bi].width = float32(k) / 4
	}
	b := &v.segs[bi]
	b.x0s, b.y0s = append(b.x0s, geo.x[0]), append(b.y0s, geo.y[0])
	b.x1s, b.y1s = append(b.x1s, geo.x[3]), append(b.y1s, geo.y[3])
	b.cols = append(b.cols, col.Literal())
	b.edge = append(b.edge, int32(edge))
}

// addTip appends one arrow head to the triangle batch.
func (v *View) addTip(edge int, geo *edgeGeo, col color.Color) {
	size := geo.tipLen
	ax, ay := rotate(geo.tipDx, geo.tipDy, tipHalfAngle)
	bx, by := rotate(geo.tipDx, geo.tipDy, -tipHalfAngle)
	v.tipXs = append(v.tipXs, geo.tipX, geo.tipX-ax*size, geo.tipX-bx*size)
	v.tipYs = append(v.tipYs, geo.tipY, geo.tipY-ay*size, geo.tipY-by*size)
	v.tipCols = append(v.tipCols, col.Literal())
	v.tipEdge = append(v.tipEdge, int32(edge))
}

// addRing appends one pinned ring to the batch of its radius.
func (v *View) addRing(x, y, r float32) {
	k := ringKey(r)
	bi, ok := v.ringIdx[k]
	if !ok {
		bi = int32(len(v.rings))
		v.ringIdx[k] = bi
		if cap(v.rings) > len(v.rings) {
			v.rings = v.rings[:len(v.rings)+1]
		} else {
			v.rings = append(v.rings, ringBatch{})
		}
		v.rings[bi].radius = float32(k) / 2
	}
	b := &v.rings[bi]
	b.xs, b.ys = append(b.xs, x), append(b.ys, y)
}

// ringVertices is the estimate of every gathered ring.
func (v *View) ringVertices(w float32) (n int) {
	for i := range v.rings {
		n += len(v.rings[i].xs) * discVertices(v.rings[i].radius, w, false)
	}
	return
}

// segmentCount is the number of gathered straight edges.
func (v *View) segmentCount() (n int) {
	for i := range v.segs {
		n += len(v.segs[i].x0s)
	}
	return
}

// judge decides what the frame can afford: given the vertices the node discs
// and the other per-edge shapes already cost, it drops the rings, then the
// tips, then thins the edges, until the estimate is under the budget.
func (v *View) judge(fixed int, ringW float32) {
	budget := v.vertexBudget()
	rings := v.ringVertices(ringW)
	tips := 3 * len(v.tipCols)
	segs := 4 * v.segmentCount()
	st := &v.stats
	st.Budget = budget
	st.EdgeStride = 1
	total := fixed + rings + tips + segs
	if total > budget && rings > 0 {
		st.NoRings = true
		total -= rings
	}
	if total > budget && tips > 0 {
		st.NoTips = true
		total -= tips
	}
	if total > budget && segs > 0 {
		room := budget - (total - segs)
		if room <= 0 {
			st.EdgeStride = math.MaxInt32
		} else {
			st.EdgeStride = (segs + room - 1) / room
		}
		total -= segs - segs/st.EdgeStride
	}
	st.Vertices = total
}

// keepEdge is the thinning rule: an edge survives when its index is a
// multiple of the stride, so one stride draws the same edges frame to frame.
func (v *View) keepEdge(edge int32) bool {
	k := v.stats.EdgeStride
	return k <= 1 || int(edge)%k == 0
}

// flushEdges sends the gathered segment and tip batches, less what the
// budget left out.
func (v *View) flushEdges() {
	for i := range v.segs {
		b := &v.segs[i]
		if v.stats.EdgeStride > 1 {
			j := 0
			for k := range b.edge {
				if !v.keepEdge(b.edge[k]) {
					continue
				}
				b.x0s[j], b.y0s[j], b.x1s[j], b.y1s[j] = b.x0s[k], b.y0s[k], b.x1s[k], b.y1s[k]
				b.cols[j], b.edge[j] = b.cols[k], b.edge[k]
				j++
			}
			b.x0s, b.y0s, b.x1s, b.y1s, b.cols, b.edge = b.x0s[:j], b.y0s[:j], b.x1s[:j], b.y1s[:j], b.cols[:j], b.edge[:j]
		}
		if len(b.x0s) == 0 {
			continue
		}
		v.stats.Edges += len(b.x0s)
		c.PaintSegments(b.x0s, b.y0s, b.x1s, b.y1s, b.cols, b.width).Send()
	}
	if v.stats.NoTips || len(v.tipCols) == 0 {
		return
	}
	if v.stats.EdgeStride > 1 {
		j := 0
		for k := range v.tipEdge {
			if !v.keepEdge(v.tipEdge[k]) {
				continue
			}
			copy(v.tipXs[3*j:3*j+3], v.tipXs[3*k:3*k+3])
			copy(v.tipYs[3*j:3*j+3], v.tipYs[3*k:3*k+3])
			v.tipCols[j], v.tipEdge[j] = v.tipCols[k], v.tipEdge[k]
			j++
		}
		v.tipXs, v.tipYs, v.tipCols, v.tipEdge = v.tipXs[:3*j], v.tipYs[:3*j], v.tipCols[:j], v.tipEdge[:j]
	}
	if len(v.tipCols) > 0 {
		c.PaintTriangles(v.tipXs, v.tipYs, v.tipCols).Send()
	}
}

// flushRings sends the gathered pinned rings, unless the budget dropped them.
func (v *View) flushRings(col color.Color, w float32) {
	if v.stats.NoRings {
		return
	}
	for i := range v.rings {
		b := &v.rings[i]
		if len(b.xs) == 0 {
			continue
		}
		c.PaintMarkers(b.xs, b.ys, 0, b.radius, color.Transparent, 0).Mesh().Outline(col, w).Send()
	}
}

// edgeMayShow is the cheap cull: false when edge i lies wholly outside the
// w×h canvas, judged on its endpoints' box widened by everything that can
// reach past it — the stroke, the head, a curve's bulge, a loop's size. An
// edge with a label is always gathered; its label sits beside the stroke.
func (v *View) edgeMayShow(i int, w, h float32) bool {
	if v.g.eLabel[i] != "" {
		return true
	}
	f, t := v.g.eFrom[i], v.g.eTo[i]
	x1, y1 := v.cam.ToScreen(v.g.x[f], v.g.y[f])
	x2, y2 := v.cam.ToScreen(v.g.x[t], v.g.y[t])
	ew := v.g.eWidth[i]
	if isNaN32(ew) {
		ew = v.style.EdgeWidth
	}
	m := v.tipPx() + max(ew, 1)*2
	if order := float32(v.g.eOrder[i]); order != 0 {
		m += abs32(v.style.CurveSize*order*v.cam.Zoom) + 1
	}
	if f == t {
		m += v.nodeOuterPx(int(f)) * (v.style.LoopSize + float32(v.g.eOrder[i]) + 1)
	}
	minX, maxX := min(x1, x2)-m, max(x1, x2)+m
	minY, maxY := min(y1, y2)-m, max(y1, y2)+m
	return maxX >= 0 && maxY >= 0 && minX <= w && minY <= h
}
