//go:build fffi_idl_code

package implot

func PlotStairsUInt(label_id string, values []uint) {
	_ = `ImPlot::PlotStairs(label_id,values,getSliceLength(values))`
}

func PlotStairsUIntV(label_id string, values []uint, xscale float64, xstart float64, flags ImPlotStairsFlags, offset int, stride int) {
	_ = `ImPlot::PlotStairs(label_id,values,getSliceLength(values),xscale,xstart,flags,offset,stride)`
}

func PlotStairsXYUInt(label_id string, xs []uint, ys []uint) {
	_ = `ImPlot::PlotStairs(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotStairsXYUIntV(label_id string, xs []uint, ys []uint, flags ImPlotStairsFlags, offset int, stride int) {
	_ = `ImPlot::PlotStairs(label_id,xs,ys,getSliceLength(xs),flags,offset,stride)`
}
