package portolan

import "math"

// Point is a position or a size in pixel space (src/geometry/Point.js). A
// value type: every method returns a new Point and leaves its receiver as it
// was — Leaflet's non-underscore forms.
type Point struct {
	X, Y float64
}

// Pt is the Point literal.
func Pt(x, y float64) Point { return Point{X: x, Y: y} }

// Add returns p + q.
func (p Point) Add(q Point) Point { return Point{p.X + q.X, p.Y + q.Y} }

// Subtract returns p − q.
func (p Point) Subtract(q Point) Point { return Point{p.X - q.X, p.Y - q.Y} }

// MultiplyBy scales both coordinates by n.
func (p Point) MultiplyBy(n float64) Point { return Point{p.X * n, p.Y * n} }

// DivideBy divides both coordinates by n.
func (p Point) DivideBy(n float64) Point { return Point{p.X / n, p.Y / n} }

// ScaleBy multiplies coordinate-wise — p.X·q.X, p.Y·q.Y.
func (p Point) ScaleBy(q Point) Point { return Point{p.X * q.X, p.Y * q.Y} }

// UnscaleBy divides coordinate-wise — p.X/q.X, p.Y/q.Y.
func (p Point) UnscaleBy(q Point) Point { return Point{p.X / q.X, p.Y / q.Y} }

// Round rounds both coordinates, half toward +∞ like JavaScript's Math.round.
func (p Point) Round() Point { return Point{jsRound(p.X), jsRound(p.Y)} }

// Floor floors both coordinates.
func (p Point) Floor() Point { return Point{math.Floor(p.X), math.Floor(p.Y)} }

// Ceil ceils both coordinates.
func (p Point) Ceil() Point { return Point{math.Ceil(p.X), math.Ceil(p.Y)} }

// Trunc truncates both coordinates toward zero.
func (p Point) Trunc() Point { return Point{math.Trunc(p.X), math.Trunc(p.Y)} }

// DistanceTo is the Euclidean distance to q.
func (p Point) DistanceTo(q Point) float64 {
	dx, dy := q.X-p.X, q.Y-p.Y
	return math.Sqrt(dx*dx + dy*dy)
}

// Equals reports exact coordinate equality.
func (p Point) Equals(q Point) bool { return p.X == q.X && p.Y == q.Y }

// Contains reports whether q lies within p taken as a size: |q.X| ≤ |p.X| and
// |q.Y| ≤ |p.Y|.
func (p Point) Contains(q Point) bool {
	return math.Abs(q.X) <= math.Abs(p.X) && math.Abs(q.Y) <= math.Abs(p.Y)
}

// String renders "Point(x, y)" to six decimals, as Leaflet's toString does.
func (p Point) String() string {
	return "Point(" + fmtNum(p.X, 6) + ", " + fmtNum(p.Y, 6) + ")"
}

// Bounds is an axis-aligned rectangle in pixel space (src/geometry/Bounds.js):
// Min is its top-left in screen terms (smallest x and y), Max its
// bottom-right. The zero value is the empty bounds — IsValid reports false and
// the first Extend sets both corners — which is what Leaflet's `new Bounds()`
// with no points is.
type Bounds struct {
	Min, Max Point
	valid    bool
}

// NewBounds is the bounds of the given points; none gives the empty bounds.
func NewBounds(points ...Point) (b Bounds) {
	for _, p := range points {
		b = b.Extend(p)
	}
	return
}

// BoundsOf is the bounds spanned by two corners, in any order.
func BoundsOf(a, b Point) Bounds { return NewBounds(a, b) }

// IsValid reports whether the bounds has been given at least one point.
func (b Bounds) IsValid() bool { return b.valid }

// Extend returns the bounds grown to include p.
func (b Bounds) Extend(p Point) Bounds {
	if !b.valid {
		return Bounds{Min: p, Max: p, valid: true}
	}
	return Bounds{
		Min:   Point{math.Min(p.X, b.Min.X), math.Min(p.Y, b.Min.Y)},
		Max:   Point{math.Max(p.X, b.Max.X), math.Max(p.Y, b.Max.Y)},
		valid: true,
	}
}

// ExtendBounds returns the bounds grown to include o; an empty o changes
// nothing.
func (b Bounds) ExtendBounds(o Bounds) Bounds {
	if !o.valid {
		return b
	}
	return b.Extend(o.Min).Extend(o.Max)
}

// GetCenter is the centre point.
func (b Bounds) GetCenter() Point {
	return Point{(b.Min.X + b.Max.X) / 2, (b.Min.Y + b.Max.Y) / 2}
}

// GetBottomLeft is (Min.X, Max.Y).
func (b Bounds) GetBottomLeft() Point { return Point{b.Min.X, b.Max.Y} }

// GetTopRight is (Max.X, Min.Y).
func (b Bounds) GetTopRight() Point { return Point{b.Max.X, b.Min.Y} }

// GetTopLeft is Min.
func (b Bounds) GetTopLeft() Point { return b.Min }

// GetBottomRight is Max.
func (b Bounds) GetBottomRight() Point { return b.Max }

// GetSize is Max − Min.
func (b Bounds) GetSize() Point { return b.Max.Subtract(b.Min) }

// Contains reports whether p lies inside or on the bounds.
func (b Bounds) Contains(p Point) bool {
	return p.X >= b.Min.X && p.X <= b.Max.X && p.Y >= b.Min.Y && p.Y <= b.Max.Y
}

// ContainsBounds reports whether o lies entirely inside or on the bounds.
func (b Bounds) ContainsBounds(o Bounds) bool {
	return o.Min.X >= b.Min.X && o.Max.X <= b.Max.X && o.Min.Y >= b.Min.Y && o.Max.Y <= b.Max.Y
}

// Intersects reports whether the two rectangles share at least a point,
// touching edges included.
func (b Bounds) Intersects(o Bounds) bool {
	return o.Max.X >= b.Min.X && o.Min.X <= b.Max.X && o.Max.Y >= b.Min.Y && o.Min.Y <= b.Max.Y
}

// Overlaps reports whether the two rectangles share an area — touching edges
// excluded.
func (b Bounds) Overlaps(o Bounds) bool {
	return o.Max.X > b.Min.X && o.Min.X < b.Max.X && o.Max.Y > b.Min.Y && o.Min.Y < b.Max.Y
}

// Pad grows the bounds by ratio × its extent on every side.
func (b Bounds) Pad(ratio float64) Bounds {
	bx := math.Abs(b.Min.X-b.Max.X) * ratio
	by := math.Abs(b.Min.Y-b.Max.Y) * ratio
	return BoundsOf(Point{b.Min.X - bx, b.Min.Y - by}, Point{b.Max.X + bx, b.Max.Y + by})
}

// Equals reports whether both bounds are valid and share both corners.
func (b Bounds) Equals(o Bounds) bool {
	return b.valid && o.valid && b.Min.Equals(o.Min) && b.Max.Equals(o.Max)
}
