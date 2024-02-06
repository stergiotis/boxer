//go:build fffi_idl_code

package implot

func PlotScatterInt16(label_id string, values []int16) {
	_ = `ImPlot::PlotScatter(label_id,values,getSliceLength(values))`
}

func PlotScatterInt16V(label_id string, values []int16, xscale float64, xstart float64, flags ImPlotScatterFlags, offset int, stride int) {
	_ = `ImPlot::PlotScatter(label_id,values,getSliceLength(values),xscale,xstart,flags,offset,stride)`
}

func PlotScatterXYInt16(label_id string, xs []int16, ys []int16) {
	_ = `ImPlot::PlotScatter(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotScatterXYInt16V(label_id string, xs []int16, ys []int16, flags ImPlotScatterFlags, offset int, stride int) {
	_ = `ImPlot::PlotScatter(label_id,xs,ys,getSliceLength(xs),flags,offset,stride)`
}
