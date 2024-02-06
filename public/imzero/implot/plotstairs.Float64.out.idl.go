//go:build fffi_idl_code

package implot

func PlotStairsFloat64(label_id string, values []float64) {
	_ = `ImPlot::PlotStairs(label_id,values,getSliceLength(values))`
}

func PlotStairsFloat64V(label_id string, values []float64, xscale float64, xstart float64, flags ImPlotStairsFlags, offset int, stride int) {
	_ = `ImPlot::PlotStairs(label_id,values,getSliceLength(values),xscale,xstart,flags,offset,stride)`
}

func PlotStairsXYFloat64(label_id string, xs []float64, ys []float64) {
	_ = `ImPlot::PlotStairs(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotStairsXYFloat64V(label_id string, xs []float64, ys []float64, flags ImPlotStairsFlags, offset int, stride int) {
	_ = `ImPlot::PlotStairs(label_id,xs,ys,getSliceLength(xs),flags,offset,stride)`
}
