//go:build fffi_idl_code

package implot

func PlotInfLinesUInt[T ~uint](label_id string, values []T) {
	_ = `ImPlot::PlotInfLines(label_id,values,getSliceLength(values))`
}

func PlotInfLinesUIntV[T ~uint](label_id string, values []T, flags ImPlotInfLinesFlags, offset int, stride int) {
	_ = `ImPlot::PlotInfLines(label_id,values,getSliceLength(values),flags,offset,stride)`
}
