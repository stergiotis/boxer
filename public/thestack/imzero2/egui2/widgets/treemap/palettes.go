//go:build llm_generated_opus47

package treemap

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
)

// Named palettes — 8-entry uint32 RGBA samples drawn from the IDS data-encoding
// LUTs (ADR-0031 §SD3) at evenly-spaced t ∈ [0, 1]. Use as the palette
// argument to DepthColoring, CategoricalColoring, or ContinuousColoring.
//
// Each constant samples a 256-entry IDS sequential LUT at 8 points and packs
// the per-channel bytes with a fixed 0xee alpha (~93%) so the cells match the
// surrounding chrome. The viridis-family IDS LUTs are the same matplotlib
// data (van der Walt & Smith 2014, CC0) the previous pre-IDS literal arrays
// were sampled from; visual character is preserved for the matplotlib palettes
// (Viridis8 / Magma8 / Inferno8 / Plasma8) and aligned with the IDS default
// for the Crameri palettes (Batlow8 — IDS sequential default per
// ADR-0031 §SD3).

// treemapAlpha is the per-cell opacity for palette-derived fills; matches
// the original 0xee value so cell rendering stays byte-identical to the
// pre-IDS palette.
const treemapAlpha uint8 = 0xee

// sample8 samples an IDS sequential palette at 8 evenly-spaced t ∈ [0, 1]
// points and packs each sample as 0xRRGGBBAA. Used at package init to
// materialise the per-palette constants below.
func sample8(p styletokens.SequentialE) (out []uint32) {
	out = make([]uint32, 8)
	for i := 0; i < 8; i++ {
		t := float32(i) / 7.0
		rgb := styletokens.Sequential(p, t)
		out[i] = uint32(rgb.R)<<24 | uint32(rgb.G)<<16 | uint32(rgb.B)<<8 | uint32(treemapAlpha)
	}
	return
}

// Batlow8 — IDS sequential default (Crameri, MIT). Perceptually-uniform,
// lightness-monotonic, CVD-safe; the Swiss-aligned default for any new
// treemap that doesn't specifically need a matplotlib-lineage palette.
var Batlow8 = sample8(styletokens.SequentialBatlow)

// Viridis8 — matplotlib viridis (van der Walt & Smith, CC0). Purple →
// teal → yellow. Sampled from the same matplotlib data the previous
// pre-IDS literal array used; values are nearly byte-identical.
var Viridis8 = sample8(styletokens.SequentialViridis)

// Magma8 — matplotlib magma. Black → purple → red → yellow. Higher
// contrast at the dark end than Viridis; works well on dark backgrounds.
var Magma8 = sample8(styletokens.SequentialMagma)

// Inferno8 — matplotlib inferno. Black → purple → orange → yellow.
// Similar to Magma but warmer.
var Inferno8 = sample8(styletokens.SequentialInferno)

// Plasma8 — matplotlib plasma. Purple → magenta → yellow. New to the
// IDS-aligned palette set (the pre-IDS palettes.go didn't expose
// plasma; the matplotlib data was always available via cmcrameri's
// vendored set, just not surfaced here).
var Plasma8 = sample8(styletokens.SequentialPlasma)

// Cividis8 — historical name retained for source-compat with
// colorscale_demo and other Cividis8 callers. Now backed by IDS Lapaz
// (Crameri, MIT, blue→yellow, CVD-safe), since cividis itself was
// omitted from the IDS v1 bundle per the ADR-0033 amendment
// (cividis is in neither cmcrameri nor the dim13/colormap upstream).
// Lapaz preserves the "explicitly CVD-safe sequential" intent.
var Cividis8 = sample8(styletokens.SequentialLapaz)

// DefaultDepthColors aliases Batlow8 — the IDS sequential default
// (ADR-0031 §SD3). Previously aliased Viridis8 (pre-IDS); the alias
// rotation aligns the treemap default with the fleet-wide design system.
// Callers wanting the previous behaviour can name Viridis8 explicitly.
var DefaultDepthColors = Batlow8
