package implot

import (
	"math"
	"testing"
)

// newTestPlot builds a bare Plot for the declaration/fit layer, which is
// bindings-free (Begin needs a live StateManager; declarations do not).
func newTestPlot() *Plot { return NewDetached() }

func TestPieSpans(t *testing.T) {
	// Sum > 1 normalizes to a full circle, honoring angle0 and order.
	spans := pieSpans([]float64{1, 1, 2}, 90)
	if spans == nil {
		t.Fatal("nil spans for positive values")
	}
	total := 0.0
	for _, sp := range spans {
		total += sp[1] - sp[0]
	}
	if math.Abs(total-2*math.Pi) > 1e-9 {
		t.Errorf("normalized total span %v, want 2π", total)
	}
	if math.Abs(spans[0][0]-math.Pi/2) > 1e-9 {
		t.Errorf("angle0=90° must start at π/2, got %v", spans[0][0])
	}
	if spans[1][0] != spans[0][1] || spans[2][0] != spans[1][1] {
		t.Error("slices not contiguous")
	}
	if math.Abs((spans[2][1]-spans[2][0])-math.Pi) > 1e-9 {
		t.Errorf("value 2 of 4 must span a half circle, got %v", spans[2][1]-spans[2][0])
	}
	// Sum < 1 renders a partial pie: total span = sum · 2π.
	spans = pieSpans([]float64{0.25, 0.25}, 0)
	total = spans[1][1] - spans[0][0]
	if math.Abs(total-math.Pi) > 1e-9 {
		t.Errorf("partial pie span %v, want π", total)
	}
	// NaN / negative values become zero-span slices; the rest keep theirs.
	spans = pieSpans([]float64{math.NaN(), -1, 1}, 0)
	if spans[0][0] != spans[0][1] || spans[1][0] != spans[1][1] {
		t.Error("NaN/negative slices must have zero span")
	}
	if math.Abs((spans[2][1]-spans[2][0])-2*math.Pi) > 1e-9 {
		t.Error("sole positive value must cover the full circle")
	}
	// Nothing positive → nil.
	if s := pieSpans([]float64{-3, 0}, 0); s != nil {
		t.Error("expected nil spans for non-positive values")
	}
}

func TestArcChunksConvex(t *testing.T) {
	for _, span := range []float64{0.5, math.Pi, 4.0, 2 * math.Pi} {
		var buf [3][2]float64
		chunks := arcChunks(1, 1+span, buf[:0])
		got := 0.0
		prev := 1.0
		for _, ch := range chunks {
			if ch[1]-ch[0] > math.Pi+1e-9 {
				t.Errorf("span %v: chunk wider than a half circle: %v", span, ch)
			}
			if ch[0] != prev {
				t.Errorf("span %v: chunks not contiguous at %v", span, ch[0])
			}
			prev = ch[1]
			got += ch[1] - ch[0]
		}
		if math.Abs(got-span) > 1e-9 {
			t.Errorf("chunks cover %v, want %v", got, span)
		}
	}
}

func TestDigitalRuns(t *testing.T) {
	xs := []float64{0, 1, 2, 3, 4, 5, 6, 7}
	ys := []float64{1, 1, 0, 0, math.NaN(), 1, 1, 1}
	type run struct{ x0, x1, v float64 }
	var runs []run
	digitalRuns(xs, ys, func(x0, x1, v float64) { runs = append(runs, run{x0, x1, v}) })
	// Equal-value stretches merge; a run ends at its transition sample;
	// the NaN ends the 0-run at its last valid sample and splits the tail.
	want := []run{{0, 2, 1}, {2, 3, 0}, {5, 7, 1}}
	if len(runs) != len(want) {
		t.Fatalf("runs = %+v, want %+v", runs, want)
	}
	for i := range want {
		if runs[i] != want[i] {
			t.Errorf("run[%d] = %+v, want %+v", i, runs[i], want[i])
		}
	}
	// A single sample yields no run (pairs are required, as upstream).
	runs = runs[:0]
	digitalRuns([]float64{1}, []float64{1}, func(x0, x1, v float64) { runs = append(runs, run{x0, x1, v}) })
	if len(runs) != 0 {
		t.Errorf("single sample produced runs: %+v", runs)
	}
}

func TestErrorBarsFit(t *testing.T) {
	p := newTestPlot()
	p.ErrorBars("e", []float64{1, 2}, []float64{5, 6}, []float64{1, 0.5}, []float64{2, 3})
	if p.dataYMin != 4 || p.dataYMax != 9 {
		t.Errorf("vertical fit y = [%v, %v], want [4, 9]", p.dataYMin, p.dataYMax)
	}
	if p.dataXMin != 1 || p.dataXMax != 2 {
		t.Errorf("vertical fit x = [%v, %v], want [1, 2]", p.dataXMin, p.dataXMax)
	}
	ph := newTestPlot()
	ph.ErrorBarsH("e", []float64{5, 6}, []float64{1, 2}, []float64{1, 0.5}, []float64{2, 3})
	if ph.dataXMin != 4 || ph.dataXMax != 9 {
		t.Errorf("horizontal fit x = [%v, %v], want [4, 9]", ph.dataXMin, ph.dataXMax)
	}
	// A hidden series must not contribute to fit.
	hp := newTestPlot()
	hp.st.hidden["e"] = true
	hp.ErrorBars("e", []float64{1}, []float64{5}, []float64{1}, []float64{2})
	if hp.dataOk {
		t.Error("hidden error bars contributed to fit")
	}
}

func TestInfLinesFit(t *testing.T) {
	// Regression: the fit loop keyed its bound to len(xs), so InfLinesH
	// (nil xs) never contributed y.
	p := newTestPlot()
	p.InfLinesH("h", []float64{3, 7})
	if p.dataYMin != 3 || p.dataYMax != 7 {
		t.Errorf("InfLinesH fit y = [%v, %v], want [3, 7]", p.dataYMin, p.dataYMax)
	}
	if !math.IsInf(p.dataXMin, 1) {
		t.Error("InfLinesH must not fit x")
	}
	pv := newTestPlot()
	pv.InfLinesV("v", []float64{-2, 4})
	if pv.dataXMin != -2 || pv.dataXMax != 4 {
		t.Errorf("InfLinesV fit x = [%v, %v], want [-2, 4]", pv.dataXMin, pv.dataXMax)
	}
}

func TestDigitalFitXOnly(t *testing.T) {
	p := newTestPlot()
	p.Digital("d", []float64{0, 5}, []float64{1, 0})
	if p.dataXMin != 0 || p.dataXMax != 5 {
		t.Errorf("digital fit x = [%v, %v], want [0, 5]", p.dataXMin, p.dataXMax)
	}
	if !math.IsInf(p.dataYMin, 1) {
		t.Error("digital must not fit y")
	}
}

func TestLegendIndicesDedup(t *testing.T) {
	series := []seriesFrame{{label: "a"}, {label: ""}, {label: "b"}, {label: "a"}}
	idxs := legendIndices(series)
	if len(idxs) != 2 || idxs[0] != 0 || idxs[1] != 2 {
		t.Errorf("legendIndices = %v, want [0 2]", idxs)
	}
}

func TestAssignSlotSharing(t *testing.T) {
	p := newTestPlot()
	a := p.assignSlot("x")
	b := p.assignSlot("") // unlabeled items consume a slot each
	c2 := p.assignSlot("x")
	d := p.assignSlot("y")
	if a != c2 {
		t.Errorf("same label must share a slot: %d vs %d", a, c2)
	}
	if a != 0 || b != 1 || d != 2 {
		t.Errorf("slot order = %d %d %d, want 0 1 2", a, b, d)
	}
}

func TestSetNextStyleConsumedOnce(t *testing.T) {
	p := newTestPlot()
	p.SetNextColor(0x11223344).SetNextWeight(2.5)
	p.Line("a", []float64{0, 1}, []float64{0, 1})
	p.Line("b", []float64{0, 1}, []float64{0, 1})
	if !p.series[0].colOk || p.series[0].colHex != 0x11223344 || p.series[0].weight != 2.5 {
		t.Errorf("override not applied to the next item: %+v", p.series[0])
	}
	if p.series[1].colOk || p.series[1].weight != 0 {
		t.Errorf("override leaked past the next item: %+v", p.series[1])
	}
	// A non-styleable declarator consumes a pending override.
	p.SetNextColor(0xffffffff)
	p.Pie([]string{"s"}, []float64{1}, 0, 0, 1, 0, "")
	p.Line("c", []float64{0, 1}, []float64{0, 1})
	last := p.series[len(p.series)-1]
	if last.colOk {
		t.Error("override leaked through a pie declaration")
	}
}

func TestShadedBetweenAndIncludeFit(t *testing.T) {
	p := newTestPlot()
	p.ShadedBetween("band", []float64{0, 1}, []float64{2, 3}, []float64{5, 7})
	if p.dataYMin != 2 || p.dataYMax != 7 {
		t.Errorf("between fit y = [%v, %v], want [2, 7]", p.dataYMin, p.dataYMax)
	}
	p.IncludeY(-1)
	p.IncludeX(10)
	if p.dataYMin != -1 || p.dataXMax != 10 {
		t.Errorf("include not applied: y min %v, x max %v", p.dataYMin, p.dataXMax)
	}
}

func TestCustomTicksFilter(t *testing.T) {
	p := newTestPlot()
	p.SetupAxisTicks(AxisY1, []float64{0, 25, 50, 75, 100}, []string{"0", "25", "50", "75", "100"})
	if len(p.yCustomTicks) != 5 || p.yCustomTicks[1].label != "25" || !p.yCustomTicks[1].major {
		t.Fatalf("custom ticks not recorded: %+v", p.yCustomTicks)
	}
	got := filterTicksInRange(Range{20, 80}, p.yCustomTicks, nil)
	if len(got) != 3 || got[0].value != 25 || got[2].value != 75 {
		t.Errorf("range filter = %+v, want the 25/50/75 ticks", got)
	}
	// Mismatched lengths clip to the shorter side.
	p.SetupAxisTicks(AxisX1, []float64{1, 2, 3}, []string{"a", "b"})
	if len(p.xCustomTicks) != 2 {
		t.Errorf("length clip failed: %+v", p.xCustomTicks)
	}
}

func TestContrastText(t *testing.T) {
	// The branch colors follow the active chrome (chrome.go); the
	// luminance split itself is chrome-independent.
	if contrastText(0xfafafaff) != colContrastDark {
		t.Error("light fill must get dark text")
	}
	if contrastText(0x101010ff) != colContrastLite {
		t.Error("dark fill must get light text")
	}
}
