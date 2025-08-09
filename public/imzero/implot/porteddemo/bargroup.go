package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/implot"
)

func MakeDemoBarGroupsDemo() (r Demofunc) {
	data := []int8{83, 67, 23, 89, 83, 78, 91, 82, 85, 90, // midterm
		80, 62, 56, 99, 55, 78, 88, 78, 90, 100, // final
		80, 69, 52, 92, 72, 78, 75, 76, 89, 95} // course

	ilabels := []implot.NullSeparatedStringArray{
		implot.MakeNullSeparatedStringArray("Midterm Exam"),
		implot.MakeNullSeparatedStringArray("Midterm Exam", "Final Exam"),
		implot.MakeNullSeparatedStringArray("Midterm Exam", "Final Exam", "Course Grade"),
	}
	glabels := implot.MakeNullSeparatedStringArray("S1", "S2", "S3", "S4", "S5", "S6", "S7", "S8", "S9", "S10")
	positions := []float64{0.0, 1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0}
	size := float32(0.67)
	flags := implot.ImPlotBarGroupsFlags_None
	horz := false
	items := 3
	groups := 10

	r = func() {
		flags, _ = imgui.ToggleFlags("Stacked", flags, implot.ImPlotBarGroupsFlags_Stacked)
		imgui.SameLine()
		horz, _ = imgui.Toggle("Horizontal", horz)
		items, _ = imgui.SliderInt("Items", items, 1, 3)
		size, _ = imgui.SliderFloat("Size", size, 0.0, 1.0)

		if implot.BeginPlot("Bar Group") {
			implot.SetupLegend(implot.ImPlotLocation_East, implot.ImPlotLegendFlags_Outside)
			//implot.SetupFinish()
			if horz {
				implot.SetupAxes("Score", "Student", implot.ImPlotAxisFlags_AutoFit, implot.ImPlotAxisFlags_AutoFit)
				implot.SetupAxisTicks(implot.ImAxis_Y1, positions, glabels, false)
				implot.SetupFinish()
				implot.PlotBarGroupsInt8V(ilabels[items-1], data, groups, float64(size), 0.0, flags|implot.ImPlotBarGroupsFlags_Horizontal)
			} else {
				implot.SetupAxes("Student", "Score", implot.ImPlotAxisFlags_AutoFit, implot.ImPlotAxisFlags_AutoFit)
				implot.SetupAxisTicks(implot.ImAxis_X1, positions, glabels, false)
				implot.SetupFinish()
				implot.PlotBarGroupsInt8V(ilabels[items-1], data, groups, float64(size), 0.0, flags)
			}
			implot.EndPlot()
		}
	}
	return
}
