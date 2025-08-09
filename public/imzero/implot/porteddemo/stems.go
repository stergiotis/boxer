package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/implot"
	"math"
)

func MakeStemDemo() (r Demofunc) {
	xs := make([]float32, 51, 51)
	ys1 := make([]float32, 51, 51)
	ys2 := make([]float32, 51, 51)
	for i := 0; i < 51; i++ {
		xs[i] = float32(i) * 0.02
		ys1[i] = float32(1.0 + 0.5*math.Sin(25.0*float64(xs[i]))*math.Cos(2.0*float64(xs[i])))
		ys2[i] = float32(0.5 + 0.25*math.Sin(15.0*float64(xs[i]))*math.Sin(float64(xs[i])))
	}
	r = func() {
		if implot.BeginPlot("Stemp Plots") {
			implot.SetupAxisLimits(implot.ImAxis_X1, 0, 1.0, implot.ImPlotCond_Once)
			implot.SetupAxisLimits(implot.ImAxis_Y1, 0, 1.6, implot.ImPlotCond_Once)
			implot.SetupFinish()
			implot.PlotStemsXYFloat32("Stems 1", xs, ys1)
			implot.SetNextMarkerStyleV(implot.ImPlotMarker_Circle, implot.ImPlotAutoFloat32, implot.ImPlotAutoCol, implot.ImPlotAutoFloat32, implot.ImPlotAutoCol)
			implot.PlotStemsXYFloat32("Stems 2", xs, ys2)

			implot.EndPlot()
		}
	}
	return
}
