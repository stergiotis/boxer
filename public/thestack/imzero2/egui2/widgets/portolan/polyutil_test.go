// Ports spec/suites/geometry/PolyUtilSpec.js from Leaflet at
// c96f31a7a350a07cfbc852cf88e6ca69af5f5ec9. Top-level functions follow the
// upstream `describe` groups, subtests carry the upstream `it` titles.
//
// Not ported from upstream (JavaScript-specific, no Go analogue):
//
//	"#polygonCenter" › "shows warning if latlngs is not flat" — a nested
//	  ring argument and a console.warn spy.
//	"#polygonCenter" › "iterates only over the array values" —
//	  Array.prototype pollution.
//
// "throws error if latlngs not passed" and "throws error if latlng array is
// empty" are ported as the error return the Go signature has in place of the
// throw. "computes center of a small polygon" and "computes center of a big
// polygon" go through Polygon.getCenter upstream, which is polygonCenter over
// the map's CRSI — EPSG3857 — on a ring whose last point differs from its
// first, so it is called directly here.

package portolan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPolyUtil_ClipPolygon(t *testing.T) {
	t.Run("clips polygon by bounds", func(t *testing.T) {
		bounds := BoundsOf(Pt(0, 0), Pt(10, 10))

		points := []Point{
			Pt(5, 5),
			Pt(15, 10),
			Pt(10, 15),
		}

		// check clip without rounding
		clipped := ClipPolygon(points, bounds, false)

		assert.Equal(t, []Point{
			Pt(7.5, 10),
			Pt(5, 5),
			Pt(10, 7.5),
			Pt(10, 10),
		}, clipped)

		// check clip with rounding
		clippedRounded := ClipPolygon(points, bounds, true)

		assert.Equal(t, []Point{
			Pt(8, 10),
			Pt(5, 5),
			Pt(10, 8),
			Pt(10, 10),
		}, clippedRounded)
	})
}

func TestPolyUtil_PolygonCenter(t *testing.T) {
	// beforeEach: the map's CRSI, which is the default.
	crs := EPSG3857

	// More tests in PolygonSpec

	t.Run("computes center of polygon", func(t *testing.T) {
		latlngs := []LatLng{LL(0, 0), LL(10, 0), LL(10, 10), LL(0, 10)}
		center, err := PolygonCenter(latlngs, crs)
		require.NoError(t, err)
		assertNearLatLng(t, LL(5.019148099025293, 5), center)
	})

	t.Run("computes center of a small polygon", func(t *testing.T) {
		latlngs := []LatLng{
			LL(42.87097909758862, -81.12594320566181),
			LL(42.87108302016597, -81.12594320566181),
			LL(42.87108302016597, -81.12576504805303),
			LL(42.87097909758862, -81.12576504805303),
		}
		center, err := PolygonCenter(latlngs, crs)
		require.NoError(t, err)
		assertNearLatLng(t, LL(42.87103105887729, -81.12585412685742), center)
	})

	t.Run("computes center of a big polygon", func(t *testing.T) {
		latlngs := []LatLng{LL(90, -180), LL(90, 180), LL(-90, 180), LL(-90, -180)}
		center, err := PolygonCenter(latlngs, crs)
		require.NoError(t, err)
		assertNearLatLng(t, LL(0, 0), center)
	})

	t.Run("throws error if latlngs not passed", func(t *testing.T) {
		_, err := PolygonCenter(nil, crs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "latlngs not passed")
	})

	t.Run("throws error if latlng array is empty", func(t *testing.T) {
		_, err := PolygonCenter([]LatLng{}, crs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "latlngs not passed")
	})
}
