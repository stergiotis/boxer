//go:build fffi_idl_code

package implot

func PlotLineUInt32[T ~uint32](label_id string, values []T) {
	_ = `ImPlot::PlotLine(label_id,values,getSliceLength(values))`
}

func PlotLineUInt32V[T ~uint32](label_id string, values []T, xscale float64, xstart float64, flags ImPlotLineFlags, offset int, stride int) {
	_ = `ImPlot::PlotLine(label_id,values,getSliceLength(values),xscale,xstart,flags,offset,stride)`
}

func PlotLineXYUInt32(label_id string, xs []uint32, ys []uint32) {
	_ = `ImPlot::PlotLine(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotLineXYUInt32V(label_id string, xs []uint32, ys []uint32, flags ImPlotLineFlags, offset int, stride int) {
	_ = `ImPlot::PlotLine(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)),flags,offset,stride)`
}
