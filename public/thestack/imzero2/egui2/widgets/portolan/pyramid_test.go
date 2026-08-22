// Port of Leaflet's spec/suites/layer/tile/GridLayerSpec.js at upstream commit
// c96f31a7a350a07cfbc852cf88e6ca69af5f5ec9, against the pyramid (pyramid.go)
// and the view (view.go). Each upstream `describe` is a Test function, each
// `it` a subtest named by its upstream title. The counts are the tile events
// PyramidStats records — tileloadstart, tileload, tileunload, tileerror —
// which is how the upstream suite pins the pyramid.
//
// The upstream grid is a GridLayer with a synchronous createTile, whose
// _tileReady Leaflet defers to the next animation frame; here OnRequest only
// records the request and deliver() is that frame. Tile opacity and the prune
// timer run from sinon's fake clock upstream and from Tick with an advancing
// fake time here. Leaflet's default zoomSnap is 1 where the View's is 0, so
// every view here snaps.
//
// Two mechanisms differ from upstream and are noted at the tests they touch:
//   - Leaflet's non-animated view changes fire viewprereset, on which
//     GridLayer drops every tile (_invalidateAll) before reloading; the port
//     has no such event and keeps the old level's loaded tiles until the new
//     level covers them (the prune), which reaches the same tileunload totals
//     in the zoom series by a different route.
//   - The "animated grid" describe animates its zooms; the port zooms hard
//     (the zoom animation is milestone M3), so the counts upstream reaches
//     through the animation's noPrune window arrive at the zoom itself, and
//     the fade and the prune timer do the rest. The final counts are upstream's.
//
// Ported with an analogue rather than literally:
//   - #redraw › "can be called before map.setView": Sync and Update on an
//     unloaded view are the no-op.
//   - #getMaxZoom, #getMinZoom over several layers: the View keeps one tile
//     source's limits, so the fold over a layer set (src/layer/Layer.js
//     _updateZoomLevels) is the test's zoomLimitLayers.
//   - min/maxNativeZoom option › "redraws tiles properly after changing
//     maxNativeZoom": the pyramid has no redraw yet; Reset then SetView is
//     the same drop-and-reload, reaching into the package's fields.
//   - #createTile › "does not add the .leaflet-tile-loaded class to tile
//     elements": the port's mark of a tile that did not load is Failed.
//
// Not ported from upstream:
//   - #setOpacity › "can be called before map.setView" and "works when map
//     has fadeAnimated=false (IE8 is exempt)": the layer's opacity is the
//     DOM container's CSS; the port's Opacity is a TileSource field that
//     Draw multiplies in.
//   - #onAdd › "is called after zoomend on first map load": the order of a
//     layer's add against the map's events is the widget's (M4).
//   - #getMaxZoom, #getMinZoom › accessing a gridlayer's properties ›
//     "provides a container": DOM.
//   - number of 256px tiles loaded in synchronous animated grid @800x600px ›
//     "Loads 224, unloads 209 tiles on MAD-TRD flyTo()": flyTo is a later
//     milestone (M3).

package portolan

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gridSource is the upstream suite's `new GridLayer()`: TileLayer's defaults
// but for maxZoom, which a GridLayer does not have.
func gridSource() TileSource {
	src := NewTileSource("{z}/{x}/{y}")
	src.MaxZoom = math.Inf(1)
	return src
}

// gridTest is GridLayerSpec's map and grid: a View, a Pyramid, a synchronous
// createTile that remembers what it was asked for and delivers on the next
// frame, and the fake clock the upstream suite drives with sinon.
type gridTest struct {
	t   *testing.T
	v   *View
	p   *Pyramid
	now time.Time
	// pending are the requests not yet delivered — upstream's _tileReady,
	// which a synchronous createTile defers to the next animation frame.
	pending []TileCoords
	// requested is every request, by the unwrapped coordinates the pyramid
	// keys tiles by; created is the same calls with the wrapped coordinates
	// upstream passes createTile.
	requested, created []TileCoords
}

// newGridTest is the suite's beforeEach: a map of the given size with the
// grid's zoom limits registered on it (GridLayer.beforeAdd → _addZoomLimit).
// The clock starts away from the zero time for the reason the upstream suite
// ticks its clock off 0: a zero instant reads as "not set".
func newGridTest(t *testing.T, size Point, src TileSource) *gridTest {
	t.Helper()
	g := &gridTest{
		t:   t,
		v:   NewView(ViewOptions{Size: size, ZoomSnap: 1}),
		p:   NewPyramid(src),
		now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	g.v.SetLayerZoomLimits(src.MinZoom, src.MaxZoom, true, !math.IsInf(src.MaxZoom, 1))
	g.p.OnRequest = func(coords, wrapped TileCoords) {
		g.pending = append(g.pending, coords)
		g.requested = append(g.requested, coords)
		g.created = append(g.created, wrapped)
	}
	return g
}

// sync hands the frame's view events to the pyramid, as GridLayer's
// getEvents wiring does.
func (g *gridTest) sync() { g.p.Sync(g.v, g.v.TakeEvents()) }

func (g *gridTest) setView(center LatLng, zoom float64) { g.v.SetView(center, zoom); g.sync() }
func (g *gridTest) setZoom(zoom float64)                { g.v.SetZoom(zoom); g.sync() }
func (g *gridTest) zoomIn()                             { g.v.ZoomIn(0); g.sync() }
func (g *gridTest) panBy(offset Point)                  { g.v.PanBy(offset); g.sync() }

// addLayer is map.addLayer(grid) on a map that already has a view:
// GridLayer.onAdd → _resetView.
func (g *gridTest) addLayer() {
	g.v.TakeEvents()
	g.p.SetView(g.v, false, false)
}

// deliver is the next animation frame's _tileReady for every pending request.
func (g *gridTest) deliver() { g.deliverAs(false) }

// deliverErrors is deliver with every tile's done() called with an error.
func (g *gridTest) deliverErrors() { g.deliverAs(true) }

func (g *gridTest) deliverAs(failed bool) {
	pending := g.pending
	g.pending = nil
	for _, c := range pending {
		g.p.TileReady(g.v, c, failed, g.now)
	}
}

// tick is one animation frame of _updateOpacity and the prune timer.
func (g *gridTest) tick() bool { return g.p.Tick(g.v, g.now) }

// advance is clock.tick.
func (g *gridTest) advance(d time.Duration) { g.now = g.now.Add(d) }

// expectCounts is the suite's counts assertion: tileloadstart, tileload and
// tileunload so far.
func (g *gridTest) expectCounts(loadStart, load, unload int) {
	g.t.Helper()
	s := g.p.Stats()
	assert.Equal(g.t, loadStart, s.TileLoadStart, "tileloadstart")
	assert.Equal(g.t, load, s.TileLoad, "tileload")
	assert.Equal(g.t, unload, s.TileUnload, "tileunload")
}

// createdKeys are the wrapped "x:y:z" keys createTile was called with, in
// order.
func (g *gridTest) createdKeys() (keys []string) {
	for _, c := range g.created {
		keys = append(keys, c.Key())
	}
	return
}

// zoomLimitLayers is the map-side bookkeeping of src/layer/Layer.js —
// _addZoomLimit, _removeZoomLimit and _updateZoomLevels: a map's limits are
// the widest of its layers'. The View holds one source's limits (it has one
// tile source), so the fold over several lives here.
type zoomLimitLayers struct {
	v      *View
	layers map[int][2]float64
	n      int
}

func (l *zoomLimitLayers) add(minZoom, maxZoom float64) (id int) {
	if l.layers == nil {
		l.layers = map[int][2]float64{}
	}
	l.n++
	l.layers[l.n] = [2]float64{minZoom, maxZoom}
	l.update()
	return l.n
}

func (l *zoomLimitLayers) remove(id int) {
	delete(l.layers, id)
	l.update()
}

func (l *zoomLimitLayers) update() {
	minZoom, maxZoom := math.Inf(1), math.Inf(-1)
	for _, lim := range l.layers {
		minZoom = math.Min(minZoom, lim[0])
		maxZoom = math.Max(maxZoom, lim[1])
	}
	l.v.SetLayerZoomLimits(minZoom, maxZoom, !math.IsInf(minZoom, 1), !math.IsInf(maxZoom, -1))
}

func TestGridLayer_Redraw(t *testing.T) {
	t.Run("can be called before map.setView", func(t *testing.T) {
		// Upstream the layer's add waits for the map's first view and
		// redraw() is a no-op until then; here Sync and Update on an
		// unloaded view request nothing.
		g := newGridTest(t, Pt(800, 600), gridSource())
		g.sync()
		g.p.Update(g.v)
		assert.Empty(t, g.created)
		assert.Equal(t, 0, g.p.TileCount())
	})
}

func TestGridLayer_PositionsTilesCorrectlyWithWrappingAndBounding(t *testing.T) {
	g := newGridTest(t, Pt(800, 600), gridSource())
	g.setView(LL(0, 0), 1)
	g.deliver()

	// Upstream keys each tile by its DOM position, which is relative to the
	// level's origin — the pixel origin after a project∘unproject round trip
	// that clamps at the Mercator latitude limit, so the level origin is
	// (-144, 0) where the pixel origin is (-144, -44), and the level's
	// transform adds the 44 px back. Draw returns the on-screen rect
	// directly, so the rows sit at 44 and 300 where upstream's positions
	// read 0 and 256: the same tiles in the same places.
	loaded := map[string][2]int{}
	for _, d := range g.p.Draw(g.v, g.now) {
		loaded[fmt.Sprintf("%g:%g", d.Rect.Min.X, d.Rect.Min.Y)] = [2]int{d.Wrapped.X, d.Wrapped.Y}
	}
	assert.Equal(t, map[string][2]int{
		"144:44":   {0, 0},
		"400:44":   {1, 0},
		"144:300":  {0, 1},
		"400:300":  {1, 1},
		"-112:44":  {1, 0},
		"656:44":   {0, 0},
		"-112:300": {1, 1},
		"656:300":  {0, 1},
	}, loaded)
	// The bounding: the world is two rows of tiles at zoom 1 and no tile is
	// requested outside them; the wrapping: the columns either side of the
	// world are the world's own.
	assert.Equal(t, 2, g.p.GlobalRows())
	assert.Equal(t, 8, g.p.TileCount())
	assert.Equal(t, TileCoords{1, 0, 1}, g.p.WrapCoords(TileCoords{-1, 0, 1}))
	assert.Equal(t, TileCoords{0, 1, 1}, g.p.WrapCoords(TileCoords{2, 1, 1}))
}

func TestGridLayer_TilePyramid(t *testing.T) {
	t.Run("removes tiles for unused zoom levels", func(t *testing.T) {
		g := newGridTest(t, Pt(800, 600), gridSource())
		g.p.SetFade(false)
		g.setView(LL(0, 0), 1)
		// Zoom out before the zoom-1 tiles' frame, as upstream does: they
		// leave unloaded — Leaflet's _invalidateAll on viewprereset, here
		// TileLayer's _abortLoading, so tileabort rather than tileunload.
		g.setZoom(0)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		assert.Equal(t, 8, g.p.Stats().Abort)
		assert.Equal(t, []int{0}, g.p.Levels())
		// Upstream keys the surviving tiles by the wrapped coordinates
		// createTile saw and finds one: the world's single tile at zoom 0,
		// which the 800 px wide view shows five times.
		wrapped := map[string]bool{}
		for _, d := range g.p.Draw(g.v, g.now) {
			wrapped[d.Wrapped.Key()] = true
		}
		assert.Equal(t, map[string]bool{"0:0:0": true}, wrapped)
		assert.Equal(t, 5, g.p.TileCount())
	})
}

func TestGridLayer_CreateTile(t *testing.T) {
	// Simpler sizes to test: 512 px square, four tiles at zoom 10.
	newGrid := func(t *testing.T) *gridTest {
		g := newGridTest(t, Pt(512, 512), gridSource())
		g.v.SetView(LL(0, 0), 10)
		return g
	}

	t.Run("only creates tiles for visible area on zoom in", func(t *testing.T) {
		g := newGrid(t)
		g.addLayer()
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		assert.Len(t, g.created, 4) // on layer add
		g.created = nil
		g.zoomIn()
		g.deliver()
		require.Equal(t, 2, g.p.Stats().Load)
		assert.Len(t, g.created, 4) // on zoom in
	})

	t.Run("when done() is called with an error parameter", func(t *testing.T) {
		// createTile calls done('error') for every tile: here every pending
		// request is delivered failed.
		newFailing := func(t *testing.T) *gridTest {
			g := newGrid(t)
			g.addLayer()
			g.deliverErrors()
			require.Len(t, g.created, 4)
			return g
		}
		t.Run("does not raise tileload events", func(t *testing.T) {
			g := newFailing(t)
			assert.Equal(t, 0, g.p.Stats().TileLoad)
		})
		t.Run("raises tileerror events", func(t *testing.T) {
			g := newFailing(t)
			assert.Equal(t, 4, g.p.Stats().TileError)
		})
		t.Run("does not add the .leaflet-tile-loaded class to tile elements", func(t *testing.T) {
			g := newFailing(t)
			for _, c := range g.requested {
				assert.True(t, g.p.Failed(c), "tile %v", c)
			}
		})
	})
}

func TestGridLayer_GetMaxZoomGetMinZoom(t *testing.T) {
	newMap := func() *View {
		v := NewView(ViewOptions{Size: Pt(800, 600), ZoomSnap: 1})
		v.SetView(LL(0, 0), 1)
		return v
	}

	t.Run("when a gridlayer is added to a map with no other layers", func(t *testing.T) {
		t.Run("has the same zoomlevels as the gridlayer", func(t *testing.T) {
			v := newMap()
			const maxZoom, minZoom = 10.0, 5.0
			src := gridSource()
			src.MinZoom, src.MaxZoom = minZoom, maxZoom
			v.SetLayerZoomLimits(src.MinZoom, src.MaxZoom, true, true)
			assert.Equal(t, maxZoom, v.MaxZoom())
			assert.Equal(t, minZoom, v.MinZoom())
		})
	})

	t.Run("when a gridlayer is added to a map that already has a gridlayer", func(t *testing.T) {
		t.Run("has its zoomlevels updated to fit the new layer", func(t *testing.T) {
			v := newMap()
			layers := zoomLimitLayers{v: v}
			layers.add(10, 15)
			assert.Equal(t, 10.0, v.MinZoom())
			assert.Equal(t, 15.0, v.MaxZoom())

			layers.add(5, 10)
			assert.Equal(t, 5.0, v.MinZoom())  // changed
			assert.Equal(t, 15.0, v.MaxZoom()) // unchanged

			layers.add(10, 20)
			assert.Equal(t, 5.0, v.MinZoom())  // unchanged
			assert.Equal(t, 20.0, v.MaxZoom()) // changed

			layers.add(0, 25)
			assert.Equal(t, 0.0, v.MinZoom())  // changed
			assert.Equal(t, 25.0, v.MaxZoom()) // changed
		})
	})

	t.Run("when a gridlayer is removed from a map", func(t *testing.T) {
		t.Run("has its zoomlevels updated to only fit the layers it currently has", func(t *testing.T) {
			v := newMap()
			layers := zoomLimitLayers{v: v}
			tiles := []int{
				layers.add(10, 15),
				layers.add(5, 10),
				layers.add(10, 20),
				layers.add(0, 25),
			}
			assert.Equal(t, 0.0, v.MinZoom())
			assert.Equal(t, 25.0, v.MaxZoom())

			layers.remove(tiles[0])
			assert.Equal(t, 0.0, v.MinZoom())
			assert.Equal(t, 25.0, v.MaxZoom())

			layers.remove(tiles[3])
			assert.Equal(t, 5.0, v.MinZoom())
			assert.Equal(t, 20.0, v.MaxZoom())

			layers.remove(tiles[2])
			assert.Equal(t, 5.0, v.MinZoom())
			assert.Equal(t, 10.0, v.MaxZoom())

			layers.remove(tiles[1])
			assert.Equal(t, 0.0, v.MinZoom())
			assert.Equal(t, math.Inf(1), v.MaxZoom())
		})
	})
}

func TestGridLayer_MinMaxNativeZoomOption(t *testing.T) {
	t.Run("calls createTile() with maxNativeZoom when map zoom is larger", func(t *testing.T) {
		src := gridSource()
		src.HasMaxNativeZoom, src.MaxNativeZoom = true, 5
		g := newGridTest(t, Pt(800, 600), src)
		g.v.SetView(LL(0, 0), 10)
		g.addLayer()
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		require.NotEmpty(t, g.created, "No tiles loaded")
		for _, c := range g.created {
			assert.Equal(t, 5, c.Z)
		}
	})

	t.Run("calls createTile() with minNativeZoom when map zoom is smaller", func(t *testing.T) {
		src := gridSource()
		src.HasMinNativeZoom, src.MinNativeZoom = true, 5
		g := newGridTest(t, Pt(800, 600), src)
		g.v.SetView(LL(0, 0), 3)
		g.addLayer()
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		require.NotEmpty(t, g.created, "No tiles loaded")
		for _, c := range g.created {
			assert.Equal(t, 5, c.Z)
		}
	})

	t.Run("redraws tiles properly after changing maxNativeZoom", func(t *testing.T) {
		const initialZoom = 12
		g := newGridTest(t, Pt(800, 600), gridSource())
		g.v.SetView(LL(0, 0), initialZoom)
		g.addLayer()
		assert.Equal(t, initialZoom, g.p.zoom) // grid._tileZoom

		// grid.options.maxNativeZoom = 11; grid.redraw(): the pyramid has no
		// redraw of its own yet, so drop every tile and pick the level again.
		g.p.src.HasMaxNativeZoom, g.p.src.MaxNativeZoom = true, 11
		g.p.Reset()
		g.p.SetView(g.v, false, false)
		assert.Equal(t, 11, g.p.zoom)
	})
}

func TestGridLayer_TilesLoadedInSynchronousNonAnimatedGrid(t *testing.T) {
	// 256 px tiles @800x600px; fadeAnimation and zoomAnimation off.
	newGrid := func(t *testing.T) *gridTest {
		g := newGridTest(t, Pt(800, 600), gridSource())
		g.p.SetFade(false)
		return g
	}

	t.Run("Loads 8 tiles zoom 1", func(t *testing.T) {
		g := newGrid(t)
		g.setView(LL(0, 0), 1)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		g.expectCounts(8, 8, 0)
	})

	t.Run("Loads 5 tiles zoom 0", func(t *testing.T) {
		g := newGrid(t)
		g.setView(LL(0, 0), 0)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		g.expectCounts(5, 5, 0)
	})

	t.Run("Loads 16 tiles zoom 10", func(t *testing.T) {
		g := newGrid(t)
		g.setView(LL(0, 0), 10)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		g.expectCounts(16, 16, 0)
	})

	// zoomSeries is the three zoom its: 16 tiles at `from`, then a hard zoom
	// to `to` loads 16 more and unloads the first 16 — upstream all at once
	// through viewprereset's _invalidateAll, here the ones the new level
	// cannot keep at the zoom and the rest as the new tiles cover them.
	zoomSeries := func(t *testing.T, from, to float64) {
		g := newGrid(t)
		g.setView(LL(0, 0), from)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		g.expectCounts(16, 16, 0)

		g.setZoom(to)
		g.deliver()
		require.Equal(t, 2, g.p.Stats().Load)
		g.expectCounts(32, 32, 16)
	}

	t.Run("Loads 32, unloads 16 tiles zooming in 10-11", func(t *testing.T) { zoomSeries(t, 10, 11) })
	t.Run("Loads 32, unloads 16 tiles zooming out 11-10", func(t *testing.T) { zoomSeries(t, 11, 10) })
	t.Run("Loads 32, unloads 16 tiles zooming out 18-10", func(t *testing.T) { zoomSeries(t, 18, 10) })
}

func TestGridLayer_TilesLoadedInSynchronousAnimatedGrid(t *testing.T) {
	// 256 px tiles @800x600px; the fade on. Upstream also animates the zoom,
	// which the port does not yet (M3): see the file comment.
	newGrid := func(t *testing.T) *gridTest {
		g := newGridTest(t, Pt(800, 600), gridSource())
		g.p.SetFade(true)
		return g
	}

	t.Run("Loads 32, unloads 16 tiles zooming in 10-11", func(t *testing.T) {
		g := newGrid(t)
		g.advance(time.Millisecond)
		g.setView(LL(0, 0), 10)
		// The first setView is not animated, so it starts loading at once:
		// 16 tiles from z10.
		assert.Equal(t, 16, g.p.Stats().TileLoadStart)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		g.expectCounts(16, 16, 0)

		// A frame lets _updateOpacity start; 300 ms later the z10 tiles are
		// opaque, and the prune the load scheduled has run with nothing to
		// prune.
		g.tick()
		g.advance(300 * time.Millisecond)
		g.tick()
		g.expectCounts(16, 16, 0)

		// Zoom in: 16 tiles from z11, total 32. Upstream keeps every z10 tile
		// until its zoom animation ends and only then prunes the 12 that are
		// not parents of the new tiles; the port prunes them at the zoom.
		g.setZoom(11)
		g.expectCounts(32, 16, 12)
		g.deliver()
		require.Equal(t, 2, g.p.Stats().Load)
		g.expectCounts(32, 32, 12)

		// The 4 z10 tiles under the new level stay until the z11 tiles are
		// opaque and the prune after the load runs.
		g.tick()
		g.expectCounts(32, 32, 12)
		g.advance(300 * time.Millisecond)
		g.tick()
		g.expectCounts(32, 32, 16)
		assert.False(t, g.tick(), "nothing left to fade or prune")
	})

	t.Run("Loads 32, unloads 16 tiles zooming in 10-18", func(t *testing.T) {
		g := newGrid(t)
		g.setView(LL(0, 0), 10)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		g.expectCounts(16, 16, 0)

		// Eight levels up no z10 tile is an ancestor the new level keeps, so
		// all 16 go at the zoom (upstream: in the frame after the load).
		g.setZoom(18)
		g.deliver()
		require.Equal(t, 2, g.p.Stats().Load)
		g.expectCounts(32, 32, 16)
	})

	t.Run("Loads 32, unloads 16 tiles zooming out 11-10", func(t *testing.T) {
		g := newGrid(t)
		g.advance(time.Millisecond)
		g.setView(LL(0, 0), 11)
		assert.Equal(t, 16, g.p.Stats().TileLoadStart)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		g.expectCounts(16, 16, 0)
		g.tick()
		g.advance(300 * time.Millisecond)
		g.tick()

		// Zoom out: upstream requests the 4 z10 tiles under the z11 extent
		// when its animation starts and the other 12 when it ends; the port
		// requests all 16 at the zoom. Nothing is unloaded yet: every z11
		// tile is a child of a z10 tile still loading.
		g.setZoom(10)
		g.expectCounts(32, 16, 0)
		g.deliver()
		require.Equal(t, 2, g.p.Stats().Load)
		g.expectCounts(32, 32, 0)

		// Once the z10 tiles are opaque, the 16 z11 tiles they cover go.
		g.tick()
		g.expectCounts(32, 32, 0)
		g.advance(300 * time.Millisecond)
		g.tick()
		g.expectCounts(32, 32, 16)
	})

	t.Run("Loads 32, unloads 16 tiles zooming out 18-10", func(t *testing.T) {
		g := newGrid(t)
		g.setView(LL(0, 0), 18)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		g.expectCounts(16, 16, 0)

		// Eight levels down no z18 tile is a descendant the new level keeps,
		// so all 16 go at the zoom (upstream: in the frame after the load).
		g.setZoom(10)
		g.deliver()
		require.Equal(t, 2, g.p.Stats().Load)
		g.expectCounts(32, 32, 16)
	})
}

func TestGridLayer_ConfigurableTilePruning(t *testing.T) {
	// 256 px tiles @800x600px; fadeAnimation and zoomAnimation off.
	newGrid := func(t *testing.T, keepBuffer int) *gridTest {
		src := gridSource()
		src.KeepBuffer = keepBuffer
		g := newGridTest(t, Pt(800, 600), src)
		g.p.SetFade(false)
		return g
	}

	t.Run("Loads map, moves forth by 512 px, keepBuffer = 0", func(t *testing.T) {
		g := newGrid(t, 0)
		// 800px width * 600px height => 4 tiles horizontally * 4 tiles vertically = 16 tiles
		g.setView(LL(0, 0), 10)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		g.expectCounts(16, 16, 0)

		// Move by 512 px each way => 2 tile rows and 2 tile columns: 12 new
		// tiles, total 16 + 12 = 28, and the 12 old tiles outside the new
		// (unbuffered) range are pruned — upstream in the frame after the
		// load, here at the move.
		g.panBy(Pt(512, 512))
		g.deliver()
		require.Equal(t, 2, g.p.Stats().Load)
		g.expectCounts(28, 28, 12)
	})

	t.Run("Loads map, moves forth and back by 512 px, keepBuffer = 0", func(t *testing.T) {
		g := newGrid(t, 0)
		g.setView(LL(0, 0), 10)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		g.expectCounts(16, 16, 0)

		g.panBy(Pt(512, 512))
		g.deliver()
		require.Equal(t, 2, g.p.Stats().Load)
		g.expectCounts(28, 28, 12)

		g.panBy(Pt(-512, -512))
		g.deliver()
		require.Equal(t, 3, g.p.Stats().Load)
		g.expectCounts(40, 40, 24)
	})

	t.Run("Loads map, moves forth and back by 512 px, default keepBuffer", func(t *testing.T) {
		g := newGrid(t, 2)
		g.setView(LL(0, 0), 10)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		g.expectCounts(16, 16, 0)

		g.panBy(Pt(512, 512))
		g.deliver()
		require.Equal(t, 2, g.p.Stats().Load)
		g.expectCounts(28, 28, 0)

		// Back within the buffer: every tile is still there, nothing loads.
		loads := g.p.Stats().Load
		g.panBy(Pt(-512, -512))
		g.deliver()
		assert.Equal(t, loads, g.p.Stats().Load, "load")
		g.expectCounts(28, 28, 0)
	})
}

func TestGridLayer_NoWrapOption(t *testing.T) {
	t.Run("When false, uses same coords at zoom 0 for all tiles", func(t *testing.T) {
		src := gridSource()
		src.NoWrap = false
		g := newGridTest(t, Pt(800, 600), src)
		g.setView(LL(0, 0), 0)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		assert.Equal(t, []string{"0:0:0", "0:0:0", "0:0:0", "0:0:0", "0:0:0"}, g.createdKeys())
	})

	t.Run("When true, uses different coords at zoom level 0 for all tiles", func(t *testing.T) {
		src := gridSource()
		src.NoWrap = true
		g := newGridTest(t, Pt(800, 600), src)
		g.setView(LL(0, 0), 0)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		assert.Equal(t, []string{"0:0:0", "-1:0:0", "1:0:0", "-2:0:0", "2:0:0"}, g.createdKeys())
	})

	t.Run("When true and with bounds, loads just one tile at zoom level 0", func(t *testing.T) {
		src := gridSource()
		src.NoWrap = true
		src.Bounds = LatLngBoundsOf(LL(-90, -180), LL(90, 180))
		g := newGridTest(t, Pt(800, 600), src)
		g.setView(LL(0, 0), 0)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		assert.Equal(t, []string{"0:0:0"}, g.createdKeys())
	})
}

func TestGridLayer_SanityChecksForInfinity(t *testing.T) {
	// Upstream's throw is Point's own ("Invalid Point object: (NaN, NaN)" —
	// the map has no zoom yet); here the view takes the infinite centre and
	// the pyramid refuses the infinite tile range.
	const infinite = "portolan: attempted to load an infinite number of tiles"

	t.Run("Throws error on map center at plus Infinity longitude", func(t *testing.T) {
		g := newGridTest(t, Pt(800, 600), gridSource())
		assert.PanicsWithValue(t, infinite, func() {
			g.v.PanTo(LL(math.Inf(1), math.Inf(1)))
			g.sync()
		})
	})

	t.Run("Throws error on map center at minus Infinity longitude", func(t *testing.T) {
		g := newGridTest(t, Pt(800, 600), gridSource())
		assert.PanicsWithValue(t, infinite, func() {
			g.v.PanTo(LL(math.Inf(-1), math.Inf(-1)))
			g.sync()
		})
	})
}

func TestGridLayer_DoesNotCallGetZoomScaleWithNullAfterInvalidateAll(t *testing.T) {
	// "doesn't call map's getZoomScale method with null after _invalidateAll
	// method was called": after _invalidateAll the grid has no level and a
	// redraw must not project with an undefined zoom. Here Reset forgets the
	// level, Update is then a no-op, and SetView starts over.
	g := newGridTest(t, Pt(800, 600), gridSource())
	g.setView(LL(0, 0), 0)
	g.deliver()
	require.Equal(t, 5, g.p.TileCount())

	g.p.Reset()
	assert.Equal(t, 0, g.p.TileCount())
	assert.Empty(t, g.p.Levels())
	g.created = nil
	g.p.Update(g.v)
	assert.Empty(t, g.created, "no level to update")
	g.p.SetView(g.v, false, false)
	assert.Len(t, g.created, 5)
}
