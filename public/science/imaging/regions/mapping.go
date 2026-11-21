package regions

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// MappingE says how a region's shape reached an image's pixels (ADR-0399
// §SD2).
type MappingE uint8

const (
	// MappingIdentity: the region was in pixel coordinates already.
	MappingIdentity MappingE = iota + 1
	// MappingExact: the shape was mapped exactly and kept its kind.
	MappingExact
	// MappingPolygon: the shape was mapped exactly and became a polygon — a
	// rectangle under a map that does not keep its right angles, or a
	// rectangle on the sky, whose sides are great-circle arcs.
	MappingPolygon
	// MappingApproximated: the shape became a polygon whose vertices lie on
	// the mapped outline and whose edges stay within a tolerance of it.
	MappingApproximated
)

// AllMappings lists the ways a shape reaches pixels.
var AllMappings = []MappingE{MappingIdentity, MappingExact, MappingPolygon, MappingApproximated}

func (inst MappingE) String() string {
	switch inst {
	case MappingIdentity:
		return "identity"
	case MappingExact:
		return "exact"
	case MappingPolygon:
		return "polygon"
	case MappingApproximated:
		return "approximated"
	}
	return "none"
}

// Mapping is how a region reached an image's pixels.
type Mapping struct {
	How MappingE
	// From is the region's frame; To the placement's, the pixel frame for
	// [MappingIdentity].
	From, To Frame
	// Tolerance is, for [MappingApproximated], the largest distance in
	// pixels an edge may stray from the mapped outline; zero otherwise.
	Tolerance float64
	// Vertices is the number of vertices of a shape that became a polygon.
	Vertices int
}

// DefaultTolerance is the tolerance, in pixels, of a shape approximated by a
// polygon when none is given.
const DefaultTolerance = 0.01

// MaxApproximationVertices bounds the polygon an approximation may make; a
// shape that needs more at the tolerance asked for is refused.
const MaxApproximationVertices = 1 << 16

// ToPixels brings a shape in frame to the pixel coordinates of an image
// whose placement is p (ADR-0399 §SD2). A shape in the pixel frame is
// returned as it is. Otherwise the placement's frame must match the shape's
// ([Frame.Matches]), or the shape is refused naming both frames.
//
// For a physical or reference-system frame the placement is affine and the
// mapping exact: a point, a line, a polygon and an ellipse keep their kind —
// an ellipse's centre, radii and angle are those of the image ellipse — and
// a rectangle stays a rectangle where the map keeps its right angles and
// becomes the polygon of its corners where it does not.
//
// For a celestial frame every position goes through [imaging.Placement.Pixel],
// and a shape any of whose points lies on the hemisphere the projection
// cannot show is refused. The gnomonic projection takes great circles to
// straight lines, so a point, a line and a polygon map exactly, and a
// rectangle exactly to the polygon of its corners; an ellipse becomes a
// polygon refined until each edge lies within tolerance pixels of the
// mapped outline (zero takes [DefaultTolerance]).
func ToPixels(s Shape, frame Frame, p imaging.Placement, tolerance float64) (px Shape, m Mapping, err error) {
	if s.IsZero() {
		return px, m, eb.Build().Errorf("no shape to map")
	}
	if err = frame.Validate(); err != nil {
		return
	}
	m.From = frame
	if frame.Kind == FrameKindPixel {
		m.How, m.To = MappingIdentity, frame
		return s, m, nil
	}
	if !frame.Matches(p) {
		to, _ := FrameOf(p)
		return px, m, eb.Build().Stringer("region", frame).Stringer("image", to).
			Errorf("region's frame is not the image's frame")
	}
	if err = p.Validate(); err != nil {
		return
	}
	m.To, _ = FrameOf(p)
	switch frame.Kind {
	case FrameKindPhysical, FrameKindReferenceSystem:
		px, m.How, err = mapAffine(s, p)
	case FrameKindCelestial:
		if math.IsNaN(tolerance) || tolerance < 0 || math.IsInf(tolerance, 0) {
			return px, m, eb.Build().Float64("tolerance", tolerance).Errorf("tolerance must be a finite number not below zero")
		}
		if tolerance == 0 {
			tolerance = DefaultTolerance
		}
		px, m.How, err = mapSky(s, p, tolerance)
		if m.How == MappingApproximated {
			m.Tolerance = tolerance
		}
	}
	if err != nil {
		return Shape{}, Mapping{}, err
	}
	if px.kind == ShapeKindPolygon && s.kind != ShapeKindPolygon {
		m.Vertices = len(px.vertices)
	}
	return px, m, nil
}

// linear is a 2 × 2 matrix, row-major.
type linear [4]float64

func (inst linear) apply(x, y float64) (float64, float64) {
	return inst[0]*x + inst[1]*y, inst[2]*x + inst[3]*y
}

func (inst linear) mul(o linear) linear {
	return linear{
		inst[0]*o[0] + inst[1]*o[2], inst[0]*o[1] + inst[1]*o[3],
		inst[2]*o[0] + inst[3]*o[2], inst[2]*o[1] + inst[3]*o[3],
	}
}

// toPixel is the linear part of the map from a placement's frame to pixels:
// the inverse of the affine's.
func toPixel(p imaging.Placement) linear {
	a := &p.Affine
	det := a[1]*a[5] - a[2]*a[4]
	return linear{a[5] / det, -a[2] / det, -a[4] / det, a[1] / det}
}

func rotation(deg float64) linear {
	s, c := sincosDeg(deg)
	return linear{c, -s, s, c}
}

func pixelOf(p imaging.Placement, q Point) (Point, error) {
	col, row, ok := p.Pixel(q.X, q.Y)
	if !ok {
		return Point{}, eb.Build().Float64("x", q.X).Float64("y", q.Y).
			Errorf("position has no pixel under the placement: not on the hemisphere it shows")
	}
	return Point{X: col, Y: row}, nil
}

func pixelsOf(p imaging.Placement, qs []Point) (out []Point, err error) {
	out = make([]Point, len(qs))
	for i, q := range qs {
		if out[i], err = pixelOf(p, q); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// mapAffine maps a shape through an affine placement's inverse.
func mapAffine(s Shape, p imaging.Placement) (px Shape, how MappingE, err error) {
	how = MappingExact
	switch s.kind {
	case ShapeKindPoint:
		var a Point
		if a, err = pixelOf(p, s.a); err == nil {
			px, err = NewPoint(a)
		}
	case ShapeKindLine:
		var ends []Point
		if ends, err = pixelsOf(p, []Point{s.a, s.b}); err == nil {
			px, err = NewLine(ends[0], ends[1])
		}
	case ShapeKindPolygon:
		var vs []Point
		if vs, err = pixelsOf(p, s.vertices); err == nil {
			px, err = NewPolygon(vs)
		}
	case ShapeKindEllipse:
		var c Point
		if c, err = pixelOf(p, s.a); err != nil {
			return
		}
		m := toPixel(p).mul(rotation(s.angle)).mul(linear{s.b.X, 0, 0, s.b.Y})
		rx, ry, angle := ellipseOf(m)
		px, err = NewEllipse(c, rx, ry, angle)
	case ShapeKindRectangle:
		var c Point
		if c, err = pixelOf(p, s.a); err != nil {
			return
		}
		m := toPixel(p).mul(rotation(s.angle)).mul(linear{s.b.X / 2, 0, 0, s.b.Y / 2})
		// The rectangle is c + {a·m₁ + b·m₂ : a, b ∈ [−1, 1]}, with m₁ and m₂
		// the columns of m: a rectangle where they are perpendicular, to
		// within rounding.
		m1x, m1y, m2x, m2y := m[0], m[2], m[1], m[3]
		l1, l2 := math.Hypot(m1x, m1y), math.Hypot(m2x, m2y)
		if math.Abs(m1x*m2x+m1y*m2y) <= 1e-12*l1*l2 {
			px, err = NewRectangle(c, 2*l1, 2*l2, math.Atan2(m1y, m1x)*180/math.Pi)
			break
		}
		how = MappingPolygon
		var vs []Point
		if vs, err = pixelsOf(p, s.corners()); err == nil {
			px, err = NewPolygon(vs)
		}
	}
	if err != nil {
		return Shape{}, 0, eb.Build().Stringer("kind", s.kind).Errorf("shape does not map to pixels: %w", err)
	}
	return px, how, nil
}

// ellipseOf is the radii and angle in degrees of the ellipse
// {m·u : |u| ≤ 1}, from the singular value decomposition of m in closed
// form: m = R(φ)·diag(σ₁, σ₂)·R(ψ), and the unit disc turned by ψ and
// mirrored is the unit disc again.
func ellipseOf(m linear) (rx, ry, angle float64) {
	e, f := (m[0]+m[3])/2, (m[0]-m[3])/2
	g, h := (m[2]+m[1])/2, (m[2]-m[1])/2
	q, r := math.Hypot(e, h), math.Hypot(f, g)
	a1, a2 := math.Atan2(g, f), math.Atan2(h, e)
	return q + r, math.Abs(q - r), (a2 + a1) / 2 * 180 / math.Pi
}

// mapSky maps a shape on the sky through a gnomonic placement.
func mapSky(s Shape, p imaging.Placement, tolerance float64) (px Shape, how MappingE, err error) {
	how = MappingExact
	ref := unitVector(p.Frame.RefLon, p.Frame.RefLat)
	visible := func(v [3]float64) bool { return dot(v, ref) > 0 }
	switch s.kind {
	case ShapeKindPoint:
		var a Point
		if a, err = pixelOf(p, s.a); err == nil {
			px, err = NewPoint(a)
		}
	case ShapeKindLine:
		var ends []Point
		if ends, err = pixelsOf(p, []Point{s.a, s.b}); err == nil {
			px, err = NewLine(ends[0], ends[1])
		}
	case ShapeKindPolygon:
		// The shorter great-circle arc between two points of the open
		// hemisphere stays in it, so the vertices decide.
		var vs []Point
		if vs, err = pixelsOf(p, s.vertices); err == nil {
			px, err = NewPolygon(vs)
		}
	case ShapeKindRectangle:
		how = MappingPolygon
		t := newTangentPlane(s.a)
		cs := s.cornerOffsets()
		vs := make([]Point, len(cs))
		for i, c := range cs {
			v := t.direction(c.X, c.Y)
			if !visible(v) {
				return Shape{}, 0, eb.Build().Int("corner", i).Errorf("rectangle reaches the hemisphere the projection cannot show")
			}
			if vs[i], err = pixelOf(p, lonLat(v)); err != nil {
				return Shape{}, 0, err
			}
		}
		px, err = NewPolygon(vs)
	case ShapeKindEllipse:
		how = MappingApproximated
		t := newTangentPlane(s.a)
		if !t.ellipseVisible(s, ref) {
			return Shape{}, 0, eb.Build().Errorf("ellipse reaches the hemisphere the projection cannot show")
		}
		var vs []Point
		if vs, err = approximateEllipse(s, t, p, tolerance); err == nil {
			px, err = NewPolygon(vs)
		}
	}
	if err != nil {
		return Shape{}, 0, eb.Build().Stringer("kind", s.kind).Errorf("shape does not map to pixels: %w", err)
	}
	return px, how, nil
}

func unitVector(lon, lat float64) [3]float64 {
	so, co := sincosDeg(lon)
	sa, ca := sincosDeg(lat)
	return [3]float64{ca * co, ca * so, sa}
}

func dot(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

func lonLat(v [3]float64) Point {
	lon := math.Atan2(v[1], v[0]) * 180 / math.Pi
	if lon < 0 {
		lon += 360
	}
	return Point{X: lon, Y: math.Atan2(v[2], math.Hypot(v[0], v[1])) * 180 / math.Pi}
}

// tangentPlane is the plane tangent to the sky at a centre: a point (ξ, η)
// of it, in degrees, +ξ east and +η north, is the direction
// centre + ξ·east + η·north (in radians), the gnomonic projection about the
// centre.
type tangentPlane struct {
	centre, east, north [3]float64
}

func newTangentPlane(c Point) (t tangentPlane) {
	so, co := sincosDeg(c.X)
	sa, ca := sincosDeg(c.Y)
	t.centre = [3]float64{ca * co, ca * so, sa}
	t.east = [3]float64{-so, co, 0}
	t.north = [3]float64{-sa * co, -sa * so, ca}
	return t
}

func (inst tangentPlane) direction(xi, eta float64) (v [3]float64) {
	x, y := xi*math.Pi/180, eta*math.Pi/180
	for i := range v {
		v[i] = inst.centre[i] + x*inst.east[i] + y*inst.north[i]
	}
	n := math.Sqrt(dot(v, v))
	for i := range v {
		v[i] /= n
	}
	return v
}

// ellipseVisible reports whether every point of an ellipse laid out in the
// plane lies on the hemisphere about ref: the direction of (ξ, η) is a
// positive multiple of centre + ξ·east + η·north, so the condition is a
// half-plane, and the ellipse lies in it when the linear function's
// smallest value over the ellipse is positive.
func (inst tangentPlane) ellipseVisible(s Shape, ref [3]float64) bool {
	g0 := dot(inst.centre, ref)
	gx, gy := dot(inst.east, ref)*math.Pi/180, dot(inst.north, ref)*math.Pi/180
	// The ellipse is R(θ)·diag(rx, ry)·u for |u| ≤ 1; the smallest value of
	// g·(ξ, η) over it is −|(R(θ)·diag(rx, ry))ᵀ g|.
	m := rotation(s.angle).mul(linear{s.b.X, 0, 0, s.b.Y})
	tx, ty := m[0]*gx+m[2]*gy, m[1]*gx+m[3]*gy
	return g0-math.Hypot(tx, ty) > 0
}

// approximateEllipse is a polygon whose vertices lie on an ellipse on the
// sky as the placement maps it, refined from 16 vertices by halving any
// edge whose outline strays from it by more than tolerance pixels at a
// quarter, the half or three quarters of the way along the edge's span of
// the ellipse's parameter.
func approximateEllipse(s Shape, t tangentPlane, p imaging.Placement, tolerance float64) (vs []Point, err error) {
	sn, cs := sincosDeg(s.angle)
	rx, ry := s.b.X, s.b.Y
	at := func(u float64) (Point, error) {
		su, cu := math.Sincos(u)
		x, y := rx*cu, ry*su
		v := t.direction(cs*x-sn*y, sn*x+cs*y)
		return pixelOf(p, lonLat(v))
	}
	type piece struct {
		u0, u1 float64
		p0, p1 Point
	}
	const start = 16
	stack := make([]piece, 0, 64)
	first, err := at(0)
	if err != nil {
		return nil, err
	}
	prev := first
	pieces := make([]piece, 0, start)
	for i := 1; i <= start; i++ {
		u := 2 * math.Pi * float64(i) / start
		q := first
		if i < start {
			if q, err = at(u); err != nil {
				return nil, err
			}
		}
		pieces = append(pieces, piece{u0: 2 * math.Pi * float64(i-1) / start, u1: u, p0: prev, p1: q})
		prev = q
	}
	for i := len(pieces) - 1; i >= 0; i-- {
		stack = append(stack, pieces[i])
	}
	vs = make([]Point, 0, 4*start)
	for len(stack) > 0 {
		pc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		worst := 0.0
		var mid Point
		for _, f := range [...]float64{0.25, 0.5, 0.75} {
			q, qerr := at(pc.u0 + f*(pc.u1-pc.u0))
			if qerr != nil {
				return nil, qerr
			}
			if f == 0.5 {
				mid = q
			}
			worst = max(worst, segmentDistance(q, pc.p0, pc.p1))
		}
		if worst <= tolerance {
			vs = append(vs, pc.p0)
			if len(vs) > MaxApproximationVertices {
				return nil, eb.Build().Int("limit", MaxApproximationVertices).Float64("tolerance", tolerance).
					Errorf("ellipse needs more vertices than the limit at this tolerance")
			}
			continue
		}
		um := (pc.u0 + pc.u1) / 2
		if len(stack)+len(vs) > MaxApproximationVertices || !(um > pc.u0 && um < pc.u1) {
			return nil, eb.Build().Int("limit", MaxApproximationVertices).Float64("tolerance", tolerance).
				Errorf("ellipse needs more vertices than the limit at this tolerance")
		}
		stack = append(stack, piece{u0: um, u1: pc.u1, p0: mid, p1: pc.p1}, piece{u0: pc.u0, u1: um, p0: pc.p0, p1: mid})
	}
	return vs, nil
}

// segmentDistance is the distance from q to the segment from a to b.
func segmentDistance(q, a, b Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 0 {
		t = min(max(((q.X-a.X)*dx+(q.Y-a.Y)*dy)/l2, 0), 1)
	}
	return math.Hypot(q.X-(a.X+t*dx), q.Y-(a.Y+t*dy))
}
