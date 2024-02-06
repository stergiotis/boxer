//go:build fffi_idl_code

package implot

// PlotHistogram2DInt8 Plots two dimensional, bivariate histogram as a heatmap. #x_bins and #y_bins can be a positive integer or an ImPlotBin. If #range is left unspecified, the min/max of
// #xs an #ys will be used as the ranges. Otherwise, outlier values outside of range are not binned. The largest bin count or density is returned.
func PlotHistogram2DInt8[T ~int8](label_id string, xs []T, ys []T) (r float64) {
	_ = `r = ImPlot::PlotHistogram2D(label_id,xs,ys,getSliceLength(xs))`
	return
}
// PlotHistogram2DV Plots two dimensional, bivariate histogram as a heatmap. #x_bins and #y_bins can be a positive integer or an ImPlotBin. If #range is left unspecified, the min/max of
// #xs an #ys will be used as the ranges. Otherwise, outlier values outside of range are not binned. The largest bin count or density is returned.
func PlotHistogram2DInt8V[T ~int8](label_id string, xs []T, ys []T, x_bins ImPlotBin, y_bins ImPlotBin, rangeP ImPlotRect, flags ImPlotHistogramFlags) (r float64) {
	_ = `r = ImPlot::PlotHistogram2D(label_id,xs,ys,(int)getSliceLength(xs),x_bins,y_bins,ImPlotRect(rangeP[0],rangeP[1],rangeP[2],rangeP[3]),flags)`
	return
}
