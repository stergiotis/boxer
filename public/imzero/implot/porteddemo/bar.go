package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/implot"
)

type barsstate struct {
	data []int8
}

var bars = barsstate{
	data: []int8{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
}

func DemoBarPlot() {
	if implot.BeginPlot("Bar Plot") {
		data := bars.data
		implot.PlotBarsInt8V("Vertical", data, 0.7, 1.0, 0, 0, 1)
		implot.PlotBarsInt8V("Horizontal", data, 0.4, 1.0, implot.ImPlotBarsFlags_Horizontal, 0, 1)
		implot.EndPlot()
	}
}
