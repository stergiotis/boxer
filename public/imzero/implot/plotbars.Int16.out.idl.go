//go:build fffi_idl_code

package implot

func PlotBarsInt16(label_id string, values []int16) {
	_ = `ImPlot::PlotBars(label_id,values,getSliceLength(values))`
}

func PlotBarsInt16V(label_id string, values []int16, bar_size float64, shift float64, flags ImPlotBarsFlags, offset int, stride int) {
	_ = `ImPlot::PlotBars(label_id,values,getSliceLength(values),bar_size,shift,flags,offset,stride)`
}

func PlotBarsXYInt16(label_id string, xs []int16, ys []int16, bar_size float64) {
	_ = `ImPlot::PlotBars(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)),bar_size)`
}

func PlotBarsXYInt16V(label_id string, xs []int16, ys []int16, bar_size float64, flags ImPlotBarsFlags, offset int, stride int) {
	_ = `ImPlot::PlotBars(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)),bar_size,flags,offset,stride)`
}
