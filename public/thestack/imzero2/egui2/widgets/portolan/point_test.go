// Ports spec/suites/geometry/PointSpec.js and spec/suites/geometry/
// BoundsSpec.js from Leaflet at c96f31a7a350a07cfbc852cf88e6ca69af5f5ec9.
// Top-level functions follow the upstream `describe` groups, subtests carry
// the upstream `it` titles.
//
// Not ported from upstream (JavaScript-specific, no Go analogue):
//
//	PointSpec "Point creation" › "leaves Point instances as is" —
//	  constructor identity on a Point argument; Point is a value type here.
//	PointSpec "Point creation" › "creates a point from an array of
//	  coordinates" — the [x, y] constructor form.
//	PointSpec "Point creation" › "creates a point from an object with x and y
//	  properties" — the {x, y} constructor form.
//	PointSpec "Point creation" › "does not fail on invalid arguments" —
//	  Point.validate and throwing on undefined / null.
//	BoundsSpec "#extend" › "extends the bounds by undefined" — Extend with
//	  no argument.
//	BoundsSpec "#extend" › "extends the bounds by raw object" — the {x, y}
//	  constructor form (Extend by the same point is covered above it).
//	BoundsSpec "Bounds creation" › "creates bounds from array of number
//	  arrays" — the [[x, y], [x, y]] constructor form.
//
// Two PointSpec cases about the constructor's round flag — "rounds the given
// x and y if the third argument is true" and "creates a point out of three
// arguments" — are ported onto Point.Round, which is what that flag calls.

package portolan

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPoint_Constructor(t *testing.T) {
	t.Run("creates a point with the given x and y", func(t *testing.T) {
		p := Pt(1.5, 2.5)
		assert.Equal(t, 1.5, p.X)
		assert.Equal(t, 2.5, p.Y)
	})

	t.Run("rounds the given x and y if the third argument is true", func(t *testing.T) {
		assert.Equal(t, Pt(1, 3), Pt(1.3, 2.7).Round())
		// Port addition: the rounding is JavaScript's Math.round — half
		// toward +∞ — which doc.go promises and Go's math.Round would break.
		assert.Equal(t, Pt(-2, 3), Pt(-2.5, 2.5).Round())
	})
}

func TestPoint_Subtract(t *testing.T) {
	t.Run("subtracts the given point from this one", func(t *testing.T) {
		a, b := Pt(50, 30), Pt(20, 10)
		assert.Equal(t, Pt(30, 20), a.Subtract(b))
	})
}

func TestPoint_Add(t *testing.T) {
	t.Run("adds given point to this one", func(t *testing.T) {
		assert.Equal(t, Pt(70, 40), Pt(50, 30).Add(Pt(20, 10)))
	})
}

func TestPoint_DivideBy(t *testing.T) {
	t.Run("divides this point by the given amount", func(t *testing.T) {
		assert.Equal(t, Pt(10, 6), Pt(50, 30).DivideBy(5))
	})
}

func TestPoint_MultiplyBy(t *testing.T) {
	t.Run("multiplies this point by the given amount", func(t *testing.T) {
		assert.Equal(t, Pt(100, 60), Pt(50, 30).MultiplyBy(2))
	})
}

func TestPoint_Floor(t *testing.T) {
	t.Run("returns a new point with floored coordinates", func(t *testing.T) {
		assert.Equal(t, Pt(50, 30), Pt(50.56, 30.123).Floor())
		assert.Equal(t, Pt(-51, -31), Pt(-50.56, -30.123).Floor())
	})
}

func TestPoint_Trunc(t *testing.T) {
	t.Run("returns a new point with truncated coordinates", func(t *testing.T) {
		assert.Equal(t, Pt(50, 30), Pt(50.56, 30.123).Trunc())
		assert.Equal(t, Pt(-50, -30), Pt(-50.56, -30.123).Trunc())
	})
}

func TestPoint_DistanceTo(t *testing.T) {
	t.Run("calculates distance between two points", func(t *testing.T) {
		p1 := Pt(0, 30)
		p2 := Pt(40, 0)
		assert.Equal(t, 50.0, p1.DistanceTo(p2))
	})
}

func TestPoint_Equals(t *testing.T) {
	t.Run("returns true if points are equal", func(t *testing.T) {
		p1 := Pt(20.4, 50.12)
		p2 := Pt(20.4, 50.12)
		p3 := Pt(20.5, 50.13)

		assert.True(t, p1.Equals(p2))
		assert.False(t, p1.Equals(p3))
	})
}

func TestPoint_Contains(t *testing.T) {
	t.Run("returns true if the point is bigger in absolute dimensions than the passed one", func(t *testing.T) {
		p1 := Pt(50, 30)
		p2 := Pt(-40, 20)
		p3 := Pt(60, -20)
		p4 := Pt(-40, -40)

		assert.True(t, p1.Contains(p2))
		assert.False(t, p1.Contains(p3))
		assert.False(t, p1.Contains(p4))
	})
}

func TestPoint_ToString(t *testing.T) {
	t.Run("formats a string out of point coordinates", func(t *testing.T) {
		assert.Equal(t, "Point(50, 30)", Pt(50, 30).String())
		assert.Equal(t, "Point(50.123457, 30.123457)", Pt(50.1234567, 30.1234567).String())
	})
}

func TestPoint_Creation(t *testing.T) {
	t.Run("creates a point out of three arguments", func(t *testing.T) {
		assert.Equal(t, Pt(50, 30), Pt(50.1, 30.1).Round())
	})
}

// boundsFixture is BoundsSpec's beforeEach: a spans two corners, b is built
// from three points, c is the empty bounds. Bounds is a value type, so each
// test works on its own copy.
func boundsFixture() (a, b, c Bounds) {
	a = BoundsOf(
		Pt(14, 12), // left, top
		Pt(30, 40)) // right, bottom
	b = NewBounds(
		Pt(20, 12), // center, top
		Pt(14, 20), // left, middle
		Pt(30, 40)) // right, bottom
	return a, b, Bounds{}
}

func TestBounds_Constructor(t *testing.T) {
	t.Run("creates bounds with proper min & max on (Point, Point)", func(t *testing.T) {
		a, _, _ := boundsFixture()
		assert.Equal(t, Pt(14, 12), a.Min)
		assert.Equal(t, Pt(30, 40), a.Max)
	})

	t.Run("creates bounds with proper min & max on (Point[])", func(t *testing.T) {
		_, b, _ := boundsFixture()
		assert.Equal(t, Pt(14, 12), b.Min)
		assert.Equal(t, Pt(30, 40), b.Max)
	})
}

func TestBounds_Extend(t *testing.T) {
	t.Run("extends the bounds to contain the given point", func(t *testing.T) {
		a, b, _ := boundsFixture()
		a = a.Extend(Pt(50, 20))
		assert.Equal(t, Pt(14, 12), a.Min)
		assert.Equal(t, Pt(50, 40), a.Max)

		b = b.Extend(Pt(25, 50))
		assert.Equal(t, Pt(14, 12), b.Min)
		assert.Equal(t, Pt(30, 50), b.Max)
	})

	// Upstream has two cases under this title: the first extends by a point,
	// the second by a bounds.
	t.Run("extends the bounds by given bounds", func(t *testing.T) {
		a, _, _ := boundsFixture()
		a = a.Extend(Pt(20, 50))
		assert.Equal(t, Pt(30, 50), a.Max)
	})

	t.Run("extends the bounds by given bounds (a Bounds argument)", func(t *testing.T) {
		a, _, _ := boundsFixture()
		a = a.ExtendBounds(BoundsOf(Pt(20, 50), Pt(8, 40)))
		assert.Equal(t, Pt(8, 50), a.GetBottomLeft())
	})

	t.Run("extend the bounds by an empty bounds object", func(t *testing.T) {
		a, _, _ := boundsFixture()
		assert.Equal(t, a, a.ExtendBounds(Bounds{}))
	})
}

func TestBounds_GetCenter(t *testing.T) {
	t.Run("returns the center point", func(t *testing.T) {
		a, _, _ := boundsFixture()
		assert.Equal(t, Pt(22, 26), a.GetCenter())
	})
}

func TestBounds_Pad(t *testing.T) {
	t.Run("pads the bounds by a given ratio", func(t *testing.T) {
		a, _, _ := boundsFixture()
		assert.Equal(t, BoundsOf(Pt(6, -2), Pt(38, 54)), a.Pad(0.5))
	})
}

func TestBounds_Contains(t *testing.T) {
	t.Run("contains other bounds or point", func(t *testing.T) {
		a, b, _ := boundsFixture()
		a = a.Extend(Pt(50, 10))
		assert.True(t, a.ContainsBounds(b))
		assert.False(t, b.ContainsBounds(a))
		assert.True(t, a.Contains(Pt(24, 25)))
		assert.False(t, a.Contains(Pt(54, 65)))
	})
}

func TestBounds_IsValid(t *testing.T) {
	t.Run("returns true if properly set up", func(t *testing.T) {
		a, _, _ := boundsFixture()
		assert.True(t, a.IsValid())
	})

	t.Run("returns false if is invalid", func(t *testing.T) {
		_, _, c := boundsFixture()
		assert.False(t, c.IsValid())
	})

	t.Run("returns true if extended", func(t *testing.T) {
		_, _, c := boundsFixture()
		c = c.Extend(Pt(0, 0))
		assert.True(t, c.IsValid())
	})
}

func TestBounds_GetSize(t *testing.T) {
	t.Run("returns the size of the bounds as point", func(t *testing.T) {
		a, _, _ := boundsFixture()
		assert.Equal(t, Pt(16, 28), a.GetSize())
	})
}

func TestBounds_Intersects(t *testing.T) {
	t.Run("returns true if bounds intersect", func(t *testing.T) {
		a, b, _ := boundsFixture()
		assert.True(t, a.Intersects(b))
	})

	t.Run("two bounds intersect if they have at least one point in common", func(t *testing.T) {
		a, _, _ := boundsFixture()
		assert.True(t, a.Intersects(BoundsOf(Pt(14, 12), Pt(6, 5))))
	})

	t.Run("returns false if bounds not intersect", func(t *testing.T) {
		a, _, _ := boundsFixture()
		assert.False(t, a.Intersects(BoundsOf(Pt(100, 100), Pt(120, 120))))
	})
}

func TestBounds_Overlaps(t *testing.T) {
	t.Run("returns true if bounds overlaps", func(t *testing.T) {
		a, b, _ := boundsFixture()
		assert.True(t, a.Overlaps(b))
	})

	t.Run("two bounds overlaps if their intersection is an area", func(t *testing.T) {
		a, _, _ := boundsFixture()
		// point in common
		assert.False(t, a.Overlaps(BoundsOf(Pt(14, 12), Pt(6, 5))))
		// matching boundary
		assert.False(t, a.Overlaps(BoundsOf(Pt(30, 12), Pt(35, 25))))
	})

	t.Run("returns false if bounds not overlaps", func(t *testing.T) {
		a, _, _ := boundsFixture()
		assert.False(t, a.Overlaps(BoundsOf(Pt(100, 100), Pt(120, 120))))
	})
}

func TestBounds_GetBottomLeft(t *testing.T) {
	t.Run("returns the proper bounds bottom-left value", func(t *testing.T) {
		a, _, _ := boundsFixture()
		assert.Equal(t, Pt(14, 40), a.GetBottomLeft()) // left, bottom
	})
}

func TestBounds_GetTopRight(t *testing.T) {
	t.Run("returns the proper bounds top-right value", func(t *testing.T) {
		a, _, _ := boundsFixture()
		assert.Equal(t, Pt(30, 12), a.GetTopRight()) // right, top
	})
}

func TestBounds_GetTopLeft(t *testing.T) {
	t.Run("returns the proper bounds top-left value", func(t *testing.T) {
		a, _, _ := boundsFixture()
		assert.Equal(t, Pt(14, 12), a.GetTopLeft()) // left, top
	})
}

func TestBounds_GetBottomRight(t *testing.T) {
	t.Run("returns the proper bounds bottom-right value", func(t *testing.T) {
		a, _, _ := boundsFixture()
		assert.Equal(t, Pt(30, 40), a.GetBottomRight()) // right, bottom
	})
}

func TestBounds_Equals(t *testing.T) {
	t.Run("returns true if bounds equal", func(t *testing.T) {
		a, _, _ := boundsFixture()
		assert.True(t, a.Equals(BoundsOf(Pt(14, 12), Pt(30, 40))))
		assert.False(t, a.Equals(BoundsOf(Pt(14, 13), Pt(30, 40))))
		// Upstream's `equals(null)`: the empty bounds stands in for null.
		assert.False(t, a.Equals(Bounds{}))
	})
}
