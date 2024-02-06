//go:build fffi_idl_code

package implot

func PlotScatterUInt32(label_id string, values []uint32) {
	_ = `ImPlot::PlotScatter(label_id,values,getSliceLength(values))`
}

func PlotScatterUInt32V(label_id string, values []uint32, xscale float64, xstart float64, flags ImPlotScatterFlags, offset int, stride int) {
	_ = `ImPlot::PlotScatter(label_id,values,getSliceLength(values),xscale,xstart,flags,offset,stride)`
}

func PlotScatterXYUInt32(label_id string, xs []uint32, ys []uint32) {
	_ = `ImPlot::PlotScatter(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotScatterXYUInt32V(label_id string, xs []uint32, ys []uint32, flags ImPlotScatterFlags, offset int, stride int) {
	_ = `ImPlot::PlotScatter(label_id,xs,ys,getSliceLength(xs),flags,offset,stride)`
}
