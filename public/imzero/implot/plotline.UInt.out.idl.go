//go:build fffi_idl_code

package implot

func PlotLineUInt[T ~uint](label_id string, values []T) {
	_ = `ImPlot::PlotLine(label_id,values,getSliceLength(values))`
}

func PlotLineUIntV[T ~uint](label_id string, values []T, xscale float64, xstart float64, flags ImPlotLineFlags, offset int, stride int) {
	_ = `ImPlot::PlotLine(label_id,values,getSliceLength(values),xscale,xstart,flags,offset,stride)`
}

func PlotLineXYUInt(label_id string, xs []uint, ys []uint) {
	_ = `ImPlot::PlotLine(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotLineXYUIntV(label_id string, xs []uint, ys []uint, flags ImPlotLineFlags, offset int, stride int) {
	_ = `ImPlot::PlotLine(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)),flags,offset,stride)`
}
