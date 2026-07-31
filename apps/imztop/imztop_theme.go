package imztop

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Named colours used across panels for threshold cues. Sourced from the
// IDS semantic palette (ADR-0031 §SD2) so the imztop chrome reads as
// part of the same visual system as the carousel, logviewer, and any
// future IDS consumer. Per ADR-0020 SD8 the palette is intentionally
// small; egui-native dark renders the rest. No theme-file parser, no
// theme switcher.
var (
	colorGood    = color.Hex(styletokens.SuccessDefault.AsHex())
	colorWarn    = color.Hex(styletokens.WarningDefault.AsHex())
	colorHot     = color.Hex(styletokens.ErrorDefault.AsHex())
	colorBgClear = color.Transparent

	// colorGridLine is the IDS-neutral reference-line / 100% threshold
	// color used by every panel's egui_plot pane. NeutralBorderFaint
	// (L≈0.26) reads as a faint divider against bg.panel; replaces the
	// pre-IDS 0x44444488 ad-hoc grey.
	colorGridLine = color.Hex(styletokens.NeutralBorderFaint.AsHex())

	// colorMetricPrimary is the line color for a panel's primary
	// scalar series — CPU-total, Mem-used, etc. Routes through
	// styletokens.InfoDefault so single-metric panels read as the
	// "informational" status family.
	colorMetricPrimary = color.Hex(styletokens.InfoDefault.AsHex())

	// colorCursor is the high-contrast indicator color used by the
	// CPU heatmap's hover-cursor strip. NeutralTextExtreme (a near-
	// white) gives the cursor maximal pop against any palette stop
	// it crosses while still sourcing from the IDS catalogue.
	colorCursor = color.Hex(styletokens.NeutralTextExtreme.AsHex())

	// colorAxisTick / colorAxisLabel style the CPU heatmap's time-axis
	// tick marks and labels. Mirror imzrt's spectrogram axis styling so
	// the two dashboards' scrolling-heatmap chrome reads identically: a
	// faint IDS border for the ticks, secondary text for the labels.
	colorAxisTick  = color.Hex(styletokens.NeutralBorderFaint.AsHex())
	colorAxisLabel = color.Hex(styletokens.NeutralTextSecondary.AsHex())
)

// withAlpha overrides the alpha byte of an IDS palette token while
// keeping its RGB, returning a color.Color suitable for the
// translucent ProgressBar fills used by the disk / mem / battery
// panels. Bridges styletokens (opaque-by-design semantic tokens) to
// imztop's "tinted fill" idiom until the IDS designsystem ships an
// alpha-aware token path (ADR-0029 §SD12).
//
// Layout matches RGBA8.AsHex (0xRRGGBBAA) and color.Hex's expected
// input — same pattern the CPU heatmap uses inline at line 235 to
// re-alpha colormap palette stops.
func withAlpha(token styletokens.RGBA8, alpha uint8) (cl color.Color) {
	cl = color.Hex((token.AsHex() & 0xffffff00) | uint32(alpha))
	return
}

// qualitativeColor returns the idx-th color of the IDS qualitative
// cycle (Okabe-Ito — see ADR-0156) as a color.Color. Used for
// categorical series colors: per-core CPU lines, per-device disk / GPU
// markers, Rx/Tx pairs in network panels. idx wraps inside the
// styletokens accessor; callers don't need a modulo guard.
func qualitativeColor(idx int) (cl color.Color) {
	cl = color.Hex(styletokens.QualitativeCycle(idx).AsHex())
	return
}

// markerColor returns a pick from the qualitative cycle for tiny
// categorical markers (the per-device "●" prefix in disk / net / GPU
// panels). It is now a straight pass-through to qualitativeColor.
//
// It used to reshuffle the cycle so bright stops came first, because
// the previous palette (batlowS) opened with a near-black navy that
// disappeared under a single glyph. That reorder only deferred the
// problem — a caller with 8+ series still reached the dark stops — and
// its stated reason for not reshuffling plot lines ("a 1.2 px stroke
// carries enough pixels that even the dark stops read fine") did not
// hold: the navy measured 1.00:1 against the plot surface, the same
// luminance as the background, so no stroke width could rescue it.
// ADR-0156 fixed the palette instead; every entry now clears 3:1.
//
// Kept as a named seam rather than inlined: markers and lines have
// different legibility constraints, and a future divergence should have
// somewhere to live.
func markerColor(idx int) (cl color.Color) {
	cl = qualitativeColor(idx)
	return
}

// Density-aware spacing helpers — IDS spacing tokens at the active
// density (cached once at newApp from styletokens.DensityFromEnv()).
// Naming shortens the styletokens accessor names so
// c.AddSpace(inst.spaceItems()) stays legible inside chained widget
// builders. ADR-0032 §SD2.
//
// Every panel's intra-panel rhythm flows through these methods; the
// previous spacingRowGap / spacingSectionGap / spacingPanelGap named
// consts were retired in favour of the named-token routes below so
// `IMZERO2_DENSITY=tight` / `=roomy` retunes the whole app uniformly.
func (inst *App) spaceHair() (px float32)   { px = styletokens.PaddingHair(inst.density); return }
func (inst *App) spaceInner() (px float32)  { px = styletokens.PaddingInner(inst.density); return }
func (inst *App) spaceTight() (px float32)  { px = styletokens.PaddingTight(inst.density); return }
func (inst *App) spaceInline() (px float32) { px = styletokens.GapInline(inst.density); return }
func (inst *App) spaceItems() (px float32)  { px = styletokens.GapItems(inst.density); return }
func (inst *App) spaceOuter() (px float32)  { px = styletokens.PaddingOuter(inst.density); return }

// thresholdColor maps a 0..100 percentage to a named colour: good
// below 60, warn 60..80, hot above 80. Returned colour is suitable
// for RichTextLabelColored's foreground argument.
func thresholdColor(pct float32) (cl color.Color) {
	switch {
	case pct >= 80:
		cl = colorHot
	case pct >= 60:
		cl = colorWarn
	default:
		cl = colorGood
	}
	return
}
