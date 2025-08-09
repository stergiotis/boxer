package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/implot"
)

func MakePieChartDemo() (r Demofunc) {
	labels1 := implot.MakeNullSeparatedStringArray("Frogs", "Hogs", "Dogs", "Logs")
	labels2 := implot.MakeNullSeparatedStringArray("A", "B", "C", "D", "E")
	data1 := []float32{0.15, 0.30, 0.2, 0.05}
	data2 := []float32{1, 1, 2, 3, 5}
	flags := implot.ImPlotPieChartFlags_None
	r = func() {
		imgui.SetNextItemWidth(250)
		t, _ := imgui.DragFloat4V("Values", [4]float32{data1[0], data1[1], data1[2], data1[3]}, 0.01, 0.0, 1.0, "%.02f", 0)
		data1[0] = t[0]
		data1[1] = t[1]
		data1[2] = t[2]
		data1[3] = t[3]
		flags, _ = imgui.ToggleFlags("ImPlotPieChartFlags_Normalize", flags, implot.ImPlotPieChartFlags_Normalize)
		flags, _ = imgui.ToggleFlags("ImPlotPieChartFlags_IgnoreHidden", flags, implot.ImPlotPieChartFlags_IgnoreHidden)
		if implot.BeginPlotV("##Pie1", imgui.ImVec2(complex(250.0, 250.0)), implot.ImPlotFlags_Equal|implot.ImPlotFlags_NoMouseText) {
			implot.SetupAxes("", "", implot.ImPlotAxisFlags_NoDecorations, implot.ImPlotAxisFlags_NoDecorations)
			implot.SetupAxesLimits(0, 1, 0, 1, implot.ImPlotCond_Once)
			implot.SetupFinish()

			implot.PlotPieChartFloat32V(labels1, data1, 0.5, 0.5, 0.4, "%.2f", 90, flags)

			implot.EndPlot()
		}

		imgui.SameLine()
		implot.PushColormapById(implot.ImPlotColormap_Pastel)
		if implot.BeginPlotV("##Pie2", imgui.ImVec2(complex(250.0, 250.0)), implot.ImPlotFlags_Equal|implot.ImPlotFlags_NoMouseText) {
			implot.SetupAxes("", "", implot.ImPlotAxisFlags_NoDecorations, implot.ImPlotAxisFlags_NoDecorations)
			implot.SetupAxesLimits(0, 1, 0, 1, implot.ImPlotCond_Once)
			implot.SetupFinish()

			implot.PlotPieChartFloat32V(labels2, data2, 0.5, 0.5, 0.4, "%.0f", 180, flags)

			implot.EndPlot()
		}
		implot.PopColormap(1)
	}
	return
}
