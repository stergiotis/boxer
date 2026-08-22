// Ports spec/suites/geometry/LineUtilSpec.js from Leaflet at
// c96f31a7a350a07cfbc852cf88e6ca69af5f5ec9. Top-level functions follow the
// upstream `describe` groups, subtests carry the upstream `it` titles.
//
// Not ported from upstream (JavaScript-specific, no Go analogue):
//
//	"#isFlat" › "should return true for an array of LatLngs",
//	  "should return true for an array of LatLngs arrays",
//	  "should return true for an empty array",
//	  "should return false for a nested array of LatLngs",
//	  "should return false for a nested empty array" — isFlat tells a
//	  LatLng[] from a LatLng[][] at run time; []LatLng settles that in the
//	  type system and the function has no port.
//	"#polylineCenter" › "shows warning if latlngs is not flat" — a nested
//	  ring argument and a console.warn spy.
//	"#polylineCenter" › "iterates only over the array values" —
//	  Array.prototype pollution.
//
// "#polylineCenter" › "throws error if latlngs not passed" and "throws error
// if latlng array is empty" each appear twice upstream; both are ported once,
// as the error return the Go signature has in place of the throw.
// "computes center of a small line" goes through Polyline.getCenter upstream,
// which is polylineCenter over the map's CRSI — EPSG3857 — so it is called
// directly here.

package portolan

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertNearLatLng is the spec suite's nearLatLng matcher: each coordinate
// within 1e-4 degrees of the expected one.
func assertNearLatLng(t *testing.T, expected, actual LatLng) {
	t.Helper()
	assert.InDelta(t, expected.Lat, actual.Lat, 1e-4, "lat")
	assert.InDelta(t, expected.Lng, actual.Lng, 1e-4, "lng")
}

func TestLineUtil_ClipSegment(t *testing.T) {
	// beforeEach
	bounds := BoundsOf(Pt(5, 0), Pt(15, 10))

	t.Run("clips a segment by bounds", func(t *testing.T) {
		a := Pt(0, 0)
		b := Pt(15, 15)

		ca, cb, ok := ClipSegment(a, b, bounds, false)
		require.True(t, ok)
		assert.Equal(t, Pt(5, 5), ca)
		assert.Equal(t, Pt(10, 10), cb)

		c := Pt(5, -5)
		d := Pt(20, 10)

		cc, cd, ok := ClipSegment(c, d, bounds, false)
		require.True(t, ok)
		assert.Equal(t, Pt(10, 0), cc)
		assert.Equal(t, Pt(15, 5), cd)
	})

	t.Run("uses last bit code and reject segments out of bounds", func(t *testing.T) {
		// Upstream reads the module-level code left by the previous case's
		// last call, clipSegment((5, -5), (20, 10)); the clipper carries it
		// explicitly, so that call is replayed first.
		var clipper SegmentClipper
		_, _, ok := clipper.Clip(Pt(5, -5), Pt(20, 10), bounds, false, false)
		require.True(t, ok)

		a := Pt(15, 15)
		b := Pt(25, 20)
		_, _, ok = clipper.Clip(a, b, bounds, true, false)
		assert.False(t, ok)
	})

	t.Run("can round numbers in clipped bounds", func(t *testing.T) {
		a := Pt(4, 5)
		b := Pt(8, 6)

		ca, cb, ok := ClipSegment(a, b, bounds, false)
		require.True(t, ok)
		assert.Equal(t, Pt(5, 5.25), ca)
		assert.Equal(t, b, cb)

		ca, cb, ok = ClipSegment(a, b, bounds, true)
		require.True(t, ok)
		assert.Equal(t, Pt(5, 5), ca)
		assert.Equal(t, b, cb)
	})
}

func TestLineUtil_PointToSegmentDistanceAndClosestPointOnSegment(t *testing.T) {
	p1 := Pt(0, 10)
	p2 := Pt(10, 0)
	p := Pt(0, 0)

	t.Run("calculates distance from new Point to segment", func(t *testing.T) {
		assert.Equal(t, math.Sqrt(200)/2, PointToSegmentDistance(p, p1, p2))
	})

	t.Run("calculates new Point closest to segment", func(t *testing.T) {
		assert.Equal(t, Pt(5, 5), ClosestPointOnSegment(p, p1, p2))
	})
}

func TestLineUtil_Simplify(t *testing.T) {
	t.Run("simplifies polylines according to tolerance", func(t *testing.T) {
		points := []Point{
			Pt(0, 0),
			Pt(0.01, 0),
			Pt(0.5, 0.01),
			Pt(0.7, 0),
			Pt(1, 0),
			Pt(1.999, 0.999),
			Pt(2, 1),
		}

		simplified := Simplify(points, 0.1)

		assert.Equal(t, []Point{
			Pt(0, 0),
			Pt(1, 0),
			Pt(2, 1),
		}, simplified)
	})
}

func TestLineUtil_PolylineCenter(t *testing.T) {
	// beforeEach: the map's CRSI, which is the default.
	crs := EPSG3857

	// More tests in PolylineSpec

	t.Run("computes center of line", func(t *testing.T) {
		latlngs := []LatLng{LL(80, 0), LL(80, 90)}
		center, err := PolylineCenter(latlngs, crs)
		require.NoError(t, err)
		assertNearLatLng(t, LL(80, 45), center)
	})

	t.Run("computes center of a small line", func(t *testing.T) {
		latlngs := []LatLng{
			LL(50.49898323576035, 30.509834789772036),
			LL(50.49998323576035, 30.509834789772036),
			LL(50.49998323576035, 30.509939789772037),
			LL(50.49898323576035, 30.509939789772037),
		}
		center, err := PolylineCenter(latlngs, crs)
		require.NoError(t, err)
		assertNearLatLng(t, LL(50.49998323576035, 30.50989603626345), center)
	})

	t.Run("throws error if latlngs not passed", func(t *testing.T) {
		_, err := PolylineCenter(nil, crs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "latlngs not passed")
	})

	t.Run("throws error if latlng array is empty", func(t *testing.T) {
		_, err := PolylineCenter([]LatLng{}, crs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "latlngs not passed")
	})
}
