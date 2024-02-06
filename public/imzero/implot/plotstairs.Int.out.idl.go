//go:build fffi_idl_code

package implot

func PlotStairsInt(label_id string, values []int) {
	_ = `ImPlot::PlotStairs(label_id,values,getSliceLength(values))`
}

func PlotStairsIntV(label_id string, values []int, xscale float64, xstart float64, flags ImPlotStairsFlags, offset int, stride int) {
	_ = `ImPlot::PlotStairs(label_id,values,getSliceLength(values),xscale,xstart,flags,offset,stride)`
}

func PlotStairsXYInt(label_id string, xs []int, ys []int) {
	_ = `ImPlot::PlotStairs(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotStairsXYIntV(label_id string, xs []int, ys []int, flags ImPlotStairsFlags, offset int, stride int) {
	_ = `ImPlot::PlotStairs(label_id,xs,ys,getSliceLength(xs),flags,offset,stride)`
}
