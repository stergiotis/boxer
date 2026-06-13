// Package cvd simulates color-vision deficiency via the
// Brettel-Viénot-Mollon (1997) method, used by the IDS palette verifier
// (ADR-0031 §SD5, ADR-0033 §SD6) to assert ΔE > 15 between same-emphasis
// semantic-palette pairs under deuteranopia / protanopia / tritanopia.
//
// The matrices are the Viénot-Brettel-Mollon 1999 simplified linear
// approximation (good fit for moderate-to-strong dichromacy; the original
// 1997 formulation includes a non-linear hue-plane projection that is
// only meaningfully different at the gamut extremes — adequate for
// design-system pair grading).
package cvd

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/oklab"
)

// Type names a CVD condition.
type Type string

const (
	Deuteranopia Type = "deuteranopia"
	Protanopia   Type = "protanopia"
	Tritanopia   Type = "tritanopia"
)

// 3x3 simulation matrices applied in linear-sRGB space. Source: Viénot,
// Brettel, Mollon (1999), "Digital video colourmaps for checking the
// legibility of displays by dichromats".
var matrices = map[Type][9]float64{
	// Protanopia
	Protanopia: {
		0.152286, 1.052583, -0.204868,
		0.114503, 0.786281, 0.099216,
		-0.003882, -0.048116, 1.051998,
	},
	// Deuteranopia
	Deuteranopia: {
		0.367322, 0.860646, -0.227968,
		0.280085, 0.672501, 0.047413,
		-0.011820, 0.042940, 0.968881,
	},
	// Tritanopia
	Tritanopia: {
		1.255528, -0.076749, -0.178779,
		-0.078411, 0.930809, 0.147602,
		0.004733, 0.691367, 0.303900,
	},
}

// Simulate returns the sRGB triple as it would be perceived under the
// chosen CVD condition. Inputs and outputs are gamma-encoded sRGB uint8s
// in [0, 255].
func Simulate(t Type, r, g, b uint8) (rOut, gOut, bOut uint8) {
	m := matrices[t]
	lr := oklab.SrgbToLinear(float64(r) / 255.0)
	lg := oklab.SrgbToLinear(float64(g) / 255.0)
	lb := oklab.SrgbToLinear(float64(b) / 255.0)

	tr := m[0]*lr + m[1]*lg + m[2]*lb
	tg := m[3]*lr + m[4]*lg + m[5]*lb
	tb := m[6]*lr + m[7]*lg + m[8]*lb

	rOut = oklab.SrgbU8(oklab.LinearToSrgb(tr))
	gOut = oklab.SrgbU8(oklab.LinearToSrgb(tg))
	bOut = oklab.SrgbU8(oklab.LinearToSrgb(tb))
	return
}

// DeltaEOklab returns Euclidean distance in OKLab × 100 (the rough
// equivalent of CIEDE76 scale, suitable for ΔE > 15 thresholds).
//
// Inputs are gamma-encoded sRGB uint8s.
func DeltaEOklab(r1, g1, b1, r2, g2, b2 uint8) (de float64) {
	L1, A1, B1 := oklab.LinearSrgbToOklab(
		oklab.SrgbToLinear(float64(r1)/255.0),
		oklab.SrgbToLinear(float64(g1)/255.0),
		oklab.SrgbToLinear(float64(b1)/255.0),
	)
	L2, A2, B2 := oklab.LinearSrgbToOklab(
		oklab.SrgbToLinear(float64(r2)/255.0),
		oklab.SrgbToLinear(float64(g2)/255.0),
		oklab.SrgbToLinear(float64(b2)/255.0),
	)
	dL := L1 - L2
	dA := A1 - A2
	dB := B1 - B2
	de = 100.0 * math.Sqrt(dL*dL+dA*dA+dB*dB)
	return
}
