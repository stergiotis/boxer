//go:build fffi_idl_code

package implot

func PlotStemsInt8[T ~int8](label_id string, values []T) {
	_ = `ImPlot::PlotStems(label_id,values,(int)getSliceLength(values))`
}

func PlotStemsInt8V[T ~int8](label_id string, values []T, ref float64, scale float64, start float64, flags ImPlotStemsFlags, offset int, stride int) {
	_ = `ImPlot::PlotStems(label_id,values,(int)getSliceLength(values),ref,scale,start,flags,offset,stride)`
}

func PlotStemsXYInt8[T ~int8](label_id string, xs []T, ys []T) {
_ = `ImPlot::PlotStems(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotStemsXYInt8V[T ~int8](label_id string, xs []T, ys []T, ref float64, flags ImPlotStemsFlags, offset int, stride int) {
_ = `ImPlot::PlotStems(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)),ref,flags,offset,stride)`
}
