// SPDX-License-Identifier: MIT

package gen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/palette"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens/data_encoding"
)

// okabeItoPublicationOrder is the qualitative cycle as it shipped before
// ADR-0156's 2026-11-01 update: Okabe-Ito in publication order, black
// dropped. Its slot 0, orange, sits ΔE 6.1 from the dark theme's warning —
// the collision the cross-palette gate was added for, so it doubles as the
// fixture: if the gate ever stops rejecting it, the gate has rotted.
var okabeItoPublicationOrder = [][3]uint8{
	{230, 159, 0}, {86, 180, 233}, {0, 158, 115}, {240, 228, 66},
	{0, 114, 178}, {213, 94, 0}, {204, 121, 167},
}

// TestCrossPaletteShippedCycleClearsGate runs over the shipped table rather
// than the vendor converter's output, so a hand-edit of okabe_ito.out.go or
// a semantic palette change that walks a Default onto slot 0 is caught.
func TestCrossPaletteShippedCycleClearsGate(t *testing.T) {
	gating, _, err := CrossPalette("", data_encoding.OkabeIto[:])
	if err != nil {
		t.Fatalf("CrossPalette: %v", err)
	}
	if len(gating) > 0 {
		t.Errorf("shipped qualitative cycle fails the cross-palette gate:\n  %s", strings.Join(gating, "\n  "))
	}
}

func TestCrossPaletteRejectsPublicationOrder(t *testing.T) {
	gating, _, err := CrossPalette("", okabeItoPublicationOrder)
	if err != nil {
		t.Fatalf("CrossPalette: %v", err)
	}
	joined := strings.Join(gating, "\n")
	want := "dark: slot 0 (#e69f00) vs semantic.warning.default"
	if !strings.Contains(joined, want) {
		t.Errorf("gate missed orange vs the dark theme's warning; want %q in:\n%s", want, joined)
	}
}

// TestCrossPaletteCoversEveryTheme puts each theme's own warning Default at
// slot 0 and expects a gating finding naming that theme, so a theme added to
// the generator cannot escape the check.
func TestCrossPaletteCoversEveryTheme(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}
	for _, th := range themes {
		var pf palette.File
		_, err = toml.DecodeFile(filepath.Join(root, th.source), &pf)
		if err != nil {
			t.Fatalf("%s: decode: %v", th.source, err)
		}
		tokens, err := palette.Resolve(&pf)
		if err != nil {
			t.Fatalf("%s: resolve: %v", th.source, err)
		}
		var warn [3]uint8
		found := false
		for _, tk := range tokens {
			if tk.Name == "semantic.warning.default" {
				warn = [3]uint8{tk.R, tk.G, tk.B}
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: no semantic.warning.default", th.source)
		}
		gating, _, err := CrossPalette(root, [][3]uint8{warn})
		if err != nil {
			t.Fatalf("CrossPalette: %v", err)
		}
		want := th.label() + ": slot 0"
		if !strings.Contains(strings.Join(gating, "\n"), want) {
			t.Errorf("no gating finding %q for a cycle opening on that theme's warning:\n%v", want, gating)
		}
	}
}

// TestCrossPaletteThemeSplit pins the gating/advisory split: slot 0 within
// the gate floor gates, any later slot within the advisory level is
// advisory, and only *.default semantic tokens are measured.
func TestCrossPaletteThemeSplit(t *testing.T) {
	amber := [3]uint8{230, 181, 93}
	tokens := []palette.Token{
		{Name: "semantic.warning.default", R: amber[0], G: amber[1], B: amber[2]},
		{Name: "semantic.warning.subtle", R: 10, G: 200, B: 10},
		{Name: "neutral.spine.text_primary", R: 10, G: 200, B: 10},
	}
	green := [3]uint8{10, 200, 10}
	gating, advisory := crossPaletteTheme("t", tokens, [][3]uint8{amber, amber, green})
	if len(gating) != 1 || !strings.HasPrefix(gating[0], "t: slot 0 ") {
		t.Errorf("want one slot-0 gating finding, got %v", gating)
	}
	if len(advisory) != 1 || !strings.HasPrefix(advisory[0], "t: slot 1 ") {
		t.Errorf("want one slot-1 advisory finding (subtle and spine tokens ignored), got %v", advisory)
	}

	// Slot 0 between the gate floor and the advisory level is advisory only.
	near := [3]uint8{204, 121, 167}
	tokens = []palette.Token{{Name: "semantic.error.default", R: 255, G: 158, B: 150}}
	gating, advisory = crossPaletteTheme("t", tokens, [][3]uint8{near})
	if len(gating) != 0 || len(advisory) != 1 {
		t.Errorf("ΔE between %.0f and %.0f at slot 0: want advisory only, got gating=%v advisory=%v",
			CrossPaletteGateFloor, CrossPaletteAdvisoryLevel, gating, advisory)
	}
}
