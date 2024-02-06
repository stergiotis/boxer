//go:build fffi_idl_code

package implot

func PlotShadedFloat64(label_id string, values []float64) {
	_ = `ImPlot::PlotShaded(label_id,values,(int)getSliceLength(values))`
}

func PlotShadedFloat64V(label_id string, values []float64, yref float64, xscale float64, xstart float64, flags ImPlotShadedFlags, offset int, stride int) {
	_ = `ImPlot::PlotShaded(label_id,values,(int)getSliceLength(values),yref,xscale,xstart,flags,offset,stride)`
}

func PlotShadedXYFloat64(label_id string, xs []float64, ys []float64) {
	_ = `ImPlot::PlotShaded(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotShadedXYFloat64V(label_id string, xs []float64, ys []float64, yref float64, flags ImPlotShadedFlags, offset int, stride int) {
	_ = `ImPlot::PlotShaded(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)),yref,flags,offset,stride)`
}

func PlotShadedXY1Y2Float64(label_id string, xs []float64, y1s []float64, y2s []float64) {
	_ = `ImPlot::PlotShaded(label_id,xs,y1s,y2s,(int)std::min(std::min(getSliceLength(xs),getSliceLength(y1s)),getSliceLength(y2s)))`
}

func PlotShadedXY1Y2Float64V(label_id string, xs []float64, y1s []float64, y2s []float64, flags ImPlotShadedFlags, offset int, stride int) {
	_ = `ImPlot::PlotShaded(label_id,xs,y1s,y2s,(int)std::min(std::min(getSliceLength(xs),getSliceLength(y1s)),getSliceLength(y2s)),flags,offset,stride)`
}
