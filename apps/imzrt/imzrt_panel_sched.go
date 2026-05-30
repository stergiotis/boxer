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
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/heatmapscroll"
)

// spectroWidthSlots is the spectrogram ring width in time steps — 10 minutes of
// history at the default 1 Hz cadence. Fixed at construction (the scrolling
// texture is sized to it).
const spectroWidthSlots uint32 = 600

// The /sched/latencies histogram spans sub-ns to ~1000s, but real scheduling
// latencies live in a narrow band. Clip the spectrogram to [10ns, 100ms] so its
// height is spent on the latencies that actually occur: the bottom carries the
// normal fast schedules and the band climbs under scheduling pressure. Buckets
// outside the window (including the runtime's open ±Inf end bins) are dropped.
const (
	spectroLoSec                 = 10e-9  // 10 ns
	spectroHiSec                 = 100e-3 // 100 ms
	spectroDisplayHeight float32 = 240
)

// schedSpectroState is per-window state for the scheduling-latency spectrogram.
// Built lazily on the first column that carries the histogram's bucket layout, so
// the clipped bucket range and the texture height lock to the runtime's buckets.
type schedSpectroState struct {
	hs           *heatmapscroll.HeatmapScroll
	cfg          *colormap.Config
	loIdx, hiIdx int    // displayed bucket-index range [loIdx, hiIdx] after clipping
	nDisplay     int    // hiIdx - loIdx + 1
	loLabel      string // latency at the bottom edge
	hiLabel      string // latency at the top edge
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
		// ScrollLeft — classical spectrogram: newest column on the right.
		st.hs.SetOrientation(heatmapscroll.ScrollLeft)
		st.colBuf = make([]float32, st.nDisplay)
		// Prefill the ring so it opens as a full background rectangle.
		for range spectroWidthSlots {
			st.hs.PushColumn(st.colBuf)
		}
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
	c.Label(fmt.Sprintf("y: %s (bottom) → %s (top), log · x: time, newest right · brighter = more goroutines waiting",
		st.loLabel, st.hiLabel)).Send()
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
// ScrollLeft puts oldest on the left and newest on the right, so labels render in
// ascending order. Mirrors imztop's heatmap x-axis (timeticks).
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
	for range c.Horizontal().KeepIter() {
		for i := range layout.TickLabels {
			c.Label(layout.TickLabels[i]).Send()
			if i < len(layout.TickLabels)-1 {
				c.AddSpace(24)
			}
		}
	}
}
