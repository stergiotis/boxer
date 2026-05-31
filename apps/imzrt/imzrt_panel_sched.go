package imzrt

import (
	"fmt"
	"image/color"
	"runtime"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/math/numerical/timeticks"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colorscale"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/heatmapscroll"
)

// spectroWidthSlots is the spectrogram ring width in time steps — 10 minutes of
// history at the default 1 Hz cadence. Fixed at construction (the scrolling
// texture is sized to it).
const spectroWidthSlots uint32 = 600

// The /sched/latencies histogram spans sub-ns to ~1000s, but real scheduling
// latencies live in a narrow band. Clip the spectrogram to [10ns, 1ms] so its
// height is spent on the latencies that actually occur: the bottom carries the
// normal fast schedules and the band climbs under scheduling pressure. p99 is
// typically ~100µs, so a 1ms ceiling leaves headroom for pressure spikes while
// dropping the perpetually-empty 1–100ms band (which rendered in the panel
// background colour, reading as dead space above the data); the rarer >1ms tail
// still shows on the p99 line plot below. Buckets outside the window (including
// the runtime's open ±Inf end bins) are dropped.
const (
	spectroLoSec                 = 10e-9 // 10 ns
	spectroHiSec                 = 1e-3  // 1 ms
	spectroDisplayHeight float32 = 240
	spectroLegendW       float32 = 280
	// The colorscale legend stacks a gradient (55% of height) + a 5 px tick
	// strip + a label row; below ~40 px the labels paint past the canvas clip
	// rect and disappear. 44 matches imztop's topology legend and clears that
	// floor so the count ticks keep their labels.
	spectroLegendH float32 = 44
)

// schedSpectroState is per-window state for the scheduling-latency spectrogram.
// Built lazily on the first column that carries the histogram's bucket layout, so
// the clipped bucket range and the texture height lock to the runtime's buckets.
type schedSpectroState struct {
	hs           *heatmapscroll.HeatmapScroll
	cfg          *colormap.Config
	legend       *colorscale.ColorScale // colour→count legend bound to cfg
	loIdx, hiIdx int                    // displayed bucket-index range [loIdx, hiIdx] after clipping
	nDisplay     int                    // hiIdx - loIdx + 1
	loLabel      string                 // latency at the bottom edge
	hiLabel      string                 // latency at the top edge
	colBuf       []float32
	lastPushedMs int64
	smoothedMax  float64 // smoothed peak per-interval bucket count → colormap DataMax
}

func (inst *App) renderSchedPanel(snap *PublishedSnapshot) {
	inst.sectionHeader("Scheduler")

	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("goroutines %s", humanCount(snap.Goroutines))).Send()
		c.Label(fmt.Sprintf("· GOMAXPROCS %d/%d", runtime.GOMAXPROCS(0), runtime.NumCPU())).Send()
		for rt := range c.RichTextLabelColored(latencyThresholdColor(snap.SchedP99Sec), colorBgClear, fmt.Sprintf("· sched p99 %s", humanDuration(snap.SchedP99Sec))) {
			rt.Strong()
		}
	}
	if snap.STWAvailable {
		for range c.Horizontal().KeepIter() {
			c.Label(fmt.Sprintf("STW this interval: gc %d", snap.STWGCCount)).Send()
			c.Label(fmt.Sprintf("· other %d", snap.STWOtherCount)).Send()
		}
	}

	t := snap.HistTimeUnixSec

	// Goroutine population over time.
	if len(t) >= 2 && len(snap.HistGoroutines) == len(t) {
		c.AddSpace(inst.spaceTight())
		inst.sectionHeader("Goroutines")
		c.PlotLine("goroutines", t, snap.HistGoroutines).Width(2.0).Color(colorMetricPrimary).Send()
		c.Plot(inst.ids.PrepareStr("sched-goroutines-plot")).
			Height(140).
			YAxisLabel("count").
			Legend().
			IncludeY(0).
			AllowZoom2(true, false).
			AllowDrag2(true, false).
			AllowScroll2(true, false).
			Send()
	}

	// Scheduling-latency spectrogram: each column is one interval's /sched/latencies
	// distribution, colour = how many goroutines waited that long (log scale).
	c.AddSpace(inst.spaceTight())
	inst.sectionHeader("Scheduling-latency spectrogram")
	inst.renderSchedSpectrogram(snap)

	// Rolling p99 scheduling latency — the spectrogram's hot band, as a line.
	if len(t) >= 2 && len(snap.HistSchedP99Ms) == len(t) {
		c.AddSpace(inst.spaceTight())
		inst.sectionHeader("Scheduling latency p99")
		c.PlotLine("p99", t, snap.HistSchedP99Ms).Width(2.0).Color(colorWarn).Send()
		c.Plot(inst.ids.PrepareStr("sched-p99-plot")).
			Height(140).
			YAxisLabel("ms").
			Legend().
			IncludeY(0).
			AllowZoom2(true, false).
			AllowDrag2(true, false).
			AllowScroll2(true, false).
			Send()
	}
}

func (inst *App) renderSchedSpectrogram(snap *PublishedSnapshot) {
	buckets := snap.SchedLatColBuckets
	if len(buckets) < 3 {
		c.Label("scheduling-latency data not yet available…").Send()
		return
	}
	st := &inst.schedSpectro

	if st.hs == nil {
		st.loIdx, st.hiIdx = clipBucketRange(buckets, spectroLoSec, spectroHiSec)
		st.nDisplay = st.hiIdx - st.loIdx + 1
		st.loLabel = humanDuration(buckets[st.loIdx])
		st.hiLabel = humanDuration(buckets[st.hiIdx+1])
		// Log scale: count 1 at the palette floor, the smoothed peak at the
		// ceiling; count 0 is non-positive → underflow → background. DataMax is
		// rescaled live each tick (a plain mutable field).
		st.cfg = colormap.NewConfig(sequentialPalette(), 1, 2)
		st.cfg.Scale = colormap.ScaleLogE
		bg := color.NRGBA{
			R: styletokens.NeutralBgSurface.R,
			G: styletokens.NeutralBgSurface.G,
			B: styletokens.NeutralBgSurface.B,
			A: 0xff,
		}
		st.cfg.BadColor = bg
		st.cfg.UnderflowColor = bg
		st.hs = heatmapscroll.New(inst.ids, "sched-spectro", st.cfg, spectroWidthSlots, uint32(st.nDisplay))
		// ScrollRight — newest column on the LEFT, ageing rightward. Matches
		// imztop's CPU heatmap so the sibling dashboards scroll the same way
		// and the x-tick labels (rendered newest-left below) line up with it.
		// Deliberately opposite to this panel's Goroutines/p99 line plots
		// (newest-right): we favour ADR-0061 SD10 (reuse imztop's pattern) over
		// M3's "p99 aligns with the hot band" goal. Don't flip to newest-right
		// without revisiting that trade-off.
		st.hs.SetOrientation(heatmapscroll.ScrollRight)
		st.colBuf = make([]float32, st.nDisplay)
		// Prefill the ring so it opens as a full background rectangle.
		for range spectroWidthSlots {
			st.hs.PushColumn(st.colBuf)
		}
		// Legend for the colour axis (goroutine count), bound to the same cfg the
		// heatmap colours from — now possible because colorscale takes a
		// colormap.Config. It tracks cfg.DataMax live as the scale rescales.
		st.legend = colorscale.New(inst.ids, "sched-spectro-scale", st.cfg,
			colorscale.WithSize(spectroLegendW, spectroLegendH),
			colorscale.WithDesiredTicks(4),
			colorscale.WithLabelFormat(func(v float64) string { return fmt.Sprintf("%.0f", v) }),
		)
	}

	// One column per published sample; guard against re-pushing across the many
	// render frames between sampler ticks.
	if snap.SampledAtUnixMs > st.lastPushedMs && len(snap.SchedLatColCounts) > st.hiIdx {
		var colMax float64
		for j := range st.nDisplay {
			cnt := snap.SchedLatColCounts[st.loIdx+j]
			if float64(cnt) > colMax {
				colMax = float64(cnt)
			}
			// Low latency (j=0, loIdx) at the bottom texture row.
			st.colBuf[st.nDisplay-1-j] = float32(cnt)
		}
		// Fast-attack / slow-release peak so the colour scale tracks pressure
		// without flickering on a single busy interval.
		if colMax > st.smoothedMax {
			st.smoothedMax = colMax
		} else {
			st.smoothedMax = st.smoothedMax*0.98 + colMax*0.02
		}
		dmax := st.smoothedMax
		if dmax < 2 {
			dmax = 2
		}
		st.cfg.DataMax = dmax
		st.hs.PushColumn(st.colBuf)
		st.lastPushedMs = snap.SampledAtUnixMs
	}

	st.hs.SetDisplaySize(0, spectroDisplayHeight)
	st.hs.Render()
	renderSpectroXTicks(snap.HistTimeUnixSec)
	c.Label(fmt.Sprintf("y: %s (bottom) → %s (top), log · x: time, newest left", st.loLabel, st.hiLabel)).Send()
	c.Label("colour = goroutines waiting per interval (log):").Send()
	st.legend.Render()
}

// clipBucketRange returns the inclusive count-bin index range whose buckets
// overlap [loSec, hiSec]. Bins entirely below loSec or above hiSec — including the
// runtime's open ±Inf end bins and the always-empty extreme-latency bins — are
// dropped, so the spectrogram spends its height on the latencies that occur.
func clipBucketRange(buckets []float64, loSec, hiSec float64) (lo, hi int) {
	n := len(buckets) - 1 // number of count bins
	lo = 0
	for lo < n-1 && buckets[lo+1] <= loSec {
		lo++
	}
	hi = n - 1
	for hi > lo && buckets[hi] >= hiSec {
		hi--
	}
	return
}

// renderSpectroXTicks draws calendar-aware time labels under the spectrogram.
// The spectrogram scrolls ScrollRight (newest column on the left), so labels
// render newest → oldest with the leftmost label under the most-recent column.
// timeticks returns ascending (oldest → newest); iterate in reverse to match.
// Mirrors imztop's renderCPUHeatmapXTicks.
func renderSpectroXTicks(timeUnixSec []float64) {
	if len(timeUnixSec) < 2 {
		return
	}
	minT := time.Unix(int64(timeUnixSec[0]), 0).Local()
	maxT := time.Unix(int64(timeUnixSec[len(timeUnixSec)-1]), 0).Local()
	if !maxT.After(minT) {
		return
	}
	layout := timeticks.TimeTicks(minT, maxT, timeticks.TimeTickOptions{
		PanelWidthPx:    600,
		TargetSpacingPx: 120,
		Location:        time.Local,
	})
	if len(layout.TickLabels) == 0 {
		return
	}
	n := len(layout.TickLabels)
	for range c.Horizontal().KeepIter() {
		for i := n - 1; i >= 0; i-- {
			c.Label(layout.TickLabels[i]).Send()
			if i > 0 {
				c.AddSpace(24)
			}
		}
	}
}
