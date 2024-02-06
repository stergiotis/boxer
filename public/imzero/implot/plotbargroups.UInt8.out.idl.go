//go:build fffi_idl_code

package implot

func PlotBarGroupsUInt8(label_ids NullSeparatedStringArray, values []uint8, groups int) {
	_ = `
size_t n_labels;
auto ary_labels = convertNullSeparatedStringArrayToArray(label_ids,n_labels);
if(groups == 0) {
   groups = (int)(getSliceLength(values)/n_labels);
}
ImPlot::PlotBarGroups(ary_labels,values,(int)n_labels,groups);
`
}

func PlotBarGroupsUInt8V(label_ids NullSeparatedStringArray, values []uint8, groups int, size float64, shift float64, flags ImPlotBarGroupsFlags) {
	_ = `
size_t n_labels;
auto ary_labels = convertNullSeparatedStringArrayToArray(label_ids,n_labels);
if(groups == 0) {
   groups = (int)(getSliceLength(values)/n_labels);
}
ImPlot::PlotBarGroups(ary_labels,values,(int)n_labels,groups,size,shift,flags);
`
}
