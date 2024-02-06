//go:build fffi_idl_code

package implot

func PlotLineFloat64[T ~float64](label_id string, values []T) {
	_ = `ImPlot::PlotLine(label_id,values,getSliceLength(values))`
}

func PlotLineFloat64V[T ~float64](label_id string, values []T, xscale float64, xstart float64, flags ImPlotLineFlags, offset int, stride int) {
	_ = `ImPlot::PlotLine(label_id,values,getSliceLength(values),xscale,xstart,flags,offset,stride)`
}

func PlotLineXYFloat64(label_id string, xs []float64, ys []float64) {
	_ = `ImPlot::PlotLine(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotLineXYFloat64V(label_id string, xs []float64, ys []float64, flags ImPlotLineFlags, offset int, stride int) {
	_ = `ImPlot::PlotLine(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)),flags,offset,stride)`
}
