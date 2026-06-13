// SPDX-License-Identifier: MIT

package styletokens

import "github.com/stergiotis/boxer/public/config/env"

// AccessibilityE is the Tier-2 IDS accessibility preset (ADR-0031 §SD9
// proposed). When non-default it overrides the Tier-1 family-default
// palette choices. End users who care about CVD or grayscale-friendly
// readouts touch only IDS_ACCESSIBILITY; widget authors don't need
// per-widget palette plumbing.
type AccessibilityE uint8

const (
	// AccessibilityDefault honours the Tier-1 IDS_PALETTE_* selections.
	// The IDS-curated catalogue is CVD-safe by design (Crameri 2018 +
	// matplotlib viridis family), so the "default" preset already works
	// for protanopia, deuteranopia, and most tritanopia cases.
	AccessibilityDefault AccessibilityE = iota
	// AccessibilityHighContrast forces a darker / wider-luminance-range
	// sequential (batlowK, recommended in Crameri 2018 §3 for tritanopia
	// and for projection / dim-room reading). Diverging stays at vik
	// (already strong-contrast). Widgets may additionally widen palette
	// t-range and boost fill alpha under this preset.
	AccessibilityHighContrast
	// AccessibilityMonochrome forces a grayscale-only sequential (grayC).
	// Required for achromatopsia, grayscale printing, and dark-on-dark
	// projector setups. Diverging stays at vik (no monochrome diverging
	// in the catalogue; vik works under R/G CVD which is the relevant
	// case for grayscale-output users).
	AccessibilityMonochrome
)

// sequentialStringKeys maps each SequentialE value to its IDS_PALETTE_*
// env-var string key. Order MUST match the SequentialE iota order so
// indexing by the enum is valid.
var sequentialStringKeys = []string{
	SequentialBatlow:  "batlow",
	SequentialLapaz:   "lapaz",
	SequentialOslo:    "oslo",
	SequentialLajolla: "lajolla",
	SequentialViridis: "viridis",
	SequentialMagma:   "magma",
	SequentialPlasma:  "plasma",
	SequentialInferno: "inferno",
	SequentialBatlowK: "batlow_k",
	SequentialGrayC:   "gray_c",
}

var divergingStringKeys = []string{
	DivergingVik:  "vik",
	DivergingRoma: "roma",
	DivergingBroc: "broc",
	DivergingCork: "cork",
}

var accessibilityStringKeys = []string{
	AccessibilityDefault:      "default",
	AccessibilityHighContrast: "high-contrast",
	AccessibilityMonochrome:   "monochrome",
}

// Tier-1 env vars: per-family user defaults.
var (
	PaletteSequentialEnv = env.NewCategorialString(env.Spec{
		Name: "IDS_PALETTE_SEQUENTIAL",
		Description: "user-default sequential palette for ordered-data charts " +
			"(boxenplot, treemap, heatmap). All values in the IDS catalogue " +
			"are perceptually uniform and CVD-safe; the choice is aesthetic. " +
			"Overridden by IDS_ACCESSIBILITY when that is non-default.",
		Category: env.CategoryDev,
		Default:  "batlow",
	}, sequentialStringKeys)

	PaletteDivergingEnv = env.NewCategorialString(env.Spec{
		Name: "IDS_PALETTE_DIVERGING",
		Description: "user-default diverging palette for signed-deviation charts " +
			"(delta heatmaps, residual plots). Overridden by IDS_ACCESSIBILITY.",
		Category: env.CategoryDev,
		Default:  "vik",
	}, divergingStringKeys)
)

// Tier-2 env var: single accessibility preset, the "one-knob" surface
// for CVD / monochrome users (ADR-0029 §SD9 single-adjust principle).
var AccessibilityEnv = env.NewCategorialString(env.Spec{
	Name: "IDS_ACCESSIBILITY",
	Description: "accessibility preset overriding Tier-1 palettes: " +
		"default (honour IDS_PALETTE_*), high-contrast (batlowK + alpha/range boost), " +
		"monochrome (grayC for sequential, vik fallback for diverging).",
	Category: env.CategoryDev,
	Default:  "default",
}, accessibilityStringKeys)

// AccessibilityFromEnv reads IDS_ACCESSIBILITY. Returns
// AccessibilityDefault for empty / unknown values.
func AccessibilityFromEnv() (a AccessibilityE) {
	a = accessibilityFromString(AccessibilityEnv.Get())
	return
}

// SequentialDefault resolves the effective sequential palette, applying
// the Tier-2 preset override on top of the Tier-1 explicit user pick.
//
//	IDS_ACCESSIBILITY=monochrome  → SequentialGrayC   (overrides any IDS_PALETTE_SEQUENTIAL)
//	IDS_ACCESSIBILITY=high-contrast → SequentialBatlowK
//	IDS_ACCESSIBILITY=default + IDS_PALETTE_SEQUENTIAL=viridis → SequentialViridis
//	(neither set)                 → SequentialBatlow  (IDS default)
func SequentialDefault() (s SequentialE) {
	a := AccessibilityFromEnv()
	if a != AccessibilityDefault {
		s = presetSequential(a)
		return
	}
	s = sequentialFromString(PaletteSequentialEnv.Get())
	return
}

// DivergingDefault resolves the effective diverging palette under the
// same priority order as SequentialDefault.
func DivergingDefault() (d DivergingE) {
	a := AccessibilityFromEnv()
	if a != AccessibilityDefault {
		d = presetDiverging(a)
		return
	}
	d = divergingFromString(PaletteDivergingEnv.Get())
	return
}

// presetSequential maps an accessibility preset to its prescribed
// sequential palette. AccessibilityDefault is intentionally not handled
// here — callers must short-circuit on default and fall through to the
// Tier-1 family value.
func presetSequential(a AccessibilityE) (s SequentialE) {
	switch a {
	case AccessibilityHighContrast:
		s = SequentialBatlowK
	case AccessibilityMonochrome:
		s = SequentialGrayC
	default:
		s = SequentialBatlow
	}
	return
}

// presetDiverging maps an accessibility preset to its prescribed
// diverging palette. Vik is universal (CVD-safe, lightness-symmetric)
// and stays selected under every preset — the IDS catalogue does not
// currently include a monochrome diverging ramp.
func presetDiverging(a AccessibilityE) (d DivergingE) {
	d = DivergingVik
	return
}

func sequentialFromString(key string) (s SequentialE) {
	for i, k := range sequentialStringKeys {
		if k == key {
			s = SequentialE(i)
			return
		}
	}
	s = SequentialBatlow
	return
}

func divergingFromString(key string) (d DivergingE) {
	for i, k := range divergingStringKeys {
		if k == key {
			d = DivergingE(i)
			return
		}
	}
	d = DivergingVik
	return
}

func accessibilityFromString(key string) (a AccessibilityE) {
	for i, k := range accessibilityStringKeys {
		if k == key {
			a = AccessibilityE(i)
			return
		}
	}
	a = AccessibilityDefault
	return
}
