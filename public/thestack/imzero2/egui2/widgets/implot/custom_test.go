package implot

import (
	"math"
	"testing"
)

func TestCustomDeclarationOrderAndFlags(t *testing.T) {
	p := newTestPlot()
	fn := func(DrawCtx) {}
	p.Custom("bands", fn)
	p.Line("load", []float64{0, 1}, []float64{0, 1})
	p.SetNextColor(0x11223344)
	p.CustomUnclipped("callout", fn)

	if len(p.series) != 3 {
		t.Fatalf("series count %d, want 3", len(p.series))
	}
	wantKinds := []seriesKind{kindCustom, kindLine, kindCustom}
	for i, k := range wantKinds {
		if p.series[i].kind != k {
			t.Errorf("series[%d].kind = %v, want %v", i, p.series[i].kind, k)
		}
	}
	if p.series[0].unclipped || !p.series[2].unclipped {
		t.Errorf("unclipped flags = %v/%v, want false/true",
			p.series[0].unclipped, p.series[2].unclipped)
	}
	if p.series[0].custom == nil || p.series[2].custom == nil {
		t.Error("custom closure not recorded")
	}
	// SetNextColor flows into the custom item like any other item.
	if !p.series[2].colOk || p.series[2].colHex != 0x11223344 {
		t.Errorf("SetNextColor not consumed by CustomUnclipped: ok=%v hex=%#x",
			p.series[2].colOk, p.series[2].colHex)
	}
	// Declaring an item locks setup, custom included.
	if !p.setupLocked {
		t.Error("Custom must lock setup like any item declaration")
	}
}

func TestCustomNilFnIsNoOp(t *testing.T) {
	p := newTestPlot()
	p.Custom("a", nil)
	p.CustomUnclipped("b", nil)
	if len(p.series) != 0 {
		t.Errorf("nil closures recorded %d series, want 0", len(p.series))
	}
	if p.setupLocked {
		t.Error("nil-closure no-op must not lock setup")
	}
}

func TestCustomFitParticipationIsExplicit(t *testing.T) {
	p := newTestPlot()
	p.Custom("lanes", func(DrawCtx) {})
	if p.dataOk {
		t.Error("a Custom item alone must not contribute fit extents")
	}
	p.IncludeX(3).IncludeX(9).IncludeY(-2)
	if !p.dataOk || p.dataXMin != 3 || p.dataXMax != 9 || p.dataYMin != -2 {
		t.Errorf("IncludeX/Y extents = x[%v,%v] ymin %v, want x[3,9] ymin -2",
			p.dataXMin, p.dataXMax, p.dataYMin)
	}
}

func TestCustomLegendParticipation(t *testing.T) {
	p := newTestPlot()
	fn := func(DrawCtx) {}
	p.Custom("lanes", fn)
	p.Custom("", fn)      // anonymous: no legend row
	p.Custom("lanes", fn) // same label: merges, no second row
	p.Line("load", []float64{0}, []float64{0})

	leg := legendIndices(p.series)
	if len(leg) != 2 {
		t.Fatalf("legend rows %d, want 2 (lanes, load)", len(leg))
	}
	if p.series[leg[0]].label != "lanes" || p.series[leg[1]].label != "load" {
		t.Errorf("legend labels %q/%q, want lanes/load",
			p.series[leg[0]].label, p.series[leg[1]].label)
	}
	// Same-label customs share one palette slot (the label→item registry).
	if p.series[0].slot != p.series[2].slot {
		t.Errorf("same-label slots %d/%d, want shared", p.series[0].slot, p.series[2].slot)
	}
}

func TestEmittingGuardBlocksDeclarations(t *testing.T) {
	p := newTestPlot()
	p.Line("base", []float64{0, 1}, []float64{0, 1})
	n := len(p.series)
	p.emitting = true
	p.Custom("late", func(DrawCtx) {})
	p.Line("late", []float64{2}, []float64{2})
	p.Heatmap("late", []float64{1}, 1, 1, nil, 0, 0, 1, 1)
	p.emitting = false
	if len(p.series) != n {
		t.Errorf("declarations during emission recorded %d series, want %d",
			len(p.series), n)
	}
}

func TestTransformExportRoundTrip(t *testing.T) {
	// Linear: known corners, then round-trip.
	tr := Transform{newTransform(Range{0, 10}, Range{-1, 1},
		ScaleLinear, ScaleLinear, 50, 20, 500, 300)}
	if got := tr.PxX(0); got != 50 {
		t.Errorf("PxX(0) = %v, want 50 (area left)", got)
	}
	if got := tr.PxX(10); got != 550 {
		t.Errorf("PxX(10) = %v, want 550 (area right)", got)
	}
	if got := tr.PxY(-1); got != 320 {
		t.Errorf("PxY(-1) = %v, want 320 (area bottom; plot-up is pixel-down)", got)
	}
	if got := tr.PxY(1); got != 20 {
		t.Errorf("PxY(1) = %v, want 20 (area top)", got)
	}
	for _, v := range []float64{0, 2.5, 7.75, 10} {
		if got := tr.PlotX(tr.PxX(v)); math.Abs(got-v) > 1e-4 {
			t.Errorf("PlotX(PxX(%v)) = %v", v, got)
		}
	}
	for _, v := range []float64{-1, -0.25, 0.5, 1} {
		if got := tr.PlotY(tr.PxY(v)); math.Abs(got-v) > 1e-4 {
			t.Errorf("PlotY(PxY(%v)) = %v", v, got)
		}
	}
	// Log scale: the inverse goes through the scale's inverse too.
	trLog := Transform{newTransform(Range{1, 1000}, Range{0, 1},
		ScaleLog10, ScaleLinear, 0, 0, 300, 100)}
	for _, v := range []float64{1, 10, 100, 1000} {
		if got := trLog.PlotX(trLog.PxX(v)); math.Abs(got-v)/v > 1e-4 {
			t.Errorf("log PlotX(PxX(%v)) = %v", v, got)
		}
	}
	if got := trLog.PxX(10); math.Abs(float64(got)-100) > 1e-3 {
		t.Errorf("log PxX(10) = %v, want 100 (one decade of three across 300px)", got)
	}
}

func TestPixelReadbacksNilSafe(t *testing.T) {
	// Detached plots have no canvas: every readback must report !ok, and a
	// nil receiver must not panic (the HoverPlotPos discipline).
	p := newTestPlot()
	if _, _, ok := p.HoverPixelPos(); ok {
		t.Error("HoverPixelPos ok on a detached plot")
	}
	if _, _, ok := p.ClickedPixelPos(); ok {
		t.Error("ClickedPixelPos ok on a detached plot")
	}
	if _, _, _, _, ok := p.PlotAreaPrev(); ok {
		t.Error("PlotAreaPrev ok before first render")
	}
	var nilP *Plot
	if _, _, ok := nilP.HoverPixelPos(); ok {
		t.Error("nil receiver HoverPixelPos ok")
	}
	if _, _, ok := nilP.ClickedPixelPos(); ok {
		t.Error("nil receiver ClickedPixelPos ok")
	}
	if _, _, _, _, ok := nilP.PlotAreaPrev(); ok {
		t.Error("nil receiver PlotAreaPrev ok")
	}
}

func TestAxisRangePrevUnpinnedAxis(t *testing.T) {
	// The range a readback caller needs is the settled one, whoever settled
	// it. An axis left to the autofit — no SetupAxisLimits, so hasRange stays
	// false — has a range as real as a pinned one once the plot has rendered,
	// and a caller placing its own ticks against it must get it. Gating on
	// hasRange left such an axis on the default locator forever.
	p := newTestPlot()
	p.st.prevOk = true
	p.st.y.rng = Range{0, 7.3}
	lo, hi, ok := p.AxisRangePrev(AxisY1)
	if !ok {
		t.Fatal("AxisRangePrev !ok for an autofit axis")
	}
	if lo != 0 || hi != 7.3 {
		t.Errorf("AxisRangePrev = %v..%v, want 0..7.3", lo, hi)
	}
	if p.st.y.hasRange {
		t.Error("the autofit path must not set hasRange; the test would prove nothing")
	}
	// A pinned axis is unaffected.
	p.st.x.rng = Range{10, 20}
	p.st.x.hasRange = true
	if lo, hi, ok := p.AxisRangePrev(AxisX1); !ok || lo != 10 || hi != 20 {
		t.Errorf("AxisRangePrev(X1) = %v..%v ok=%v, want 10..20 ok", lo, hi, ok)
	}
	// The two remaining gates still hold.
	p.st.y.rng = Range{5, 5}
	if _, _, ok := p.AxisRangePrev(AxisY1); ok {
		t.Error("AxisRangePrev ok for a degenerate range")
	}
	p.st.prevOk = false
	p.st.y.rng = Range{0, 7.3}
	if _, _, ok := p.AxisRangePrev(AxisY1); ok {
		t.Error("AxisRangePrev ok before the first render")
	}
	var nilP *Plot
	if _, _, ok := nilP.AxisRangePrev(AxisY1); ok {
		t.Error("nil receiver AxisRangePrev ok")
	}
}
