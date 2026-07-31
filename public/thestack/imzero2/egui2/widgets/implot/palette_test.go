package implot

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
)

// Both palettes are always compiled and asserted here, as chrome_test.go
// does for the chrome pair.
func TestSetSeriesPalette(t *testing.T) {
	defer SetSeriesPalette(PaletteIDS) // restore the default for other tests

	SetSeriesPalette(PaletteDeep)
	if got, want := seriesColor(0), uint32(0x4c72b0ff); got != want {
		t.Errorf("deep slot 0 = %#x, want %#x", got, want)
	}
	if got, want := seriesColor(9), uint32(0x64b5cdff); got != want {
		t.Errorf("deep slot 9 = %#x, want %#x", got, want)
	}

	SetSeriesPalette(PaletteIDS)
	// The IDS branch must agree with the token accessor at *every* slot,
	// including past the wrap — the two cycle at the same length or they
	// disagree about which series shares a color with which.
	for slot := range 25 {
		want := styletokens.QualitativeCycle(slot).AsHex()
		if got := seriesColor(slot); got != want {
			t.Errorf("ids slot %d = %#x, want %#x", slot, got, want)
		}
	}

	// Slots past the cycle wrap rather than run off the table.
	n := styletokens.QualitativeCycleLen
	if got, want := seriesColor(n+3), seriesColor(3); got != want {
		t.Errorf("slot %d = %#x, want the slot-3 color %#x", n+3, got, want)
	}
}

// The box-zoom rect takes the accent role precisely so it cannot collide
// with a series color (ADR-0031: annotations semantic, series qualitative).
// ChromeClassic is deliberately out of scope — its selection rect *is*
// Deep's first color, which is the reason the IDS chrome moved it.
func TestBoxZoomIsNotASeriesColor(t *testing.T) {
	defer SetSeriesPalette(PaletteIDS)
	defer SetChrome(ChromeIDS)
	SetChrome(ChromeIDS)

	for _, palette := range []PaletteE{PaletteIDS, PaletteDeep} {
		SetSeriesPalette(palette)
		for slot := range 10 {
			if seriesColor(slot)&^0xff == colBoxStroke&^0xff {
				t.Errorf("palette %d slot %d shares the box-zoom color %#x",
					palette, slot, colBoxStroke)
			}
		}
	}
}
