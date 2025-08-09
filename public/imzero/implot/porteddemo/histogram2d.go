package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/implot"
	"math/rand/v2"
)

func MakeHistogram2DDemo() (r Demofunc) {
	count := 50000
	xybins := []int{100, 100}
	histFlags := implot.ImPlotHistogramFlags_None

	dist1 := make([]float32, 100000, 100000)
	dist2 := make([]float32, 100000, 100000)

	ra := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	maxCount := 0.0
	flags := implot.ImPlotAxisFlags_AutoFit | implot.ImPlotAxisFlags_Foreground
	rect := implot.MakeImPlotRect(-6.0, 6.0, -6.0, 6.0)
	populateWithNormalDistribution(ra, len(dist1), 1.0, 2.0, dist1)
	populateWithNormalDistribution(ra, len(dist2), 1.0, 1.0, dist2)
	r = func() {
		count, _ = imgui.SliderInt("Count", count, 100, 100000)
		xybins, _ = imgui.SliderIntNV("Bins", xybins, 1, 500, "%d", 0)
		imgui.SameLine()
		histFlags, _ = imgui.ToggleFlags("Density", histFlags, implot.ImPlotHistogramFlags_Density)

		implot.PushColormap("Hot")
		// FIXME imgui.GetStyle().ItemSpacing.x
		if implot.BeginPlotV("##Hist2D", imgui.MakeImVec2(real(imgui.GetContentRegionAvail())-100.0-10.0, 0.0), 0) {
			implot.SetupAxes("", "", flags, flags)
			implot.SetupAxesLimits(-6.0, 6.0, -6.0, 6.0, implot.ImPlotCond_Once)
			implot.SetupFinish()

			maxCount = implot.PlotHistogram2DFloat32V("Hist2D", dist1[:count], dist2[:count], implot.ImPlotBin(xybins[0]), implot.ImPlotBin(xybins[1]), rect, histFlags)
			implot.EndPlot()
		}
		imgui.SameLine()
		var scaleName string
		if histFlags&implot.ImPlotHistogramFlags_Density != 0 {
			scaleName = "Density"
		} else {
			scaleName = "Count"
		}
		implot.ColormapScale(scaleName, 0, maxCount, imgui.MakeImVec2(100.0, 0.0), "%.3f", 0, implot.ImPlotColormap_AUTO)
		implot.PopColormap(1)
	}
	return
}
