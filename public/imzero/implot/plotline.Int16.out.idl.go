//go:build fffi_idl_code

package implot

func PlotLineInt16[T ~int16](label_id string, values []T) {
	_ = `ImPlot::PlotLine(label_id,values,getSliceLength(values))`
}

func PlotLineInt16V[T ~int16](label_id string, values []T, xscale float64, xstart float64, flags ImPlotLineFlags, offset int, stride int) {
	_ = `ImPlot::PlotLine(label_id,values,getSliceLength(values),xscale,xstart,flags,offset,stride)`
}

func PlotLineXYInt16(label_id string, xs []int16, ys []int16) {
	_ = `ImPlot::PlotLine(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotLineXYInt16V(label_id string, xs []int16, ys []int16, flags ImPlotLineFlags, offset int, stride int) {
	_ = `ImPlot::PlotLine(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)),flags,offset,stride)`
}
