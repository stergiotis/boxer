//go:build fffi_idl_code

package implot

func PlotErrorBarsUInt[T ~uint](label_id string, xs []T, ys []T, errs []T) {
	_ = `ImPlot::PlotErrorBars(label_id,xs,ys,errs,std::min(getSliceLength(xs),getSliceLength(ys)))`
}

func PlotErrorBarsUIntV[T ~uint](label_id string, xs []T, ys []T, errs []T, flags ImPlotErrorBarsFlags, offset int, stride int) {
	_ = `ImPlot::PlotErrorBars(label_id,xs,ys,errs,std::min(std::min(getSliceLength(xs),getSliceLength(ys)),getSliceLength(errs)),flags,offset,stride)`
}

func PlotErrorBarsPosNegUInt[T ~uint](label_id string, xs []T, ys []T, neg []T, pos []T) {
_ = `ImPlot::PlotErrorBars(label_id,xs,ys,neg,pos,std::min(std::min(std::min(getSliceLength(xs),getSliceLength(ys)),getSliceLength(neg)),getSliceLength(pos)))`
}

func PlotErrorBarsPosNegUIntV[T ~uint](label_id string, xs []T, ys []T, neg []T, pos []T, flags ImPlotErrorBarsFlags, offset int, stride int) {
_ = `ImPlot::PlotErrorBars(label_id,xs,ys,neg,pos,std::min(std::min(std::min(getSliceLength(xs),getSliceLength(ys)),getSliceLength(neg)),getSliceLength(pos)),flags,offset,stride)`
}
