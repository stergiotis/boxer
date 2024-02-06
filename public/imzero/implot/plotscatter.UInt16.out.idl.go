//go:build fffi_idl_code

package implot

func PlotScatterUInt16(label_id string, values []uint16) {
	_ = `ImPlot::PlotScatter(label_id,values,getSliceLength(values))`
}

func PlotScatterUInt16V(label_id string, values []uint16, xscale float64, xstart float64, flags ImPlotScatterFlags, offset int, stride int) {
	_ = `ImPlot::PlotScatter(label_id,values,getSliceLength(values),xscale,xstart,flags,offset,stride)`
}

func PlotScatterXYUInt16(label_id string, xs []uint16, ys []uint16) {
	_ = `ImPlot::PlotScatter(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotScatterXYUInt16V(label_id string, xs []uint16, ys []uint16, flags ImPlotScatterFlags, offset int, stride int) {
	_ = `ImPlot::PlotScatter(label_id,xs,ys,getSliceLength(xs),flags,offset,stride)`
}
