//go:build fffi_idl_code

package implot

func PlotShadedInt32(label_id string, values []int32) {
	_ = `ImPlot::PlotShaded(label_id,values,(int)getSliceLength(values))`
}

func PlotShadedInt32V(label_id string, values []int32, yref float64, xscale float64, xstart float64, flags ImPlotShadedFlags, offset int, stride int) {
	_ = `ImPlot::PlotShaded(label_id,values,(int)getSliceLength(values),yref,xscale,xstart,flags,offset,stride)`
}

func PlotShadedXYInt32(label_id string, xs []int32, ys []int32) {
	_ = `ImPlot::PlotShaded(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotShadedXYInt32V(label_id string, xs []int32, ys []int32, yref float64, flags ImPlotShadedFlags, offset int, stride int) {
	_ = `ImPlot::PlotShaded(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)),yref,flags,offset,stride)`
}

func PlotShadedXY1Y2Int32(label_id string, xs []int32, y1s []int32, y2s []int32) {
	_ = `ImPlot::PlotShaded(label_id,xs,y1s,y2s,(int)std::min(std::min(getSliceLength(xs),getSliceLength(y1s)),getSliceLength(y2s)))`
}

func PlotShadedXY1Y2Int32V(label_id string, xs []int32, y1s []int32, y2s []int32, flags ImPlotShadedFlags, offset int, stride int) {
	_ = `ImPlot::PlotShaded(label_id,xs,y1s,y2s,(int)std::min(std::min(getSliceLength(xs),getSliceLength(y1s)),getSliceLength(y2s)),flags,offset,stride)`
}
