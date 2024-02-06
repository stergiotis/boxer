//go:build fffi_idl_code

package implot

func PlotStairsUInt32(label_id string, values []uint32) {
	_ = `ImPlot::PlotStairs(label_id,values,getSliceLength(values))`
}

func PlotStairsUInt32V(label_id string, values []uint32, xscale float64, xstart float64, flags ImPlotStairsFlags, offset int, stride int) {
	_ = `ImPlot::PlotStairs(label_id,values,getSliceLength(values),xscale,xstart,flags,offset,stride)`
}

func PlotStairsXYUInt32(label_id string, xs []uint32, ys []uint32) {
	_ = `ImPlot::PlotStairs(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotStairsXYUInt32V(label_id string, xs []uint32, ys []uint32, flags ImPlotStairsFlags, offset int, stride int) {
	_ = `ImPlot::PlotStairs(label_id,xs,ys,getSliceLength(xs),flags,offset,stride)`
}
