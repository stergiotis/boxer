package imzrt

import (
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

func (inst *App) renderHeapPanel(snap *PublishedSnapshot) {
	inst.sectionHeader("Heap")

	// Instant readouts.
	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("live %s", humanBytes(snap.HeapLiveBytes))).Send()
		c.Label(fmt.Sprintf("· objects %s", humanBytes(snap.HeapObjectsBytes))).Send()
		c.Label(fmt.Sprintf("· goal %s", humanBytes(snap.HeapGoalBytes))).Send()
		c.Label(fmt.Sprintf("· mapped %s", humanBytes(snap.TotalMappedBytes))).Send()
	}
	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("released %s", humanBytes(snap.ReleasedBytes))).Send()
		c.Label(fmt.Sprintf("· objects #%s", humanCount(snap.HeapObjectsCount))).Send()
		c.Label(fmt.Sprintf("· alloc %s/s", humanBytes(uint64(snap.AllocRateBytesPerSec)))).Send()
		c.Label(fmt.Sprintf("· GC %s (%s forced) @ %.2f/s",
			humanCount(snap.GCCyclesTotal), humanCount(snap.GCCyclesForced), snap.GCPerSec)).Send()
	}

	// Headroom-to-GOMEMLIMIT gauge — tinted good→hot as mapped memory nears the limit.
	c.AddSpace(inst.spaceInner())
	if snap.MemLimitSet() {
		var frac float32
		if snap.GOMemLimitBytes > 0 {
			frac = float32(float64(snap.TotalMappedBytes) / float64(snap.GOMemLimitBytes))
		}
		if frac > 1 {
			frac = 1
		}
		c.ProgressBar(frac).
			Text(fmt.Sprintf("mapped %s / GOMEMLIMIT %s (%.1f%%)",
				humanBytes(snap.TotalMappedBytes), humanBytes(snap.GOMemLimitBytes), frac*100)).
			Fill(thresholdColor(frac * 100)).
			Send()
	} else {
		c.Label("GOMEMLIMIT: off (no soft memory limit set)").Send()
	}

	t := snap.HistTimeUnixSec
	if len(t) < 2 {
		return
	}

	// GC sawtooth — heap objects climbing toward the GC goal, GOMEMLIMIT as ceiling.
	if len(snap.HistHeapObjectsMiB) == len(t) {
		c.AddSpace(inst.spaceTight())
		inst.sectionHeader("GC sawtooth")
		p := inst.beginTimePlot("##heap-sawtooth", 180, "MiB", t)
		p.SetNextColor(colorMetricPrimary.Literal()).SetNextWeight(2.0)
		p.Line("heap in use", t, snap.HistHeapObjectsMiB)
		if len(snap.HistHeapGoalMiB) == len(t) {
			p.SetNextColor(colorWarn.Literal()).SetNextWeight(1.0)
			p.Line("GC goal", t, snap.HistHeapGoalMiB)
		}
		if snap.MemLimitSet() {
			p.SetNextColor(colorHot.Literal()).SetNextWeight(1.0)
			p.InfLinesH("GOMEMLIMIT", []float64{mib(snap.GOMemLimitBytes)})
		}
		p.End()
	}

	// Stacked memory classes over time. Drawn as cumulative boundaries filled to
	// zero, largest first, so each smaller band paints over the one above it;
	// band k keeps colour k, so the plot legend maps names to the visible bands.
	bands := heapBands(snap)
	if len(bands) > 0 {
		c.AddSpace(inst.spaceTight())
		inst.sectionHeader("Memory classes")
		cum := cumulativeBands(bands)
		p := inst.beginTimePlot("##heap-classes", 180, "MiB", t)
		for k := len(cum) - 1; k >= 0; k-- {
			p.SetNextColor(bandColor(k).Literal()).SetNextWeight(1.0)
			p.Shaded(bands[k].name, t, cum[k], 0)
		}
		p.End()
	}
}

// heapBand is one stacked memory-class series (MiB over time).
type heapBand struct {
	name   string
	series []float64
}

// heapBands returns the five memory-class bands, bottom (objects) to top (other),
// only when all are present and length-aligned with the time axis. They partition
// the mapped total, so stacking them recovers (≈) total mapped memory.
func heapBands(snap *PublishedSnapshot) (out []heapBand) {
	n := len(snap.HistTimeUnixSec)
	cand := []heapBand{
		{"objects", snap.HistHeapObjectsMiB},
		{"idle", snap.HistIdleMiB},
		{"stacks", snap.HistStacksMiB},
		{"metadata", snap.HistMetadataMiB},
		{"other", snap.HistOtherMiB},
	}
	for _, b := range cand {
		if len(b.series) != n {
			return nil
		}
	}
	out = cand
	return
}

// cumulativeBands returns running cumulative sums: out[k][ti] is the sum of bands
// 0..k at time ti — the stacked boundary lines.
func cumulativeBands(bands []heapBand) (out [][]float64) {
	if len(bands) == 0 {
		return
	}
	n := len(bands[0].series)
	out = make([][]float64, len(bands))
	for k := range bands {
		out[k] = make([]float64, n)
	}
	for ti := range n {
		var acc float64
		for k := range bands {
			acc += bands[k].series[ti]
			out[k][ti] = acc
		}
	}
	return
}
