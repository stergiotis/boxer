//go:build llm_generated_opus47

package styletokens

import (
	"strings"

	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

// DensityE is the IDS density preset (ADR-0029 §SD3, ADR-0032 §SD1).
// Three modes; per-app, fleet-wide. Boxer enum-suffix convention.
type DensityE uint8

const (
	DensityTight    DensityE = 0
	DensityStandard DensityE = 1
	DensityRoomy    DensityE = 2
)

// String returns the preset name (lower-case; matches IMZERO2_DENSITY).
func (inst DensityE) String() (s string) {
	switch inst {
	case DensityTight:
		s = "tight"
	case DensityStandard:
		s = "standard"
	case DensityRoomy:
		s = "roomy"
	default:
		s = "standard"
	}
	return
}

// DensityFromEnv reads IMZERO2_DENSITY (case-insensitive). Anything other
// than tight/standard/roomy returns DensityStandard.
//
// Per-user config file support ($XDG_CONFIG_HOME/imzero2/density.toml,
// ADR-0032 §SD1) lands later; the env var is the M0 surface.
func DensityFromEnv() (d DensityE) {
	switch strings.ToLower(strings.TrimSpace(imzero2env.Density.Get())) {
	case "tight":
		d = DensityTight
	case "roomy":
		d = DensityRoomy
	default:
		d = DensityStandard
	}
	return
}
