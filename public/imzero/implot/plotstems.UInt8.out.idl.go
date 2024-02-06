//go:build fffi_idl_code

package implot

func PlotStemsUInt8[T ~uint8](label_id string, values []T) {
	_ = `ImPlot::PlotStems(label_id,values,(int)getSliceLength(values))`
}

func PlotStemsUInt8V[T ~uint8](label_id string, values []T, ref float64, scale float64, start float64, flags ImPlotStemsFlags, offset int, stride int) {
	_ = `ImPlot::PlotStems(label_id,values,(int)getSliceLength(values),ref,scale,start,flags,offset,stride)`
}

func PlotStemsXYUInt8[T ~uint8](label_id string, xs []T, ys []T) {
_ = `ImPlot::PlotStems(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotStemsXYUInt8V[T ~uint8](label_id string, xs []T, ys []T, ref float64, flags ImPlotStemsFlags, offset int, stride int) {
_ = `ImPlot::PlotStems(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)),ref,flags,offset,stride)`
}
