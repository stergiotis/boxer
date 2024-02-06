//go:build fffi_idl_code

package implot

func PlotShadedFloat32(label_id string, values []float32) {
	_ = `ImPlot::PlotShaded(label_id,values,(int)getSliceLength(values))`
}

func PlotShadedFloat32V(label_id string, values []float32, yref float64, xscale float64, xstart float64, flags ImPlotShadedFlags, offset int, stride int) {
	_ = `ImPlot::PlotShaded(label_id,values,(int)getSliceLength(values),yref,xscale,xstart,flags,offset,stride)`
}

func PlotShadedXYFloat32(label_id string, xs []float32, ys []float32) {
	_ = `ImPlot::PlotShaded(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotShadedXYFloat32V(label_id string, xs []float32, ys []float32, yref float64, flags ImPlotShadedFlags, offset int, stride int) {
	_ = `ImPlot::PlotShaded(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)),yref,flags,offset,stride)`
}

func PlotShadedXY1Y2Float32(label_id string, xs []float32, y1s []float32, y2s []float32) {
_ = `ImPlot::PlotShaded(label_id,xs,y1s,y2s,(int)std::min(std::min(getSliceLength(xs),getSliceLength(y1s)),getSliceLength(y2s)))`
}

func PlotShadedXY1Y2Float32V(label_id string, xs []float32, y1s []float32, y2s []float32, flags ImPlotShadedFlags, offset int, stride int) {
_ = `ImPlot::PlotShaded(label_id,xs,y1s,y2s,(int)std::min(std::min(getSliceLength(xs),getSliceLength(y1s)),getSliceLength(y2s)),flags,offset,stride)`
}