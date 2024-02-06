//go:build fffi_idl_code

package implot

func PlotDigitalFloat64[T ~float64](label_id string, xs []T, ys []T) {
	_ = `ImPlot::PlotDigital(label_id,xs,ys,getSliceLength(xs))`
}

func PlotDigitalFloat64V[T ~float64](label_id string, xs []T, ys []T, flags ImPlotDigitalFlags, offset int, stride int) {
	_ = `ImPlot::PlotDigital(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)),flags,offset,stride)`
}
