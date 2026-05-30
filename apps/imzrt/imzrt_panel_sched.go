package imzrt

import (
	"fmt"
	"image/color"
	"runtime"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/heatmapscroll"
)

// spectroWidthSlots is the spectrogram ring width in time steps — 10 minutes of
// history at the default 1 Hz cadence. Fixed at construction (the underlying
// scrolling texture is sized to it).
const spectroWidthSlots uint32 = 600

// spectroDisplayHeight is the on-screen height (px) the bucket-tall texture is
// stretched to, so the heatmap reads regardless of the runtime's bucket count.
const spectroDisplayHeight float32 = 200

// schedSpectroState is per-window state for the scheduling-latency spectrogram.
// Initialised lazily on the first column that carries the histogram's bucket
// layout, so the texture height locks to the runtime's bucket count.
type schedSpectroState struct {
	hs           *heatmapscroll.HeatmapScroll
	cfg          *colormap.Config
	nBuckets     uint32 // count bins = len(buckets)-1, locked at first push
	colBuf       []float32
	lastPushedMs int64
	smoothedMax  float64 // smoothed peak per-interval bucket count → colormap DataMax
}

func (inst *App) renderSchedPanel(snap *PublishedSnapshot) {
	inst.sectionHeader("Scheduler")

	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("goroutines %s", humanCount(snap.Goroutines))).Send()
		c.Label(fmt.Sprintf("· GOMAXPROCS %d/%d", runtime.GOMAXPROCS(0), runtime.NumCPU())).Send()
		c.Label(fmt.Sprintf("· sched p99 %s", humanDuration(snap.SchedP99Sec))).Send()
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
	nb := len(snap.SchedLatColBuckets)
	if nb < 2 {
		c.Label("scheduling-latency data not yet available…").Send()
		return
	}
	nBuckets := uint32(nb - 1)
	st := &inst.schedSpectro

	if st.hs == nil {
		st.nBuckets = nBuckets
		// Log scale: count 1 sits at the palette floor, the smoothed peak at the
		// ceiling; count 0 is non-positive → underflow → background. DataMax is
		// rescaled live each tick (it is a plain mutable field).
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
		st.hs = heatmapscroll.New(inst.ids, "sched-spectro", st.cfg, spectroWidthSlots, nBuckets)
		// ScrollLeft — classical spectrogram: newest column on the right.
		st.hs.SetOrientation(heatmapscroll.ScrollLeft)
		st.colBuf = make([]float32, nBuckets)
		// Prefill the ring so it opens as a full background rectangle (all-zero
		// columns map to underflow → bg) rather than a sparse edge of real data.
		for range spectroWidthSlots {
			st.hs.PushColumn(st.colBuf)
		}
	}

	// One column per published sample. Guard against re-pushing the same column
	// across the many render frames between sampler ticks.
	if snap.SampledAtUnixMs > st.lastPushedMs && uint32(len(snap.SchedLatColCounts)) == st.nBuckets {
		var colMax float64
		for i := range st.nBuckets {
			cnt := snap.SchedLatColCounts[i]
			if float64(cnt) > colMax {
				colMax = float64(cnt)
			}
			// Low latency at the bottom (bucket 0 → last texture row).
			st.colBuf[st.nBuckets-1-i] = float32(cnt)
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
	c.Label("low latency at bottom · brighter = more goroutines waiting (log scale) · newest on the right").Send()
}
