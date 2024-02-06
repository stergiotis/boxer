package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/implot"
	"math"
)

type linesstate struct {
	xs1 []float32
	ys1 []float32
	xs2 []float32
	ys2 []float32
}

var lines = &linesstate{
	xs1: nil,
	ys1: nil,
	xs2: nil,
	ys2: nil,
}

func DemoLinePlot() {
	if lines.xs1 == nil {
		xs1 := make([]float32, 0, 1001)
		ys1 := make([]float32, 1001, 1001)
		for i := 0; i < 1001; i++ {
			x := float32(i) * 0.001
			xs1 = append(xs1, x)
		}
		xs2 := make([]float32, 0, 20)
		ys2 := make([]float32, 20, 20)
		for i := 0; i < 20; i++ {
			x := float32(i) * 1.0 / 19.0
			xs2 = append(xs2, x)
		}
		lines.xs1 = xs1
		lines.ys1 = ys1
		lines.xs2 = xs2
		lines.ys2 = ys2
	}
	xs1 := lines.xs1
	ys1 := lines.ys1
	xs2 := lines.xs2
	ys2 := lines.ys2

	t := imgui.GetTime()
	for i := 0; i < 1001; i++ {
		x := xs1[i]
		ys1[i] = float32(0.5 + 0.5*math.Sin(50.0*(float64(x)+t/10.0)))
	}
	for i := 0; i < 20; i++ {
		x := xs2[i]
		ys2[i] = x * x
	}
	if implot.BeginPlot("Line Plots") {
		implot.SetupAxes("x", "y", 0, 0)
		implot.PlotLineXYFloat32("f(x)", xs1, ys1)
		implot.SetNextMarkerStyleV(implot.ImPlotMarker_Circle, implot.ImPlotAutoFloat32, implot.ImPlotAutoCol, implot.ImPlotAutoFloat32, implot.ImPlotAutoCol)
		implot.PlotLineXYFloat32V("g(x)", xs2, ys2, implot.ImPlotLineFlags_Segments, 0, 4)
		implot.EndPlot()
	}
}
