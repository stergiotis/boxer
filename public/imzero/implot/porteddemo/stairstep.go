package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/implot"
	"math"
)

func MakeStairstepDemo() (r Demofunc) {
	ys1 := make([]float32, 21, 21)
	ys2 := make([]float32, 21, 21)
	for i := 0; i < 21; i++ {
		ys1[i] = float32(0.75 + 0.2*math.Sin(10.0*float64(i)*0.05))
		ys2[i] = float32(0.25 + 0.2*math.Sin(10.0*float64(i)*0.05))
	}
	flags := implot.ImPlotStairsFlags_None
	r = func() {
		flags, _ = imgui.ToggleFlags("ImPlotStairsFlags_Shaded", flags, implot.ImPlotStairsFlags_Shaded)
		if implot.BeginPlot("Stairstep Plot") {
			implot.SetupAxes("x", "f(x)", 0, 0)
			implot.SetupAxesLimits(0.0, 1.0, 0.0, 1.0, implot.ImPlotCond_Once)
			implot.SetupFinish()

			implot.PushStyleColorImVec4(implot.ImPlotCol_Line, imgui.ImVec4{0.5, 0.5, 0.5, 1.0})
			implot.PlotLineFloat32V("##1", ys1, 0.05, 0.0, 0, 0, 4)
			implot.PlotLineFloat32V("##2", ys2, 0.05, 0.0, 0, 0, 4)
			implot.PopStyleColor(1)

			implot.SetNextMarkerStyleV(implot.ImPlotMarker_Circle, implot.ImPlotAutoFloat32, implot.ImPlotAutoCol, implot.ImPlotAutoFloat32, implot.ImPlotAutoCol)
			implot.SetNextFillStyleV(implot.ImPlotAutoCol, 0.25)
			implot.PlotStairsFloat32V("Post Step (default)", ys1, 0.05, 0.0, flags, 0, 4)
			implot.SetNextMarkerStyleV(implot.ImPlotMarker_Circle, implot.ImPlotAutoFloat32, implot.ImPlotAutoCol, implot.ImPlotAutoFloat32, implot.ImPlotAutoCol)
			implot.SetNextFillStyleV(implot.ImPlotAutoCol, 0.25)
			implot.PlotStairsFloat32V("Pre Step", ys2, 0.05, 0.0, flags|implot.ImPlotStairsFlags_PreStep, 0, 4)

			implot.EndPlot()
		}
	}
	return
}
