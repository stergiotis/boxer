package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/implot"
)

func MakeMarkersAndTextDemo() (r Demofunc) {
	var mk_size float32
	var mk_weight float32
	r = func() {
		if mk_size == 0.0 {
			style := &implot.ImPlotStyle{}
			style.Load(implot.GetStyle())
			mk_size = style.MarkerSize
			mk_weight = style.MarkerWeight
		}
		mk_size, _ = imgui.DragFloatV("Marker Size", mk_size, 0.1, 2.0, 10.0, "%.2f px", 0)
		mk_weight, _ = imgui.DragFloatV("Marker Weight", mk_weight, 0.05, 0.5, 3.0, "%.2f px", 0)
		if implot.BeginPlotV("##MarkerStyles", imgui.MakeImVec2(-1, 0), implot.ImPlotFlags_CanvasOnly) {
			implot.SetupAxes("", "", implot.ImPlotAxisFlags_NoDecorations, implot.ImPlotAxisFlags_NoDecorations)
			implot.SetupAxesLimits(0, 10, 0, 12, implot.ImPlotCond_Once)
			implot.SetupFinish()

			xs := []int8{1, 4}
			ys := []int8{10, 11}
			var m implot.ImPlotMarker

			// filled markers
			for m = 0; m < implot.ImPlotMarker_COUNT; m++ {
				imgui.PushIDInt(int(m))
				implot.SetNextMarkerStyleV(m, mk_size, implot.ImPlotAutoCol, mk_weight, implot.ImPlotAutoCol)
				implot.PlotLineXYInt8("##Filled", xs, ys)
				imgui.PopID()
				ys[0]--
				ys[1]--
			}
			xs[0] = 6
			xs[1] = 9
			ys[0] = 10
			ys[1] = 11
			// open markers
			for m = 0; m < implot.ImPlotMarker_COUNT; m++ {
				imgui.PushIDInt(int(m))
				implot.SetNextMarkerStyleV(m, mk_size, imgui.MakeImVec4(0, 0, 0, 0), mk_weight, implot.ImPlotAutoCol)
				implot.PlotLineXYInt8("##Open", xs, ys)
				imgui.PopID()
				ys[0]--
				ys[1]--
			}

			implot.PlotText("Filled Markers", 2.5, 6.0)
			implot.PlotText("Open Markers", 7.5, 6.0)

			implot.PushStyleColorImVec4(implot.ImPlotCol_InlayText, imgui.MakeImVec4(1, 0, 1, 1))
			implot.PlotTextV("Vertical Text", 5.0, 6.0, imgui.MakeImVec2(0, 0), implot.ImPlotTextFlags_Vertical)
			implot.PopStyleColor(1)

			implot.EndPlot()
		}
	}
	return
}
