//go:build !bootstrap

package implot

import "github.com/stergiotis/boxer/public/imzero/imgui"

// ImPlotAutoCol Special color used to indicate that a color should be deduced automatically
var ImPlotAutoCol = imgui.ImVec4{0.0, 0.0, 0.0, -1.0}

const ImPlotAuto = -1
const ImPlotAutoFloat32 = float32(ImPlotAuto)

type ImPlotMarker int

const (
	ImPlotMarker_None     = ImPlotMarker(-1) // no marker
	ImPlotMarker_Circle   = iota - 1         // a circle marker (default)
	ImPlotMarker_Square   = iota - 1         // a square maker
	ImPlotMarker_Diamond  = iota - 1         // a diamond marker
	ImPlotMarker_Up       = iota - 1         // an upward-pointing triangle marker
	ImPlotMarker_Down     = iota - 1         // an downward-pointing triangle marker
	ImPlotMarker_Left     = iota - 1         // an leftward-pointing triangle marker
	ImPlotMarker_Right    = iota - 1         // an rightward-pointing triangle marker
	ImPlotMarker_Cross    = iota - 1         // a cross marker (not fillable)
	ImPlotMarker_Plus     = iota - 1         // a plus marker (not fillable)
	ImPlotMarker_Asterisk = iota - 1         // a asterisk marker (not fillable)
	ImPlotMarker_COUNT    = iota - 1 + 1
	ImPlotMarker_AUTO     = ImPlotMarker_Circle // auto value
)
