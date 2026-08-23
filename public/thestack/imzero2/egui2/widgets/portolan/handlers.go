package portolan

import (
	"math"
	"time"
)

// The interaction handlers of src/map/handler/*.js as state machines over
// pointer samples and times (ADR-0204 §SD6): the widget feeds them what the
// painter lane's registers report each frame, tests feed them samples. They
// act on a View; the widget owns the readback and the drawing.

// HandlerOptions are Leaflet's interaction options. The zero value is
// Leaflet's defaults: a zero number means its default, and the two switches
// are spelled so that false is upstream's setting — a partial literal keeps
// the rest at Leaflet's values rather than at zero.
type HandlerOptions struct {
	// NoInertia turns off the coast after a drag's release (DragHandler's
	// inertia, on upstream); InertiaDeceleration in px/s² (3400),
	// InertiaMaxSpeed in px/s (+Inf), EaseLinearity the inertia pan's ease
	// (0.2).
	NoInertia           bool
	InertiaDeceleration float64
	InertiaMaxSpeed     float64
	EaseLinearity       float64
	// MaxBoundsViscosity slows a drag past MaxBounds: 0 lets it through, 1
	// stops it at the edge.
	MaxBoundsViscosity float64
	// WheelDebounce and WheelPxPerZoom are ScrollWheelZoomHandler's 40 ms
	// and 60 px.
	WheelDebounce  time.Duration
	WheelPxPerZoom float64
	// NoBounceAtZoomLimits stops a pinch at the zoom limits instead of
	// letting it overshoot and snap back (Leaflet's bounceAtZoomLimits, on
	// upstream).
	NoBounceAtZoomLimits bool
	// KeyboardPanDelta is the arrow keys' pan in px (80).
	KeyboardPanDelta float64
}

// DefaultHandlerOptions are Leaflet's defaults, spelled out.
func DefaultHandlerOptions() HandlerOptions { return HandlerOptions{}.withDefaults() }

// withDefaults fills every zero number with Leaflet's value.
func (o HandlerOptions) withDefaults() HandlerOptions {
	if o.InertiaDeceleration <= 0 {
		o.InertiaDeceleration = 3400
	}
	if o.InertiaMaxSpeed <= 0 {
		o.InertiaMaxSpeed = math.Inf(1)
	}
	if o.EaseLinearity <= 0 {
		o.EaseLinearity = 0.2
	}
	if o.WheelDebounce <= 0 {
		o.WheelDebounce = 40 * time.Millisecond
	}
	if o.WheelPxPerZoom <= 0 {
		o.WheelPxPerZoom = 60
	}
	if o.KeyboardPanDelta <= 0 {
		o.KeyboardPanDelta = 80
	}
	return o
}

// ---- drag: DragHandler + Draggable ------------------------------------------

// dragHandler pans the view by the pointer's offset from its press origin,
// keeps the last 50 ms of positions for the inertia, and limits the offset
// viscously at MaxBounds.
type dragHandler struct {
	active      bool
	origin      Point
	startCenter LatLng
	positions   []Point
	times       []time.Time
	lastPos     Point
	lastTime    time.Time
	offsetLimit Bounds
	hasLimit    bool
	viscosity   float64
	lastOffset  Point
}

const dragInertiaWindow = 50 * time.Millisecond

// start begins a drag at the press origin (container px).
func (d *dragHandler) start(v *View, origin Point, now time.Time, opts HandlerOptions) {
	v.Stop()
	d.active = true
	d.origin = origin
	d.startCenter = v.Center()
	d.lastOffset = Point{}
	d.positions, d.times = d.positions[:0], d.times[:0]
	d.hasLimit = false
	if mb := v.MaxBounds(); mb.IsValid() && opts.MaxBoundsViscosity > 0 {
		d.offsetLimit = BoundsOf(
			v.LatLngToContainerPoint(mb.GetNorthWest()).MultiplyBy(-1),
			v.LatLngToContainerPoint(mb.GetSouthEast()).MultiplyBy(-1).Add(v.Size()))
		d.hasLimit = true
		d.viscosity = math.Min(1, math.Max(0, opts.MaxBoundsViscosity))
	}
	v.MoveStart(false)
	// The inertia samples begin with the first moved position (Leaflet's
	// _onDrag), not the press origin: with the origin in, a flick shorter
	// than the 50 ms window would count the origin-to-first-sample
	// distance at zero elapsed time and fly faster than upstream.
}

func viscousLimit(value, threshold, viscosity float64) float64 {
	return value - (value-threshold)*viscosity
}

// move applies a pointer position (container px) during the drag.
func (d *dragHandler) move(v *View, pos Point, now time.Time, opts HandlerOptions) {
	if !d.active {
		return
	}
	offset := pos.Subtract(d.origin)
	if d.hasLimit && d.viscosity > 0 {
		l := d.offsetLimit
		if offset.X < l.Min.X {
			offset.X = viscousLimit(offset.X, l.Min.X, d.viscosity)
		}
		if offset.Y < l.Min.Y {
			offset.Y = viscousLimit(offset.Y, l.Min.Y, d.viscosity)
		}
		if offset.X > l.Max.X {
			offset.X = viscousLimit(offset.X, l.Max.X, d.viscosity)
		}
		if offset.Y > l.Max.Y {
			offset.Y = viscousLimit(offset.Y, l.Max.Y, d.viscosity)
		}
	}
	if offset != d.lastOffset {
		v.MoveTo(v.Unproject(v.Project(d.startCenter).Subtract(offset)), v.Zoom())
		d.lastOffset = offset
	}
	if !opts.NoInertia {
		// Leaflet records the (unlimited) pane position; the offset is it.
		d.lastPos, d.lastTime = d.origin.Add(offset), now
		d.positions = append(d.positions, d.lastPos)
		d.times = append(d.times, now)
		d.prune(now)
	}
}

func (d *dragHandler) prune(now time.Time) {
	for len(d.positions) > 1 && now.Sub(d.times[0]) > dragInertiaWindow {
		d.positions = d.positions[1:]
		d.times = d.times[1:]
	}
}

// end releases the drag; with inertia on and two or more samples in the
// window, the view keeps moving along the last direction and decelerates.
func (d *dragHandler) end(v *View, now time.Time, opts HandlerOptions) {
	if !d.active {
		return
	}
	d.active = false
	noInertia := opts.NoInertia || len(d.times) < 2
	if noInertia {
		v.MoveEnd(false)
		return
	}
	d.prune(now)
	if len(d.times) < 2 {
		v.MoveEnd(false)
		return
	}
	direction := d.lastPos.Subtract(d.positions[0])
	duration := float64(d.lastTime.Sub(d.times[0])) / float64(time.Second)
	if duration <= 0 {
		v.MoveEnd(false)
		return
	}
	ease := opts.EaseLinearity
	speedVector := direction.MultiplyBy(ease / duration)
	speed := speedVector.DistanceTo(Point{})
	limitedSpeed := math.Min(opts.InertiaMaxSpeed, speed)
	limitedSpeedVector := speedVector
	if speed > 0 {
		limitedSpeedVector = speedVector.MultiplyBy(limitedSpeed / speed)
	}
	decelerationDuration := limitedSpeed / (opts.InertiaDeceleration * ease)
	offset := limitedSpeedVector.MultiplyBy(-decelerationDuration / 2).Round()
	if offset.X == 0 && offset.Y == 0 {
		v.MoveEnd(false)
		return
	}
	offset = v.LimitOffset(offset, v.MaxBounds())
	v.PanByAnimated(offset, AnimateOptions{
		Animate:       AnimateYes,
		Duration:      time.Duration(decelerationDuration * float64(time.Second)),
		EaseLinearity: ease,
		NoMoveStart:   true,
	})
}

// ---- wheel: ScrollWheelZoomHandler -----------------------------------------

// wheelHandler accumulates wheel pixels and zooms WheelDebounce after the
// first of them, through Leaflet's sigmoid, about the last pointer position.
type wheelHandler struct {
	delta    float64
	start    time.Time
	lastPos  Point
	hasStart bool
}

// wheel records wheel pixels (positive = zoom in) at a container position.
func (w *wheelHandler) wheel(deltaPx float64, pos Point, now time.Time) {
	w.delta += deltaPx
	w.lastPos = pos
	if !w.hasStart {
		w.start, w.hasStart = now, true
	}
}

// tick performs the zoom once the debounce has elapsed.
func (w *wheelHandler) tick(v *View, now time.Time, opts HandlerOptions) {
	if !w.hasStart || now.Sub(w.start) < opts.WheelDebounce {
		return
	}
	w.perform(v, opts)
}

// perform is _performZoom.
func (w *wheelHandler) perform(v *View, opts HandlerOptions) {
	delta := w.delta
	w.delta, w.hasStart = 0, false
	if delta == 0 {
		return
	}
	zoom := v.TargetZoom()
	snap := v.ZoomSnap()
	d2 := delta / (opts.WheelPxPerZoom * 4)
	d3 := 4 * math.Log(2/(1+math.Exp(-math.Abs(d2)))) / math.Ln2
	d4 := d3
	if snap != 0 {
		d4 = math.Ceil(d3/snap) * snap
	}
	if delta < 0 {
		d4 = -d4
	}
	dz := v.LimitZoom(zoom+d4) - zoom
	if dz == 0 {
		return
	}
	v.SetZoomAroundAnimated(w.lastPos, zoom+dz, AnimateOptions{})
}

// ---- pinch: PinchZoomHandler over egui's zoom factor --------------------------

// pinchHandler zooms by the multiplicative factors egui reports for a pinch
// (or ctrl+wheel), about the reported anchor, without snapping while the
// gesture lasts; a gesture ends after pinchIdle without a step and snaps to
// the zoom limits with the zoom animation, as Leaflet's _onPointerEnd does.
type pinchHandler struct {
	active    bool
	startZoom float64
	scale     float64
	anchor    Point
	anchorGeo LatLng
	center    LatLng
	zoom      float64
	lastStep  time.Time
}

const pinchIdle = 120 * time.Millisecond

// step applies one frame's zoom factor at an anchor.
func (p *pinchHandler) step(v *View, factor float64, anchor Point, now time.Time, opts HandlerOptions) {
	if factor <= 0 || factor == 1 {
		return
	}
	if !p.active {
		v.Stop()
		p.active = true
		p.startZoom = v.Zoom()
		p.scale = 1
		p.anchor = anchor
		p.anchorGeo = v.ContainerPointToLatLng(anchor)
		v.MoveStart(true)
	}
	p.scale *= factor
	p.zoom = v.ScaleZoomAt(p.scale, p.startZoom)
	if opts.NoBounceAtZoomLimits &&
		((p.zoom < v.MinZoom() && p.scale < 1) || (p.zoom > v.MaxZoom() && p.scale > 1)) {
		p.zoom = v.LimitZoom(p.zoom)
	}
	// The anchor may drift (a two-finger pan); keep its geography under it.
	p.anchor = anchor
	p.center = v.centerKeeping(p.anchorGeo, anchor, p.zoom)
	p.lastStep = now
	v.events.Animating = true
	v.MoveTo(p.center, p.zoom)
}

// tick ends the gesture when no step has arrived for pinchIdle.
func (p *pinchHandler) tick(v *View, now time.Time) {
	if !p.active || now.Sub(p.lastStep) < pinchIdle {
		return
	}
	p.active = false
	v.AnimateZoomTo(p.center, v.LimitZoom(p.zoom))
}

// ---- double click -------------------------------------------------------------

// doubleClick is DoubleClickZoomHandler: a level in about the point, out
// with shift.
func doubleClick(v *View, at Point, shift bool) {
	delta := v.ZoomDelta()
	if shift {
		delta = -delta
	}
	v.SetZoomAroundAnimated(at, v.TargetZoom()+delta, AnimateOptions{})
}

// ---- box zoom: BoxZoomHandler -----------------------------------------------

// boxZoom draws a rectangle from a shift-press and fits it on release.
type boxZoom struct {
	active bool
	moved  bool
	start  Point
	point  Point
}

func (b *boxZoom) begin(start Point) {
	b.active, b.moved = true, false
	b.start, b.point = start, start
}

func (b *boxZoom) move(pos Point) {
	if !b.active {
		return
	}
	if pos != b.start {
		b.moved = true
	}
	b.point = pos
}

// rect is the box in container pixels while it is being drawn.
func (b *boxZoom) rect() (Bounds, bool) {
	if !b.active || !b.moved {
		return Bounds{}, false
	}
	return BoundsOf(b.start, b.point), true
}

// finish ends the gesture; a box that moved is fitted.
func (b *boxZoom) finish(v *View) {
	if !b.active {
		return
	}
	b.active = false
	if !b.moved {
		return
	}
	bounds := LatLngBoundsOf(v.ContainerPointToLatLng(b.start), v.ContainerPointToLatLng(b.point))
	_ = v.FitBoundsAnimated(bounds, FitOptions{}, AnimateOptions{})
}

// cancel is the Escape key.
func (b *boxZoom) cancel() { b.active, b.moved = false, false }

// ---- keyboard: KeyboardHandler's pan ------------------------------------------

// keyboardPan pans by the arrow keys: KeyboardPanDelta px, three times that
// with shift, limited to MaxBounds, ignored while a pan animates. The zoom
// keys (+/−) need keycodes this tree's vocabulary does not carry yet.
func keyboardPan(v *View, dx, dy float64, shift bool, opts HandlerOptions) {
	if v.anim.pan != nil {
		return
	}
	offset := Point{dx, dy}.MultiplyBy(opts.KeyboardPanDelta)
	if shift {
		offset = offset.MultiplyBy(3)
	}
	if v.MaxBounds().IsValid() {
		offset = v.LimitOffset(offset, v.MaxBounds())
	}
	v.PanByAnimated(offset, AnimateOptions{})
}
