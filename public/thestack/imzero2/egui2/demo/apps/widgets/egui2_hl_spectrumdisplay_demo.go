package widgets

// =============================================================================
// DEMO: spectrumdisplay — RF spectrum-analyzer display (ADR-0091)
// =============================================================================
//
// A single SpectrumDisplay streaming a synthetic FM-band scene: a few fixed
// "stations" plus one slowly drifting carrier, over a noise floor. It exercises
// the frequency axis (MHz), the dB colorbar, the optional spectrum-line panel,
// a region band + a vertical "tuned" marker, window scaling, and the cursor
// readout (freq / dB / age under the pointer).
//
//   ┌─ time ─┬──────── waterfall ────────┬ dB ┐
//   │  s ago │  ░▒▓ stations + carrier    │ cbar│
//   ├────────┼───────────────────────────┼─────┤
//   │        │  spectrum-line trace       │     │
//   └────────┴── 88 … 108 MHz ────────────┴─────┘
// =============================================================================

import (
	"fmt"
	"image/color"
	"math"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/spectrumdisplay"
)

const (
	spectrumDemoWidthSlots  uint32 = 220
	spectrumDemoHeightSlots uint32 = 256
)

type spectrumDemoState struct {
	frame    uint64
	paused   bool
	showLine bool
	colBuf   []float32
	sd       *spectrumdisplay.SpectrumDisplay
}

var spectrumDemoTitle = icons.IconWaveform + " spectrumdisplay"

func init() {
	registry.Register(registry.Demo{
		Name:        "spectrumdisplay",
		Category:    "Charts & plots",
		Title:       spectrumDemoTitle,
		Stage:       [2]float32{900, 820},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindUX,
		Description: "RF spectrum-analyzer display: waterfall + MHz/dB axes + colorbar + annotations + cursor readout (ADR-0091).",
		Init: func(ids *c.WidgetIdStack) (state any) {
			st := &spectrumDemoState{
				colBuf:   make([]float32, spectrumDemoHeightSlots),
				showLine: true,
			}
			cfg := colormap.NewConfig(colormap.Turbo8, -110, -20)
			cfg.UnderflowColor = color.NRGBA{A: 0xff}
			cfg.OverflowColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
			sd := spectrumdisplay.New(ids, "specdemo", cfg, spectrumDemoWidthSlots, spectrumDemoHeightSlots)
			sd.SetFrequencyAxis(spectrumdisplay.AxisSpec{Min: 88e6, Max: 108e6, Unit: spectrumdisplay.AxisUnitHertz})
			sd.SetPowerAxis(spectrumdisplay.AxisSpec{Min: -110, Max: -20, Unit: spectrumdisplay.AxisUnitDecibel, UnitLabel: "dBm"})
			sd.SetTimeAxis(spectrumdisplay.AxisSpec{Min: 0, Max: 6, Unit: spectrumdisplay.AxisUnitSeconds})
			sd.SetWaterfallRange(-110, -20)
			sd.SetLinePanelVisible(true)
			sd.SetMarkers([]spectrumdisplay.Marker{
				{Kind: spectrumdisplay.MarkerVertical, Freq: 98.5e6, Label: "tuned", Color: 0xffcc44ff},
			})
			sd.SetRegions([]spectrumdisplay.Region{
				{StartHz: 97e6, EndHz: 100e6, Label: "ch", Color: 0x44ff8833},
			})
			st.sd = sd
			state = st
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoSpectrumDisplay(ids, state.(*spectrumDemoState))
		},
	})
}

// spectrumDemoColumn fills out (in dB) with a noise floor near -100 dB plus a few
// fixed "stations" and one drifting carrier, computed purely from the frame counter
// so runs are reproducible. heatmapDemoPseudoRand is shared with the heatmapscroll demo.
func spectrumDemoColumn(out []float32, frame uint64) {
	h := float64(len(out) - 1)
	phase := float64(frame) * 0.02
	carrier := 0.52 + 0.06*math.Sin(phase*0.5)
	const w2 = 0.008 * 0.008
	fixed := []float64{0.12, 0.34, 0.72, 0.88}
	for i := range out {
		bin := float64(i) / h
		d := bin - carrier
		tone := math.Exp(-(d * d) / (2 * w2))
		for _, fs := range fixed {
			dd := bin - fs
			tone += 0.75 * math.Exp(-(dd*dd)/(2*w2))
		}
		noise := heatmapDemoPseudoRand(uint64(frame)<<20 | uint64(i))
		v := -100 + 6*(noise-0.5) + 78*math.Min(tone, 1.15)
		out[i] = float32(v)
	}
}

func demoSpectrumDisplay(ids *c.WidgetIdStack, st *spectrumDemoState) {
	st.frame++
	if !st.paused {
		spectrumDemoColumn(st.colBuf, st.frame)
		st.sd.PushColumn(st.colBuf)
	}
	for range c.Vertical().KeepIter() {
		c.Label("spectrumdisplay — waterfall + frequency/dB axes + colorbar + annotations (ADR-0091).").Send() // designlint:ignore=L1 (widget identifier; lowercase per Go package name)
		for range c.Horizontal().KeepIter() {
			c.Checkbox(ids.PrepareStr("sd-pause"), st.paused, "Pause stream").SendRespVal(&st.paused)
			c.AddSpace(padDefault())
			c.Checkbox(ids.PrepareStr("sd-line"), st.showLine, "Line panel").SendRespVal(&st.showLine)
			c.AddSpace(padDefault())
			r := st.sd.Readout()
			txt := "cursor: —"
			if r.Ok {
				txt = fmt.Sprintf("cursor: %.3f MHz  %.0f dB  %d cols ago", r.Freq/1e6, r.Db, r.Age)
			}
			c.Label(txt).Send()
		}
		c.AddSpace(padInner())
		st.sd.SetLinePanelVisible(st.showLine)
		st.sd.SetDisplaySize(840, 560)
		st.sd.Render()
	}
}
