//go:build fffi_idl_code

package implot

func PlotPieChartUInt32[T ~uint32](label_ids NullSeparatedStringArray, values []T, x float64, y float64, radius float64) {
	_ = `
size_t n_label_ids;
auto ary_label_ids = convertNullSeparatedStringArrayToArray(label_ids,n_label_ids);
assert(n_label_ids == getSliceLength(values));
ImPlot::PlotPieChart(ary_label_ids,values,(int)std::min(n_label_ids,getSliceLength(values)),x,y,radius);
`
}

func PlotPieChartUInt32V[T ~uint32](label_ids NullSeparatedStringArray, values []T, x float64, y float64, radius float64, label_fmt string, angle0 float64, flags ImPlotPieChartFlags) {
_ = `
size_t n_label_ids;
auto ary_label_ids = convertNullSeparatedStringArrayToArray(label_ids,n_label_ids);
assert(n_label_ids == getSliceLength(values));
ImPlot::PlotPieChart(ary_label_ids,values,(int)std::min(n_label_ids,getSliceLength(values)),x,y,radius,label_fmt,angle0,flags);
`
}
