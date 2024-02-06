//go:build fffi_idl_code

package imgui

func PlotFlameV(label string, starts []float32, stops []float32, levels []uint8, captions []string, overlayText string, scaleMin float32, scaleMax float32, size ImVec2) {
	_ = `
    auto n = std::min(std::min(std::min(getSliceLength(starts),getSliceLength(stops)),getSliceLength(levels)),getSliceLength(captions));
    flameGraphData d;
    d.starts = starts;
    d.ends = stops;
    d.levels = levels;
    d.captions = (const char**)captions;
	ImGuiWidgetFlameGraph::PlotFlame(label,flameGraphValueGetter,&d,(int)n,0,overlayText,scaleMin,scaleMax,size);
`
}
