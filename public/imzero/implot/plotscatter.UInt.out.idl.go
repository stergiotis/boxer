//go:build fffi_idl_code

package implot

func PlotScatterUInt(label_id string, values []uint) {
	_ = `ImPlot::PlotScatter(label_id,values,getSliceLength(values))`
}

func PlotScatterUIntV(label_id string, values []uint, xscale float64, xstart float64, flags ImPlotScatterFlags, offset int, stride int) {
	_ = `ImPlot::PlotScatter(label_id,values,getSliceLength(values),xscale,xstart,flags,offset,stride)`
}

func PlotScatterXYUInt(label_id string, xs []uint, ys []uint) {
	_ = `ImPlot::PlotScatter(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotScatterXYUIntV(label_id string, xs []uint, ys []uint, flags ImPlotScatterFlags, offset int, stride int) {
	_ = `ImPlot::PlotScatter(label_id,xs,ys,getSliceLength(xs),flags,offset,stride)`
}
