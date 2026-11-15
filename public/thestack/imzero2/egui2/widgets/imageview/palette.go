package imageview

import (
	"slices"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/science/imaging/display"
)

// The colour maps a single plane can be shown through (ADR-0373 §SD5): the
// plain grey ramp, and the design system's sequential and diverging scales.
// Nothing else is offered.
var sequentialScales = map[string]styletokens.SequentialE{
	"batlow":  styletokens.SequentialBatlow,
	"batlowk": styletokens.SequentialBatlowK,
	"lapaz":   styletokens.SequentialLapaz,
	"oslo":    styletokens.SequentialOslo,
	"lajolla": styletokens.SequentialLajolla,
	"viridis": styletokens.SequentialViridis,
	"magma":   styletokens.SequentialMagma,
	"plasma":  styletokens.SequentialPlasma,
	"inferno": styletokens.SequentialInferno,
	"grayc":   styletokens.SequentialGrayC,
}

var divergingScales = map[string]styletokens.DivergingE{
	"vik":  styletokens.DivergingVik,
	"roma": styletokens.DivergingRoma,
	"broc": styletokens.DivergingBroc,
	"cork": styletokens.DivergingCork,
}

// PaletteGray is the name of the plain grey ramp; the empty name means it
// too.
const PaletteGray = "gray"

// PaletteNames lists the colour maps [display.Recipe.Palette] may name, the
// grey ramp first.
func PaletteNames() []string {
	names := make([]string, 0, 1+len(sequentialScales)+len(divergingScales))
	for name := range sequentialScales {
		names = append(names, name)
	}
	for name := range divergingScales {
		names = append(names, name)
	}
	slices.Sort(names)
	return append([]string{PaletteGray}, names...)
}

// paletteFor resolves a recipe's palette name. An unknown name reports ok
// false; the caller keeps what it had.
func paletteFor(name string) (palette *display.Palette, ok bool) {
	palette = display.GrayPalette()
	if name != "" && name != PaletteGray {
		seq, isSeq := sequentialScales[name]
		div, isDiv := divergingScales[name]
		if !isSeq && !isDiv {
			return nil, false
		}
		for i := range palette.Colors {
			t := float32(i) / 255
			if isSeq {
				palette.Colors[i] = styletokens.Sequential(seq, t).AsHex()
			} else {
				palette.Colors[i] = styletokens.Diverging(div, 2*t-1).AsHex()
			}
		}
	}
	// A value flagged as below or above the limits takes the two ends of the
	// diverging default, which no sequential ramp and no grey contains.
	palette.Under = styletokens.Diverging(styletokens.DivergingVik, -1).AsHex()
	palette.Over = styletokens.Diverging(styletokens.DivergingVik, 1).AsHex()
	return palette, true
}
