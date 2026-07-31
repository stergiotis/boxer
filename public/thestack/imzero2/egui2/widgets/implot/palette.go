package implot

import "github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"

// PaletteE selects the data-series palette: the color a series takes from
// its slot, and the swatch its legend row draws. Independent of the chrome
// palette (chrome.go) — chrome is the frame around the data, this is the
// data. Both are ten-entry cycles, so a slot maps the same way under either.
type PaletteE uint8

const (
	// PaletteIDS cycles the IDS qualitative data-encoding palette
	// (styletokens.QualitativeCycle — Crameri batlowS, ADR-0031 §SD7).
	// The default: series identity is a data encoding, and the fleet reads
	// its qualitative colors from one source.
	PaletteIDS PaletteE = iota
	// PaletteDeep is ImPlot's default colormap ("Deep", seaborn's deep 10),
	// the palette the port shipped with. Kept selectable for continuity
	// with upstream captures, as ChromeClassic is for the chrome.
	PaletteDeep
)

// paletteDeep is upstream's "Deep" table, cycled by series declaration
// order.
var paletteDeep = [10]uint32{
	0x4c72b0ff, 0xdd8452ff, 0x55a868ff, 0xc44e52ff, 0x8172b3ff,
	0x937860ff, 0xda8bc3ff, 0x8c8c8cff, 0xccb974ff, 0x64b5cdff,
}

// seriesPalette is the active table, assigned by SetSeriesPalette (package
// init applies PaletteIDS). Packed 0xRRGGBBAA, as color.Hex expects.
var seriesPalette [10]uint32

func init() { SetSeriesPalette(PaletteIDS) }

// SetSeriesPalette switches the data-series palette. Same contract as
// SetChrome: process-wide, called at application startup (or between
// frames), not safe against a concurrently rendering plot. A per-series
// SetNextColor still overrides whichever palette is active.
func SetSeriesPalette(palette PaletteE) {
	if palette == PaletteDeep {
		seriesPalette = paletteDeep
		return
	}
	for i := range seriesPalette {
		seriesPalette[i] = styletokens.QualitativeCycle(i).AsHex()
	}
}

// seriesColor resolves a series' palette slot to its color, cycling once
// the slots outrun the palette.
func seriesColor(slot int) (hex uint32) {
	hex = seriesPalette[slot%len(seriesPalette)]
	return
}
