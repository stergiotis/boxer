package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/implot"
)

func MakeErrorbarDemo() (r Demofunc) {
	xs := []float32{1, 2, 3, 4, 5}
	bar := []float32{1, 2, 5, 3, 4}
	lin1 := []float32{8, 8, 9, 7, 8}
	lin2 := []float32{6, 7, 6, 9, 6}
	err1 := []float32{0.2, 0.4, 0.2, 0.6, 0.4}
	err2 := []float32{0.4, 0.2, 0.4, 0.8, 0.6}
	err3 := []float32{0.09, 0.14, 0.09, 0.12, 0.16}
	err4 := []float32{0.02, 0.08, 0.15, 0.05, 0.2}
	r = func() {
		if implot.BeginPlot("##ErrorBars") {
			implot.SetupAxesLimits(0.0, 6.0, 0.0, 10.0, implot.ImPlotCond_Once)
			implot.SetupFinish()

			implot.PlotBarsXYFloat32V("Bar", xs, bar, 0.5, 0, 0, 4)
			implot.PlotErrorBarsFloat32("Bar", xs, bar, err1)
			implot.SetNextErrorBarStyleV(implot.GetColormapColor(1, implot.ImPlotColormap_AUTO), 0.0, implot.ImPlotAutoFloat32)
			implot.PlotErrorBarsPosNegFloat32("Line", xs, lin1, err1, err2)
			implot.SetNextMarkerStyleV(implot.ImPlotMarker_Square, implot.ImPlotAutoFloat32, implot.ImPlotAutoCol, implot.ImPlotAutoFloat32, implot.ImPlotAutoCol)
			implot.PlotLineXYFloat32("Line", xs, lin1)
			implot.PushStyleColorImVec4(implot.ImPlotCol_ErrorBar, implot.GetColormapColor(2, implot.ImPlotAuto))
			implot.PlotErrorBarsFloat32("Scatter", xs, lin2, err2)
			implot.PlotErrorBarsPosNegFloat32V("Scatter", xs, lin2, err3, err4, implot.ImPlotErrorBarsFlags_Horizontal, 0, 4)
			implot.PopStyleColor(1)
			implot.PlotScatterXYFloat32("Scatter", xs, lin2)

			implot.EndPlot()
		}
	}
	return
}
