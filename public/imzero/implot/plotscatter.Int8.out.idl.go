//go:build fffi_idl_code

package implot

func PlotScatterInt8(label_id string, values []int8) {
	_ = `ImPlot::PlotScatter(label_id,values,getSliceLength(values))`
}

func PlotScatterInt8V(label_id string, values []int8, xscale float64, xstart float64, flags ImPlotScatterFlags, offset int, stride int) {
	_ = `ImPlot::PlotScatter(label_id,values,getSliceLength(values),xscale,xstart,flags,offset,stride)`
}

func PlotScatterXYInt8(label_id string, xs []int8, ys []int8) {
	_ = `ImPlot::PlotScatter(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotScatterXYInt8V(label_id string, xs []int8, ys []int8, flags ImPlotScatterFlags, offset int, stride int) {
	_ = `ImPlot::PlotScatter(label_id,xs,ys,getSliceLength(xs),flags,offset,stride)`
}
