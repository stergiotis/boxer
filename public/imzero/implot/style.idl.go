//go:build fffi_idl_code

package implot

func loadStyle(ptr ImPlotStyleForeignPtr, bs []bool, fs []float32, vec2s []float32, cols []float32, markers []int, maps []ImPlotColormap) {
	_ = `
    auto s = (ImPlotStyle*)ptr;
	int i;
	
#define ASSIGN(l,r) ((l) = (r))
	i = 0;
    ASSIGN(s->UseLocalTime, bs[i++]);
    ASSIGN(s->UseISO8601, bs[i++]);
    ASSIGN(s->Use24HourClock, bs[i++]);

	i = 0;
	ASSIGN(s->LineWeight, fs[i++]);
	ASSIGN(s->MarkerSize, fs[i++]);
	ASSIGN(s->MarkerWeight, fs[i++]);
	ASSIGN(s->FillAlpha, fs[i++]);
	ASSIGN(s->ErrorBarSize, fs[i++]);
	ASSIGN(s->ErrorBarWeight, fs[i++]);
	ASSIGN(s->DigitalBitHeight, fs[i++]);
	ASSIGN(s->DigitalBitGap, fs[i++]);
	ASSIGN(s->PlotBorderSize, fs[i++]);
	ASSIGN(s->MinorAlpha, fs[i++]);
	
	i = 0;
	ASSIGN(s->MajorTickLen.x, vec2s[i++]);
	ASSIGN(s->MajorTickLen.y, vec2s[i++]);
	ASSIGN(s->MinorTickLen.x, vec2s[i++]);
	ASSIGN(s->MinorTickLen.y, vec2s[i++]);
	ASSIGN(s->MajorTickSize.x, vec2s[i++]);
	ASSIGN(s->MajorTickSize.y, vec2s[i++]);
	ASSIGN(s->MinorTickSize.x, vec2s[i++]);
	ASSIGN(s->MinorTickSize.y, vec2s[i++]);
	ASSIGN(s->MajorGridSize.x, vec2s[i++]);
	ASSIGN(s->MajorGridSize.y, vec2s[i++]);
	ASSIGN(s->MinorGridSize.x, vec2s[i++]);
	ASSIGN(s->MinorGridSize.y, vec2s[i++]);
	ASSIGN(s->PlotPadding.x, vec2s[i++]);
	ASSIGN(s->PlotPadding.y, vec2s[i++]);
	ASSIGN(s->LabelPadding.x, vec2s[i++]);
	ASSIGN(s->LabelPadding.y, vec2s[i++]);
	ASSIGN(s->LegendPadding.x, vec2s[i++]);
	ASSIGN(s->LegendPadding.y, vec2s[i++]);
	ASSIGN(s->LegendInnerPadding.x, vec2s[i++]);
	ASSIGN(s->LegendInnerPadding.y, vec2s[i++]);
	ASSIGN(s->LegendSpacing.x, vec2s[i++]);
	ASSIGN(s->LegendSpacing.y, vec2s[i++]);
	ASSIGN(s->MousePosPadding.x, vec2s[i++]);
	ASSIGN(s->MousePosPadding.y, vec2s[i++]);
	ASSIGN(s->AnnotationPadding.x, vec2s[i++]);
	ASSIGN(s->AnnotationPadding.y, vec2s[i++]);
	ASSIGN(s->FitPadding.x, vec2s[i++]);
	ASSIGN(s->FitPadding.y, vec2s[i++]);
	ASSIGN(s->PlotDefaultSize.x, vec2s[i++]);
	ASSIGN(s->PlotDefaultSize.y, vec2s[i++]);
	ASSIGN(s->PlotMinSize.x, vec2s[i++]);
	ASSIGN(s->PlotMinSize.y, vec2s[i++]);

	i = 0;
    for(i = 0;i<ImPlotCol_COUNT;i++) {
		 ASSIGN(s->Colors[i].x, cols[i*4+0]);
		 ASSIGN(s->Colors[i].y, cols[i*4+1]);
		 ASSIGN(s->Colors[i].z, cols[i*4+2]);
		 ASSIGN(s->Colors[i].w, cols[i*4+3]);
	}

	i = 0;
    ASSIGN(s->Colormap, maps[i++]);
#undef ASSIGN
`
	return
}
func GetStyle() (r ImPlotStyleForeignPtr) {
	_ = `r = (uintptr_t)&ImPlot::GetStyle()`
	return
}
func dumpStyle(ptr ImPlotStyleForeignPtr) (bs []bool, fs []float32, vec2s []float32, cols []float32, markers []int, maps []ImPlotColormap) {
	_ = `
	auto s = (ImPlotStyle*)ptr;
	size_t bs_len = 3;
    bs = (decltype(bs))arenaCalloc(bs_len,sizeof(*bs));
	size_t fs_len = 10;
    fs = (decltype(fs))arenaCalloc(fs_len,sizeof(*fs));
	size_t vec2s_len = 16*2;
    vec2s = (decltype(vec2s))arenaCalloc(vec2s_len,sizeof(*vec2s));
	size_t cols_len = 4*ImPlotCol_COUNT;
    cols = (decltype(cols))arenaCalloc(cols_len,sizeof(*cols));
	size_t markers_len = 2;
    markers = (decltype(markers))arenaCalloc(markers_len,sizeof(*markers));
	size_t maps_len = 1;
    maps = (decltype(maps))arenaCalloc(maps_len,sizeof(*maps));

	int i;
	
#define ASSIGN(l,r) ((r) = (l))
	i = 0;
    ASSIGN(s->UseLocalTime, bs[i++]);
    ASSIGN(s->UseISO8601, bs[i++]);
    ASSIGN(s->Use24HourClock, bs[i++]);

	i = 0;
	ASSIGN(s->LineWeight, fs[i++]);
	ASSIGN(s->MarkerSize, fs[i++]);
	ASSIGN(s->MarkerWeight, fs[i++]);
	ASSIGN(s->FillAlpha, fs[i++]);
	ASSIGN(s->ErrorBarSize, fs[i++]);
	ASSIGN(s->ErrorBarWeight, fs[i++]);
	ASSIGN(s->DigitalBitHeight, fs[i++]);
	ASSIGN(s->DigitalBitGap, fs[i++]);
	ASSIGN(s->PlotBorderSize, fs[i++]);
	ASSIGN(s->MinorAlpha, fs[i++]);
	
	i = 0;
	ASSIGN(s->MajorTickLen.x, vec2s[i++]);
	ASSIGN(s->MajorTickLen.y, vec2s[i++]);
	ASSIGN(s->MinorTickLen.x, vec2s[i++]);
	ASSIGN(s->MinorTickLen.y, vec2s[i++]);
	ASSIGN(s->MajorTickSize.x, vec2s[i++]);
	ASSIGN(s->MajorTickSize.y, vec2s[i++]);
	ASSIGN(s->MinorTickSize.x, vec2s[i++]);
	ASSIGN(s->MinorTickSize.y, vec2s[i++]);
	ASSIGN(s->MajorGridSize.x, vec2s[i++]);
	ASSIGN(s->MajorGridSize.y, vec2s[i++]);
	ASSIGN(s->MinorGridSize.x, vec2s[i++]);
	ASSIGN(s->MinorGridSize.y, vec2s[i++]);
	ASSIGN(s->PlotPadding.x, vec2s[i++]);
	ASSIGN(s->PlotPadding.y, vec2s[i++]);
	ASSIGN(s->LabelPadding.x, vec2s[i++]);
	ASSIGN(s->LabelPadding.y, vec2s[i++]);
	ASSIGN(s->LegendPadding.x, vec2s[i++]);
	ASSIGN(s->LegendPadding.y, vec2s[i++]);
	ASSIGN(s->LegendInnerPadding.x, vec2s[i++]);
	ASSIGN(s->LegendInnerPadding.y, vec2s[i++]);
	ASSIGN(s->LegendSpacing.x, vec2s[i++]);
	ASSIGN(s->LegendSpacing.y, vec2s[i++]);
	ASSIGN(s->MousePosPadding.x, vec2s[i++]);
	ASSIGN(s->MousePosPadding.y, vec2s[i++]);
	ASSIGN(s->AnnotationPadding.x, vec2s[i++]);
	ASSIGN(s->AnnotationPadding.y, vec2s[i++]);
	ASSIGN(s->FitPadding.x, vec2s[i++]);
	ASSIGN(s->FitPadding.y, vec2s[i++]);
	ASSIGN(s->PlotDefaultSize.x, vec2s[i++]);
	ASSIGN(s->PlotDefaultSize.y, vec2s[i++]);
	ASSIGN(s->PlotMinSize.x, vec2s[i++]);
	ASSIGN(s->PlotMinSize.y, vec2s[i++]);

	i = 0;
    for(i = 0;i<ImPlotCol_COUNT;i++) {
		 ASSIGN(s->Colors[i].x, cols[i*4+0]);
		 ASSIGN(s->Colors[i].y, cols[i*4+1]);
		 ASSIGN(s->Colors[i].z, cols[i*4+2]);
		 ASSIGN(s->Colors[i].w, cols[i*4+3]);
	}

	i = 0;
    ASSIGN(s->Colormap, maps[i++]);
#undef ASSIGN
`
	return
}
