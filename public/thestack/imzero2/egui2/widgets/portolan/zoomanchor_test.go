package portolan

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A zoom animation turns its target centre into a fixed point to zoom about:
// the centre offset over 1 − 1/scale. At scale 1 — a "zoom" to the scale the
// view already holds — that divisor is 0, and the anchor comes out ±Inf, or
// NaN on an axis whose centre offset is 0 as well (an even viewport side puts
// one there exactly). Leaflet cannot reach it, because setView sends an
// unchanged zoom to the pan; AnimateZoomTo can, and the anchor's geography
// then carries the non-finite value into the centre, where it stays: every
// later projection of a NaN centre is NaN, so the map cannot be dragged or
// zoomed out of it and the pyramid refuses every update from then on.

func TestZoomAnimationRefusesAScaleItCannotAnchorOn(t *testing.T) {
	// The view as a gesture leaves it: a zoom animation in flight toward
	// another target, the zoom itself not yet moved (a running animation
	// interpolates the zoom only from Tick).
	t0 := handlerSpecT0
	// 800×601: the even side is the one whose centre offset is 0, so the
	// anchor takes the NaN there and the ±Inf on the odd one — the report's
	// lng=NaN beside a latitude pinned at the Mercator clamp.
	newView := func() *View {
		v := NewView(ViewOptions{Size: Pt(800, 601)})
		v.SetView(LL(47, 8), 5)
		v.TakeEvents()
		v.SetClock(t0)
		v.SetViewAnimated(v.Center(), 5.18, AnimateOptions{})
		require.True(t, v.AnimatingZoom(), "a zoom animation is in flight")
		require.Equal(t, 5.0, v.Zoom(), "whose zoom the view has not taken yet")
		return v
	}

	t.Run("a target equal to the view ends the gesture and leaves the animation alone", func(t *testing.T) {
		v := newView()
		v.AnimateZoomTo(v.Center(), v.Zoom())
		assert.True(t, v.TakeEvents().MoveEnd)
		require.True(t, v.AnimatingZoom(), "the animation of another gesture runs on")
		// The frames that follow stay on the animation's own target.
		for i := 1; i <= 20; i++ {
			v.Tick(t0.Add(time.Duration(i) * handlerSpecFrame))
			requireFiniteView(t, v)
		}
		assert.InDelta(t, 5.18, v.Zoom(), 1e-12)
	})

	t.Run("a centre elsewhere at the same zoom is a hard reset", func(t *testing.T) {
		v := newView()
		v.AnimateZoomTo(LL(48, 9), v.Zoom())
		requireFiniteView(t, v)
		assert.True(t, v.Center().Equals(LL(48, 9)), "got %v", v.Center())
		assert.True(t, v.TakeEvents().ViewReset)
		assert.False(t, v.AnimatingZoom(), "the reset supersedes the animation it replaced")
	})
}

// The gesture that reaches it, as the widget's frame runs it: a pinch's idle
// end (120 ms after the last step) and a wheel notch's debounce (40 ms after
// the scroll) can fall in one frame, and the frame applies the wheel first.
// The animation it starts does not move the view — a zoom animation
// interpolates only from Tick, on the frames after — so the pinch then ends
// on the zoom the view already holds: tryAnimatedZoom's scale is 1.
func TestPinchEndDuringAWheelZoomKeepsTheCentreProjectable(t *testing.T) {
	// The notch lands on a different frame each time round, so one run has it
	// arrive where its debounce coincides with the pinch's idle rather than
	// relying on the two constants staying where they are.
	for notchAt := 1; notchAt <= 10; notchAt++ {
		// ZoomSnap 0, the widget's own default (ADR-0204 §SD6): the pinch
		// ends on the fractional zoom its last step left, the view's own.
		v := NewView(ViewOptions{Size: Pt(800, 601)})
		v.SetView(LL(47, 8), 5)
		v.TakeEvents()
		opts := DefaultHandlerOptions()
		var pinch pinchHandler
		var wheel wheelHandler
		anchor := Pt(400, 300)

		v.SetClock(handlerSpecT0)
		pinch.step(v, 1.13, anchor, handlerSpecT0, opts)
		for i := 1; i <= 30; i++ { // the frame loop of widget.go's frame()
			now := handlerSpecT0.Add(time.Duration(i) * handlerSpecFrame)
			v.SetClock(now)
			v.Tick(now)
			if i == notchAt { // a scroll notch after the fingers have stopped
				wheel.wheel(-handlerSpecWheelNotch, anchor, now)
			}
			wheel.tick(v, now, opts)
			pinch.tick(v, now)
			requireFiniteView(t, v)
		}
		// Which of the two gestures the view ends on depends on their order
		// (the end of one supersedes the other's animation, see
		// SetViewAnimated); either way it ends about the anchor they shared,
		// and the view a drag would then reach is still geography.
		require.InDelta(t, 47, v.Center().Lat, 1, "notch at frame %d: %v", notchAt, v.Center())
		require.InDelta(t, 8, v.Center().Lng, 1, "notch at frame %d: %v", notchAt, v.Center())
		v.MoveTo(v.Unproject(v.Project(v.Center()).Subtract(Pt(3, 4))), v.Zoom())
		requireFiniteView(t, v)
	}
}

// requireFiniteView is the invariant the two tests share: a view that can
// still be projected, which is what the pyramid's tile range needs and what a
// centre carrying NaN or ±Inf can never give again.
func requireFiniteView(t *testing.T, v *View) {
	t.Helper()
	c := v.Center()
	require.True(t, finite(c.Lat), "lat %v", c.Lat)
	require.True(t, finite(c.Lng), "lng %v", c.Lng)
	require.True(t, finite(v.Zoom()), "zoom %v", v.Zoom())
}
