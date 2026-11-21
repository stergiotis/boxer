package implot

import (
	"math"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
)

// A released box asks for the range under it on each axis, through last
// frame's transform; a box under 5 px on a side is a Shift+click, and a
// NoZoom axis keeps its range.
func TestBoxZoomRanges(t *testing.T) {
	pr := barsTransform() // [0,10]² onto 100×100 px; px y = 100 − 10·v
	rx, ry, okX, okY := boxZoomRanges(pr, [2]float32{70, 20}, [2]float32{20, 60}, gestureLocks{})
	if !okX || !okY {
		t.Fatalf("ok = (%v,%v), want both", okX, okY)
	}
	if rx != (Range{2, 7}) || ry != (Range{4, 8}) {
		t.Errorf("ranges = x%v y%v, want x{2 7} y{4 8}", rx, ry)
	}
	if _, _, okX, okY = boxZoomRanges(pr, [2]float32{20, 20}, [2]float32{24, 60}, gestureLocks{}); okX || okY {
		t.Errorf("a 4 px wide box zoomed (%v,%v)", okX, okY)
	}
	if _, _, okX, okY = boxZoomRanges(pr, [2]float32{70, 20}, [2]float32{20, 60}, gestureLocks{noZoomY: true}); !okX || okY {
		t.Errorf("NoZoom y: ok = (%v,%v), want (true,false)", okX, okY)
	}
}

// A finished gesture keeps the range it asked for — sanitized and held to
// the constraints — except a fit, which takes the range End settled on.
func TestGestureSettle(t *testing.T) {
	var g gestureTrack
	x := axisState{rng: Range{0, 100}, consMin: 0, consMax: 50, consOk: true}
	y := axisState{rng: Range{-1, 1}}
	g.finish(AxisX1, GestureBoxZoom, Range{40, 60})
	g.finish(AxisY1, GestureFit, Range{5, 6})
	g.settle(&x, &y)
	if g.done[AxisX1] != (gestureRec{GestureBoxZoom, Range{30, 50}}) {
		t.Errorf("box zoom settled to %+v, want {box zoom [30,50]} (pulled inside the constraint)", g.done[AxisX1])
	}
	if g.done[AxisY1] != (gestureRec{GestureFit, Range{-1, 1}}) {
		t.Errorf("fit settled to %+v, want the axis's own range", g.done[AxisY1])
	}
	if g.pending != [2]gestureRec{} {
		t.Errorf("settle left pending %+v", g.pending)
	}
}

// Wheel steps make one gesture, with the range of the last step, once the
// wheel has rested; another gesture starting ends the run at once.
func TestWheelRun(t *testing.T) {
	var g gestureTrack
	t0 := time.Unix(1_800_000_000, 0)
	g.wheelStep([2]bool{true, false}, [2]Range{{0, 8}, {0, 1}}, t0)
	g.wheelStep([2]bool{true, false}, [2]Range{{1, 6}, {0, 1}}, t0.Add(100*time.Millisecond))
	if !g.wheelFlush(t0.Add(350*time.Millisecond), false) {
		t.Fatal("run finished 250 ms after its last step, before the wheel rested")
	}
	if g.pending[AxisX1].kind != GestureNone {
		t.Fatalf("pending %+v before the run finished", g.pending[AxisX1])
	}
	if g.wheelFlush(t0.Add(400*time.Millisecond), false) {
		t.Fatal("run still waiting 300 ms after its last step")
	}
	if g.pending[AxisX1] != (gestureRec{GestureWheelZoom, Range{1, 6}}) || g.pending[AxisY1].kind != GestureNone {
		t.Errorf("pending = %+v, want a wheel zoom to [1,6] on x only", g.pending)
	}

	var h gestureTrack
	h.wheelStep([2]bool{false, true}, [2]Range{{}, {2, 3}}, t0)
	if h.wheelFlush(t0, true) || h.pending[AxisY1].kind != GestureWheelZoom {
		t.Errorf("a forced flush left pending %+v", h.pending)
	}
	if h.wheelFlush(t0, true) {
		t.Error("an empty run reports waiting")
	}
}

// gestureRig renders frames of one plot under a discarding channel, with
// scripted input between them; retained state is what is under test, so
// the plot is released only at cleanup.
type gestureRig struct {
	t            *testing.T
	ids          *c.WidgetIdStack
	sm           *c.StateManager
	canvas, area widgethandle.WidgetHandle
	decl         func(p *Plot)
	last         *Plot
}

const gestureTitle = "##gesture-test"

func newGestureRig(t *testing.T, decl func(p *Plot)) *gestureRig {
	t.Cleanup(egui2test.Install())
	r := &gestureRig{t: t, ids: c.NewWidgetIdStack(), sm: c.CurrentApplicationState.StateManager, decl: decl}
	for range c.IdScope(r.ids.PrepareStr(gestureTitle)) {
		r.canvas = widgethandle.Make(r.ids.PrepareStr("implot-canvas").Derive())
		r.area = widgethandle.Make(r.ids.PrepareStr("implot-area").Derive())
	}
	t.Cleanup(func() {
		if r.last != nil {
			Release(r.last.ScopeId())
		}
	})
	return r
}

// frame renders one frame after script has set this frame's input.
func (r *gestureRig) frame(script func()) *Plot {
	r.sm.ScriptReset()
	if script != nil {
		script()
	}
	p := Begin(r.ids, gestureTitle, 400, 300)
	r.decl(p)
	p.End()
	r.last = p
	return p
}

// at scripts the pointer over the plot area at plot-space (x, y), through
// last frame's transform, with the area's flags.
func (r *gestureRig) at(x, y float64, flags c.ResponseFlagsE, shift bool) {
	pr := r.last.st.prev
	var mods uint8
	if shift {
		mods = 1
	}
	px, py := pr.pxX(x), pr.pxY(y)
	r.sm.ScriptCanvasCursor(r.canvas, c.CanvasCursorValue{PosX: px, PosY: py})
	r.sm.ScriptCanvasCursor(r.area, c.CanvasCursorValue{PosX: px, PosY: py, Mods: mods})
	r.sm.ScriptPointer(c.PointerValue{X: px, Y: py, Valid: true})
	r.sm.ScriptResponse(r.canvas, c.ContainsPointerResponseFlags)
	r.sm.ScriptResponse(r.area, flags)
}

func requireNoGesture(t *testing.T, p *Plot, when string) {
	t.Helper()
	for _, a := range []AxisE{AxisX1, AxisY1} {
		if _, _, k, ok := p.RangeGesture(a); ok {
			t.Errorf("%s: axis %d reports %v", when, a, k)
		}
	}
}

// A Shift+drag box, released, reaches exactly one handle — the next frame's
// — with the box's range, even on an axis the caller pins every frame.
func TestRangeGestureBoxZoom(t *testing.T) {
	r := newGestureRig(t, func(p *Plot) {
		p.SetupAxisLimits(AxisX1, 0, 10, CondAlways).SetupAxisLimits(AxisY1, 0, 10, CondAlways)
		p.Line("l", []float64{0, 10}, []float64{0, 10})
	})
	r.frame(nil)
	requireNoGesture(t, r.frame(func() { r.at(2, 3, c.DragStartedResponseFlags, true) }), "drag started")
	requireNoGesture(t, r.frame(func() { r.at(6, 8, c.DraggedResponseFlags, true) }), "dragging")
	requireNoGesture(t, r.frame(func() { r.at(6, 8, c.DragStoppedResponseFlags, true) }), "the frame the box was released in")

	p := r.frame(nil)
	for _, w := range []struct {
		axis     AxisE
		min, max float64
	}{{AxisX1, 2, 6}, {AxisY1, 3, 8}} {
		vmin, vmax, kind, ok := p.RangeGesture(w.axis)
		if !ok || kind != GestureBoxZoom || math.Abs(vmin-w.min) > 0.05 || math.Abs(vmax-w.max) > 0.05 {
			t.Errorf("axis %d reports (%v,%v,%v,%v), want a box zoom to [%v,%v]", w.axis, vmin, vmax, kind, ok, w.min, w.max)
		}
	}
	if lo, hi, _ := p.AxisLimits(AxisX1); lo != 0 || hi != 10 {
		t.Errorf("the pinned axis moved to [%v,%v]", lo, hi)
	}
	requireNoGesture(t, r.frame(nil), "the frame after the report")
}

// Under WheelScrollsPane a plain wheel step leaves the plot alone, for the
// pane to scroll, while a pinch or Ctrl+wheel step still zooms it.
func TestWheelScrollsPane(t *testing.T) {
	clock := time.Unix(1_800_000_000, 0)
	clockNow = func() time.Time { return clock }
	t.Cleanup(func() { clockNow = time.Now })
	r := newGestureRig(t, func(p *Plot) {
		p.WheelScrollsPane()
		p.SetupAxisLimits(AxisX1, 0, 10, CondOnce).SetupAxisLimits(AxisY1, 0, 10, CondOnce)
		p.Line("l", []float64{0, 10}, []float64{0, 10})
	})
	r.frame(nil)
	step := func(zoom, scrollY float32) func() {
		return func() {
			r.sm.ScriptCanvasWheel(r.canvas, c.CanvasWheelValue{Zoom: zoom, ScrollY: scrollY, HoverX: float32(math.NaN()), HoverY: float32(math.NaN())})
		}
	}
	r.frame(step(1, 40))
	r.frame(nil)
	if lo, hi, _ := r.last.AxisLimits(AxisX1); lo != 0 || hi != 10 {
		t.Errorf("a plain wheel step zoomed the plot to [%v,%v]", lo, hi)
	}
	clock = clock.Add(wheelQuiet)
	r.frame(nil)
	requireNoGesture(t, r.frame(nil), "after a plain wheel step")

	r.frame(step(1.25, 0))
	r.frame(nil)
	if lo, hi, _ := r.last.AxisLimits(AxisX1); hi-lo >= 10 {
		t.Errorf("a pinch step left the plot at [%v,%v]; it zooms under WheelScrollsPane too", lo, hi)
	}
}

// Wheel steps report once, after the wheel rests, with the last step's range.
func TestRangeGestureWheel(t *testing.T) {
	clock := time.Unix(1_800_000_000, 0)
	clockNow = func() time.Time { return clock }
	t.Cleanup(func() { clockNow = time.Now })
	r := newGestureRig(t, func(p *Plot) {
		p.SetupAxisLimits(AxisX1, 0, 10, CondOnce).SetupAxisLimits(AxisY1, 0, 10, CondOnce)
		p.Line("l", []float64{0, 10}, []float64{0, 10})
	})
	r.frame(nil)
	notch := func() {
		r.sm.ScriptCanvasWheel(r.canvas, c.CanvasWheelValue{Zoom: 1, ScrollY: 40, HoverX: float32(math.NaN()), HoverY: float32(math.NaN())})
	}
	for range 3 {
		requireNoGesture(t, r.frame(notch), "a wheel step")
		clock = clock.Add(50 * time.Millisecond)
	}
	wantMin, wantMax, _ := r.last.AxisLimits(AxisX1)
	requireNoGesture(t, r.frame(nil), "a pause under 300 ms")
	clock = clock.Add(wheelQuiet)
	requireNoGesture(t, r.frame(nil), "the frame the run finished in")
	vmin, vmax, kind, ok := r.frame(nil).RangeGesture(AxisX1)
	if !ok || kind != GestureWheelZoom || vmin != wantMin || vmax != wantMax {
		t.Errorf("reports (%v,%v,%v,%v), want one wheel zoom to [%v,%v]", vmin, vmax, kind, ok, wantMin, wantMax)
	}
	requireNoGesture(t, r.frame(nil), "after the report")
}

// A double-click reports a fit with the fitted range; an auto-fit and a
// link adopting another plot's range report nothing.
func TestRangeGestureFitAndNonGestures(t *testing.T) {
	var linkMin, linkMax float64
	xs := []float64{0, 2}
	r := newGestureRig(t, func(p *Plot) {
		p.SetupAxes("", "", AxisFlagsAutoFit, AxisFlagsNone)
		p.SetupAxisLinks(AxisY1, &linkMin, &linkMax)
		p.Line("l", xs, []float64{0, 1})
	})
	r.frame(nil)
	linkMin, linkMax = -5, 5 // another plot moved the linked axis
	xs = []float64{0, 3}     // and the auto-fit moves x
	requireNoGesture(t, r.frame(nil), "a link and an auto-fit")
	if lo, hi, _ := r.last.AxisLimits(AxisY1); lo != -5 || hi != 5 {
		t.Fatalf("the link did not move y (%v,%v); the case above tested nothing", lo, hi)
	}
	requireNoGesture(t, r.frame(nil), "the frame after")

	r.frame(func() { r.at(1, 0, c.DoubleClickedResponseFlags, false) })
	p := r.frame(nil)
	if vmin, vmax, kind, ok := p.RangeGesture(AxisY1); !ok || kind != GestureFit || vmin != 0 || vmax != 1 {
		t.Errorf("y reports (%v,%v,%v,%v), want a fit to the data [0,1]", vmin, vmax, kind, ok)
	}
	if _, _, _, ok := (*Plot)(nil).RangeGesture(AxisX1); ok {
		t.Error("a nil plot reports a gesture")
	}
}

// A selection covers the drag on the axes it selects, held to the plot
// area; a selected side of 5 px or under is a click and selects nothing.
func TestSelectRanges(t *testing.T) {
	pr := barsTransform() // [0,10]² onto 100×100 px; px y = 100 − 10·v
	rx, _, okX, okY := selectRanges(pr, SelectX, [2]float32{70, 50}, [2]float32{20, 52})
	if !okX || okY || rx != (Range{2, 7}) {
		t.Errorf("SelectX = %v (%v,%v), want x{2 7} only — a short y side does not matter", rx, okX, okY)
	}
	_, ry, okX, okY := selectRanges(pr, SelectY, [2]float32{50, 20}, [2]float32{50, 60})
	if okX || !okY || ry != (Range{4, 8}) {
		t.Errorf("SelectY = %v (%v,%v), want y{4 8} only", ry, okX, okY)
	}
	rx, ry, okX, okY = selectRanges(pr, SelectXY, [2]float32{-30, 20}, [2]float32{20, 160})
	if !okX || !okY || rx != (Range{0, 2}) || ry != (Range{0, 8}) {
		t.Errorf("SelectXY past the area = x%v y%v (%v,%v), want x{0 2} y{0 8}", rx, ry, okX, okY)
	}
	if _, _, okX, okY = selectRanges(pr, SelectXY, [2]float32{20, 20}, [2]float32{60, 24}); okX || okY {
		t.Errorf("a 4 px tall SelectXY box selected (%v,%v)", okX, okY)
	}
	if _, _, okX, _ = selectRanges(pr, SelectX, [2]float32{-40, 20}, [2]float32{-10, 60}); okX {
		t.Error("a drag wholly left of the area selected a range")
	}
}

// Under DragSelects a plain drag reports a selection of the range under it,
// once, and leaves the view where it was; Shift+drag still box-zooms, and
// without the mode the same drag pans.
func TestRangeGestureSelect(t *testing.T) {
	mode := SelectX
	r := newGestureRig(t, func(p *Plot) {
		p.DragSelects(mode)
		p.SetupAxisLimits(AxisX1, 0, 10, CondOnce).SetupAxisLimits(AxisY1, 0, 10, CondOnce)
		p.Line("l", []float64{0, 10}, []float64{0, 10})
	})
	drag := func(x0, y0, x1, y1 float64, shift bool) *Plot {
		r.frame(func() { r.at(x0, y0, c.DragStartedResponseFlags, shift) })
		r.frame(func() { r.at(x1, y1, c.DraggedResponseFlags, shift) })
		r.frame(func() { r.at(x1, y1, c.DragStoppedResponseFlags, shift) })
		return r.frame(nil)
	}
	r.frame(nil)

	p := drag(2, 3, 6, 8, false)
	if vmin, vmax, kind, ok := p.RangeGesture(AxisX1); !ok || kind != GestureSelect || math.Abs(vmin-2) > 0.05 || math.Abs(vmax-6) > 0.05 {
		t.Errorf("x reports (%v,%v,%v,%v), want a selection of [2,6]", vmin, vmax, kind, ok)
	}
	if _, _, k, ok := p.RangeGesture(AxisY1); ok {
		t.Errorf("SelectX reported %v on y", k)
	}
	for _, a := range []AxisE{AxisX1, AxisY1} {
		if lo, hi, _ := p.AxisLimits(a); lo != 0 || hi != 10 {
			t.Errorf("a selection moved axis %d to [%v,%v]", a, lo, hi)
		}
	}
	requireNoGesture(t, r.frame(nil), "the frame after the selection's report")

	if _, _, kind, ok := drag(2, 3, 6, 8, true).RangeGesture(AxisX1); !ok || kind != GestureBoxZoom {
		t.Errorf("Shift+drag under DragSelects reports (%v,%v), want a box zoom", kind, ok)
	}

	mode = SelectNone
	r.frame(nil)
	r.frame(nil)
	if _, _, kind, ok := drag(2, 3, 6, 8, false).RangeGesture(AxisX1); !ok || kind != GesturePan {
		t.Errorf("a plain drag without DragSelects reports (%v,%v), want a pan", kind, ok)
	}
}
