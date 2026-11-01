// SPDX-License-Identifier: MIT

package gen

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/cvd"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/palette"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Cross-palette floors: the qualitative cycle against the semantic palette's
// Default tokens (ADR-0029 §SD4 — the two palettes are disjoint; ADR-0156,
// Updates 2026-11-01). Normal-vision OKLab ΔE, measured per theme.
const (
	// CrossPaletteGateFloor is enforcing, and for slot 0 only: the colour of
	// the first series, which every chart draws. Empirical, like ADR-0156
	// §SD3's dichromacy floor — nothing in the literature gives a ΔE at which
	// a series colour stops reading as a status colour. It sits above the
	// collision this check was added for (Okabe-Ito orange against the dark
	// theme's warning, ΔE 6.1) and below what the shipped slot 0 reaches in
	// every theme.
	CrossPaletteGateFloor = 10.0
	// CrossPaletteAdvisoryLevel is reported for every slot, never gating —
	// the same ΔE 15 the semantic palette's own CVD check reports at. A
	// seven-entry CVD-safe set cannot clear it for every slot in every theme:
	// the light theme's status hues sit on the same blue, green and red
	// families Okabe-Ito spans.
	CrossPaletteAdvisoryLevel = 15.0
)

// CrossPalette measures a qualitative cycle against the Default token of
// every semantic role in every theme. gating holds the slot-0 findings at or
// below CrossPaletteGateFloor; advisory holds every other finding at or below
// CrossPaletteAdvisoryLevel. repoRoot "" means auto-detect.
func CrossPalette(repoRoot string, cycle [][3]uint8) (gating, advisory []string, err error) {
	if repoRoot == "" {
		repoRoot, err = findRepoRoot()
		if err != nil {
			return
		}
	}
	for _, th := range themes {
		var pf palette.File
		_, err = toml.DecodeFile(filepath.Join(repoRoot, th.source), &pf)
		if err != nil {
			err = eb.Build().Str("source", th.source).Errorf("decode palette: %w", err)
			return
		}
		var tokens []palette.Token
		tokens, err = palette.Resolve(&pf)
		if err != nil {
			err = eb.Build().Str("source", th.source).Errorf("resolve tokens: %w", err)
			return
		}
		g, a := crossPaletteTheme(th.label(), tokens, cycle)
		gating = append(gating, g...)
		advisory = append(advisory, a...)
	}
	return
}

// label names the theme in a cross-palette finding. Unlike prefixed, the IDS
// palette is named too: a cross-palette finding is about a pair of palettes,
// and "dark" is what IMZERO2_THEME calls it (styletokens.ThemeE).
func (inst theme) label() (s string) {
	s = inst.name
	if s == "" {
		s = "dark"
	}
	return
}

// crossPaletteTheme measures every cycle slot against every semantic
// *.default token of one theme. Neutral is included: it is a status colour
// in the semantic palette too, and a grey series is as misleading as an
// amber one next to a "none" chip.
func crossPaletteTheme(themeLabel string, tokens []palette.Token, cycle [][3]uint8) (gating, advisory []string) {
	for slot, c := range cycle {
		for _, t := range tokens {
			if !strings.HasPrefix(t.Name, "semantic.") || !strings.HasSuffix(t.Name, ".default") {
				continue
			}
			de := cvd.DeltaEOklab(c[0], c[1], c[2], t.R, t.G, t.B)
			switch {
			case slot == 0 && de <= CrossPaletteGateFloor:
				gating = append(gating, fmt.Sprintf(
					"%s: slot 0 (#%02x%02x%02x) vs %s (%s): ΔE %.1f ≤ %.1f — the first series reads as a status colour",
					themeLabel, c[0], c[1], c[2], t.Name, t.Hex(), de, CrossPaletteGateFloor))
			case de <= CrossPaletteAdvisoryLevel:
				advisory = append(advisory, fmt.Sprintf(
					"%s: slot %d (#%02x%02x%02x) vs %s (%s): ΔE %.1f ≤ %.1f",
					themeLabel, slot, c[0], c[1], c[2], t.Name, t.Hex(), de, CrossPaletteAdvisoryLevel))
			}
		}
	}
	return
}
