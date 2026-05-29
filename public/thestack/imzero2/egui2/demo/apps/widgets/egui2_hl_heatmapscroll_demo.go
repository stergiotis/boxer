package widgets

// =============================================================================
// DEMO: heatmapscroll — spectrogram / waterfall wrapper (ADR-0009)
// =============================================================================
//
// A 2×2 grid of HeatmapScroll widgets, one per orientation, streaming a
// synthetic drifting-chirp pattern. Each panel uses a different colormap
// and showcases a distinct feature of the wrapper:
//
//   ┌─ ScrollLeft  (Viridis, NEAREST) ──┬─ ScrollRight (Plasma,  LINEAR)  ──┐
//   │  classical audio spectrogram       │  linear-filter sampling contrast │
//   ├─ ScrollUp    (Inferno, NEAREST) ──┼─ ScrollDown  (Cividis, NEAREST) ──┤
//   │  vertical-forward waterfall        │  classical RF waterfall          │
//   └────────────────────────────────────┴──────────────────────────────────┘
//
// Controls:
//   - Pause: freeze the data stream (rings stop scrolling; hover/click
//     keep working on the static content).
//   - Inject NaN: periodically sprinkle NaN and +Inf into the stream so
//     the caller-chosen BadColor (bright red) is visible as speckle.
//
// Readouts per panel:
//   - Hover row/col in ring-buffer space (one frame behind; see ADR-0009
//     Consequences / Negative).
//   - Click counter (aggregated across all four panels in the header).
//
// The data source is a drifting fundamental tone + 3rd harmonic + a
// small deterministic noise floor, computed purely from the frame
// counter so runs are reproducible.
// =============================================================================

import (
	"fmt"
	"image/color"
	"math"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/heatmapscroll"
)

const (
	heatmapDemoWidthSlots  uint32 = 160
	heatmapDemoHeightSlots uint32 = 160
)

// heatmapscrollDemoState bundles every piece of per-app-instance state
// the demo touches. Each open gallery window owns its own four
// HeatmapScroll widgets, its own click counter, and its own column
// buffer so two windows can pause independently and have distinct
// hover / click feedback.
type heatmapscrollDemoState struct {
	frame  uint64
	paused bool
	inject bool
	clicks uint64
	colBuf []float32
	panels [4]heatmapDemoPanel
}

// heatmapDemoPanel bundles a HeatmapScroll with its display label and
// the orientation it renders. The orientation is stored redundantly so
// the render loop can annotate the hover readout as (bin, time) vs
// (time, bin) without reaching into the widget's internals.
type heatmapDemoPanel struct {
	label       string
	orientation heatmapscroll.Orientation
	hs          *heatmapscroll.HeatmapScroll
}

// heatmapDemoTitle keeps the demo title declaration with the rest of the
// demo's code — the registration side uses it directly. Using an
// `icons.IconWaveform` glyph for quick visual identification in the
// gallery, matching the other streaming/spectrogram-style entries.
var heatmapDemoTitle = icons.IconWaveform + " heatmapscroll"

func init() {
	registry.Register(registry.Demo{
		Name:        "heatmapscroll",
		Category:    "Charts & plots",
		Title:       heatmapDemoTitle,
		Stage:       [2]float32{780, 820},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindUX,
		Description: "Spectrogram-style scrolling heatmap with a colorscale legend; an ADR-0009 streaming-texture consumer.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			st := &heatmapscrollDemoState{
				colBuf: make([]float32, heatmapDemoHeightSlots),
			}
			st.initPanels(ids)
			state = st
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoHeatmapScroll(ids, state.(*heatmapscrollDemoState))
		},
	})
}

func (st *heatmapscrollDemoState) initPanels(ids *c.WidgetIdStack) {
	// One Config per panel so SetConfig-style tweaks later won't leak across
	// panels. Data range matches the synthetic generator's expected peak
	// (~1.4 with both tones additively constructive).
	const (
		dataMin = 0.0
		dataMax = 1.4
	)
	bad := color.NRGBA{R: 0xff, G: 0x33, B: 0x33, A: 0xff}  // bright red speckle
	under := color.NRGBA{A: 0xff}                           // opaque black
	over := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff} // white for +Inf / clipping

	mkCfg := func(palette []uint32) *colormap.Config {
		cfg := colormap.NewConfig(palette, dataMin, dataMax)
		cfg.BadColor = bad
		cfg.UnderflowColor = under
		cfg.OverflowColor = over
		return cfg
	}

	st.panels = [4]heatmapDemoPanel{
		{
			label:       "ScrollLeft · Viridis · NEAREST",
			orientation: heatmapscroll.ScrollLeft,
			hs:          heatmapscroll.New(ids, "hm-scroll-left", mkCfg(colormap.Viridis8), heatmapDemoWidthSlots, heatmapDemoHeightSlots),
		},
		{
			label:       "ScrollRight · Plasma · LINEAR",
			orientation: heatmapscroll.ScrollRight,
			hs:          heatmapscroll.New(ids, "hm-scroll-right", mkCfg(colormap.Plasma8), heatmapDemoWidthSlots, heatmapDemoHeightSlots),
		},
		{
			label:       "ScrollUp · Inferno · NEAREST",
			orientation: heatmapscroll.ScrollUp,
			hs:          heatmapscroll.New(ids, "hm-scroll-up", mkCfg(colormap.Inferno8), heatmapDemoWidthSlots, heatmapDemoHeightSlots),
		},
		{
			label:       "ScrollDown · Cividis · NEAREST",
			orientation: heatmapscroll.ScrollDown,
			hs:          heatmapscroll.New(ids, "hm-scroll-down", mkCfg(colormap.Cividis8), heatmapDemoWidthSlots, heatmapDemoHeightSlots),
		},
	}
	for i := range st.panels {
		st.panels[i].hs.SetOrientation(st.panels[i].orientation)
	}
	st.panels[1].hs.SetFilter(heatmapscroll.FilterLinear) // contrast sampling modes
}

// heatmapDemoPseudoRand returns a deterministic noise value in [0, 1)
// from a 64-bit seed — two splitmix64 rounds, good enough for visual
// noise without pulling in math/rand or needing per-seed state.
func heatmapDemoPseudoRand(seed uint64) float64 {
	seed ^= seed >> 30
	seed *= 0xbf58476d1ce4e5b9
	seed ^= seed >> 27
	seed *= 0x94d049bb133111eb
	seed ^= seed >> 31
	return float64(seed>>11) / float64(1<<53)
}

// heatmapDemoGenerateColumn fills out with a drifting two-tone pattern:
// a fundamental that slowly sweeps across the bin axis + its 3rd harmonic
// + a small deterministic noise floor. The frame counter drives both the
// sweep phase and the noise seed so the whole sequence is reproducible.
// injectBad sprinkles NaN and +Inf at fixed rows so BadColor substitution
// (and OverflowColor for +Inf) are visible as speckle.
func heatmapDemoGenerateColumn(out []float32, frame uint64, injectBad bool) {
	h := float64(len(out) - 1)
	phase := float64(frame) * 0.025
	f0 := 0.28 + 0.18*math.Sin(phase*0.7)
	f1 := 3.0 * f0
	const peakWidthSq = 0.015 * 0.015
	for i := range out {
		bin := float64(i) / h
		d0 := bin - f0
		d1 := bin - f1
		v := math.Exp(-(d0*d0)/(2*peakWidthSq)) +
			0.45*math.Exp(-(d1*d1)/(2*peakWidthSq))
		// Deterministic noise seeded from (frame, row).
		v += 0.025 * (heatmapDemoPseudoRand(uint64(frame)<<20|uint64(i)) - 0.5)
		out[i] = float32(v)
	}
	if injectBad {
		// Periodic speckle so the BadColor / OverflowColor substitutions
		// are eye-catching without swamping the legitimate signal.
		if frame%7 == 0 {
			out[7] = float32(math.NaN())
			out[len(out)/2] = float32(math.Inf(1))
			out[len(out)-10] = float32(math.NaN())
		}
	}
}

func demoHeatmapScroll(ids *c.WidgetIdStack, st *heatmapscrollDemoState) {
	st.frame++

	if !st.paused {
		heatmapDemoGenerateColumn(st.colBuf, st.frame, st.inject)
		for i := range st.panels {
			st.panels[i].hs.PushColumn(st.colBuf)
		}
	}

	for range c.Vertical().KeepIter() {
		renderHeatmapDemoIntro()
		c.AddSpace(gapInline())
		st.renderHeatmapDemoControls(ids)
		c.AddSpace(padInner())
		st.renderHeatmapDemoGrid()
	}
}

// renderHeatmapDemoIntro emits a short block of explanatory text at the
// top of the demo so a first-time viewer can decode what the four panels
// are showing without reading the ADR. Multiple single-line Labels are
// used because no Wrap-atom is wired up in components yet.
func renderHeatmapDemoIntro() {
	c.Label("heatmapscroll — streaming spectrogram/waterfall over a caller-owned ring.").Send() // designlint:ignore=L1 (widget identifier; lowercase per Go package name)
	c.Label("All four panels receive the same synthetic chirp (drifting fundamental + 3rd").Send()
	c.Label("harmonic + deterministic noise); they differ only in orientation, colormap,").Send() // designlint:ignore=L1 (continuation of preceding line)
	c.Label("and filter, so the four renderings line up time-aligned for visual compare.").Send() // designlint:ignore=L1 (continuation of preceding line)
	c.Label("Hover readout is (row, col) = (screen-y index, screen-x index): for horizontal").Send()
	c.Label("panels that is (bin, time); for vertical panels (time, bin) — same pointer").Send() // designlint:ignore=L1 (continuation of preceding line)
	c.Label("position, axes rotated. Click anywhere inside a panel to bump the counter.").Send() // designlint:ignore=L1 (continuation of preceding line)
	c.Label("Pause freezes new samples (Render still fires). Inject NaN/+Inf sprinkles").Send()
	c.Label("non-finite samples so BadColor (red speckle) and OverflowColor (white) show.").Send() // designlint:ignore=L1 (continuation of preceding line)
}

func (st *heatmapscrollDemoState) renderHeatmapDemoControls(ids *c.WidgetIdStack) {
	for range c.Horizontal().KeepIter() {
		c.Checkbox(ids.PrepareStr("hm-pause"), st.paused, "Pause stream").SendRespVal(&st.paused)
		c.AddSpace(padDefault())
		c.Checkbox(ids.PrepareStr("hm-inject"), st.inject, "Inject NaN/+Inf").SendRespVal(&st.inject)
		c.AddSpace(padDefault())
		// Sum stats across panels so one header summarises the full stream.
		var total colormap.ColumnStats
		for i := range st.panels {
			total.Add(st.panels[i].hs.TotalStats())
		}
		c.LabelAtoms(c.Atoms().Text(fmt.Sprintf(
			"frame=%d  clicks=%d  bad=%d  under=%d  over=%d",
			st.frame, st.clicks,
			total.BadSamples, total.Underflow, total.Overflow,
		)).Keep()).Send()
	}
}

func (st *heatmapscrollDemoState) renderHeatmapDemoGrid() {
	for range c.Vertical().KeepIter() {
		for range c.Horizontal().KeepIter() {
			st.renderHeatmapDemoPanel(&st.panels[0])
			c.AddSpace(gapItems())
			st.renderHeatmapDemoPanel(&st.panels[1])
		}
		c.AddSpace(gapInline())
		for range c.Horizontal().KeepIter() {
			st.renderHeatmapDemoPanel(&st.panels[2])
			c.AddSpace(gapItems())
			st.renderHeatmapDemoPanel(&st.panels[3])
		}
	}
}

func (st *heatmapscrollDemoState) renderHeatmapDemoPanel(p *heatmapDemoPanel) {
	for range c.Vertical().KeepIter() {
		c.LabelAtoms(c.Atoms().Text(p.label).Keep()).Send()
		p.hs.Render()
		if p.hs.Clicked() {
			st.clicks++
		}
		row, col, ok := p.hs.HoveredCell()
		var txt string
		if ok {
			// Annotate the semantic meaning of row/col per the
			// ADR-0009 SD11 screen-axis convention: horizontal
			// panels map (row, col) to (bin, time); vertical
			// panels rotate the screen axes, so the mapping
			// swaps to (time, bin).
			rowTag, colTag := "bin", "time"
			if p.orientation == heatmapscroll.ScrollUp || p.orientation == heatmapscroll.ScrollDown {
				rowTag, colTag = "time", "bin"
			}
			txt = fmt.Sprintf("hover: row=%d (%s) col=%d (%s)", row, rowTag, col, colTag)
		} else {
			txt = "hover: —"
		}
		c.LabelAtoms(c.Atoms().Text(txt).Keep()).Send()
	}
}
