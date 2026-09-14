package graphview

import (
	"math"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

type edgeKindE uint8

const (
	edgeKindStraight edgeKindE = 0
	edgeKindCurved   edgeKindE = 1
	edgeKindLoop     edgeKindE = 2
)

// edgeGeo is one edge in canvas pixels: for a straight edge x[0],y[0] and
// x[3],y[3] are the trimmed endpoints; for a curve the four cubic control
// points; for a loop the cubic as drawn, with loopCx/loopCy/loopR the circle
// the pick test uses. tipX/tipY is the arrow head's tip and tipDx/tipDy its
// unit direction.
type edgeGeo struct {
	kind   edgeKindE
	x, y   [4]float32
	loopCx float32 // self-loop pick circle
	loopCy float32
	loopR  float32
	width  float32
	tipX   float32
	tipY   float32
	tipDx  float32
	tipDy  float32
	tipLen float32 // the head's length on screen
	hasTip bool
	hidden bool // the nodes overlap past the trimmed ends: nothing to paint
}

// edgeGapPx is the space, in screen pixels, between an edge's ends — the
// arrow head's tip included — and the outer edge of the node it meets, its
// outline included.
const edgeGapPx = 2

// edgeGeometry resolves edge i against the camera. The arrow head is
// subtracted from the stroke's end so the stroke does not poke through the
// head, and both ends stop edgeGapPx short of the node's outer edge, its
// donut ring included, so neither the stroke nor the head runs into it.
func (v *View) edgeGeometry(i int) (geo edgeGeo) {
	style := &v.style
	f, t := v.g.eFrom[i], v.g.eTo[i]
	geo.width = v.g.eWidth[i]
	if isNaN32(geo.width) {
		geo.width = style.EdgeWidth
	}
	geo.width = max(geo.width*v.decorScale(), edgeMinWidthPx)
	order := float32(v.g.eOrder[i])
	x1, y1 := v.cam.ToScreen(v.g.x[f], v.g.y[f])
	x2, y2 := v.cam.ToScreen(v.g.x[t], v.g.y[t])
	o1 := v.nodeOuterPx(int(f))
	// The trimmed ends: the outer edge, the half of a node outline that lies
	// outside it, and the gap.
	pad := v.nodeStrokePx()/2 + edgeGapPx
	e1 := o1 + pad
	e2 := v.nodeOuterPx(int(t)) + pad
	// An undirected picture draws no head, so the stroke runs to the gap
	// (ADR-0232 §SD5); a head of no size trims nothing.
	tip := v.tipPx()
	if v.Opts.Undirected {
		tip = 0
	}
	geo.tipLen = tip

	if f == t {
		// egui_graphs' loop: a cubic leaving the node at 45° left, bulging
		// upward by loopSize, re-entering at 45° right. It is sized from the
		// outer edge, so a donut ring does not swallow it.
		geo.kind = edgeKindLoop
		loopSize := o1 * (style.LoopSize + order)
		s := float32(math.Sqrt2 / 2)
		sx, sy := x1-e1*s, y1-e1*s
		ex, ey := x1+e1*s, y1-e1*s
		geo.x = [4]float32{ex, x1 + loopSize, x1 - loopSize, sx}
		geo.y = [4]float32{ey, y1 - loopSize, y1 - loopSize, sy}
		geo.loopR = loopSize * 0.75
		geo.hasTip = tip > 0
		geo.tipX, geo.tipY = sx, sy
		geo.tipDx, geo.tipDy = unit(sx-geo.x[2], sy-geo.y[2])
		// The stroke ends where the head begins.
		geo.x[3], geo.y[3] = sx-geo.tipDx*tip, sy-geo.tipDy*tip
		// Pick geometry: the loop's visual centre.
		geo.loopCx, geo.loopCy = x1, y1-loopSize*0.75
		return
	}

	dx, dy := x2-x1, y2-y1
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l <= 1e-3 {
		geo.kind = edgeKindStraight
		geo.x = [4]float32{x1, x1, x2, x2}
		geo.y = [4]float32{y1, y1, y2, y2}
		return
	}
	ux, uy := dx/l, dy/l
	sx, sy := x1+ux*e1, y1+uy*e1
	ex, ey := x2-ux*e2, y2-uy*e2
	// Discs that overlap, or all but touch, leave no stroke between them:
	// an inverted segment would paint under the discs and its label beside
	// them.
	geo.hidden = l <= e1+e2
	if order == 0 {
		geo.kind = edgeKindStraight
		geo.hasTip = tip > 0 && l > e1+e2+tip
		geo.tipX, geo.tipY = ex, ey
		geo.tipDx, geo.tipDy = ux, uy
		if geo.hasTip {
			ex, ey = ex-ux*tip, ey-uy*tip
		}
		geo.x = [4]float32{sx, sx, ex, ex}
		geo.y = [4]float32{sy, sy, ey, ey}
		return
	}
	// Curved parallel edge: bulge perpendicular to the chord by
	// CurveSize · order, control points a third of the way along the chord.
	geo.kind = edgeKindCurved
	perpX, perpY := -uy, ux
	off := style.CurveSize * order * v.cam.Zoom
	seg := max(l/3, 1)
	c1x, c1y := sx+ux*seg+perpX*off, sy+uy*seg+perpY*off
	c2x, c2y := ex-ux*seg+perpX*off, ey-uy*seg+perpY*off
	geo.hasTip = tip > 0 && l > e1+e2+tip
	geo.tipX, geo.tipY = ex, ey
	geo.tipDx, geo.tipDy = unit(ex-c2x, ey-c2y)
	if geo.hasTip {
		ex, ey = ex-geo.tipDx*tip, ey-geo.tipDy*tip
	}
	geo.x = [4]float32{sx, c1x, c2x, ex}
	geo.y = [4]float32{sy, c1y, c2y, ey}
	return
}

// tipHalfAngle is the arrow head's half opening angle.
const tipHalfAngle = 0.4

// paint emits the frame's paint commands, clipped to the canvas: edges,
// then nodes batched by colour and radius, then donuts, highlights and
// labels. Per-node text and strokes are skipped for nodes outside the
// canvas; markers are one batch and cheap to let the host clip.
func (v *View) paint(w, h float32) {
	c.PaintClipPush(0, 0, w, h).Send()
	defer c.PaintClipPop().Send()
	style := &v.style
	o := &v.Opts

	// Auras, beneath everything (ADR-0224 §SD11).
	v.paintAuras()

	// Edges.
	for i := range v.g.eFrom {
		if o.HideEdges {
			break
		}
		geo := v.edgeGeometry(i)
		if geo.hidden {
			continue
		}
		col := v.g.eCol[i]
		if isUnset(col) {
			col = style.EdgeColor
		}
		// The edge's declared paint fades; the selection and hover colours
		// are the widget's own and stay legible (ADR-0224 §SD14).
		op := v.g.eOpacity[i]
		width := geo.width
		if _, sel := v.selEdges[v.g.edgeRef(int32(i))]; sel {
			col, op = style.Selected, 1
			width += 1
		}
		if int32(i) == v.hoveredEdge {
			col, op = style.Highlight, 1
			width += 1
		}
		col = fade(col, op)
		switch geo.kind {
		case edgeKindStraight:
			c.PaintLine(geo.x[0], geo.y[0], geo.x[3], geo.y[3], col, width).Send()
		default:
			c.PaintCubicBezier(geo.x[0], geo.y[0], geo.x[1], geo.y[1], geo.x[2], geo.y[2], geo.x[3], geo.y[3], col, width).Send()
		}
		if geo.hasTip {
			size := geo.tipLen
			ax, ay := rotate(geo.tipDx, geo.tipDy, tipHalfAngle)
			bx, by := rotate(geo.tipDx, geo.tipDy, -tipHalfAngle)
			v.tipXs = [3]float32{geo.tipX, geo.tipX - ax*size, geo.tipX - bx*size}
			v.tipYs = [3]float32{geo.tipY, geo.tipY - ay*size, geo.tipY - by*size}
			c.PaintPolygonFilled(v.tipXs[:], v.tipYs[:], col).Send()
		}
		if lbl := v.g.eLabel[i]; lbl != "" {
			lx, ly := edgeLabelPos(geo, lbl, style.EdgeLabelFontSize)
			if onCanvas(lx, ly, style.EdgeLabelFontSize*4, w, h) {
				v.paintLabel(lx, ly, 1, 1, lbl, style.EdgeLabelFontSize, fade(style.EdgeLabelColor, v.g.eOpacity[i]))
			}
		}
	}

	// Nodes: one marker batch per (colour, radius), in first-seen order.
	v.buildBatches()
	for i := range v.batches {
		b := &v.batches[i]
		v.batchXs = v.batchXs[:0]
		v.batchYs = v.batchYs[:0]
		for _, s := range b.slots {
			sx, sy := v.cam.ToScreen(v.g.x[s], v.g.y[s])
			v.batchXs = append(v.batchXs, sx)
			v.batchYs = append(v.batchYs, sy)
		}
		c.PaintMarkers(v.batchXs, v.batchYs, 0, radiusPx(b.radius, v.cam.Zoom), b.col, 0).Send()
	}
	if style.NodeStrokeW > 0 {
		for i := range v.g.ids {
			sx, sy := v.cam.ToScreen(v.g.x[i], v.g.y[i])
			r := v.nodeRadiusPx(i)
			if onCanvas(sx, sy, r, w, h) {
				c.PaintCircleStroke(sx, sy, r, fade(style.NodeStroke, v.g.opacity[i]), v.nodeStrokePx()).Send()
			}
		}
	}

	// Donuts: one concave ring sector per slice, skipped when the ring
	// would be too small to read (ADR-0224 §SD9).
	for i := range v.g.ids {
		if !v.donutDrawable(i) {
			continue
		}
		d := v.g.donut[i]
		rIn := v.nodeRadiusPx(i)
		rOut := rIn + style.DonutWidth
		sx, sy := v.cam.ToScreen(v.g.x[i], v.g.y[i])
		if !onCanvas(sx, sy, rOut, w, h) {
			continue
		}
		v.arcs = donutArcs(d, style.DonutTrack, v.arcs[:0])
		if op := v.g.opacity[i]; op < 1 {
			for j := range v.arcs {
				v.arcs[j].col = fade(v.arcs[j].col, op)
			}
		}
		for _, a := range v.arcs {
			// A span past half a turn splits in two so the outline never
			// touches itself.
			for _, seg := range splitArc(a.a0, a.a1) {
				v.arcXs, v.arcYs = ringSector(sx, sy, rIn, rOut, seg[0], seg[1], v.arcXs[:0], v.arcYs[:0])
				// The concave fill is a raw mesh with no feathering; a hairline
				// stroke in the slice's own colour is what anti-aliases its edge.
				c.PaintPolygonFilled(v.arcXs, v.arcYs, a.col).Concave().Stroke(a.col, styletokens.StrokeHair).Send()
			}
		}
	}

	// Highlights and labels, for the nodes that have any.
	dragging := v.dragSlot()
	for i := range v.g.ids {
		id := v.g.ids[i]
		_, sel := v.selNodes[id]
		hov := (v.hoveredOk && id == v.hoveredId) || int32(i) == dragging
		pinned := v.g.isPinned(i)
		lbl := v.g.label[i]
		always := o.LabelsAlways || v.g.labelAlways[i]
		if !(sel || hov || pinned || (always && lbl != "")) {
			continue
		}
		sx, sy := v.cam.ToScreen(v.g.x[i], v.g.y[i])
		r := v.nodeOuterPx(i)
		if !onCanvas(sx, sy, r+style.LabelFontSize*4, w, h) {
			continue
		}
		if pinned {
			c.PaintCircleStroke(sx, sy, r+1, style.PinnedStroke, styletokens.StrokeHair).Send()
		}
		if sel {
			c.PaintCircleStroke(sx, sy, r+2, style.Selected, styletokens.StrokeStrong).Send()
		}
		if hov {
			c.PaintCircleStroke(sx, sy, r+3, style.Highlight, styletokens.StrokeRegular).Send()
		}
		if lbl != "" && (always || sel || hov) {
			v.paintLabel(sx, sy-r-2, 1, 2, lbl, style.LabelFontSize, fade(style.LabelColor, v.g.opacity[i]))
		}
	}

	// The selection rectangle in flight (ADR-0224 §SD12).
	if v.drag.active && v.drag.isRect {
		minX, maxX := min(v.drag.x0, v.drag.lastX), max(v.drag.x0, v.drag.lastX)
		minY, maxY := min(v.drag.y0, v.drag.lastY), max(v.drag.y0, v.drag.lastY)
		c.PaintRectFilled(minX, minY, maxX, maxY, 0, style.SelectionBox).Send()
		c.PaintRectStroke(minX, minY, maxX, maxY, 0, style.Selected, styletokens.StrokeHair).Send()
	}
}

// buildBatches groups the nodes by (fill, radius) into v.batches, in the
// order a key is first seen, so the paint order is a function of the
// declaration alone.
func (v *View) buildBatches() {
	for i := range v.batches {
		v.batches[i].slots = v.batches[i].slots[:0]
	}
	v.batches = v.batches[:0]
	clear(v.batchIdx)
	for i := range v.g.ids {
		col := v.nodeFill(i)
		radius := v.nodeRadius(i)
		k := uint64(col.Literal())<<32 | uint64(math.Float32bits(radius))
		bi, ok := v.batchIdx[k]
		if !ok {
			bi = int32(len(v.batches))
			v.batchIdx[k] = bi
			if cap(v.batches) > len(v.batches) {
				v.batches = v.batches[:len(v.batches)+1]
				v.batches[bi].col, v.batches[bi].radius = col, radius
			} else {
				v.batches = append(v.batches, nodeBatch{col: col, radius: radius})
			}
		}
		v.batches[bi].slots = append(v.batches[bi].slots, int32(i))
	}
}

// paintLabel emits one anchored text at the style's face, over a halo when
// the style has one: the same text in the halo colour, one pixel off in
// each cardinal direction, which is the painter lane's outline — it lays
// text out but does not stroke it. The four copies share egui's per-frame
// galley cache, so the halo costs tessellation, not layout.
func (v *View) paintLabel(x, y float32, anchorH, anchorV uint8, text string, size float32, col color.Color) {
	if halo := v.style.LabelHalo; !isUnset(halo) && halo.Literal()&0xff != 0 {
		halo = fade(halo, float32(col.Literal()&0xff)/255)
		for _, d := range labelHaloOffsets {
			v.paintText(x+d[0], y+d[1], anchorH, anchorV, text, size, halo)
		}
	}
	v.paintText(x, y, anchorH, anchorV, text, size, col)
}

// labelHaloOffsets are the halo's copies, in screen pixels off the text.
var labelHaloOffsets = [4][2]float32{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}

func (v *View) paintText(x, y float32, anchorH, anchorV uint8, text string, size float32, col color.Color) {
	txt := c.PaintText(x, y, anchorH, anchorV, text, size, col)
	if v.style.Monospace {
		txt = txt.Monospace()
	}
	txt.Send()
}

// The edge label's text box, estimated from the rune count: the painter
// lane measures no text at paint time, and a label is short.
const (
	edgeLabelGlyphW = 0.55 // advance per rune as a fraction of the font size
	edgeLabelLineH  = 1.2  // line height as a fraction of the font size
	edgeLabelGapPx  = 2    // clearance between the stroke and the box
)

// edgeLabelPos places an edge's label beside the middle of its stroke, on
// the side that faces up: the text box's centre is pushed along the
// curve's normal by as much of the box as lies along that normal, so the
// box clears the stroke whether the edge runs flat, upright or between.
func edgeLabelPos(geo edgeGeo, text string, fontSize float32) (x, y float32) {
	var mx, my, tx, ty float32
	switch geo.kind {
	case edgeKindStraight:
		mx, my = (geo.x[0]+geo.x[3])/2, (geo.y[0]+geo.y[3])/2
		tx, ty = geo.x[3]-geo.x[0], geo.y[3]-geo.y[0]
	default:
		mx, my = bezierAt(geo.x, geo.y, 0.5)
		tx, ty = bezierTangentAt(geo.x, geo.y, 0.5)
	}
	tx, ty = unit(tx, ty)
	nx, ny := -ty, tx
	if ny > 0 || (ny == 0 && nx > 0) {
		nx, ny = -nx, -ny
	}
	halfW := edgeLabelGlyphW * fontSize * float32(utf8.RuneCountInString(text)) / 2
	halfH := edgeLabelLineH * fontSize / 2
	off := halfH*abs32(ny) + halfW*abs32(nx) + edgeLabelGapPx
	return mx + nx*off, my + ny*off
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

// fade scales a colour's alpha by op, which opacityOr1 has already resolved,
// so op == 1 is the common case and returns the colour untouched. A retained
// colour flattens to its originating literal: the scaled value is a new
// colour, which the retained slot does not hold (ADR-0224 §SD14).
func fade(col color.Color, op float32) color.Color {
	if op >= 1 || isUnset(col) {
		return col
	}
	lit := col.Literal()
	a := uint32(float32(lit&0xff)*op + 0.5)
	return color.Hex(lit&^0xff | a)
}

// onCanvas reports whether a disc of radius r around (x, y) touches the
// w×h canvas.
func onCanvas(x, y, r, w, h float32) bool {
	return x+r >= 0 && y+r >= 0 && x-r <= w && y-r <= h
}

// splitArc returns the arc as one span, or two when it exceeds half a turn.
func splitArc(a0, a1 float32) [][2]float32 {
	if a1-a0 <= math.Pi {
		return [][2]float32{{a0, a1}}
	}
	mid := (a0 + a1) / 2
	return [][2]float32{{a0, mid}, {mid, a1}}
}

// bezierTangentAt is the cubic's derivative at t, unnormalised.
func bezierTangentAt(x, y [4]float32, t float32) (dx, dy float32) {
	mt := 1 - t
	a := 3 * mt * mt
	b := 6 * mt * t
	cc := 3 * t * t
	return a*(x[1]-x[0]) + b*(x[2]-x[1]) + cc*(x[3]-x[2]), a*(y[1]-y[0]) + b*(y[2]-y[1]) + cc*(y[3]-y[2])
}

func bezierAt(x, y [4]float32, t float32) (bx, by float32) {
	mt := 1 - t
	a := mt * mt * mt
	b := 3 * mt * mt * t
	cc := 3 * mt * t * t
	d := t * t * t
	return a*x[0] + b*x[1] + cc*x[2] + d*x[3], a*y[0] + b*y[1] + cc*y[2] + d*y[3]
}

func unit(x, y float32) (ux, uy float32) {
	l := float32(math.Hypot(float64(x), float64(y)))
	if l <= 1e-6 {
		return 0, -1
	}
	return x / l, y / l
}

func rotate(x, y, a float32) (rx, ry float32) {
	s, cs := math.Sincos(float64(a))
	return x*float32(cs) - y*float32(s), x*float32(s) + y*float32(cs)
}

// distSegment is the distance from p to the segment ab.
func distSegment(ax, ay, bx, by, px, py float32) float32 {
	dx, dy := bx-ax, by-ay
	l2 := dx*dx + dy*dy
	t := float32(0)
	if l2 > 0 {
		t = ((px-ax)*dx + (py-ay)*dy) / l2
		t = min(max(t, 0), 1)
	}
	cx, cy := ax+t*dx, ay+t*dy
	return float32(math.Hypot(float64(px-cx), float64(py-cy)))
}

// distBezier is the distance from p to the cubic, sampled as a polyline.
func distBezier(x, y [4]float32, px, py float32) float32 {
	const samples = 12
	best := float32(math.MaxFloat32)
	lx, ly := x[0], y[0]
	for i := 1; i <= samples; i++ {
		t := float32(i) / samples
		nx, ny := bezierAt(x, y, t)
		best = min(best, distSegment(lx, ly, nx, ny, px, py))
		lx, ly = nx, ny
	}
	return best
}
