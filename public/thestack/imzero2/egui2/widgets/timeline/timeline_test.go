package timeline

import (
	"math"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/math/numerical/timeticks"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
)

// =============================================================================
// Pure-function tests (no Timeline construction needed)
// =============================================================================

func TestClamp01(t *testing.T) {
	cases := []struct {
		in, want float32
	}{
		{-0.5, 0},
		{0, 0},
		{0.5, 0.5},
		{1, 1},
		{1.5, 1},
	}
	for _, tc := range cases {
		if got := clamp01(tc.in); got != tc.want {
			t.Errorf("clamp01(%v): got %v want %v", tc.in, got, tc.want)
		}
	}
}

func TestClamp01_NaN(t *testing.T) {
	nan := float32(math.NaN())
	if got := clamp01(nan); got != 0 {
		t.Errorf("clamp01(NaN): got %v want 0", got)
	}
}

func TestClamp01ToRange(t *testing.T) {
	cases := []struct {
		v, lo, hi, want float32
	}{
		{-1, 0, 10, 0},
		{5, 0, 10, 5},
		{20, 0, 10, 10},
	}
	for _, tc := range cases {
		if got := clamp01ToRange(tc.v, tc.lo, tc.hi); got != tc.want {
			t.Errorf("clamp01ToRange(%v,%v,%v): got %v want %v", tc.v, tc.lo, tc.hi, got, tc.want)
		}
	}
}

func TestCursorInsideCanvas(t *testing.T) {
	nan := float32(math.NaN())
	cases := []struct {
		name   string
		hx, hy float32
		w, h   float32
		want   bool
	}{
		{"nan_x", nan, 50, 100, 100, false},
		{"nan_y", 50, nan, 100, 100, false},
		{"inside", 50, 50, 100, 100, true},
		{"outside_x", 150, 50, 100, 100, false},
		{"outside_y", 50, 150, 100, 100, false},
		{"corner", 100, 100, 100, 100, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := canvasPointer{HoverX: tc.hx, HoverY: tc.hy}
			if got := cursorInsideCanvas(cp, tc.w, tc.h); got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

// TestApplyZoomInput exercises the ADR-0140-migrated zoom handler: it consumes
// a per-canvas CanvasWheelValue (factor + canvas-local anchor) instead of the
// global zoom register, and is a pure viewport transform, so it needs no FFI.
func TestApplyZoomInput(t *testing.T) {
	const effW float32 = 100
	// A timeline pinned to a known [0,1000]ms view. explicitRange short-circuits
	// pinToCurrentView so the handler operates on exactly these bounds.
	newTL := func() *Timeline {
		return &Timeline{explicitRange: true, viewMinMS: 0, viewMaxMS: 1000}
	}

	t.Run("identity_zoom_is_noop", func(t *testing.T) {
		tl := newTL()
		tl.applyZoomInput(c.CanvasWheelValue{Zoom: 1.0, HoverX: 50}, effW)
		if tl.viewMinMS != 0 || tl.viewMaxMS != 1000 {
			t.Errorf("zoom==1 mutated view: [%d,%d]", tl.viewMinMS, tl.viewMaxMS)
		}
	})

	// NaN anchor is how "this canvas did not own the wheel" arrives (the R23
	// default) — even a real zoom factor must be ignored.
	t.Run("nan_anchor_is_noop", func(t *testing.T) {
		tl := newTL()
		tl.applyZoomInput(c.CanvasWheelValue{Zoom: 2.0, HoverX: float32(math.NaN())}, effW)
		if tl.viewMinMS != 0 || tl.viewMaxMS != 1000 {
			t.Errorf("NaN anchor mutated view: [%d,%d]", tl.viewMinMS, tl.viewMaxMS)
		}
	})

	// Zoom in x2 anchored at the left edge: span halves, the anchored instant
	// (t=0) stays put.
	t.Run("zoom_in_left_anchor", func(t *testing.T) {
		tl := newTL()
		tl.applyZoomInput(c.CanvasWheelValue{Zoom: 2.0, HoverX: 0}, effW)
		if got := tl.viewMaxMS - tl.viewMinMS; got != 500 {
			t.Errorf("span: got %d want 500", got)
		}
		if tl.viewMinMS != 0 {
			t.Errorf("left anchor moved: viewMinMS=%d want 0", tl.viewMinMS)
		}
	})

	// Zoom in x2 anchored at the centre: span halves around t=500.
	t.Run("zoom_in_centre_anchor", func(t *testing.T) {
		tl := newTL()
		tl.applyZoomInput(c.CanvasWheelValue{Zoom: 2.0, HoverX: effW / 2}, effW)
		if tl.viewMinMS != 250 || tl.viewMaxMS != 750 {
			t.Errorf("centre anchor: got [%d,%d] want [250,750]", tl.viewMinMS, tl.viewMaxMS)
		}
	})

	// Zoom out (factor < 1) widens the span; centre anchor keeps t=500 centred.
	t.Run("zoom_out_centre_anchor", func(t *testing.T) {
		tl := newTL()
		tl.applyZoomInput(c.CanvasWheelValue{Zoom: 0.5, HoverX: effW / 2}, effW)
		if got := tl.viewMaxMS - tl.viewMinMS; got != 2000 {
			t.Errorf("span: got %d want 2000", got)
		}
		if tl.viewMinMS != -500 || tl.viewMaxMS != 1500 {
			t.Errorf("centre anchor: got [%d,%d] want [-500,1500]", tl.viewMinMS, tl.viewMaxMS)
		}
	})
}

func TestSplitLines(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"one", []string{"one"}},
		{"one\ntwo", []string{"one", "two"}},
		{"a\nb\nc", []string{"a", "b", "c"}},
		{"trailing\n", []string{"trailing", ""}},
	}
	for _, tc := range cases {
		got := splitLines(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitLines(%q): got %v want %v", tc.in, got, tc.want)
			continue
		}
		for i, line := range got {
			if line != tc.want[i] {
				t.Errorf("splitLines(%q)[%d]: got %q want %q", tc.in, i, line, tc.want[i])
			}
		}
	}
}

func TestFormatAnnotationTooltip(t *testing.T) {
	a := &layout.Annotation{TMS: time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC).UnixMilli(), Number: 3, Label: "deploy"}
	got := formatAnnotationTooltip(a)
	if got != "#3  deploy\n2026-05-15 09:00:00" {
		t.Errorf("got %q", got)
	}
	a.Label = ""
	got = formatAnnotationTooltip(a)
	if got != "#3\n2026-05-15 09:00:00" {
		t.Errorf("no-label: got %q", got)
	}
}

func TestFormatBandTooltip(t *testing.T) {
	b := layout.BackgroundBand{
		FromMS: time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC).UnixMilli(),
		ToMS:   time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC).UnixMilli(),
		Label:  "Saturday",
	}
	got := formatBandTooltip(b)
	if got != "Saturday\n2026-05-16 00:00 – 2026-05-17 00:00" {
		t.Errorf("got %q", got)
	}
}

// =============================================================================
// Timeline construction + state lifecycle (needs ids stack)
// =============================================================================

func newTestTimeline(t *testing.T, intervals []*layout.IntervalEvent, opts ...Option) *Timeline {
	t.Helper()
	ids := c.NewWidgetIdStack()
	return New(ids, "test-tl", intervals, opts...)
}

func TestNew_DefaultsApplied(t *testing.T) {
	tl := newTestTimeline(t, nil)
	if tl.containerW != defaultContainerW {
		t.Errorf("containerW default: got %v want %v", tl.containerW, defaultContainerW)
	}
	if !tl.interactionEnabled {
		t.Error("interactionEnabled should default true")
	}
	if tl.rawPointThreshold != defaultRawPointThreshold {
		t.Errorf("rawPointThreshold: got %d", tl.rawPointThreshold)
	}
	if tl.visuals.LaneHeight == 0 {
		t.Error("DefaultVisuals not applied (LaneHeight zero)")
	}
	if !tl.intensityEncoded {
		t.Error("intensityEncoded should default true")
	}
	wantInterval := color.Hex(styletokens.AccentDefault.AsHex()).Keep()
	if tl.visuals.IntervalColor != wantInterval {
		t.Errorf("IntervalColor default: got %+v want %+v", tl.visuals.IntervalColor, wantInterval)
	}
	wantPoint := color.Hex(styletokens.InfoDefault.AsHex()).Keep()
	if tl.visuals.PointColor != wantPoint {
		t.Errorf("PointColor default: got %+v want %+v", tl.visuals.PointColor, wantPoint)
	}
}

// TestIntensityEncoding_OptionAndSetter locks in the construction-time
// option and the runtime setter that the play HMI flips per query: with no
// _tl_intensity column the colormap would collapse every glyph to its dark,
// near-invisible end, so the host turns encoding off to fall back to the
// flat IntervalColor / PointColor fills.
func TestIntensityEncoding_OptionAndSetter(t *testing.T) {
	tl := newTestTimeline(t, nil, WithIntensityEncoding(false))
	if tl.intensityEncoded {
		t.Error("WithIntensityEncoding(false) should clear intensityEncoded")
	}
	tl.SetIntensityEncoding(true)
	if !tl.intensityEncoded {
		t.Error("SetIntensityEncoding(true) should set intensityEncoded")
	}
	tl.SetIntensityEncoding(false)
	if tl.intensityEncoded {
		t.Error("SetIntensityEncoding(false) should clear intensityEncoded")
	}
}

// TestIntervalColors_Option locks in the categorical interval palette: when set
// it is stored verbatim and (per paintIntervals) takes precedence over intensity
// encoding so callers can paint bars by a discrete category (e.g. deploy state).
func TestIntervalColors_Option(t *testing.T) {
	if tl := newTestTimeline(t, nil); len(tl.intervalColors) != 0 {
		t.Errorf("intervalColors should default empty, got %d", len(tl.intervalColors))
	}
	palette := []color.Color{
		color.Hex(styletokens.SuccessDefault.AsHex()).Keep(),
		color.Hex(styletokens.ErrorDefault.AsHex()).Keep(),
	}
	tl := newTestTimeline(t, nil, WithIntervalColors(palette))
	if len(tl.intervalColors) != len(palette) {
		t.Fatalf("intervalColors len: got %d want %d", len(tl.intervalColors), len(palette))
	}
	for i := range palette {
		if tl.intervalColors[i] != palette[i] {
			t.Errorf("intervalColors[%d]: got %+v want %+v", i, tl.intervalColors[i], palette[i])
		}
	}
}

func TestNew_PanicsOnNilIds(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on nil ids")
		}
	}()
	_ = New(nil, "k", nil)
}

func TestNew_PanicsOnEmptyScopeKey(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on empty scope key")
		}
	}()
	ids := c.NewWidgetIdStack()
	_ = New(ids, "", nil)
}

func TestComputeViewRange_AutoFit(t *testing.T) {
	a := &layout.IntervalEvent{FromMS: 1000, ToMS: 2000}
	b := &layout.IntervalEvent{FromMS: 5000, ToMS: 6000}
	tl := newTestTimeline(t, []*layout.IntervalEvent{a, b})
	t0, t1 := tl.computeViewRange()
	// Auto-fit: from min FromMS to max ToMS, padded by 2% on each side.
	if t0.UnixMilli() >= 1000 {
		t.Errorf("t0: got %d want < 1000 (padding)", t0.UnixMilli())
	}
	if t1.UnixMilli() <= 6000 {
		t.Errorf("t1: got %d want > 6000 (padding)", t1.UnixMilli())
	}
}

func TestComputeViewRange_Explicit(t *testing.T) {
	pinned := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	pinnedEnd := pinned.Add(time.Hour)
	tl := newTestTimeline(t, nil, WithRange(pinned, pinnedEnd))
	t0, t1 := tl.computeViewRange()
	if !t0.Equal(pinned) || !t1.Equal(pinnedEnd) {
		t.Errorf("explicit range not preserved: got [%v,%v]", t0, t1)
	}
}

func TestComputeViewRange_EmptyFallbackToNowHour(t *testing.T) {
	tl := newTestTimeline(t, nil)
	t0, t1 := tl.computeViewRange()
	span := t1.Sub(t0)
	if span < emptyFallback || span > emptyFallback+time.Second {
		t.Errorf("empty fallback span: got %v want ~%v", span, emptyFallback)
	}
}

func TestPinToCurrentView_DegenerateReturnsFalse(t *testing.T) {
	// Empty timeline with no explicit range — computeViewRange returns
	// [now-1h, now] which is NOT degenerate. We need to construct a case
	// where the auto-fit range is degenerate. Easiest: single instant.
	ev := &layout.IntervalEvent{FromMS: 1000, ToMS: 1000}
	tl := newTestTimeline(t, []*layout.IntervalEvent{ev})
	// With padding, even single-instant gets 1ms padding on each side
	// → non-degenerate. So pin succeeds.
	if !tl.pinToCurrentView() {
		t.Error("non-degenerate auto-fit should pin successfully")
	}
	if !tl.explicitRange || !tl.interactivePin {
		t.Errorf("after pin: explicitRange=%v interactivePin=%v want both true", tl.explicitRange, tl.interactivePin)
	}
}

func TestPinToCurrentView_Idempotent(t *testing.T) {
	tl := newTestTimeline(t, []*layout.IntervalEvent{{FromMS: 0, ToMS: 100}})
	tl.pinToCurrentView()
	min1, max1 := tl.viewMinMS, tl.viewMaxMS
	tl.pinToCurrentView()
	if tl.viewMinMS != min1 || tl.viewMaxMS != max1 {
		t.Errorf("second pin mutated state: was [%d,%d] now [%d,%d]", min1, max1, tl.viewMinMS, tl.viewMaxMS)
	}
}

// TestPanBy_ShiftsViewWithoutResizing locks in the grab-and-drag contract:
// dragging right walks the view backwards in time (content follows the
// cursor) and the span is untouched — pan translates, only zoom scales.
func TestPanBy_ShiftsViewWithoutResizing(t *testing.T) {
	tl := newTestTimeline(t, nil, WithRange(
		time.UnixMilli(0).UTC(), time.UnixMilli(1000).UTC()))
	// 1000 ms over a 100-px axis (labelW=0) → 10 ms/px. Drag right by 10 px
	// → the view moves 100 ms earlier.
	tl.panBy(10, 0, 100)
	if tl.viewMinMS != -100 || tl.viewMaxMS != 900 {
		t.Errorf("drag right: got [%d,%d] want [-100,900]", tl.viewMinMS, tl.viewMaxMS)
	}
	tl.panBy(-10, 0, 100)
	if tl.viewMinMS != 0 || tl.viewMaxMS != 1000 {
		t.Errorf("drag back left should restore: got [%d,%d] want [0,1000]", tl.viewMinMS, tl.viewMaxMS)
	}
}

// TestPanBy_ExcludesLabelBandFromScale guards the drift bug: the tick map maps
// [labelW, effW] onto the view, so pan must divide by the axis width, not the
// container width, or the data slides against the ticks under the cursor.
func TestPanBy_ExcludesLabelBandFromScale(t *testing.T) {
	tl := newTestTimeline(t, nil, WithRange(
		time.UnixMilli(0).UTC(), time.UnixMilli(1000).UTC()))
	// effW=200, labelW=100 → axis is 100 px wide → 10 ms/px, as above. If the
	// label band leaked into the scale it would be 5 ms/px and drift by half.
	tl.panBy(10, 100, 200)
	if tl.viewMinMS != -100 || tl.viewMaxMS != 900 {
		t.Errorf("with label band: got [%d,%d] want [-100,900]", tl.viewMinMS, tl.viewMaxMS)
	}
}

// TestPanBy_PinsAutoFitView locks in that the first pan materializes the
// auto-fit range as a user-driven pin, so a later data swap reverts to
// auto-fit rather than stranding the user over stale coordinates.
func TestPanBy_PinsAutoFitView(t *testing.T) {
	tl := newTestTimeline(t, []*layout.IntervalEvent{{FromMS: 0, ToMS: 1000}})
	if tl.explicitRange {
		t.Fatal("setup: auto-fit expected before pan")
	}
	tl.panBy(10, 0, 100)
	if !tl.explicitRange || !tl.interactivePin {
		t.Errorf("pan should pin interactively: explicitRange=%v interactivePin=%v",
			tl.explicitRange, tl.interactivePin)
	}
}

// TestPanBy_DegenerateInputsAreNoOps covers the guards: a zero delta, and an
// axis fully consumed by the label band (labelW >= effW, reachable when a
// narrow pane meets a long lane hint) must not move or corrupt the view.
func TestPanBy_DegenerateInputsAreNoOps(t *testing.T) {
	cases := []struct {
		name             string
		dx, labelW, effW float32
	}{
		{"zero delta", 0, 0, 100},
		{"label band eats the axis", 10, 200, 200},
		{"label band wider than container", 10, 300, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tl := newTestTimeline(t, nil, WithRange(
				time.UnixMilli(0).UTC(), time.UnixMilli(1000).UTC()))
			tl.panBy(tc.dx, tc.labelW, tc.effW)
			if tl.viewMinMS != 0 || tl.viewMaxMS != 1000 {
				t.Errorf("view moved: got [%d,%d] want [0,1000]", tl.viewMinMS, tl.viewMaxMS)
			}
		})
	}
}

func TestSetIntervals_DropsInteractivePin(t *testing.T) {
	ev := &layout.IntervalEvent{FromMS: 0, ToMS: 100}
	tl := newTestTimeline(t, []*layout.IntervalEvent{ev})
	tl.pinToCurrentView() // simulate user pan
	if !tl.interactivePin {
		t.Fatal("setup: expected interactivePin true after pin")
	}
	tl.SetIntervals([]*layout.IntervalEvent{{FromMS: 1000, ToMS: 2000}})
	if tl.interactivePin || tl.explicitRange {
		t.Errorf("SetIntervals should drop interactive pin: interactivePin=%v explicitRange=%v",
			tl.interactivePin, tl.explicitRange)
	}
}

func TestSetIntervals_PreservesCallerDrivenPin(t *testing.T) {
	pinned := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	tl := newTestTimeline(t, nil, WithRange(pinned, pinned.Add(time.Hour)))
	if tl.interactivePin {
		t.Fatal("WithRange should not set interactivePin")
	}
	tl.SetIntervals([]*layout.IntervalEvent{{FromMS: 0, ToMS: 100}})
	if !tl.explicitRange {
		t.Error("SetIntervals dropped caller-driven explicit range — should preserve")
	}
}

func TestSelection_DefaultIsNone(t *testing.T) {
	tl := newTestTimeline(t, nil)
	sel := tl.Selection()
	if sel.Kind != SelectionNone {
		t.Errorf("default selection: got %v want SelectionNone", sel.Kind)
	}
	if sel.Interval != nil || sel.Bucket != nil || sel.Annotation != nil {
		t.Error("default selection: pointers should all be nil")
	}
}

func TestSelectAnnotationByNumber(t *testing.T) {
	a1 := &layout.Annotation{TMS: 1000, Number: 1, Label: "one"}
	a2 := &layout.Annotation{TMS: 2000, Number: 2, Label: "two"}
	tl := newTestTimeline(t, nil, WithAnnotations([]*layout.Annotation{a1, a2}))
	tl.SelectAnnotationByNumber(2)
	sel := tl.Selection()
	if sel.Kind != SelectionAnnotation || sel.Annotation != a2 {
		t.Errorf("select-by-number 2: got Kind=%v Annotation=%p (want %p)", sel.Kind, sel.Annotation, a2)
	}
	// Unknown number is a no-op (preserves existing selection).
	tl.SelectAnnotationByNumber(99)
	if sel := tl.Selection(); sel.Annotation != a2 {
		t.Error("select-by-unknown-number changed state")
	}
}

func TestSelectIntervalByPointer_Hit(t *testing.T) {
	a := &layout.IntervalEvent{FromMS: 0, ToMS: 100}
	b := &layout.IntervalEvent{FromMS: 200, ToMS: 300}
	tl := newTestTimeline(t, []*layout.IntervalEvent{a, b})
	tl.SelectIntervalByPointer(b)
	sel := tl.Selection()
	if sel.Kind != SelectionInterval || sel.Interval != b {
		t.Errorf("got Kind=%v Interval=%p want SelectionInterval %p", sel.Kind, sel.Interval, b)
	}
}

func TestSelectIntervalByPointer_MissNoOp(t *testing.T) {
	a := &layout.IntervalEvent{FromMS: 0, ToMS: 100}
	stranger := &layout.IntervalEvent{FromMS: 0, ToMS: 100}
	tl := newTestTimeline(t, []*layout.IntervalEvent{a})
	tl.SelectIntervalByPointer(stranger)
	if tl.Selection().Kind != SelectionNone {
		t.Errorf("stranger pointer should not change selection; got %v", tl.Selection().Kind)
	}
}

func TestSelectIntervalByPointer_NilNoOp(t *testing.T) {
	a := &layout.IntervalEvent{FromMS: 0, ToMS: 100}
	tl := newTestTimeline(t, []*layout.IntervalEvent{a})
	tl.SelectIntervalByPointer(nil)
	if tl.Selection().Kind != SelectionNone {
		t.Errorf("nil pointer should be a no-op; got %v", tl.Selection().Kind)
	}
}

func TestSelectBucketAt_BeforeFirstRenderNoOp(t *testing.T) {
	tl := newTestTimeline(t, nil, WithPointEvents([]*layout.PointEvent{{TMS: 1000}}))
	// lastViewPxWidth == 0 (no render yet).
	tl.SelectBucketAt(1000)
	if tl.Selection().Kind != SelectionNone {
		t.Error("SelectBucketAt before first render should be a no-op")
	}
}

func TestSelectBucketAt_NoLODIndexNoOp(t *testing.T) {
	tl := newTestTimeline(t, nil)
	// rebuildLOD ran with empty scales → lodIndex still non-nil but empty.
	// Fake a "last-view" so the guard passes.
	tl.lastViewMinMS = 0
	tl.lastViewMaxMS = 10_000
	tl.lastViewPxWidth = 100
	tl.SelectBucketAt(5000)
	if tl.Selection().Kind != SelectionNone {
		t.Error("SelectBucketAt with no points should be a no-op")
	}
}

func TestSelectBucketAt_HitsBucketContainingT(t *testing.T) {
	tl := newTestTimeline(t, nil,
		WithPointEvents([]*layout.PointEvent{
			{TMS: 1500, Intensity: 0.5},
		}),
		WithLODScales([]time.Duration{1 * time.Second}))
	// Simulate the renderBody view cache: 10-second view at 100 px wide
	// → 1ms/px which is finer than the 1-second scale, so PickScale
	// picks the 1-second bucket.
	tl.lastViewMinMS = 0
	tl.lastViewMaxMS = 10_000
	tl.lastViewPxWidth = 100
	tl.SelectBucketAt(1700) // inside [1000, 2000) at 1s scale
	sel := tl.Selection()
	if sel.Kind != SelectionBucket {
		t.Fatalf("expected SelectionBucket, got %v", sel.Kind)
	}
	if sel.Bucket == nil || sel.Bucket.StartMS != 1000 {
		t.Errorf("Bucket: got %+v want StartMS=1000", sel.Bucket)
	}
	if sel.Bucket.Count != 1 {
		t.Errorf("Count: got %d want 1", sel.Bucket.Count)
	}
}

func TestSelectBucketAt_NoEventInBucketNoOp(t *testing.T) {
	tl := newTestTimeline(t, nil,
		WithPointEvents([]*layout.PointEvent{{TMS: 1500}}),
		WithLODScales([]time.Duration{1 * time.Second}))
	tl.lastViewMinMS = 0
	tl.lastViewMaxMS = 10_000
	tl.lastViewPxWidth = 100
	tl.SelectBucketAt(5500) // inside [5000, 6000) at 1s scale — no event there
	if tl.Selection().Kind != SelectionNone {
		t.Error("SelectBucketAt with no event at tMS should be a no-op")
	}
}

func TestClearSelection(t *testing.T) {
	a := &layout.Annotation{TMS: 1000, Number: 1}
	tl := newTestTimeline(t, nil, WithAnnotations([]*layout.Annotation{a}))
	tl.SelectAnnotationByNumber(1)
	tl.ClearSelection()
	if tl.Selection().Kind != SelectionNone {
		t.Error("ClearSelection should reset to SelectionNone")
	}
}

func TestSetAnnotations_ClearsAnnotationSelection(t *testing.T) {
	a := &layout.Annotation{TMS: 1000, Number: 1}
	tl := newTestTimeline(t, nil, WithAnnotations([]*layout.Annotation{a}))
	tl.SelectAnnotationByNumber(1)
	if tl.Selection().Kind != SelectionAnnotation {
		t.Fatal("setup: annotation should be selected")
	}
	tl.SetAnnotations(nil)
	if tl.Selection().Kind != SelectionNone {
		t.Error("SetAnnotations should clear annotation selection")
	}
}

func TestEffectiveContainerW_AvailableOverridesFallback(t *testing.T) {
	tl := newTestTimeline(t, nil, WithContainerWidth(500))
	if w := tl.effectiveContainerW(1000, true); w != 1000 {
		t.Errorf("probe width: got %v want 1000", w)
	}
}

func TestEffectiveContainerW_NaNFallsBackToContainer(t *testing.T) {
	tl := newTestTimeline(t, nil, WithContainerWidth(500))
	if w := tl.effectiveContainerW(float32(math.NaN()), true); w != 500 {
		t.Errorf("NaN probe width: got %v want 500 (fallback)", w)
	}
}

// A seq that never captured reads back absent, not zero — the fallback has to
// survive it, which is what a hidden tab hits on the frame it comes back.
func TestEffectiveContainerW_NoProbeFallsBackToContainer(t *testing.T) {
	tl := newTestTimeline(t, nil, WithContainerWidth(500))
	if w := tl.effectiveContainerW(0, false); w != 500 {
		t.Errorf("absent probe: got %v want 500 (fallback)", w)
	}
}

func TestComputeLabelW_NoHints(t *testing.T) {
	intervals := []*layout.IntervalEvent{
		{FromMS: 0, ToMS: 10}, // no LaneHint
	}
	tl := newTestTimeline(t, intervals)
	// renderBody normally populates laneAssn; do it manually for the test.
	tl.laneAssn = layout.PackLanes(intervals)
	if w := tl.computeLabelW(); w != 0 {
		t.Errorf("no-hints labelW: got %v want 0", w)
	}
}

func TestComputeLabelW_WithHints(t *testing.T) {
	intervals := []*layout.IntervalEvent{
		{FromMS: 0, ToMS: 10, LaneHint: "alice"},
		{FromMS: 0, ToMS: 10, LaneHint: "bobby"}, // 5 chars
	}
	tl := newTestTimeline(t, intervals)
	tl.laneAssn = layout.PackLanes(intervals)
	w := tl.computeLabelW()
	want := float32(5)*tooltipCharWidthPx + 2*labelPaddingX
	if w != want {
		t.Errorf("labelW: got %v want %v", w, want)
	}
}

func TestComputeVerticalLayout_RugAndAnnotationsReserveSpace(t *testing.T) {
	intervals := []*layout.IntervalEvent{{FromMS: 0, ToMS: 100}}
	tl := newTestTimeline(t, intervals,
		WithPointEvents([]*layout.PointEvent{{TMS: 50}}),
		WithAnnotations([]*layout.Annotation{{TMS: 50, Number: 1}}))
	tl.laneAssn = layout.PackLanes(intervals)
	vl := tl.computeVerticalLayout(0, 1, 0, 1000)
	if vl.topReserved == 0 {
		t.Error("annotations should reserve top space")
	}
	if vl.laneBaseY <= vl.topReserved {
		t.Errorf("rug should push laneBaseY below topReserved: top=%v laneBase=%v",
			vl.topReserved, vl.laneBaseY)
	}
	if vl.axisStartPx != 0 || vl.axisEndPx != 1000 {
		t.Errorf("axis bounds: got [%v,%v] want [0,1000]", vl.axisStartPx, vl.axisEndPx)
	}
}

func TestComputeVerticalLayout_NoExtras(t *testing.T) {
	intervals := []*layout.IntervalEvent{{FromMS: 0, ToMS: 100}}
	tl := newTestTimeline(t, intervals)
	tl.laneAssn = layout.PackLanes(intervals)
	vl := tl.computeVerticalLayout(0, 1, 0, 1000)
	if vl.topReserved != 0 {
		t.Errorf("no-annotations: topReserved should be 0, got %v", vl.topReserved)
	}
	if vl.laneBaseY != 0 {
		t.Errorf("no-rug: laneBaseY should be 0, got %v", vl.laneBaseY)
	}
}

func TestComputeVerticalLayout_FlagRowsGrowAnnotationBand(t *testing.T) {
	intervals := []*layout.IntervalEvent{{FromMS: 0, ToMS: 100}}
	tl := newTestTimeline(t, intervals,
		WithAnnotations([]*layout.Annotation{{TMS: 50, Number: 1}}))
	tl.laneAssn = layout.PackLanes(intervals)
	oneRow := tl.computeVerticalLayout(0, 1, 0, 1000)
	twoRows := tl.computeVerticalLayout(0, 2, 0, 1000)
	wantGrowth := tl.visuals.AnnotationBandH
	if got := twoRows.topReserved - oneRow.topReserved; got != wantGrowth {
		t.Errorf("second flag row should add AnnotationBandH: got +%v want +%v", got, wantGrowth)
	}
	// flagRows < 1 clamps to 1 — the band must not collapse while panning
	// all flags off-view.
	zeroRows := tl.computeVerticalLayout(0, 0, 0, 1000)
	if zeroRows.topReserved != oneRow.topReserved {
		t.Errorf("flagRows=0 should clamp to one row: got %v want %v",
			zeroRows.topReserved, oneRow.topReserved)
	}
}

func TestComputeFlagLayout_FiltersOffscreenAndStaggers(t *testing.T) {
	// View 0..1000ms mapped to x 0..1000 → 1px/ms. Default flag width 26:
	// annotations at 500/510ms collide (10px apart) and stagger; 800ms is
	// far enough for row 0; 2000ms is off-view and must not participate.
	coincidentA := &layout.Annotation{TMS: 500, Number: 1}
	coincidentB := &layout.Annotation{TMS: 510, Number: 2}
	lone := &layout.Annotation{TMS: 800, Number: 3}
	offscreen := &layout.Annotation{TMS: 2000, Number: 4}
	tl := newTestTimeline(t, nil,
		WithAnnotations([]*layout.Annotation{coincidentA, coincidentB, lone, nil, offscreen}))
	tm := layout.ComputeTickMap(time.UnixMilli(0).UTC(), time.UnixMilli(1000).UTC(),
		0, 1000, nil, timeticks.TimeStep{})
	fl := tl.computeFlagLayout(tm, 0, 1000)
	if len(fl.anns) != 3 {
		t.Fatalf("visible annotations: got %d want 3 (nil + offscreen filtered)", len(fl.anns))
	}
	if fl.anns[0] != coincidentA || fl.anns[1] != coincidentB || fl.anns[2] != lone {
		t.Fatalf("visible order should preserve input order, got %v", fl.anns)
	}
	if fl.rowCount != 2 {
		t.Errorf("rowCount: got %d want 2", fl.rowCount)
	}
	if fl.rows[0] != 0 || fl.rows[1] != 1 || fl.rows[2] != 0 {
		t.Errorf("rows: got %v want [0,1,0]", fl.rows)
	}
}

func TestHitTestAnnotation_StaggeredFlagsResolveByRow(t *testing.T) {
	a := &layout.Annotation{TMS: 500, Number: 1}
	b := &layout.Annotation{TMS: 500, Number: 2} // same instant — full x tie
	tl := newTestTimeline(t, nil, WithAnnotations([]*layout.Annotation{a, b}))
	fl := flagLayout{
		anns:     []*layout.Annotation{a, b},
		xs:       []float32{500, 500},
		rows:     []int32{0, 1},
		rowCount: 2,
	}
	rowH := tl.visuals.AnnotationBandH
	if got := tl.hitTestAnnotation(fl, 500, rowH*0.5); got != a {
		t.Errorf("click in row-0 slab: got %v want a (#1)", got)
	}
	if got := tl.hitTestAnnotation(fl, 500, rowH*1.5); got != b {
		t.Errorf("click in row-1 slab: got %v want b (#2)", got)
	}
	// Below the band both dashes coincide: the corridor tier resolves the
	// tie toward the later slice entry, matching paint z-order.
	if got := tl.hitTestAnnotation(fl, 500, rowH*4); got != b {
		t.Errorf("dash-corridor tie: got %v want b (painted on top)", got)
	}
	// Outside the corridor: miss.
	if got := tl.hitTestAnnotation(fl, 500+annotationHitCorridorPx+1, rowH*4); got != nil {
		t.Errorf("outside corridor: got %v want nil", got)
	}
}

func TestHitTestAnnotation_CorridorFallbackInsideBand(t *testing.T) {
	// A cursor in the flag band but on no flag rect — here: in the row-1
	// slab while the nearby flag sits in row 0 — still resolves via the
	// dash corridor, because the dashed line passes through that space.
	a := &layout.Annotation{TMS: 500, Number: 1}
	b := &layout.Annotation{TMS: 900, Number: 2}
	tl := newTestTimeline(t, nil, WithAnnotations([]*layout.Annotation{a, b}))
	fl := flagLayout{
		anns:     []*layout.Annotation{a, b},
		xs:       []float32{500, 900},
		rows:     []int32{0, 1},
		rowCount: 2,
	}
	if got := tl.hitTestAnnotation(fl, 505, tl.visuals.AnnotationBandH*1.5); got != a {
		t.Errorf("corridor fallback in band: got %v want a", got)
	}
}

func TestClipToAxis(t *testing.T) {
	vl := verticalLayout{axisStartPx: 100, axisEndPx: 500}
	cases := []struct {
		name    string
		x0, x1  float32
		wantOk  bool
		wantCx0 float32
		wantCx1 float32
	}{
		{"entirely_left", 0, 50, false, 0, 0},
		{"entirely_right", 600, 700, false, 0, 0},
		{"clip_left", 50, 200, true, 100, 200},
		{"clip_right", 400, 600, true, 400, 500},
		{"inside", 200, 300, true, 200, 300},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cx0, cx1, ok := vl.clipToAxis(tc.x0, tc.x1)
			if ok != tc.wantOk {
				t.Errorf("ok: got %v want %v", ok, tc.wantOk)
			}
			if ok && (cx0 != tc.wantCx0 || cx1 != tc.wantCx1) {
				t.Errorf("clipped: got [%v,%v] want [%v,%v]", cx0, cx1, tc.wantCx0, tc.wantCx1)
			}
		})
	}
}

// =============================================================================
// Lane selection + lane cursor
// =============================================================================

// newLaneTestTimeline builds a timeline over three hinted lanes and does
// what renderBody would: run the packer. Lane order is first-seen hint
// order — a, b, c — so lane i sits at laneBaseY + i*(LaneHeight+LaneGap).
func newLaneTestTimeline(t *testing.T, opts ...Option) (tl *Timeline, intervals []*layout.IntervalEvent) {
	t.Helper()
	intervals = []*layout.IntervalEvent{
		{FromMS: 0, ToMS: 100, LaneHint: "a"},
		{FromMS: 0, ToMS: 100, LaneHint: "b"},
		{FromMS: 0, ToMS: 100, LaneHint: "c"},
	}
	tl = newTestTimeline(t, intervals, opts...)
	tl.laneAssn = layout.PackLanes(intervals)
	return
}

func TestHitTestLane_RowsOwnTheirTrailingGap(t *testing.T) {
	tl, _ := newLaneTestTimeline(t)
	vl := tl.computeVerticalLayout(0, 0, 0, 1000)
	pitch := tl.visuals.LaneHeight + tl.visuals.LaneGap
	cases := []struct {
		name    string
		cursorY float32
		wantIdx int32
		wantOk  bool
	}{
		{"above_lane_area", vl.laneBaseY - 1, 0, false},
		{"lane_0_top", vl.laneBaseY, 0, true},
		{"lane_0_middle", vl.laneBaseY + tl.visuals.LaneHeight/2, 0, true},
		// The gutter below a bar belongs to the lane above it, so a click
		// between rows selects rather than clears.
		{"lane_0_trailing_gap", vl.laneBaseY + tl.visuals.LaneHeight + 1, 0, true},
		{"lane_1_middle", vl.laneBaseY + pitch + tl.visuals.LaneHeight/2, 1, true},
		{"lane_2_middle", vl.laneBaseY + 2*pitch + tl.visuals.LaneHeight/2, 2, true},
		// laneAreaH excludes the trailing gap after the last lane, so the
		// row band ends flush with the bars.
		{"below_lane_area", vl.laneBaseY + vl.laneAreaH, 0, false},
		{"on_the_axis", vl.axisBaselineY, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, ok := tl.hitTestLane(vl, tc.cursorY)
			if ok != tc.wantOk {
				t.Fatalf("ok: got %v want %v", ok, tc.wantOk)
			}
			if ok && idx != tc.wantIdx {
				t.Errorf("idx: got %d want %d", idx, tc.wantIdx)
			}
		})
	}
}

func TestHitTestLane_NoLanesIsAMiss(t *testing.T) {
	tl := newTestTimeline(t, nil)
	tl.laneAssn = layout.PackLanes(nil)
	// laneAreaH falls back to one LaneHeight for the empty state; nothing
	// there is clickable.
	vl := tl.computeVerticalLayout(0, 0, 0, 1000)
	if _, ok := tl.hitTestLane(vl, vl.laneBaseY+1); ok {
		t.Error("empty assignment should not report a lane hit")
	}
}

// dispatchTestTickMap mirrors the tick map renderBody builds: a 0..1000ms
// view across the full 0..1000px axis, i.e. 1px per ms.
func dispatchTestTickMap(vl verticalLayout) (tm layout.TickMap) {
	tm = layout.ComputeTickMap(
		time.UnixMilli(0).UTC(), time.UnixMilli(1000).UTC(),
		float64(vl.axisStartPx), float64(vl.axisEndPx), nil, timeticks.TimeStep{})
	return
}

func TestDispatchClick_BareLaneRowSelectsTheLane(t *testing.T) {
	var fired []SelectionInfo
	tl, _ := newLaneTestTimeline(t, WithOnSelection(func(sel SelectionInfo) {
		fired = append(fired, sel)
	}))
	vl := tl.computeVerticalLayout(0, 0, 0, 1000)
	tm := dispatchTestTickMap(vl)
	pitch := tl.visuals.LaneHeight + tl.visuals.LaneGap
	// x=500 is past the events (which end at 100ms → x=100), so this lands
	// on bare row space in lane 1.
	tl.dispatchClick(tm, flagLayout{}, vl, 500, vl.laneBaseY+pitch+tl.visuals.LaneHeight/2)

	sel := tl.Selection()
	if sel.Kind != SelectionLane {
		t.Fatalf("Kind: got %v want SelectionLane", sel.Kind)
	}
	if sel.Lane == nil || sel.Lane.Hint != "b" {
		t.Fatalf("Lane: got %+v want hint %q", sel.Lane, "b")
	}
	if len(fired) != 1 {
		t.Fatalf("listener fired %d times, want 1", len(fired))
	}
	// The listener must see the materialised lane, not a bare Kind.
	if fired[0].Lane == nil || fired[0].Lane.Hint != "b" {
		t.Errorf("listener payload: got %+v want hint %q", fired[0].Lane, "b")
	}
}

func TestDispatchClick_ClickingTheSelectedLaneAgainClears(t *testing.T) {
	tl, _ := newLaneTestTimeline(t)
	vl := tl.computeVerticalLayout(0, 0, 0, 1000)
	tm := dispatchTestTickMap(vl)
	y := vl.laneBaseY + tl.visuals.LaneHeight/2
	tl.dispatchClick(tm, flagLayout{}, vl, 500, y)
	if tl.Selection().Kind != SelectionLane {
		t.Fatal("setup: first click should select lane 0")
	}
	tl.dispatchClick(tm, flagLayout{}, vl, 500, y)
	if got := tl.Selection().Kind; got != SelectionNone {
		t.Errorf("second click on the same lane: got %v want SelectionNone", got)
	}
}

func TestDispatchClick_BarWinsOverItsLane(t *testing.T) {
	tl, intervals := newLaneTestTimeline(t)
	vl := tl.computeVerticalLayout(0, 0, 0, 1000)
	tm := dispatchTestTickMap(vl)
	// x=50 is inside lane 0's bar ([0,100]ms → [0,100]px); the interval tier
	// runs before the lane tier so the finer target wins.
	tl.dispatchClick(tm, flagLayout{}, vl, 50, vl.laneBaseY+tl.visuals.LaneHeight/2)
	sel := tl.Selection()
	if sel.Kind != SelectionInterval || sel.Interval != intervals[0] {
		t.Errorf("got Kind=%v Interval=%p want SelectionInterval %p", sel.Kind, sel.Interval, intervals[0])
	}
}

func TestDispatchClick_OutsideTheLaneAreaStillClears(t *testing.T) {
	tl, _ := newLaneTestTimeline(t)
	vl := tl.computeVerticalLayout(0, 0, 0, 1000)
	tm := dispatchTestTickMap(vl)
	tl.dispatchClick(tm, flagLayout{}, vl, 500, vl.laneBaseY+tl.visuals.LaneHeight/2)
	if tl.Selection().Kind != SelectionLane {
		t.Fatal("setup: expected a lane selection")
	}
	tl.dispatchClick(tm, flagLayout{}, vl, 500, vl.axisBaselineY+2) // on the axis
	if got := tl.Selection().Kind; got != SelectionNone {
		t.Errorf("click below the lane area: got %v want SelectionNone", got)
	}
}

func TestSelectionLane_HintedLaneSurvivesARepack(t *testing.T) {
	tl, _ := newLaneTestTimeline(t)
	tl.SelectLaneByHint("c") // last lane, index 2
	if idx, ok := tl.resolveLane(tl.selLane); !ok || idx != 2 {
		t.Fatalf("setup: resolveLane got (%d,%v) want (2,true)", idx, ok)
	}
	// New data reorders the hints and drops one: "c" moves to index 0.
	next := []*layout.IntervalEvent{
		{FromMS: 500, ToMS: 600, LaneHint: "c"},
		{FromMS: 500, ToMS: 600, LaneHint: "a"},
	}
	tl.SetIntervals(next)
	tl.laneAssn = layout.PackLanes(next)

	sel := tl.Selection()
	if sel.Kind != SelectionLane {
		t.Fatalf("Kind after repack: got %v want SelectionLane", sel.Kind)
	}
	if sel.Lane == nil || sel.Lane.Hint != "c" {
		t.Fatalf("Lane after repack: got %+v want hint %q", sel.Lane, "c")
	}
	if idx, _ := tl.resolveLane(tl.selLane); idx != 0 {
		t.Errorf("resolved index after repack: got %d want 0", idx)
	}
}

func TestSelectionLane_HintedLaneDegradesWhenItDisappears(t *testing.T) {
	tl, _ := newLaneTestTimeline(t)
	tl.SelectLaneByHint("b")
	next := []*layout.IntervalEvent{{FromMS: 0, ToMS: 10, LaneHint: "a"}}
	tl.SetIntervals(next)
	tl.laneAssn = layout.PackLanes(next)
	sel := tl.Selection()
	if sel.Kind != SelectionNone {
		t.Errorf("vanished lane: got Kind=%v want SelectionNone", sel.Kind)
	}
	if sel.Lane != nil {
		t.Errorf("vanished lane: Lane should be nil, got %+v", sel.Lane)
	}
}

func TestSelectionLane_AutoLaneResolvesByIndexOnly(t *testing.T) {
	// Two fully-overlapping unhinted events → two auto lanes, no names.
	intervals := []*layout.IntervalEvent{
		{FromMS: 0, ToMS: 100},
		{FromMS: 0, ToMS: 100},
	}
	tl := newTestTimeline(t, intervals)
	tl.laneAssn = layout.PackLanes(intervals)
	vl := tl.computeVerticalLayout(0, 0, 0, 1000)
	tm := dispatchTestTickMap(vl)
	pitch := tl.visuals.LaneHeight + tl.visuals.LaneGap
	tl.dispatchClick(tm, flagLayout{}, vl, 500, vl.laneBaseY+pitch+tl.visuals.LaneHeight/2)
	if sel := tl.Selection(); sel.Kind != SelectionLane || sel.Lane == nil || sel.Lane.Hint != "" {
		t.Fatalf("auto-lane selection: got %+v", sel)
	}
	if tl.selLane.hint != "" || tl.selLane.index != 1 {
		t.Fatalf("auto-lane ref: got %+v want {hint:\"\" index:1}", tl.selLane)
	}
	// A hint claiming that index means the auto lane the ref meant is gone.
	// PackLanes emits every hinted lane before the auto lanes, so two hints
	// are what it takes to reach index 1.
	next := []*layout.IntervalEvent{
		{FromMS: 0, ToMS: 100, LaneHint: "first"},
		{FromMS: 0, ToMS: 100, LaneHint: "second"},
		{FromMS: 0, ToMS: 100},
	}
	tl.laneAssn = layout.PackLanes(next)
	if tl.laneAssn.Lanes[1].Hint == "" {
		t.Fatal("setup: expected index 1 to be claimed by a hinted lane")
	}
	if got := tl.Selection().Kind; got != SelectionNone {
		t.Errorf("auto lane displaced by a hint: got %v want SelectionNone", got)
	}
}

func TestSelectLaneByHint_MissAndEmptyAreNoOps(t *testing.T) {
	tl, _ := newLaneTestTimeline(t)
	tl.SelectLaneByHint("nope")
	if got := tl.Selection().Kind; got != SelectionNone {
		t.Errorf("unknown hint: got %v want SelectionNone", got)
	}
	tl.SelectLaneByHint("b")
	tl.SelectLaneByHint("")
	if sel := tl.Selection(); sel.Kind != SelectionLane || sel.Lane.Hint != "b" {
		t.Errorf("empty hint should not disturb an existing selection: got %+v", sel)
	}
	tl.SelectLaneByHint("nope")
	if sel := tl.Selection(); sel.Kind != SelectionLane || sel.Lane.Hint != "b" {
		t.Errorf("missed hint should not disturb an existing selection: got %+v", sel)
	}
}

func TestSelectLaneByHint_BeforeFirstRenderIsANoOp(t *testing.T) {
	// laneAssn is populated by Render; nothing to match before then.
	tl := newTestTimeline(t, []*layout.IntervalEvent{{FromMS: 0, ToMS: 10, LaneHint: "a"}})
	tl.SelectLaneByHint("a")
	if got := tl.Selection().Kind; got != SelectionNone {
		t.Errorf("select before first render: got %v want SelectionNone", got)
	}
}

func TestSelectionInfo_LaneIsASnapshotNotAnAlias(t *testing.T) {
	tl, _ := newLaneTestTimeline(t)
	tl.SelectLaneByHint("a")
	before := tl.Selection().Lane
	if before == nil {
		t.Fatal("setup: expected a lane")
	}
	// Repacking must not mutate a SelectionInfo the caller already holds.
	next := []*layout.IntervalEvent{{FromMS: 0, ToMS: 10, LaneHint: "a"}, {FromMS: 0, ToMS: 10, LaneHint: "a"}}
	tl.laneAssn = layout.PackLanes(next)
	if len(before.Items) != 1 {
		t.Errorf("held snapshot mutated by a repack: got %d items want 1", len(before.Items))
	}
	if after := tl.Selection().Lane; len(after.Items) != 2 {
		t.Errorf("fresh read should see the repack: got %d items want 2", len(after.Items))
	}
}
