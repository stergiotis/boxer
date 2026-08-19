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

	inst.paintBrushStrip(tm, vl, viewMinMS, viewMaxMS)

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
