// Port of Leaflet's spec/suites/map/handler/*Spec.js at upstream commit
// c96f31a7a350a07cfbc852cf88e6ca69af5f5ec9 — DragHandlerSpec,
// ScrollWheelZoomHandlerSpec, DoubleClickZoomHandlerSpec,
// PinchZoomHandlerSpec, BoxZoomHandlerSpec and KeyboardHandlerSpec — the
// `it`s whose subject is a handler's effect on the view, run against the
// state machines of handlers.go. Each upstream `describe` is a Test
// function, each `it` a subtest named by its upstream title.
//
// Conventions of the port:
//   - The spec's map is specView() (SpecHelper's 400×400 container and
//     Leaflet's default zoomSnap of 1); describes that size or configure
//     their map differently say so. `zoomAnimation: false` is
//     SetZoomAnimation(false).
//   - Upstream drives the handlers with synthetic pointer events
//     (prosthetic-hand, UIEventSimulator) and real timers. Here a test feeds
//     a handler the container-pixel samples those events would have carried,
//     on a fake clock (handlerSpecT0, advanced with SetClock and Tick the way
//     the widget's frame does), and steps the animations the view starts —
//     16 ms frames — to their end before asserting the settled view.
//   - A prosthetic-hand `moveBy(dx, dy, ms)` / `moveTo(x, y, ms)` is a
//     linear path sampled every frame (handlerSpecDragTo); two fingers'
//     distances become the multiplicative zoom factors the pinch handler
//     takes, at their midpoint (handlerSpecPinch).
//   - UIEventSimulator's wheel and dblclick land at client (0, 0), which is
//     container (0, 0); its `deltaY: ±120` at deltaMode 0 is ±120 wheel
//     pixels through DomEvent.getWheelDelta on the Chromium the suite runs
//     on (Linux Chrome's wheelPxFactor is devicePixelRatio, 1 headless).
//   - `on('zoomend' | 'moveend' | 'zoom' | …)` become assertions on the
//     TakeEvents flags; `drag`, `dragend`, `boxzoomstart` and `boxzoomend`
//     have no view event and are noted where they occur (the box's
//     existence is rect()).
//   - Where the spec only bounds a value (greaterThan / lessThan) the port
//     also pins the exact value its arithmetic gives — the wheel's sigmoid,
//     the anchoring — so a regression there shows.
//
// Not ported from upstream:
//   - DragHandler › #addHook › "calls the map with dragging enabled", "calls
//     the map with dragging and worldCopyJump enabled", "calls the map with
//     dragging disabled and worldCopyJump enabled; enables dragging after
//     setting center and zoom": the enable()/disable() handler API and
//     worldCopyJump; dragging is the widget's NoDragging.
//   - DragHandler › pointer events › in CSS scaled container › "change the
//     center of the map, compensating for CSS scale": CSS transforms; the
//     painter lane reports logical pixels.
//   - DragHandler › pointer events › "does not change the center of the map
//     when pointer is moved less than the drag threshold": Draggable's 3 px
//     clickTolerance; here egui decides when a press becomes a drag and the
//     widget feeds the handler from DragStarted on — the handler has no
//     threshold.
//   - DragHandler › pointer events › "does not trigger preclick nor click",
//     "does not trigger preclick nor click when dragging on top of a static
//     marker", "does not trigger preclick nor click when dragging a marker":
//     click suppression and markers.
//   - DragHandler › touch events › "change the center of the map", "does not
//     change the center of the map when finger is moved less than the drag
//     threshold", "reset itself after pointerup": touch-event plumbing (the
//     last is the pinch/drag pointer bookkeeping).
//   - ScrollWheelZoomHandler › "scrollWheelZoom: 'center'" and
//     DoubleClickZoomHandler › "doubleClickZoom: 'center'": the port has no
//     'center' option; the anchor is the caller's.
//   - DoubleClickZoomHandler › "can be disabled using doubleClickZoom:
//     false": the widget's NoDoubleClickZoom; nothing to call.
//   - PinchZoomHandler › "Layer is rendered correctly while pinch zoom when
//     zoomAnim is true", "… when zoomAnim is false": the SVG renderer's path
//     geometry.
//   - PinchZoomHandler › "disables pinchZoom when touchZoom is false
//     (backward compatibility)", "enables pinchZoom when touchZoom is true
//     and pinchZoom is false (touchZoom takes precedence)": the deprecated
//     option alias and its console warning.
//   - BoxZoomHandler › "_clearDeferredResetState": the deferred reset timer
//     that keeps the click after a box from firing.
//   - BoxZoomHandler › "cancel boxZoom by pressing ESC and re-enable click
//     event on the map" is ported for the box and the view; its click
//     assertions (the click before and after) are click plumbing.
//   - KeyboardHandler › arrow keys › "move the map over 180° with
//     worldCopyJump true": worldCopyJump.
//   - KeyboardHandler › plus/minus keys › "zoom in", "zoom out" and › does
//     not move the map if disabled › "no zoom in": the zoom keys need
//     keycodes this tree's vocabulary does not carry yet.
//   - KeyboardHandler › does not move the map if disabled › "no move north":
//     the widget's NoKeyboard; nothing to call.
//   - KeyboardHandler › popup closing › "closes a popup when pressing
//     escape", › popup closing disabled › "close of popup when pressing
//     escape disabled via options": popups.
//   - KeyboardHandler › keys events binding › "keypress", "keydown",
//     "keyup": the map's keyboard DOM events.
//
// Beyond upstream, which has no `it` for them: the inertia and the
// maxBoundsViscosity of src/map/handler/DragHandler.js, bounceAtZoomLimits
// of PinchZoomHandler.js, the shift / maxBounds / pan-in-progress rules of
// KeyboardHandler.js and the animated form of the wheel's and the
// double-click's zoom, with expectations worked from those sources; they
// are the Test…_ functions after each spec's.

package portolan

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// handlerSpecT0 is the fake clock's origin.
var handlerSpecT0 = time.Unix(1000, 0)

// handlerSpecFrame is the frame the view is ticked at and the hand's sample
// interval.
const handlerSpecFrame = 16 * time.Millisecond

// handlerSpecWheelNotch is UIEventSimulator's `deltaY: 120, deltaMode: 0`
// after getWheelDelta on the suite's browser — 120 wheel pixels; scrollIn
// is +120 (deltaY −120), scrollOut −120.
const handlerSpecWheelNotch = 120.0

// handlerSpecRun ticks the view frame by frame from `from` over d, ending
// exactly at from+d, and returns that time.
func handlerSpecRun(v *View, from time.Time, d time.Duration) time.Time {
	end := from.Add(d)
	for t := from.Add(handlerSpecFrame); t.Before(end); t = t.Add(handlerSpecFrame) {
		v.Tick(t)
	}
	v.Tick(end)
	return end
}

// handlerSpecDragTo is the hand's moveTo(x, y, ms) during a drag: the pointer
// goes from `from` to `to` over dur on a linear path, sampled every frame
// and at the end; the view is ticked before each sample as the widget's
// frame does. Returns the time of the last sample.
func handlerSpecDragTo(v *View, d *dragHandler, from, to Point, t0 time.Time, dur time.Duration, opts HandlerOptions) time.Time {
	if dur <= 0 {
		v.Tick(t0)
		d.move(v, to, t0, opts)
		return t0
	}
	t := t0
	for elapsed := handlerSpecFrame; ; elapsed += handlerSpecFrame {
		if elapsed > dur {
			elapsed = dur
		}
		f := float64(elapsed) / float64(dur)
		t = t0.Add(elapsed)
		v.Tick(t)
		d.move(v, from.Add(to.Subtract(from).MultiplyBy(f)), t, opts)
		if elapsed == dur {
			break
		}
	}
	return t
}

// handlerSpecPinch is a two-finger pinch whose fingers sit symmetric about
// anchor and whose distance goes from d0 to d1 over dur, linearly: the
// per-frame distance ratios are the factors egui would report. each, when
// not nil, runs after every step. Returns the time of the last step.
func handlerSpecPinch(v *View, p *pinchHandler, anchor Point, d0, d1 float64, dur time.Duration, t0 time.Time, opts HandlerOptions, each func()) time.Time {
	prev := d0
	t := t0
	for elapsed := handlerSpecFrame; ; elapsed += handlerSpecFrame {
		if elapsed > dur {
			elapsed = dur
		}
		f := float64(elapsed) / float64(dur)
		dist := d0 + (d1-d0)*f
		t = t0.Add(elapsed)
		v.Tick(t)
		p.step(v, dist/prev, anchor, t, opts)
		prev = dist
		if each != nil {
			each()
		}
		if elapsed == dur {
			break
		}
	}
	return t
}

// handlerSpecPaneOffset is the spec's map.getOffset(): how far the map pane
// has moved since load — here, how far the geography that was under the
// viewport centre has.
func handlerSpecPaneOffset(v *View, loadedCenter LatLng) Point {
	return v.LatLngToContainerPoint(loadedCenter).Subtract(v.Size().DivideBy(2))
}

// describe('DragHandler')
func TestDragHandler(t *testing.T) {
	// describe('pointer events') — every map here has inertia: false.
	opts := DefaultHandlerOptions()
	opts.NoInertia = true

	t.Run("change the center of the map", func(t *testing.T) {
		v := specView()
		v.SetView(LL(0, 0), 1)
		v.TakeEvents()
		v.SetClock(handlerSpecT0)

		start := Pt(200, 200)
		offset := Pt(256, 32)
		finish := start.Add(offset)

		// pointer.moveTo(start.x, start.y, 0).down().moveBy(5, 0, 20)
		//        .moveTo(finish.x, finish.y, 1000).up()
		var d dragHandler
		d.start(v, start, handlerSpecT0, opts)
		tm := handlerSpecDragTo(v, &d, start, start.Add(Pt(5, 0)), handlerSpecT0, 20*time.Millisecond, opts)
		tm = handlerSpecDragTo(v, &d, start.Add(Pt(5, 0)), finish, tm, time.Second, opts)
		d.end(v, tm, opts)

		assert.Equal(t, offset, handlerSpecPaneOffset(v, LL(0, 0)))
		assert.Equal(t, 1.0, v.Zoom())
		assertNearLatLng(t, LL(21.943045533, -180), v.Center())
		// dragstart/drag/dragend are movestart/move/moveend here.
		ev := v.TakeEvents()
		assert.True(t, ev.MoveStart && ev.Move && ev.MoveEnd, "movestart, move and moveend: %+v", ev)
		assert.False(t, ev.ZoomStart || ev.Zoom || ev.ZoomEnd, "no zoom events: %+v", ev)
	})

	t.Run("does not change the center of the map when drag is disabled on click", func(t *testing.T) {
		// Upstream disables dragging on pointerdown, so the press never becomes
		// a drag. Here that is the widget not calling start (NoDragging); the
		// moves and the release then reach a handler that was never started,
		// and must be no-ops.
		v := specView()
		originalCenter := LL(0, 0)
		v.SetView(originalCenter, 1)
		v.TakeEvents()
		v.SetClock(handlerSpecT0)

		// pointer.moveTo(200, 200, 0).down().moveBy(5, 0, 20).moveBy(256, 32, 200).up()
		var d dragHandler
		tm := handlerSpecDragTo(v, &d, Pt(200, 200), Pt(205, 200), handlerSpecT0, 20*time.Millisecond, opts)
		tm = handlerSpecDragTo(v, &d, Pt(205, 200), Pt(461, 232), tm, 200*time.Millisecond, opts)
		d.end(v, tm, opts)

		assert.Equal(t, 1.0, v.Zoom())
		// Expect center point to be the same as before the click
		assert.Equal(t, originalCenter, v.Center())
		assert.False(t, v.TakeEvents().Any(), "no drag event should have been fired")
	})
}

// DragHandler.js's inertia — _onDragEnd's speed from the samples of the
// last 50 ms, the deceleration over inertiaDeceleration and the eased pan
// that follows — has no upstream `it`; the expectations are worked from the
// source with the defaults (inertiaDeceleration 3400 px/s², easeLinearity
// 0.2, inertiaMaxSpeed ∞).
func TestDragHandler_Inertia(t *testing.T) {
	opts := DefaultHandlerOptions()
	newMap := func() *View {
		v := specView()
		v.SetView(LL(0, 0), 1)
		v.TakeEvents()
		v.SetClock(handlerSpecT0)
		return v
	}
	// flick drags at 1000 px/s along +x: 10 px every 10 ms for 100 ms, the
	// view ticked before every sample; returns the time of the last one. The
	// drag itself moves the map 100 px, at zoom 1 a centre of (0, −70.3125).
	flick := func(v *View, d *dragHandler) time.Time {
		d.start(v, Pt(200, 200), handlerSpecT0, opts)
		tm := handlerSpecT0
		for i := 1; i <= 10; i++ {
			tm = handlerSpecT0.Add(time.Duration(i) * 10 * time.Millisecond)
			v.Tick(tm)
			d.move(v, Pt(200+10*float64(i), 200), tm, opts)
		}
		return tm
	}
	// The window keeps the samples of the last 50 ms — (250,200) at 50 ms …
	// (300,200) at 100 ms: direction (50, 0) over 0.05 s, speed vector
	// (50,0)·(0.2/0.05) = (200, 0) px/s, deceleration duration 200/(3400·0.2)
	// = 0.294 s, offset (200,0)·(−0.294/2) rounded = (−29, 0): 29 more pixels
	// west, (0, −90.703125).
	decelerationDuration := func(speed float64) time.Duration {
		return time.Duration(speed / (3400 * 0.2) * float64(time.Second))
	}
	wantDuration := decelerationDuration(200)

	t.Run("continues the pan after release and decelerates to a stop", func(t *testing.T) {
		v := newMap()
		var d dragHandler
		tm := flick(v, &d)
		assertNearLatLng(t, LL(0, -70.3125), v.Center())
		v.TakeEvents()

		d.end(v, tm, opts)
		require.True(t, v.Animating(), "the inertia pan runs")
		ev := v.TakeEvents()
		assert.False(t, ev.MoveEnd, "moveend waits for the pan")
		assert.False(t, ev.MoveStart, "noMoveStart")

		// Half way the ease-out (1 − (1 − t)^5) has covered 97 % of the offset.
		mid := handlerSpecRun(v, tm, wantDuration/2)
		assert.True(t, v.Animating())
		assert.Less(t, v.Center().Lng, -70.3125)
		assert.Greater(t, v.Center().Lng, -90.703125)
		assert.False(t, v.TakeEvents().MoveEnd)

		handlerSpecRun(v, mid, wantDuration/2+handlerSpecFrame)
		assert.False(t, v.Animating())
		assertNearLatLng(t, LL(0, -90.703125), v.Center())
		ev = v.TakeEvents()
		assert.True(t, ev.MoveEnd, "moveend at the end of the pan")
		assert.False(t, ev.MoveStart)
	})

	t.Run("is capped at inertiaMaxSpeed", func(t *testing.T) {
		v := newMap()
		capped := opts
		capped.InertiaMaxSpeed = 100
		var d dragHandler
		d.start(v, Pt(200, 200), handlerSpecT0, capped)
		tm := handlerSpecT0
		for i := 1; i <= 10; i++ {
			tm = handlerSpecT0.Add(time.Duration(i) * 10 * time.Millisecond)
			v.Tick(tm)
			d.move(v, Pt(200+10*float64(i), 200), tm, capped)
		}
		d.end(v, tm, capped)
		// speed 100: deceleration duration 100/680 = 0.147 s, offset
		// (100,0)·(−0.147/2) = (−7.35, 0) → (−7, 0): 107 px west in all.
		require.True(t, v.Animating())
		handlerSpecRun(v, tm, decelerationDuration(100)+handlerSpecFrame)
		assert.False(t, v.Animating())
		assertNearLatLng(t, LL(0, -75.234375), v.Center())
	})

	t.Run("does not run when the pointer rested before release", func(t *testing.T) {
		v := newMap()
		var d dragHandler
		tm := flick(v, &d)
		v.TakeEvents()
		// 200 ms without a move: the window empties to one sample.
		rest := tm.Add(200 * time.Millisecond)
		v.Tick(rest)
		d.end(v, rest, opts)
		assert.False(t, v.Animating())
		assert.True(t, v.TakeEvents().MoveEnd)
		assertNearLatLng(t, LL(0, -70.3125), v.Center())
	})

	t.Run("does not run on a press and release", func(t *testing.T) {
		v := newMap()
		var d dragHandler
		d.start(v, Pt(200, 200), handlerSpecT0, opts)
		d.end(v, handlerSpecT0, opts)
		assert.False(t, v.Animating())
		ev := v.TakeEvents()
		assert.True(t, ev.MoveStart && ev.MoveEnd, "%+v", ev)
		assert.Equal(t, LL(0, 0), v.Center())
	})
}

// DragHandler.js's maxBoundsViscosity — _onPreDragLimit's viscous limit of
// the drag offset at maxBounds — has no upstream `it`. A 400×400 view at
// zoom 1 whose maxBounds are exactly its viewport: every drag offset is past
// the limit, which is 0, so the viscosity alone decides how far the map
// follows the pointer; moveend then pans back inside whatever it let
// through.
func TestDragHandler_MaxBoundsViscosity(t *testing.T) {
	for _, tc := range []struct {
		viscosity float64
		moved     Point
	}{
		{0, Pt(50, 50)},
		{0.5, Pt(25, 25)},
		{1, Pt(0, 0)},
	} {
		t.Run(fmt.Sprintf("maxBoundsViscosity %v", tc.viscosity), func(t *testing.T) {
			v := specView()
			v.SetView(LL(0, 0), 1)
			v.SetMaxBounds(v.Bounds())
			v.TakeEvents()
			v.SetClock(handlerSpecT0)
			opts := DefaultHandlerOptions()
			opts.NoInertia = true
			opts.MaxBoundsViscosity = tc.viscosity

			var d dragHandler
			d.start(v, Pt(200, 200), handlerSpecT0, opts)
			t1 := handlerSpecT0.Add(handlerSpecFrame)
			v.Tick(t1)
			d.move(v, Pt(250, 250), t1, opts)
			assert.Equal(t, tc.moved, handlerSpecPaneOffset(v, LL(0, 0)), "mid-drag")
			assert.Equal(t, tc.moved != Pt(0, 0), v.TakeEvents().Move, "a move event only when the map moved")

			t2 := t1.Add(handlerSpecFrame)
			v.Tick(t2)
			d.end(v, t2, opts)
			assert.True(t, v.TakeEvents().MoveEnd)
			assert.True(t, LL(0, 0).Equals(v.Center()), "back inside maxBounds, got %v", v.Center())
			assert.Equal(t, Pt(0, 0), handlerSpecPaneOffset(v, LL(0, 0)))
		})
	}
}

// describe('ScrollWheelZoomHandler')
func TestScrollWheelZoomHandler(t *testing.T) {
	// beforeEach: center [0, 0], zoom 3, zoomAnimation: false
	newMap := func() *View {
		v := specView()
		v.SetZoomAnimation(false)
		v.SetView(LL(0, 0), 3)
		v.TakeEvents()
		v.SetClock(handlerSpecT0)
		return v
	}
	opts := DefaultHandlerOptions()
	const scrollIn, scrollOut = handlerSpecWheelNotch, -handlerSpecWheelNotch
	at := Pt(0, 0) // UIEventSimulator's event lands at client (0, 0)

	t.Run("zooms out while firing 'wheel' event", func(t *testing.T) {
		v := newMap()
		zoom := v.Zoom()
		var w wheelHandler
		w.wheel(scrollOut, at, handlerSpecT0)
		w.tick(v, handlerSpecT0, opts)
		assert.Equal(t, zoom, v.Zoom(), "nothing before the debounce")
		w.tick(v, handlerSpecT0.Add(opts.WheelDebounce), opts)

		require.True(t, v.TakeEvents().ZoomEnd, "zoomend")
		assert.Less(t, v.Zoom(), zoom)
		// 120 px through the sigmoid is 1.26 levels, two with zoomSnap 1,
		// about (0, 0).
		assert.Equal(t, 1.0, v.Zoom())
		assertNearLatLng(t, LL(-71.96538769913127, 105.46875), v.Center())
	})

	t.Run("zooms in while firing 'wheel' event", func(t *testing.T) {
		v := newMap()
		zoom := v.Zoom()
		var w wheelHandler
		w.wheel(scrollIn, at, handlerSpecT0)
		w.tick(v, handlerSpecT0.Add(opts.WheelDebounce), opts)

		require.True(t, v.TakeEvents().ZoomEnd, "zoomend")
		assert.Greater(t, v.Zoom(), zoom)
		assert.Equal(t, 5.0, v.Zoom())
		assertNearLatLng(t, LL(25.48295117535531, -26.3671875), v.Center())
	})

	t.Run("changes the option 'wheelDebounceTime'", func(t *testing.T) {
		v := newMap()
		slow := opts
		slow.WheelDebounce = 100 * time.Millisecond
		zoom := v.Zoom()
		var w wheelHandler

		w.wheel(scrollIn, at, handlerSpecT0)
		// setTimeout(…, 50): a second notch, and no zoomend yet
		t50 := handlerSpecT0.Add(50 * time.Millisecond)
		w.tick(v, t50, slow)
		w.wheel(scrollIn, at, t50)
		assert.False(t, v.TakeEvents().ZoomEnd, "spy.notCalled")
		assert.Equal(t, zoom, v.Zoom())
		w.tick(v, handlerSpecT0.Add(99*time.Millisecond), slow)
		assert.False(t, v.TakeEvents().ZoomEnd)

		// The debounce runs from the first notch, not the last.
		w.tick(v, handlerSpecT0.Add(100*time.Millisecond), slow)
		require.True(t, v.TakeEvents().ZoomEnd, "spy.calledOnce")
		assert.Greater(t, v.Zoom(), zoom)
		// Both notches in one zoom: 240 px is 2.19 levels, three with zoomSnap 1.
		assert.Equal(t, 6.0, v.Zoom())
		assertNearLatLng(t, LL(29.38217507514529, -30.76171875), v.Center())
		w.tick(v, handlerSpecT0.Add(200*time.Millisecond), slow)
		assert.False(t, v.TakeEvents().ZoomEnd, "once")
		assert.Equal(t, 6.0, v.Zoom())
	})

	t.Run("changes the option 'wheelPxPerZoomLevel'", func(t *testing.T) {
		v := newMap()
		v.SetZoom(15) // map.setZoom(15, {animate: false})
		v.TakeEvents()
		zoom := v.Zoom()
		var w wheelHandler

		w.wheel(scrollIn, at, handlerSpecT0)
		w.tick(v, handlerSpecT0.Add(opts.WheelDebounce), opts)
		require.True(t, v.TakeEvents().ZoomEnd)
		assert.Greater(t, v.Zoom(), zoom)
		zoomDiff := v.Zoom() - zoom
		assert.Equal(t, 2.0, zoomDiff)

		v.SetZoom(zoom)
		assert.Equal(t, zoom, v.Zoom())
		v.TakeEvents()

		// wheelPxPerZoomLevel = 30 / getWheelPxFactor()
		fast := opts
		fast.WheelPxPerZoom = 30
		t1 := handlerSpecT0.Add(time.Second)
		w.wheel(scrollIn, at, t1)
		w.tick(v, t1.Add(fast.WheelDebounce), fast)
		require.True(t, v.TakeEvents().ZoomEnd)
		assert.Greater(t, v.Zoom(), zoom)
		assert.Greater(t, v.Zoom()-zoom, zoomDiff)
		assert.Equal(t, 3.0, v.Zoom()-zoom)
	})
}

// The wheel's zoom with the zoom animation on (Leaflet's default, which the
// spec turns off): the notch starts a 250 ms zoom animation about the pointer
// and the settled view is the spec's. No upstream `it`.
func TestScrollWheelZoomHandler_Animated(t *testing.T) {
	v := specView()
	v.SetView(LL(0, 0), 3)
	v.TakeEvents()
	v.SetClock(handlerSpecT0)
	opts := DefaultHandlerOptions()
	at := Pt(0, 0)
	geoAtPointer := v.ContainerPointToLatLng(at)

	var w wheelHandler
	w.wheel(handlerSpecWheelNotch, at, handlerSpecT0)
	t40 := handlerSpecT0.Add(opts.WheelDebounce)
	v.Tick(t40)
	w.tick(v, t40, opts)
	ev := v.TakeEvents()
	assert.True(t, ev.ZoomStart && ev.MoveStart && ev.ZoomAnimStart, "%+v", ev)
	assert.False(t, ev.ZoomEnd)
	assert.Equal(t, 5.0, ev.ZoomAnimZoom)
	assertNearLatLng(t, LL(25.48295117535531, -26.3671875), ev.ZoomAnimCenter)
	assert.Equal(t, 3.0, v.Zoom(), "the first frame has not run")

	mid := handlerSpecRun(v, t40, zoomAnimDuration/2)
	assert.True(t, v.AnimatingZoom())
	assert.Greater(t, v.Zoom(), 3.0)
	assert.Less(t, v.Zoom(), 5.0)
	// The geography under the pointer stays under it throughout.
	under := v.Unproject(v.Project(v.Center()).Subtract(v.Size().DivideBy(2)).Add(at))
	assert.True(t, geoAtPointer.Equals(under), "anchored at the pointer: %v vs %v", geoAtPointer, under)

	handlerSpecRun(v, mid, zoomAnimDuration/2)
	assert.False(t, v.Animating())
	assert.Equal(t, 5.0, v.Zoom())
	assertNearLatLng(t, LL(25.48295117535531, -26.3671875), v.Center())
	ev = v.TakeEvents()
	assert.True(t, ev.ZoomEnd && ev.MoveEnd, "%+v", ev)
}

// describe('DoubleClickZoomHandler')
func TestDoubleClickZoomHandler(t *testing.T) {
	// beforeEach: center [0, 0], zoom 3, zoomAnimation: false
	newMap := func() *View {
		v := specView()
		v.SetZoomAnimation(false)
		v.SetView(LL(0, 0), 3)
		v.TakeEvents()
		v.SetClock(handlerSpecT0)
		return v
	}
	at := Pt(0, 0) // UIEventSimulator's dblclick lands at client (0, 0)

	t.Run("zooms in while dblclick", func(t *testing.T) {
		v := newMap()
		zoom := v.Zoom()
		doubleClick(v, at, false)
		require.True(t, v.TakeEvents().ZoomEnd, "zoomend")
		assertNearLatLng(t, LL(17.308687886770034, -17.578125000000004), v.Center())
		assert.Greater(t, v.Zoom(), zoom)
		assert.Equal(t, 4.0, v.Zoom())
	})

	t.Run("zooms out while dblclick and holding shift", func(t *testing.T) {
		v := newMap()
		zoom := v.Zoom()
		doubleClick(v, at, true)
		require.True(t, v.TakeEvents().ZoomEnd, "zoomend")
		assertNearLatLng(t, LL(-33.137551192346145, 35.15625000000001), v.Center())
		assert.Less(t, v.Zoom(), zoom)
		assert.Equal(t, 2.0, v.Zoom())
	})
}

// The double-click's zoom with the zoom animation on: a 250 ms zoom
// animation about the click that settles on the spec's view. No upstream
// `it`.
func TestDoubleClickZoomHandler_Animated(t *testing.T) {
	v := specView()
	v.SetView(LL(0, 0), 3)
	v.TakeEvents()
	v.SetClock(handlerSpecT0)
	at := Pt(0, 0)
	geoAtClick := v.ContainerPointToLatLng(at)

	doubleClick(v, at, false)
	ev := v.TakeEvents()
	assert.True(t, ev.ZoomStart && ev.MoveStart && ev.ZoomAnimStart, "%+v", ev)
	assert.Equal(t, 4.0, ev.ZoomAnimZoom)
	assertNearLatLng(t, LL(17.308687886770034, -17.578125000000004), ev.ZoomAnimCenter)
	assert.Equal(t, 3.0, v.Zoom())

	mid := handlerSpecRun(v, handlerSpecT0, zoomAnimDuration/2)
	assert.True(t, v.AnimatingZoom())
	assert.Greater(t, v.Zoom(), 3.0)
	assert.Less(t, v.Zoom(), 4.0)
	under := v.Unproject(v.Project(v.Center()).Subtract(v.Size().DivideBy(2)).Add(at))
	assert.True(t, geoAtClick.Equals(under), "anchored at the click: %v vs %v", geoAtClick, under)

	handlerSpecRun(v, mid, zoomAnimDuration/2)
	assert.False(t, v.Animating())
	assert.Equal(t, 4.0, v.Zoom())
	assertNearLatLng(t, LL(17.308687886770034, -17.578125000000004), v.Center())
	ev = v.TakeEvents()
	assert.True(t, ev.ZoomEnd && ev.MoveEnd, "%+v", ev)
}

// describe('PinchZoomHandler')
func TestPinchZoomHandler(t *testing.T) {
	// beforeEach: pinchZoom: true, inertia: false, zoomAnimation: false, in
	// a 600×600 container.
	newMap := func(zoom float64) *View {
		v := specViewSized(600, 600)
		v.SetZoomAnimation(false)
		v.SetView(LL(0, 0), zoom)
		v.TakeEvents()
		v.SetClock(handlerSpecT0)
		return v
	}
	opts := DefaultHandlerOptions()
	opts.NoInertia = true
	// Both fingers move 200 px along y = 300 over 500 ms, symmetric about
	// (300, 300): out from 50 px apart to 450, in from 450 to 50.
	mid := Pt(300, 300)
	const pinchDuration = 500 * time.Millisecond
	pinchOut := func(v *View, p *pinchHandler, each func()) time.Time {
		return handlerSpecPinch(v, p, mid, 50, 450, pinchDuration, handlerSpecT0, opts, each)
	}
	pinchIn := func(v *View, p *pinchHandler, each func()) time.Time {
		return handlerSpecPinch(v, p, mid, 450, 50, pinchDuration, handlerSpecT0, opts, each)
	}
	// up(100): the fingers lift; the gesture ends once no step has come for
	// pinchIdle.
	liftOff := func(v *View, p *pinchHandler, last time.Time) {
		end := last.Add(pinchIdle + 10*time.Millisecond)
		v.Tick(end)
		p.tick(v, end)
	}

	t.Run("Increases zoom when pinching out", func(t *testing.T) {
		v := newMap(1)
		var p pinchHandler
		last := pinchOut(v, &p, nil)
		// Initial zoom 1, initial distance 50px, final distance 450px: 1 + log2(9)
		// while the fingers are down, snapped on release.
		assert.InDelta(t, 4.169925001442312, v.Zoom(), 1e-9)
		liftOff(v, &p, last)
		require.True(t, v.TakeEvents().ZoomEnd, "zoomend")
		assert.True(t, v.Center().Equals(LL(0, 0)), "got %v", v.Center())
		assert.Equal(t, 4.0, v.Zoom())
	})

	t.Run("Decreases zoom when pinching in", func(t *testing.T) {
		v := newMap(4)
		var p pinchHandler
		last := pinchIn(v, &p, nil)
		// Initial zoom 4, initial distance 450px, final distance 50px
		assert.InDelta(t, 0.8300749985576874, v.Zoom(), 1e-9)
		liftOff(v, &p, last)
		require.True(t, v.TakeEvents().ZoomEnd, "zoomend")
		assert.True(t, v.Center().Equals(LL(0, 0)), "got %v", v.Center())
		assert.Equal(t, 1.0, v.Zoom())
	})

	t.Run("fires zoom event while pinch zoom", func(t *testing.T) {
		v := newMap(4)
		var p pinchHandler
		zoomEvents, pinchZoomEvent := 0, false
		last := pinchIn(v, &p, func() {
			ev := v.TakeEvents()
			if ev.Zoom {
				zoomEvents++
				// e.pinch: the port marks a pinch's frames Animating.
				pinchZoomEvent = pinchZoomEvent || ev.Animating
			}
		})
		liftOff(v, &p, last)
		require.True(t, v.TakeEvents().ZoomEnd, "zoomend")
		assert.Greater(t, zoomEvents, 1, "spy.callCount > 1")
		assert.True(t, pinchZoomEvent)
		assert.True(t, v.Center().Equals(LL(0, 0)), "got %v", v.Center())
		assert.Equal(t, 1.0, v.Zoom())
	})

	t.Run("Dragging is possible after pinch zoom", func(t *testing.T) {
		v := newMap(8)
		var p pinchHandler
		last := pinchIn(v, &p, nil)
		liftOff(v, &p, last)
		assert.Equal(t, 5.0, v.Zoom())

		// f1.wait(100).moveTo(200, 300, 0).down().moveBy(5, 0, 20).moveBy(-150, 0, 200).up()
		var d dragHandler
		t0 := last.Add(pinchIdle + 100*time.Millisecond)
		v.Tick(t0)
		d.start(v, Pt(200, 300), t0, opts)
		tm := handlerSpecDragTo(v, &d, Pt(200, 300), Pt(205, 300), t0, 20*time.Millisecond, opts)
		tm = handlerSpecDragTo(v, &d, Pt(205, 300), Pt(55, 300), tm, 200*time.Millisecond, opts)
		d.end(v, tm, opts)

		assert.Equal(t, 0.0, v.Center().Lat)
		assert.Greater(t, v.Center().Lng, 5.0)
		// 145 px east at zoom 5
		assert.InDelta(t, 6.3720703125, v.Center().Lng, 1e-9)
	})

	t.Run("PinchZoom works with disabled map dragging", func(t *testing.T) {
		// dragging: false is the widget not calling the drag handler; the
		// pinch alone is the previous case's.
		v := newMap(4)
		var p pinchHandler
		last := pinchIn(v, &p, nil)
		liftOff(v, &p, last)
		require.True(t, v.TakeEvents().ZoomEnd, "zoomend")
		assert.True(t, v.Center().Equals(LL(0, 0)), "got %v", v.Center())
		assert.Equal(t, 1.0, v.Zoom())
	})
}

// PinchZoomHandler.js's bounceAtZoomLimits has no upstream `it`: on (the
// default) the gesture overshoots the zoom limits and the release snaps back
// to them; off, the gesture stops at them.
func TestPinchZoomHandler_BounceAtZoomLimits(t *testing.T) {
	newMap := func() *View {
		v := specViewOpts(ViewOptions{Size: Pt(600, 600), MaxZoom: 3, HasMaxZoom: true})
		v.SetZoomAnimation(false)
		v.SetView(LL(0, 0), 1)
		v.TakeEvents()
		v.SetClock(handlerSpecT0)
		return v
	}
	mid := Pt(300, 300)

	t.Run("on: overshoots while the fingers are down and snaps back on release", func(t *testing.T) {
		v := newMap()
		opts := DefaultHandlerOptions()
		require.False(t, opts.NoBounceAtZoomLimits)
		var p pinchHandler
		last := handlerSpecPinch(v, &p, mid, 50, 450, 500*time.Millisecond, handlerSpecT0, opts, nil)
		assert.InDelta(t, 4.169925001442312, v.Zoom(), 1e-9, "past maxZoom 3")
		end := last.Add(pinchIdle)
		v.Tick(end)
		p.tick(v, end)
		assert.Equal(t, 3.0, v.Zoom())
		assert.True(t, v.TakeEvents().ZoomEnd)
	})

	t.Run("off: stops at the limit", func(t *testing.T) {
		v := newMap()
		opts := DefaultHandlerOptions()
		opts.NoBounceAtZoomLimits = true
		var p pinchHandler
		maxSeen := 0.0
		last := handlerSpecPinch(v, &p, mid, 50, 450, 500*time.Millisecond, handlerSpecT0, opts, func() {
			if v.Zoom() > maxSeen {
				maxSeen = v.Zoom()
			}
		})
		assert.Equal(t, 3.0, maxSeen, "never past maxZoom")
		assert.Equal(t, 3.0, v.Zoom())
		end := last.Add(pinchIdle)
		v.Tick(end)
		p.tick(v, end)
		assert.Equal(t, 3.0, v.Zoom())
		assert.True(t, v.TakeEvents().ZoomEnd)
	})
}

// describe('BoxZoomHandler')
func TestBoxZoomHandler(t *testing.T) {
	// beforeEach: center [0, 0], zoom 3, zoomAnimation: false
	newMap := func() *View {
		v := specView()
		v.SetZoomAnimation(false)
		v.SetView(LL(0, 0), 3)
		v.TakeEvents()
		v.SetClock(handlerSpecT0)
		return v
	}

	t.Run("cancel boxZoom by pressing ESC and re-enable click event on the map", func(t *testing.T) {
		v := newMap()
		var b boxZoom
		// finger.moveTo(100, 100).shift().down(); finger.moveBy(100, 100)
		b.begin(Pt(100, 100))
		b.move(Pt(200, 200))
		// boxzoomstart fired and not boxzoomend; one .leaflet-zoom-box in the
		// container: the box is being drawn.
		r, ok := b.rect()
		require.True(t, ok)
		assert.True(t, BoundsOf(Pt(100, 100), Pt(200, 200)).Equals(r), "got %v", r)
		assert.Equal(t, 3.0, v.Zoom())

		// keydown Escape
		b.cancel()
		_, ok = b.rect()
		assert.False(t, ok, "no .leaflet-zoom-box left")

		// finger.unshift().up(): the release after the cancel fits nothing —
		// boxzoomend not called, the view as it was.
		b.finish(v)
		assert.Equal(t, 3.0, v.Zoom())
		assert.Equal(t, LL(0, 0), v.Center())
		assert.False(t, v.TakeEvents().Any())
	})

	t.Run("zooms from level 3 to level 5", func(t *testing.T) {
		v := newMap()
		assert.Equal(t, 3.0, v.Zoom())
		var b boxZoom
		// finger.shift().moveTo(100, 100).down(); finger.moveBy(100, 100).unshift().up()
		b.begin(Pt(100, 100))
		b.move(Pt(200, 200))
		_, started := b.rect() // boxzoomstart
		b.finish(v)
		_, stillDrawing := b.rect() // boxzoomend: the box is gone
		assert.True(t, started)
		assert.False(t, stillDrawing)
		ev := v.TakeEvents()
		assert.True(t, ev.Zoom, "zoom: %+v", ev)
		assert.Equal(t, 5.0, v.Zoom())
	})

	t.Run("doesn't start box zoom if shift key is not pressed", func(t *testing.T) {
		// Shift is read by the widget, which begins the box only with it: an
		// unshifted press-move-release reaches a box that was never begun.
		v := newMap()
		assert.Equal(t, 3.0, v.Zoom())
		var b boxZoom
		b.move(Pt(200, 200))
		_, ok := b.rect()
		assert.False(t, ok, "no boxzoomstart")
		b.finish(v)
		assert.Equal(t, 3.0, v.Zoom())
		assert.False(t, v.TakeEvents().Any())
	})

	t.Run("zooms out when dragged box is larger than map", func(t *testing.T) {
		// createContainer('75px', '75px'), zoom 10
		smallMap := specViewSized(75, 75)
		smallMap.SetZoomAnimation(false)
		smallMap.SetView(LL(0, 0), 10)
		smallMap.TakeEvents()
		smallMap.SetClock(handlerSpecT0)
		var b boxZoom

		// finger.moveTo(50, 50).shift().down(); finger.moveBy(150, 150).unshift().up()
		b.begin(Pt(50, 50))
		b.move(Pt(200, 200))
		b.finish(smallMap)
		assert.Equal(t, 9.0, smallMap.Zoom())

		// finger.moveTo(50, 50).shift().down(); finger.moveBy(300, 300).unshift().up()
		b.begin(Pt(50, 50))
		b.move(Pt(350, 350))
		b.finish(smallMap)
		assert.Equal(t, 7.0, smallMap.Zoom())
	})
}

// describe('KeyboardHandler')
func TestKeyboardHandler(t *testing.T) {
	// beforeEach: zoomAnimation: false, setView([0, 0], 5), focused. The spec
	// stubs panBy to animate: false to save time; here the pan animation the
	// arrows start is stepped to its end.
	newMap := func() *View {
		v := specView()
		v.SetZoomAnimation(false)
		v.SetView(LL(0, 0), 5)
		v.TakeEvents()
		v.SetClock(handlerSpecT0)
		return v
	}
	opts := DefaultHandlerOptions()
	arrow := func(t *testing.T, v *View, dx, dy float64) {
		t.Helper()
		keyboardPan(v, dx, dy, false, opts)
		handlerSpecRun(v, handlerSpecT0, panAnimDefaultDuration)
		require.False(t, v.Animating())
	}

	// describe('arrow keys')
	t.Run("move the map north", func(t *testing.T) {
		v := newMap()
		arrow(t, v, 0, -1) // ArrowUp
		assert.Greater(t, v.Center().Lat, 0.0)
		// keyboardPanDelta 80 px
		assert.Equal(t, Pt(0, 80), handlerSpecPaneOffset(v, LL(0, 0)))
		assertNearLatLng(t, LL(3.5134210456400448, 0), v.Center())
	})

	t.Run("move the map south", func(t *testing.T) {
		v := newMap()
		arrow(t, v, 0, 1) // ArrowDown
		assert.Less(t, v.Center().Lat, 0.0)
		assert.Equal(t, Pt(0, -80), handlerSpecPaneOffset(v, LL(0, 0)))
		assertNearLatLng(t, LL(-3.513421045640032, 0), v.Center())
	})

	t.Run("move the map west", func(t *testing.T) {
		v := newMap()
		arrow(t, v, -1, 0) // ArrowLeft
		assert.Less(t, v.Center().Lng, 0.0)
		assert.Equal(t, Pt(80, 0), handlerSpecPaneOffset(v, LL(0, 0)))
		assertNearLatLng(t, LL(0, -3.515625), v.Center())
	})

	t.Run("move the map east", func(t *testing.T) {
		v := newMap()
		arrow(t, v, 1, 0) // ArrowRight
		assert.Greater(t, v.Center().Lng, 0.0)
		assert.Equal(t, Pt(-80, 0), handlerSpecPaneOffset(v, LL(0, 0)))
		assertNearLatLng(t, LL(0, 3.515625), v.Center())
	})
}

// KeyboardHandler.js's _onKeyDown rules for the arrows beyond the four
// directions — shift triples the pan, maxBounds limit it, a key while a pan
// animates is ignored — have no upstream `it`.
func TestKeyboardHandler_PanRules(t *testing.T) {
	newMap := func() *View {
		v := specView()
		v.SetView(LL(0, 0), 5)
		v.TakeEvents()
		v.SetClock(handlerSpecT0)
		return v
	}
	opts := DefaultHandlerOptions()

	t.Run("shift pans three times keyboardPanDelta", func(t *testing.T) {
		v := newMap()
		keyboardPan(v, 0, -1, true, opts)
		handlerSpecRun(v, handlerSpecT0, panAnimDefaultDuration)
		assert.Equal(t, Pt(0, 240), handlerSpecPaneOffset(v, LL(0, 0)))
	})

	t.Run("maxBounds limit the pan", func(t *testing.T) {
		v := newMap()
		// The viewport plus 40 px to the north: an 80 px pan north stops at 40.
		v.SetMaxBounds(LatLngBoundsOf(v.ContainerPointToLatLng(Pt(0, -40)), v.ContainerPointToLatLng(Pt(400, 400))))
		v.TakeEvents()
		keyboardPan(v, 0, -1, false, opts)
		handlerSpecRun(v, handlerSpecT0, panAnimDefaultDuration)
		assert.Equal(t, Pt(0, 40), handlerSpecPaneOffset(v, LL(0, 0)))
		// and no pan at all against the edge
		keyboardPan(v, -1, 0, false, opts)
		handlerSpecRun(v, handlerSpecT0.Add(panAnimDefaultDuration), panAnimDefaultDuration)
		assert.Equal(t, Pt(0, 40), handlerSpecPaneOffset(v, LL(0, 0)))
	})

	t.Run("a key while a pan animates is ignored", func(t *testing.T) {
		v := newMap()
		keyboardPan(v, 0, -1, false, opts)
		t1 := handlerSpecT0.Add(handlerSpecFrame)
		v.Tick(t1)
		require.True(t, v.Animating())
		keyboardPan(v, 0, -1, false, opts) // ignored: the first pan is in progress
		handlerSpecRun(v, t1, panAnimDefaultDuration)
		require.False(t, v.Animating())
		assert.Equal(t, Pt(0, 80), handlerSpecPaneOffset(v, LL(0, 0)))
		// Once it has ended the next key pans again.
		t2 := handlerSpecT0.Add(time.Second)
		v.Tick(t2)
		keyboardPan(v, 0, -1, false, opts)
		handlerSpecRun(v, t2, panAnimDefaultDuration)
		assert.Equal(t, Pt(0, 160), handlerSpecPaneOffset(v, LL(0, 0)))
	})
}

func TestHandlerOptions_PartialLiteralKeepsLeafletDefaults(t *testing.T) {
	// A literal that sets one knob must not zero the others (the widget
	// used to substitute DefaultHandlerOptions only for the all-zero value, so
	// a partial literal gave WheelPxPerZoom 0 and a NaN inertia pan).
	got := HandlerOptions{MaxBoundsViscosity: 1}.withDefaults()
	want := DefaultHandlerOptions()
	want.MaxBoundsViscosity = 1
	assert.Equal(t, want, got)
	assert.False(t, got.NoInertia, "the zero value of the switches is upstream's setting")
	assert.False(t, got.NoBounceAtZoomLimits)
	assert.Equal(t, 60.0, got.WheelPxPerZoom)
	assert.Equal(t, 3400.0, got.InertiaDeceleration)
	assert.Equal(t, 0.2, got.EaseLinearity)
	assert.True(t, math.IsInf(got.InertiaMaxSpeed, 1))
}
