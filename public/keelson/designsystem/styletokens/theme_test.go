package styletokens

import "testing"

// TestParseThemeAcceptsFreshOnly pins the contract of IMZERO2_THEME: one
// word selects the light theme, anything else — including the names a
// reader might guess — is the IDS palette, so a typo degrades to the
// default look rather than to a third state.
func TestParseThemeAcceptsFreshOnly(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want ThemeE
	}{
		{"fresh", ThemeFresh}, {" FRESH ", ThemeFresh}, {"Fresh", ThemeFresh},
		{"", ThemeDark}, {"dark", ThemeDark}, {"ids", ThemeDark}, {"light", ThemeDark},
	} {
		if got := parseTheme(tc.in); got != tc.want {
			t.Errorf("parseTheme(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestFreshPaletteIsLight is the property the theme exists for: after the
// apply-function runs, the spine reads as paper under ink. Restores the
// IDS values afterwards so the other tests keep the palette they expect.
func TestFreshPaletteIsLight(t *testing.T) {
	saved := []RGBA8{NeutralBgPanel, NeutralBgSurface, NeutralTextPrimary, AccentDefault}
	defer func() {
		NeutralBgPanel, NeutralBgSurface, NeutralTextPrimary, AccentDefault = saved[0], saved[1], saved[2], saved[3]
	}()
	applyFreshPalette()
	lum := func(c RGBA8) int { return int(c.R) + int(c.G) + int(c.B) }
	if lum(NeutralBgPanel) < 3*200 || lum(NeutralBgSurface) < 3*200 {
		t.Errorf("fresh panel/surface are not light: %v %v", NeutralBgPanel, NeutralBgSurface)
	}
	if lum(NeutralTextPrimary) > 3*80 {
		t.Errorf("fresh text is not ink: %v", NeutralTextPrimary)
	}
	if AccentDefault == saved[3] {
		t.Error("applyFreshPalette left the accent at the IDS value")
	}
}
