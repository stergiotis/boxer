//go:build fffi_idl_code

package implot

func PlotHistogramUInt32[T ~uint32](label_id string, values []T) (r float64) {
	_ = `r = ImPlot::PlotHistogram(label_id,values,getSliceLength(values))`
	return
}

func PlotHistogramUInt32V[T ~uint32](label_id string, values []T, bins ImPlotBin, bar_scale float64, rangeP ImPlotRange, flags ImPlotHistogramFlags) (r float64) {
	_ = `r = ImPlot::PlotHistogram(label_id,values,getSliceLength(values),bins,bar_scale,ImPlotRange(rangeP[0],rangeP[1]),flags)`
	return
}
