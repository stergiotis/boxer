//go:build llm_generated_opus46

package widgets

import (
	"math"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// =============================================================================
// DEMO: Line chart with sine/cosine
// =============================================================================

func demoPlotLines(ids *c.WidgetIdStack) {
	const n = 200
	xs := make([]float64, n)
	sinYs := make([]float64, n)
	cosYs := make([]float64, n)
	for i := 0; i < n; i++ {
		x := float64(i) * 0.1
		xs[i] = x
		sinYs[i] = math.Sin(x)
		cosYs[i] = math.Cos(x)
	}

	c.PlotLine("sin(x)", xs, sinYs).Width(2.0).Color(color.Hex(0x4488ffff)).Send()
	c.PlotLine("cos(x)", xs, cosYs).Width(2.0).Color(color.Hex(0xff8844ff)).Send()
	c.PlotHLine("zero", 0.0).Color(color.Hex(0x888888ff)).Width(1.0).Send()

	c.Plot(ids.PrepareStr("plot-lines")).
		Width(500).Height(300).
		XAxisLabel("x").YAxisLabel("y").
		Legend().
		AllowZoom(true).AllowDrag(true).
		Send()
}

// =============================================================================
// DEMO: Scatter plot
// =============================================================================

func demoPlotScatter(ids *c.WidgetIdStack) {
	const n = 100
	xs := make([]float64, n)
	ys := make([]float64, n)
	for i := 0; i < n; i++ {
		t := float64(i) * 0.15
		xs[i] = math.Sin(t) * (1.0 + 0.3*math.Sin(t*5))
		ys[i] = math.Cos(t) * (1.0 + 0.3*math.Cos(t*3))
	}

	c.PlotScatter("spiral", xs, ys).
		Color(color.Hex(0x44cc88ff)).Radius(3.0).Shape(0).Filled(true).Send()

	c.Plot(ids.PrepareStr("plot-scatter")).
		Width(400).Height(400).
		DataAspect(1.0).
		Legend().
		Send()
}

// =============================================================================
// DEMO: Bar chart
// =============================================================================

func demoPlotBars(ids *c.WidgetIdStack) {
	arguments := []float64{0, 1, 2, 3, 4, 5, 6}
	values := []float64{12.0, 25.0, 18.0, 32.0, 15.0, 28.0, 22.0}
	labels := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

	c.PlotBars("daily sales", arguments, values).
		Color(color.Hex(0x6688ccff)).Width(0.7).Send()

	// Add text labels
	for i, lbl := range labels {
		c.PlotText(lbl, float64(i), -2.0, lbl).Send()
	}

	c.PlotVLine("mid-week", 2.5).Color(color.Hex(0xff444488)).Width(1.0).Send()

	c.Plot(ids.PrepareStr("plot-bars")).
		Width(500).Height(300).
		YAxisLabel("Sales ($k)").
		Legend().
		IncludeY(0).
		Send()
}

// =============================================================================
// DEMO: Combined (multiple element types)
// =============================================================================

func demoPlotCombined(ids *c.WidgetIdStack) {
	const n = 50
	xs := make([]float64, n)
	ys := make([]float64, n)
	scatterXs := make([]float64, n)
	scatterYs := make([]float64, n)
	for i := 0; i < n; i++ {
		x := float64(i) * 0.2
		xs[i] = x
		ys[i] = math.Log(x + 1)
		// scatter points with noise around the line
		scatterXs[i] = x
		scatterYs[i] = math.Log(x+1) + 0.3*math.Sin(float64(i)*1.7)
	}

	c.PlotLine("log(x+1)", xs, ys).Color(color.Hex(0xff4444ff)).Width(2.0).Send()
	c.PlotScatter("measurements", scatterXs, scatterYs).
		Color(color.Hex(0x4444ffff)).Radius(2.5).Shape(0).Filled(true).Send()
	c.PlotHLine("threshold", 2.5).Color(color.Hex(0x44ff44ff)).Width(1.5).Highlight(true).Send()
	c.PlotText("target", 5.0, 2.7, "target threshold").Color(color.Hex(0x44ff44ff)).Send()

	c.Plot(ids.PrepareStr("plot-combined")).
		Width(600).Height(350).
		XAxisLabel("Time (s)").YAxisLabel("Amplitude").
		Legend().
		AllowZoom(true).AllowDrag(true).AllowBoxedZoom(true).
		Send()
}
