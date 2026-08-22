package portolan

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// View is the map's camera — the view state of src/map/Map.js without its
// DOM: a CRS, a centre, a zoom and a viewport size in pixels, plus the zoom
// limits, the zoom snapping and the optional max bounds that constrain them.
// Everything that projects between geography and the viewport goes through
// it.
//
// Leaflet moves the map pane by a pixel offset during a drag or a pan
// animation and derives the centre from that offset; here the centre is the
// state and the pane offset is always zero, so layer points and container
// points coincide. The handlers (M3) move the centre directly.
//
// Events are recorded per frame in a ViewEvents and read with TakeEvents;
// there is no listener list.
type View struct {
	crs    CRSI
	center LatLng
	zoom   float64
	size   Point
	loaded bool

	hasMinZoom, hasMaxZoom bool
	minZoom, maxZoom       float64
	// The tile source's limits, consulted when the view's own are unset —
	// Leaflet's _layersMinZoom/_layersMaxZoom.
	hasLayersMin, hasLayersMax   bool
	layersMinZoom, layersMaxZoom float64
	zoomSnap, zoomDelta          float64
	maxBounds                    LatLngBounds

	pixelOrigin Point
	events      ViewEvents
	// enforcingBounds breaks the moveend → panInsideBounds → moveend loop,
	// as Leaflet's _enforcingBounds does.
	enforcingBounds bool
}

// ViewEvents are the Leaflet map events a frame produced, as flags. Move
// covers any centre or zoom change; the Start/End pairs bracket a gesture or
// a programmatic view change.
type ViewEvents struct {
	MoveStart, Move, MoveEnd bool
	ZoomStart, Zoom, ZoomEnd bool
	// ViewReset is a hard view change (setView without animation); Load is
	// the first.
	ViewReset, Load bool
}

// Any reports whether anything happened.
func (e ViewEvents) Any() bool {
	return e.MoveStart || e.Move || e.MoveEnd || e.ZoomStart || e.Zoom || e.ZoomEnd || e.ViewReset || e.Load
}

// ViewOptions configures a View. The zero value is Leaflet's defaults except
// for ZoomSnap, which is 0 (continuous zoom — what this tree's map does today,
// ADR-0204 §SD6) where Leaflet's is 1.
type ViewOptions struct {
	// CRS defaults to EPSG3857.
	CRS CRSI
	// Size is the viewport in pixels; SetSize changes it later.
	Size Point
	// MinZoom/MaxZoom, when set, override the tile source's.
	MinZoom, MaxZoom       float64
	HasMinZoom, HasMaxZoom bool
	// ZoomSnap forces the zoom to multiples of itself (0 = off); ZoomDelta is
	// the step of ZoomIn/ZoomOut and the double-click (0 = 1).
	ZoomSnap, ZoomDelta float64
	// MaxBounds, when valid, is the area the view is kept inside.
	MaxBounds LatLngBounds
}

// NewView makes an unloaded view: nothing projects until SetView.
func NewView(opts ViewOptions) *View {
	v := &View{
		crs:        opts.CRS,
		size:       opts.Size,
		hasMinZoom: opts.HasMinZoom, minZoom: opts.MinZoom,
		hasMaxZoom: opts.HasMaxZoom, maxZoom: opts.MaxZoom,
		zoomSnap:  opts.ZoomSnap,
		zoomDelta: opts.ZoomDelta,
		maxBounds: opts.MaxBounds,
	}
	if v.crs == nil {
		v.crs = EPSG3857
	}
	if v.zoomDelta == 0 {
		v.zoomDelta = 1
	}
	return v
}

// Loaded reports whether SetView has been called at least once.
func (v *View) Loaded() bool { return v.loaded }

// CRS is the view's coordinate reference system.
func (v *View) CRS() CRSI { return v.crs }

// Center is the geographic centre of the viewport.
func (v *View) Center() LatLng { return v.center }

// Zoom is the current zoom, fractional when the view allows it.
func (v *View) Zoom() float64 { return v.zoom }

// Size is the viewport in pixels.
func (v *View) Size() Point { return v.size }

// SetSize changes the viewport, keeping the centre where it is (Leaflet's
// invalidateSize with pan: true).
func (v *View) SetSize(size Point) {
	if size == v.size {
		return
	}
	v.size = size
	if v.loaded {
		v.pixelOrigin = v.newPixelOrigin(v.center, v.zoom)
	}
}

// ZoomSnap is the snapping step, 0 for continuous zoom.
func (v *View) ZoomSnap() float64 { return v.zoomSnap }

// ZoomDelta is the step of ZoomIn/ZoomOut.
func (v *View) ZoomDelta() float64 { return v.zoomDelta }

// MinZoom is the effective lower zoom limit: the view's own, else the tile
// source's, else 0.
func (v *View) MinZoom() float64 {
	switch {
	case v.hasMinZoom:
		return v.minZoom
	case v.hasLayersMin:
		return v.layersMinZoom
	}
	return 0
}

// MaxZoom is the effective upper zoom limit: the view's own, else the tile
// source's, else +Inf.
func (v *View) MaxZoom() float64 {
	switch {
	case v.hasMaxZoom:
		return v.maxZoom
	case v.hasLayersMax:
		return v.layersMaxZoom
	}
	return math.Inf(1)
}

// SetMinZoom sets the view's own lower limit and zooms in to it if needed.
func (v *View) SetMinZoom(zoom float64) {
	old, had := v.minZoom, v.hasMinZoom
	v.minZoom, v.hasMinZoom = zoom, true
	if v.loaded && (!had || old != zoom) && v.zoom < zoom {
		v.SetZoom(zoom)
	}
}

// SetMaxZoom sets the view's own upper limit and zooms out to it if needed.
func (v *View) SetMaxZoom(zoom float64) {
	old, had := v.maxZoom, v.hasMaxZoom
	v.maxZoom, v.hasMaxZoom = zoom, true
	if v.loaded && (!had || old != zoom) && v.zoom > zoom {
		v.SetZoom(zoom)
	}
}

// SetLayerZoomLimits is what a tile source reports as its zoom range; it
// applies where the view's own limits are unset (Leaflet's _updateZoomLevels).
// A zoom outside the new range moves to its edge, before the first SetView
// too — there SetZoom only records it, as upstream does.
func (v *View) SetLayerZoomLimits(minZoom, maxZoom float64, hasMin, hasMax bool) {
	v.layersMinZoom, v.hasLayersMin = minZoom, hasMin
	v.layersMaxZoom, v.hasLayersMax = maxZoom, hasMax
	if !v.hasMaxZoom && hasMax && v.zoom > maxZoom {
		v.SetZoom(maxZoom)
	}
	if !v.hasMinZoom && hasMin && v.zoom < minZoom {
		v.SetZoom(minZoom)
	}
}

// MaxBounds is the area the view is kept inside; invalid when none.
func (v *View) MaxBounds() LatLngBounds { return v.maxBounds }

// SetMaxBounds sets or clears (invalid bounds) the area the view is kept
// inside, and pans inside it at once when loaded.
func (v *View) SetMaxBounds(bounds LatLngBounds) {
	v.maxBounds = bounds
	if bounds.IsValid() && v.loaded {
		v.panInsideMaxBounds()
	}
}

// TakeEvents returns the events recorded since the last call and clears them.
func (v *View) TakeEvents() (e ViewEvents) {
	e, v.events = v.events, ViewEvents{}
	return
}

// ---- projections and conversions ----------------------------------------

// Project takes a geographic point to the CRS's pixel space at the current
// zoom.
func (v *View) Project(ll LatLng) Point { return v.crs.LatLngToPoint(ll, v.zoom) }

// ProjectAt is Project at an explicit zoom.
func (v *View) ProjectAt(ll LatLng, zoom float64) Point { return v.crs.LatLngToPoint(ll, zoom) }

// Unproject inverts Project.
func (v *View) Unproject(p Point) LatLng { return v.crs.PointToLatLng(p, v.zoom) }

// UnprojectAt inverts ProjectAt.
func (v *View) UnprojectAt(p Point, zoom float64) LatLng { return v.crs.PointToLatLng(p, zoom) }

// ZoomScale is the scale factor between two zooms (Leaflet's getZoomScale);
// a NaN fromZoom means the current zoom.
func (v *View) ZoomScale(toZoom, fromZoom float64) float64 {
	if math.IsNaN(fromZoom) {
		fromZoom = v.zoom
	}
	return v.crs.Scale(toZoom) / v.crs.Scale(fromZoom)
}

// ScaleZoom is the zoom reached by scaling fromZoom (NaN = current) by
// scale; +Inf when the arithmetic has no answer (Leaflet's getScaleZoom).
func (v *View) ScaleZoom(scale, fromZoom float64) float64 {
	if math.IsNaN(fromZoom) {
		fromZoom = v.zoom
	}
	zoom := v.crs.Zoom(scale * v.crs.Scale(fromZoom))
	if math.IsNaN(zoom) {
		return math.Inf(1)
	}
	return zoom
}

// PixelOrigin is the top-left of the viewport in projected pixels at the
// current zoom, rounded to whole pixels — what layer points are relative to.
func (v *View) PixelOrigin() Point { return v.pixelOrigin }

func (v *View) newPixelOrigin(center LatLng, zoom float64) Point {
	viewHalf := v.size.DivideBy(2)
	return v.ProjectAt(center, zoom).Subtract(viewHalf).Round()
}

// PixelBounds is the viewport in projected pixels at the current zoom.
func (v *View) PixelBounds() Bounds {
	return BoundsOf(v.pixelOrigin, v.pixelOrigin.Add(v.size))
}

// PixelBoundsAt is the viewport in projected pixels for a hypothetical centre
// and zoom.
func (v *View) PixelBoundsAt(center LatLng, zoom float64) Bounds {
	tl := v.newPixelOrigin(center, zoom)
	return BoundsOf(tl, tl.Add(v.size))
}

// PixelWorldBounds is the whole world in projected pixels at a zoom (NaN =
// current); false for an infinite CRS.
func (v *View) PixelWorldBounds(zoom float64) (Bounds, bool) {
	if math.IsNaN(zoom) {
		zoom = v.zoom
	}
	return v.crs.GetProjectedBounds(zoom)
}

// LayerPointToLatLng converts a point relative to the pixel origin.
func (v *View) LayerPointToLatLng(p Point) LatLng { return v.Unproject(p.Add(v.pixelOrigin)) }

// LatLngToLayerPoint converts to a point relative to the pixel origin,
// rounded to whole pixels as Leaflet does.
func (v *View) LatLngToLayerPoint(ll LatLng) Point {
	return v.Project(ll).Round().Subtract(v.pixelOrigin)
}

// ContainerPointToLatLng converts a viewport point (origin top-left) to
// geography. With no pane offset it is LayerPointToLatLng.
func (v *View) ContainerPointToLatLng(p Point) LatLng { return v.LayerPointToLatLng(p) }

// LatLngToContainerPoint converts geography to a viewport point.
func (v *View) LatLngToContainerPoint(ll LatLng) Point { return v.LatLngToLayerPoint(ll) }

// WrapLatLng wraps a point into the CRS's wrap range.
func (v *View) WrapLatLng(ll LatLng) LatLng { return v.crs.WrapLatLng(ll) }

// WrapLatLngBounds shifts a bounds so its centre is in the wrap range.
func (v *View) WrapLatLngBounds(b LatLngBounds) LatLngBounds { return v.crs.WrapLatLngBounds(b) }

// Distance is the CRS's distance between two points.
func (v *View) Distance(a, b LatLng) float64 { return v.crs.Distance(a, b) }

// Bounds is the geographic extent of the viewport.
func (v *View) Bounds() LatLngBounds {
	b := v.PixelBounds()
	return LatLngBoundsOf(v.Unproject(b.GetBottomLeft()), v.Unproject(b.GetTopRight()))
}

// BoundsZoom is the highest zoom at which bounds fits the viewport less
// padding (inside=false), or the lowest at which it fills it (inside=true),
// snapped and clamped to the zoom limits — Leaflet's getBoundsZoom.
func (v *View) BoundsZoom(bounds LatLngBounds, inside bool, padding Point) float64 {
	zoom := v.zoom
	minZ, maxZ := v.MinZoom(), v.MaxZoom()
	nw, se := bounds.GetNorthWest(), bounds.GetSouthEast()
	size := v.size.Subtract(padding)
	boundsSize := BoundsOf(v.ProjectAt(se, zoom), v.ProjectAt(nw, zoom)).GetSize()
	snap := v.zoomSnap
	scalex, scaley := size.X/boundsSize.X, size.Y/boundsSize.Y
	var scale float64
	if inside {
		scale = math.Max(scalex, scaley)
	} else {
		scale = math.Min(scalex, scaley)
	}
	zoom = v.ScaleZoom(scale, zoom)
	if snap != 0 {
		// don't jump if within 1% of a snap level
		zoom = jsRound(zoom/(snap/100)) * (snap / 100)
		if inside {
			zoom = math.Ceil(zoom/snap) * snap
		} else {
			zoom = math.Floor(zoom/snap) * snap
		}
	}
	return math.Max(minZ, math.Min(maxZ, zoom))
}

// ---- view changes -----------------------------------------------------------

// SetView moves to a centre and zoom at once, without animation: zoom is
// snapped and clamped, the centre is kept inside MaxBounds, and the events of
// a hard view change are recorded (Leaflet's setView → _resetView).
func (v *View) SetView(center LatLng, zoom float64) {
	zoom = v.LimitZoom(zoom)
	center = v.LimitCenter(center, zoom, v.maxBounds)
	v.resetView(center, zoom, false)
}

// SetZoom changes the zoom about the centre; before the first SetView it just
// records the zoom.
func (v *View) SetZoom(zoom float64) {
	if !v.loaded {
		v.zoom = zoom
		return
	}
	v.SetView(v.center, zoom)
}

// ZoomIn zooms in by delta, or ZoomDelta when delta is 0.
func (v *View) ZoomIn(delta float64) {
	if delta == 0 {
		delta = v.zoomDelta
	}
	v.SetZoom(v.zoom + delta)
}

// ZoomOut zooms out by delta, or ZoomDelta when delta is 0.
func (v *View) ZoomOut(delta float64) {
	if delta == 0 {
		delta = v.zoomDelta
	}
	v.SetZoom(v.zoom - delta)
}

// SetZoomAround zooms so the geography under a viewport point stays under
// it — the wheel's and the double-click's zoom.
func (v *View) SetZoomAround(containerPoint Point, zoom float64) {
	scale := v.ZoomScale(zoom, math.NaN())
	viewHalf := v.size.DivideBy(2)
	centerOffset := containerPoint.Subtract(viewHalf).MultiplyBy(1 - 1/scale)
	newCenter := v.ContainerPointToLatLng(viewHalf.Add(centerOffset))
	v.SetView(newCenter, zoom)
}

// SetZoomAroundLatLng is SetZoomAround with the anchor given in geography.
func (v *View) SetZoomAroundLatLng(ll LatLng, zoom float64) {
	v.SetZoomAround(v.LatLngToContainerPoint(ll), zoom)
}

// FitOptions are the paddings and cap of FitBounds.
type FitOptions struct {
	PaddingTopLeft, PaddingBottomRight Point
	// Padding applies to both corners when the per-corner ones are zero.
	Padding Point
	// MaxZoom caps the resulting zoom when HasMaxZoom.
	MaxZoom    float64
	HasMaxZoom bool
}

func (o FitOptions) paddings() (tl, br Point) {
	tl, br = o.PaddingTopLeft, o.PaddingBottomRight
	if tl == (Point{}) {
		tl = o.Padding
	}
	if br == (Point{}) {
		br = o.Padding
	}
	return
}

// BoundsCenterZoom is the centre and zoom FitBounds would move to (Leaflet's
// _getBoundsCenterZoom).
func (v *View) BoundsCenterZoom(bounds LatLngBounds, opts FitOptions) (center LatLng, zoom float64) {
	paddingTL, paddingBR := opts.paddings()
	zoom = v.BoundsZoom(bounds, false, paddingTL.Add(paddingBR))
	if opts.HasMaxZoom {
		zoom = math.Min(opts.MaxZoom, zoom)
	}
	if math.IsInf(zoom, 1) {
		return bounds.GetCenter(), zoom
	}
	paddingOffset := paddingBR.Subtract(paddingTL).DivideBy(2)
	swPoint := v.ProjectAt(bounds.GetSouthWest(), zoom)
	nePoint := v.ProjectAt(bounds.GetNorthEast(), zoom)
	center = v.UnprojectAt(swPoint.Add(nePoint).DivideBy(2).Add(paddingOffset), zoom)
	return center, zoom
}

// FitBounds moves the view to show bounds as large as possible. An invalid
// bounds is an error, as in Leaflet.
func (v *View) FitBounds(bounds LatLngBounds, opts FitOptions) error {
	if !bounds.IsValid() {
		return eh.Errorf("portolan: bounds are not valid")
	}
	center, zoom := v.BoundsCenterZoom(bounds, opts)
	v.SetView(center, zoom)
	return nil
}

// FitWorld fits the whole world.
func (v *View) FitWorld(opts FitOptions) {
	_ = v.FitBounds(LatLngBoundsOf(LL(-90, -180), LL(90, 180)), opts)
}

// PanTo moves the centre without changing the zoom.
func (v *View) PanTo(center LatLng) { v.SetView(center, v.zoom) }

// PanBy moves the view by a pixel offset, rounded to whole pixels as
// Leaflet's panBy does; (0,0) only records moveend.
func (v *View) PanBy(offset Point) {
	offset = offset.Round()
	if offset.X == 0 && offset.Y == 0 {
		v.events.MoveEnd = true
		return
	}
	v.resetView(v.Unproject(v.Project(v.center).Add(offset)), v.zoom, false)
}

// PanInsideBounds pans the least distance that puts the viewport inside
// bounds.
func (v *View) PanInsideBounds(bounds LatLngBounds) {
	v.enforcingBounds = true
	newCenter := v.LimitCenter(v.center, v.zoom, bounds)
	if !v.center.Equals(newCenter) {
		v.PanTo(newCenter)
	}
	v.enforcingBounds = false
}

// PanInside pans the least distance that brings a point into the padded
// viewport.
func (v *View) PanInside(ll LatLng, paddingTL, paddingBR Point) {
	pixelCenter := v.Project(v.center)
	pixelPoint := v.Project(ll)
	pixelBounds := v.PixelBounds()
	paddedBounds := BoundsOf(pixelBounds.Min.Add(paddingTL), pixelBounds.Max.Subtract(paddingBR))
	paddedSize := paddedBounds.GetSize()
	if paddedBounds.Contains(pixelPoint) {
		return
	}
	v.enforcingBounds = true
	centerOffset := pixelPoint.Subtract(paddedBounds.GetCenter())
	offset := paddedBounds.Extend(pixelPoint).GetSize().Subtract(paddedSize)
	if centerOffset.X < 0 {
		pixelCenter.X -= offset.X
	} else {
		pixelCenter.X += offset.X
	}
	if centerOffset.Y < 0 {
		pixelCenter.Y -= offset.Y
	} else {
		pixelCenter.Y += offset.Y
	}
	v.PanTo(v.Unproject(pixelCenter))
	v.enforcingBounds = false
}

// MoveStart records the start of a gesture (Leaflet's _moveStart).
func (v *View) MoveStart(zoomChanged bool) {
	if zoomChanged {
		v.events.ZoomStart = true
	}
	v.events.MoveStart = true
}

// MoveTo is a gesture's per-frame view change (Leaflet's _move): the centre
// and zoom are taken as given — no limits, no rounding — and only move and
// zoom are recorded. The handlers pair it with MoveStart and MoveEnd.
func (v *View) MoveTo(center LatLng, zoom float64) {
	zoomChanged := v.zoom != zoom
	v.zoom = zoom
	v.center = center
	v.loaded = true
	v.pixelOrigin = v.newPixelOrigin(center, zoom)
	if zoomChanged {
		v.events.Zoom = true
	}
	v.events.Move = true
}

// MoveEnd records the end of a gesture (Leaflet's _moveEnd) and, with
// MaxBounds set, pans back inside them.
func (v *View) MoveEnd(zoomChanged bool) {
	if zoomChanged {
		v.events.ZoomEnd = true
	}
	v.events.MoveEnd = true
	v.panInsideMaxBounds()
}

// resetView is Leaflet's _resetView: a hard view change with its full event
// sequence.
func (v *View) resetView(center LatLng, zoom float64, noMoveStart bool) {
	loading := !v.loaded
	v.loaded = true
	zoom = v.LimitZoom(zoom)
	zoomChanged := v.zoom != zoom
	if zoomChanged {
		v.events.ZoomStart = true
	}
	if !noMoveStart {
		v.events.MoveStart = true
	}
	v.MoveTo(center, zoom)
	v.MoveEnd(zoomChanged)
	v.events.ViewReset = true
	if loading {
		v.events.Load = true
	}
}

func (v *View) panInsideMaxBounds() {
	if !v.enforcingBounds && v.maxBounds.IsValid() {
		v.PanInsideBounds(v.maxBounds)
	}
}

// ---- limits -----------------------------------------------------------------

// LimitZoom snaps zoom to ZoomSnap and clamps it to the zoom limits.
func (v *View) LimitZoom(zoom float64) float64 {
	minZ, maxZ := v.MinZoom(), v.MaxZoom()
	if snap := v.zoomSnap; snap != 0 {
		zoom = jsRound(zoom/snap) * snap
	}
	return math.Max(minZ, math.Min(maxZ, zoom))
}

// LimitCenter is the nearest centre to center whose viewport, at zoom, lies
// inside bounds; an invalid bounds limits nothing. Movements of at most one
// pixel are ignored, as Leaflet's _limitCenter does.
func (v *View) LimitCenter(center LatLng, zoom float64, bounds LatLngBounds) LatLng {
	if !bounds.IsValid() {
		return center
	}
	centerPoint := v.ProjectAt(center, zoom)
	viewHalf := v.size.DivideBy(2)
	viewBounds := BoundsOf(centerPoint.Subtract(viewHalf), centerPoint.Add(viewHalf))
	offset := v.boundsOffset(viewBounds, bounds, zoom)
	if math.Abs(offset.X) <= 1 && math.Abs(offset.Y) <= 1 {
		return center
	}
	return v.UnprojectAt(centerPoint.Add(offset), zoom)
}

// LimitOffset trims a pan offset so the viewport stays inside bounds.
func (v *View) LimitOffset(offset Point, bounds LatLngBounds) Point {
	if !bounds.IsValid() {
		return offset
	}
	viewBounds := v.PixelBounds()
	newBounds := BoundsOf(viewBounds.Min.Add(offset), viewBounds.Max.Add(offset))
	return offset.Add(v.boundsOffset(newBounds, bounds, v.zoom))
}

func (v *View) boundsOffset(pxBounds Bounds, maxBounds LatLngBounds, zoom float64) Point {
	projectedMaxBounds := BoundsOf(
		v.ProjectAt(maxBounds.GetNorthEast(), zoom),
		v.ProjectAt(maxBounds.GetSouthWest(), zoom))
	minOffset := projectedMaxBounds.Min.Subtract(pxBounds.Min)
	maxOffset := projectedMaxBounds.Max.Subtract(pxBounds.Max)
	return Point{rebound(minOffset.X, -maxOffset.X), rebound(minOffset.Y, -maxOffset.Y)}
}

func rebound(left, right float64) float64 {
	if left+right > 0 {
		return jsRound(left-right) / 2
	}
	return math.Max(0, math.Ceil(left)) - math.Max(0, math.Floor(right))
}
