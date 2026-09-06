package timeline

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
)

// The range brush (ADR-0043 §SD16).
//
// It lives on its own canvas below the tick axis rather than on the main one.
// Drag there is already pan, and the click dispatch relies on egui arbitrating
// click-vs-drag so a pan never lands a selection; a brush on the same surface
// would make that arbitration three-way for every caller, including the ones
// that never asked for a brush. A second canvas has its own id, so its own
// response flags and its own cursor row — the main canvas is untouched by
// construction, and a timeline without WithBrush emits nothing extra at all.
//
// The brush deliberately does not go through [SelectionInfo]. A range is not
// an interval, bucket, annotation or lane, and widening SelectionKindE to
// pretend otherwise would break the "pointer non-nil iff Kind matches"
// contract the selection model rests on.

// brushCanvasIdKey names the strip's canvas within the widget's id scope. Its
// own key is what keeps its responses and pointer row separate from the main
// canvas's.
const brushCanvasIdKey = "brush-strip"

// brushMinDragPx is how far a gesture must travel before it commits a range.
// Below it the gesture reads as a click, which clears the brush — a
// zero-width range is not a selection anyone means to make, and it would map
// to an empty replay window.
const brushMinDragPx = 3.0

// BrushRange is a committed brush selection, in epoch milliseconds UTC.
// FromMS < ToMS always: the gesture is normalised, so dragging right-to-left
// gives the same range as left-to-right.
type BrushRange struct {
	FromMS int64
	ToMS   int64
}

// BrushListener fires once per completed brush gesture, on the frame the
// release lands. A click that never travelled (see brushMinDragPx) clears the
// brush and fires with ok=false.
//
// Validation: nil is a no-op — the brush still tracks and paints, and the
// caller can poll [Timeline.Brush] instead.
type BrushListener func(r BrushRange, ok bool)

// WithBrush enables the range-brush strip and registers the completion
// callback.
//
// Opt-in by design: without it no strip is emitted, no row is reserved, and
// the widget renders exactly as it did before the brush existed — which is
// what lets it land on a widget with existing callers.
//
// To shade the brushed range on the main canvas, feed it back through
// [WithBackgroundBands]; that is what bands are for, and it keeps the brush
// from reaching into the main paint path.
//
// Validation: nil listener is accepted (see [BrushListener]).
func WithBrush(onBrush BrushListener) Option {
	return func(inst *Timeline) {
		inst.brushEnabled = true
		inst.onBrush = onBrush
	}
}

// Brush returns the committed range. ok is false when nothing is brushed.
//
// Validation: snapshot at boundary — the value reflects the last completed
// gesture, not one in progress.
func (inst *Timeline) Brush() (r BrushRange, ok bool) {
	if !inst.brushHas {
		return
	}
	r, ok = BrushRange{FromMS: inst.brushFromMS, ToMS: inst.brushToMS}, true
	return
}

// SetBrush sets the committed range programmatically — for restoring a
// persisted selection, or reflecting one a sibling control made. Bounds are
// normalised; an empty or inverted-to-equal range clears instead.
//
// Does not fire the listener: the callback reports what the user did, and a
// caller that just set the value already knows.
func (inst *Timeline) SetBrush(fromMS, toMS int64) {
	if fromMS == toMS {
		inst.ClearBrush()
		return
	}
	if fromMS > toMS {
		fromMS, toMS = toMS, fromMS
	}
	inst.brushFromMS, inst.brushToMS, inst.brushHas = fromMS, toMS, true
}

// ClearBrush drops the committed range. Does not fire the listener.
func (inst *Timeline) ClearBrush() {
	inst.brushHas = false
	inst.brushFromMS, inst.brushToMS = 0, 0
}

// brushReserved reports whether the strip participates in this frame.
func (inst *Timeline) brushReserved() (yes bool) {
	yes = inst.brushEnabled
	return
}

// renderBrushStrip reads last frame's press state for the strip, advances the
// gesture, paints the track, and emits the strip's own canvas.
//
// It runs after the main canvas has been sent, so the strip sits below it in
// the enclosing Ui and its paint ops drain into its own canvas rather than the
// main one.
//
// Input arrives a frame late, exactly as the main canvas's pan does: the
// response flags and the canvas-relative pointer both describe the frame
// before this one. That is why a gesture is tracked as a state machine over
// frames rather than resolved within one.
func (inst *Timeline) renderBrushStrip(tm layout.TickMap, vl verticalLayout, viewMinMS, viewMaxMS int64) {
	if !inst.brushReserved() {
		return
	}
	stateMgr := c.CurrentApplicationState.StateManager
	handle := widgethandle.Make(inst.ids.PrepareStr(brushCanvasIdKey).Derive())

	resp := stateMgr.GetResponse(handle)
	down := resp.HasIsPointerButtonDown()
	x, xOK := inst.brushCursorX(stateMgr, handle, vl)
	if inst.interactionEnabled {
		settled := inst.advanceBrush(tm, down, x, xOK, viewMinMS, viewMaxMS)
		// A click the press-sampling never saw. The button-down flag is read
		// once per frame, so a press and release inside one frame — a fast
		// click, and every synthesised one — leaves the state machine having
		// observed nothing at all. egui's own click edge is not sampled that
		// way: it is set on the release of a gesture that stayed under the
		// drag threshold, which is exactly the gesture that should clear.
		//
		// Gated on `settled` so a slow click, which the machine did see, does
		// not clear twice and fire the listener twice.
		if !settled && !inst.brushing && resp.HasPrimaryClicked() {
			inst.clearBrushAndNotify()
		}
	}

	// The affordance layers (§SD19), ordered so nothing the user is doing is
	// covered by something they might do. Hover reads last frame's flags, like
	// every other input here; a frame's lag on a highlight is not visible.
	//
	// All of it is gated on interaction: a strip that cannot be dragged must
	// not advertise a drag, and a read-only timeline renders the plain track
	// it did before the hint existed.
	hovered := inst.interactionEnabled && resp.HasHovered()
	if inst.interactionEnabled && !inst.brushing && !inst.brushHas {
		inst.paintBrushHint(vl)
	}
	if hovered && !inst.brushing {
		inst.paintBrushHoverWash(vl)
	}
	inst.paintBrushStrip(tm, vl, viewMinMS, viewMaxMS)
	if hovered && !inst.brushing {
		// Over the range fill: the guide answers "where would this press
		// land", and inside an existing range is exactly where that is worth
		// asking.
		inst.paintBrushHoverGuide(vl, x, xOK)
	}

	strip := c.PaintCanvas(inst.ids.PrepareStr(brushCanvasIdKey), vl.axisEndPx, inst.visuals.BrushStripH).
		Background(inst.visuals.BrushTrackColor)
	if inst.interactionEnabled {
		// Drag and click only. The strip deliberately does not CaptureZoom:
		// the wheel belongs to the main canvas, and capturing it here would
		// make the zoom gesture depend on which of the two the pointer
		// happened to be over.
		strip = strip.Sense(true, true, true)
	}
	strip.Send()
}

// brushCursorX resolves the strip-relative pointer x, clamped to the axis.
//
// While a gesture is in flight the pointer may leave the strip — dragging past
// either end is the normal way to select up to an edge — and the per-canvas
// row reports NaN for that. Rather than dropping the sample (which would
// freeze the pending range mid-drag), the last known x is held and the global
// pointer decides which edge to clamp to.
func (inst *Timeline) brushCursorX(stateMgr *c.StateManager, handle widgethandle.WidgetHandle, vl verticalLayout) (x float32, ok bool) {
	if cur, got := stateMgr.GetCanvasCursor(handle); got && !math.IsNaN(float64(cur.PosX)) {
		x = min(max(cur.PosX, vl.axisStartPx), vl.axisEndPx)
		inst.brushLastX, inst.brushLastXOK = x, true
		ok = true
		return
	}
	if !inst.brushLastXOK {
		return
	}
	x = inst.brushLastX
	if gp := stateMgr.GetPointer(); gp.Valid {
		// Off the strip: the pointer's global x still says which side it left
		// on, which is all the clamp needs.
		if gp.X < vl.axisStartPx {
			x = vl.axisStartPx
		} else if gp.X > vl.axisEndPx {
			x = vl.axisEndPx
		}
	}
	ok = true
	return
}

// advanceBrush runs the press → drag → release state machine. It reports
// whether a gesture finished on this frame, so the caller can tell a click it
// already handled from one it never observed.
func (inst *Timeline) advanceBrush(tm layout.TickMap, down bool, x float32, xOK bool, viewMinMS, viewMaxMS int64) (settled bool) {
	clampMS := func(ms int64) int64 {
		return min(max(ms, viewMinMS), viewMaxMS)
	}
	switch {
	case down && !inst.brushing:
		if !xOK {
			return
		}
		inst.brushing = true
		inst.brushAnchorX = x
		inst.brushAnchorMS = clampMS(tm.MapXToMS(float64(x)))
		inst.brushCurMS = inst.brushAnchorMS
	case down && inst.brushing:
		if !xOK {
			return
		}
		inst.brushCurX = x
		inst.brushCurMS = clampMS(tm.MapXToMS(float64(x)))
	case !down && inst.brushing:
		inst.brushing = false
		inst.commitBrush()
		settled = true
	}
	return
}

// commitBrush turns the finished gesture into a range, or into a clear when it
// never travelled far enough to mean one.
func (inst *Timeline) commitBrush() {
	travelled := float64(inst.brushCurX - inst.brushAnchorX)
	if travelled < 0 {
		travelled = -travelled
	}
	if travelled < brushMinDragPx {
		inst.clearBrushAndNotify()
		return
	}
	from, to := inst.brushAnchorMS, inst.brushCurMS
	if from > to {
		from, to = to, from
	}
	if from == to {
		// Sub-millisecond travel at a coarse zoom: pixels moved but the range
		// rounds to nothing, which is a clear rather than an empty selection.
		inst.clearBrushAndNotify()
		return
	}
	inst.brushFromMS, inst.brushToMS, inst.brushHas = from, to, true
	if inst.onBrush != nil {
		inst.onBrush(BrushRange{FromMS: from, ToMS: to}, true)
	}
}

// clearBrushAndNotify drops the range and tells the listener it went. Both
// ways a gesture can end in "no range" land here, so the listener cannot
// observe one of them and miss the other.
func (inst *Timeline) clearBrushAndNotify() {
	inst.ClearBrush()
	if inst.onBrush != nil {
		inst.onBrush(BrushRange{}, false)
	}
}

// paintBrushStrip draws the committed range, or the pending one while a
// gesture is in flight. The pending range wins: during a drag the user is
// asking about the range they are making, not the one they made.
func (inst *Timeline) paintBrushStrip(tm layout.TickMap, vl verticalLayout, viewMinMS, viewMaxMS int64) {
	fromMS, toMS, has := inst.brushFromMS, inst.brushToMS, inst.brushHas
	pending := inst.brushing
	if pending {
		fromMS, toMS = inst.brushAnchorMS, inst.brushCurMS
		if fromMS > toMS {
			fromMS, toMS = toMS, fromMS
		}
		has = true
	}
	if !has || toMS <= fromMS {
		return
	}
	// A range entirely outside the view paints nothing; one that overlaps is
	// clipped, so a brush wider than the viewport still reads as "covers this
	// whole strip" rather than vanishing.
	if toMS <= viewMinMS || fromMS >= viewMaxMS {
		return
	}
	x0 := float32(tm.MapMSToX(max(fromMS, viewMinMS)))
	x1 := float32(tm.MapMSToX(min(toMS, viewMaxMS)))
	cx0, cx1, ok := vl.clipToAxis(x0, x1)
	if !ok {
		return
	}
	fill := inst.visuals.BrushFillColor
	edge := inst.visuals.BrushEdgeColor
	if pending {
		fill = inst.visuals.BrushPendingFillColor
	}
	h := inst.visuals.BrushStripH
	c.PaintRectFilled(cx0, 0, cx1, h, 0, fill).Send()
	// Edges are what make the ends graspable, and they are drawn only where
	// the bound is actually in view — an edge painted at a clip boundary would
	// claim the range ends there.
	if x0 >= vl.axisStartPx {
		c.PaintLine(cx0, 0, cx0, h, edge, brushEdgeWidthPx).Send()
	}
	if x1 <= vl.axisEndPx {
		c.PaintLine(cx1, 0, cx1, h, edge, brushEdgeWidthPx).Send()
	}
}

// brushEdgeWidthPx is the vertical rule at each end of the brushed range.
const brushEdgeWidthPx float32 = 1.5

// The resting affordance (ADR-0043 §SD19).
//
// An empty brush strip used to paint as a bare panel-coloured band, which
// reads as a gap in the layout rather than as a control — the option's own
// premise was that the affordance is "visible rather than remembered", and a
// blank row is neither. The strip now carries a slider-like rail with a cap at
// each end of the axis and a caption between them, plus a hover state that
// previews where a press would anchor.
//
// It is chrome, so it yields: the rail and caption paint only on an unbrushed,
// unbrushing, interactive strip, and both degrade rather than crowd when the
// space for them is not there.

// defaultBrushHintText is the caption on an unbrushed strip. It names the
// gesture and its result, in that order, because the gesture is the part the
// user has to guess.
const defaultBrushHintText = "drag to select a range"

const (
	// brushHintFontSize sits one step under the tick labels' 11 px: the hint
	// is read once, the axis is read continuously, and the smaller of the two
	// should be the one that stops being looked at.
	brushHintFontSize float32 = 9
	// brushHintCharWidthPx is tooltipCharWidthPx scaled to the hint's font —
	// the same ASCII-only estimate, for the same reason (egui's text
	// measurement is not surfaced through FFFI2). It only decides whether the
	// caption is dropped, so an over-estimate costs a caption and never a
	// clipped one.
	brushHintCharWidthPx float32 = 5.3
	brushHintRailWidthPx float32 = 1
	// brushHintCapHeightPx is the upright at each end of the rail. It is what
	// makes the rail read as a bounded track rather than as a rule someone
	// drew across the strip.
	brushHintCapHeightPx float32 = 6
	// brushHintGapPx is the clear space the rail leaves either side of the
	// caption, so the text is framed rather than struck through.
	brushHintGapPx float32 = 6
	// brushHintMinStripH drops the caption on a strip too short to centre it
	// without touching both edges. The rail still paints — it needs one row of
	// pixels, and a caller who shrank the strip still wants it to look like a
	// track.
	brushHintMinStripH float32 = 12
	// brushHintMinFreePx is the rail that must survive on each side of the
	// caption. Below it the two stubs read as decoration flanking a label
	// instead of as one track interrupted by one, so the caption goes and the
	// rail spans the axis whole.
	brushHintMinFreePx float32 = 24
	// brushHoverGuideWidthPx matches the canvas crosshair above it: the guide
	// answers the same question on the same pointer, and a heavier rule would
	// claim to be a bound that has been placed.
	brushHoverGuideWidthPx float32 = 1
)

// brushHintLayout is one frame of resting-affordance geometry: a rail along
// the axis at mid-height, broken over [gapX0, gapX1] for the caption. The gap
// is empty (gapX0 == gapX1) when there is no caption, which is also what text
// == "" says — both are checked so a caller reading one field cannot draw the
// wrong conclusion from the other.
type brushHintLayout struct {
	railY        float32
	x0, x1       float32
	capX0, capX1 float32
	gapX0, gapX1 float32
	capH         float32
	textX        float32
	text         string
}

// computeBrushHintLayout arranges the rail, its caps and the caption within
// the strip. ok is false when there is no axis to draw along.
//
// Split out from the paint so the degradation ladder — caption, then rail
// alone — is testable without a renderer, the way the gesture machine is.
func computeBrushHintLayout(vl verticalLayout, stripH float32, text string) (l brushHintLayout, ok bool) {
	if stripH <= 0 || vl.axisEndPx <= vl.axisStartPx {
		return
	}
	l.x0, l.x1 = vl.axisStartPx, vl.axisEndPx
	l.railY = stripH / 2
	// The caps are inset by half a stroke so neither is clipped in half
	// against the canvas bounds; axisEndPx is the canvas width exactly.
	inset := brushHintRailWidthPx / 2
	l.capX0, l.capX1 = l.x0+inset, l.x1-inset
	l.capH = min(brushHintCapHeightPx, stripH)
	mid := (l.x0 + l.x1) / 2
	l.gapX0, l.gapX1 = mid, mid
	ok = true
	if text == "" || stripH < brushHintMinStripH {
		return
	}
	half := float32(len(text))*brushHintCharWidthPx/2 + brushHintGapPx
	if mid-half-l.x0 < brushHintMinFreePx || l.x1-(mid+half) < brushHintMinFreePx {
		return
	}
	l.gapX0, l.gapX1 = mid-half, mid+half
	l.textX, l.text = mid, text
	return
}

// paintBrushHint draws the resting affordance. Callers gate it; it does not
// consult the gesture state itself.
func (inst *Timeline) paintBrushHint(vl verticalLayout) {
	l, ok := computeBrushHintLayout(vl, inst.visuals.BrushStripH, inst.visuals.BrushHintText)
	if !ok {
		return
	}
	col := inst.visuals.BrushHintColor
	if l.gapX0 > l.x0 {
		c.PaintLine(l.x0, l.railY, l.gapX0, l.railY, col, brushHintRailWidthPx).Send()
	}
	if l.x1 > l.gapX1 {
		c.PaintLine(l.gapX1, l.railY, l.x1, l.railY, col, brushHintRailWidthPx).Send()
	}
	capY0, capY1 := l.railY-l.capH/2, l.railY+l.capH/2
	c.PaintLine(l.capX0, capY0, l.capX0, capY1, col, brushHintRailWidthPx).Send()
	c.PaintLine(l.capX1, capY0, l.capX1, capY1, col, brushHintRailWidthPx).Send()
	if l.text != "" {
		c.PaintText(l.textX, l.railY, anchorCenter, anchorCenter, l.text, brushHintFontSize, col).Send()
	}
}

// paintBrushHoverWash tints the track while the pointer is over it. Painted
// under the range fill so a committed range stays the brighter of the two.
func (inst *Timeline) paintBrushHoverWash(vl verticalLayout) {
	if vl.axisEndPx <= vl.axisStartPx {
		return
	}
	c.PaintRectFilled(vl.axisStartPx, 0, vl.axisEndPx, inst.visuals.BrushStripH, 0,
		inst.visuals.BrushHoverColor).Send()
}

// paintBrushHoverGuide previews the bound a press would place, in the same ink
// as a committed edge — the guide is that edge, one gesture early.
func (inst *Timeline) paintBrushHoverGuide(vl verticalLayout, x float32, xOK bool) {
	if !xOK || x < vl.axisStartPx || x > vl.axisEndPx {
		return
	}
	c.PaintLine(x, 0, x, inst.visuals.BrushStripH, inst.visuals.BrushEdgeColor, brushHoverGuideWidthPx).Send()
}

// brushAlpha re-alphas an IDS token, keeping its RGB. The palette's semantic
// tokens are opaque by design, and the brush needs translucency so the layers
// beneath it stay readable — the same bridge imztop's withAlpha makes, kept
// local rather than exported because nothing outside this widget needs it.
//
// Layout matches RGBA8.AsHex (0xRRGGBBAA) and color.Hex's expected input.
func brushAlpha(tokenHex uint32, alpha uint8) (cl color.Color) {
	cl = color.Hex((tokenHex & 0xffffff00) | uint32(alpha)).Keep()
	return
}
