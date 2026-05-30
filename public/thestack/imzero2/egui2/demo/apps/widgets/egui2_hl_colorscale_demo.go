//go:build llm_generated_opus47

package widgets

import (
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colorscale"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// =============================================================================
// Colorscale demo — interactive showcase of treemap.Colormap / colorscale /
// finddivisions features:
//
//   - Palette choice (Viridis, Magma, Inferno, Cividis)
//   - Linear vs log-base-10 axis
//   - Data-range adjustment (min/max)
//   - Tick density (desiredTicks)
//
// The treemap's cell coloring and the legend's gradient share a single
// *treemap.Colormap, so changes to any control ripple through both widgets
// consistently. Hovering the legend dims treemap cells whose leaf size
// falls outside a ±0.5-log-decade band around the hovered value.
// =============================================================================

// csPalette bundles a display name with the raw RGBA slice so the radio
// buttons have something to show.
type csPalette struct {
	name   string
	colors []uint32
}

var csPalettes = []csPalette{
	{"Viridis", treemap.Viridis8},
	{"Magma", treemap.Magma8},
	{"Inferno", treemap.Inferno8},
	{"Cividis", treemap.Cividis8},
}

// csConfig is the full set of user-controllable settings. Widget instances
// are rebuilt whenever this struct's value changes.
type csConfig struct {
	paletteIdx int
	tickerIdx  int
	isLog      bool
	minVal     float64
	maxVal     float64
	ticks      float64 // f64 to match SliderF64; rounded at use
}

// csTickers bundles a display name with the TickerE value. Order is stable so
// radio-button indices can map directly to entries.
var csTickers = []struct {
	name string
	t    colorscale.TickerE
}{
	{"Talbot", colorscale.TickerTalbot},
	{"Heckbert", colorscale.TickerHeckbert},
	{"Nelder", colorscale.TickerNelder},
}

// colorscaleDemoState is the per-app-instance state. csCfg is the
// live user-controllable settings; csCfgPrev is the value from last
// frame, so rebuildCsWidgets fires whenever it diverges. The
// colormap / treemap / colorscale / hover-band quadruple is rebuilt
// together whenever cfg changes — they're tied through the same
// Colormap pointer.
type colorscaleDemoState struct {
	ids       *c.WidgetIdStack
	cfg       csConfig
	cfgPrev   csConfig
	colormap  *treemap.Colormap
	tm        *treemap.Treemap
	scale     *colorscale.ColorScale
	hoverBand *colorscale.HoverBand
}

func init() {
	registry.Register(registry.Demo{
		Name:        "colorscale",
		Category:    "Charts & plots",
		Title:       "colorscale legend",
		Stage:       [2]float32{1024, 600},
		Kind:        registry.DemoKindMixed,
		Description: "Colorscale legend renderer (continuous gradient + discrete tick labels) used by the heatmap and choropleth demos.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			state = &colorscaleDemoState{
				ids:     ids,
				cfg:     csConfig{paletteIdx: 0, tickerIdx: 0, isLog: true, minVal: 1, maxVal: 1000, ticks: 6},
				cfgPrev: csConfig{paletteIdx: -1}, // force initial build
			}
			return
		},
		RenderStateful: func(_ *c.WidgetIdStack, state any) {
			demoColorscale(state.(*colorscaleDemoState))
		},
		SourceFunc: demoColorscale,
	})
}

// rebuildCsWidgets (re-)constructs the Colormap, treemap, and colorscale
// using the current state.cfg. Called any time a control changes.
func (st *colorscaleDemoState) rebuildCsWidgets() {
	// Validate: log mode requires strictly positive bounds.
	min, max := st.cfg.minVal, st.cfg.maxVal
	if st.cfg.isLog {
		if min <= 0 {
			min = 0.001
		}
	}
	if max <= min {
		max = min * 10
	}
	pal := csPalettes[st.cfg.paletteIdx].colors
	if st.cfg.isLog {
		st.colormap = treemap.NewLogColormap(pal, min, max)
	} else {
		st.colormap = treemap.NewColormap(pal, min, max)
	}

	root := makeSampleTree()
	valFn := leafSizeForColormap(st.cfg.isLog)

	st.hoverBand = colorscale.NewHoverBand(
		st.colormap,
		treemap.ContinuousColoringFromMap(st.colormap, valFn),
		valFn,
	)

	st.tm = treemap.New(st.ids, "cs-tm", root,
		treemap.WithContainerSize(700, 360),
		treemap.WithAnimationDuration(0.28),
		treemap.WithColoring(st.hoverBand),
	)

	st.scale = colorscale.New(st.ids, "cs-scale", st.colormap.Config(),
		colorscale.WithSize(700, 42),
		colorscale.WithDesiredTicks(int(st.cfg.ticks)),
		colorscale.WithTicker(csTickers[st.cfg.tickerIdx].t),
	)

	st.scale.OnHover(func(h colorscale.HoverInfo) {
		if !h.Ok {
			st.hoverBand.ClearBand()
			return
		}
		st.hoverBand.SetBand(h.Value)
	})
}

// leafSizeForColormap returns a data-access fn suitable for the current
// scale type. For log colormaps, values are clamped to ≥1 so they stay in
// the positive domain.
func leafSizeForColormap(isLog bool) func(*layout.Node) float64 {
	return func(n *layout.Node) float64 {
		if n == nil {
			return 1
		}
		s := n.Size
		if s <= 0 {
			s = n.TotalSize()
		}
		if isLog && s < 1 {
			s = 1
		}
		return s
	}
}

func demoColorscale(st *colorscaleDemoState) {
	// Force the window's content area wide and tall enough that the
	// controls above and treemap below are always visible together.
	c.UiSetMinWidth(720)

	// Detect config changes and rebuild. Comparing by value is fine because
	// csConfig has no slices/maps.
	if st.cfg != st.cfgPrev {
		st.rebuildCsWidgets()
		st.cfgPrev = st.cfg
	}

	for range c.Vertical().KeepIter() {
		st.renderCsControls()
		c.AddSpace(gapItems())
		st.scale.Render()
		c.AddSpace(gapSections()) // clearer visual separation between legend and map
		st.tm.Render()
	}
}

// renderCsControls emits the row of interactive controls above the legend.
func (st *colorscaleDemoState) renderCsControls() {
	// Palette row.
	for range c.Horizontal().KeepIter() {
		c.LabelAtoms(c.Atoms().Text("Palette:").Keep()).Send()
		for i, p := range csPalettes {
			if c.Button(st.ids.PrepareSeq(uint64(0xc5100+i)), c.Atoms().Text(p.name).Keep()).
				Selected(st.cfg.paletteIdx == i).
				Frame(true).
				SendResp().HasPrimaryClicked() {
				st.cfg.paletteIdx = i
			}
		}
	}

	// TickerE row. Talbot is overlap-aware; Heckbert/Nelder are simpler and
	// highlight the visual differences in tick placement. In log mode,
	// Heckbert/Nelder produce non-power-of-10 ticks (e.g., 3.16, 31.6) while
	// Talbot keeps clean decades — flip the scale checkbox to compare.
	for range c.Horizontal().KeepIter() {
		c.LabelAtoms(c.Atoms().Text("Ticker:").Keep()).Send()
		for i, tk := range csTickers {
			if c.Button(st.ids.PrepareSeq(uint64(0xc5110+i)), c.Atoms().Text(tk.name).Keep()).
				Selected(st.cfg.tickerIdx == i).
				Frame(true).
				SendResp().HasPrimaryClicked() {
				st.cfg.tickerIdx = i
			}
		}
	}

	// Toggles + sliders row. Checkbox for log, sliders for min/max/ticks.
	for range c.Horizontal().KeepIter() {
		c.Checkbox(st.ids.PrepareSeq(0xc5200), st.cfg.isLog, "Log scale").SendRespVal(&st.cfg.isLog)
		c.AddSpace(padDefault())

		c.SliderF64(st.ids.PrepareSeq(0xc5201), st.cfg.ticks, 3, 12).
			Text("ticks").
			SendRespVal(&st.cfg.ticks)
	}

	for range c.Horizontal().KeepIter() {
		// Min / Max ranges depend on scale type: log forbids 0.
		var minLo float64 = 0
		if st.cfg.isLog {
			minLo = 0.01
		}
		c.SliderF64(st.ids.PrepareSeq(0xc5202), st.cfg.minVal, minLo, 100).
			Text("min").
			SendRespVal(&st.cfg.minVal)
		c.AddSpace(padDefault())
		c.SliderF64(st.ids.PrepareSeq(0xc5203), st.cfg.maxVal, 10, 100000).
			Text("max").
			SendRespVal(&st.cfg.maxVal)
	}

	// Read-out row: show the active colormap + current hover value.
	for range c.Horizontal().KeepIter() {
		ticksInt := int(st.cfg.ticks)
		c.LabelAtoms(c.Atoms().Text(fmt.Sprintf(
			"%s  |  %s  |  %s  |  [%g, %g]  |  %d ticks",
			csPalettes[st.cfg.paletteIdx].name,
			csTickers[st.cfg.tickerIdx].name,
			scaleTypeName(st.cfg.isLog),
			st.cfg.minVal, st.cfg.maxVal,
			ticksInt,
		)).Keep()).Send()

		if st.scale != nil {
			if h := st.scale.HoveredValue(); h.Ok {
				c.AddSpace(padOuter())
				c.LabelAtoms(c.Atoms().Text(fmt.Sprintf("hover value ≈ %.3g", h.Value)).Keep()).Send()
			}
		}
	}
}

func scaleTypeName(isLog bool) string {
	if isLog {
		return "log"
	}
	return "linear"
}
