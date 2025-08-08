//go:build !bootstrap

package implot

import "github.com/stergiotis/boxer/public/imzero/imgui"

type ImPlotPoint complex128

// NullSeparatedStringArray A string containing multiple substrings separated by the null character. No null termination is needed.
type NullSeparatedStringArray string

type ImPlotRange complex128

type ImPlotRect [4]float64

type ImPlotStyleForeignPtr uintptr

type ImPlotStyle struct {
	LineWeight       float32 // = 1,      item line weight in pixels
	Marker           int     // = ImPlotMarker_None, marker specification
	MarkerSize       float32 // = 4,      marker size in pixels (roughly the marker's "radius")
	MarkerWeight     float32 // = 1,      outline weight of markers in pixels
	FillAlpha        float32 // = 1,      alpha modifier applied to plot fills
	ErrorBarSize     float32 // = 5,      error bar whisker width in pixels
	ErrorBarWeight   float32 // = 1.5,    error bar whisker weight in pixels
	DigitalBitHeight float32 // = 8,      digital channels bit height (at y = 1.0f) in pixels
	DigitalBitGap    float32 // = 4,      digital channels bit padding gap in pixels
	// plot styling variables
	PlotBorderSize     float32      // = 1,      line thickness of border around plot area
	MinorAlpha         float32      // = 0.25    alpha multiplier applied to minor axis grid lines
	MajorTickLen       imgui.ImVec2 // = 10,10   major tick lengths for X and Y axes
	MinorTickLen       imgui.ImVec2 // = 5,5     minor tick lengths for X and Y axes
	MajorTickSize      imgui.ImVec2 // = 1,1     line thickness of major ticks
	MinorTickSize      imgui.ImVec2 // = 1,1     line thickness of minor ticks
	MajorGridSize      imgui.ImVec2 // = 1,1     line thickness of major grid lines
	MinorGridSize      imgui.ImVec2 // = 1,1     line thickness of minor grid lines
	PlotPadding        imgui.ImVec2 // = 10,10   padding between widget frame and plot area, labels, or outside legends (i.e. main padding)
	LabelPadding       imgui.ImVec2 // = 5,5     padding between axes labels, tick labels, and plot edge
	LegendPadding      imgui.ImVec2 // = 10,10   legend padding from plot edges
	LegendInnerPadding imgui.ImVec2 // = 5,5     legend inner padding from legend edges
	LegendSpacing      imgui.ImVec2 // = 5,0     spacing between legend entries
	MousePosPadding    imgui.ImVec2 // = 10,10   padding between plot edge and interior mouse location text
	AnnotationPadding  imgui.ImVec2 // = 2,2     text padding around annotation labels
	FitPadding         imgui.ImVec2 // = 0,0     additional fit padding as a percentage of the fit extents (e.g. ImVec2(0.1f,0.1f) adds 10% to the fit extents of X and Y)
	PlotDefaultSize    imgui.ImVec2 // = 400,300 default size used when ImVec2(0,0) is passed to BeginPlot
	PlotMinSize        imgui.ImVec2 // = 200,150 minimum size plot frame can be when shrunk
	// style colors
	Colors []imgui.ImVec4 // Array of styling colors. Indexable with ImPlotCol_ enums.
	// colormap
	Colormap ImPlotColormap // The current colormap. Set this to either an ImPlotColormap_ enum or an index returned by AddColormap.
	// settings/flags
	UseLocalTime   bool // = false,  axis labels will be formatted for your timezone when ImPlotAxisFlag_Time is enabled
	UseISO8601     bool // = false,  dates will be formatted according to ISO 8601 where applicable (e.g. YYYY-MM-DD, YYYY-MM, --MM-DD, etc.)
	Use24HourClock bool // = false,  times will be formatted using a 24 hour clock
}

type ImDrawListPtr = imgui.ImDrawListPtr
