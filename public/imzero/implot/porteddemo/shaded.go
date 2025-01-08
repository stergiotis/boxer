package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/implot"
	"math"
	"math/rand"
)

type shadedstate struct {
	r                          *rand.Rand
	xs, ys, ys1, ys2, ys3, ys4 []float32
	alpha                      float32
	inited                     bool
}

var shaded = shadedstate{
	inited: false,
	xs:     make([]float32, 0, 1001),
	ys:     make([]float32, 1001, 1001),
	ys1:    make([]float32, 1001, 1001),
	ys2:    make([]float32, 1001, 1001),
	ys3:    make([]float32, 1001, 1001),
	ys4:    make([]float32, 1001, 1001),
	alpha:  0.75,
	r:      rand.New(rand.NewSource(0)),
}

func DemoShadedPlot() {
	if !shaded.inited {
		xs := make([]float32, 0, 1001)
		for i := 0; i < 1001; i++ {
			x := float32(i) * 0.001
			xs = append(xs, x)
		}
		shaded.xs = xs
	}

	xs := shaded.xs
	ys := shaded.ys
	ys1 := shaded.ys1
	ys2 := shaded.ys2
	ys3 := shaded.ys3
	ys4 := shaded.ys4
	r := shaded.r
	alpha := shaded.alpha
	for i := 0; i < 1001; i++ {
		x := xs[i]
		ys[i] = float32(0.25 + 0.25*math.Sin(25.0*float64(x))*math.Sin(5.0*float64(x)) + r.Float64()*0.02 - 0.01)
		ys1[i] = ys[i] + r.Float32()*0.11 + 0.1
		ys2[i] = ys[i] - r.Float32()*0.11 - 0.1
		ys3[i] = 0.75 + 0.2*float32(math.Sin(25.0*float64(x)))
		ys4[i] = 0.75 + 0.1*float32(math.Cos(25.0*float64(x)))
	}
	alpha, _ = imgui.Knob("Alpha", alpha, 0.01, 1.0)
	shaded.alpha = alpha

	if implot.BeginPlot("Shaded Plots") {
		implot.PushStyleVar(implot.ImPlotStyleVar_FillAlpha, alpha)
		implot.PlotShadedXY1Y2Float32("Uncertain Data", xs, ys1, ys2)
		implot.PlotLineXYFloat32("Uncertain Data", xs, ys)
		implot.PlotShadedXY1Y2Float32("Overlapping", xs, ys3, ys4)
		implot.PlotLineXYFloat32("Overlapping", xs, ys3)
		implot.PlotLineXYFloat32("Overlapping", xs, ys4)
		implot.EndPlot()
	}
}
