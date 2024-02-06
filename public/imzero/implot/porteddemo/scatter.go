package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/implot"
	"math/rand"
)

func MakeScatterDemo() (r demofunc) {
	ra := rand.New(rand.NewSource(0))

	var xs1, ys1 []float32
	xs1 = make([]float32, 100, 100)
	ys1 = make([]float32, 100, 100)
	for i := 0; i < 100; i++ {
		xs1[i] = float32(i) * 0.01
		ys1[i] = xs1[i] + 0.1*(ra.Float32())
	}
	var xs2, ys2 []float32
	xs2 = make([]float32, 50, 50)
	ys2 = make([]float32, 50, 50)
	for i := 0; i < 50; i++ {
		xs2[i] = 0.25 + 0.2*ra.Float32()
		ys2[i] = 0.75 + 0.2*ra.Float32()
	}
	r = func() {
		if implot.BeginPlot("Scatter Plot") {
			implot.PlotScatterXYFloat32("Data 1", xs1, ys1)
			implot.PushStyleVar(implot.ImPlotStyleVar_FillAlpha, 0.25)
			implot.SetNextMarkerStyleV(implot.ImPlotMarker_Square, 6, implot.GetColormapColor(1, implot.ImPlotColormap_AUTO), implot.ImPlotAutoFloat32, implot.GetColormapColor(1, implot.ImPlotColormap_AUTO))
			implot.PlotScatterXYFloat32("Data 2", xs2, ys2)
			implot.PopStyleVar(1)
			implot.EndPlot()
		}
	}
	return
}
