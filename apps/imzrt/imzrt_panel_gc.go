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
		for rt := range c.RichTextLabelColored(latencyThresholdColor(snap.PauseP99Sec), colorBgClear, fmt.Sprintf("· p99 %s", humanDuration(snap.PauseP99Sec))) {
			rt.Strong()
		}
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
		p := inst.beginTimePlot("##gc-pauses", 180, "ms", t)
		inst.lineSmoothed(p, "max", t, snap.HistPauseMaxMs, colorHot, colorHotFaint, 1.0)
		inst.lineSmoothed(p, "p99", t, snap.HistPauseP99Ms, colorWarn, colorWarnFaint, 1.5)
		inst.lineSmoothed(p, "p50", t, snap.HistPauseP50Ms, colorMetricPrimary, colorMetricPrimaryFaint, 2.0)
		p.End()
	}

	// GC cycle rate, total vs forced.
	if len(snap.HistGCPerSec) == len(t) {
		c.AddSpace(inst.spaceTight())
		inst.sectionHeader("GC rate")
		p := inst.beginTimePlot("##gc-rate", 140, "1/s", t)
		inst.lineSmoothed(p, "cycles/s", t, snap.HistGCPerSec, colorMetricPrimary, colorMetricPrimaryFaint, 2.0)
		if len(snap.HistGCForcedPerSec) == len(t) {
			// Deliberately raw: forced GCs are discrete events, and a low-pass
			// smears their spike train into a misleading blur (imzrt_smooth.go).
			p.SetNextColor(colorHot.Literal()).SetNextWeight(1.0)
			p.Line("forced/s", t, snap.HistGCForcedPerSec)
		}
		p.End()
	}

	// Allocation rate — the pressure driving GC pacing.
	if len(snap.HistAllocMiBs) == len(t) {
		c.AddSpace(inst.spaceTight())
		inst.sectionHeader("Allocation rate")
		p := inst.beginTimePlot("##gc-alloc", 140, "MiB/s", t)
		inst.lineSmoothed(p, "MiB/s", t, snap.HistAllocMiBs, colorMetricPrimary, colorMetricPrimaryFaint, 2.0)
		p.End()
	}
}
