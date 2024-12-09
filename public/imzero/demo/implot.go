//go:build !bootstrap

package demo

import (
	"math"

	"github.com/stergiotis/boxer/public/imzero/implot"
	"github.com/stergiotis/boxer/public/imzero/implot/porteddemo"
)

var values []float32

func RenderLinePlotDemo() {
	if implot.BeginPlotV("hello", complex(600, 300), 0) {
		if values == nil {
			const l = 4096
			values = make([]float32, 0, l)
			for i := 0; i < l; i++ {
				values = append(values, float32(math.Sin(float64(i)/l*math.Pi)))
			}
		}
		implot.SetupAxes("x", "y", 0, 0)
		implot.SetupFinish()
		implot.PlotLineFloat32("f(x)", values)
		implot.EndPlot()
	}
}

func RenderImPlotDemo() {
	implot.ShowDemoWindow()
}

var pd = porteddemo.MakeRenderPortedDemo()

func RenderImPlotPortedDemo() {
	pd()
}
