package regions

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// MaxOutlineVertices bounds the polyline [OutlinePolyline] gives an
// ellipse; an ellipse that needs more at the tolerance asked for gets this
// many, and its edges may then stray further than asked.
const MaxOutlineVertices = 1 << 14

// minEllipseVertices is the fewest vertices an ellipse's outline has, however
// coarse the tolerance.
const minEllipseVertices = 16

// OutlinePolyline is a shape's outline as a polyline in its own coordinates,
// for drawing (ADR-0399 §M2): a point is its one position, a line its two
// ends, a rectangle its four corners and a polygon its vertices — exactly,
// with closed true for the two, whose last vertex joins the first — and an
// ellipse a closed polygon whose vertices lie on the ellipse and whose edges
// stay within tolerance of it, in the shape's own units.
//
// The ellipse is sampled at equal steps of its parameter u, the point
// centre + R(θ)·(rx·cos u, ry·sin u). An edge spanning Δu strays from the
// unit circle by 1 − cos(Δu/2) before the linear map that makes the
// ellipse, which stretches a distance by at most max(rx, ry); so Δu is
// chosen as 2·acos(1 − tolerance / max(rx, ry)), at least 16 and at most
// [MaxOutlineVertices] steps around. The outline is never what a
// measurement reads: coverage is decided from the parameters.
func OutlinePolyline(s Shape, tolerance float64) (vertices []Point, closed bool, err error) {
	switch s.kind {
	case ShapeKindPoint:
		return []Point{s.a}, false, nil
	case ShapeKindLine:
		return []Point{s.a, s.b}, false, nil
	case ShapeKindRectangle:
		return s.corners(), true, nil
	case ShapeKindPolygon:
		return s.Vertices(), true, nil
	case ShapeKindEllipse:
		if !(tolerance > 0) || math.IsInf(tolerance, 0) {
			return nil, false, eb.Build().Float64("tolerance", tolerance).Errorf("an outline's tolerance is a finite number above zero")
		}
		return ellipseOutline(s, tolerance), true, nil
	}
	return nil, false, eb.Build().Errorf("no shape to outline")
}

// ellipseSteps is the number of equal steps of the parameter that keep each
// edge of an ellipse of largest radius r within tolerance of it.
func ellipseSteps(r, tolerance float64) int {
	n := MaxOutlineVertices
	if tolerance < r {
		step := 2 * math.Acos(1-tolerance/r)
		if step > 0 {
			n = int(math.Ceil(2 * math.Pi / step))
		}
	} else {
		n = minEllipseVertices
	}
	return min(max(n, minEllipseVertices), MaxOutlineVertices)
}

func ellipseOutline(s Shape, tolerance float64) []Point {
	rx, ry := s.b.X, s.b.Y
	n := ellipseSteps(max(rx, ry), tolerance)
	sn, cs := sincosDeg(s.angle)
	out := make([]Point, n)
	for i := range n {
		su, cu := math.Sincos(2 * math.Pi * float64(i) / float64(n))
		x, y := rx*cu, ry*su
		out[i] = Point{X: s.a.X + cs*x - sn*y, Y: s.a.Y + sn*x + cs*y}
	}
	return out
}

// FromPixels is the inverse of [ToPixels]: a shape in an image's pixel
// coordinates brought to the frame of its placement p, which must match to
// (ADR-0399 §SD2). Into the pixel frame it is the shape as it is.
//
// For a physical or reference-system frame the placement is affine and the
// mapping exact: a point, a line, a polygon and an ellipse keep their kind,
// and a rectangle stays one where the affine keeps its right angles and
// becomes the polygon of its corners otherwise ([MappingPolygon]).
//
// For a celestial frame every position goes through [imaging.Placement.World].
// The gnomonic projection takes straight lines to great circles, so a
// point, a line and a polygon map exactly, and a rectangle exactly to the
// polygon of its corners, whose sides are the great-circle arcs a
// celestial polygon has. An ellipse in pixels is not an ellipse laid out in
// the plane tangent at its centre, and is refused.
func FromPixels(px Shape, to Frame, p imaging.Placement) (s Shape, m Mapping, err error) {
	if px.IsZero() {
		return s, m, eb.Build().Errorf("no shape to map")
	}
	if err = to.Validate(); err != nil {
		return
	}
	m.From, m.To = PixelFrame(), to
	if to.Kind == FrameKindPixel {
		m.How = MappingIdentity
		return px, m, nil
	}
	if !to.Matches(p) {
		f, _ := FrameOf(p)
		return s, Mapping{}, eb.Build().Stringer("frame", to).Stringer("image", f).
			Errorf("the frame asked for is not the image's frame")
	}
	if err = p.Validate(); err != nil {
		return s, Mapping{}, err
	}
	m.How = MappingExact
	switch px.kind {
	case ShapeKindPoint:
		var a Point
		if a, err = worldOf(p, px.a); err == nil {
			s, err = NewPoint(a)
		}
	case ShapeKindLine:
		var ends []Point
		if ends, err = worldsOf(p, []Point{px.a, px.b}); err == nil {
			s, err = NewLine(ends[0], ends[1])
		}
	case ShapeKindPolygon:
		var vs []Point
		if vs, err = worldsOf(p, px.vertices); err == nil {
			s, err = NewPolygon(vs)
		}
	case ShapeKindEllipse:
		if to.Kind == FrameKindCelestial {
			return Shape{}, Mapping{}, eb.Build().Errorf("an ellipse in pixels is no ellipse on the sky: a celestial ellipse is laid out in the plane tangent at its centre")
		}
		var c Point
		if c, err = worldOf(p, px.a); err != nil {
			break
		}
		lin := fromPixel(p).mul(rotation(px.angle)).mul(linear{px.b.X, 0, 0, px.b.Y})
		rx, ry, angle := ellipseOf(lin)
		s, err = NewEllipse(c, rx, ry, angle)
	case ShapeKindRectangle:
		if to.Kind != FrameKindCelestial {
			var c Point
			if c, err = worldOf(p, px.a); err != nil {
				break
			}
			lin := fromPixel(p).mul(rotation(px.angle)).mul(linear{px.b.X / 2, 0, 0, px.b.Y / 2})
			m1x, m1y, m2x, m2y := lin[0], lin[2], lin[1], lin[3]
			l1, l2 := math.Hypot(m1x, m1y), math.Hypot(m2x, m2y)
			if math.Abs(m1x*m2x+m1y*m2y) <= 1e-12*l1*l2 {
				s, err = NewRectangle(c, 2*l1, 2*l2, math.Atan2(m1y, m1x)*180/math.Pi)
				break
			}
		}
		m.How = MappingPolygon
		var vs []Point
		if vs, err = worldsOf(p, px.corners()); err == nil {
			s, err = NewPolygon(vs)
		}
	}
	if err != nil {
		return Shape{}, Mapping{}, eb.Build().Stringer("kind", px.kind).Errorf("shape does not map from pixels: %w", err)
	}
	if s.kind == ShapeKindPolygon && px.kind != ShapeKindPolygon {
		m.Vertices = len(s.vertices)
	}
	return s, m, nil
}

// fromPixel is the linear part of the map from pixels to an affine
// placement's frame.
func fromPixel(p imaging.Placement) linear {
	a := &p.Affine
	return linear{a[1], a[2], a[4], a[5]}
}

func worldOf(p imaging.Placement, q Point) (Point, error) {
	a, b, ok := p.World(q.X, q.Y)
	if !ok {
		return Point{}, eb.Build().Float64("col", q.X).Float64("row", q.Y).
			Errorf("position has no frame coordinates under the placement")
	}
	return Point{X: a, Y: b}, nil
}

func worldsOf(p imaging.Placement, qs []Point) (out []Point, err error) {
	out = make([]Point, len(qs))
	for i, q := range qs {
		if out[i], err = worldOf(p, q); err != nil {
			return nil, err
		}
	}
	return out, nil
}
