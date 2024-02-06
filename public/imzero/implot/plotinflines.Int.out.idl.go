//go:build fffi_idl_code

package implot

func PlotInfLinesInt[T ~int](label_id string, values []T) {
	_ = `ImPlot::PlotInfLines(label_id,values,getSliceLength(values))`
}

func PlotInfLinesIntV[T ~int](label_id string, values []T, flags ImPlotInfLinesFlags, offset int, stride int) {
	_ = `ImPlot::PlotInfLines(label_id,values,getSliceLength(values),flags,offset,stride)`
}
