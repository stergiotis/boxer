package regions

import (
	"math"
	"math/big"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ShapeKindE says which of the five shapes a [Shape] is (ADR-0399 §SD1). The
// zero value is no shape and does not validate.
type ShapeKindE uint8

const (
	ShapeKindPoint ShapeKindE = iota + 1
	ShapeKindLine
	ShapeKindRectangle
	ShapeKindEllipse
	ShapeKindPolygon
)

// AllShapeKinds lists the kinds a shape can have.
var AllShapeKinds = []ShapeKindE{ShapeKindPoint, ShapeKindLine, ShapeKindRectangle, ShapeKindEllipse, ShapeKindPolygon}

// String is the kind's name in a region document.
func (inst ShapeKindE) String() string {
	switch inst {
	case ShapeKindPoint:
		return "point"
	case ShapeKindLine:
		return "line"
	case ShapeKindRectangle:
		return "rectangle"
	case ShapeKindEllipse:
		return "ellipse"
	case ShapeKindPolygon:
		return "polygon"
	}
	return "none"
}

// IsArea reports whether shapes of the kind enclose an area: a rectangle, an
// ellipse or a polygon, which coverage by area applies to (ADR-0399 §SD4).
func (inst ShapeKindE) IsArea() bool {
	return inst == ShapeKindRectangle || inst == ShapeKindEllipse || inst == ShapeKindPolygon
}

// MaxMagnitude bounds every coordinate and size a shape holds, so that no
// difference or product of two of them overflows and a pixel index derived
// from one fits an int.
const MaxMagnitude = 1e15

// Point is a position in a region's frame: in pixel coordinates X is the
// column and Y the row coordinate; in a celestial frame X is the longitude
// and Y the latitude in degrees; in any other frame the frame's two
// coordinates in its order.
type Point struct {
	X, Y float64
}

// Shape is one of five shapes kept by its parameters (ADR-0399 §SD1): never
// replaced by an approximation of itself, so an ellipse read back is an
// ellipse. It is made by [NewPoint], [NewLine], [NewRectangle], [NewEllipse]
// or [NewPolygon], which validate it; the zero value is no shape.
//
// Angles are in degrees, and a positive angle turns the +x axis toward the
// +y axis. In pixel coordinates rows grow downward, so a positive angle turns
// clockwise on screen; in a frame whose y axis points north it turns
// counter-clockwise on a map. The convention is kept through every mapping:
// a mapped shape's angle is measured in the coordinates it is mapped to.
type Shape struct {
	kind ShapeKindE
	// a is the point, the line's start, or the centre; b the line's end, a
	// rectangle's width and height, or an ellipse's two radii.
	a, b     Point
	angle    float64
	vertices []Point
}

func checkCoordinate(name string, v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > MaxMagnitude {
		return eb.Build().Str("parameter", name).Float64("value", v).Float64("limit", MaxMagnitude).
			Errorf("shape parameter is not a finite number within the limit")
	}
	return nil
}

func checkPoint(name string, p Point) error {
	if err := checkCoordinate(name+".x", p.X); err != nil {
		return err
	}
	return checkCoordinate(name+".y", p.Y)
}

func checkSize(name string, v float64) error {
	if err := checkCoordinate(name, v); err != nil {
		return err
	}
	if !(v > 0) {
		return eb.Build().Str("parameter", name).Float64("value", v).Errorf("shape size must be positive")
	}
	return nil
}

func checkAngle(v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return eb.Build().Float64("angle", v).Errorf("shape angle is not a finite number")
	}
	return nil
}

// NewPoint is a point.
func NewPoint(at Point) (s Shape, err error) {
	if err = checkPoint("at", at); err != nil {
		return
	}
	return Shape{kind: ShapeKindPoint, a: at}, nil
}

// NewLine is the segment from one point to another. Both may be the same
// point: a line of zero length.
func NewLine(from, to Point) (s Shape, err error) {
	if err = checkPoint("from", from); err != nil {
		return
	}
	if err = checkPoint("to", to); err != nil {
		return
	}
	return Shape{kind: ShapeKindLine, a: from, b: to}, nil
}

// NewRectangle is a rectangle about centre, width along its own x axis and
// height along its own y axis, turned by angle degrees (see [Shape]).
func NewRectangle(centre Point, width, height, angle float64) (s Shape, err error) {
	if err = checkPoint("centre", centre); err != nil {
		return
	}
	if err = checkSize("width", width); err != nil {
		return
	}
	if err = checkSize("height", height); err != nil {
		return
	}
	if err = checkAngle(angle); err != nil {
		return
	}
	return Shape{kind: ShapeKindRectangle, a: centre, b: Point{X: width, Y: height}, angle: angle}, nil
}

// NewEllipse is an ellipse about centre with radius rx along its own x axis
// and ry along its own y axis, turned by angle degrees (see [Shape]). A
// circle is an ellipse whose radii are equal.
func NewEllipse(centre Point, rx, ry, angle float64) (s Shape, err error) {
	if err = checkPoint("centre", centre); err != nil {
		return
	}
	if err = checkSize("rx", rx); err != nil {
		return
	}
	if err = checkSize("ry", ry); err != nil {
		return
	}
	if err = checkAngle(angle); err != nil {
		return
	}
	return Shape{kind: ShapeKindEllipse, a: centre, b: Point{X: rx, Y: ry}, angle: angle}, nil
}

// NewPolygon is the polygon of one ring through vertices, closed from the
// last vertex back to the first. It needs at least three vertices and a
// signed (shoelace) area that is not zero. A ring that crosses itself is not
// refused: its inside is decided by the even-odd rule — a point is inside
// when a ray from it crosses the ring an odd number of times — by both
// coverage rules alike. A figure-eight whose two loops have equal area has a
// signed area of zero and is refused. The vertices are copied.
func NewPolygon(vertices []Point) (s Shape, err error) {
	if len(vertices) < 3 {
		return s, eb.Build().Int("vertices", len(vertices)).Errorf("a polygon needs at least three vertices")
	}
	for i, v := range vertices {
		if err = checkPoint("vertex", v); err != nil {
			return s, eb.Build().Int("vertex", i).Errorf("polygon vertex: %w", err)
		}
	}
	if signedAreaSign(vertices) == 0 {
		return s, eb.Build().Int("vertices", len(vertices)).Errorf("polygon has no area")
	}
	return Shape{kind: ShapeKindPolygon, vertices: slices.Clone(vertices)}, nil
}

// signedArea is the shoelace area of a ring: positive when it turns from +x
// toward +y.
func signedArea(vs []Point) float64 {
	var sum float64
	o := vs[0]
	for i := 1; i+1 < len(vs); i++ {
		a, b := vs[i], vs[i+1]
		sum += (a.X-o.X)*(b.Y-o.Y) - (b.X-o.X)*(a.Y-o.Y)
	}
	return sum / 2
}

// signedAreaSign is the sign of [signedArea], decided exactly: a sliver
// whose area rounds to zero still has one.
func signedAreaSign(vs []Point) int {
	var sum, mag float64
	o := vs[0]
	for i := 1; i+1 < len(vs); i++ {
		a, b := vs[i], vs[i+1]
		p, q := (a.X-o.X)*(b.Y-o.Y), (b.X-o.X)*(a.Y-o.Y)
		sum += p - q
		mag += math.Abs(p) + math.Abs(q)
	}
	// Each term carries a relative error of about 3ε, and the running sum
	// adds ε per term; 8(n + 2)ε of the magnitude bounds both.
	if mag > 0x1p-900 && math.Abs(sum) > 8*float64(len(vs)+2)*0x1p-53*mag {
		return sign(sum)
	}
	var total, x, y big.Rat
	for i := 1; i+1 < len(vs); i++ {
		a, b := vs[i], vs[i+1]
		x.Mul(ratDiff(a.X, o.X), ratDiff(b.Y, o.Y))
		y.Mul(ratDiff(b.X, o.X), ratDiff(a.Y, o.Y))
		total.Add(&total, x.Sub(&x, &y))
	}
	return total.Sign()
}

// Kind is the shape's kind; zero for the zero value.
func (inst Shape) Kind() ShapeKindE { return inst.kind }

// IsZero reports whether the shape is no shape.
func (inst Shape) IsZero() bool { return inst.kind == 0 }

// At is a point's position; the zero point for any other kind.
func (inst Shape) At() Point {
	if inst.kind != ShapeKindPoint {
		return Point{}
	}
	return inst.a
}

// Ends is a line's start and end; zero points for any other kind.
func (inst Shape) Ends() (from, to Point) {
	if inst.kind != ShapeKindLine {
		return
	}
	return inst.a, inst.b
}

// Centre is a rectangle's or an ellipse's centre; the zero point for any
// other kind.
func (inst Shape) Centre() Point {
	if inst.kind != ShapeKindRectangle && inst.kind != ShapeKindEllipse {
		return Point{}
	}
	return inst.a
}

// Size is a rectangle's width and height; zero for any other kind.
func (inst Shape) Size() (width, height float64) {
	if inst.kind != ShapeKindRectangle {
		return
	}
	return inst.b.X, inst.b.Y
}

// Radii is an ellipse's radii along its own x and y axes; zero for any other
// kind.
func (inst Shape) Radii() (rx, ry float64) {
	if inst.kind != ShapeKindEllipse {
		return
	}
	return inst.b.X, inst.b.Y
}

// Angle is a rectangle's or an ellipse's rotation in degrees (see [Shape]);
// zero for any other kind.
func (inst Shape) Angle() float64 { return inst.angle }

// Vertices is a polygon's ring, a copy; nil for any other kind.
func (inst Shape) Vertices() []Point { return slices.Clone(inst.vertices) }

// Outline is the vertices of a polygon, or the four corners of a rectangle
// in the order its own axes give them — (−w/2, −h/2), (+w/2, −h/2),
// (+w/2, +h/2), (−w/2, +h/2) turned by the angle and moved to the centre —
// and nil for any other kind. A rectangle's corners are exact where the
// angle is a multiple of 90° and the half sizes added to the centre are.
func (inst Shape) Outline() []Point {
	switch inst.kind {
	case ShapeKindPolygon:
		return slices.Clone(inst.vertices)
	case ShapeKindRectangle:
		return inst.corners()
	}
	return nil
}

func (inst Shape) corners() []Point {
	out := inst.cornerOffsets()
	for i := range out {
		out[i].X += inst.a.X
		out[i].Y += inst.a.Y
	}
	return out
}

// cornerOffsets is a rectangle's corners relative to its centre.
func (inst Shape) cornerOffsets() []Point {
	s, c := sincosDeg(inst.angle)
	hw, hh := inst.b.X/2, inst.b.Y/2
	out := make([]Point, 0, 4)
	for _, u := range [4][2]float64{{-hw, -hh}, {hw, -hh}, {hw, hh}, {-hw, hh}} {
		out = append(out, Point{X: c*u[0] - s*u[1], Y: s*u[0] + c*u[1]})
	}
	return out
}

// Bounds is the smallest axis-aligned box holding the shape, in its own
// coordinates.
func (inst Shape) Bounds() (lo, hi Point) {
	switch inst.kind {
	case ShapeKindPoint:
		return inst.a, inst.a
	case ShapeKindLine:
		return Point{X: min(inst.a.X, inst.b.X), Y: min(inst.a.Y, inst.b.Y)},
			Point{X: max(inst.a.X, inst.b.X), Y: max(inst.a.Y, inst.b.Y)}
	case ShapeKindEllipse:
		e := newEllipseGeom(inst)
		return Point{X: e.cx - e.hx, Y: e.cy - e.hy}, Point{X: e.cx + e.hx, Y: e.cy + e.hy}
	case ShapeKindRectangle, ShapeKindPolygon:
		vs := inst.vertices
		if inst.kind == ShapeKindRectangle {
			vs = inst.corners()
		}
		lo, hi = vs[0], vs[0]
		for _, v := range vs[1:] {
			lo.X, lo.Y = min(lo.X, v.X), min(lo.Y, v.Y)
			hi.X, hi.Y = max(hi.X, v.X), max(hi.Y, v.Y)
		}
		return lo, hi
	}
	return
}

// sincosDeg is the sine and cosine of an angle in degrees, exact at the
// multiples of 90°.
func sincosDeg(deg float64) (s, c float64) {
	r := math.Mod(deg, 360)
	if r < 0 {
		r += 360
	}
	switch r {
	case 0:
		return 0, 1
	case 90:
		return 1, 0
	case 180:
		return 0, -1
	case 270:
		return -1, 0
	}
	return math.Sincos(r * math.Pi / 180)
}
