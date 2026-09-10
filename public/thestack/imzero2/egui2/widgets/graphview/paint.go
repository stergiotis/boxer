package graphview

import (
	"math"

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
	hasTip bool
}

// edgeGeometry resolves edge i against the camera. The arrow head is
// subtracted from the stroke's end so the stroke does not poke through the
// head, and both ends stop at the node discs.
func (v *View) edgeGeometry(style Style, i int) (geo edgeGeo) {
	f, t := v.g.eFrom[i], v.g.eTo[i]
	geo.width = v.g.eWidth[i]
	if geo.width <= 0 {
		geo.width = style.EdgeWidth
	}
	order := float32(v.g.eOrder[i])
	x1, y1 := v.cam.toScreen(v.g.x[f], v.g.y[f])
	x2, y2 := v.cam.toScreen(v.g.x[t], v.g.y[t])
	r1 := v.nodeRadius(style, int(f)) * v.cam.zoom
	r2 := v.nodeRadius(style, int(t)) * v.cam.zoom
	tip := style.TipSize

	if f == t {
		// egui_graphs' loop: a cubic leaving the disc at 45° left, bulging
		// upward by loopSize, re-entering at 45° right.
		geo.kind = edgeKindLoop
		loopSize := r1 * (style.LoopSize + order)
		s := float32(math.Sqrt2 / 2)
		sx, sy := x1-r1*s, y1-r1*s
		ex, ey := x1+r1*s, y1-r1*s
		geo.x = [4]float32{ex, x1 + loopSize, x1 - loopSize, sx}
		geo.y = [4]float32{ey, y1 - loopSize, y1 - loopSize, sy}
		geo.loopR = loopSize * 0.75
		geo.hasTip = true
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
	sx, sy := x1+ux*r1, y1+uy*r1
	ex, ey := x2-ux*r2, y2-uy*r2
	if order == 0 {
		geo.kind = edgeKindStraight
		geo.hasTip = l > r1+r2+tip
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
	off := style.CurveSize * order * v.cam.zoom
	seg := max(l/3, 1)
	c1x, c1y := sx+ux*seg+perpX*off, sy+uy*seg+perpY*off
	c2x, c2y := ex-ux*seg+perpX*off, ey-uy*seg+perpY*off
	geo.hasTip = l > r1+r2+tip
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
// then nodes batched by colour and radius, then highlights and labels.
func (v *View) paint(style Style, w, h float32) {
	c.PaintClipPush(0, 0, w, h).Send()
	defer c.PaintClipPop().Send()
	o := &v.Opts

	// Edges.
	for i := range v.g.eFrom {
		geo := v.edgeGeometry(style, i)
		col := v.g.eCol[i]
		if isUnset(col) {
			col = style.EdgeColor
		}
		width := geo.width
		from, to := v.g.ids[v.g.eFrom[i]], v.g.ids[v.g.eTo[i]]
		if _, sel := v.selEdges[[2]uint64{from, to}]; sel {
			col = style.Selected
			width += 1
		}
		if int32(i) == v.hoveredEdge {
			col = style.Highlight
			width += 1
		}
		switch geo.kind {
		case edgeKindStraight:
			c.PaintLine(geo.x[0], geo.y[0], geo.x[3], geo.y[3], col, width).Send()
		default:
			c.PaintCubicBezier(geo.x[0], geo.y[0], geo.x[1], geo.y[1], geo.x[2], geo.y[2], geo.x[3], geo.y[3], col, width).Send()
		}
		if geo.hasTip {
			size := style.TipSize
			ax, ay := rotate(geo.tipDx, geo.tipDy, tipHalfAngle)
			bx, by := rotate(geo.tipDx, geo.tipDy, -tipHalfAngle)
			xs := []float32{geo.tipX, geo.tipX - ax*size, geo.tipX - bx*size}
			ys := []float32{geo.tipY, geo.tipY - ay*size, geo.tipY - by*size}
			c.PaintPolygonFilled(xs, ys, col).Send()
		}
		if lbl := v.g.eLabel[i]; lbl != "" {
			mx, my := edgeMidpoint(geo)
			txt := c.PaintText(mx, my-3, 1, 2, lbl, style.EdgeLabelFontSize, style.EdgeLabelColor)
			if style.Monospace {
				txt = txt.Monospace()
			}
			txt.Send()
		}
	}

	// Nodes: one marker batch per (colour, radius).
	for k := range v.batches {
		v.batches[k] = v.batches[k][:0]
	}
	for i := range v.g.ids {
		col := v.g.col[i]
		if isUnset(col) {
			col = style.NodeFill
		}
		k := batchKey(col, v.nodeRadius(style, i))
		v.batches[k] = append(v.batches[k], int32(i))
	}
	for k, slots := range v.batches {
		if len(slots) == 0 {
			delete(v.batches, k)
			continue
		}
		v.batchXs = v.batchXs[:0]
		v.batchYs = v.batchYs[:0]
		for _, s := range slots {
			sx, sy := v.cam.toScreen(v.g.x[s], v.g.y[s])
			v.batchXs = append(v.batchXs, sx)
			v.batchYs = append(v.batchYs, sy)
		}
		col := color.Hex(uint32(k >> 32))
		r := math.Float32frombits(uint32(k)) * v.cam.zoom
		c.PaintMarkers(v.batchXs, v.batchYs, 0, r, col, 0).Send()
	}
	if style.NodeStrokeW > 0 {
		for i := range v.g.ids {
			sx, sy := v.cam.toScreen(v.g.x[i], v.g.y[i])
			c.PaintCircleStroke(sx, sy, v.nodeRadius(style, i)*v.cam.zoom, style.NodeStroke, style.NodeStrokeW).Send()
		}
	}

	// Highlights and labels.
	for i := range v.g.ids {
		id := v.g.ids[i]
		_, sel := v.selNodes[id]
		hov := v.hoveredOk && id == v.hoveredId
		sx, sy := v.cam.toScreen(v.g.x[i], v.g.y[i])
		r := v.nodeRadius(style, i) * v.cam.zoom
		if sel {
			c.PaintCircleStroke(sx, sy, r+2, style.Selected, styletokens.StrokeStrong).Send()
		}
		if hov {
			c.PaintCircleStroke(sx, sy, r+3, style.Highlight, styletokens.StrokeRegular).Send()
		}
		if lbl := v.g.label[i]; lbl != "" && (o.LabelsAlways || sel || hov) {
			txt := c.PaintText(sx, sy-r-2, 1, 2, lbl, style.LabelFontSize, style.LabelColor)
			if style.Monospace {
				txt = txt.Monospace()
			}
			txt.Send()
		}
	}
}

func edgeMidpoint(geo edgeGeo) (x, y float32) {
	switch geo.kind {
	case edgeKindStraight:
		return (geo.x[0] + geo.x[3]) / 2, (geo.y[0] + geo.y[3]) / 2
	case edgeKindLoop:
		return geo.loopCx, geo.loopCy - geo.loopR
	}
	return bezierAt(geo.x, geo.y, 0.5)
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
