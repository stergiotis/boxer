package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/implot"
	"math/rand/v2"
)

func MakeHeatmapsDemo() (r Demofunc) {
	values1 := []float32{
		0.8, 2.4, 2.5, 3.9, 0.0, 4.0, 0.0,
		2.4, 0.0, 4.0, 1.0, 2.7, 0.0, 0.0,
		1.1, 2.4, 0.8, 4.3, 1.9, 4.4, 0.0,
		0.6, 0.0, 0.3, 0.0, 3.1, 0.0, 0.0,
		0.7, 1.7, 0.6, 2.6, 2.2, 6.2, 0.0,
		1.3, 1.2, 0.0, 0.0, 0.0, 3.2, 5.1,
		0.1, 2.0, 0.0, 1.4, 0.0, 1.9, 6.3}
	scale_min := 0.0
	scale_max := 6.3
	xlabels := implot.MakeNullSeparatedStringArray("C1", "C2", "C3", "C4", "C5", "C6", "C7")
	ylabels := implot.MakeNullSeparatedStringArray("R1", "R2", "R3", "R4", "R5", "R6", "R7")
	ma := implot.ImPlotColormap_Viridis
	hm_flags := implot.ImPlotHeatmapFlags_None
	axes_flags := implot.ImPlotAxisFlags_Lock | implot.ImPlotAxisFlags_NoGridLines | implot.ImPlotAxisFlags_NoTickMarks

	ra := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	const size = 80
	values2 := make([]float32, size*size, size*size)
	r = func() {
		if implot.ColormapButton(implot.GetColormapName(ma), imgui.ImVec2(complex(225, 0)), ma) {
			ma = implot.ImPlotColormap((int(ma) + 1) % implot.GetColormapCount())
			// We bust the color cache of our plots so that item colors will
			// resample the new colormap in the event that they have already
			// been created. See documentation in implot.h.
			implot.BustColorCacheV("##Heatmap1")
			implot.BustColorCacheV("##Heatmap2")
		}
		imgui.SameLine()
		imgui.LabelText("##Colormap Index", "Change Colormap")
		imgui.SetNextItemWidth(225)
		// FIXME drag float

		hm_flags, _ = imgui.ToggleFlags("Column Major", hm_flags, implot.ImPlotHeatmapFlags_ColMajor)

		implot.PushColormapById(ma)

		if implot.BeginPlotV("##Heatmap1", imgui.ImVec2(complex(225, 225)), implot.ImPlotFlags_NoLegend|implot.ImPlotFlags_NoMouseText) {
			implot.SetupAxes("", "", axes_flags, axes_flags)
			implot.SetupAxisTicksRange(implot.ImAxis_X1, 0+1.0/14.0, 1-1.0/14.0, 7, xlabels, true)
			implot.SetupAxisTicksRange(implot.ImAxis_Y1, 1-1.0/14.0, 0+1.0/14.0, 7, ylabels, true)
			implot.SetupFinish()
			implot.PlotHeatmapFloat32V("heat", values1, 7, scale_min, scale_max, "%g", implot.ImPlotPoint(complex(0, 0)), implot.ImPlotPoint(complex(1, 1)), hm_flags)

			implot.EndPlot()
		}
		imgui.SameLine()
		implot.ColormapScale("##HeatScale", scale_min, scale_max, imgui.ImVec2(complex(60.0, 225.0)), "%.02f", implot.ImPlotColormapScaleFlags_None, ma)

		imgui.SameLine()

		for i := 0; i < size*size; i++ {
			values2[i] = ra.Float32()
		}
		if implot.BeginPlotV("##Heatmap2", imgui.ImVec2(complex(225, 225)), implot.ImPlotFlags_None) {
			implot.SetupAxes("", "", implot.ImPlotAxisFlags_NoDecorations, implot.ImPlotAxisFlags_NoDecorations)
			implot.SetupAxesLimits(-1.0, 1.0, -1.0, 1.0, implot.ImPlotCond_Once)
			implot.SetupFinish()
			implot.PlotHeatmapFloat32V("heat1", values2, size, 0, 1, "", implot.ImPlotPoint(complex(0, 0)), implot.ImPlotPoint(complex(1, 1)), 0)
			implot.PlotHeatmapFloat32V("heat2", values2, size, 0, 1, "", implot.ImPlotPoint(complex(-1, -1)), implot.ImPlotPoint(complex(0, 0)), implot.ImPlotHeatmapFlags_None)

			implot.EndPlot()
		}

		implot.PopColormap(1)
	}
	return
}
