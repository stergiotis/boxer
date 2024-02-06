package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/implot"
	"math"
	"math/rand"
)

func populateWithNormalDistribution(ra *rand.Rand, n int, mu float32, sigma float32, dest []float32) {
	for i := 0; i < n; i++ {
		dest[i] = float32(ra.NormFloat64()*float64(sigma) + float64(mu))
	}
}
func MakeHistogramDemo() (r demofunc) {
	hist_flags := implot.ImPlotHistogramFlags_Density
	bins := implot.ImPlotBin(50)
	mu := 5.0
	sigma := 2.0
	rangeP := false
	rr := implot.ImPlotRange(complex(-3.0, 13.0))
	ra := rand.New(rand.NewSource(0))
	dist := make([]float32, 10000, 10000)
	populateWithNormalDistribution(ra, len(dist), float32(mu), float32(sigma), dist)
	x := make([]float32, 100, 100)
	y := make([]float32, 100, 100)
	r = func() {
		imgui.SetNextItemWidth(200)
		if imgui.RadioButton("Sqrt", bins == implot.ImPlotBin_Sqrt) {
			bins = implot.ImPlotBin_Sqrt
		}
		imgui.SameLine()
		if imgui.RadioButton("Sturges", bins == implot.ImPlotBin_Sturges) {
			bins = implot.ImPlotBin_Sturges
		}
		imgui.SameLine()
		if imgui.RadioButton("Rice", bins == implot.ImPlotBin_Rice) {
			bins = implot.ImPlotBin_Rice
		}
		imgui.SameLine()
		if imgui.RadioButton("Scott", bins == implot.ImPlotBin_Scott) {
			bins = implot.ImPlotBin_Scott
		}
		imgui.SameLine()
		if imgui.RadioButton("N Bins", bins >= 0) {
			bins = 50
		}
		if bins >= 0 {
			imgui.SameLine()
			imgui.SetNextItemWidth(200)
			t, _ := imgui.SliderInt("##Bins", int(bins), 1, 100)
			bins = implot.ImPlotBin(t)
		}
		hist_flags, _ = imgui.ToggleFlags("Horizontal", hist_flags, implot.ImPlotHistogramFlags_Horizontal)
		imgui.SameLine()
		hist_flags, _ = imgui.ToggleFlags("Density", hist_flags, implot.ImPlotHistogramFlags_Density)
		imgui.SameLine()
		hist_flags, _ = imgui.ToggleFlags("Cumulative", hist_flags, implot.ImPlotHistogramFlags_Cumulative)

		rangeP, _ = imgui.Toggle("Range", rangeP)
		if rangeP {
			imgui.SameLine()
			imgui.SetNextItemWidth(200)
			u := [2]float32{real(complex64(rr)), imag(complex64(rr))}
			u, _ = imgui.DragFloat2V("##Range2", u, 0.1, -3, 13, "%.3f", imgui.ImGuiSliderFlags_None)
			rr = implot.ImPlotRange(complex(u[0], u[1]))
			imgui.SameLine()
			hist_flags, _ = imgui.ToggleFlags("ExcludeOutliers", hist_flags, implot.ImPlotHistogramFlags_NoOutliers)
		} else {
			rr = implot.ImPlotRange(complex(-3.0, 13.0))
		}
		if hist_flags&implot.ImPlotHistogramFlags_Density != 0 {
			for i := 0; i < 100; i++ {
				x[i] = -3 + 16*float32(i)/99.0
				y[i] = float32(math.Exp(-(float64(x[i])-mu)*(float64(x[i])-mu)/(2.0*sigma*sigma)) / (sigma * math.Sqrt(2*math.Pi)))
			}
			if hist_flags&implot.ImPlotHistogramFlags_Cumulative != 0 {
				for i := 1; i < 100; i++ {
					y[i] += y[i-1]
				}
				for i := 0; i < 100; i++ {
					y[i] /= y[99]
				}
			}
		}

		if implot.BeginPlot("##Histogram") {
			implot.SetupAxes("", "", implot.ImPlotAxisFlags_AutoFit, implot.ImPlotAxisFlags_AutoFit)
			implot.SetNextFillStyleV(implot.ImPlotAutoCol, 0.5)
			implot.SetupFinish()

			implot.PlotHistogramFloat32V("Empirical", dist, bins, 1.0, rr, hist_flags)
			if (hist_flags&implot.ImPlotHistogramFlags_Density) != 0 && !((hist_flags & implot.ImPlotHistogramFlags_NoOutliers) != 0) {
				if hist_flags&implot.ImPlotHistogramFlags_Horizontal != 0 {
					implot.PlotLineXYFloat32("Theoretical", y, x)
				} else {
					implot.PlotLineXYFloat32("Theoretical", x, y)
				}
			}

			implot.EndPlot()
		}
	}
	return
}
