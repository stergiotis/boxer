package imzrt

import (
	"fmt"
	"image/color"
	"math"
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
	// spectroYAxisW is the latency-axis gutter drawn left of the spectrogram;
	// its labels are power-of-10 latencies at spectroYAxisFont.
	spectroYAxisW    float32 = 44
	spectroYAxisFont float32 = 10
	// spectroXAxisH is the x-axis tick+label row height; spectroAxisPad reserves
	// width for item spacing/margin when stretching to fill; spectroMinTexW is
	// the floor below which the native texture width is kept.
	spectroXAxisH  float32 = 18
	spectroAxisPad float32 = 16
	spectroMinTexW float32 = 240
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
	loEdge       float64                // latency at the bottom edge of the displayed range
	hiEdge       float64                // latency at the top edge of the displayed range
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
		st.loEdge = buckets[st.loIdx]
		st.hiEdge = buckets[st.hiIdx+1]
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
		// ScrollLeft — newest column on the RIGHT, ageing leftward, matching
		// this panel's Goroutines/p99 line plots and every other plot in the
		// app. The spectrogram's hot band now lines up vertically with the p99
		// line below (same instant → same screen-x), delivering ADR-0061 M3's
		// "p99 aligns with the hot band" goal. (Until 2026-06-17 this used
		// ScrollRight/newest-left to mirror imztop's CPU heatmap per SD10;
		// imztop was flipped in tandem, so the two dashboards stay consistent.)
		st.hs.SetOrientation(heatmapscroll.ScrollLeft)
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

	// Stretch the texture to fill the panel width (minus the y-axis gutter) so
	// the spectrogram and its x-axis use all available space, not the native
	// 600 px. 0 keeps the native width as a fallback before the available size
	// is known (one-frame lag on CaptureAvailableSize is fine for a stable dock).
	texW := float32(0)
	c.CaptureAvailableSize()
	avail := c.CurrentApplicationState.StateManager.GetAvailableSize()
	if avail.W > 0 && !math.IsNaN(float64(avail.W)) {
		if cand := avail.W - spectroYAxisW - spectroAxisPad; cand > spectroMinTexW {
			texW = cand
		}
	}
	st.hs.SetDisplaySize(texW, spectroDisplayHeight)
	for range c.Horizontal().KeepIter() {
		inst.renderSpectroYTicks(st.loEdge, st.hiEdge, spectroDisplayHeight)
		st.hs.Render()
	}
	xw := texW
	if xw <= 0 {
		xw = float32(spectroWidthSlots)
	}
	inst.renderSpectroXTicks(snap.HistTimeUnixSec, xw)
	c.Label("y: latency (log) · x: time, newest right").Send()
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

// renderSpectroXTicks paints calendar-aware time labels under the spectrogram,
// spanning the texture width w. The spectrogram scrolls ScrollLeft (newest on
// the right), so the newest tick sits at x=w and time increases left-to-right,
// matching the p99 line plot below. Labels are positioned by time across the
// full width (not fixed-gap), so the axis is fully populated. Only the last
// spectroWidthSlots samples are visible, so the range comes from that tail of
// the history. Edge ticks anchor inward to avoid clipping; indented by
// spectroYAxisW to align under the texture.
func (inst *App) renderSpectroXTicks(timeUnixSec []float64, w float32) {
	n := len(timeUnixSec)
	if n < 2 || w <= 0 {
		return
	}
	lo := 0
	if n > int(spectroWidthSlots) {
		lo = n - int(spectroWidthSlots)
	}
	minT := time.Unix(int64(timeUnixSec[lo]), 0).Local()
	maxT := time.Unix(int64(timeUnixSec[n-1]), 0).Local()
	minMS := minT.UnixMilli()
	spanMS := maxT.UnixMilli() - minMS
	if spanMS <= 0 {
		return
	}
	layout := timeticks.TimeTicks(minT, maxT, timeticks.TimeTickOptions{
		PanelWidthPx:    int32(w),
		TargetSpacingPx: 120,
		Location:        time.Local,
	})
	const edgeGuard float32 = 18
	for range c.Horizontal().KeepIter() {
		c.AddSpace(spectroYAxisW)
		for i, tv := range layout.TickValues {
			if i >= len(layout.TickLabels) {
				break
			}
			// newest-right: oldest → x=0, newest → right edge (x=w).
			norm := float64(tv.UnixMilli()-minMS) / float64(spanMS)
			px := float32(norm * float64(w))
			if px < 0 || px > w {
				continue
			}
			c.PaintLine(px, 0, px, 4, colorAxisTick, 1.0).Send()
			ah := uint8(1)
			switch {
			case px < edgeGuard:
				ah = 0
			case px > w-edgeGuard:
				ah = 2
			}
			c.PaintText(px, 6, ah, 0, layout.TickLabels[i], spectroYAxisFont, colorAxisLabel).Send()
		}
		c.PaintCanvas(inst.ids.PrepareStr("sched-spectro-xaxis"), w, spectroXAxisH).
			Background(colorBgClear).
			Send()
	}
}

// fmtLatencyTick renders an exact power-of-ten latency compactly for the y-axis,
// dropping trailing zeros: 10ns, 100ns, 1µs, 100µs, 1ms.
func fmtLatencyTick(sec float64) string {
	switch {
	case sec < 1e-6:
		return fmt.Sprintf("%gns", sec*1e9)
	case sec < 1e-3:
		return fmt.Sprintf("%gµs", sec*1e6)
	case sec < 1:
		return fmt.Sprintf("%gms", sec*1e3)
	default:
		return fmt.Sprintf("%gs", sec)
	}
}

// renderSpectroYTicks paints power-of-ten latency labels (log scale) in a gutter
// to the left of the spectrogram, aligned to the texture's displayed height h.
// loEdge / hiEdge are the bottom / top latency boundaries of the displayed bucket
// range. The runtime buckets are ~geometric, so a value maps to a log fraction of
// the range; low latency sits at the bottom, matching the texture's row flip.
// loEdge can round to ~0 (the lowest bucket opens at zero), so the log floor is
// clamped to spectroLoSec. Always emits the spectroYAxisW-wide canvas so the
// texture beside it and the x-ticks below stay aligned.
func (inst *App) renderSpectroYTicks(loEdge, hiEdge float64, h float32) {
	lo := loEdge
	if lo < spectroLoSec {
		lo = spectroLoSec
	}
	if hiEdge > lo {
		lmin, lmax := math.Log10(lo), math.Log10(hiEdge)
		for e := math.Ceil(lmin); e <= lmax; e++ {
			frac := (e - lmin) / (lmax - lmin)
			y := h * float32(1-frac)
			c.PaintLine(spectroYAxisW-3, y, spectroYAxisW, y, colorAxisTick, 1.0).Send()
			// Center the label on its tick, except within a font height of the
			// top/bottom edge, where a centered label would paint past the canvas
			// clip rect — anchor it inward there (top edge → text below the line,
			// bottom edge → text above the line).
			av := uint8(1) // middle
			switch {
			case y < spectroYAxisFont:
				av = 0 // top
			case y > h-spectroYAxisFont:
				av = 2 // bottom
			}
			c.PaintText(spectroYAxisW-5, y, 2, av, fmtLatencyTick(math.Pow(10, e)), spectroYAxisFont, colorAxisLabel).Send()
		}
	}
	c.PaintCanvas(inst.ids.PrepareStr("sched-spectro-yaxis"), spectroYAxisW, h).
		Background(colorBgClear).
		Send()
}
