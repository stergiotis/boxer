//go:build fffi_idl_code

package implot

import . "github.com/stergiotis/boxer/public/imzero/imgui"

func GetLocationPos(outer_rect ImRect, inner_size ImVec2, loc ImPlotLocation, pad ImVec2) (r ImVec2) {
	_ = `auto r = ImPlot::GetLocationPos(ImRect(outer_rect[0],outer_rect[1],outer_rect[2],outer_rect[3]), inner_size, loc, pad)`
	return
}

// SetupAxisTicks Sets an axis' ticks and optionally the labels. To keep the default ticks, set keep_default=true.
func SetupAxisTicks(idx ImAxis, values []float64, labels NullSeparatedStringArray, show_default bool) {
	_ = `
size_t n_labels;
auto ary_labels = convertNullSeparatedStringArrayToArray(labels,n_labels);
ImPlot::SetupAxisTicks(idx, values, (int)n_labels, ary_labels, show_default);
`
}

// SetupAxisTicks Sets an axis' ticks and optionally the labels for the next plot. To keep the default ticks, set keep_default=true.
func SetupAxisTicksRange(idx ImAxis, v_min float64, v_max float64, n_ticks int, labels NullSeparatedStringArray, show_default bool) {
	_ = `
size_t n_labels;
auto ary_labels = convertNullSeparatedStringArrayToArray(labels,n_labels);
ImPlot::SetupAxisTicks(idx, v_min, v_max, (int)n_labels, ary_labels, show_default);
`
}

// Annotation Shows an annotation callout at a chosen point. Clamping keeps annotations in the plot area. Annotations are always rendered on top.
func AnnotationText(x float64, y float64, col ImVec4, pix_offset ImVec2, clamp bool, text string) {
	_ = `ImPlot::Annotation(x, y, col, pix_offset, clamp, "%.*s", (int)getStringLength(text),text)`
}
