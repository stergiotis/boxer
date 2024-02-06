//go:build fffi_idl_code

package implot

func PlotShadedUInt32(label_id string, values []uint32) {
	_ = `ImPlot::PlotShaded(label_id,values,(int)getSliceLength(values))`
}

func PlotShadedUInt32V(label_id string, values []uint32, yref float64, xscale float64, xstart float64, flags ImPlotShadedFlags, offset int, stride int) {
	_ = `ImPlot::PlotShaded(label_id,values,(int)getSliceLength(values),yref,xscale,xstart,flags,offset,stride)`
}

func PlotShadedXYUInt32(label_id string, xs []uint32, ys []uint32) {
	_ = `ImPlot::PlotShaded(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotShadedXYUInt32V(label_id string, xs []uint32, ys []uint32, yref float64, flags ImPlotShadedFlags, offset int, stride int) {
	_ = `ImPlot::PlotShaded(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)),yref,flags,offset,stride)`
}

func PlotShadedXY1Y2UInt32(label_id string, xs []uint32, y1s []uint32, y2s []uint32) {
_ = `ImPlot::PlotShaded(label_id,xs,y1s,y2s,(int)std::min(std::min(getSliceLength(xs),getSliceLength(y1s)),getSliceLength(y2s)))`
}

func PlotShadedXY1Y2UInt32V(label_id string, xs []uint32, y1s []uint32, y2s []uint32, flags ImPlotShadedFlags, offset int, stride int) {
_ = `ImPlot::PlotShaded(label_id,xs,y1s,y2s,(int)std::min(std::min(getSliceLength(xs),getSliceLength(y1s)),getSliceLength(y2s)),flags,offset,stride)`
}