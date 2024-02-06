//go:build fffi_idl_code

package implot

func PlotStemsUInt32[T ~uint32](label_id string, values []T) {
	_ = `ImPlot::PlotStems(label_id,values,(int)getSliceLength(values))`
}

func PlotStemsUInt32V[T ~uint32](label_id string, values []T, ref float64, scale float64, start float64, flags ImPlotStemsFlags, offset int, stride int) {
	_ = `ImPlot::PlotStems(label_id,values,(int)getSliceLength(values),ref,scale,start,flags,offset,stride)`
}

func PlotStemsXYUInt32[T ~uint32](label_id string, xs []T, ys []T) {
_ = `ImPlot::PlotStems(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotStemsXYUInt32V[T ~uint32](label_id string, xs []T, ys []T, ref float64, flags ImPlotStemsFlags, offset int, stride int) {
_ = `ImPlot::PlotStems(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)),ref,flags,offset,stride)`
}
