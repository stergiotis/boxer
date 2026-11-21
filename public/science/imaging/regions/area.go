package regions

import (
	"image"
	"iter"
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// AreaFractions is coverage by area (ADR-0399 §SD4): for every pixel of a
// width × height image that an area shape in pixel coordinates covers in
// part, the fraction of the pixel's area inside the shape, in (0, 1], in
// row-major order. It is computed in closed form, with no supersampling: a
// polygon or a rectangle by integrating its edges over the pixel's square;
// an ellipse by mapping it to the unit circle, where the pixel becomes a
// parallelogram whose intersection with the circle is a sum of triangles
// and circular sectors. A polygon's inside is decided by the even-odd rule,
// as for coverage by centre. A point or a line covers no area and is
// refused.
func AreaFractions(s Shape, width, height int) (seq iter.Seq2[image.Point, float64], err error) {
	if width <= 0 || height <= 0 {
		return nil, eb.Build().Int("width", width).Int("height", height).Errorf("image size must be positive")
	}
	switch s.kind {
	case ShapeKindRectangle, ShapeKindPolygon:
		vs := s.vertices
		if s.kind == ShapeKindRectangle {
			vs = s.corners()
		}
		return func(yield func(image.Point, float64) bool) {
			polygonFractions(vs, width, height, func(col, row int, f float64) bool {
				return yield(image.Pt(col, row), f)
			})
		}, nil
	case ShapeKindEllipse:
		return func(yield func(image.Point, float64) bool) {
			e := newEllipseGeom(s)
			ellipseFractions(&e, width, height, func(col, row int, f float64) bool {
				return yield(image.Pt(col, row), f)
			})
		}, nil
	}
	return nil, eb.Build().Stringer("kind", s.kind).Errorf("coverage by area applies to a rectangle, an ellipse or a polygon")
}

// rowAccumulator collects one row's covered areas: partial ones per column,
// and runs of whole-height contributions as differences, summed when the row
// is done.
type rowAccumulator struct {
	part, diff []float64
}

func (inst *rowAccumulator) reset(width int) {
	if cap(inst.part) < width {
		inst.part, inst.diff = make([]float64, width), make([]float64, width+1)
		return
	}
	inst.part, inst.diff = inst.part[:width], inst.diff[:width+1]
	clear(inst.part)
	clear(inst.diff)
}

// emit yields the row's fractions in column order, clamped to (0, 1].
func (inst *rowAccumulator) emit(row int, yield func(col, row int, f float64) bool) bool {
	run := 0.0
	for col := range inst.part {
		run += inst.diff[col]
		f := min(run+inst.part[col], 1)
		if f > 0 {
			if !yield(col, row, f) {
				return false
			}
		}
	}
	return true
}

// addEdge adds sign·∫ clamp(x(y) − col, 0, 1) dy over a strip of height h
// in which an edge runs linearly from xa to xb, for every column: the area,
// within each column, left of the edge — so that the area between a left
// and a right edge is the right one's minus the left one's.
func (inst *rowAccumulator) addEdge(xa, xb, h, sign float64) {
	width := len(inst.part)
	x0, x1 := min(xa, xb), max(xa, xb)
	cLo := clampIndex(math.Floor(x0), 0, width)
	// Columns left of cLo lie wholly left of the edge.
	inst.diff[0] += sign * h
	inst.diff[cLo] -= sign * h
	cHi := clampIndex(math.Ceil(x1), 0, width)
	for col := cLo; col < cHi; col++ {
		inst.part[col] += sign * h * meanClamp(x0, x1, float64(col))
	}
}

// meanClamp is the mean of clamp(x − c, 0, 1) for x spread evenly over
// [x0, x1]: the parts below c, within [c, c + 1] and above weighted by their
// lengths, so that a short interval loses no precision.
func meanClamp(x0, x1, c float64) float64 {
	if x1 <= x0 {
		return min(max(x0-c, 0), 1)
	}
	lo, hi := max(x0, c), min(x1, c+1)
	var mid float64
	if hi > lo {
		mid = (hi - lo) * ((lo+hi)/2 - c)
	}
	above := max(x1-max(x0, c+1), 0)
	return (mid + above) / (x1 - x0)
}

// polygonFractions yields the area fractions of a ring under the even-odd
// rule. Each pixel row is cut into strips at the vertices and at the points
// where two edges cross; within a strip the edges do not cross, so sorted by
// position they bound the inside in pairs — first to second, third to
// fourth — and each pair's area per column is integrated exactly.
func polygonFractions(vs []Point, width, height int, yield func(col, row int, f float64) bool) {
	edges := ringEdges(vs)
	if len(edges) == 0 {
		return
	}
	yLo, yHi := vs[0].Y, vs[0].Y
	for _, v := range vs {
		yLo, yHi = min(yLo, v.Y), max(yHi, v.Y)
	}
	r0, r1 := clampIndex(math.Floor(yLo), 0, height), clampIndex(math.Ceil(yHi), 0, height)
	var acc rowAccumulator
	active := make([]polyEdge, 0, 8)
	cuts := make([]float64, 0, 16)
	type crossing struct {
		mid    float64
		xa, xb float64
	}
	spanning := make([]crossing, 0, 8)
	next := 0
	for row := r0; row < r1; row++ {
		top, bottom := float64(row), float64(row+1)
		for next < len(edges) && edges[next].y0 < bottom {
			active = append(active, edges[next])
			next++
		}
		active = slices.DeleteFunc(active, func(e polyEdge) bool { return e.y1 <= top })
		if len(active) == 0 {
			continue
		}
		cuts = append(cuts[:0], top, bottom)
		for i, e := range active {
			if e.y0 > top && e.y0 < bottom {
				cuts = append(cuts, e.y0)
			}
			if e.y1 > top && e.y1 < bottom {
				cuts = append(cuts, e.y1)
			}
			for _, f := range active[i+1:] {
				if y, ok := edgesCross(e, f, top, bottom); ok {
					cuts = append(cuts, y)
				}
			}
		}
		slices.Sort(cuts)
		cuts = slices.Compact(cuts)
		acc.reset(width)
		for i := 0; i+1 < len(cuts); i++ {
			y0, y1 := cuts[i], cuts[i+1]
			spanning = spanning[:0]
			for _, e := range active {
				if e.y0 <= y0 && e.y1 >= y1 {
					xa, xb := edgeX(e, y0), edgeX(e, y1)
					spanning = append(spanning, crossing{mid: xa + xb, xa: xa, xb: xb})
				}
			}
			slices.SortFunc(spanning, func(p, q crossing) int {
				switch {
				case p.mid < q.mid:
					return -1
				case p.mid > q.mid:
					return 1
				}
				return 0
			})
			for j := 0; j+1 < len(spanning); j += 2 {
				acc.addEdge(spanning[j].xa, spanning[j].xb, y1-y0, -1)
				acc.addEdge(spanning[j+1].xa, spanning[j+1].xb, y1-y0, 1)
			}
		}
		if !acc.emit(row, yield) {
			return
		}
	}
}

// edgeX is where an edge is at y, exactly at its ends.
func edgeX(e polyEdge, y float64) float64 {
	switch y {
	case e.y0:
		return e.x0
	case e.y1:
		return e.x1
	}
	return e.x0 + (y-e.y0)*(e.x1-e.x0)/(e.y1-e.y0)
}

// edgesCross is the y strictly between top and bottom at which two edges
// cross, if they do.
func edgesCross(e, f polyEdge, top, bottom float64) (y float64, ok bool) {
	ya, yb := max(top, e.y0, f.y0), min(bottom, e.y1, f.y1)
	if !(yb > ya) {
		return 0, false
	}
	da := edgeX(e, ya) - edgeX(f, ya)
	db := edgeX(e, yb) - edgeX(f, yb)
	if !(da < 0 && db > 0) && !(da > 0 && db < 0) {
		return 0, false
	}
	y = ya + (yb-ya)*da/(da-db)
	return y, y > top && y < bottom
}

// ellipseFractions yields the area fractions of an ellipse. On each pixel
// row the columns wholly inside — those whose square's two vertical sides
// both lie inside, which for a convex shape puts the square inside — are
// whole, the columns outside the row strip's extent are not visited, and
// the rest are computed exactly.
func ellipseFractions(e *ellipseGeom, width, height int, yield func(col, row int, f float64) bool) {
	r0, r1 := clampIndex(math.Floor(e.cy-e.hy), 0, height), clampIndex(math.Ceil(e.cy+e.hy), 0, height)
	m := newUnitMap(e)
	// A margin far above rounding keeps a pixel the ellipse touches from
	// being taken for one it misses, or for one it covers whole.
	eps := 1e-9 * (1 + e.hx + math.Abs(e.cx))
	for row := r0; row < r1; row++ {
		top, bottom := float64(row), float64(row+1)
		ya, yb := max(top, e.cy-e.hy), min(bottom, e.cy+e.hy)
		if !(yb > ya) {
			continue
		}
		xmin, xmax := e.cx-e.hx, e.cx+e.hx
		if d := e.cy - e.dyR; d < ya || d > yb {
			la, _, _ := e.chordClamped(ya)
			lb, _, _ := e.chordClamped(yb)
			xmin = min(la, lb)
		}
		if d := e.cy + e.dyR; d < ya || d > yb {
			_, ra, _ := e.chordClamped(ya)
			_, rb, _ := e.chordClamped(yb)
			xmax = max(ra, rb)
		}
		innerLo, innerHi := 1, 0
		if top >= e.cy-e.hy && bottom <= e.cy+e.hy {
			lt, rt, _ := e.chordClamped(top)
			lbot, rbot, _ := e.chordClamped(bottom)
			innerLo = clampIndex(math.Ceil(max(lt, lbot)+eps), 0, width)
			innerHi = clampIndex(math.Floor(min(rt, rbot)-eps), 0, width)
		}
		c0 := clampIndex(math.Floor(xmin-eps), 0, width)
		c1 := clampIndex(math.Ceil(xmax+eps), 0, width)
		for col := c0; col < c1; col++ {
			var f float64
			if col >= innerLo && col+1 <= innerHi {
				f = 1
			} else {
				f = m.fraction(float64(col), top)
			}
			if f > 0 && !yield(col, row, f) {
				return
			}
		}
	}
}

// chordClamped is [ellipseGeom.chord] for a y within rounding of the
// ellipse's extent, taken as the tangent there.
func (inst *ellipseGeom) chordClamped(y float64) (xl, xr float64, ok bool) {
	y = min(max(y, inst.cy-inst.hy), inst.cy+inst.hy)
	return inst.chord(y)
}

// unitMap takes pixel coordinates to the coordinates in which the ellipse
// is the unit circle: u = R(−θ)(p − centre) / (rx, ry).
type unitMap struct {
	cx, cy, s, c, rx, ry float64
}

func newUnitMap(e *ellipseGeom) unitMap {
	return unitMap{cx: e.cx, cy: e.cy, s: e.s, c: e.c, rx: e.rx, ry: e.ry}
}

func (inst unitMap) apply(x, y float64) (u, v float64) {
	dx, dy := x-inst.cx, y-inst.cy
	return (inst.c*dx + inst.s*dy) / inst.rx, (-inst.s*dx + inst.c*dy) / inst.ry
}

// fraction is the share of pixel [col, col+1) × [row, row+1) inside the
// ellipse: the area of its image, a parallelogram, inside the unit circle,
// times rx·ry, the ratio of the two areas.
func (inst unitMap) fraction(col, row float64) float64 {
	var q [4][2]float64
	for i, p := range [4][2]float64{{col, row}, {col + 1, row}, {col + 1, row + 1}, {col, row + 1}} {
		q[i][0], q[i][1] = inst.apply(p[0], p[1])
	}
	return min(max(circlePolygonArea(q[:])*inst.rx*inst.ry, 0), 1)
}

// circlePolygonArea is the area of a convex polygon's intersection with the
// unit circle about the origin: the sum, over its edges, of the signed area
// of the triangle from the origin to the edge intersected with the circle —
// a triangle where the edge is inside, a circular sector where it is
// outside.
func circlePolygonArea(q [][2]float64) float64 {
	inside := true
	for _, p := range q {
		if p[0]*p[0]+p[1]*p[1] > 1 {
			inside = false
			break
		}
	}
	if inside {
		var a float64
		for i, p := range q {
			n := q[(i+1)%len(q)]
			a += p[0]*n[1] - n[0]*p[1]
		}
		return math.Abs(a) / 2
	}
	if !polygonReachesCircle(q) {
		return 0
	}
	var sum float64
	for i, p := range q {
		sum += triangleInCircle(p, q[(i+1)%len(q)])
	}
	return math.Abs(sum)
}

// polygonReachesCircle reports whether a convex polygon meets the unit
// circle's disc: the origin is inside it, or an edge comes within 1 of it.
func polygonReachesCircle(q [][2]float64) bool {
	pos, neg := false, false
	for i, p := range q {
		n := q[(i+1)%len(q)]
		cr := p[0]*n[1] - n[0]*p[1]
		pos, neg = pos || cr > 0, neg || cr < 0
		dx, dy := n[0]-p[0], n[1]-p[1]
		l2 := dx*dx + dy*dy
		t := 0.0
		if l2 > 0 {
			t = min(max(-(p[0]*dx+p[1]*dy)/l2, 0), 1)
		}
		cx, cy := p[0]+t*dx, p[1]+t*dy
		if cx*cx+cy*cy < 1 {
			return true
		}
	}
	return !(pos && neg)
}

// triangleInCircle is the signed area of the triangle (origin, a, b)
// intersected with the unit circle.
func triangleInCircle(a, b [2]float64) float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]
	qa := dx*dx + dy*dy
	if qa == 0 {
		return 0
	}
	qb := a[0]*dx + a[1]*dy
	qc := a[0]*a[0] + a[1]*a[1] - 1
	disc := qb*qb - qa*qc
	if disc <= 0 {
		return sector(a, b)
	}
	sq := math.Sqrt(disc)
	// The roots in the stable form: t1 ≤ t2.
	var t1, t2 float64
	if qb >= 0 {
		t1 = (-qb - sq) / qa
		t2 = qc / (qa * t1)
		if t1 == 0 {
			t2 = (-qb + sq) / qa
		}
	} else {
		t2 = (-qb + sq) / qa
		t1 = qc / (qa * t2)
	}
	if t1 > t2 {
		t1, t2 = t2, t1
	}
	if t2 <= 0 || t1 >= 1 {
		return sector(a, b)
	}
	if t1 <= 0 && t2 >= 1 {
		return (a[0]*b[1] - b[0]*a[1]) / 2
	}
	p, q := a, b
	var area float64
	if t1 > 0 {
		p = [2]float64{a[0] + t1*dx, a[1] + t1*dy}
		area += sector(a, p)
	}
	if t2 < 1 {
		q = [2]float64{a[0] + t2*dx, a[1] + t2*dy}
		area += sector(q, b)
	}
	return area + (p[0]*q[1]-q[0]*p[1])/2
}

// sector is the signed area of the unit circle's sector between the
// directions of a and b.
func sector(a, b [2]float64) float64 {
	return math.Atan2(a[0]*b[1]-b[0]*a[1], a[0]*b[0]+a[1]*b[1]) / 2
}
