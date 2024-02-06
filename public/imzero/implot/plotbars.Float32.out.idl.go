//go:build fffi_idl_code

package implot

func PlotBarsFloat32(label_id string, values []float32) {
	_ = `ImPlot::PlotBars(label_id,values,getSliceLength(values))`
}

func PlotBarsFloat32V(label_id string, values []float32, bar_size float64, shift float64, flags ImPlotBarsFlags, offset int, stride int) {
	_ = `ImPlot::PlotBars(label_id,values,getSliceLength(values),bar_size,shift,flags,offset,stride)`
}

func PlotBarsXYFloat32(label_id string, xs []float32, ys []float32, bar_size float64) {
	_ = `ImPlot::PlotBars(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)),bar_size)`
}

func PlotBarsXYFloat32V(label_id string, xs []float32, ys []float32, bar_size float64, flags ImPlotBarsFlags, offset int, stride int) {
	_ = `ImPlot::PlotBars(label_id,xs,ys,std::min(getSliceLength(xs),getSliceLength(ys)),bar_size,flags,offset,stride)`
}
