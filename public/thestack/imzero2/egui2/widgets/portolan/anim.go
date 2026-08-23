package portolan

import (
	"math"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// The animated view changes of Map.js — setView's animated pan and zoom,
// panBy's pan animation (src/dom/PosAnimation.js), flyTo — as state machines
// stepped by Tick from the widget's frame. Leaflet drives these with
// requestAnimationFrame and CSS transitions; here the frame is the clock and
// the interpolated view is what Draw paints.

// AnimateE says whether a view change animates: AnimateAuto is Leaflet's
// default — animate when the change stays within the viewport — AnimateYes
// forces it, AnimateNo forbids it.
type AnimateE uint8

const (
	AnimateAuto AnimateE = iota
	AnimateYes
	AnimateNo
)

// AnimateOptions are setView's / panBy's animation options.
type AnimateOptions struct {
	Animate AnimateE
	// Duration of a pan animation; 0 is Leaflet's 0.25 s.
	Duration time.Duration
	// EaseLinearity of the pan's ease-out (0 is Leaflet's 0.5; inertia
	// uses 0.2).
	EaseLinearity float64
	// NoMoveStart suppresses the movestart event (inertia's pan).
	NoMoveStart bool
}

// FlyOptions are flyTo's.
type FlyOptions struct {
	// Animate: AnimateNo degrades to setView.
	Animate AnimateE
	// Duration, 0 = Leaflet's formula (0.8 s per unit of the trajectory).
	Duration    time.Duration
	NoMoveStart bool
}

const (
	// zoomAnimDuration is Leaflet's CSS zoom transition, 250 ms with
	// cubic-bezier(0, 0, 0.25, 1).
	zoomAnimDuration = 250 * time.Millisecond
	// zoomAnimationThreshold is how many levels a zoom may span and still
	// animate.
	zoomAnimationThreshold = 4.0
	panAnimDefaultDuration = 250 * time.Millisecond
	panAnimDefaultEase     = 0.5
)

// easeOut is PosAnimation's curve: 1 − (1 − t)^power, power = 1/easeLinearity.
func easeOut(t, easeLinearity float64) float64 {
	power := 1 / math.Max(easeLinearity, 0.2)
	return 1 - math.Pow(1-t, power)
}

// zoomEase is cubic-bezier(0, 0, 0.25, 1) — the CSS transition timing of
// Leaflet's zoom animation — solved for y at x = t.
func zoomEase(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	const x1, y1, x2, y2 = 0.0, 0.0, 0.25, 1.0
	bx := func(s float64) float64 {
		u := 1 - s
		return 3*u*u*s*x1 + 3*u*s*s*x2 + s*s*s
	}
	by := func(s float64) float64 {
		u := 1 - s
		return 3*u*u*s*y1 + 3*u*s*s*y2 + s*s*s
	}
	// Newton on x(s) = t, bisection fallback.
	s := t
	for i := 0; i < 8; i++ {
		x := bx(s) - t
		if math.Abs(x) < 1e-7 {
			return by(s)
		}
		u := 1 - s
		dx := 3*u*u*x1 + 6*u*s*(x2-x1) + 3*s*s*(1-x2)
		if dx == 0 {
			break
		}
		s -= x / dx
		if s < 0 || s > 1 {
			break
		}
	}
	lo, hi := 0.0, 1.0
	for i := 0; i < 40; i++ {
		s = (lo + hi) / 2
		if bx(s) < t {
			lo = s
		} else {
			hi = s
		}
	}
	return by(s)
}

type panAnimation struct {
	startPx   Point // projected centre at the start, current zoom
	offset    Point
	start     time.Time
	duration  time.Duration
	linearity float64
}

type zoomAnimation struct {
	z0, z1      float64
	c1          LatLng
	anchor      Point  // the fixed point, in container pixels
	anchorGeo   LatLng // its geography at the start
	scale       float64
	start       time.Time
	noMoveStart bool
}

type flyAnimation struct {
	from, to      Point
	startZoom     float64
	targetCenter  LatLng
	targetZoom    float64
	w0, u1, r0, s float64
	start         time.Time
	duration      time.Duration
}

// animState is the View's animation machinery.
type animState struct {
	pan  *panAnimation
	zoom *zoomAnimation
	fly  *flyAnimation
	// now is the clock of the last Tick; animations started between Ticks
	// use it, so a frame's handlers and animations agree on the time.
	now time.Time
	// enabled is Leaflet's zoomAnimation option.
	zoomEnabled bool
}

// SetClock sets the frame's time without stepping animations; Tick does
// both. Handlers that start an animation between Ticks see this clock.
func (v *View) SetClock(now time.Time) { v.anim.now = now }

func (v *View) clock() time.Time {
	if v.anim.now.IsZero() {
		return time.Now()
	}
	return v.anim.now
}

// Animating reports a pan, zoom or fly animation in progress.
func (v *View) Animating() bool { return v.anim.pan != nil || v.anim.zoom != nil || v.anim.fly != nil }

// AnimatingZoom reports a zoom animation in progress (Leaflet's
// _animatingZoom).
func (v *View) AnimatingZoom() bool { return v.anim.zoom != nil }

// TargetZoom is the zoom a running zoom animation ends at, or Zoom when
// none runs — what a handler adds its next step to, so that a request
// arriving mid-animation (and restarting it, see tryAnimatedZoom) builds on
// the target rather than on the interpolated zoom and loses the remainder.
// Leaflet's getZoom() during its zoom animation is likewise the pre-animation
// zoom, not an interpolated one.
func (v *View) TargetZoom() float64 {
	if a := v.anim.zoom; a != nil {
		return a.z1
	}
	return v.zoom
}

// SetZoomAnimation turns the zoom animation on or off (on by default).
func (v *View) SetZoomAnimation(on bool) { v.anim.zoomEnabled = on }

// Stop ends every animation where it is. A pan and a fly stop in place
// (Leaflet's _stop — though a stopped fly records zoom, zoomend and moveend
// here, where Leaflet cancels its frame silently, so the pyramid re-levels);
// a zoom animation ends at its interpolated view, which Leaflet never
// interrupts — a press during its 250 ms is ignored there, and here starts
// the drag from where the map is.
func (v *View) Stop() {
	v.stopPanFly()
	v.stopZoom()
}

// stopPanFly is Leaflet's _stop: the pan and fly animations end in place.
func (v *View) stopPanFly() {
	if v.anim.pan != nil {
		v.anim.pan = nil
		v.MoveEnd(false)
	}
	if v.anim.fly != nil {
		v.anim.fly = nil
		v.events.Zoom, v.events.Move = true, true
		v.MoveEnd(true)
	}
}

// stopZoom ends a zoom animation at its interpolated view, recording the
// zoom and move its silent frames did not, so the pyramid re-levels from
// the target level it was told about.
func (v *View) stopZoom() {
	if v.anim.zoom == nil {
		return
	}
	v.anim.zoom = nil
	v.events.Zoom, v.events.Move = true, true
	v.MoveEnd(true)
}

// Tick advances the animations to now and reports whether one is still
// running (the widget asks for another frame).
func (v *View) Tick(now time.Time) (animating bool) {
	v.anim.now = now
	if a := v.anim.pan; a != nil {
		t := float64(now.Sub(a.start)) / float64(a.duration)
		if t < 1 {
			p := easeOut(t, a.linearity)
			v.MoveTo(v.Unproject(a.startPx.Add(a.offset.MultiplyBy(p))), v.zoom)
		} else {
			v.anim.pan = nil
			v.MoveTo(v.Unproject(a.startPx.Add(a.offset)), v.zoom)
			v.MoveEnd(false)
		}
	}
	if a := v.anim.zoom; a != nil {
		t := float64(now.Sub(a.start)) / float64(zoomAnimDuration)
		if t < 1 {
			p := zoomEase(t)
			s := 1 + (a.scale-1)*p
			zoom := a.z0 + math.Log2(s)
			v.setSilently(v.centerKeeping(a.anchorGeo, a.anchor, zoom), zoom)
		} else {
			v.anim.zoom = nil
			v.MoveTo(a.c1, a.z1)
			v.MoveEnd(true)
		}
	}
	if a := v.anim.fly; a != nil {
		t := float64(now.Sub(a.start)) / float64(a.duration)
		if t <= 1 {
			s := (1 - math.Pow(1-t, 1.5)) * a.s
			center := v.UnprojectAt(a.from.Add(a.to.Subtract(a.from).MultiplyBy(a.u(s)/a.u1)), a.startZoom)
			zoom := v.ScaleZoomAt(a.w0/a.w(s), a.startZoom)
			v.events.Animating = true
			v.MoveTo(center, zoom)
		} else {
			v.anim.fly = nil
			v.MoveTo(a.targetCenter, a.targetZoom)
			v.MoveEnd(true)
		}
	}
	return v.Animating()
}

// setSilently moves the view without recording events — Leaflet's _move
// with suppressEvent, what a zoom animation's frames do.
func (v *View) setSilently(center LatLng, zoom float64) {
	v.center, v.zoom = center, zoom
	v.pixelOrigin = v.newPixelOrigin(center, zoom)
}

// centerKeeping is the centre that puts geo under the container point at
// a zoom.
func (v *View) centerKeeping(geo LatLng, at Point, zoom float64) LatLng {
	return v.UnprojectAt(v.ProjectAt(geo, zoom).Subtract(at.Subtract(v.size.DivideBy(2))), zoom)
}

// centerOffset is Leaflet's _getCenterOffset: a point's layer offset from the
// viewport centre.
func (v *View) centerOffset(ll LatLng) Point {
	return v.LatLngToLayerPoint(ll).Subtract(v.size.DivideBy(2))
}

// SetViewAnimated is Leaflet's setView with options on a loaded map: the zoom
// is snapped and clamped, the centre kept inside MaxBounds, a running pan or
// fly stopped; then a zoom change tries the zoom animation — restarting one
// already running from its interpolated view, see tryAnimatedZoom — a pure
// pan the pan animation, and what cannot animate is a hard reset.
func (v *View) SetViewAnimated(center LatLng, zoom float64, opts AnimateOptions) {
	zoom = v.LimitZoom(zoom)
	center = v.LimitCenter(center, zoom, v.maxBounds)
	v.stopPanFly()
	if v.loaded {
		var moved bool
		if v.zoom != zoom {
			moved = v.tryAnimatedZoom(center, zoom, opts)
		} else {
			// A pan while a zoom animation runs ends it where it is: the
			// two would otherwise fight for the view frame by frame.
			v.stopZoom()
			moved = v.tryAnimatedPan(center, opts)
		}
		if moved {
			return
		}
	}
	// A hard reset supersedes a running zoom animation outright.
	v.anim.zoom = nil
	v.resetView(center, zoom, opts.NoMoveStart)
}

// SetZoomAnimated is SetZoom with animation.
func (v *View) SetZoomAnimated(zoom float64, opts AnimateOptions) {
	if !v.loaded {
		v.zoom = zoom
		return
	}
	v.SetViewAnimated(v.center, zoom, opts)
}

// SetZoomAroundAnimated is SetZoomAround with animation — the wheel's and
// the double-click's zoom.
func (v *View) SetZoomAroundAnimated(containerPoint Point, zoom float64, opts AnimateOptions) {
	scale := v.ZoomScale(zoom)
	viewHalf := v.size.DivideBy(2)
	centerOffset := containerPoint.Subtract(viewHalf).MultiplyBy(1 - 1/scale)
	newCenter := v.ContainerPointToLatLng(viewHalf.Add(centerOffset))
	v.SetViewAnimated(newCenter, zoom, opts)
}

// PanToAnimated is PanTo with animation.
func (v *View) PanToAnimated(center LatLng, opts AnimateOptions) {
	v.SetViewAnimated(center, v.zoom, opts)
}

// PanByAnimated is Leaflet's panBy: a rounded pixel offset, animated over
// Duration with an ease-out unless it would move farther than the viewport
// (and AnimateAuto), in which case it is a hard reset.
func (v *View) PanByAnimated(offset Point, opts AnimateOptions) {
	offset = offset.Round()
	if offset.X == 0 && offset.Y == 0 {
		v.events.MoveEnd = true
		return
	}
	// A running zoom animation ends where it is (its frames would fight
	// the pan's for the view); a running pan ends with its moveend, which
	// is what PosAnimation.run's stop() does before the new run.
	v.stopZoom()
	if v.anim.pan != nil {
		v.anim.pan = nil
		v.MoveEnd(false)
	}
	if opts.Animate != AnimateYes && !v.size.Contains(offset) {
		v.resetView(v.Unproject(v.Project(v.center).Add(offset)), v.zoom, false)
		return
	}
	if !opts.NoMoveStart {
		v.events.MoveStart = true
	}
	if opts.Animate == AnimateNo {
		v.MoveTo(v.Unproject(v.Project(v.center).Add(offset)), v.zoom)
		v.MoveEnd(false)
		return
	}
	duration := opts.Duration
	if duration <= 0 {
		duration = panAnimDefaultDuration
	}
	linearity := opts.EaseLinearity
	if linearity == 0 {
		linearity = panAnimDefaultEase
	}
	v.anim.pan = &panAnimation{
		startPx: v.Project(v.center), offset: offset,
		start: v.clock(), duration: duration, linearity: linearity,
	}
}

// tryAnimatedPan is Leaflet's _tryAnimatedPan.
func (v *View) tryAnimatedPan(center LatLng, opts AnimateOptions) bool {
	offset := v.centerOffset(center).Trunc()
	if opts.Animate != AnimateYes && !v.size.Contains(offset) {
		return false
	}
	v.PanByAnimated(offset, opts)
	return true
}

// tryAnimatedZoom is Leaflet's _tryAnimatedZoom: a zoom of at most the
// threshold whose fixed point lies in the viewport (unless forced) animates
// about that point over 250 ms. Where Leaflet ignores a request while a zoom
// animation runs, this restarts the animation from the interpolated view —
// egui smooths a wheel notch over a dozen frames, and dropping those would
// lose most of every notch.
func (v *View) tryAnimatedZoom(center LatLng, zoom float64, opts AnimateOptions) bool {
	if !v.anim.zoomEnabled || opts.Animate == AnimateNo || math.Abs(zoom-v.zoom) > zoomAnimationThreshold {
		return false
	}
	scale := v.ZoomScale(zoom)
	offset := v.centerOffset(center).DivideBy(1 - 1/scale)
	if opts.Animate != AnimateYes && !v.size.Contains(offset) {
		return false
	}
	if v.anim.zoom == nil {
		v.MoveStart(true)
		if opts.NoMoveStart {
			v.events.MoveStart = false
		}
	}
	anchor := v.size.DivideBy(2).Add(offset)
	v.anim.zoom = &zoomAnimation{
		z0: v.zoom, z1: zoom, c1: center,
		anchor: anchor, anchorGeo: v.ContainerPointToLatLng(anchor),
		scale: scale, start: v.clock(), noMoveStart: opts.NoMoveStart,
	}
	v.events.ZoomAnimStart = true
	v.events.ZoomAnimCenter, v.events.ZoomAnimZoom = center, zoom
	return true
}

// AnimateZoomTo animates to a centre and zoom about their fixed point even
// when that point lies outside the viewport — the pinch's end (Leaflet's
// _animateZoom called directly). A target equal to the current view just
// records the end events.
func (v *View) AnimateZoomTo(center LatLng, zoom float64) {
	if v.anim.zoom == nil && zoom == v.zoom && center.Equals(v.center) {
		v.MoveEnd(true)
		return
	}
	if !v.tryAnimatedZoom(center, zoom, AnimateOptions{Animate: AnimateYes}) {
		v.resetView(center, zoom, false)
	}
}

// FlyTo is Leaflet's flyTo: the van Wijk–Nuij trajectory that zooms out,
// pans and zooms back in along one smooth curve. An explicit Duration
// overrides the trajectory's own.
func (v *View) FlyTo(targetCenter LatLng, targetZoom float64, opts FlyOptions) {
	if opts.Animate == AnimateNo || !v.loaded {
		v.SetView(targetCenter, targetZoom)
		return
	}
	v.Stop()
	startZoom := v.zoom
	targetZoom = v.LimitZoom(targetZoom)
	from := v.Project(v.center)
	to := v.Project(targetCenter)
	w0 := math.Max(v.size.X, v.size.Y)
	w1 := w0 * v.ZoomScaleAt(startZoom, targetZoom)
	u1 := to.DistanceTo(from)
	if u1 == 0 {
		u1 = 1
	}
	const rho = 1.42
	rho2 := rho * rho
	r := func(i int) float64 {
		s1, s2 := 1.0, w0
		if i != 0 {
			s1, s2 = -1.0, w1
		}
		t1 := w1*w1 - w0*w0 + s1*rho2*rho2*u1*u1
		b1 := 2 * s2 * rho2 * u1
		b := t1 / b1
		sq := math.Sqrt(b*b+1) - b
		if sq < 0.000000015 {
			return -18
		}
		return math.Log(sq)
	}
	r0 := r(0)
	S := (r(1) - r0) / rho
	duration := opts.Duration
	if duration <= 0 {
		duration = time.Duration(S * 0.8 * float64(time.Second))
	}
	v.anim.fly = &flyAnimation{
		from: from, to: to, startZoom: startZoom,
		targetCenter: targetCenter, targetZoom: targetZoom,
		w0: w0, u1: u1, r0: r0, s: S, start: v.clock(), duration: duration,
	}
	v.MoveStart(true)
	if opts.NoMoveStart {
		v.events.MoveStart = false
	}
	// The first frame of the trajectory, so the view moves on the call.
	v.Tick(v.clock())
}

func (a *flyAnimation) w(s float64) float64 {
	return a.w0 * (math.Cosh(a.r0) / math.Cosh(a.r0+1.42*s))
}

func (a *flyAnimation) u(s float64) float64 {
	const rho2 = 1.42 * 1.42
	return a.w0 * (math.Cosh(a.r0)*math.Tanh(a.r0+1.42*s) - math.Sinh(a.r0)) / rho2
}

// FlyToBounds flies to the view FitBounds would jump to.
func (v *View) FlyToBounds(bounds LatLngBounds, fit FitOptions, opts FlyOptions) error {
	if !bounds.IsValid() {
		return eh.Errorf("portolan: bounds are not valid")
	}
	center, zoom := v.BoundsCenterZoom(bounds, fit)
	v.FlyTo(center, zoom, opts)
	return nil
}

// FitBoundsAnimated is FitBounds with setView's animation rule.
func (v *View) FitBoundsAnimated(bounds LatLngBounds, fit FitOptions, opts AnimateOptions) error {
	if !bounds.IsValid() {
		return eh.Errorf("portolan: bounds are not valid")
	}
	center, zoom := v.BoundsCenterZoom(bounds, fit)
	v.SetViewAnimated(center, zoom, opts)
	return nil
}
