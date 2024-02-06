//go:build fffi_idl_code

package implot

func PlotShadedUInt8(label_id string, values []uint8) {
	_ = `ImPlot::PlotShaded(label_id,values,(int)getSliceLength(values))`
}

func PlotShadedUInt8V(label_id string, values []uint8, yref float64, xscale float64, xstart float64, flags ImPlotShadedFlags, offset int, stride int) {
	_ = `ImPlot::PlotShaded(label_id,values,(int)getSliceLength(values),yref,xscale,xstart,flags,offset,stride)`
}

func PlotShadedXYUInt8(label_id string, xs []uint8, ys []uint8) {
	_ = `ImPlot::PlotShaded(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotShadedXYUInt8V(label_id string, xs []uint8, ys []uint8, yref float64, flags ImPlotShadedFlags, offset int, stride int) {
	_ = `ImPlot::PlotShaded(label_id,xs,ys,(int)std::min(getSliceLength(xs),getSliceLength(ys)),yref,flags,offset,stride)`
}

func PlotShadedXY1Y2UInt8(label_id string, xs []uint8, y1s []uint8, y2s []uint8) {
	_ = `ImPlot::PlotShaded(label_id,xs,y1s,y2s,(int)std::min(std::min(getSliceLength(xs),getSliceLength(y1s)),getSliceLength(y2s)))`
}

func PlotShadedXY1Y2UInt8V(label_id string, xs []uint8, y1s []uint8, y2s []uint8, flags ImPlotShadedFlags, offset int, stride int) {
	_ = `ImPlot::PlotShaded(label_id,xs,y1s,y2s,(int)std::min(std::min(getSliceLength(xs),getSliceLength(y1s)),getSliceLength(y2s)),flags,offset,stride)`
}
