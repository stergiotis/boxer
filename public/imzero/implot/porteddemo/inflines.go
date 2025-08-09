package porteddemo

import "github.com/stergiotis/boxer/public/imzero/implot"

func MakeInfLinesDemo() (r Demofunc) {
	vals := []float32{0.25, 0.5, 0.75}
	r = func() {
		if implot.BeginPlot("##Infinite") {
			implot.SetupAxes("", "", implot.ImPlotAxisFlags_NoInitialFit, implot.ImPlotAxisFlags_NoInitialFit)
			implot.SetupFinish()
			implot.PlotInfLinesFloat32("Vertical", vals)
			implot.PlotInfLinesFloat32V("Horizontal", vals, implot.ImPlotInfLinesFlags_Horizontal, 0, 4)
			implot.EndPlot()
		}
	}
	return
}
