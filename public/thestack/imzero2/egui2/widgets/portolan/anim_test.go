// Port of the animation `it`s of Leaflet's spec/suites/map/MapSpec.js and
// spec/suites/layer/tile/GridLayerSpec.js at upstream commit
// c96f31a7a350a07cfbc852cf88e6ca69af5f5ec9, against the view's animations
// (anim.go) and the pyramid's handling of them (pyramid.go's Sync). The
// non-animated `it`s of the same describes live in view_test.go and
// pyramid_test.go; each upstream `describe` is a Test function here as there
// — suffixed _Animated where the describe already has one — and each `it` a
// subtest named by its upstream title.
//
// Conventions of the port:
//   - Time is the test's: SetClock before a call that starts an animation,
//     then Tick per frame. Where the spec awaits zoomend/moveend or ticks a
//     fake clock, the frames run past the animation's end; intermediate
//     states are asserted only where the spec asserts them. A frame is 16 ms
//     (the browser's requestAnimationFrame) in the map cases and 40 ms (the
//     spec's runFrames) in the grid's flyTo.
//   - `once('zoomend', …)` becomes the ZoomEnd flag of the frame that ended
//     the animation; "call zoomend once" is the count of frames that set it.
//   - Leaflet fires the upstream Map's events to listeners; here they are the
//     ViewEvents flags of each frame's TakeEvents, and the grid's counts are
//     PyramidStats, as in pyramid_test.go, whose gridTest harness these tests
//     reuse.
//
// Ported with an analogue rather than literally:
//   - #stop (all three): upstream replaces map.stop with a sinon spy and
//     asserts whether setView/flyTo/panTo called it — the method-call
//     bookkeeping has no form here. Each is ported as the behaviour its
//     title names: Stop on an idle view is a no-op, Stop during a fly or a
//     pan ends it where it is.
//   - #setZoom › when the map has been loaded › "does not overwrite zoom
//     passed as map option": the zoom stays because the animated zoom is
//     pending (view_test.go left it for this reason); here the pending
//     animation is the port's.
//   - #flyToBounds "doesn't fly if already in bounds": upstream's map has no
//     zoom (setView without one on an unloaded map), which degenerates its
//     fly to a jump; under view_test.go's zoom-0 convention the port flies,
//     and the first frame leaves the centre where the spec reads it.
//   - GridLayerSpec's animated grid: the counts upstream reaches through
//     its CSS zoom animation's window are reached here through the port's
//     zoom animation (ZoomAnimStart → Sync loads the target level under
//     noPrune, the zoom's end prunes). Where the port's mechanism differs —
//     the pyramid's tiledPixelBounds has no zoom-animation branch, the prune
//     after a load is scheduled at arrival — the test asserts what the port
//     does and says why at the assertion. The MAD-TRD flyTo reproduces the
//     spec's totals exactly under GridLayer's wiring at this commit (no
//     per-move update: its updateWhenIdle default is inverted against its
//     own doc comment), and pins the widget's own wiring (an update on
//     every frame's Move) beside it.
//
// Not ported from upstream:
//   - #remove "does not throw if removed during animation", "does not throw
//     if removed before transition end complete": map.remove, the animation
//     proxy element and transitionend — DOM.
//   - #flyToBounds "throws if map is not set before", "throws if passed
//     invalid bounds": throws have no Go form (FlyToBounds takes a
//     LatLngBounds value and, unlike FitBoundsAnimated, returns no error).

package portolan

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// animSpecFrame is a browser's animation frame.
const animSpecFrame = 16 * time.Millisecond

// animSpecT0 is the clock animations start at; away from the zero time, which
// the view reads as "no clock".
var animSpecT0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// animSpecRun steps the view's animations one frame at a time from start
// until Tick reports nothing running, returning every frame's events (the
// first frame's include whatever the call that started the animation
// recorded). It fails the test if the animation outlives maxFrames.
func animSpecRun(t *testing.T, v *View, start time.Time, maxFrames int) (frames []ViewEvents) {
	t.Helper()
	for i := 1; i <= maxFrames; i++ {
		running := v.Tick(start.Add(time.Duration(i) * animSpecFrame))
		frames = append(frames, v.TakeEvents())
		if !running {
			return frames
		}
	}
	t.Fatalf("animation still running after %d frames", maxFrames)
	return frames
}

// animSpecCount is how many frames satisfy pick.
func animSpecCount(frames []ViewEvents, pick func(ViewEvents) bool) (n int) {
	for _, f := range frames {
		if pick(f) {
			n++
		}
	}
	return
}

func animSpecZoomEnd(e ViewEvents) bool { return e.ZoomEnd }
func animSpecMoveEnd(e ViewEvents) bool { return e.MoveEnd }

// describe('#setView') — the `it`s with pan/zoom options; the rest are in
// view_test.go.

func TestMap_SetView_Animated(t *testing.T) {
	t.Run("passes duration option to panBy", func(t *testing.T) {
		// new LeafletMap(div, {zoom: 13, center: [0, 0]}): a detached 0×0
		// div whose constructor setView is a hard reset.
		v := NewView(ViewOptions{ZoomSnap: 1})
		v.SetView(LL(0, 0), 13)
		v.TakeEvents()
		v.SetClock(animSpecT0)
		v.SetViewAnimated(LL(51.605, -0.11), 13, AnimateOptions{Animate: AnimateYes, Duration: 13 * time.Second})
		// Upstream spies panBy and reads duration: 13 off its options; here
		// the pan animation itself runs for the 13 s.
		require.True(t, v.Animating())
		assert.True(t, v.Tick(animSpecT0.Add(13*time.Second-time.Millisecond)), "still panning just before 13 s")
		assert.False(t, v.Tick(animSpecT0.Add(13*time.Second)), "done at 13 s")
		assert.Equal(t, 13.0, v.Zoom())
		// The pan's offset is truncated to whole pixels, so the centre lands
		// within a pixel of the target.
		assert.Less(t, v.Center().DistanceTo(LL(51.605, -0.11)), 5.0)
	})

	t.Run("prevents firing movestart noMoveStart", func(t *testing.T) {
		// setView(center, zoom, {pan: {noMoveStart: true}}) on an unloaded
		// map is a hard reset without movestart; view_test.go reaches the
		// same through resetView, this is the option's public form.
		v := specView()
		v.SetViewAnimated(LL(51.505, -0.09), 13, AnimateOptions{NoMoveStart: true})
		e := v.TakeEvents()
		assert.True(t, e.MoveEnd)
		assert.False(t, e.MoveStart)
	})
}

// describe('#setZoom') › when the map has been loaded — the `it` whose
// expectation rests on the animated zoom; the rest are in view_test.go.

func TestMap_SetZoom_Animated(t *testing.T) {
	t.Run("when the map has been loaded", func(t *testing.T) {
		t.Run("does not overwrite zoom passed as map option", func(t *testing.T) {
			// new LeafletMap(div, {zoom: 13}); setView([0, 0]); setZoom(15):
			// the zoom is still 13 because the two-level zoom animates, and
			// the animation has not had its first frame.
			v := NewView(ViewOptions{ZoomSnap: 1})
			v.SetZoom(13)
			v.PanTo(LL(0, 0))
			v.SetClock(animSpecT0)
			v.SetZoomAnimated(15, AnimateOptions{})
			assert.Equal(t, 13.0, v.Zoom())
			assert.True(t, v.AnimatingZoom())
			// Once the animation has run its 250 ms, the zoom is 15.
			animSpecRun(t, v, animSpecT0, 32)
			assert.Equal(t, 15.0, v.Zoom())
		})
	})
}

// describe('#stop')

func TestMap_Stop(t *testing.T) {
	t.Run("does not try to stop the animation if it wasn't set before", func(t *testing.T) {
		// Upstream: setView's _stop does not reach the public stop (a spy).
		// Here: with nothing running, Stop records nothing and moves
		// nothing, and the panTo that follows is an ordinary view change.
		v := specView()
		v.SetView(LL(50, 50), 10)
		v.TakeEvents()
		v.SetClock(animSpecT0)
		v.Stop()
		assert.Equal(t, ViewEvents{}, v.TakeEvents())
		assert.Equal(t, LL(50, 50), v.Center())
		assert.False(t, v.Animating())
		// (10, 10) is farther than the viewport: a hard reset, not a pan.
		v.PanToAnimated(LL(10, 10), AnimateOptions{})
		assert.False(t, v.Animating())
		assert.Less(t, v.Center().DistanceTo(LL(10, 10)), 5.0)
		assert.True(t, v.TakeEvents().ViewReset)
	})

	t.Run("stops the execution of the flyTo animation", func(t *testing.T) {
		v := specView()
		v.PanTo(LL(0, 0)) // setView([0, 0]): zoom 0
		v.TakeEvents()
		v.SetClock(animSpecT0)
		// flyTo(latlng) with no zoom flies at the current zoom: the
		// trajectory dips below it and comes back.
		v.FlyTo(LL(51.505, -0.09), v.Zoom(), FlyOptions{})
		require.True(t, v.Animating())
		// Part way along, stop: the fly ends where it is — Leaflet's _stop
		// cancels the fly's next frame — with the end of the zoom and the
		// move it was.
		v.Tick(animSpecT0.Add(4 * animSpecFrame))
		v.TakeEvents()
		midway := v.Center()
		require.True(t, midway.Lat > 0 && midway.Lat < 51.505, "mid-flight: %v", midway)
		require.Less(t, v.Zoom(), 0.0, "mid-flight the zoom is below the start zoom")
		v.Stop()
		assert.False(t, v.Animating())
		assert.Equal(t, midway, v.Center())
		e := v.TakeEvents()
		assert.True(t, e.Zoom && e.Move && e.ZoomEnd && e.MoveEnd, "%+v", e)
		// Nothing runs on.
		assert.False(t, v.Tick(animSpecT0.Add(time.Second)))
		assert.Equal(t, midway, v.Center())
		assert.Equal(t, ViewEvents{}, v.TakeEvents())
	})

	t.Run("stops the execution of the panTo animation", func(t *testing.T) {
		v := specView()
		v.PanTo(LL(0, 0)) // setView([0, 0]): zoom 0
		v.TakeEvents()
		v.SetClock(animSpecT0)
		v.PanToAnimated(LL(51.505, -0.09), AnimateOptions{})
		require.True(t, v.Animating(), "a pan of less than the viewport animates")
		// Half way through the pan, stop: the pan ends where it is
		// (PosAnimation's stop), with moveend.
		v.Tick(animSpecT0.Add(125 * time.Millisecond))
		v.TakeEvents()
		midway := v.Center()
		assert.NotEqual(t, LL(0, 0), midway)
		v.Stop()
		assert.False(t, v.Animating())
		assert.Equal(t, midway, v.Center())
		e := v.TakeEvents()
		assert.True(t, e.MoveEnd)
		assert.False(t, e.ZoomEnd)
		// Nothing runs on.
		assert.False(t, v.Tick(animSpecT0.Add(time.Second)))
		assert.Equal(t, midway, v.Center())
		assert.Equal(t, ViewEvents{}, v.TakeEvents())
	})
}

// describe('#flyTo')

func TestMap_FlyTo(t *testing.T) {
	// An 800×600 container (visibility hidden, which changes nothing here).
	newMap := func() *View { return specViewSized(800, 600) }

	t.Run("move to requested center and zoom, and call zoomend once", func(t *testing.T) {
		newCenter, newZoom := LL(10, 11), 12.0
		v := newMap()
		v.SetView(LL(0, 0), 0)
		v.TakeEvents()
		v.SetClock(animSpecT0)
		v.FlyTo(newCenter, newZoom, FlyOptions{Duration: 100 * time.Millisecond})
		frames := animSpecRun(t, v, animSpecT0, 32)
		assert.Equal(t, 1, animSpecCount(frames, animSpecZoomEnd), "zoomend once")
		assert.Equal(t, newCenter, v.Center())
		assert.Equal(t, newZoom, v.Zoom())
		// The frame that ended the fly carries the end of the move too.
		last := frames[len(frames)-1]
		assert.True(t, last.ZoomEnd && last.MoveEnd && last.Move && last.Zoom)
		// The frames before it are the trajectory: move and zoom every
		// frame, marked as animating, and no end.
		for i, f := range frames[:len(frames)-1] {
			assert.True(t, f.Move && f.Zoom && f.Animating, "frame %d: %+v", i, f)
			assert.False(t, f.MoveEnd || f.ZoomEnd, "frame %d: %+v", i, f)
		}
	})

	t.Run("flyTo start latlng == end latlng", func(t *testing.T) {
		dc := LL(38.91, -77.04)
		v := newMap()
		v.SetView(dc, 14)
		v.TakeEvents()
		v.SetClock(animSpecT0)
		v.FlyTo(dc, 4, FlyOptions{Duration: 100 * time.Millisecond})
		frames := animSpecRun(t, v, animSpecT0, 32)
		assert.Equal(t, 1, animSpecCount(frames, animSpecZoomEnd))
		assert.Equal(t, dc, v.Center())
		assert.Equal(t, 4.0, v.Zoom())
	})

	t.Run("flyTo should honour maxZoom", func(t *testing.T) {
		newCenter, maxZoom := LL(10, 11), 20.0
		v := specViewOpts(ViewOptions{Size: Pt(800, 600), MaxZoom: maxZoom, HasMaxZoom: true})
		v.SetView(LL(0, 0), 0)
		v.TakeEvents()
		v.SetClock(animSpecT0)
		v.FlyTo(newCenter, 22, FlyOptions{Animate: AnimateYes, Duration: 100 * time.Millisecond})
		frames := animSpecRun(t, v, animSpecT0, 32)
		assert.Equal(t, 1, animSpecCount(frames, animSpecZoomEnd))
		assert.Equal(t, newCenter, v.Center())
		assert.Equal(t, maxZoom, v.Zoom())
	})

	t.Run("should handle parameters leading to Math.log(sq) issue", func(t *testing.T) {
		// 1024×1024, visible. The guard in flyTo's r(i) — sq below 1.5e-8
		// reads as log −18 rather than −Inf — keeps the trajectory finite
		// and the fly from never ending; the port's guard is the same, and
		// the fly must end within its own duration.
		v := specViewSized(1024, 1024)
		coordinatesA, coordinatesB := LL(59.0009, 60.0), LL(59, 60.02)
		bounds := LatLngBoundsOf(coordinatesA, coordinatesB)
		center := bounds.GetCenter()
		require.NoError(t, v.FitBounds(bounds, FitOptions{}))
		v.TakeEvents()
		v.SetClock(animSpecT0)
		v.FlyTo(center, 11, FlyOptions{Animate: AnimateYes})
		// The trajectory's own duration (0.8 s per unit of S) is a few
		// seconds; 2000 frames is 32 s.
		frames := animSpecRun(t, v, animSpecT0, 2000)
		assert.Equal(t, 1, animSpecCount(frames, animSpecZoomEnd))
		assert.Equal(t, center, v.Center())
		assert.Equal(t, 11.0, v.Zoom())
	})
}

// describe('#flyToBounds')

func TestMap_FlyToBounds(t *testing.T) {
	t.Run("doesn't fly if already in bounds", func(t *testing.T) {
		v := specView()
		v.PanTo(LL(0, 0)) // setView([0, 0]): zoom 0
		bounds := LatLngBoundsOf(LL(-1, -1), LL(1, 1))
		expectedCenter := LL(0, 0)
		v.SetClock(animSpecT0)
		v.FlyToBounds(bounds, FitOptions{}, FlyOptions{})
		// Upstream's map has no zoom here (see the file comment); the port's
		// fly — to the zoom that fits the bounds — has run its first frame,
		// which leaves the centre where it started, and that is what the
		// spec reads.
		assertNearLL(t, expectedCenter, v.Center(), 1e-4)
	})

	// flyToBounds with {animate: false} is setView: the bounds' centre and
	// zoom at once, with zoomend.
	flyCase := func(t *testing.T, bounds LatLngBounds, expectedCenter LatLng, tolerance float64) {
		t.Helper()
		v := specView()
		v.SetViewAnimated(LL(0, 0), 0, AnimateOptions{Animate: AnimateNo})
		v.TakeEvents()
		v.FlyToBounds(bounds, FitOptions{}, FlyOptions{Animate: AnimateNo})
		e := v.TakeEvents()
		require.True(t, e.ZoomEnd, "zoomend")
		assert.False(t, v.Animating())
		got := v.Bounds()
		assert.Less(t, math.Abs(got.GetEast()-bounds.GetEast()), tolerance)
		assert.Less(t, math.Abs(got.GetWest()-bounds.GetWest()), tolerance)
		assert.Less(t, math.Abs(got.GetNorth()-bounds.GetNorth()), tolerance)
		assert.Less(t, math.Abs(got.GetSouth()-bounds.GetSouth()), tolerance)
		assertNearLL(t, expectedCenter, v.Center(), 1e-4)
	}

	t.Run("flies to requested bounds and corresponding center (low zoom case)", func(t *testing.T) {
		flyCase(t, LatLngBoundsOf(LL(40, 10), LL(10, 40)), LL(25.9461, 25), 2.7)
	})

	t.Run("flies to requested bounds and corresponding center (middle zoom case)", func(t *testing.T) {
		flyCase(t, LatLngBoundsOf(LL(30, 20), LL(20, 30)), LL(25.102, 25), 4)
	})

	t.Run("flies to requested bounds and corresponding center (high zoom case)", func(t *testing.T) {
		flyCase(t, LatLngBoundsOf(LL(13, 12), LL(12, 13)), LL(12.5004, 12.5), 0.05)
	})
}

// ---- GridLayerSpec ---------------------------------------------------------

// animSpecGridFrame is one frame of the widget's loop on the grid harness:
// the view's animations step to the clock, the frame's events reach the
// pyramid. The tiles' arrival (deliver) and the pyramid's Tick are the
// caller's, as the spec sequences them.
func animSpecGridFrame(g *gridTest) {
	g.v.Tick(g.now)
	g.sync()
}

// animSpecGridZoom is map.setZoom(zoom, {animate: true}) on the grid harness:
// the view's clock is the grid's, and the frame's events — the start of the
// zoom animation — reach the pyramid at once.
func animSpecGridZoom(g *gridTest, zoom float64) {
	g.v.SetClock(g.now)
	g.v.SetZoomAnimated(zoom, AnimateOptions{Animate: AnimateYes})
	g.sync()
}

// describe('number of 256px tiles loaded in synchronous animated grid
// @800x600px') — with the zoom animation; pyramid_test.go holds the same
// describe driven by hard zooms (milestone M2), this one drives the port's
// zoom animation (M3) and asserts the spec's intermediate counts.

func TestGridLayer_TilesLoadedInSynchronousAnimatedGrid_Animated(t *testing.T) {
	newGrid := func(t *testing.T) *gridTest {
		g := newGridTest(t, Pt(800, 600), gridSource())
		g.p.SetFade(true)
		return g
	}

	t.Run("Loads 32, unloads 16 tiles zooming in 10-11", func(t *testing.T) {
		g := newGrid(t)
		// Advance the time to !== 0 otherwise `tile.loaded` timestamp will
		// appear to be falsy.
		g.advance(time.Millisecond)
		g.setView(LL(0, 0), 10)
		// The first setView does not animate, therefore it starts loading
		// tiles immediately: 16 tiles from z10.
		assert.Equal(t, 16, g.p.Stats().TileLoadStart)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		assert.Equal(t, 16, g.p.Stats().TileLoad)
		assert.Equal(t, 0, g.p.Stats().TileUnload)

		// A frame lets _updateOpacity start; > 250 ms later the z10 tiles
		// are opaque and the prune the load scheduled has run (nothing to
		// prune).
		g.tick()
		g.advance(300 * time.Millisecond)
		g.tick()

		// setZoom(11, {animate: true}): the zoom animation starts — upstream
		// a frame later, here at the call — and the pyramid loads the z11
		// level at once: 16 extra tiles, total 32.
		animSpecGridZoom(g, 11)
		assert.Equal(t, 32, g.p.Stats().TileLoadStart)
		assert.True(t, g.v.AnimatingZoom())

		// The z11 tiles arrive: one frame into the zoom animation, nothing
		// is unloaded (the level was loaded under noPrune).
		g.deliver()
		require.Equal(t, 2, g.p.Stats().Load)
		assert.Equal(t, 32, g.p.Stats().TileLoad)
		assert.Equal(t, 0, g.p.Stats().TileUnload)

		// > 250 ms later the zoom animation ends: the zoom event's SetView
		// prunes the 12 z10 tiles outside the new level, but not the 4
		// under it — the z11 tiles are not yet active, so their parents are
		// kept.
		g.advance(300 * time.Millisecond)
		animSpecGridFrame(g)
		assert.False(t, g.v.AnimatingZoom())
		assert.Equal(t, 12, g.p.Stats().TileUnload)

		// Upstream the remaining 4 go at 851 ms, when the prune _tileReady
		// scheduled after its load listeners (the spec's, which had moved
		// the clock by then) fires. Here TileReady schedules that prune at
		// arrival, 250 ms after the z11 tiles' load — the same instant the
		// zoom animation ends — so the pyramid's Tick of this very frame
		// runs it, after the fade has made the z11 tiles active: the 4 z10
		// tiles go now, and a further 300 ms changes nothing.
		g.tick()
		assert.Equal(t, 16, g.p.Stats().TileUnload)
		g.advance(300 * time.Millisecond)
		g.tick()
		assert.Equal(t, 16, g.p.Stats().TileUnload)
		assert.Equal(t, 32, g.p.Stats().TileLoadStart)
		assert.Equal(t, 32, g.p.Stats().TileLoad)
	})

	t.Run("Loads 32, unloads 16 tiles zooming in 10-18", func(t *testing.T) {
		g := newGrid(t)
		g.setView(LL(0, 0), 10)
		g.advance(250 * time.Millisecond)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		g.expectCounts(16, 16, 0)

		// setZoom(18, {animate: true}): eight levels is past the zoom
		// animation threshold of 4, so this zoom is hard, upstream as here;
		// upstream's viewprereset drops every z10 tile, the port's prune at
		// the zoom drops them too (none is an ancestor the new level keeps).
		animSpecGridZoom(g, 18)
		assert.False(t, g.v.AnimatingZoom())
		assert.Equal(t, 32, g.p.Stats().TileLoadStart)
		assert.Equal(t, 16, g.p.Stats().TileUnload)
		g.advance(250 * time.Millisecond)
		g.deliver()
		require.Equal(t, 2, g.p.Stats().Load)
		g.tick()
		g.expectCounts(32, 32, 16)
	})

	t.Run("Loads 32, unloads 16 tiles zooming out 11-10", func(t *testing.T) {
		g := newGrid(t)
		g.advance(time.Millisecond)
		g.setView(LL(0, 0), 11)
		// The first setView does not animate: 16 tiles from z11.
		assert.Equal(t, 16, g.p.Stats().TileLoadStart)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		assert.Equal(t, 16, g.p.Stats().TileLoad)
		assert.Equal(t, 0, g.p.Stats().TileUnload)
		g.tick()
		g.advance(300 * time.Millisecond)
		g.tick()

		// setZoom(10, {animate: true}). Upstream's _getTiledPixelBounds
		// sizes the request during a zoom animation to the larger of the
		// two zooms — at the beginning of a zoom-out that is the z11
		// viewport, 4 tiles of z10 (tileloadstart 20) — and requests the
		// other 12 when the animation ends. The port's tiledPixelBounds has
		// no zoom-animation branch: the z10 level is loaded for the whole
		// viewport at the start, all 16 at once (tileloadstart 32), 250 ms
		// before upstream asks for the last 12. The totals are upstream's.
		animSpecGridZoom(g, 10)
		assert.Equal(t, 32, g.p.Stats().TileLoadStart)
		g.deliver()
		require.Equal(t, 2, g.p.Stats().Load)
		assert.Equal(t, 32, g.p.Stats().TileLoad)
		// No tile is unloaded yet: every z11 tile is a child of a z10 tile
		// that is not yet active.
		assert.Equal(t, 0, g.p.Stats().TileUnload)

		// > 250 ms later the zoom animation ends; its prune finds the z10
		// tiles loaded but not yet active and keeps their z11 children —
		// nothing to prune yet, upstream as here.
		g.advance(300 * time.Millisecond)
		animSpecGridFrame(g)
		assert.False(t, g.v.AnimatingZoom())
		assert.Equal(t, 32, g.p.Stats().TileLoadStart)
		assert.Equal(t, 0, g.p.Stats().TileUnload)

		// Then the fade makes the z10 tiles active and the prune after their
		// load drops all 16 z11 tiles, now covered.
		g.tick()
		assert.Equal(t, 16, g.p.Stats().TileUnload)
		g.advance(300 * time.Millisecond)
		g.tick()
		g.expectCounts(32, 32, 16)
	})

	t.Run("Loads 32, unloads 16 tiles zooming out 18-10", func(t *testing.T) {
		g := newGrid(t)
		g.setView(LL(0, 0), 18)
		g.advance(250 * time.Millisecond)
		g.deliver()
		require.Equal(t, 1, g.p.Stats().Load)
		g.expectCounts(16, 16, 0)

		// Eight levels: a hard zoom, upstream as here (see zooming in
		// 10-18); no z18 tile is a descendant the new level keeps.
		animSpecGridZoom(g, 10)
		assert.False(t, g.v.AnimatingZoom())
		assert.Equal(t, 32, g.p.Stats().TileLoadStart)
		assert.Equal(t, 16, g.p.Stats().TileUnload)
		g.advance(250 * time.Millisecond)
		g.deliver()
		require.Equal(t, 2, g.p.Stats().Load)
		g.tick()
		g.expectCounts(32, 32, 16)
	})

	t.Run("Loads 224, unloads 209 tiles on MAD-TRD flyTo()", func(t *testing.T) {
		mad, trd := LL(40.40, -3.7), LL(63.41, 10.41)

		// fly is the spec's scenario on a fresh grid — keepBuffer 0, the
		// view on Madrid, flyTo Trondheim under runFrames (40 ms a frame,
		// up to 500) — and returns the counts at zoomend, where the spec
		// reads them, and the tiles then held. A frame is the widget's: the
		// fly steps, its events reach the pyramid, the tiles requested last
		// frame arrive (upstream's createTile is synchronous and _tileReady
		// the next animation frame), the pyramid's Tick fades and prunes.
		// perMoveUpdate says whether a frame's Move reaches the pyramid (the
		// widget's wiring) or only MoveEnd does (GridLayer's at this
		// commit, see below); the fly's level changes reach it either way.
		fly := func(t *testing.T, perMoveUpdate bool) (final PyramidStats, held int) {
			t.Helper()
			src := gridSource()
			src.KeepBuffer = 0
			g := newGridTest(t, Pt(800, 600), src)
			g.p.SetFade(true)
			g.setView(mad, 12)
			g.advance(250 * time.Millisecond)
			g.deliver()
			require.Equal(t, 1, g.p.Stats().Load)
			assert.Equal(t, PyramidStats{Loading: 1, Load: 1, TileLoadStart: 12, TileLoad: 12}, g.p.Stats())

			sync := func() ViewEvents {
				ev := g.v.TakeEvents()
				if !perMoveUpdate && !ev.MoveEnd {
					ev.Move = false
				}
				g.p.Sync(g.v, ev)
				return ev
			}
			g.v.SetClock(g.now)
			g.v.FlyTo(trd, 12, FlyOptions{Animate: AnimateYes})
			sync() // the fly's first frame, at the call
			for frame := 0; frame < 500; frame++ {
				arrived := g.pending
				g.pending = nil
				g.advance(40 * time.Millisecond)
				g.v.Tick(g.now)
				if sync().ZoomEnd {
					return g.p.Stats(), g.p.TileCount()
				}
				for _, c := range arrived {
					g.p.TileReady(g.v, c, false, g.now)
				}
				g.tick()
			}
			t.Fatal("the fly did not end within 500 frames")
			return
		}

		// GridLayer wires its (throttled) move handler only when
		// updateWhenIdle is false, and at this commit the option defaults to
		// !Browser.reducedMotion — true in the spec's browser, though its
		// doc comment says the default is false — so the spec's grid loads
		// tiles at the fly's level changes and at moveend only. Wired so,
		// the port reproduces the spec's totals exactly: the trajectory's
		// fourteen level changes request 224 tiles, all of which load, and
		// the prunes after each load and at the zoom's end unload 209; 15
		// are held at zoomend (the spec counts 15 tiles + the container).
		final, held := fly(t, false)
		assert.Equal(t, 224, final.TileLoadStart, "tileloadstart")
		assert.Equal(t, 224, final.TileLoad, "tileload")
		assert.Equal(t, 209, final.TileUnload, "tileunload")
		assert.Equal(t, 0, final.TileError, "tileerror")
		assert.Equal(t, 0, final.Abort)
		assert.Equal(t, 15, held)

		// The widget hands the pyramid every frame's Move — the documented
		// intent of updateWhenIdle: false, through GridLayer's 200 ms
		// throttle (Pyramid.updateInterval: the first move updates at once,
		// the rest of the interval coalesces into one update at its end) —
		// so it also requests the tiles of the viewports the fly passes
		// through between level changes: 77 more, of which 27 are dropped
		// unloaded at the next level change (TileLayer's abort, which the
		// spec's bare GridLayer does not have; upstream would load and then
		// unload them) and the other 50 load and are pruned. The same 15
		// are held at the end. Unthrottled, the same flight requested 104
		// more, 18 of them aborted.
		final, held = fly(t, true)
		assert.Equal(t, 301, final.TileLoadStart, "tileloadstart")
		assert.Equal(t, 274, final.TileLoad, "tileload")
		assert.Equal(t, 259, final.TileUnload, "tileunload")
		assert.Equal(t, 27, final.Abort, "tileabort")
		assert.Equal(t, 0, final.TileError, "tileerror")
		assert.Equal(t, 15, held)
	})
}
