// SPDX-License-Identifier: MIT

package styletokens

import (
	"strings"

	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

// ThemeE names the colour theme an imzero2 process runs with (ADR-0258).
// Boxer enum-suffix convention; the discriminants match the Rust `Theme`
// enum, which the drift test checks.
type ThemeE uint8

const (
	// ThemeDark is the IDS palette of ADR-0031: the default.
	ThemeDark ThemeE = 0
	// ThemeFresh is the light theme of ADR-0258.
	ThemeFresh ThemeE = 1
)

// String returns the theme name (lower-case; matches IMZERO2_THEME).
func (inst ThemeE) String() (s string) {
	switch inst {
	case ThemeFresh:
		s = "fresh"
	default:
		s = "dark"
	}
	return
}

// ThemeFromEnv reads IMZERO2_THEME (case-insensitive). "fresh" selects the
// light theme; anything else, including unset, is the IDS dark palette.
func ThemeFromEnv() (th ThemeE) {
	return parseTheme(imzero2env.Theme.Get())
}

func parseTheme(v string) (th ThemeE) {
	if strings.EqualFold(strings.TrimSpace(v), "fresh") {
		th = ThemeFresh
	}
	return
}

// activeTheme is resolved once, in this package's init, before any
// importer's initialisation runs. That ordering is the whole design: a
// widget may copy a palette token into a package-level variable at init
// and still get the theme, because the palette variables have already been
// overwritten by the time its init runs. It is also why there is no
// SetActiveTheme — such a copy would not follow a later switch.
var activeTheme ThemeE

// ActiveTheme is the theme this process runs with. Chosen at launch from
// IMZERO2_THEME and fixed for the life of the process (ADR-0258 §SD3);
// the Rust overlay reads the same variable, so both sides agree.
func ActiveTheme() (th ThemeE) {
	return activeTheme
}

func init() {
	activeTheme = ThemeFromEnv()
	if activeTheme == ThemeFresh {
		applyFreshPalette()
	}
}
