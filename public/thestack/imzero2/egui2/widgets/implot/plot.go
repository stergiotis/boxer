package implot

import (
	"iter"
	"math"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/legend"
)

// Typography and tick geometry — palette-independent (the chrome colors
// live in chrome.go behind SetChrome, the data-series colors in palette.go
// behind SetSeriesPalette).
const (
	tickFontSize  = 10.5
	labelFontSize = 12.0
	titleFontSize = 13.5
	tickLen       = 4.0
	// charW is one tick-label glyph's width — the estimation idiom of
	// text.go, pre-multiplied for the tick font because every use of it is
	// at that size. Prefer EstimateTextWidth where a label may not be ASCII.
	charW = tickFontSize * GlyphWidthRatio
)

// axisState is the retained per-axis half of a plot's state.
type axisState struct {
	rng      Range
	fitNext  bool
	hasRange bool
	flags    AxisFlags
	label    string
	scale    ScaleE
	// loc is a ScaleTime axis's zone (SetupAxisTimeZone); nil is UTC.
	loc *time.Location
	// touched: a gesture moved this axis; AxisFlagsFollow stops
	// refitting until the next explicit fit clears it.
	touched bool

	// Axis links (SetupAxisLinks): caller-held shared range endpoints and
	// the values this plot last wrote, distinguishing an external move
	// from this plot's own gesture.
	linkMin, linkMax         *float64
	lastLinkMin, lastLinkMax float64

	// Viewport constraints (SetupAxisLimitsConstraints): the visible
	// range is clamped inside [consMin, consMax] after gestures and fits.
	consMin, consMax float64
	consOk           bool
}

// plotState is the retained state of one plot id across frames — the
// ImPool slot of the port. Interaction gestures read it one frame behind.
type plotState struct {
	x, y        axisState
	initialized bool
	onceApplied bool

	// previous frame's transform + plot-area rect (canvas px), the basis
	// for interpreting this frame's gestures.
	prev   transform
	prevOk bool
	// prevWheelScrollsPane is WheelScrollsPane as the previous frame's
	// canvas was emitted under: whether it left the plain wheel to the pane.
	prevWheelScrollsPane bool
	dragging             bool
	dragBox              bool
	boxStart             [2]float32
	boxCur               [2]float32
	lastDrag             [2]float32
	hoverPos             [2]float32
	hoverOk              bool

	// Legend state: per-label visibility toggles (clicks) and the label
	// hovered last frame (series highlight). Same-label items share one
	// legend entry and palette slot, as in ImPlot's label→item registry.
	hidden      map[string]bool
	legendHover string
	legendItems []legend.Item // this frame's rows, reused across frames
	heatCache   map[string]*heatTex
	imgSent     map[string]uint64

	// Range gestures the person made, finished, settled and handed to the
	// next frame's handle (gesture.go).
	gestures gestureTrack

	// The per-series readout of the last End (seriesreadout.go): the x it
	// read at and a row per series, reused across frames.
	readout   []ReadoutRow
	readoutX  float64
	readoutOk bool

	// Context-menu state: open flag, screen anchor, and an open-counter
	// that salts the window id so each opening re-anchors at the pointer.
	ctxOpen   bool
	ctxScreen [2]float32
	ctxSeq    uint64

	// scratch buffers reused across frames to keep steady-state allocation flat.
	scratchX []float32
	scratchY []float32
	ticksX   []tick
	ticksY   []tick
	// Tick-label placement, one band per axis (ticklabels.go). Retained
	// with the rest of the frame scratch, not to carry state between
	// frames — each frame's layout refills them from scratch.
	xBand labelBand
	yBand labelBand
}

var pool = make(map[uint64]*plotState, 8)

// seriesFrame is one item call, held until End so auto-fit sees the whole
// frame's data before the ranges freeze. Fields beyond xs/ys apply per
// kind: marker+radius for scatter, width for bars (the bar height of
// horizontal ones), yref for shaded/stems/bars (on the x axis for
// horizontal bars), neg/pos for error bars. slot is the palette slot assigned per label in
// first-declaration order.
type seriesFrame struct {
	kind     seriesKind
	label    string
	xs, ys   []float64
	neg, pos []float64
	ys2      []float64
	marker   MarkerE
	radius   float32
	width    float64
	yref     float64
	slot     int
	colHex   uint32
	colOk    bool
	weight   float32
	heat     *heatFrame
	pie      *pieFrame
	img      *imageFrame
	boxes    *boxFrame
	txt      *textFrame
	// custom is the kindCustom draw closure (custom.go); unclipped lifts
	// the plot-area clip around its call.
	custom    func(DrawCtx)
	unclipped bool
}

// Plot is the frame-transient handle between Begin and End. Methods follow
// ImPlot's protocol: Setup* first, then items; the first item locks setup.
type Plot struct {
	ids           *c.WidgetIdStack
	st            *plotState
	scopeId       uint64
	w, h          float32
	title         string // full identity string; titleShown is what renders
	titleShown    string
	setupLocked   bool
	series        []seriesFrame
	tools         []toolFrame
	toolPos       [2]float32
	toolPosOk     bool
	dataXMin      float64
	dataXMax      float64
	dataYMin      float64
	dataYMax      float64
	dataOk        bool
	slotByLabel   map[string]int
	nextSlot      int
	digitalOffset float32
	nextColHex    uint32
	nextColOk     bool
	nextWeight    float32
	noInputs      bool
	noLegend      bool
	// wheelScrollsPane leaves the plain wheel to the surrounding pane.
	wheelScrollsPane bool
	// Per-series hover readout and crosshair link (seriesreadout.go).
	hoverSeries bool
	noSeriesBox bool
	crosshair   *CrosshairLink
	// legendPlace is this frame's SetupLegend; legendW/H the box End
	// measured for it before the layout, which an outside legend shrinks.
	legendPlace      LegendPlacementE
	legendW, legendH float32
	noMouseText      bool
	ended            bool
	// emitting is true while End invokes item renderers; item declarations
	// from inside a Custom closure are debug-logged no-ops (they would
	// silently never render — the series loop is already underway).
	emitting  bool
	customSeq uint64 // ordinal of the next Custom item emitted; scopes its hit-region ids
	clickOk   bool
	clickPos  [2]float32
	// gesture is what RangeGesture reports: the gestures that settled last
	// frame, taken over from the plot state at Begin.
	gesture      [2]gestureRec
	xCustomTicks []tick
	yCustomTicks []tick
}

// Begin opens a plot with the given title (which is also its identity, as
// in ImPlot — the "##" convention applies: everything from "##" on is
// identity only and does not render, so "##rates" shows no title bar) and
// canvas size in pixels. Interactions from the previous frame are applied
// to the retained ranges here, before any Setup call. Every Begin must be
// paired with End.
func Begin(ids *c.WidgetIdStack, title string, w float32, h float32) *Plot {
	scope := ids.PrepareStr(title)
	scopeId := scope.DeriveStacked()
	st, ok := pool[scopeId]
	if !ok {
		st = &plotState{hidden: make(map[string]bool, 4)}
		pool[scopeId] = st
	}
	st.beginFrame()
	shown, _, _ := strings.Cut(title, "##")
	p := &Plot{ids: ids, st: st, scopeId: scopeId, w: w, h: h, title: title, titleShown: shown,
		dataXMin: math.Inf(1), dataXMax: math.Inf(-1), dataYMin: math.Inf(1), dataYMax: math.Inf(-1)}
	p.gesture = st.gestures.done
	st.gestures.done = [2]gestureRec{}
	p.applyInteractions()
	return p
}

// beginFrame returns the Setup state a caller re-declares every frame to its
// default, leaving the retained interaction state (ranges, gesture history,
// legend visibility) alone.
//
// The axis scale belongs to the first group and was in the second, which is
// only visible when a caller STOPS asking for a non-linear axis: nothing else
// writes the slot, so the last SetupAxisScale stood forever. Unchecking play's
// `log y` put the checkbox back and left the plot logarithmic (2026-08-15) —
// and a temporal x, once seen, would have kept formatting later numeric
// results as dates. Setup is a per-frame protocol in upstream and here; End
// already consumes the viewport constraints on the same reasoning, and the
// stickiness was an unintended deviation rather than one of the documented
// ones.
func (st *plotState) beginFrame() {
	st.x.scale, st.y.scale = ScaleLinear, ScaleLinear
	st.x.loc, st.y.loc = nil, nil
}

// Scoped opens a plot and yields it exactly once; End runs when the
// body finishes or breaks early, so the id scope always closes — the
// range-based counterpart to Begin/End, mirroring c.IdScope (including
// its deferred pop-on-panic discipline). Prefer it for straight-line
// plot bodies:
//
//	for p := range implot.Scoped(ids, "##rates", w, h) {
//		p.SetupAxes("t", "MiB/s", implot.AxisFlagsNone, implot.AxisFlagsNone)
//		p.Line("rate", xs, ys)
//	}
//
// Begin/End remains for bodies where the handle must outlive a lexical
// block; an explicit End inside a Scoped body is harmless (End is
// idempotent).
func Scoped(ids *c.WidgetIdStack, title string, w float32, h float32) iter.Seq[*Plot] {
	return func(yield func(*Plot) bool) {
		p := Begin(ids, title, w, h)
		defer p.End()
		yield(p)
	}
}

// ScopeId is the id Begin derived for this plot, under which its retained
// state lives until [Release].
func (p *Plot) ScopeId() uint64 { return p.scopeId }

// Release drops the retained state — ranges, gesture history, legend
// visibility — of the plot whose [Plot.ScopeId] is scopeId. The state of a
// plot outlives its last frame, so a caller whose plots come and go, such as
// a notebook's cells, releases each one it will not draw again; a plot drawn
// after its release starts afresh. Call it from the frame goroutine, like
// Begin.
func Release(scopeId uint64) { delete(pool, scopeId) }

// NewDetached returns a plot handle bound to no canvas and no frame,
// for headless tests of widgets that declare into a *Plot: items
// accumulate and fit extents compute, nothing renders. End must not be
// called on it (there is no id stack to close).
func NewDetached() *Plot {
	return &Plot{st: &plotState{hidden: make(map[string]bool, 4)},
		dataXMin: math.Inf(1), dataXMax: math.Inf(-1),
		dataYMin: math.Inf(1), dataYMax: math.Inf(-1)}
}

// canvasHandle / areaHandle derive the same ids the render pass will use,
// without consuming the id-stack state (prepare + Derive is deterministic).
func (p *Plot) canvasHandle() widgethandle.WidgetHandle {
	return widgethandle.Make(p.ids.PrepareStr("implot-canvas").Derive())
}

func (p *Plot) areaHandle() widgethandle.WidgetHandle {
	return widgethandle.Make(p.ids.PrepareStr("implot-area").Derive())
}

// pxWindow is the plot-area rect a gesture manipulates, in canvas pixels.
// Pan and zoom compose here rather than in plot space, so every gesture
// stays correct on any monotone scale (log, symlog), not just linear: the
// window is inverted through the transform once, at the end.
type pxWindow struct{ x0, x1, y0, y1 float32 }

// gestureLocks is the per-axis AxisFlagsNoPan / AxisFlagsNoZoom pair,
// resolved once a frame.
//
// The locks are applied per gesture kind rather than to the resulting
// range, because the two are not the same: an anchored wheel zoom moves an
// axis's centre as well as its span, so restoring only the span afterwards
// would let the wheel pan a NoZoom axis.
type gestureLocks struct{ noPanX, noPanY, noZoomX, noZoomY bool }

func locksOf(x *axisState, y *axisState) gestureLocks {
	return gestureLocks{
		noPanX:  x.flags&AxisFlagsNoPan != 0,
		noPanY:  y.flags&AxisFlagsNoPan != 0,
		noZoomX: x.flags&AxisFlagsNoZoom != 0,
		noZoomY: y.flags&AxisFlagsNoZoom != 0,
	}
}

// pan translates the window by a pixel delta, skipping a NoPan axis, and
// reports which axes moved. A zero delta on an axis does not count as a
// move: a purely vertical drag must not push x through the transform and
// back, which is lossy and would creep frame after frame.
func (g gestureLocks) pan(w *pxWindow, dx float32, dy float32) (movedX bool, movedY bool) {
	if !g.noPanX && dx != 0 {
		w.x0 -= dx
		w.x1 -= dx
		movedX = true
	}
	if !g.noPanY && dy != 0 {
		w.y0 -= dy
		w.y1 -= dy
		movedY = true
	}
	return movedX, movedY
}

// zoom scales the window about the anchor point, skipping a NoZoom axis.
func (g gestureLocks) zoom(w *pxWindow, ax float32, ay float32, factor float32) (movedX bool, movedY bool) {
	if !g.noZoomX {
		w.x0 = ax - (ax-w.x0)/factor
		w.x1 = ax + (w.x1-ax)/factor
		movedX = true
	}
	if !g.noZoomY {
		w.y0 = ay - (ay-w.y0)/factor
		w.y1 = ay + (w.y1-ay)/factor
		movedY = true
	}
	return movedX, movedY
}

// requestFit marks the axes a fit gesture may re-range, skipping a NoZoom
// one. A fit rewrites a span, so it is a zoom by any other name and the axis
// whose range the caller owns declines it exactly as it declines the wheel.
//
// The flag is only ever set here, never cleared: assigning the negation would
// drop a fit already pending from FitNext or from an earlier gesture this
// frame.
func (g gestureLocks) requestFit(x *axisState, y *axisState, wantX bool, wantY bool) (fitX bool, fitY bool) {
	if wantX && !g.noZoomX {
		x.fitNext = true
		fitX = true
	}
	if wantY && !g.noZoomY {
		y.fitNext = true
		fitY = true
	}
	return fitX, fitY
}

// applyInteractions interprets last frame's gesture registers against last
// frame's transform: drag pan, Shift+drag box-zoom, anchored wheel zoom,
// double-click fit. One-frame lag by design (see doc.go).
func (p *Plot) applyInteractions() {
	st := p.st
	sm := c.CurrentApplicationState.StateManager
	cur, curOk := sm.GetCanvasCursor(p.canvasHandle())
	canvasFlags := sm.GetResponse(p.canvasHandle())
	flags := sm.GetResponse(p.areaHandle())
	wheel := sm.GetCanvasWheel(p.canvasHandle())
	mods := sm.GetModifiers()
	ptr := sm.GetPointer()

	// Gesture positions come from the global R20 pointer minus the R24
	// canvas origin: R24's own pointer field goes NaN while the sense
	// region (registered above the canvas) owns a drag, since the canvas
	// then neither hovers nor drags. The origin half of R24 is always
	// stamped. Hover display additionally gates on the canvas's
	// contains_pointer bit so a pointer over an overlapping window does
	// not read as a plot hover.
	posOk := curOk && ptr.Valid
	var posX, posY float32
	if posOk {
		posX = ptr.X - cur.OriginX
		posY = ptr.Y - cur.OriginY
	}
	// The sense region's own R24 row is more precise when present: on the
	// drag-started frame it reports the press origin and the press event's
	// own modifiers (fast input batches press, moves and release into one
	// frame; R20 would start mid-gesture and the frame-end modifier state
	// would miss a modifier already released again).
	boxMod := mods.Shift
	if areaCur, areaOk := sm.GetCanvasCursor(p.areaHandle()); areaOk {
		boxMod = areaCur.Shift()
		if !isNaN32(areaCur.PosX) {
			posX, posY = areaCur.PosX, areaCur.PosY
			posOk = true
		}
	}

	p.toolPos = [2]float32{posX, posY}
	p.toolPosOk = posOk

	st.hoverOk = posOk && canvasFlags.HasContainsPointer() && st.prevOk &&
		float64(posX) >= st.prev.px0 && float64(posX) <= st.prev.px0+st.prev.plotW &&
		float64(posY) >= st.prev.py0 && float64(posY) <= st.prev.py0+st.prev.plotH
	if posOk {
		st.hoverPos = [2]float32{posX, posY}
	}
	p.clickOk = flags.HasPrimaryClicked() && posOk
	p.clickPos = [2]float32{posX, posY}

	if !st.prevOk {
		return
	}

	// A wheel run finishes when the wheel has rested, or as soon as another
	// gesture starts. Until it finishes, a repaint is booked for when it
	// would: an idle window draws no frame on its own, and the run would
	// otherwise wait for the next pointer move to report.
	gs := &st.gestures
	now := clockNow()
	otherStarts := flags.HasDragStarted() || flags.HasDoubleClicked()
	if gs.wheelFlush(now, otherStarts) {
		c.RequestRepaintAfter(wheelQuiet.Seconds())
	}

	if flags.HasDragStarted() && posOk {
		st.dragging = true
		gs.panMoved = [2]bool{}
		st.dragBox = boxMod
		st.boxStart = [2]float32{posX, posY}
		st.boxCur = st.boxStart
		st.lastDrag = st.boxStart
	}

	// Pan and zoom compose in one pixel-space window against last frame's
	// transform: start from the previous plot-area window, shift it by the
	// pan delta, scale it about the wheel anchor, and invert the corners
	// through the transform once. Working in pixels keeps every gesture
	// correct on any monotone scale (log, symlog), not just linear.
	pr := st.prev
	w := pxWindow{
		x0: float32(pr.px0), x1: float32(pr.px0 + pr.plotW),
		y0: float32(pr.py0), y1: float32(pr.py0 + pr.plotH),
	}
	locks := locksOf(&st.x, &st.y)
	movedX, movedY := false, false

	if st.dragging && flags.HasDragged() && posOk {
		if st.dragBox {
			st.boxCur = [2]float32{posX, posY}
		} else {
			mx, my := locks.pan(&w, posX-st.lastDrag[0], posY-st.lastDrag[1])
			movedX, movedY = movedX || mx, movedY || my
			gs.panMoved[AxisX1] = gs.panMoved[AxisX1] || mx
			gs.panMoved[AxisY1] = gs.panMoved[AxisY1] || my
			st.lastDrag = [2]float32{posX, posY}
		}
	}
	panEnded := false
	if flags.HasDragStopped() && st.dragging {
		if st.dragBox {
			// Box-zoom is a zoom on both axes; a NoZoom axis keeps its
			// range and the box degenerates to a one-axis zoom.
			rx, ry, okX, okY := boxZoomRanges(pr, st.boxStart, st.boxCur, locks)
			if okX {
				st.x.rng = rx
				st.x.touched = true
				gs.finish(AxisX1, GestureBoxZoom, rx)
			}
			if okY {
				st.y.rng = ry
				st.y.touched = true
				gs.finish(AxisY1, GestureBoxZoom, ry)
			}
		} else {
			panEnded = true
		}
		st.dragging = false
		st.dragBox = false
	}
	if flags.HasDoubleClicked() {
		// The range is the one End fits to; settle fills it in.
		fx, fy := locks.requestFit(&st.x, &st.y, true, true)
		if fx {
			gs.finish(AxisX1, GestureFit, st.x.rng)
		}
		if fy {
			gs.finish(AxisY1, GestureFit, st.y.rng)
		}
	}
	if flags.HasSecondaryClicked() && posOk && curOk {
		st.ctxOpen = true
		st.ctxSeq++
		st.ctxScreen = [2]float32{cur.OriginX + posX, cur.OriginY + posY}
	}

	// Wheel: egui delivers pinch/ctrl-wheel as Zoom and plain wheel as
	// ScrollY; ImPlot's convention is that the plain wheel zooms a plot, so
	// both fold into one multiplicative factor, anchored at the pointer.
	// Under WheelScrollsPane the plain wheel belongs to the surrounding pane.
	wheelX, wheelY := false, false
	zoom := float32(wheel.Zoom)
	if wheel.ScrollY != 0 && !st.prevWheelScrollsPane {
		zoom *= float32(math.Pow(1.1, float64(wheel.ScrollY)/40.0))
	}
	if zoom != 1.0 && zoom > 0 {
		ax, ay := wheel.HoverX, wheel.HoverY
		if isNaN32(ax) {
			ax = (w.x0 + w.x1) / 2
			ay = (w.y0 + w.y1) / 2
		}
		wx, wy := locks.zoom(&w, ax, ay, zoom)
		movedX, movedY = movedX || wx, movedY || wy
		wheelX, wheelY = wx, wy
	}
	// Assign per axis: an axis no gesture moved is never written back, so it
	// keeps its exact range instead of accumulating a lossy round trip
	// through the transform on every frame the other axis is dragged.
	if movedX {
		st.x.rng = Range{pr.plotX(w.x0), pr.plotX(w.x1)}
		st.x.touched = true
	}
	if movedY {
		st.y.rng = Range{pr.plotY(w.y1), pr.plotY(w.y0)}
		st.y.touched = true
	}
	if wheelX || wheelY {
		gs.wheelStep([2]bool{wheelX, wheelY}, [2]Range{st.x.rng, st.y.rng}, now)
		c.RequestRepaintAfter(wheelQuiet.Seconds())
	}
	if panEnded {
		if gs.panMoved[AxisX1] {
			gs.finish(AxisX1, GesturePan, st.x.rng)
		}
		if gs.panMoved[AxisY1] {
			gs.finish(AxisY1, GesturePan, st.y.rng)
		}
		gs.panMoved = [2]bool{}
	}
}

// SetupAxes names the axes and sets their flags. Must precede the first
// item call, per the ImPlot protocol.
func (p *Plot) SetupAxes(xlabel string, ylabel string, xflags AxisFlags, yflags AxisFlags) *Plot {
	if p.warnIfLocked("SetupAxes") {
		return p
	}
	p.st.x.label, p.st.y.label = xlabel, ylabel
	p.st.x.flags, p.st.y.flags = xflags, yflags
	return p
}

// AxisE selects an axis for SetupAxisLimits. M1 has one x and one y.
type AxisE uint8

const (
	AxisX1 AxisE = iota
	AxisY1
)

// SetupAxisLimits sets an axis range. CondOnce applies only the first
// time this plot id is seen; CondAlways pins the axis every frame.
func (p *Plot) SetupAxisLimits(axis AxisE, vmin float64, vmax float64, cond Cond) *Plot {
	if p.warnIfLocked("SetupAxisLimits") {
		return p
	}
	ax := &p.st.x
	if axis == AxisY1 {
		ax = &p.st.y
	}
	if cond == CondAlways || !p.st.onceApplied {
		ax.rng = sanitizeScaled(Range{vmin, vmax}, ax.scale)
		ax.hasRange = true
	}
	return p
}

// SetupAxisScale selects the axis scale (linear, time, log10, symlog).
// Like every Setup call it must precede the first item — and, like every
// Setup call, it holds for THIS frame only: an axis whose scale is not
// re-declared is linear again next frame. A caller toggling a log axis
// therefore states the choice, not the change.
func (p *Plot) SetupAxisScale(axis AxisE, scale ScaleE) *Plot {
	if p.warnIfLocked("SetupAxisScale") {
		return p
	}
	if axis == AxisY1 {
		p.st.y.scale = scale
	} else {
		p.st.x.scale = scale
	}
	return p
}

// SetupAxisTimeZone sets the zone a ScaleTime axis is read in: its ticks
// fall on the zone's calendar boundaries (midnights, month starts; a day
// with a DST change is 23 or 25 hours long) and its tick labels, the hover
// readout and TagX/TagY print its wall-clock time, the latter two with the
// zone's abbreviation. A nil loc is UTC, the default. The values stay Unix
// seconds; only their reading changes. Other scales ignore it. Per-frame
// Setup state like the scale, and a house extension: upstream has a
// UseLocalTime flag, not a zone.
func (p *Plot) SetupAxisTimeZone(axis AxisE, loc *time.Location) *Plot {
	if p.warnIfLocked("SetupAxisTimeZone") {
		return p
	}
	if axis == AxisY1 {
		p.st.y.loc = loc
	} else {
		p.st.x.loc = loc
	}
	return p
}

// SetupAxisLimitsConstraints clamps the axis's visible range inside
// [vmin, vmax] — upstream's SetupAxisLimitsConstraints. Pan and zoom
// cannot escape the constraint; a viewport wider than it is pulled in.
// Like all Setup state it is re-declared every frame.
func (p *Plot) SetupAxisLimitsConstraints(axis AxisE, vmin float64, vmax float64) *Plot {
	if p.warnIfLocked("SetupAxisLimitsConstraints") {
		return p
	}
	ax := &p.st.x
	if axis == AxisY1 {
		ax = &p.st.y
	}
	ax.consMin, ax.consMax, ax.consOk = vmin, vmax, true
	return p
}

// constrain pulls the range inside the axis's constraints, preserving
// the span when it fits and clamping to the full constraint otherwise.
func (ax *axisState) constrain() {
	ax.rng = ax.constrainRange(ax.rng)
}

// constrainRange is r pulled inside the axis's constraints, as constrain
// does to the visible range; r itself when there are none.
func (ax *axisState) constrainRange(r Range) Range {
	if !ax.consOk {
		return r
	}
	lo, hi := ax.consMin, ax.consMax
	if !(hi > lo) {
		return r
	}
	if r.Size() >= hi-lo {
		return Range{lo, hi}
	}
	if r.Min < lo {
		return Range{lo, lo + r.Size()}
	} else if r.Max > hi {
		return Range{hi - r.Size(), hi}
	}
	return r
}

// SetupAxisTicks replaces the axis's located ticks with caller-supplied
// major ticks (upstream's SetupAxisTicks): values in plot space with
// their labels, re-declared every frame like all Setup state. Ticks
// outside the visible range are dropped at render. An empty values
// slice restores the default locator.
func (p *Plot) SetupAxisTicks(axis AxisE, values []float64, labels []string) *Plot {
	if p.warnIfLocked("SetupAxisTicks") {
		return p
	}
	n := min(len(values), len(labels))
	dst := p.xCustomTicks
	if axis == AxisY1 {
		dst = p.yCustomTicks
	}
	dst = dst[:0]
	for i := range n {
		dst = append(dst, tick{value: values[i], major: true, label: labels[i]})
	}
	if axis == AxisY1 {
		p.yCustomTicks = dst
	} else {
		p.xCustomTicks = dst
	}
	return p
}

// NoInputs disables every interaction surface of this plot for the
// frame — upstream's ImPlotFlags_NoInputs: no pan/zoom/box-zoom/fit
// gestures, no wheel capture (the wheel scrolls the surrounding pane
// instead), no clickable legend rows. For sparklines and other inert
// thumbnails. Callable any time before End.
func (p *Plot) NoInputs() *Plot {
	p.noInputs = true
	return p
}

// WheelScrollsPane leaves the plain wheel to the surrounding pane for the
// frame: it scrolls the page the plot sits in instead of zooming the plot.
// Pinch and Ctrl+wheel still zoom, and every other gesture stays. For plots
// stacked in a scrolling page, such as a dashboard's panels, where a wheel
// that zooms whichever plot the pointer crosses keeps the page from
// scrolling. Callable any time before End.
func (p *Plot) WheelScrollsPane() *Plot {
	p.wheelScrollsPane = true
	return p
}

// NoMouseText drops the hover readout in the plot's corner for the frame
// and keeps every interaction — upstream's ImPlotFlags_NoMouseText. For a
// plot that shows its own hover text (a callout, a line under the plot);
// HoverPlotPos still reports the position.
func (p *Plot) NoMouseText() *Plot {
	p.noMouseText = true
	return p
}

// NoLegend suppresses the legend for the frame even when labeled
// series exist — upstream's ImPlotFlags_NoLegend.
func (p *Plot) NoLegend() *Plot {
	p.noLegend = true
	return p
}

// LegendPlacementE says where a plot's legend goes — the location half of
// upstream's SetupLegend, narrowed to the placements a caller has asked for.
type LegendPlacementE uint8

const (
	// LegendInside is the default: a list over the plot area's top-left
	// corner, over the data.
	LegendInside LegendPlacementE = iota
	// LegendRight is a list to the right of the plot area, top-aligned with
	// it; the area narrows by the legend's width. Upstream's
	// Outside|East|North.
	LegendRight
	// LegendBelow lays the items out in a row under the x axis label,
	// wrapping onto further lines at the canvas width; the area gets shorter
	// by the legend's height. Upstream's Outside|South|Horizontal.
	LegendBelow
)

// SetupLegend places the legend. An outside placement takes its room from
// the plot area, so a long legend leaves less of it; MinBoxHeight does not
// count a legend below. Like every Setup call it holds for this frame only
// and must precede the first item.
func (p *Plot) SetupLegend(place LegendPlacementE) *Plot {
	if p.warnIfLocked("SetupLegend") {
		return p
	}
	p.legendPlace = place
	return p
}

// SeriesHidden reports whether the series under label is hidden — the state
// a click on its legend row toggles. A house extension: upstream keeps the
// flag on its item and exposes it only through the legend.
func (p *Plot) SeriesHidden(label string) bool {
	if p == nil || p.st == nil {
		return false
	}
	return p.st.hidden[label]
}

// SetSeriesHidden hides or shows the series under label, as a click on its
// legend row does. The flag is retained state, not a per-frame declaration:
// it holds until the person clicks the row or the caller sets it again, so
// a caller sets it on a change — restoring saved state, or from a legend it
// draws itself — and not every frame, which would undo the person's clicks.
// Callable any time before End; it applies to this frame.
func (p *Plot) SetSeriesHidden(label string, hidden bool) *Plot {
	if p == nil || p.st == nil {
		return p
	}
	if hidden {
		p.st.hidden[label] = true
	} else {
		delete(p.st.hidden, label)
	}
	return p
}

// FitNext requests a data re-fit of both axes this frame — the
// programmatic double-click fit (upstream's SetNextAxesToFit, applied
// to the already-open plot). Drive it from a one-frame flag, e.g. a
// "Reset zoom" button clicked last frame.
func (p *Plot) FitNext() *Plot {
	p.st.x.fitNext = true
	p.st.y.fitNext = true
	return p
}

// AxisLimits reports an axis's current visible range — upstream's
// GetPlotLimits, narrowed to one axis. It is the readback a caller needs to
// re-pin a range it derives from the plot area (a depth axis whose span is
// the area height over a fixed row height) without discarding the scroll
// position a pan has since put there.
//
// The range is this frame's if Setup has already run, and last frame's
// otherwise; ok is false until the plot has resolved a range once.
func (p *Plot) AxisLimits(axis AxisE) (vmin float64, vmax float64, ok bool) {
	if p == nil || p.st == nil || !p.st.initialized {
		return 0, 0, false // nil-safe for headless widget tests
	}
	ax := &p.st.x
	if axis == AxisY1 {
		ax = &p.st.y
	}
	return ax.rng.Min, ax.rng.Max, true
}

// Clicked reports a primary click on the plot area, with the click
// position in plot space — one frame behind, like every register read.
// Serves nearest-point selection on scatter clouds.
func (p *Plot) Clicked() (x float64, y float64, ok bool) {
	if p == nil || p.st == nil || !p.clickOk || !p.st.prevOk {
		return 0, 0, false
	}
	return p.st.prev.plotX(p.clickPos[0]), p.st.prev.plotY(p.clickPos[1]), true
}

// HoverPlotPos returns the pointer's plot-space position while it is
// over the plot area (one-frame lag). ok is false when the pointer is
// elsewhere or the plot has not rendered yet.
func (p *Plot) HoverPlotPos() (x float64, y float64, ok bool) {
	if p == nil || p.st == nil {
		return 0, 0, false // nil-safe for headless widget tests
	}
	st := p.st
	if !st.hoverOk || !st.prevOk {
		return 0, 0, false
	}
	return st.prev.plotX(st.hoverPos[0]), st.prev.plotY(st.hoverPos[1]), true
}

func (p *Plot) warnIfLocked(fn string) bool {
	if p.setupLocked {
		log.Debug().Str("plot", p.title).Str("call", fn).
			Msg("implot: Setup call after the first item is ignored (protocol: Setup* before items)")
		return true
	}
	return false
}

func (p *Plot) warnIfEmitting(fn string) bool {
	if p.emitting {
		log.Debug().Str("plot", p.title).Str("call", fn).
			Msg("implot: item declaration during End emission is ignored (a Custom closure must only paint)")
		return true
	}
	return false
}

// Line declares a line series. The slices must be equal-length; NaN points
// split the line, as in ImPlot. Data is not copied — the slices must stay
// valid until End.
func (p *Plot) Line(label string, xs []float64, ys []float64) *Plot {
	p.addSeries(seriesFrame{kind: kindLine, label: label, xs: xs, ys: ys}, true, true)
	return p
}

// SetNextColor overrides the next declared item's series color (the color
// half of upstream's SetNextLineStyle). It applies to the immediately
// following item declaration only; the legend swatch follows the override.
func (p *Plot) SetNextColor(colHex uint32) *Plot {
	p.nextColHex = colHex
	p.nextColOk = true
	return p
}

// SetNextWeight overrides the next declared item's stroke weight (the
// weight half of upstream's SetNextLineStyle).
func (p *Plot) SetNextWeight(weight float32) *Plot {
	p.nextWeight = weight
	return p
}

// takeNextStyle consumes the pending SetNext* overrides. Declarators that
// cannot use them (heatmap, pie, image) still consume, so a pending
// override never leaks onto a later item.
func (p *Plot) takeNextStyle() (colHex uint32, colOk bool, weight float32) {
	colHex, colOk, weight = p.nextColHex, p.nextColOk, p.nextWeight
	p.nextColHex, p.nextColOk, p.nextWeight = 0, false, 0
	return
}

// assignSlot maps a label to its palette slot in first-declaration order.
// Same-label items share one slot (and one legend entry) — the upstream
// label→item registry semantics, which lets error bars decorate the series
// they belong to. Unlabeled items each consume a slot of their own.
func (p *Plot) assignSlot(label string) int {
	if label != "" {
		if s, ok := p.slotByLabel[label]; ok {
			return s
		}
	}
	s := p.nextSlot
	p.nextSlot++
	if label != "" {
		if p.slotByLabel == nil {
			p.slotByLabel = make(map[string]int, 8)
		}
		p.slotByLabel[label] = s
	}
	return s
}

// legendIndices returns the index of each label's first declaration, in
// declaration order — one legend row per distinct label.
func legendIndices(series []seriesFrame) []int {
	idxs := make([]int, 0, len(series))
	seen := make(map[string]bool, len(series))
	for si := range series {
		l := series[si].label
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		idxs = append(idxs, si)
	}
	return idxs
}

func isNaN32(v float32) bool { return v != v }

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
