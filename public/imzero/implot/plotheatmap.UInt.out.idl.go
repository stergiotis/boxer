//go:build fffi_idl_code

package implot

func PlotHeatmapUInt[T ~uint](label_id string, values []T, rows int) {
	_ = `ImPlot::PlotHeatmap(label_id,values,rows,((int)getSliceLength(values))/rows)`
}

func PlotHeatmapUIntV[T ~uint](label_id string, values []T, rows int, scale_min float64, scale_max float64, label_fmt string, bounds_min ImPlotPoint, bounds_max ImPlotPoint, flags ImPlotHeatmapFlags) {
	_ = `ImPlot::PlotHeatmap(label_id,values,rows,((int)getSliceLength(values))/rows,scale_min,scale_max,label_fmt,bounds_min,bounds_max,flags)`
}
