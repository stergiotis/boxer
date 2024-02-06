//go:build fffi_idl_code

package implot

func PlotBarsInt32(label_id string, values []int32) {
	_ = `ImPlot::PlotBars(label_id,values,getSliceLength(values))`
}

func PlotBarsInt32V(label_id string, values []int32, bar_size float64, shift float64, flags ImPlotBarsFlags, offset int, stride int) {
	_ = `ImPlot::PlotBars(label_id,values,getSliceLength(values),bar_size,shift,flags,offset,stride)`
}

func PlotBarsXYInt32(label_id string, xs []int32, ys []int32, bar_size float64) {
	_ = `ImPlot::PlotBars(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)),bar_size)`
}

func PlotBarsXYInt32V(label_id string, xs []int32, ys []int32, bar_size float64, flags ImPlotBarsFlags, offset int, stride int) {
	_ = `ImPlot::PlotBars(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)),bar_size,flags,offset,stride)`
}
