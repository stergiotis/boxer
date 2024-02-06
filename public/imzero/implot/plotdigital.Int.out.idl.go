//go:build fffi_idl_code

package implot

func PlotDigitalInt[T ~int](label_id string, xs []T, ys []T) {
	_ = `ImPlot::PlotDigital(label_id,xs,ys,getSliceLength(xs))`
}

func PlotDigitalIntV[T ~int](label_id string, xs []T, ys []T, flags ImPlotDigitalFlags, offset int, stride int) {
	_ = `ImPlot::PlotDigital(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)),flags,offset,stride)`
}
