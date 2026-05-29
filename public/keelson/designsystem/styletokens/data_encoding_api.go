//go:build llm_generated_opus47

// SPDX-License-Identifier: MIT

package styletokens

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens/data_encoding"
)

// SequentialE selects an IDS sequential data-encoding palette per
// ADR-0031 §SD3. Batlow is the IDS default; the matplotlib-lineage
// alternates (viridis / magma / plasma / inferno) are available for
// callers who specifically want that visual character.
type SequentialE uint8

const (
	// SequentialBatlow is the IDS default sequential (Crameri, MIT) —
	// perceptually-uniform, lightness-monotonic, CVD-safe.
	SequentialBatlow SequentialE = iota
	// SequentialLapaz — blue→yellow Crameri alternate; CVD-safe.
	SequentialLapaz
	// SequentialOslo — pure-blue Crameri alternate.
	SequentialOslo
	// SequentialLajolla — orange-warm Crameri alternate.
	SequentialLajolla
	// SequentialViridis — matplotlib viridis (van der Walt & Smith, CC0).
	SequentialViridis
	// SequentialMagma — matplotlib magma alternate.
	SequentialMagma
	// SequentialPlasma — matplotlib plasma alternate.
	SequentialPlasma
	// SequentialInferno — matplotlib inferno alternate.
	SequentialInferno
	// SequentialBatlowK — Crameri batlow's darker variant; recommended for
	// tritanopia per Crameri 2018 §3. Used by AccessibilityHighContrast
	// when its preset is mapped to a tritanopia-tuned sequential ramp.
	SequentialBatlowK
	// SequentialGrayC — Crameri grayscale (continuous) sequential.
	// White→black direction upstream; widgets that want Hofmann
	// "shallow=dark, deep=light" reading invert the palette t-range.
	// Used by AccessibilityMonochrome.
	SequentialGrayC
)

// DivergingE selects an IDS diverging data-encoding palette per
// ADR-0031 §SD3. Vik is the IDS default; the others vary terminal hues
// for domain-specific encodings.
type DivergingE uint8

const (
	// DivergingVik is the IDS default diverging (Crameri, MIT) —
	// perceptually-uniform, symmetric around a neutral midpoint.
	DivergingVik DivergingE = iota
	// DivergingRoma — alternate diverging with different terminal hues.
	DivergingRoma
	// DivergingBroc — green-purple diverging.
	DivergingBroc
	// DivergingCork — teal-pink diverging.
	DivergingCork
)

// sequentialLUTs is the per-enum 256-entry LUT lookup. Indexing by the
// enum's uint8 discriminant keeps Sequential branch-free at the cost of a
// pointer-sized slot per enum value; the table is computed once at package
// init.
var sequentialLUTs = [...]*[256][3]uint8{
	SequentialBatlow:   &data_encoding.Batlow,
	SequentialLapaz:    &data_encoding.Lapaz,
	SequentialOslo:     &data_encoding.Oslo,
	SequentialLajolla:  &data_encoding.Lajolla,
	SequentialViridis:  &data_encoding.Viridis,
	SequentialMagma:    &data_encoding.Magma,
	SequentialPlasma:   &data_encoding.Plasma,
	SequentialInferno:  &data_encoding.Inferno,
	SequentialBatlowK:  &data_encoding.BatlowK,
	SequentialGrayC:    &data_encoding.GrayC,
}

var divergingLUTs = [...]*[256][3]uint8{
	DivergingVik:  &data_encoding.Vik,
	DivergingRoma: &data_encoding.Roma,
	DivergingBroc: &data_encoding.Broc,
	DivergingCork: &data_encoding.Cork,
}

// Sequential samples a sequential palette at t ∈ [0, 1]. Out-of-range
// values are clamped. Mirrors the Rust `style::tokens::sequential` accessor.
// Alpha is always 0xFF (opaque) — callers that need transparency pack the
// alpha themselves.
func Sequential(palette SequentialE, t float32) (rgba RGBA8) {
	// Bounds-check the enum so a corrupted / forward-compatible enum
	// value cannot panic — fall back to the IDS default (Batlow) the
	// same way the env-resolution helpers do for unknown strings.
	if int(palette) >= len(sequentialLUTs) {
		palette = SequentialBatlow
	}
	lut := sequentialLUTs[palette]
	idx := lutIndexFromT(t)
	c := lut[idx]
	rgba = RGBA8{R: c[0], G: c[1], B: c[2], A: 0xFF}
	return
}

// Diverging samples a diverging palette at t ∈ [-1, 1] (sign carries
// direction; magnitude carries distance from the neutral midpoint).
// Out-of-range values are clamped. Mirrors the Rust
// `style::tokens::diverging` accessor.
func Diverging(palette DivergingE, t float32) (rgba RGBA8) {
	// Bounds-check the enum (see Sequential for rationale).
	if int(palette) >= len(divergingLUTs) {
		palette = DivergingVik
	}
	lut := divergingLUTs[palette]
	// [-1, 1] → [0, 1] → [0, 255]
	mapped := (t*0.5 + 0.5)
	idx := lutIndexFromT(mapped)
	c := lut[idx]
	rgba = RGBA8{R: c[0], G: c[1], B: c[2], A: 0xFF}
	return
}

// QualitativeCycle returns the idx-th color from the IDS qualitative
// palette (BatlowS, 10 entries, Crameri MIT). idx wraps modulo 10 so it
// can be called with any non-negative cycle index. Mirrors the Rust
// `style::tokens::qualitative_cycle` accessor.
func QualitativeCycle(idx int) (rgba RGBA8) {
	if idx < 0 {
		idx = -idx
	}
	lut := &data_encoding.BatlowS
	c := lut[idx%len(lut)]
	rgba = RGBA8{R: c[0], G: c[1], B: c[2], A: 0xFF}
	return
}

// lutIndexFromT clamps t to [0, 1] and maps it to a 0..255 LUT index.
func lutIndexFromT(t float32) (idx int) {
	switch {
	case t <= 0:
		idx = 0
	case t >= 1:
		idx = 255
	default:
		idx = int(t * 255.0)
		if idx > 255 {
			idx = 255
		}
	}
	return
}
