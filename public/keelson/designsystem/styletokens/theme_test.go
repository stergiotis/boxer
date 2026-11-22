package styletokens

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"
)

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
// values it found afterwards so the other tests keep the palette they expect.
// Under IMZERO2_THEME=fresh the palette is fresh before the test starts, so
// the accent can only be seen to change from the dark theme.
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
	if ActiveTheme() == ThemeDark && AccentDefault == saved[3] {
		t.Error("applyFreshPalette left the accent at the IDS value")
	}
}

// TestButtonPaddingYFollowsTheTheme pins the mirror of imzero2_egui's
// button padding: PaddingInner, and two more points under the fresh theme.
func TestButtonPaddingYFollowsTheTheme(t *testing.T) {
	for _, d := range []DensityE{DensityTight, DensityStandard, DensityRoomy} {
		if got, want := buttonPaddingY(d, ThemeDark), PaddingInner(d); got != want {
			t.Errorf("dark, density %v: %v, want %v", d, got, want)
		}
		if got, want := buttonPaddingY(d, ThemeFresh), PaddingInner(d)+2; got != want {
			t.Errorf("fresh, density %v: %v, want %v", d, got, want)
		}
	}
	if got, want := ButtonPaddingY(DensityStandard), buttonPaddingY(DensityStandard, ActiveTheme()); got != want {
		t.Errorf("ButtonPaddingY = %v, want the active theme's %v", got, want)
	}
}

// TestRoundingButtonFollowsTheTheme pins the mirror of imzero2_egui's
// widget radius: RoundingSm under the IDS overlay, and the fresh theme's
// ROUND_WIDGET, read from its source so the two cannot drift.
func TestRoundingButtonFollowsTheTheme(t *testing.T) {
	if got := roundingButton(ThemeDark); got != RoundingSm {
		t.Errorf("dark: %v, want RoundingSm %v", got, RoundingSm)
	}
	_, here, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(here), "..", "..", "..", "..", "rust", "imzero2", "imzero2_egui", "src", "style", "fresh.rs"))
	if err != nil {
		t.Fatalf("read fresh.rs: %v", err)
	}
	m := regexp.MustCompile(`pub const ROUND_WIDGET: u8 = (\d+);`).FindSubmatch(b)
	if m == nil {
		t.Fatal("fresh.rs: no ROUND_WIDGET")
	}
	if got, want := roundingButton(ThemeFresh), string(m[1]); strconv.FormatFloat(float64(got), 'f', -1, 32) != want {
		t.Errorf("fresh: %v, want ROUND_WIDGET %s", got, want)
	}
	if got, want := RoundingButton(), roundingButton(ActiveTheme()); got != want {
		t.Errorf("RoundingButton = %v, want the active theme's %v", got, want)
	}
}
