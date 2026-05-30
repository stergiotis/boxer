package imzrt

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Named colours sourced from the IDS semantic palette (ADR-0031) so imzrt reads
// as part of the same visual system as imztop and the carousel. Per ADR-0061
// SD14 the palette is intentionally small; egui-native dark renders the rest.
var (
	colorGood          = color.Hex(styletokens.SuccessDefault.AsHex())
	colorWarn          = color.Hex(styletokens.WarningDefault.AsHex())
	colorHot           = color.Hex(styletokens.ErrorDefault.AsHex())
	colorBgClear       = color.Transparent
	colorMetricPrimary = color.Hex(styletokens.InfoDefault.AsHex())
)

// qualitativeColor returns the idx-th colour of the IDS qualitative cycle
// (batlowS — ADR-0031 §SD3). idx wraps inside the accessor.
func qualitativeColor(idx int) (cl color.Color) {
	cl = color.Hex(styletokens.QualitativeCycle(idx).AsHex())
	return
}

// bandColor is the colour for memory-class band i (0=objects … 4=other). Band i
// keeps colour i across the legend, the swatches, and the stacked-area fill.
func bandColor(i int) (cl color.Color) {
	cl = qualitativeColor(i)
	return
}

// thresholdColor maps a 0..100 percentage to a named colour: good below 60, warn
// 60..80, hot above 80. Used to tint the headroom-to-GOMEMLIMIT gauge.
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

// sequentialPalette resamples the active IDS sequential palette
// (styletokens.SequentialDefault — ADR-0031 §SD3) into the 0xRRGGBBAA stop list
// colormap.Config consumes, so the spectrogram honours the same
// IDS_PALETTE_SEQUENTIAL / IDS_ACCESSIBILITY knobs as every other keelson heatmap.
func sequentialPalette() (palette []uint32) {
	s := styletokens.SequentialDefault()
	const stops = 256
	palette = make([]uint32, stops)
	for i := range palette {
		t := float32(i) / float32(stops-1)
		rgba := styletokens.Sequential(s, t)
		palette[i] = uint32(rgba.R)<<24 | uint32(rgba.G)<<16 | uint32(rgba.B)<<8 | uint32(rgba.A)
	}
	return
}

// Density-aware spacing (IDS spacing tokens at the active preset, ADR-0032 §SD2).
func (inst *App) spaceInner() (px float32) { px = styletokens.PaddingInner(inst.density); return }
func (inst *App) spaceTight() (px float32) { px = styletokens.PaddingTight(inst.density); return }
