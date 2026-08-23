package styletokens

import (
	"strings"
	"sync/atomic"

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
// This is the launch-time seed only. Widgets resolving spacing or type
// tokens want ActiveDensity, which also reflects a runtime switch.
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

// activeDensity is the process-wide active preset, resolved lazily from
// the environment on first read and mutable at runtime via
// SetActiveDensity (the host chrome's Layout ▸ Density menu). Held as an
// atomic so the per-frame token lookups stay a plain load — ADR-0029 §SD13
// forbids design-system work in the render path, and the env-var handle
// behind DensityFromEnv takes a mutex on every Get.
//
// Zero value means "not yet resolved"; resolved values are stored offset
// by one so that DensityTight (0) is distinguishable from unset.
var activeDensity atomic.Uint32

// ActiveDensity returns the density preset in force right now. This is the
// accessor widgets should use when resolving spacing and type tokens: it
// picks up a runtime switch on the next frame, where DensityFromEnv is
// pinned to the launch-time environment.
func ActiveDensity() (d DensityE) {
	v := activeDensity.Load()
	if v == 0 {
		d = DensityFromEnv()
		activeDensity.CompareAndSwap(0, uint32(d)+1)
		return
	}
	d = DensityE(v - 1)
	return
}

// SetActiveDensity switches the process-wide preset. Callers driving an
// imzero2 UI must also push the new preset to the Rust side
// (c.SetIdsDensity) — the two sides keep independent copies of the token
// tables and a one-sided change leaves egui's own spacing at the old
// preset. imzhost.DecorateRenderer's Layout menu does both.
//
// Out-of-range values are clamped to DensityStandard.
func SetActiveDensity(d DensityE) {
	if d > DensityRoomy {
		d = DensityStandard
	}
	activeDensity.Store(uint32(d) + 1)
}
