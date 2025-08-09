package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/imgui"
)

type Demofunc func()

func demoHeader(label string, demo Demofunc) {
	if imgui.TreeNodeEx(label) {
		demo()
		imgui.TreePop()
	}
}

func MakeRenderPortedDemo() Demofunc {
	var bargroup = MakeDemoBarGroupsDemo()
	var stairstep = MakeStairstepDemo()
	var scatter = MakeScatterDemo()
	var errorbar = MakeErrorbarDemo()
	var stems = MakeStemDemo()
	var inflines = MakeInfLinesDemo()
	var piechart = MakePieChartDemo()
	var heatmap = MakeHeatmapsDemo()
	var hist = MakeHistogramDemo()
	var hist2d = MakeHistogram2DDemo()
	var digital = MakeDigitalDemo()
	var image = MakeImageDemo()
	var markers = MakeMarkersAndTextDemo()
	var r Demofunc = func() {
		demoHeader("Lines Plot", DemoLinePlot)
		demoHeader("Shaded Plot", DemoShadedPlot)
		demoHeader("Bars Plot", DemoBarPlot)
		demoHeader("Bar Groups Plot", bargroup)
		demoHeader("Stairstep Plot", stairstep)
		demoHeader("Scatter Plot", scatter)
		demoHeader("Error Bar", errorbar)
		demoHeader("Stems Plot", stems)
		demoHeader("Infinite Lines", inflines)
		demoHeader("Pie Chart", piechart)
		demoHeader("Heatmap", heatmap)
		demoHeader("Histogram", hist)
		demoHeader("Histogram2D", hist2d)
		demoHeader("Digital", digital)
		demoHeader("Image", image)
		imgui.SetNextItemOpenV(true, imgui.ImGuiCond_Once)
		demoHeader("Markers & Text", markers)
	}
	return r
}
