// Port of Leaflet's spec/suites/geo/crs/CRSSpec.js at upstream commit
// c96f31a7a350a07cfbc852cf88e6ca69af5f5ec9. Each upstream `describe` is a
// Test function, each `it` a subtest named by its upstream title. The chai
// helpers of spec/setup.js — `near` (a Point within delta, default 1) and
// `nearLatLng` (a LatLng within delta, default 1e-4), both inclusive — are
// assertNearPt and assertNearLL below, which projection_test.go shares.
//
// Not ported from upstream:
//   - EPSG3857 › #wrapLatLng › "does not drop altitude": LatLng carries no
//     altitude (doc.go).
//
// Two its build a CRSI of their own — SimpleCRS › wrapLatLng › "wraps coords
// if configured" subclasses SimpleCRS with wrap ranges, and
// CRS.ZoomNotPowerOfTwo spreads CRSI with a 1.5-based scale — which the
// exported API cannot do yet (ADR-0204 §SD7), so they reach for the package's
// crs struct. The `CRSI` describe exercises the base class's scale and zoom,
// which EPSG3857 inherits unchanged and stands in for here.

package portolan

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertNearPt is spec/setup.js's `near`: both coordinates of got lie within
// delta of want, inclusive.
func assertNearPt(t *testing.T, want, got Point, delta float64) {
	t.Helper()
	assert.InDelta(t, want.X, got.X, delta, "x of %v, want %v", got, want)
	assert.InDelta(t, want.Y, got.Y, delta, "y of %v, want %v", got, want)
}

// assertNearLL is spec/setup.js's `nearLatLng`: both coordinates of got lie
// within delta of want, inclusive.
func assertNearLL(t *testing.T, want, got LatLng, delta float64) {
	t.Helper()
	assert.InDelta(t, want.Lat, got.Lat, delta, "lat of %v, want %v", got, want)
	assert.InDelta(t, want.Lng, got.Lng, delta, "lng of %v, want %v", got, want)
}

// describe('EPSG3857')

func TestEPSG3857_LatLngToPoint(t *testing.T) {
	c := EPSG3857

	t.Run("projects a center point", func(t *testing.T) {
		assertNearPt(t, Pt(128, 128), c.LatLngToPoint(LL(0, 0), 0), 0.01)
	})

	t.Run("projects the northeast corner of the world", func(t *testing.T) {
		assertNearPt(t, Pt(256, 0), c.LatLngToPoint(LL(85.0511287798, 180), 0), 1)
	})
}

func TestEPSG3857_PointToLatLng(t *testing.T) {
	c := EPSG3857

	t.Run("reprojects a center point", func(t *testing.T) {
		assertNearLL(t, LL(0, 0), c.PointToLatLng(Pt(128, 128), 0), 0.01)
	})

	t.Run("reprojects the northeast corner of the world", func(t *testing.T) {
		assertNearLL(t, LL(85.0511287798, 180), c.PointToLatLng(Pt(256, 0), 0), 1e-4)
	})
}

func TestEPSG3857_Project(t *testing.T) {
	c := EPSG3857

	t.Run("projects geo coords into meter coords correctly", func(t *testing.T) {
		assertNearPt(t, Pt(3339584.7238, 6446275.84102), c.Project(LL(50, 30)), 1)
		assertNearPt(t, Pt(20037508.34279, 20037508.34278), c.Project(LL(85.0511287798, 180)), 1)
		assertNearPt(t, Pt(-20037508.34279, -20037508.34278), c.Project(LL(-85.0511287798, -180)), 1)
	})
}

func TestEPSG3857_Unproject(t *testing.T) {
	c := EPSG3857

	t.Run("unprojects meter coords into geo coords correctly", func(t *testing.T) {
		assertNearLL(t, LL(50, 30), c.Unproject(Pt(3339584.7238, 6446275.84102)), 1e-4)
		assertNearLL(t, LL(85.051129, 180), c.Unproject(Pt(20037508.34279, 20037508.34278)), 1e-4)
		assertNearLL(t, LL(-85.051129, -180), c.Unproject(Pt(-20037508.34279, -20037508.34278)), 1e-4)
	})
}

func TestEPSG3857_GetProjectedBounds(t *testing.T) {
	c := EPSG3857

	t.Run("gives correct size", func(t *testing.T) {
		worldSize := 256.0
		for i := 0; i <= 22; i++ {
			b, ok := c.GetProjectedBounds(float64(i))
			require.True(t, ok, "zoom %d", i)
			crsSize := b.GetSize()
			assert.Equal(t, worldSize, crsSize.X, "zoom %d", i)
			assert.Equal(t, worldSize, crsSize.Y, "zoom %d", i)
			worldSize *= 2
		}
	})
}

func TestEPSG3857_WrapLatLng(t *testing.T) {
	c := EPSG3857

	t.Run("wraps longitude to lie between -180 and 180 by default", func(t *testing.T) {
		assert.Equal(t, -170.0, c.WrapLatLng(LL(0, 190)).Lng)
		assert.Equal(t, 0.0, c.WrapLatLng(LL(0, 360)).Lng)
		assert.Equal(t, 20.0, c.WrapLatLng(LL(0, 380)).Lng)
		assert.Equal(t, 170.0, c.WrapLatLng(LL(0, -190)).Lng)
		assert.Equal(t, 0.0, c.WrapLatLng(LL(0, -360)).Lng)
		assert.Equal(t, -20.0, c.WrapLatLng(LL(0, -380)).Lng)
		assert.Equal(t, 90.0, c.WrapLatLng(LL(0, 90)).Lng)
		assert.Equal(t, 180.0, c.WrapLatLng(LL(0, 180)).Lng)
	})
}

func TestEPSG3857_WrapLatLngBounds(t *testing.T) {
	c := EPSG3857

	t.Run("does not wrap bounds between -180 and 180 longitude", func(t *testing.T) {
		bounds1 := c.WrapLatLngBounds(LatLngBoundsOf(LL(-10, -10), LL(10, 10)))
		bounds2 := c.WrapLatLngBounds(LatLngBoundsOf(LL(-80, -180), LL(-70, -170)))
		bounds3 := c.WrapLatLngBounds(LatLngBoundsOf(LL(70, 170), LL(80, 180)))

		assert.Equal(t, -10.0, bounds1.GetSouth())
		assert.Equal(t, -10.0, bounds1.GetWest())
		assert.Equal(t, 10.0, bounds1.GetNorth())
		assert.Equal(t, 10.0, bounds1.GetEast())

		assert.Equal(t, -80.0, bounds2.GetSouth())
		assert.Equal(t, -180.0, bounds2.GetWest())
		assert.Equal(t, -70.0, bounds2.GetNorth())
		assert.Equal(t, -170.0, bounds2.GetEast())

		assert.Equal(t, 70.0, bounds3.GetSouth())
		assert.Equal(t, 170.0, bounds3.GetWest())
		assert.Equal(t, 80.0, bounds3.GetNorth())
		assert.Equal(t, 180.0, bounds3.GetEast())
	})

	t.Run("wraps bounds when center longitude is less than -180", func(t *testing.T) {
		bounds1 := c.WrapLatLngBounds(LatLngBoundsOf(LL(0, -185), LL(10, -170)))
		bounds2 := c.WrapLatLngBounds(LatLngBoundsOf(LL(0, -190), LL(10, -175)))

		assert.Equal(t, 0.0, bounds1.GetSouth())
		assert.Equal(t, -185.0, bounds1.GetWest())
		assert.Equal(t, 10.0, bounds1.GetNorth())
		assert.Equal(t, -170.0, bounds1.GetEast())

		assert.Equal(t, 0.0, bounds2.GetSouth())
		assert.Equal(t, 170.0, bounds2.GetWest())
		assert.Equal(t, 10.0, bounds2.GetNorth())
		assert.Equal(t, 185.0, bounds2.GetEast())
	})

	t.Run("wraps bounds when center longitude is larger than +180", func(t *testing.T) {
		bounds1 := c.WrapLatLngBounds(LatLngBoundsOf(LL(0, 185), LL(10, 170)))
		bounds2 := c.WrapLatLngBounds(LatLngBoundsOf(LL(0, 190), LL(10, 175)))

		assert.Equal(t, 0.0, bounds1.GetSouth())
		assert.Equal(t, 170.0, bounds1.GetWest())
		assert.Equal(t, 10.0, bounds1.GetNorth())
		assert.Equal(t, 185.0, bounds1.GetEast())

		assert.Equal(t, 0.0, bounds2.GetSouth())
		assert.Equal(t, -185.0, bounds2.GetWest())
		assert.Equal(t, 10.0, bounds2.GetNorth())
		assert.Equal(t, -170.0, bounds2.GetEast())
	})
}

// describe('EPSG4326')

func TestEPSG4326_GetSize(t *testing.T) {
	c := EPSG4326

	t.Run("gives correct size", func(t *testing.T) {
		worldSize := 256.0
		for i := 0; i <= 22; i++ {
			b, ok := c.GetProjectedBounds(float64(i))
			require.True(t, ok, "zoom %d", i)
			crsSize := b.GetSize()
			assert.Equal(t, worldSize*2, crsSize.X, "zoom %d", i)
			// Lat bounds are half as high (-90/+90 compared to -180/+180)
			assert.Equal(t, worldSize, crsSize.Y, "zoom %d", i)
			worldSize *= 2
		}
	})
}

// describe('EPSG3395')

func TestEPSG3395_LatLngToPoint(t *testing.T) {
	c := EPSG3395

	t.Run("projects a center point", func(t *testing.T) {
		assertNearPt(t, Pt(128, 128), c.LatLngToPoint(LL(0, 0), 0), 0.01)
	})

	t.Run("projects the northeast corner of the world", func(t *testing.T) {
		assertNearPt(t, Pt(256, 0), c.LatLngToPoint(LL(85.0840591556, 180), 0), 1)
	})
}

func TestEPSG3395_PointToLatLng(t *testing.T) {
	c := EPSG3395

	t.Run("reprojects a center point", func(t *testing.T) {
		assertNearLL(t, LL(0, 0), c.PointToLatLng(Pt(128, 128), 0), 0.01)
	})

	t.Run("reprojects the northeast corner of the world", func(t *testing.T) {
		assertNearLL(t, LL(85.0840591556, 180), c.PointToLatLng(Pt(256, 0), 0), 1e-4)
	})
}

// describe('SimpleCRS')

func TestSimple_LatLngToPoint(t *testing.T) {
	c := Simple

	t.Run("converts LatLng coords to pixels", func(t *testing.T) {
		assertNearPt(t, Pt(0, 0), c.LatLngToPoint(LL(0, 0), 0), 1)
		assertNearPt(t, Pt(300, -700), c.LatLngToPoint(LL(700, 300), 0), 1)
		assertNearPt(t, Pt(2000, 400), c.LatLngToPoint(LL(-200, 1000), 1), 1)
	})
}

func TestSimple_PointToLatLng(t *testing.T) {
	c := Simple

	t.Run("converts pixels to LatLng coords", func(t *testing.T) {
		assertNearLL(t, LL(0, 0), c.PointToLatLng(Pt(0, 0), 0), 1e-4)
		assertNearLL(t, LL(700, 300), c.PointToLatLng(Pt(300, -700), 0), 1e-4)
		assertNearLL(t, LL(-200, 1000), c.PointToLatLng(Pt(2000, 400), 1), 1e-4)
	})
}

func TestSimple_GetProjectedBounds(t *testing.T) {
	t.Run("returns nothing", func(t *testing.T) {
		_, ok := Simple.GetProjectedBounds(5)
		assert.False(t, ok)
	})
}

func TestSimple_WrapLatLng(t *testing.T) {
	t.Run("returns coords as is", func(t *testing.T) {
		assert.True(t, Simple.WrapLatLng(LL(270, 400)).Equals(LL(270, 400)))
	})

	t.Run("wraps coords if configured", func(t *testing.T) {
		// Upstream subclasses SimpleCRS with wrapLng and wrapLat of
		// [-200, 200]; this is Simple with the two ranges set.
		simple, ok := Simple.(*crs)
		require.True(t, ok, "Simple is the package's crs struct")
		wrapped := *simple
		wrapped.wrapLng = &[2]float64{-200, 200}
		wrapped.wrapLat = &[2]float64{-200, 200}
		assertNearLL(t, LL(-100, 150), wrapped.WrapLatLng(LL(300, -250)), 1e-4)
	})
}

// describe('CRSI')

func TestCRS_ZoomAndScale(t *testing.T) {
	c := EPSG3857

	t.Run("convert zoom to scale and vice-versa and return the same values", func(t *testing.T) {
		zoom := 2.5
		scale := c.Scale(zoom)
		zoom2 := c.Zoom(scale)
		assert.Equal(t, zoom, formatNum(zoom2, 6))
	})
}

// describe('CRS.ZoomNotPowerOfTwo')

// zoomNotPowerOfTwoCRS is upstream's `{...CRSI, scale, zoom}`: a CRSI whose
// scale grows by 1.5 per zoom level instead of doubling.
func zoomNotPowerOfTwoCRS() CRSI {
	return &crs{
		scale: func(zoom float64) float64 { return 256 * math.Pow(1.5, zoom) },
		zoom:  func(scale float64) float64 { return math.Log(scale/256) / math.Log(1.5) },
	}
}

func TestCRS_ZoomNotPowerOfTwo_Scale(t *testing.T) {
	c := zoomNotPowerOfTwoCRS()

	t.Run("of zoom levels are related by a power of 1.5", func(t *testing.T) {
		zoom := 5.0
		scale := c.Scale(zoom)
		assert.Equal(t, 1.5*scale, c.Scale(zoom+1))
		assert.Equal(t, zoom+1, c.Zoom(1.5*scale))
	})
}

func TestCRS_ZoomNotPowerOfTwo_ZoomAndScale(t *testing.T) {
	c := zoomNotPowerOfTwoCRS()

	t.Run("convert zoom to scale and vice-versa and return the same values", func(t *testing.T) {
		zoom := 2.0
		scale := c.Scale(zoom)
		assert.Equal(t, zoom, c.Zoom(scale))
	})
}

// describe('EarthCRS')

func TestEarthCRS_Distance(t *testing.T) {
	t.Run("computes great-circle distance between two points", func(t *testing.T) {
		// Test values from http://rosettacode.org/wiki/Haversine_formula, on
		// the mean Earth radius, as upstream notes.
		p1 := LL(36.12, -86.67)
		p2 := LL(33.94, -118.40)
		d := EarthDistance(p1, p2)
		assert.GreaterOrEqual(t, d, 2886444.43)
		assert.LessOrEqual(t, d, 2886444.45)
		// EarthCRS.distance is also what LatLng.distanceTo and every Earth
		// CRS's distance reach.
		assert.Equal(t, d, p1.DistanceTo(p2))
		assert.Equal(t, d, EPSG3857.Distance(p1, p2))
	})
}
