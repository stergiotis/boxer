//go:build fffi_idl_code

package implot

func PlotLineUInt8[T ~uint8](label_id string, values []T) {
	_ = `ImPlot::PlotLine(label_id,values,getSliceLength(values))`
}

func PlotLineUInt8V[T ~uint8](label_id string, values []T, xscale float64, xstart float64, flags ImPlotLineFlags, offset int, stride int) {
	_ = `ImPlot::PlotLine(label_id,values,getSliceLength(values),xscale,xstart,flags,offset,stride)`
}

func PlotLineXYUInt8(label_id string, xs []uint8, ys []uint8) {
	_ = `ImPlot::PlotLine(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotLineXYUInt8V(label_id string, xs []uint8, ys []uint8, flags ImPlotLineFlags, offset int, stride int) {
	_ = `ImPlot::PlotLine(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)),flags,offset,stride)`
}
