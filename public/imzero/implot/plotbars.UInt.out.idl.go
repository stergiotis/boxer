//go:build fffi_idl_code

package implot

func PlotBarsUInt(label_id string, values []uint) {
	_ = `ImPlot::PlotBars(label_id,values,getSliceLength(values))`
}

func PlotBarsUIntV(label_id string, values []uint, bar_size float64, shift float64, flags ImPlotBarsFlags, offset int, stride int) {
	_ = `ImPlot::PlotBars(label_id,values,getSliceLength(values),bar_size,shift,flags,offset,stride)`
}

func PlotBarsXYUInt(label_id string, xs []uint, ys []uint, bar_size float64) {
	_ = `ImPlot::PlotBars(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)),bar_size)`
}

func PlotBarsXYUIntV(label_id string, xs []uint, ys []uint, bar_size float64, flags ImPlotBarsFlags, offset int, stride int) {
	_ = `ImPlot::PlotBars(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)),bar_size,flags,offset,stride)`
}
