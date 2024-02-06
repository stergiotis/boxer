//go:build fffi_idl_code

package implot

func PlotStairsInt8(label_id string, values []int8) {
	_ = `ImPlot::PlotStairs(label_id,values,getSliceLength(values))`
}

func PlotStairsInt8V(label_id string, values []int8, xscale float64, xstart float64, flags ImPlotStairsFlags, offset int, stride int) {
	_ = `ImPlot::PlotStairs(label_id,values,getSliceLength(values),xscale,xstart,flags,offset,stride)`
}

func PlotStairsXYInt8(label_id string, xs []int8, ys []int8) {
	_ = `ImPlot::PlotStairs(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotStairsXYInt8V(label_id string, xs []int8, ys []int8, flags ImPlotStairsFlags, offset int, stride int) {
	_ = `ImPlot::PlotStairs(label_id,xs,ys,getSliceLength(xs),flags,offset,stride)`
}
