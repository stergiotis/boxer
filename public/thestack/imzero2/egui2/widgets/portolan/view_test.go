// Port of Leaflet's spec/suites/map/MapSpec.js at upstream commit
// c96f31a7a350a07cfbc852cf88e6ca69af5f5ec9 — the `it`s whose subject is the
// view state of src/map/Map.js or its conversions, run against View. Each
// upstream `describe` is a Test function, each `it` a subtest named by its
// upstream title.
//
// Conventions of the port:
//   - The spec's `new LeafletMap(container)` is specView(): SpecHelper's
//     400×400 container and Leaflet's default zoomSnap of 1 (this package's
//     default is 0). Describes that size their container differently say so.
//   - `setView(center)` with no zoom keeps the current zoom — PanTo here. On
//     an unloaded view that is zoom 0, where upstream carries an undefined
//     zoom the spec never inspects.
//   - A `zoom` map option is an unloaded SetZoom, which records the zoom
//     without loading, as upstream's constructor does.
//   - `once('zoomend', …)` / `once('moveend', …)` become assertions on the
//     TakeEvents flags the callback would have run on. ViewEvents is a set,
//     so where the spec sequences several events in one call only the flags
//     are asserted.
//   - `throws` expectations have no Go form — there are no exceptions, value
//     types cannot be undefined, and an unloaded View projects from its zero
//     state rather than throwing — and are listed below.
//
// Not ported from upstream:
//   - Map › "throws …" / "does not throw …" guards, per the last convention:
//     #getCenter "throws if not set before"; #setZoomAround "throws if map is
//     not loaded", "throws if zoom is empty", "throws if zoom is undefined",
//     "throws if latLng is undefined"; #getPixelBounds and #getPixelOrigin
//     "throw error if center and zoom were not set / map not loaded";
//     #distance "throw with undefined values", "throw with infinity values",
//     "throw with only 1 lat"; #containerPointToLatLng,
//     #latLngToContainerPoint, #panTo, #panInsideBounds ("throws if map is
//     not set before", "throws if passed invalid bounds"),
//     #latLngToLayerPoint, #layerPointToLatLng, #panBy "throws if map is not
//     set before".
//   - #setZoom "can be passed without a zoom specified and keep previous
//     zoom", "can be passed with a zoom level of undefined and keep previous
//     zoom": SetZoom takes a float64, there is no unspecified zoom.
//   - #setZoom › when the map has been loaded › "does not overwrite zoom
//     passed as map option": its expectation (the zoom stays 13 after
//     setZoom(15)) holds only because the animated zoom is deferred to an
//     animation frame the spec never waits for; without animation the zoom
//     is 15.
//   - #setView "passes duration option to panBy"; #stop; #flyTo;
//     #flyToBounds: animation.
//   - #setMinZoom and #setMaxZoom "reset min/max zoom if set to undefined or
//     missing param": the Go API has no unset; the unset state is the zero
//     ViewOptions, covered by #getMinZoom "returns 0 if not set…".
//   - #setMaxBounds "does not try to remove listeners if it wasn't set
//     before": listener bookkeeping; there is no listener list here.
//   - #invalidateSize "emits no move event if the size has not changed",
//     "emits a move event if the size has changed", "emits a moveend event if
//     the size has changed", "debounces the moveend event if the
//     debounceMoveend option is given", "auto invalidateSize after container
//     resize", "disables auto invalidateSize", "makes sure that auto
//     invalidateSize is removed": events, timers and the ResizeObserver;
//     SetSize records no events.
//   - #getSize "return previous size on empty map": #remove.
//   - _addZoomLimit "… in two layers that are added to map" (×2), "… NaN …"
//     (×4): the per-layer aggregation of Layer.js's _updateZoomLevels and its
//     NaN quirks belong to the tile source; View takes the aggregate through
//     SetLayerZoomLimits. The two single-layer cases are ported.
//   - #remove, #addHandler, createPane, #getPane, #getPanes, #getContainer,
//     #hasLayer, #addLayer, #removeLayer, #eachLayer, #DOM events,
//     #Geolocation, #locate, #stopLocate, #disableClickPropagation,
//     #pointerEventToLatLng, #pointerEventToContainerPoint,
//     #pointerEventToLayerPoint: the DOM, panes, layers, handlers and
//     geolocation — none of which View has. The zoom-limit consequences of
//     layers are ported through SetLayerZoomLimits (#getMinZoom and
//     #getMaxZoom, #fitBounds after layers set, #getZoom).

package portolan

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// specView is the spec's `new LeafletMap(container)`: SpecHelper's default
// 400×400 container and Leaflet's default zoomSnap of 1.
func specView() *View { return specViewSized(400, 400) }

// specViewSized is specView with the container styled to w×h.
func specViewSized(w, h float64) *View {
	return NewView(ViewOptions{Size: Pt(w, h), ZoomSnap: 1})
}

// specViewOpts is specView with map options; a zero Size is the 400×400
// container and a zero ZoomSnap is Leaflet's 1 (build a zoomSnap: 0 view with
// NewView directly).
func specViewOpts(opts ViewOptions) *View {
	if opts.Size == (Point{}) {
		opts.Size = Pt(400, 400)
	}
	if opts.ZoomSnap == 0 {
		opts.ZoomSnap = 1
	}
	return NewView(opts)
}

// assertWithin is chai's `within`: lo ≤ got ≤ hi, inclusive.
func assertWithin(t *testing.T, lo, hi, got float64) {
	t.Helper()
	assert.True(t, got >= lo && got <= hi, "got %v, want within [%v, %v]", got, lo, hi)
}

// stubCRS is the spec's sinon stub or mock of map.options.crs: an EPSG3857
// whose LatLngToPoint and Zoom a test can replace; a replacement that returns
// ok=false falls through to the real one.
type stubCRS struct {
	CRSI
	latLngToPoint func(LatLng, float64) (Point, bool)
	zoom          func(float64) (float64, bool)
}

func (s *stubCRS) LatLngToPoint(ll LatLng, zoom float64) Point {
	if s.latLngToPoint != nil {
		if p, ok := s.latLngToPoint(ll, zoom); ok {
			return p
		}
	}
	return s.CRSI.LatLngToPoint(ll, zoom)
}

func (s *stubCRS) Zoom(scale float64) float64 {
	if s.zoom != nil {
		if z, ok := s.zoom(scale); ok {
			return z
		}
	}
	return s.CRSI.Zoom(scale)
}

// describe('#getCenter')

func TestMap_GetCenter(t *testing.T) {
	t.Run("returns a precise center when zoomed in after being set (#426)", func(t *testing.T) {
		v := specView()
		center := LL(10, 10)
		v.SetView(center, 1)
		v.SetZoom(19)
		assert.Equal(t, center, v.Center())
	})

	t.Run("returns correct center after invalidateSize (#1919)", func(t *testing.T) {
		// Upstream asserts the centre is *not* the one set: invalidateSize
		// drops the cached centre and getCenter recomputes it from the pane,
		// to whole-pixel precision. The centre is the state here and a
		// resize keeps it, so the port asserts that.
		v := specView()
		center := LL(10, 10)
		v.SetView(center, 1)
		v.SetSize(Pt(300, 400))
		assert.Equal(t, center, v.Center())
	})

	t.Run("returns a new object that can be mutated without affecting the map", func(t *testing.T) {
		v := specView()
		v.SetView(LL(10, 10), 1)
		center := v.Center()
		center.Lat += 10
		assert.Equal(t, LL(10, 10), v.Center())
	})
}

// describe('#whenReady') — reduced to Loaded and the load event.

func TestMap_WhenReady(t *testing.T) {
	t.Run("when the map has not yet been loaded", func(t *testing.T) {
		t.Run("calls the callback when the map is loaded", func(t *testing.T) {
			v := specView()
			assert.False(t, v.Loaded())
			assert.False(t, v.TakeEvents().Load)

			v.SetView(LL(0, 0), 1)
			assert.True(t, v.Loaded())
			assert.True(t, v.TakeEvents().Load)
		})
	})

	t.Run("when the map has already been loaded", func(t *testing.T) {
		t.Run("calls the callback immediately", func(t *testing.T) {
			v := specView()
			v.SetView(LL(0, 0), 1)
			assert.True(t, v.Loaded())
		})
	})
}

// describe('#setView')

func TestMap_SetView(t *testing.T) {
	t.Run("sets the view of the map", func(t *testing.T) {
		v := specView()
		v.SetView(LL(51.505, -0.09), 13)
		assert.Equal(t, 13.0, v.Zoom())
		assert.Less(t, v.Center().DistanceTo(LL(51.505, -0.09)), 5.0)
		// The events of a first, zoom-changing hard view change; the other
		// describes observe them one at a time through once('zoomend') and
		// friends.
		assert.Equal(t, ViewEvents{
			MoveStart: true, Move: true, MoveEnd: true,
			ZoomStart: true, Zoom: true, ZoomEnd: true,
			ViewReset: true, Load: true,
		}, v.TakeEvents())
	})

	t.Run("can be passed without a zoom specified", func(t *testing.T) {
		v := specView()
		v.SetZoom(13)
		v.PanTo(LL(51.605, -0.11))
		assert.Equal(t, 13.0, v.Zoom())
		assert.Less(t, v.Center().DistanceTo(LL(51.605, -0.11)), 5.0)
	})

	t.Run("limits initial zoom when no zoom specified", func(t *testing.T) {
		v := specViewOpts(ViewOptions{MaxZoom: 20, HasMaxZoom: true})
		v.SetZoom(100)
		v.PanTo(LL(51.605, -0.11))
		assert.Equal(t, 20.0, v.Zoom())
		assert.Less(t, v.Center().DistanceTo(LL(51.605, -0.11)), 5.0)
	})

	t.Run("defaults to zoom passed as map option", func(t *testing.T) {
		v := NewView(ViewOptions{ZoomSnap: 1}) // a detached div: 0×0
		v.SetZoom(13)
		v.PanTo(LL(51.605, -0.11))
		assert.Equal(t, 13.0, v.Zoom())
	})

	t.Run("prevents firing movestart noMoveStart", func(t *testing.T) {
		// SetView has no noMoveStart option; the flag belongs to resetView,
		// which is what setView reaches with {pan: {noMoveStart: true}}.
		v := specView()
		v.resetView(LL(51.505, -0.09), 13, true)
		e := v.TakeEvents()
		assert.True(t, e.MoveEnd)
		assert.False(t, e.MoveStart)
	})
}

// describe('#setZoom')

func TestMap_SetZoom(t *testing.T) {
	t.Run("when the map has not yet been loaded", func(t *testing.T) {
		t.Run("set zoom level is not limited by max zoom", func(t *testing.T) {
			v := specViewOpts(ViewOptions{MaxZoom: 10, HasMaxZoom: true})
			v.SetZoom(15)
			assert.Equal(t, 15.0, v.Zoom())
		})

		t.Run("overwrites zoom passed as map option", func(t *testing.T) {
			v := NewView(ViewOptions{ZoomSnap: 1})
			v.SetZoom(13)
			v.SetZoom(15)
			assert.Equal(t, 15.0, v.Zoom())
		})
	})

	t.Run("when the map has been loaded", func(t *testing.T) {
		t.Run("set zoom level is limited by max zoom", func(t *testing.T) {
			v := specView()
			v.SetView(LL(0, 0), 0) // loads map
			v.SetMaxZoom(10)
			v.SetZoom(15)
			assert.Equal(t, 10.0, v.Zoom())
		})
	})

	t.Run("changes previous zoom level", func(t *testing.T) {
		// Upstream's `map.zoom = 10` is a stray property; the port gives the
		// view a previous zoom the honest way.
		v := specView()
		v.SetZoom(10)
		v.SetZoom(15)
		assert.Equal(t, 15.0, v.Zoom())
	})

	t.Run("can be passed with a zoom level of infinity", func(t *testing.T) {
		v := specView()
		v.SetZoom(math.Inf(1))
		assert.Equal(t, math.Inf(1), v.Zoom())
	})
}

// describe('#setZoomAround')

func TestMap_SetZoomAround(t *testing.T) {
	loaded := func() *View {
		v := specView()
		v.SetView(LL(0, 0), 0) // loads map
		return v
	}

	t.Run("pass Point and change pixel in view", func(t *testing.T) {
		v := loaded()
		point := Pt(5, 5)
		v.SetZoomAround(point, 5)
		assert.False(t, v.Bounds().Contains(v.CRS().PointToLatLng(point, 5)))
	})

	t.Run("pass Point and change pixel in view at high zoom", func(t *testing.T) {
		v := loaded()
		point := Pt(5, 5)
		v.SetZoomAround(point, 18)
		assert.False(t, v.Bounds().Contains(v.CRS().PointToLatLng(point, 18)))
	})

	t.Run("pass latLng and keep specified latLng in view", func(t *testing.T) {
		v := loaded()
		v.SetZoomAroundLatLng(LL(5, 5), 5)
		assert.True(t, v.Bounds().Contains(LL(5, 5)))
	})

	t.Run("pass latLng and keep specified latLng in view at high zoom fails", func(t *testing.T) {
		v := loaded()
		v.SetZoomAroundLatLng(LL(5, 5), 12) // usually fails around 9 zoom level
		assert.False(t, v.Bounds().Contains(LL(5, 5)))
	})

	t.Run("does not throw if latLng is infinity", func(t *testing.T) {
		v := loaded()
		v.PanTo(LL(5, 5))
		assert.NotPanics(t, func() { v.SetZoomAroundLatLng(LL(math.Inf(1), math.Inf(1)), 4) })
		assert.True(t, v.Loaded())
	})
}

// describe('#getBounds')

func TestMap_GetBounds(t *testing.T) {
	t.Run("is safe to call from within a moveend callback during initial load (#1027)", func(t *testing.T) {
		// No callbacks here: MoveTo sets the origin before MoveEnd is
		// recorded, so Bounds is computable once the first SetView returns.
		v := NewView(ViewOptions{ZoomSnap: 1}) // a detached div: 0×0
		v.SetView(LL(51.505, -0.09), 13)
		assert.True(t, v.TakeEvents().MoveEnd)
		assert.True(t, v.Bounds().IsValid())
	})
}

// describe('#getBoundsZoom')

func TestMap_GetBoundsZoom(t *testing.T) {
	const halfLength = 0.00025
	bounds := LatLngBoundsOf(LL(-halfLength, -halfLength), LL(halfLength, halfLength))
	wideBounds := LatLngBoundsOf(LL(-halfLength, -halfLength*10), LL(halfLength, halfLength*10))
	padding := Pt(100, 100)

	t.Run("returns high levels of zoom with small areas and big padding", func(t *testing.T) {
		v := specView()
		assert.Equal(t, 19.0, v.BoundsZoom(bounds, false, padding))
	})

	t.Run("returns multiples of zoomSnap when zoomSnap > 0", func(t *testing.T) {
		// zoomSnap is a construction option here: one view per setting.
		v := NewView(ViewOptions{Size: Pt(400, 400), ZoomSnap: 0.5})
		assert.Equal(t, 19.5, v.BoundsZoom(bounds, false, padding))
		v = NewView(ViewOptions{Size: Pt(400, 400), ZoomSnap: 0.2})
		assert.Equal(t, 19.6, v.BoundsZoom(bounds, false, padding))
		v = NewView(ViewOptions{Size: Pt(400, 400), ZoomSnap: 0})
		assertWithin(t, 19.6864560, 19.6864561, v.BoundsZoom(bounds, false, padding))
	})

	t.Run("getBoundsZoom does not return Infinity when projected SE - NW has negative components", func(t *testing.T) {
		// The container loses its width and height: an absolutely
		// positioned, empty div measures 0×0.
		bounds := LatLngBoundsOf(
			LL(62.18475569507688, 6.926335173954951),
			LL(62.140483526511694, 6.923933370740089))
		padding := Pt(-50, -50)

		// control case: default crs
		v := NewView(ViewOptions{Size: Pt(0, 0), ZoomSnap: 1})
		v.SetZoom(16)
		assert.Equal(t, 9.0, v.BoundsZoom(bounds, false, padding))

		// test case: EPSG:25833 (mocked, for simplicity)
		// The following coordinates are bounds projected with proj4leaflet
		// crs = EPSG:25833', '+proj=utm +zone=33 +ellps=GRS80 +units=m +no_defs
		nwCalls, seCalls := 0, 0
		mock := &stubCRS{CRSI: EPSG3857, latLngToPoint: func(ll LatLng, zoom float64) (Point, bool) {
			switch {
			case zoom == 16 && ll == bounds.GetNorthWest():
				nwCalls++
				return Pt(7800503.059925064, 6440062.353052008), true
			case zoom == 16 && ll == bounds.GetSouthEast():
				seCalls++
				return Pt(7801987.203481699, 6425186.447901004), true
			}
			return Point{}, false
		}}
		v = NewView(ViewOptions{CRS: mock, Size: Pt(0, 0), ZoomSnap: 1})
		v.SetZoom(16)
		boundsZoom := v.BoundsZoom(bounds, false, padding)
		// ensure that latLngToPoint was called with expected args
		assert.Equal(t, 1, nwCalls)
		assert.Equal(t, 1, seCalls)
		assert.Equal(t, 7.0, boundsZoom) // result expected for EPSG:25833
	})

	t.Run("respects the 'inside' parameter", func(t *testing.T) {
		v := specViewSized(1024, 400) // Make sure the width is defined
		assert.Equal(t, 17.0, v.BoundsZoom(wideBounds, false, padding))
		assert.Equal(t, 20.0, v.BoundsZoom(wideBounds, true, padding))
	})
}

// describe('#setMaxBounds')

func TestMap_SetMaxBounds(t *testing.T) {
	t.Run("aligns pixel-wise map view center with maxBounds center if it cannot move view bounds inside maxBounds (#1908)", func(t *testing.T) {
		// large view, cannot fit within maxBounds
		v := specViewSized(1000, 1000)
		// maxBounds
		bounds := LatLngBoundsOf(LL(51.5, -0.05), LL(51.55, 0.05))
		v.SetMaxBounds(bounds)
		// set view outside
		v.SetView(LL(53.0, 0.15), 12)
		// get center of bounds in pixels
		boundsCenter := v.Project(bounds.GetCenter()).Round()
		assert.Equal(t, boundsCenter, v.Project(v.Center()).Round())
	})

	t.Run("moves map view within maxBounds by changing one coordinate", func(t *testing.T) {
		// small view, can fit within maxBounds
		v := specViewSized(200, 200)
		// maxBounds
		bounds := LatLngBoundsOf(LL(51, -0.2), LL(52, 0.2))
		v.SetMaxBounds(bounds)
		// set view outside maxBounds on one direction only
		// leaves untouched the other coordinate (that is not already centered)
		initCenter := LL(53.0, 0.1)
		v.SetView(initCenter, 16)
		// one pixel coordinate hasn't changed, the other has
		pixelCenter := v.Project(v.Center()).Round()
		pixelInit := v.Project(initCenter).Round()
		assert.Equal(t, pixelInit.X, pixelCenter.X)
		assert.NotEqual(t, pixelInit.Y, pixelCenter.Y)
		// the view is inside the bounds
		assert.True(t, bounds.ContainsBounds(v.Bounds()))
	})

	t.Run("remove listeners when called without arguments", func(t *testing.T) {
		// Calling without arguments clears the bounds; here that is an
		// invalid LatLngBounds. The view is then free again.
		v := specViewSized(500, 500)
		v.SetLayerZoomLimits(0, 20, true, true) // TileLayer('', {minZoom: 0, maxZoom: 20})
		bounds := LatLngBoundsOf(LL(51.5, -0.05), LL(51.55, 0.05))
		v.SetMaxBounds(bounds)
		v.SetMaxBounds(LatLngBounds{})
		// set view outside
		center := LL(0, 0)
		v.SetView(center, 18)
		assert.True(t, v.TakeEvents().MoveEnd)
		assert.True(t, center.Equals(v.Center()))
	})

	t.Run("avoid subpixel / floating point related wobble (#8532)", func(t *testing.T) {
		v := specView()
		v.SetView(LL(50.450036, 30.5241361), 13)
		v.TakeEvents()
		v.SetMaxBounds(v.Bounds())
		assert.False(t, v.TakeEvents().MoveEnd)
	})
}

// describe('#setMinZoom and #setMaxZoom') — zoomlevelschange is not a
// ViewEvents flag; its spies reduce to the zoom and the limits, and to no
// view change being recorded.

func TestMap_SetMinZoomAndSetMaxZoom(t *testing.T) {
	t.Run("when map is not loaded", func(t *testing.T) {
		t.Run("change min and max zoom but not zoom", func(t *testing.T) {
			v := specView()
			v.SetZoom(2)
			v.SetMinZoom(3)
			assert.Equal(t, 2.0, v.Zoom())
			assert.Equal(t, 3.0, v.MinZoom())

			v.SetMaxZoom(7)
			assert.Equal(t, 2.0, v.Zoom())
			assert.Equal(t, 7.0, v.MaxZoom())
		})

		t.Run("do not fire 'zoomlevelschange'", func(t *testing.T) {
			v := specView()
			v.SetZoom(5)
			v.SetMinZoom(3)
			v.SetMaxZoom(7)

			assert.Equal(t, 5.0, v.Zoom())
			assert.Equal(t, 3.0, v.MinZoom())
			assert.Equal(t, 7.0, v.MaxZoom())
			assert.False(t, v.TakeEvents().Any())
		})
	})

	t.Run("when map is loaded", func(t *testing.T) {
		loaded := func() *View {
			v := specView()
			v.SetView(LL(0, 0), 4) // loads map
			v.TakeEvents()
			return v
		}

		t.Run("do not fire 'zoomlevelschange' if zoom level did not change", func(t *testing.T) {
			v := loaded()
			v.SetMinZoom(2)
			v.SetMaxZoom(7)

			assert.Equal(t, 4.0, v.Zoom())
			assert.Equal(t, 2.0, v.MinZoom())
			assert.Equal(t, 7.0, v.MaxZoom())
			assert.False(t, v.TakeEvents().Any())

			v.SetMinZoom(2)
			v.SetMaxZoom(7)
			assert.Equal(t, 4.0, v.Zoom())
			assert.False(t, v.TakeEvents().Any())
		})

		t.Run("fire 'zoomlevelschange' but do not change zoom if max/min zoom is less/more current zoom", func(t *testing.T) {
			v := loaded()
			v.SetMinZoom(2)
			v.SetMaxZoom(7)

			assert.Equal(t, 4.0, v.Zoom())
			assert.Equal(t, 2.0, v.MinZoom())
			assert.Equal(t, 7.0, v.MaxZoom())
			assert.False(t, v.TakeEvents().Any())
		})
	})

	t.Run("allow infinity to be passed", func(t *testing.T) {
		v := specView()
		v.SetMinZoom(math.Inf(1))
		v.SetMaxZoom(math.Inf(1))

		assert.Equal(t, math.Inf(1), v.MinZoom())
		assert.Equal(t, math.Inf(1), v.MaxZoom())
	})
}

// describe('#getMinZoom and #getMaxZoom') — the layers' contribution goes
// through SetLayerZoomLimits. A TileLayer's options default to minZoom 0 and
// maxZoom 18, so `new TileLayer('', {minZoom: 15})` contributes (15, 18) and
// `{maxZoom: 15}` contributes (0, 15).

func TestMap_GetMinZoomAndGetMaxZoom(t *testing.T) {
	t.Run("#getMinZoom", func(t *testing.T) {
		t.Run("returns 0 if not set by Map options or TileLayer options", func(t *testing.T) {
			assert.Equal(t, 0.0, specView().MinZoom())
		})
	})

	t.Run("minZoom and maxZoom options overrides any minZoom and maxZoom set on layers", func(t *testing.T) {
		v := specViewOpts(ViewOptions{MinZoom: 2, HasMinZoom: true, MaxZoom: 20, HasMaxZoom: true})
		// Three tile layers added in turn; the view sees the running
		// aggregate Layer.js's _updateZoomLevels computes after each.
		v.SetLayerZoomLimits(4, 10, true, true)
		v.SetLayerZoomLimits(4, 17, true, true)
		v.SetLayerZoomLimits(0, 22, true, true)

		assert.Equal(t, 2.0, v.MinZoom())
		assert.Equal(t, 20.0, v.MaxZoom())
	})

	t.Run("layer minZoom overrides map zoom if map has no minZoom set and layer minZoom is bigger than map zoom", func(t *testing.T) {
		v := specView()
		v.SetZoom(10)
		v.SetLayerZoomLimits(15, 18, true, true)
		assert.Equal(t, 15.0, v.MinZoom())
	})

	t.Run("layer maxZoom overrides map zoom if map has no maxZoom set and layer maxZoom is smaller than map zoom", func(t *testing.T) {
		v := specView()
		v.SetZoom(20)
		v.SetLayerZoomLimits(0, 15, true, true)
		assert.Equal(t, 15.0, v.MaxZoom())
	})

	t.Run("map's zoom is adjusted to layer's minZoom even if initialized with smaller value", func(t *testing.T) {
		v := specView()
		v.SetZoom(10)
		v.SetLayerZoomLimits(15, 18, true, true)
		assert.Equal(t, 15.0, v.Zoom())
	})

	t.Run("map's zoom is adjusted to layer's maxZoom even if initialized with larger value", func(t *testing.T) {
		v := specView()
		v.SetZoom(20)
		v.SetLayerZoomLimits(0, 15, true, true)
		assert.Equal(t, 15.0, v.Zoom())
	})
}

// describe('_addZoomLimit') — the two single-layer cases; upstream reads the
// private _layersMinZoom/_layersMaxZoom, which MinZoom/MaxZoom return while
// the view's own limits are unset.

func TestMap_AddZoomLimit(t *testing.T) {
	t.Run("update zoom levels when min zoom is a number in a layer that is added to map", func(t *testing.T) {
		v := specView()
		v.SetLayerZoomLimits(4, 18, true, true) // TileLayer('', {minZoom: 4})
		assert.Equal(t, 4.0, v.MinZoom())
	})

	t.Run("update zoom levels when max zoom is a number in a layer that is added to map", func(t *testing.T) {
		v := specView()
		v.SetLayerZoomLimits(0, 10, true, true) // TileLayer('', {maxZoom: 10})
		assert.Equal(t, 10.0, v.MaxZoom())
	})
}

// describe('#getSize')

func TestMap_GetSize(t *testing.T) {
	t.Run("return map size in pixels", func(t *testing.T) {
		assert.Equal(t, Pt(400, 400), specView().Size())
	})

	t.Run("return map size if not specified", func(t *testing.T) {
		v := NewView(ViewOptions{ZoomSnap: 1}) // a detached div
		assert.Equal(t, Pt(0, 0), v.Size())
	})

	t.Run("return map size if 0x0 pixels", func(t *testing.T) {
		v := specView()
		v.SetSize(Pt(0, 0))
		assert.Equal(t, Pt(0, 0), v.Size())
	})

	t.Run("return new pixels on change", func(t *testing.T) {
		v := specView()
		v.SetSize(Pt(300, 400))
		assert.Equal(t, Pt(300, 400), v.Size())
	})

	t.Run("return clone of size object from map", func(t *testing.T) {
		v := specView()
		size := v.Size()
		size.X = 1
		assert.Equal(t, Pt(400, 400), v.Size())
	})
}

// describe('#getPixelBounds')

func TestMap_GetPixelBounds(t *testing.T) {
	loaded := func() *View {
		v := specView()
		v.SetView(LL(0, 0), 0) // load map
		return v
	}

	t.Run("return map bounds in pixels", func(t *testing.T) {
		assert.Equal(t, BoundsOf(Pt(-72, -72), Pt(328, 328)), loaded().PixelBounds())
	})

	t.Run("return changed map bounds if really zoomed in", func(t *testing.T) {
		v := loaded()
		v.SetZoom(20)
		assert.Equal(t, BoundsOf(Pt(134217528, 134217528), Pt(134217928, 134217928)), v.PixelBounds())
	})

	t.Run("return new pixels on view change", func(t *testing.T) {
		v := loaded()
		v.SetView(LL(50, 50), 5)
		assert.Equal(t, BoundsOf(Pt(5034, 2578), Pt(5434, 2978)), v.PixelBounds())
	})
}

// describe('#getPixelOrigin')

func TestMap_GetPixelOrigin(t *testing.T) {
	loaded := func() *View {
		v := specView()
		v.SetView(LL(0, 0), 0) // load map
		return v
	}

	t.Run("return pixel origin", func(t *testing.T) {
		assert.Equal(t, Pt(-72, -72), loaded().PixelOrigin())
	})

	t.Run("return new pixels on view change", func(t *testing.T) {
		v := loaded()
		v.SetView(LL(50, 50), 5)
		assert.Equal(t, Pt(5034, 2578), v.PixelOrigin())
	})

	t.Run("return changed map bounds if really zoomed in", func(t *testing.T) {
		v := loaded()
		v.SetZoom(20)
		assert.Equal(t, Pt(134217528, 134217528), v.PixelOrigin())
	})
}

// describe('#getPixelWorldBounds')

func TestMap_GetPixelWorldBounds(t *testing.T) {
	t.Run("return map bounds in pixels", func(t *testing.T) {
		// Upstream's unloaded map has an undefined zoom, whose NaN scale
		// Transformation collapses to 1 (`scale ||= 1`): the world is the
		// unit square [2^-54, 2^-54]–[1, 1]. The unloaded View's zoom is 0,
		// so its world is that square at scale 256 — the same corners ×256.
		v := specView()
		b, ok := v.PixelWorldBounds()
		require.True(t, ok)
		assert.Equal(t, BoundsOf(
			Pt(256*5.551115123125783e-17, 256*5.551115123125783e-17), Pt(256, 256)), b)
	})

	t.Run("return changed map bounds if really zoomed in", func(t *testing.T) {
		v := specView()
		v.SetZoom(20)
		b, ok := v.PixelWorldBounds()
		require.True(t, ok)
		assert.Equal(t, BoundsOf(
			Pt(1.4901161193847656e-8, 1.4901161193847656e-8), Pt(268435456, 268435456)), b)
	})

	t.Run("return new pixels on zoom change", func(t *testing.T) {
		v := specView()
		v.SetZoom(5)
		b, ok := v.PixelWorldBounds()
		require.True(t, ok)
		assert.Equal(t, BoundsOf(
			Pt(4.547473508864641e-13, 4.547473508864641e-13), Pt(8192, 8192)), b)

		v.PanTo(LL(0, 0))

		// view does not change pixel world bounds
		b, ok = v.PixelWorldBounds()
		require.True(t, ok)
		assert.Equal(t, BoundsOf(
			Pt(4.547473508864641e-13, 4.547473508864641e-13), Pt(8192, 8192)), b)
	})

	t.Run("return infinity bounds on infinity zoom", func(t *testing.T) {
		v := specView()
		v.SetZoom(math.Inf(1))
		b, ok := v.PixelWorldBounds()
		require.True(t, ok)
		inf := math.Inf(1)
		assert.Equal(t, BoundsOf(Pt(inf, inf), Pt(inf, inf)), b)
	})
}

// describe('#invalidateSize') — the cases about keeping the centre through a
// resize, via SetSize. Upstream asserts the map pane's offset after each
// resize (1, 1, 2 px growing; 0, −1, −1 px shrinking): the pane is what keeps
// the geographic centre under the middle of the container. There is no pane
// here — SetSize recomputes the pixel origin from the centre, as a fresh
// setView at the new size would (_getNewPixelOrigin) — so the port asserts
// that: the origin is the one a setView gives, and the centre stays within
// half a pixel of the middle.

func TestMap_InvalidateSize(t *testing.T) {
	const origWidth = 100.0
	setup := func() (*View, LatLng) {
		v := specViewSized(origWidth, 100)
		center := LL(0, 0)
		v.SetView(center, 0)
		return v, center
	}
	centreKept := func(t *testing.T, v *View, center LatLng) {
		t.Helper()
		half := v.Size().DivideBy(2)
		assert.Equal(t, v.Project(center).Subtract(half).Round(), v.PixelOrigin())
		drift := v.LatLngToContainerPoint(center).Subtract(half)
		assert.LessOrEqual(t, math.Abs(drift.X), 0.5)
		assert.LessOrEqual(t, math.Abs(drift.Y), 0.5)
	}

	t.Run("pans by the right amount when growing in 1px increments", func(t *testing.T) {
		v, center := setup()
		for _, w := range []float64{origWidth + 1, origWidth + 2, origWidth + 3} {
			v.SetSize(Pt(w, 100))
			centreKept(t, v, center)
		}
	})

	t.Run("pans by the right amount when shrinking in 1px increments", func(t *testing.T) {
		v, center := setup()
		for _, w := range []float64{origWidth - 1, origWidth - 2, origWidth - 3} {
			v.SetSize(Pt(w, 100))
			centreKept(t, v, center)
		}
	})

	t.Run("pans back to the original position after growing by an odd size and back", func(t *testing.T) {
		v, center := setup()
		origin := v.PixelOrigin()
		v.SetSize(Pt(origWidth+5, 100))
		v.SetSize(Pt(origWidth, 100))
		assert.Equal(t, origin, v.PixelOrigin())
		centreKept(t, v, center)
	})

	t.Run("correctly adjusts for new container size when view is set during map initialization (#6165)", func(t *testing.T) {
		center := LL(0, 0)
		v := specViewSized(origWidth, 100)
		v.SetView(center, 0)

		// Before the map is told about the container's new size it keeps
		// the old one and positions coordinates by it.
		assert.Equal(t, 100.0, v.Size().X)
		assert.Equal(t, 50.0, v.LatLngToContainerPoint(center).X)

		// Now notifying the map that the container size has changed,
		// it should return new values and correctly position coordinates
		v.SetSize(Pt(600, 100))

		assert.Equal(t, 600.0, v.Size().X)
		assert.Equal(t, 300.0, v.LatLngToContainerPoint(center).X)
	})
}

// describe('#zoomIn and #zoomOut')

func TestMap_ZoomInAndZoomOut(t *testing.T) {
	center := LL(22, 33)
	at10 := func(opts ViewOptions) *View {
		v := specViewOpts(opts)
		v.SetView(center, 10)
		v.TakeEvents()
		return v
	}

	t.Run("zoomIn zooms by 1 zoom level by default", func(t *testing.T) {
		v := at10(ViewOptions{})
		v.ZoomIn(0)
		assert.True(t, v.TakeEvents().ZoomEnd)
		assert.Equal(t, 11.0, v.Zoom())
		assert.Equal(t, center, v.Center())
	})

	t.Run("zoomOut zooms by 1 zoom level by default", func(t *testing.T) {
		v := at10(ViewOptions{})
		v.ZoomOut(0)
		assert.True(t, v.TakeEvents().ZoomEnd)
		assert.Equal(t, 9.0, v.Zoom())
		assert.Equal(t, center, v.Center())
	})

	t.Run("zoomIn respects the zoomDelta option", func(t *testing.T) {
		v := at10(ViewOptions{ZoomSnap: 0.25, ZoomDelta: 0.25})
		v.ZoomIn(0)
		assert.True(t, v.TakeEvents().ZoomEnd)
		assert.Equal(t, 10.25, v.Zoom())
		assert.Equal(t, center, v.Center())
	})

	t.Run("zoomOut respects the zoomDelta option", func(t *testing.T) {
		v := at10(ViewOptions{ZoomSnap: 0.25, ZoomDelta: 0.25})
		v.ZoomOut(0)
		assert.True(t, v.TakeEvents().ZoomEnd)
		assert.Equal(t, 9.75, v.Zoom())
		assert.Equal(t, center, v.Center())
	})

	t.Run("zoomIn snaps to zoomSnap", func(t *testing.T) {
		v := at10(ViewOptions{ZoomSnap: 0.25})
		v.ZoomIn(0.22)
		assert.True(t, v.TakeEvents().ZoomEnd)
		assert.Equal(t, 10.25, v.Zoom())
		assert.Equal(t, center, v.Center())
	})

	t.Run("zoomOut snaps to zoomSnap", func(t *testing.T) {
		v := at10(ViewOptions{ZoomSnap: 0.25})
		v.ZoomOut(0.22)
		assert.True(t, v.TakeEvents().ZoomEnd)
		assert.Equal(t, 9.75, v.Zoom())
		assert.Equal(t, center, v.Center())
	})
}

// describe('#_getBoundsCenterZoom')

func TestMap_GetBoundsCenterZoom(t *testing.T) {
	center := LL(50.5, 30.51)

	t.Run("Returns valid center on empty bounds in uninitialized map", func(t *testing.T) {
		// Edge case from #5153
		v := specView()
		c, zoom := v.BoundsCenterZoom(LatLngBoundsOf(center, center), FitOptions{})
		assert.Equal(t, center, c)
		assert.Equal(t, math.Inf(1), zoom)
	})
}

// describe('#fitBounds') — the container is 100×100 ("fitBounds needs a map
// container with non-null area") and the view starts at (50.5, 30.51) zoom
// 15.

func TestMap_FitBounds(t *testing.T) {
	center := LL(50.5, 30.51)
	bounds := LatLngBoundsOf(LL(1, 102), LL(11, 122))
	boundsCenter := bounds.GetCenter()
	setup := func(opts ViewOptions) *View {
		opts.Size = Pt(100, 100)
		v := specViewOpts(opts)
		v.SetView(center, 15)
		v.TakeEvents()
		return v
	}

	t.Run("Snaps zoom level to integer by default", func(t *testing.T) {
		v := setup(ViewOptions{})
		require.NoError(t, v.FitBounds(bounds, FitOptions{}))
		assert.True(t, v.TakeEvents().ZoomEnd)
		assert.Equal(t, 2.0, v.Zoom())
		assert.True(t, v.Center().EqualsWithin(boundsCenter, 0.05))
	})

	t.Run("Snaps zoom to zoomSnap", func(t *testing.T) {
		v := setup(ViewOptions{ZoomSnap: 0.25})
		require.NoError(t, v.FitBounds(bounds, FitOptions{}))
		assert.True(t, v.TakeEvents().ZoomEnd)
		assert.Equal(t, 2.75, v.Zoom())
		assert.True(t, v.Center().EqualsWithin(boundsCenter, 0.05))
	})

	t.Run("can be called with an array", func(t *testing.T) {
		v := setup(ViewOptions{})
		bounds := NewLatLngBounds(LL(1, 102), LL(11, 122))
		require.NoError(t, v.FitBounds(bounds, FitOptions{}))
		assert.True(t, v.TakeEvents().ZoomEnd)
		assert.Equal(t, 2.0, v.Zoom())
		assert.True(t, v.Center().EqualsWithin(boundsCenter, 0.05))
	})

	t.Run("throws an error with invalid bounds", func(t *testing.T) {
		v := setup(ViewOptions{})
		assert.Error(t, v.FitBounds(LatLngBounds{}, FitOptions{}))
	})

	t.Run("Fits to same scale and zoom", func(t *testing.T) {
		v := setup(ViewOptions{})
		bounds, zoom := v.Bounds(), v.Zoom()
		require.NoError(t, v.FitBounds(bounds, FitOptions{}))
		assert.True(t, v.TakeEvents().MoveEnd)
		newBounds := v.Bounds()
		assertNearLL(t, bounds.GetSouthWest(), newBounds.GetSouthWest(), 1e-4)
		assertNearLL(t, bounds.GetNorthEast(), newBounds.GetNorthEast(), 1e-4)
		assert.Equal(t, zoom, v.Zoom())
	})

	t.Run("Fits to small bounds from small zoom", func(t *testing.T) {
		v := setup(ViewOptions{})
		bounds := LatLngBoundsOf(LL(57.73, 11.93), LL(57.75, 11.95))
		boundsCenter := bounds.GetCenter()
		v.SetZoom(0)
		assert.True(t, v.TakeEvents().ZoomEnd)

		require.NoError(t, v.FitBounds(bounds, FitOptions{}))
		assert.True(t, v.TakeEvents().ZoomEnd)
		assert.Equal(t, 11.0, v.Zoom())
		assert.True(t, v.Center().EqualsWithin(boundsCenter, 0.05))
	})

	t.Run("Fits to large bounds from large zoom", func(t *testing.T) {
		v := setup(ViewOptions{})
		bounds := LatLngBoundsOf(LL(90, -180), LL(-90, 180))
		boundsCenter := bounds.GetCenter()
		v.SetZoom(22)
		assert.True(t, v.TakeEvents().ZoomEnd)

		require.NoError(t, v.FitBounds(bounds, FitOptions{}))
		assert.True(t, v.TakeEvents().ZoomEnd)
		assert.Equal(t, 0.0, v.Zoom())
		assert.True(t, v.Center().EqualsWithin(boundsCenter, 0.05))
	})
}

// describe('#fitBounds after layers set') — 100×100, unloaded; upstream's
// `getZoom()` is undefined until fitBounds loads the map, which here is an
// unloaded view at its zero zoom.

func TestMap_FitBoundsAfterLayersSet(t *testing.T) {
	bounds := LatLngBoundsOf(LL(1, 102), LL(11, 122))

	t.Run("Snaps to a number after adding tile layer", func(t *testing.T) {
		v := specViewSized(100, 100)
		v.SetLayerZoomLimits(0, 18, true, true) // TileLayer(''): minZoom 0, maxZoom 18
		assert.False(t, v.Loaded())
		assert.Equal(t, 0.0, v.Zoom())
		require.NoError(t, v.FitBounds(bounds, FitOptions{}))
		assert.Equal(t, 2.0, v.Zoom())
	})

	t.Run("Snaps to a number after adding marker", func(t *testing.T) {
		// A marker is not a zoom-bound layer: it contributes no limits.
		v := specViewSized(100, 100)
		v.SetLayerZoomLimits(0, 0, false, false)
		assert.False(t, v.Loaded())
		assert.Equal(t, 0.0, v.Zoom())
		require.NoError(t, v.FitBounds(bounds, FitOptions{}))
		assert.Equal(t, 2.0, v.Zoom())
	})
}

// describe('#fitWorld') — 100×100.

func TestMap_FitWorld(t *testing.T) {
	bounds := LatLngBoundsOf(LL(90, -180), LL(-90, 180))
	boundsCenter := bounds.GetCenter()

	t.Run("map zooms out to max view with default settings", func(t *testing.T) {
		v := specViewSized(100, 100)
		v.SetZoom(5)
		v.FitWorld(FitOptions{})

		assert.Equal(t, 0.0, v.Zoom())
		assert.True(t, v.Center().EqualsWithin(boundsCenter, 0.05))
	})
}

// describe('#panInside') — 500×500, the view at (53.0, 0.15) zoom 12.

func TestMap_PanInside(t *testing.T) {
	noPad := Pt(0, 0)
	setup := func() (v *View, center, tl LatLng, tlPix Point) {
		v = specViewSized(500, 500)
		v.SetView(LL(53.0, 0.15), 12)
		center = v.Center()
		tl = v.Bounds().GetNorthWest()
		tlPix = v.PixelBounds().Min
		return
	}

	t.Run("does not pan the map when the target is within bounds", func(t *testing.T) {
		v, center, tl, _ := setup()
		v.PanInside(tl, noPad, noPad)
		assert.Equal(t, center, v.Center())
	})

	t.Run("pans the map when padding is provided and the target is within the border area", func(t *testing.T) {
		v, _, _, tlPix := setup()
		padding := Pt(40, 20)
		p := tlPix.Add(Pt(30, 0)) // Top-left
		v.PanInside(v.Unproject(p), padding, padding)
		distanceMoved := v.PixelBounds().Min.Subtract(tlPix)
		assert.True(t, distanceMoved.Equals(Pt(-10, -20)), "moved %v", distanceMoved)

		tlPix = v.PixelBounds().Min
		p = Pt(v.PixelBounds().Max.X-10, v.PixelBounds().Min.Y) // Top-right
		v.PanInside(v.Unproject(p), padding, padding)
		distanceMoved = v.PixelBounds().Min.Subtract(tlPix)
		assert.True(t, distanceMoved.Equals(Pt(30, -20)), "moved %v", distanceMoved)

		tlPix = v.PixelBounds().Min
		p = Pt(v.PixelBounds().Min.X+35, v.PixelBounds().Max.Y) // Bottom-left
		v.PanInside(v.Unproject(p), padding, padding)
		distanceMoved = v.PixelBounds().Min.Subtract(tlPix)
		assert.True(t, distanceMoved.Equals(Pt(-5, 20)), "moved %v", distanceMoved)

		tlPix = v.PixelBounds().Min
		p = Pt(v.PixelBounds().Max.X-15, v.PixelBounds().Max.Y) // Bottom-right
		v.PanInside(v.Unproject(p), padding, padding)
		distanceMoved = v.PixelBounds().Min.Subtract(tlPix)
		assert.True(t, distanceMoved.Equals(Pt(25, 20)), "moved %v", distanceMoved)
	})

	t.Run("supports different padding values for each border", func(t *testing.T) {
		// Upstream passes `paddingTL`/`paddingBR`, which panInside does not
		// read (its options are paddingTopLeft/paddingBottomRight), so both
		// of its calls run unpadded: a point 40 px in from the left edge is
		// inside, a point 20 px right of the right edge is not. Ported as it
		// behaves, then with the per-border paddings the title promises.
		v, center, _, tlPix := setup()
		p := tlPix.Add(Pt(40, 0)) // Top-Left
		v.PanInside(v.Unproject(p), noPad, noPad)
		assert.Equal(t, center, v.Center())

		br := v.PixelBounds().Max // Bottom-Right
		v.PanInside(v.Unproject(Pt(br.X+20, br.Y)), noPad, noPad)
		assert.NotEqual(t, center, v.Center())

		// With a 60×20 top-left padding the same top-left point is 20 px
		// inside the padded edge on both axes; the 10×10 bottom-right one
		// is not reached.
		v, _, _, tlPix = setup()
		v.PanInside(v.Unproject(p), Pt(60, 20), Pt(10, 10))
		distanceMoved := v.PixelBounds().Min.Subtract(tlPix)
		assert.True(t, distanceMoved.Equals(Pt(-20, -20)), "moved %v", distanceMoved)
	})

	t.Run("pans on both X and Y axes when the target is outside of the view area and both the point's coords are outside the bounds", func(t *testing.T) {
		v, center, _, tlPix := setup()
		p := v.Unproject(tlPix.Subtract(Pt(200, 200)))
		v.PanInside(p, noPad, noPad)
		assert.True(t, v.Bounds().Contains(p))
		assert.NotEqual(t, center.Lng, v.Center().Lng)
		assert.NotEqual(t, center.Lat, v.Center().Lat)
	})

	t.Run("pans only on the Y axis when the target's X coord is within bounds but the Y is not", func(t *testing.T) {
		v, center, tl, _ := setup()
		p := LL(tl.Lat+5, tl.Lng)
		v.PanInside(p, noPad, noPad)
		assert.True(t, v.Bounds().Contains(p))
		dx := math.Abs(v.Center().Lng - center.Lng)
		assert.Less(t, dx, 1.0e-9)
		assert.NotEqual(t, center.Lat, v.Center().Lat)
	})

	t.Run("pans only on the X axis when the target's Y coord is within bounds but the X is not", func(t *testing.T) {
		v, center, tl, _ := setup()
		p := LL(tl.Lat, tl.Lng-5)
		v.PanInside(p, noPad, noPad)
		assert.True(t, v.Bounds().Contains(p))
		assert.NotEqual(t, center.Lng, v.Center().Lng)
		// Upstream compares the signed difference; the magnitude is meant.
		dy := math.Abs(v.Center().Lat - center.Lat)
		assert.Less(t, dy, 1.0e-9)
	})

	t.Run("pans correctly when padding takes up more than half the display bounds", func(t *testing.T) {
		v, center, _, _ := setup()
		oldCenter := v.Project(center)
		targetOffset := Pt(0, -5) // arbitrary point above center
		target := oldCenter.Add(targetOffset)
		paddingOffset := Pt(0, 15)
		padding := v.Size().DivideBy(2). // half size
							Add(paddingOffset) // padding more than half the display bounds (replicates issue #7445)
		v.PanInside(v.Unproject(target), noPad, Pt(0, padding.Y))
		offset := v.Project(v.Center()).Subtract(oldCenter) // distance moved during the pan
		result := paddingOffset.Add(targetOffset).Subtract(offset)
		assert.True(t, result.Trunc().Equals(Pt(0, 0)), "result %v", result)
	})
}

// describe('#getScaleZoom && #getZoomScale')

func TestMap_GetScaleZoomAndGetZoomScale(t *testing.T) {
	t.Run("converts zoom to scale and vice versa and returns the same values", func(t *testing.T) {
		v := specView()
		toZoom, fromZoom := 6.25, 8.5
		scale := v.ZoomScaleAt(toZoom, fromZoom)
		assert.Equal(t, toZoom, jsRound(v.ScaleZoomAt(scale, fromZoom)*100)/100)
	})

	t.Run("converts scale to zoom and returns Infinity if map crs.zoom returns NaN", func(t *testing.T) {
		stub := &stubCRS{CRSI: EPSG3857, zoom: func(float64) (float64, bool) { return math.NaN(), true }}
		v := specViewOpts(ViewOptions{CRS: stub})
		scale, fromZoom := 0.25, 8.5
		assert.Equal(t, math.Inf(1), v.ScaleZoomAt(scale, fromZoom))
	})
}

// describe('#getZoom') — an uninitialised map's zoom is undefined upstream;
// here Loaded is false and the zoom is its zero.

func TestMap_GetZoom(t *testing.T) {
	t.Run("returns undefined if map not initialized", func(t *testing.T) {
		v := specView()
		assert.False(t, v.Loaded())
		assert.Equal(t, 0.0, v.Zoom())
	})

	t.Run("returns undefined if map not initialized but layers added", func(t *testing.T) {
		v := specView()
		v.SetLayerZoomLimits(0, 18, true, true) // TileLayer('')
		assert.False(t, v.Loaded())
		assert.Equal(t, 0.0, v.Zoom())
	})
}

// describe('#distance')

func TestMap_Distance(t *testing.T) {
	v := specView()

	t.Run("measure distance in meters", func(t *testing.T) {
		LA := LL(34.0485672098387, -118.217781922035)
		columbus := LL(39.95715687063701, -83.00205705857633)
		assertWithin(t, 3173910, 3173915, v.Distance(LA, columbus))
	})

	t.Run("accurately measure in small distances", func(t *testing.T) {
		p1 := LL(40.160857881285416, -83.00841851162649)
		p2 := LL(40.16246493902907, -83.008622359483)
		assertWithin(t, 175, 185, v.Distance(p1, p2))
	})

	t.Run("accurately measure in long distances", func(t *testing.T) {
		canada := LL(60.01810635103154, -112.19675246283015)
		newZeland := LL(-42.36275164460971, 172.39309066597883)
		assertWithin(t, 13274700, 13274800, v.Distance(canada, newZeland))
	})

	t.Run("return 0 with 2 same latLng", func(t *testing.T) {
		p := LL(20, 50)
		assert.Equal(t, 0.0, v.Distance(p, p))
	})
}

// describe('#containerPointToLayerPoint') and
// describe('#layerPointToContainerPoint') — the pane offset is always 0
// here, so layer points and container points coincide and both conversions
// are the identity. Upstream's (80, 80) and (−20, −20) after a panBy of
// (50, 50) are the moved pane; here PanBy moves the pixel origin instead,
// and the geography under a container point shifts by the same 50 px.

func TestMap_ContainerPointToLayerPoint(t *testing.T) {
	t.Run("return same point of LayerPoint is 0, 0", func(t *testing.T) {
		v := specView()
		assert.Equal(t, v.LayerPointToLatLng(Pt(25, 25)), v.ContainerPointToLatLng(Pt(25, 25)))
	})

	t.Run("return point relative to LayerPoint", func(t *testing.T) {
		v := specView()
		v.SetView(LL(20, 20), 2)
		before := v.ContainerPointToLatLng(Pt(80, 80))
		v.TakeEvents()

		v.PanBy(Pt(50, 50))
		assert.True(t, v.TakeEvents().MoveEnd)
		assert.Equal(t, v.LayerPointToLatLng(Pt(30, 30)), v.ContainerPointToLatLng(Pt(30, 30)))
		// Container (30, 30) now shows what container (80, 80) did.
		assert.True(t, before.Equals(v.ContainerPointToLatLng(Pt(30, 30))))
	})
}

func TestMap_LayerPointToContainerPoint(t *testing.T) {
	t.Run("return same point of ContainerPoint is 0, 0", func(t *testing.T) {
		v := specView()
		ll := v.LayerPointToLatLng(Pt(25, 25))
		assert.Equal(t, v.LatLngToLayerPoint(ll), v.LatLngToContainerPoint(ll))
		assert.Equal(t, Pt(25, 25), v.LatLngToContainerPoint(ll))
	})

	t.Run("return point relative to ContainerPoint", func(t *testing.T) {
		v := specView()
		v.SetView(LL(20, 20), 2)
		ll := v.ContainerPointToLatLng(Pt(30, 30))
		v.TakeEvents()

		v.PanBy(Pt(50, 50))
		assert.True(t, v.TakeEvents().MoveEnd)
		assert.Equal(t, v.LatLngToLayerPoint(ll), v.LatLngToContainerPoint(ll))
		// What was under container (30, 30) is now at (−20, −20).
		assert.Equal(t, Pt(-20, -20), v.LatLngToContainerPoint(ll))
	})
}

// describe('#containerPointToLatLng')

func TestMap_ContainerPointToLatLng(t *testing.T) {
	t.Run("returns geographical coordinate for point relative to map container", func(t *testing.T) {
		v := specView()
		center := LL(10, 10)
		v.SetView(center, 50)
		p := v.ContainerPointToLatLng(Pt(200, 200))
		assertWithin(t, 10.0000000, 10.0000001, p.Lat)
		assertWithin(t, 10.0000000, 10.0000001, p.Lng)
	})
}

// describe('#latLngToContainerPoint')

func TestMap_LatLngToContainerPoint(t *testing.T) {
	t.Run("returns point relative to map container for geographical coordinate", func(t *testing.T) {
		v := specView()
		center := LL(10, 10)
		v.PanTo(center)
		p := v.LatLngToContainerPoint(center)
		assert.Equal(t, 200.0, p.X)
		assert.Equal(t, 200.0, p.Y)
	})
}

// describe('#panTo')

func TestMap_PanTo(t *testing.T) {
	t.Run("pans the map to accurate location", func(t *testing.T) {
		v := specView()
		center := LL(50, 30)
		v.PanTo(center)
		assert.Less(t, v.Center().DistanceTo(center), 5.0)
	})
}

// describe('#panInsideBounds')

func TestMap_PanInsideBounds(t *testing.T) {
	t.Run("doesn't pan if already in bounds", func(t *testing.T) {
		v := specView()
		v.PanTo(LL(0, 0))
		bounds := LatLngBoundsOf(LL(-1, -1), LL(1, 1))
		expectedCenter := LL(0, 0)
		v.PanInsideBounds(bounds)
		assertNearLL(t, expectedCenter, v.Center(), 1e-4)
	})

	t.Run("pans to closest view in bounds", func(t *testing.T) {
		v := specView()
		bounds := LatLngBoundsOf(LL(41.8, -87.6), LL(40.7, -74))
		expectedCenter := LL(41.59452223189, -74.2738647460)
		v.SetView(LL(50.5, 30.5), 10)
		v.PanInsideBounds(bounds)
		assertNearLL(t, expectedCenter, v.Center(), 1e-4)
	})
}

// describe('#project')

func TestMap_Project(t *testing.T) {
	const tolerance = 1.0 / 1000000

	t.Run("returns pixel coordinates relative to the top-left of the CRS extents", func(t *testing.T) {
		v := specView()
		v.SetView(LL(40, -83), 5)
		a := v.ProjectAt(LL(40, -83), 5)
		assert.InDelta(t, 2207.288888, a.X, tolerance)
		assert.InDelta(t, 3101.320460, a.Y, tolerance)
	})

	t.Run("test the other coordinates", func(t *testing.T) {
		v := specView()
		v.SetView(LL(40, 83), 5)
		b := v.ProjectAt(LL(40, 83), 5)
		assert.InDelta(t, 5984.7111111, b.X, tolerance)
		assert.InDelta(t, 3101.3204602, b.Y, tolerance)
	})

	t.Run("test the prev coordinates with different zoom", func(t *testing.T) {
		v := specView()
		v.SetView(LL(40, 83), 5)
		b := v.ProjectAt(LL(40, 83), 6)
		assert.InDelta(t, 11969.422222, b.X, tolerance)
		assert.InDelta(t, 6202.640920, b.Y, tolerance)
	})
}

// describe('#latLngToLayerPoint')

func TestMap_LatLngToLayerPoint(t *testing.T) {
	t.Run("returns the corresponding pixel coordinate relative to the origin pixel", func(t *testing.T) {
		v := specView()
		center := LL(10, 10)
		v.SetView(center, 0)
		p := v.LatLngToLayerPoint(center)
		assert.Equal(t, 200.0, p.X)
		assert.Equal(t, 200.0, p.Y)
	})
}

// describe('#layerPointToLatLng')

func TestMap_LayerPointToLatLng(t *testing.T) {
	t.Run("returns the corresponding geographical coordinate for a pixel coordinate relative to the origin pixel", func(t *testing.T) {
		v := specView()
		center := LL(10, 10)
		v.SetView(center, 10)
		latlng := v.LayerPointToLatLng(Pt(200, 200))
		assertNearLL(t, LL(9.9999579356371, 10.000305175781252), latlng, 1e-4)
	})
}

// describe('#panBy')

func TestMap_PanBy(t *testing.T) {
	offset := Pt(1000, 1000)

	t.Run("pans the map by given offset", func(t *testing.T) {
		v := specView()
		center := LL(0, 0)
		v.SetView(center, 7)
		offsetCenterPoint := v.CRS().LatLngToPoint(center, 7).Add(offset)
		target := v.CRS().PointToLatLng(offsetCenterPoint, 7)

		v.PanBy(offset)
		assert.Less(t, v.Center().DistanceTo(target), 5.0)
		assertNearLL(t, LL(-10.9196177602, 10.9863281250), v.Center(), 1e-4)
	})
}

// describe('#unproject')

func TestMap_Unproject(t *testing.T) {
	t.Run("returns the latitude and langitude with given point", func(t *testing.T) {
		v := specView()
		v.SetView(LL(0, 0), 6)
		expectedOutput := LL(82.7432022836318, -175.60546875000003)
		output := v.Unproject(Pt(200, 1000))
		assertNearLL(t, expectedOutput, output, 1e-4)
	})

	t.Run("return the latitude and langitude with different zoom and points", func(t *testing.T) {
		v := specView()
		v.SetView(LL(0, 0), 10)
		expectedOutput := LL(85.03926769025156, -179.98626708984378)
		output := v.Unproject(Pt(10, 100))
		assertNearLL(t, expectedOutput, output, 1e-4)
	})
}
