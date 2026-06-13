package imztop

import (
	"fmt"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/distsummary"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

// Line colors are sourced from imztop_theme.go (colorMetricPrimary,
// colorGridLine) and the IDS qualitative cycle (qualitativeColor) for
// per-core categorical assignment. Per ADR-0031 §SD7 the qualitative
// palette is the canonical surface for "many series, no magnitude
// ordering" use cases like per-core CPU lines.

func (inst *App) renderCPUPanel(snap *PublishedSnapshot) {
	inst.sectionHeader("CPU")
	if snap.LatestCPU == nil {
		c.Label("CPU collector unavailable").Send()
		return
	}
	cpuSnap := snap.LatestCPU

	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("%s · %d cores", cpuSnap.ModelName, cpuSnap.LogicalCores)).Send()
	}
	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("load %.2f / %.2f / %.2f", cpuSnap.LoadAvg1, cpuSnap.LoadAvg5, cpuSnap.LoadAvg15)).Send()
		c.AddSpace(inst.spaceOuter())
		c.Label(fmt.Sprintf("total %d%%", cpuSnap.TotalPercent)).Send()
		if cpuSnap.UsageWattsAvailable {
			c.AddSpace(inst.spaceOuter())
			c.Label(fmt.Sprintf("%.1f W", cpuSnap.UsageWatts)).Send()
		}
	}
	c.AddSpace(inst.spaceInner())
	inst.renderCPUDistsummaries(snap)

	c.AddSpace(inst.spaceInner())
	inst.renderCPUHeatmap(snap)

	c.AddSpace(inst.spaceTight())
	inst.renderCPUSparklines(snap)
}

// renderCPUDistsummaries surfaces two complementary keelson-style
// distribution summaries for the CPU panel:
//
//   - "cores"   — cross-core CPU% at the current instant (one sample
//     per logical CPU). Answers "are some cores hot while others
//     idle right now?" at a glance; the per-core sparkline grid below
//     answers the same question with far more pixels.
//   - "history" — temporal aggregate CPU% over the sampler's history
//     window (~600 samples at 1 Hz = 10 min). Answers "how busy has
//     the system been over the window?"
//
// Both digests are receiver-owned and rebuilt each frame via Reset +
// Push. 32 + ~600 samples is microseconds; reusing the digests across
// frames avoids a heap allocation under ImZero2's continuous-repaint
// loop while keeping distsummary itself stateless. Each distsummary
// carries an [inspector.Provenance] binding so the level-2 hover popup
// renders the standard ProvenanceChip identifying the source subject
// (ADR-0026 §SD3 app-event convention).
func (inst *App) renderCPUDistsummaries(snap *PublishedSnapshot) {
	cpuSnap := snap.LatestCPU
	if cpuSnap == nil {
		return
	}
	inst.cpuCoresDigest.Reset()
	for _, p := range cpuSnap.PerCorePercent {
		inst.cpuCoresDigest.Push(float64(p))
	}
	inst.cpuHistoryDigest.Reset()
	for _, v := range snap.HistoryCPUTotal {
		inst.cpuHistoryDigest.Push(v)
	}

	sampledAt := time.UnixMilli(snap.SampledAtUnixMs)
	inst.renderDistsummaryRow(
		inst.cpuCoresDigest,
		"cores", "cpu-distsum-cores", "app.imztop.event.cpu.percore.pct",
		sampledAt, formatPercent,
	)
	c.AddSpace(inst.spaceTight())
	inst.renderDistsummaryRow(
		inst.cpuHistoryDigest,
		"history", "cpu-distsum-history", "app.imztop.event.cpu.total.pct",
		sampledAt, formatPercent,
	)
}

// renderDistsummaryRow emits one labelled distsummary inside an
// already-populated digest. Caller is responsible for Reset+Push on
// the digest before this call — keeping the fill loop at the call
// site lets each panel choose the natural sample type (float64,
// uint8, uint64) without the helper growing a generic adapter
// argument. The digest, the inspector.Provenance subject, and a
// stable widget-id suffix are all caller-supplied so the helper has
// no per-panel knowledge.
func (inst *App) renderDistsummaryRow(
	digest *tdigest.TDigest,
	label, idSuffix, subject string,
	sampledAt time.Time,
	format distsummary.FormatFunc,
) {
	ds := distsummary.New("imztop-" + idSuffix).Tasks(inst.tasks).Format(format)
	for range c.Horizontal().KeepIter() {
		c.UiSetMinWidth(cpuDistsumLabelWidth)
		c.Label(label).Send()
		c.AddSpace(inst.spaceTight())
		ds.Provenance(inspector.Provenance{
			Subject:   subject,
			SourceApp: "imztop",
			SampledAt: sampledAt,
		}).Render(inst.ids.PrepareStr(idSuffix), digest, nil)
	}
}

// cpuDistsumLabelWidth keeps the "cores" / "history" labels in a
// fixed-width column so the chart-line glyph of each distsummary's
// level-1 line lines up vertically across rows — same trick the
// distsummary demo uses (UiSetMinWidth(160) there; 70 here is enough
// for these two short labels at the default IDS body font).
const cpuDistsumLabelWidth = 70

// formatPercent renders percent values in distsummary level-1
// summary lines. 'f' with 0 precision matches the existing inline
// "%d%%" / "busy %d%%" style elsewhere in imztop and keeps the
// 5-number line narrow enough to fit beside its sibling values.
// Used by every percent-valued distsummary in the app (CPU per-core,
// CPU history, disk per-device busy%, GPU per-device busy%).
func formatPercent(v float64) (out string) {
	out = strconv.FormatFloat(v, 'f', 0, 64) + "%"
	return
}

// renderPerDeviceDistsummary fills the supplied digest from samples
// and emits one labelled row. Used by Disk / GPU panels to summarise
// the cross-device utilization% distribution at the current instant
// — the non-CPU equivalent of renderCPUDistsummaries' "cores" row.
// samples is plain []float64 because each panel needs to convert
// from its own native type (uint8 busy%) at the call site; the
// helper stays unit-agnostic.
//
// Silently skips when n < 2: a single sample collapses the 5-number
// summary to one value, and n=0 has no distribution at all. This is
// the common case for laptops with one disk / one GPU; suppressing
// the degenerate row keeps the panel honest without a "(no data)"
// stub that would mislead the reader into thinking the column was
// supposed to render. CPU's two distsummaries don't go through this
// guard — cores always has ≥4 samples on real hardware and history
// always carries the full sliding window.
func (inst *App) renderPerDeviceDistsummary(
	digest *tdigest.TDigest,
	samples []float64,
	label, idSuffix, subject string,
	sampledAt time.Time,
	format distsummary.FormatFunc,
) {
	if len(samples) < 2 {
		return
	}
	digest.Reset()
	for _, v := range samples {
		digest.Push(v)
	}
	inst.renderDistsummaryRow(digest, label, idSuffix, subject, sampledAt, format)
}

const (
	cpuSparklineCols   = 4
	cpuSparklineHeight = 44
	cpuSparklineWidth  = 88
)

// renderCPUSparklines draws a grid of per-core history sparklines.
// Each cell shows a small line plot of that core's CPU% history;
// label colour reflects the most recent value's threshold band.
func (inst *App) renderCPUSparklines(snap *PublishedSnapshot) {
	cores := snap.HistoryCPUPerCore
	times := snap.HistoryTimeUnixSec
	if len(cores) == 0 || len(times) < 2 {
		return
	}
	for row0 := 0; row0 < len(cores); row0 += cpuSparklineCols {
		if row0 > 0 {
			c.AddSpace(inst.spaceInner())
		}
		for range c.Horizontal().KeepIter() {
			for k := 0; k < cpuSparklineCols && row0+k < len(cores); k++ {
				idx := row0 + k
				core := cores[idx]
				if len(core) != len(times) {
					continue
				}
				inst.renderOneCPUSparkline(idx, times, core)
			}
		}
	}
}

func (inst *App) renderOneCPUSparkline(idx int, times, core []float64) {
	for range c.IdScope(inst.ids.PrepareSeq(uint64(0x700 + idx))) {
		for range c.Vertical().KeepIter() {
			latest := float32(0)
			if n := len(core); n > 0 {
				latest = float32(core[n-1])
			}
			for rt := range c.RichTextLabelColored(thresholdColor(latest), colorBgClear, fmt.Sprintf("c%d %3.0f%%", idx, latest)) {
				_ = rt
			}
			c.PlotLine("c", times, core).
				Width(1.5).Color(qualitativeColor(idx)).Send()
			c.Plot(inst.ids.PrepareSeq(uint64(0x780+idx))).
				Width(cpuSparklineWidth).Height(cpuSparklineHeight).
				ShowAxes(false, false).
				ShowGrid(false, false).
				IncludeY(0).IncludeY(100).
				AllowZoom2(false, false).
				AllowDrag2(false, false).
				AllowScroll2(false, false).
				Send()
		}
	}
}
