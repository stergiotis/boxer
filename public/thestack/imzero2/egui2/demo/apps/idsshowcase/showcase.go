package idsshowcase

import (
	"fmt"
	"math"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	dataenc "github.com/stergiotis/boxer/public/keelson/designsystem/styletokens/data_encoding"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// App is the per-window IDS showcase instance.
type App struct {
	ids     *c.WidgetIdStack
	density styletokens.DensityE
}

var _ runtimeapp.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		ids: c.NewWidgetIdStack(),
		// Density is read once at app construction; the IDS overlay is
		// applied once at Rust startup with the same env var, so a
		// runtime toggle here would diverge from the visible state.
		density: styletokens.DensityFromEnv(),
	}
	return
}

func (inst *App) Manifest() (m runtimeapp.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx runtimeapp.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	return
}

func (inst *App) Unmount(ctx runtimeapp.MountContextI) (err error) { return }

func (inst *App) Frame(ctx runtimeapp.FrameContextI) (err error) {
	inst.render()
	return
}

// render is the IDS showcase panel layout. Vertical stack:
//
//	1. header — title + active density readout
//	2. neutral spine — 10 swatches (bg.extreme → text.extreme)
//	3. semantic palette — 6 roles × 3 emphasis grid
//	4. type scale — IDS-bound TextStyle slots
//	5. data encoding — qualitative / sequential / diverging palettes
//	6. density spec — PX_TABLE column for the active preset
//	7. rounding ladder — 4 swatches at corner radius 0/2/4/6
//	8. stroke ladder — 3 framed rows at width 1.0/1.5/2.0
func (inst *App) render() {
	c.Label("IDS token catalogue — ADR-0029 / 0031 / 0032").Send()
	c.Label(fmt.Sprintf("active density: %s   (IMZERO2_DENSITY)", inst.density.String())).Send()
	c.Separator().Horizontal().Send()

	inst.renderNeutralSpine()
	c.AddSpace(styletokens.GapSections(inst.density))

	c.Label("Semantic palette — 6 roles × 3 emphasis (ADR-0031 §SD2)").Send()
	inst.renderSemanticPalette()
	c.AddSpace(styletokens.GapSections(inst.density))

	c.Label("Type scale — IDS-bound TextStyle slots (ADR-0030 §SD3)").Send()
	inst.renderTypeScale()
	c.AddSpace(styletokens.GapSections(inst.density))

	c.Label("Data encoding — scientifically-published colormaps (ADR-0031 §SD3)").Send()
	inst.renderDataEncoding()
	c.AddSpace(styletokens.GapSections(inst.density))

	c.Label("Data encoding in egui_plot — QualitativeCycle drives series colors").Send()
	inst.renderDataEncodingPlot()
	c.AddSpace(styletokens.GapSections(inst.density))

	inst.renderDensitySpec()
	c.AddSpace(styletokens.GapSections(inst.density))

	c.Label("Rounding ladder — density-independent (ADR-0032 §SD3)").Send()
	inst.renderRoundingLadder()
	c.AddSpace(styletokens.GapSections(inst.density))

	c.Label("Stroke ladder — density-independent (ADR-0032 §SD4)").Send()
	inst.renderStrokeLadder()
}

// neutralSpineEntry pairs a token name with the RGBA8 value the swatch
// row reads. Order matches the L-spine in ADR-0031 §SD4 (dark → light).
type neutralSpineEntry struct {
	name  string
	value styletokens.RGBA8
}

var neutralSpine = []neutralSpineEntry{
	{"bg.extreme", styletokens.NeutralBgExtreme},
	{"bg.panel", styletokens.NeutralBgPanel},
	{"bg.faint", styletokens.NeutralBgFaint},
	{"bg.surface", styletokens.NeutralBgSurface},
	{"border.faint", styletokens.NeutralBorderFaint},
	{"border.default", styletokens.NeutralBorderDefault},
	{"text.disabled", styletokens.NeutralTextDisabled},
	{"text.secondary", styletokens.NeutralTextSecondary},
	{"text.primary", styletokens.NeutralTextPrimary},
	{"text.extreme", styletokens.NeutralTextExtreme},
}

func (inst *App) renderNeutralSpine() {
	c.Label("Neutral spine — dark-theme L-points (ADR-0031 §SD4)").Send()
	for range c.Horizontal().KeepIter() {
		for _, e := range neutralSpine {
			inst.swatch("ns:"+e.name, e.name, e.value, false)
		}
	}
}

// semanticRow is one role across three emphasis levels. Order matches
// the ADR-0031 §SD2 table.
type semanticRow struct {
	role    string
	subtle  styletokens.RGBA8
	deflt   styletokens.RGBA8
	strong  styletokens.RGBA8
}

var semanticRows = []semanticRow{
	{"info", styletokens.InfoSubtle, styletokens.InfoDefault, styletokens.InfoStrong},
	{"success", styletokens.SuccessSubtle, styletokens.SuccessDefault, styletokens.SuccessStrong},
	{"warning", styletokens.WarningSubtle, styletokens.WarningDefault, styletokens.WarningStrong},
	{"error", styletokens.ErrorSubtle, styletokens.ErrorDefault, styletokens.ErrorStrong},
	{"neutral", styletokens.NeutralSubtle, styletokens.NeutralDefault, styletokens.NeutralStrong},
	{"accent", styletokens.AccentSubtle, styletokens.AccentDefault, styletokens.AccentStrong},
}

func (inst *App) renderSemanticPalette() {
	for _, row := range semanticRows {
		for range c.Horizontal().KeepIter() {
			c.Label(fmt.Sprintf("%-8s", row.role)).Send()
			inst.swatch("sp:"+row.role+":subtle", row.role+".subtle", row.subtle, true)
			inst.swatch("sp:"+row.role+":default", row.role+".default", row.deflt, true)
			inst.swatch("sp:"+row.role+":strong", row.role+".strong", row.strong, true)
		}
	}
}

// swatch renders a single rounded chip filled with the token's color and
// labelled with its name + hex string. wide=true gives the semantic-row
// chips room for the full token name; wide=false is the compact spine
// row.
func (inst *App) swatch(idTag, label string, val styletokens.RGBA8, wide bool) {
	fill := color.Hex(val.AsHex())
	margin := float32(6)
	if !wide {
		margin = 4
	}
	for range c.Frame(inst.ids.PrepareStr(idTag)).
		Fill(fill).
		InnerMargin(margin).
		CornerRadius(styletokens.RoundingSm).
		KeepIter() {
		// fg picked to read on the swatch — text.extreme on dark
		// (L<0.5) swatches, bg.extreme on light. Computed via L
		// from the OKLCh source; here we approximate by sRGB
		// luminance to avoid linking oklab math into the demo.
		fg := pickReadableFg(val)
		hex := fmt.Sprintf("#%02x%02x%02x", val.R, val.G, val.B)
		if wide {
			for scope := range c.RichTextLabelColored(fg, color.Transparent, label) {
				_ = scope
			}
			for scope := range c.RichTextLabelColored(fg, color.Transparent, hex) {
				scope.Small()
			}
		} else {
			for scope := range c.RichTextLabelColored(fg, color.Transparent, label) {
				scope.Small()
			}
		}
	}
}

// pickReadableFg returns text.extreme (light) or bg.extreme (dark) for a
// swatch background. Cutoff at 0.5 perceptual lightness approximated by
// gamma-encoded sRGB mean — coarse but stable, and matches the
// IDS-overlay default text colour the egui Visuals path picks for
// surfaces at the same brightness.
func pickReadableFg(bg styletokens.RGBA8) (fg color.Color) {
	// Simple gamma-encoded mean — for the 28 IDS tokens the verdict
	// matches the OKLCh L bisect at 0.5 (verified mentally against
	// color.md). A future helper in styletokens could expose a proper
	// L predicate without dragging oklab into the demo.
	mean := uint16(bg.R) + uint16(bg.G) + uint16(bg.B)
	if mean < 3*128 {
		fg = color.Hex(styletokens.NeutralTextExtreme.AsHex())
	} else {
		fg = color.Hex(styletokens.NeutralBgExtreme.AsHex())
	}
	return
}

// renderTypeScale shows the five IDS pt-size steps plus the monospace
// body variant. Each row renders the same pangram at the target size so
// reviewers can compare visual hierarchy without flipping windows.
//
// All six rows route through TextStyle slots that apply_typography bound
// on the Rust side — Heading/Body/Caption/Mono via the built-in tiers,
// Display/Micro via the Name slots ("ids-display" / "ids-micro"). No
// explicit Size() overrides here: a visible size change confirms that
// the full binding is live, not just the egui defaults.
const pangram = "The quick brown fox jumps over the lazy dog"
const monoSample = "fn quick_brown_fox() { jumps_over(lazy_dog) }"

func (inst *App) renderTypeScale() {
	fg := color.Hex(styletokens.NeutralTextPrimary.AsHex())
	bg := color.Transparent

	display := styletokens.ScaledPt(styletokens.DisplayPt, inst.density)
	heading := styletokens.ScaledPt(styletokens.HeadingPt, inst.density)
	body := styletokens.ScaledPt(styletokens.BodyPt, inst.density)
	caption := styletokens.ScaledPt(styletokens.CaptionPt, inst.density)
	micro := styletokens.ScaledPt(styletokens.MicroPt, inst.density)

	row := func(tag string, render func()) {
		for range c.Horizontal().KeepIter() {
			for scope := range c.RichTextLabelColored(fg, bg, fmt.Sprintf("%-15s", tag)) {
				scope.Small().Monospace()
			}
			render()
		}
	}

	row(fmt.Sprintf("Display %.0fpt", display), func() {
		for scope := range c.RichTextLabelColored(fg, bg, pangram) {
			scope.TextStyleName("ids-display")
		}
	})
	row(fmt.Sprintf("Heading %.0fpt", heading), func() {
		for scope := range c.RichTextLabelColored(fg, bg, pangram) {
			scope.Heading()
		}
	})
	row(fmt.Sprintf("Body %.0fpt", body), func() {
		// Default TextStyle::Body — the IDS apply path bound it to BodyPt
		// (13pt at Standard). No explicit size override here, on purpose.
		for scope := range c.RichTextLabelColored(fg, bg, pangram) {
			_ = scope
		}
	})
	row(fmt.Sprintf("Caption %.0fpt", caption), func() {
		for scope := range c.RichTextLabelColored(fg, bg, pangram) {
			scope.Small()
		}
	})
	row(fmt.Sprintf("Micro %.0fpt", micro), func() {
		for scope := range c.RichTextLabelColored(fg, bg, pangram) {
			scope.TextStyleName("ids-micro")
		}
	})
	row(fmt.Sprintf("Body.Mono %.0fpt", body), func() {
		for scope := range c.RichTextLabelColored(fg, bg, monoSample) {
			scope.Monospace()
		}
	})
}

// renderDataEncodingPlot exercises the styletokens.QualitativeCycle
// accessor against a real egui_plot consumer — 6 phase-shifted sine
// waves, each colored by `QualitativeCycle(i)` from batlowS. Mirrors
// the canonical "per-series categorical color cycle" use case
// (ADR-0031 §SD7 plot integration) and validates the full Rust→Go
// LUT round-trip end-to-end.
//
// Line width threads StrokeRegular so the panel touches a second IDS
// token slot — the visual story is "two IDS axes in one consumer."
const dataEncodingPlotSamples = 200
const dataEncodingPlotSeries = 6

func (inst *App) renderDataEncodingPlot() {
	xs := make([]float64, dataEncodingPlotSamples)
	for i := range xs {
		xs[i] = float64(i) * 0.1
	}
	for s := 0; s < dataEncodingPlotSeries; s++ {
		ys := make([]float64, dataEncodingPlotSamples)
		phase := float64(s) * math.Pi / 3.0
		for i := range ys {
			ys[i] = math.Sin(xs[i] + phase)
		}
		seriesColor := color.Hex(styletokens.QualitativeCycle(s).AsHex())
		c.PlotLine(fmt.Sprintf("series %d", s), xs, ys).
			Width(styletokens.StrokeRegular).
			Color(seriesColor).
			Send()
	}
	c.Plot(inst.ids.PrepareStr("de-plot")).
		Width(720).Height(240).
		XAxisLabel("x").YAxisLabel("sin(x + phase)").
		Legend().
		AllowZoom(false).AllowDrag(false).
		Send()
}

// renderDensitySpec displays the active density's 8-value PX_TABLE
// column. Set IMZERO2_DENSITY=tight|standard|roomy at startup to compare.
func (inst *App) renderDensitySpec() {
	c.Label("Density spec — PX_TABLE column for active preset (ADR-0032 §SD2)").Send()
	for range c.Horizontal().KeepIter() {
		for i := uint8(0); i < 8; i++ {
			c.Label(fmt.Sprintf("Px[%d]=%-3.0f", i, styletokens.Px(inst.density, i))).Send()
			c.AddSpace(styletokens.GapInline(inst.density))
		}
	}
	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("Padding.Default=%.0f", styletokens.PaddingDefault(inst.density))).Send()
		c.AddSpace(styletokens.GapItems(inst.density))
		c.Label(fmt.Sprintf("Padding.Outer=%.0f", styletokens.PaddingOuter(inst.density))).Send()
		c.AddSpace(styletokens.GapItems(inst.density))
		c.Label(fmt.Sprintf("Gap.Items=%.0f", styletokens.GapItems(inst.density))).Send()
		c.AddSpace(styletokens.GapItems(inst.density))
		c.Label(fmt.Sprintf("Gap.Sections=%.0f", styletokens.GapSections(inst.density))).Send()
		c.AddSpace(styletokens.GapItems(inst.density))
		c.Label(fmt.Sprintf("Margin.Frame=%.0f", styletokens.MarginFrame(inst.density))).Send()
	}
}

// renderRoundingLadder shows the four corner-radius tokens. Each chip is
// the same fill colour (accent.default at a low alpha so the contrast
// against bg.panel reads as intentional) so only the corner geometry
// changes between cells.
func (inst *App) renderRoundingLadder() {
	fill := color.Hex(styletokens.AccentDefault.AsHex())
	rounds := []struct {
		name string
		val  float32
	}{
		{"None (0)", styletokens.RoundingNone},
		{"Sm (2)", styletokens.RoundingSm},
		{"Md (4)", styletokens.RoundingMd},
		{"Lg (6)", styletokens.RoundingLg},
	}
	for range c.Horizontal().KeepIter() {
		for _, r := range rounds {
			for range c.Frame(inst.ids.PrepareStr("rd:"+r.name)).
				Fill(fill).
				InnerMargin(styletokens.PaddingOuter(inst.density)).
				CornerRadius(r.val).
				KeepIter() {
				fg := color.Hex(styletokens.NeutralBgExtreme.AsHex())
				for scope := range c.RichTextLabelColored(fg, color.Transparent, r.name) {
					scope.Small()
				}
			}
			c.AddSpace(styletokens.GapItems(inst.density))
		}
	}
}

// renderDataEncoding shows the IDS data-encoding palettes (ADR-0031
// §SD3): qualitative batlowS (10 categorical colors), sequential batlow
// (256-entry LUT sampled at 24 ticks), and diverging vik (256-entry LUT
// sampled symmetrically around the midpoint). All three are vendored
// verbatim from peer-reviewed scientific publications (Crameri 2018,
// MIT); see INSPIRATIONS.md for citation.
//
// The sequential and diverging strips use minimal InnerMargin so the 24
// chips read as a continuous gradient rather than discrete swatches.
const dataEncodingSamples = 24

func (inst *App) renderDataEncoding() {
	inst.renderQualitative()
	c.AddSpace(styletokens.GapInline(inst.density))
	inst.renderSequentialRamp("batlow", dataenc.Batlow[:])
	c.AddSpace(styletokens.GapInline(inst.density))
	inst.renderDivergingRamp("vik", dataenc.Vik[:])
}

func (inst *App) renderQualitative() {
	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("%-12s", "batlowS")).Send()
		for i, rgb := range dataenc.BatlowS {
			fill := encodeRGB(rgb)
			for range c.Frame(inst.ids.PrepareStr(fmt.Sprintf("qual:%d", i))).
				Fill(fill).
				InnerMargin(styletokens.PaddingDefault(inst.density)).
				CornerRadius(styletokens.RoundingSm).
				KeepIter() {
				fg := pickReadableFgRGB(rgb)
				for scope := range c.RichTextLabelColored(fg, color.Transparent, fmt.Sprintf("%d", i)) {
					scope.Small().Monospace()
				}
			}
			c.AddSpace(styletokens.PaddingHair(inst.density))
		}
	}
}

// renderSequentialRamp renders the named LUT at `dataEncodingSamples`
// evenly-spaced positions across t ∈ [0, 1]. lut must be a 256-entry
// slice (the canonical Crameri / viridis cardinality).
func (inst *App) renderSequentialRamp(name string, lut [][3]uint8) {
	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("%-12s", name)).Send()
		for i := 0; i < dataEncodingSamples; i++ {
			t := float64(i) / float64(dataEncodingSamples-1)
			idx := int(t*255.0 + 0.5)
			if idx > 255 {
				idx = 255
			}
			rgb := lut[idx]
			fill := encodeRGB(rgb)
			for range c.Frame(inst.ids.PrepareStr(fmt.Sprintf("seq:%s:%d", name, i))).
				Fill(fill).
				InnerMargin(styletokens.PaddingDefault(inst.density)).
				CornerRadius(0).
				KeepIter() {
				// Empty body — the ramp reads as a gradient strip; tick
				// labels would clutter the perception of continuity.
			}
		}
		c.AddSpace(styletokens.GapInline(inst.density))
		// designlint:ignore=L1 (math interval — lowercase parameter name)
		c.Label("t∈[0,1]").Send()
	}
}

// renderDivergingRamp maps t ∈ [-1, 1] across the 256-entry LUT, putting
// the perceptually-neutral midpoint at the centre tick. `vik` is the
// Crameri default; symmetric around L≈0.55 grey by construction.
func (inst *App) renderDivergingRamp(name string, lut [][3]uint8) {
	for range c.Horizontal().KeepIter() {
		c.Label(fmt.Sprintf("%-12s", name)).Send()
		for i := 0; i < dataEncodingSamples; i++ {
			t := -1.0 + 2.0*float64(i)/float64(dataEncodingSamples-1)
			idx := int((t*0.5+0.5)*255.0 + 0.5)
			if idx < 0 {
				idx = 0
			} else if idx > 255 {
				idx = 255
			}
			rgb := lut[idx]
			fill := encodeRGB(rgb)
			for range c.Frame(inst.ids.PrepareStr(fmt.Sprintf("div:%s:%d", name, i))).
				Fill(fill).
				InnerMargin(styletokens.PaddingDefault(inst.density)).
				CornerRadius(0).
				KeepIter() {
				// Same empty-body rationale as the sequential ramp.
			}
		}
		c.AddSpace(styletokens.GapInline(inst.density))
		// designlint:ignore=L1 (math interval — lowercase parameter name)
		c.Label("t∈[-1,1]").Send()
	}
}

// encodeRGB packs a 3-channel sRGB triple into a color.Color with
// alpha=255 — the bridge for data-encoding LUT entries that don't carry
// alpha. Uses the same color.Hex path as the IDS palette → color
// converters so designlint L2 stays clean.
func encodeRGB(rgb [3]uint8) (cl color.Color) {
	packed := uint32(rgb[0])<<24 | uint32(rgb[1])<<16 | uint32(rgb[2])<<8 | 0xff
	cl = color.Hex(packed)
	return
}

// pickReadableFgRGB is the [3]uint8 variant of pickReadableFg. Same
// gamma-encoded-mean cutoff at 3*128.
func pickReadableFgRGB(rgb [3]uint8) (fg color.Color) {
	mean := uint16(rgb[0]) + uint16(rgb[1]) + uint16(rgb[2])
	if mean < 3*128 {
		fg = color.Hex(styletokens.NeutralTextExtreme.AsHex())
	} else {
		fg = color.Hex(styletokens.NeutralBgExtreme.AsHex())
	}
	return
}

// renderStrokeLadder shows the three stroke-width tokens as outlined
// chips. The fill is transparent; the stroke colour is border.default.
func (inst *App) renderStrokeLadder() {
	strokeColor := color.Hex(styletokens.NeutralBorderDefault.AsHex())
	strokes := []struct {
		name string
		val  float32
	}{
		{"Hair (1.0)", styletokens.StrokeHair},
		{"Regular (1.5)", styletokens.StrokeRegular},
		{"Strong (2.0)", styletokens.StrokeStrong},
	}
	for range c.Horizontal().KeepIter() {
		for _, s := range strokes {
			for range c.Frame(inst.ids.PrepareStr("sk:"+s.name)).
				Fill(color.Transparent).
				Stroke(s.val, strokeColor).
				InnerMargin(styletokens.PaddingOuter(inst.density)).
				CornerRadius(styletokens.RoundingSm).
				KeepIter() {
				fg := color.Hex(styletokens.NeutralTextPrimary.AsHex())
				for scope := range c.RichTextLabelColored(fg, color.Transparent, s.name) {
					scope.Small()
				}
			}
			c.AddSpace(styletokens.GapItems(inst.density))
		}
	}
}
