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
	p := inst.beginTimePlot("##"+spec.plotID, 168, "MiB/s", times,
		0, rateUpperBound(spec.primarySum, spec.secondarySum))
	for i, s := range spec.primaryByDev {
		if len(s.Y) != len(times) {
			continue
		}
		p.SetNextColor(markerColor(i).Literal()).SetNextWeight(1.2)
		p.Line(fmt.Sprintf("%s %s", s.Name, spec.primaryDevLabel), times, s.Y)
	}
	for i, s := range spec.secondaryByDev {
		if len(s.Y) != len(times) {
			continue
		}
		p.SetNextColor(markerColor(i).Literal()).SetNextWeight(1.2)
		p.Line(fmt.Sprintf("%s %s", s.Name, spec.secondaryDevLabel), times, s.Y)
	}
	if len(spec.primarySum) == len(times) {
		p.SetNextColor(markerColor(0).Literal()).SetNextWeight(2.4)
		p.Line(spec.primarySumLabel, times, spec.primarySum)
	}
	if len(spec.secondarySum) == len(times) {
		p.SetNextColor(markerColor(1).Literal()).SetNextWeight(2.4)
		p.Line(spec.secondarySumLabel, times, spec.secondarySum)
	}
	p.End()
}
