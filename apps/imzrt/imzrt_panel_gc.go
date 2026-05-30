package imzrt

import (
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

func (inst *App) renderGCPanel(snap *PublishedSnapshot) {
	inst.sectionHeader("GC")

	// Instant readouts.
	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("cycles %s", humanCount(snap.GCCyclesTotal))).Send()
		c.Label(fmt.Sprintf("· forced %s", humanCount(snap.GCCyclesForced))).Send()
		c.Label(fmt.Sprintf("· %.2f/s", snap.GCPerSec)).Send()
	}
	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("pause p50 %s", humanDuration(snap.PauseP50Sec))).Send()
		c.Label(fmt.Sprintf("· p99 %s", humanDuration(snap.PauseP99Sec))).Send()
		c.Label(fmt.Sprintf("· max %s", humanDuration(snap.PauseMaxSec))).Send()
		c.Label(fmt.Sprintf("· %d in window", snap.PausesInWindow)).Send()
	}
	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("alloc %s/s", humanBytes(uint64(snap.AllocRateBytesPerSec)))).Send()
		c.Label(fmt.Sprintf("· %s obj/s", humanCount(uint64(snap.AllocObjPerSec)))).Send()
	}

	t := snap.HistTimeUnixSec
	if len(t) < 2 {
		return
	}

	// Rolling GC pause percentiles — the windowed-delta distribution (ADR-0061
	// Q1/O1) plotted over time. Quantiles come straight off the per-interval
	// histogram; routing them through distsummary would need the sample synthesis
	// O1 was chosen to avoid, so the percentile lines are the faithful view.
	if len(snap.HistPauseP99Ms) == len(t) {
		c.AddSpace(inst.spaceTight())
		inst.sectionHeader("GC pause percentiles")
		c.PlotLine("max", t, snap.HistPauseMaxMs).Width(1.0).Color(colorHot).Send()
		c.PlotLine("p99", t, snap.HistPauseP99Ms).Width(1.5).Color(colorWarn).Send()
		c.PlotLine("p50", t, snap.HistPauseP50Ms).Width(2.0).Color(colorMetricPrimary).Send()
		c.Plot(inst.ids.PrepareStr("gc-pauses-plot")).
			Height(180).
			YAxisLabel("ms").
			Legend().
			IncludeY(0).
			AllowZoom2(true, false).
			AllowDrag2(true, false).
			AllowScroll2(true, false).
			Send()
	}

	// GC cycle rate, total vs forced.
	if len(snap.HistGCPerSec) == len(t) {
		c.AddSpace(inst.spaceTight())
		inst.sectionHeader("GC rate")
		c.PlotLine("cycles/s", t, snap.HistGCPerSec).Width(2.0).Color(colorMetricPrimary).Send()
		if len(snap.HistGCForcedPerSec) == len(t) {
			c.PlotLine("forced/s", t, snap.HistGCForcedPerSec).Width(1.0).Color(colorHot).Send()
		}
		c.Plot(inst.ids.PrepareStr("gc-rate-plot")).
			Height(140).
			YAxisLabel("1/s").
			Legend().
			IncludeY(0).
			AllowZoom2(true, false).
			AllowDrag2(true, false).
			AllowScroll2(true, false).
			Send()
	}

	// Allocation rate — the pressure driving GC pacing.
	if len(snap.HistAllocMiBs) == len(t) {
		c.AddSpace(inst.spaceTight())
		inst.sectionHeader("Allocation rate")
		c.PlotLine("MiB/s", t, snap.HistAllocMiBs).Width(2.0).Color(colorMetricPrimary).Send()
		c.Plot(inst.ids.PrepareStr("gc-alloc-plot")).
			Height(140).
			YAxisLabel("MiB/s").
			Legend().
			IncludeY(0).
			AllowZoom2(true, false).
			AllowDrag2(true, false).
			AllowScroll2(true, false).
			Send()
	}
}
