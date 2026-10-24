package implot

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
)

// Both palettes are always compiled and asserted here — the anti-rot
// property a build-tag fork would not have had.
func TestSetChromePalettes(t *testing.T) {
	defer SetChrome(ChromeIDS) // restore the default for other tests

	SetChrome(ChromeClassic)
	classic := map[string][2]uint32{
		"plotBg":       {colPlotBg, 0x111318ff},
		"areaBg":       {colAreaBg, 0x1a1d24ff},
		"border":       {colBorder, 0x3a3f4bff},
		"tickLabel":    {colTickLabel, 0xaab2c0ff},
		"boxStroke":    {colBoxStroke, 0x4c72b0cc},
		"legendBg":     {colLegendBg, 0x14171dee},
		"contrastDark": {colContrastDark, 0x10131aff},
		"contrastLite": {colContrastLite, 0xe6e9eeff},
	}
	for name, v := range classic {
		if v[0] != v[1] {
			t.Errorf("classic %s = %#x, want %#x", name, v[0], v[1])
		}
	}

	SetChrome(ChromeIDS)
	ids := map[string][2]uint32{
		"plotBg":    {colPlotBg, styletokens.NeutralBgFaint.AsHex()},
		"areaBg":    {colAreaBg, styletokens.NeutralBgSurface.AsHex()},
		"border":    {colBorder, styletokens.NeutralBorderFaint.AsHex()},
		"tickLabel": {colTickLabel, styletokens.NeutralTextSecondary.AsHex()},
		"title":     {colTitle, styletokens.NeutralTextPrimary.AsHex()},
		"boxStroke": {colBoxStroke, styletokens.AccentDefault.AsHex()&^0xff | 0xcc},
		"gridMajor": {colGridMajor, styletokens.NeutralBorderFaint.AsHex()&^0xff | 0x40},
	}
	for name, v := range ids {
		if v[0] != v[1] {
			t.Errorf("ids %s = %#x, want %#x", name, v[0], v[1])
		}
	}

	// contrastText follows the active chrome.
	if got := contrastText(0xffffffff); got != colContrastDark {
		t.Errorf("contrastText(white) = %#x, want the dark branch %#x", got, colContrastDark)
	}
	if got := contrastText(0x000000ff); got != colContrastLite {
		t.Errorf("contrastText(black) = %#x, want the light branch %#x", got, colContrastLite)
	}
}

// The ink contrastText picks reads on the fill it was picked for, under the
// active theme: run once plain and once with IMZERO2_THEME=fresh. Before the
// inks were ordered by luminance, the light theme handed a light box a light
// ink.
func TestContrastTextInkReadsOnFill(t *testing.T) {
	for _, chrome := range []ChromeE{ChromeIDS, ChromeClassic} {
		SetChrome(chrome)
		for _, fill := range []uint32{0xffffffff, 0x000000ff, 0x56b4e9ff, 0xf0e442ff} {
			ink := contrastText(fill)
			if r := ContrastRatio(ink, fill); r < 4.5 {
				t.Errorf("chrome %d: ink %08x on fill %08x is %.2f:1, want ≥ 4.5:1", chrome, ink, fill, r)
			}
		}
	}
	SetChrome(ChromeIDS)
}

func TestInksByLuminance(t *testing.T) {
	if dark, lite := inksByLuminance(0xe9edf5ff, 0x24262cff); dark != 0x24262cff || lite != 0xe9edf5ff {
		t.Errorf("light-theme pair ordered %#x, %#x", dark, lite)
	}
	if dark, lite := inksByLuminance(0x101214ff, 0xe5e8ebff); dark != 0x101214ff || lite != 0xe5e8ebff {
		t.Errorf("dark-theme pair ordered %#x, %#x", dark, lite)
	}
}
