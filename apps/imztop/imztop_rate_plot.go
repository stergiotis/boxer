package imztop

import (
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// ratePlotSpec parameterises the shared rate-history plot used by the disk and
// network panels: two per-device series sets (primary/secondary — read/write or
// rx/tx) plus their aggregate Σ sums, all MiB/s over the shared time axis.
type ratePlotSpec struct {
	plotID                             string
	primaryByDev, secondaryByDev       []NamedSeries
	primaryDevLabel, secondaryDevLabel string // suffix after the device name, e.g. "R"/"W" or "rx"/"tx"
	primarySum, secondarySum           []float64
	primarySumLabel, secondarySumLabel string // full legend label, e.g. "Σ read"/"Σ rx"
}

// renderRateHistoryPlot draws spec as one MiB/s line plot. A separator and
// outer-padding gap precede it so the Y-axis labels (which start at the plot
// rect's top edge) don't read as attached to the list above. Each device
// contributes one thin line per series set (the secondary set drawn without
// highlight); the two aggregate Σ series are thick lines, and Talbot Y-ticks
// are scaled to the sums. Callers gate on len(times) >= 2.
func (inst *App) renderRateHistoryPlot(times []float64, spec ratePlotSpec) {
	c.AddSpace(inst.spaceInner())
	c.Separator().Horizontal().Send()
	c.AddSpace(inst.spaceOuter())
	for i, s := range spec.primaryByDev {
		if len(s.Y) != len(times) {
			continue
		}
		c.PlotLine(fmt.Sprintf("%s %s", s.Name, spec.primaryDevLabel), times, s.Y).
			Width(1.2).Color(markerColor(i)).Send()
	}
	for i, s := range spec.secondaryByDev {
		if len(s.Y) != len(times) {
			continue
		}
		c.PlotLine(fmt.Sprintf("%s %s", s.Name, spec.secondaryDevLabel), times, s.Y).
			Width(1.2).Color(markerColor(i)).Highlight(false).Send()
	}
	if len(spec.primarySum) == len(times) {
		c.PlotLine(spec.primarySumLabel, times, spec.primarySum).
			Width(2.4).Color(markerColor(0)).Send()
	}
	if len(spec.secondarySum) == len(times) {
		c.PlotLine(spec.secondarySumLabel, times, spec.secondarySum).
			Width(2.4).Color(markerColor(1)).Send()
	}
	plot := c.Plot(inst.ids.PrepareStr(spec.plotID)).
		Height(168).
		YAxisLabel("MiB/s").
		Legend().
		IncludeY(0).
		AllowZoom2(true, false).
		AllowDrag2(true, false).
		AllowScroll2(true, false)
	plot = applyYTalbotTicks(plot, 0, rateUpperBound(spec.primarySum, spec.secondarySum), 5)
	plot.Send()
}
