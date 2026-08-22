// Port of Leaflet's spec/suites/geo/LatLngSpec.js and
// spec/suites/geo/LatLngBoundsSpec.js at upstream commit
// c96f31a7a350a07cfbc852cf88e6ca69af5f5ec9. Each upstream `describe` is a
// Test function, each `it` a subtest named by its upstream title; the
// LatLngBounds `beforeEach` fixtures (a and c) are rebuilt at the top of each
// Test function.
//
// Not ported from upstream (LatLngSpec.js):
//   - constructor › "throws an error if invalid lat or lng": a JavaScript
//     throw on NaN input; a Go value type has no constructor to throw from.
//   - constructor › "does not set altitude if undefined" and "sets altitude":
//     LatLng carries no altitude (doc.go).
//   - validate › "returns true for valid inputs" and "returns false for
//     invalid inputs without throwing": LatLng.validate is the JavaScript
//     argument-shape check; Go has one LatLng form.
//   - #equals › "returns false if passed non-valid object": equals(null);
//     there is no null LatLng.
//   - LatLng creation › "returns LatLng instance as is", "accepts an array of
//     coordinates", "passes null or undefined as is", "accepts an object with
//     lat/lng", "accepts an object with lat/lon", "returns null if lng not
//     specified", "accepts altitude as third parameter" and "accepts an object
//     with alt": constructor polymorphism, throws and altitude.
//   - #clone › "should clone attributes" and "should create another
//     reference": LatLng is a value type — assignment copies and there is no
//     Clone; the upstream assertions also include altitude.
//
// Not ported from upstream (LatLngBoundsSpec.js):
//   - constructor › "returns an empty bounds when not argument is given to
//     factory": the L.latLngBounds factory form; the upstream body is a
//     duplicate of the preceding it, which is ported.
//   - #extend › "extends the bounds by undefined": extend(undefined); Go has
//     no undefined argument. The empty-bounds it beside it is ported.
//   - #extend › "extends the bounds by raw object": the {lat, lng} object
//     form.
//   - #contains › "returns true if contains latlng point as array" and
//     "returns true if contains latlng point as {lat:, lng:} object":
//     argument-shape variants of the LatLng-instance it, which is ported.

package portolan

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// LatLngSpec.js

func TestLatLng_Constructor(t *testing.T) {
	t.Run("sets lat and lng", func(t *testing.T) {
		a := LL(25, 74)
		assert.Equal(t, 25.0, a.Lat)
		assert.Equal(t, 74.0, a.Lng)

		b := LL(-25, -74)
		assert.Equal(t, -25.0, b.Lat)
		assert.Equal(t, -74.0, b.Lng)
	})
}

func TestLatLng_Equals(t *testing.T) {
	t.Run("returns true if compared objects are equal within a certain margin", func(t *testing.T) {
		a := LL(10, 20)
		b := LL(10+1.0e-10, 20-1.0e-10)
		assert.True(t, a.Equals(b))
	})

	t.Run("returns false if compared objects are not equal within a certain margin", func(t *testing.T) {
		a := LL(10, 20)
		b := LL(10, 23.3)
		assert.False(t, a.Equals(b))
	})
}

func TestLatLng_ToString(t *testing.T) {
	t.Run("formats a string", func(t *testing.T) {
		a := LL(10.333333333, 20.2222222)
		assert.Equal(t, "LatLng(10.333, 20.222)", a.Format(3))
		assert.Equal(t, "LatLng(10.333333, 20.222222)", a.String())
	})
}

func TestLatLng_DistanceTo(t *testing.T) {
	t.Run("calculates distance in meters", func(t *testing.T) {
		a := LL(50.5, 30.5)
		b := LL(50, 1)
		assert.True(t, math.Abs(jsRound(a.DistanceTo(b)/1000)-2084) < 5)
	})

	t.Run("does not return NaN if input points are equal", func(t *testing.T) {
		a := LL(50.5, 30.5)
		b := LL(50.5, 30.5)
		assert.Equal(t, 0.0, a.DistanceTo(b))
	})
}

func TestLatLng_Creation(t *testing.T) {
	t.Run("creates a LatLng object from two coordinates", func(t *testing.T) {
		assert.Equal(t, LatLng{Lat: 50, Lng: 30}, LL(50, 30))
	})
}

// LatLngBoundsSpec.js

func TestLatLngBounds_Constructor(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))

	t.Run("instantiates either passing two latlngs or an array of latlngs", func(t *testing.T) {
		b := NewLatLngBounds(LL(14, 12), LL(30, 40))
		assert.Equal(t, a, b)
		assert.Equal(t, LL(30, 12), b.GetNorthWest())
	})

	t.Run("returns an empty bounds when not argument is given", func(t *testing.T) {
		bounds := NewLatLngBounds()
		assert.False(t, bounds.IsValid())
		assert.Equal(t, LatLngBounds{}, bounds)
	})
}

func TestLatLngBounds_Extend(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))

	t.Run("extends the bounds by a given point", func(t *testing.T) {
		assert.Equal(t, LL(30, 50), a.Extend(LL(20, 50)).GetNorthEast())
	})

	t.Run("extends the bounds by given bounds", func(t *testing.T) {
		assert.Equal(t, LL(8, 50), a.ExtendBounds(LatLngBoundsOf(LL(20, 50), LL(8, 40))).GetSouthEast())
	})

	t.Run("extend the bounds by an empty bounds object", func(t *testing.T) {
		assert.Equal(t, a, a.ExtendBounds(LatLngBounds{}))
		assert.Equal(t, a, a.ExtendBounds(NewLatLngBounds()))
	})
}

func TestLatLngBounds_GetCenter(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))

	t.Run("returns the bounds center", func(t *testing.T) {
		assert.Equal(t, LL(22, 26), a.GetCenter())
	})
}

func TestLatLngBounds_Pad(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))

	t.Run("pads the bounds by a given ratio", func(t *testing.T) {
		assert.Equal(t, LatLngBoundsOf(LL(6, -2), LL(38, 54)), a.Pad(0.5))
	})
}

func TestLatLngBounds_Equals(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))

	t.Run("returns true if bounds equal", func(t *testing.T) {
		assert.True(t, a.Equals(LatLngBoundsOf(LL(14, 12), LL(30, 40))))
		assert.False(t, a.Equals(LatLngBoundsOf(LL(14, 13), LL(30, 40))))
		// Upstream compares against null; the empty bounds is the nearest
		// thing Go has, and it is never equal to anything.
		assert.False(t, a.Equals(LatLngBounds{}))
	})

	t.Run("returns true if compared objects are equal within a certain margin", func(t *testing.T) {
		assert.True(t, a.EqualsWithin(LatLngBoundsOf(LL(15, 11), LL(29, 41)), 1))
	})

	t.Run("returns false if compared objects are not equal within a certain margin", func(t *testing.T) {
		assert.False(t, a.EqualsWithin(LatLngBoundsOf(LL(15, 11), LL(29, 41)), 0.5))
	})
}

func TestLatLngBounds_IsValid(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))
	c := LatLngBounds{}

	t.Run("returns true if properly set up", func(t *testing.T) {
		assert.True(t, a.IsValid())
	})

	t.Run("returns false if is invalid", func(t *testing.T) {
		assert.False(t, c.IsValid())
	})

	t.Run("returns true if extended", func(t *testing.T) {
		assert.True(t, c.Extend(LL(0, 0)).IsValid())
	})
}

func TestLatLngBounds_GetWest(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))

	t.Run("returns a proper bbox west value", func(t *testing.T) {
		assert.Equal(t, 12.0, a.GetWest())
	})
}

func TestLatLngBounds_GetSouth(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))

	t.Run("returns a proper bbox south value", func(t *testing.T) {
		assert.Equal(t, 14.0, a.GetSouth())
	})
}

func TestLatLngBounds_GetEast(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))

	t.Run("returns a proper bbox east value", func(t *testing.T) {
		assert.Equal(t, 40.0, a.GetEast())
	})
}

func TestLatLngBounds_GetNorth(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))

	t.Run("returns a proper bbox north value", func(t *testing.T) {
		assert.Equal(t, 30.0, a.GetNorth())
	})
}

func TestLatLngBounds_ToBBoxString(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))

	t.Run("returns a proper left,bottom,right,top bbox", func(t *testing.T) {
		assert.Equal(t, "12,14,40,30", a.ToBBoxString())
	})
}

func TestLatLngBounds_GetNorthWest(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))

	t.Run("returns a proper north-west LatLng", func(t *testing.T) {
		assert.Equal(t, LL(a.GetNorth(), a.GetWest()), a.GetNorthWest())
	})
}

func TestLatLngBounds_GetSouthEast(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))

	t.Run("returns a proper south-east LatLng", func(t *testing.T) {
		assert.Equal(t, LL(a.GetSouth(), a.GetEast()), a.GetSouthEast())
	})
}

func TestLatLngBounds_Contains(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))

	t.Run("returns true if contains latlng point as LatLng instance", func(t *testing.T) {
		assert.True(t, a.Contains(LL(16, 20)))
		assert.False(t, a.Contains(LL(5, 20)))
	})

	t.Run("returns true if contains bounds", func(t *testing.T) {
		assert.True(t, a.ContainsBounds(LatLngBoundsOf(LL(16, 20), LL(20, 40))))
		assert.False(t, a.ContainsBounds(LatLngBoundsOf(LL(16, 50), LL(8, 40))))
	})
}

func TestLatLngBounds_Intersects(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))

	t.Run("returns true if intersects the given bounds", func(t *testing.T) {
		assert.True(t, a.Intersects(LatLngBoundsOf(LL(16, 20), LL(50, 60))))
		// Upstream asserts contains, not intersects, for the second bounds;
		// kept as written.
		assert.False(t, a.ContainsBounds(LatLngBoundsOf(LL(40, 50), LL(50, 60))))
	})

	t.Run("returns true if just touches the boundary of the given bounds", func(t *testing.T) {
		assert.True(t, a.Intersects(LatLngBoundsOf(LL(25, 40), LL(55, 50))))
	})
}

func TestLatLngBounds_Overlaps(t *testing.T) {
	a := LatLngBoundsOf(LL(14, 12), LL(30, 40))

	t.Run("returns true if overlaps the given bounds", func(t *testing.T) {
		assert.True(t, a.Overlaps(LatLngBoundsOf(LL(16, 20), LL(50, 60))))
	})

	t.Run("returns false if just touches the boundary of the given bounds", func(t *testing.T) {
		assert.False(t, a.Overlaps(LatLngBoundsOf(LL(25, 40), LL(55, 50))))
	})
}
