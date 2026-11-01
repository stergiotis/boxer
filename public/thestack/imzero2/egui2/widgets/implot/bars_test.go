package implot

import (
	"math"
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

func TestBarsFitIncludesBodyAndZero(t *testing.T) {
	p := newTestPlot()
	p.Bars("b", []float64{1, 2}, []float64{3, 5}, 0.5)
	if p.dataXMin != 0.75 || p.dataXMax != 2.25 {
		t.Errorf("Bars fit x = [%v, %v], want [0.75, 2.25]", p.dataXMin, p.dataXMax)
	}
	if p.dataYMin != 0 || p.dataYMax != 5 {
		t.Errorf("Bars fit y = [%v, %v], want [0, 5]", p.dataYMin, p.dataYMax)
	}
}

func TestBarsRefFitIncludesBase(t *testing.T) {
	p := newTestPlot()
	p.BarsRef("b", []float64{1, 2}, []float64{3, 5}, 0.5, 10)
	if p.dataYMin != 3 || p.dataYMax != 10 {
		t.Errorf("BarsRef fit y = [%v, %v], want [3, 10] (values and the base)", p.dataYMin, p.dataYMax)
	}
	if p.series[0].kind != kindBars || p.series[0].yref != 10 {
		t.Errorf("BarsRef recorded kind %v yref %v, want kindBars 10", p.series[0].kind, p.series[0].yref)
	}
}

func TestBarsHFitSwapsAxes(t *testing.T) {
	p := newTestPlot()
	p.BarsH("h", []float64{3, -5}, []float64{1, 2}, 0.5)
	if p.dataYMin != 0.75 || p.dataYMax != 2.25 {
		t.Errorf("BarsH fit y = [%v, %v], want [0.75, 2.25] (positions ± height/2)", p.dataYMin, p.dataYMax)
	}
	if p.dataXMin != -5 || p.dataXMax != 3 {
		t.Errorf("BarsH fit x = [%v, %v], want [-5, 3] (values and x=0)", p.dataXMin, p.dataXMax)
	}

	pr := newTestPlot()
	pr.BarsHRef("h", []float64{3, 5}, []float64{1, 2}, 0.5, 8)
	if pr.dataXMin != 3 || pr.dataXMax != 8 {
		t.Errorf("BarsHRef fit x = [%v, %v], want [3, 8] (values and the base)", pr.dataXMin, pr.dataXMax)
	}
	// A NaN position contributes nothing to the position axis.
	pn := newTestPlot()
	pn.BarsH("h", []float64{1, 1}, []float64{math.NaN(), 4}, 1)
	if pn.dataYMin != 3.5 || pn.dataYMax != 4.5 {
		t.Errorf("BarsH NaN fit y = [%v, %v], want [3.5, 4.5]", pn.dataYMin, pn.dataYMax)
	}
}

// barsTransform maps [0, 10] on both axes onto a 100 × 100 px area at the
// origin: px x = 10·v, px y = 100 − 10·v.
func barsTransform() transform {
	return newTransform(Range{0, 10}, Range{0, 10}, ScaleLinear, ScaleLinear, 0, 0, 100, 100)
}

type rect struct{ x0, y0, x1, y1 float32 }

func rectsOf(minXs, minYs, maxXs, maxYs []float32) []rect {
	out := make([]rect, len(minXs))
	for i := range minXs {
		out[i] = rect{minXs[i], minYs[i], maxXs[i], maxYs[i]}
	}
	return out
}

func TestBarRectsVertical(t *testing.T) {
	s := seriesFrame{kind: kindBars, xs: []float64{2, 5, math.NaN()}, ys: []float64{4, 1, 3}, width: 1, yref: 2}
	minXs, minYs, maxXs, maxYs, cols := barRects(&s, barsTransform(), 0xaabbccff)
	got := rectsOf(minXs, minYs, maxXs, maxYs)
	// Bar 0 rises from the base (y=2 → 80 px) to 4 (60 px); bar 1 lies
	// below the base and runs down to 1 (90 px); the NaN bar is dropped.
	want := []rect{{15, 60, 25, 80}, {45, 80, 55, 90}}
	if len(got) != len(want) {
		t.Fatalf("rects = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rect[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if len(cols) != 2 || cols[0] != 0xaabbccff || cols[1] != 0xaabbccff {
		t.Errorf("cols = %#x, want the series colour per bar", cols)
	}
}

func TestBarRectsHorizontalDiverging(t *testing.T) {
	// A tornado row: one bar each side of the base x=5.
	s := seriesFrame{kind: kindBarsH, xs: []float64{8, 3}, ys: []float64{6, 2}, width: 1, yref: 5}
	minXs, minYs, maxXs, maxYs, _ := barRects(&s, barsTransform(), 0)
	got := rectsOf(minXs, minYs, maxXs, maxYs)
	want := []rect{{50, 35, 80, 45}, {30, 75, 50, 85}}
	if len(got) != len(want) {
		t.Fatalf("rects = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rect[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// renderBarsFrame renders one frame of a fixed-range, legend-less plot
// under a counting discard channel and returns the messages it sent.
func renderBarsFrame(t *testing.T, decl func(p *Plot)) int {
	t.Helper()
	messages, zero, reset := scenetest.InstallCounting()
	t.Cleanup(reset)
	ids := c.NewWidgetIdStack()
	zero()
	p := Begin(ids, "##bars-test", 400, 300)
	p.SetupAxisLimits(AxisX1, -10, 10, CondAlways).
		SetupAxisLimits(AxisY1, -10, 10, CondAlways).
		NoLegend()
	decl(p)
	p.End()
	Release(p.ScopeId())
	return messages()
}

// Every bars series is one paintRectsFilled opcode, whatever its
// orientation, base or bar count.
func TestBarsRenderOneOpcodePerSeries(t *testing.T) {
	ys := []float64{1, 2, 3}
	none := renderBarsFrame(t, func(*Plot) {})
	vertical := renderBarsFrame(t, func(p *Plot) { p.Bars("v", ys, []float64{4, -2, 6}, 0.8) })
	tornado := renderBarsFrame(t, func(p *Plot) {
		p.BarsHRef("down", []float64{-3, -1, -6}, ys, 0.8, 0.5)
		p.BarsHRef("up", []float64{4, 2, 7}, ys, 0.8, 0.5)
	})
	if vertical != none+1 {
		t.Errorf("one Bars series sent %d messages over an empty plot's %d, want one more", vertical, none)
	}
	if tornado != none+2 {
		t.Errorf("two BarsHRef series sent %d messages over an empty plot's %d, want two more", tornado, none)
	}
}
