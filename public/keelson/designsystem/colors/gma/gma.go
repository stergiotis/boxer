//go:build llm_generated_opus47

// Package gma implements the CSS Color Module Level 4 §13 Gamut Mapping
// Algorithm, the perceptually-correct way to map an out-of-gamut OKLCh
// color into the sRGB display gamut.
//
// Replaces the naive "step C down by 0.005 until in-gamut" loop that
// previously lived in oklab.OklchToSrgbU8. Per ADR-0033 §SD5 / §SD7
// upgrade prompted by adversarial review (CSS Color 4 §13 reference:
// https://www.w3.org/TR/css-color-4/#binsearch).
//
// Algorithm:
//   1. If already in-gamut, return as-is.
//   2. Compute per-channel-clipped projection. If the OKLab ΔE between
//      the original and clipped is below JND (0.02), the clipped color
//      is the answer.
//   3. Otherwise bisect on C in OKLCh, holding L and h constant:
//      - At each midpoint, project via per-channel clipping.
//      - If ΔE_oklab(midpoint, clipped) < JND, the clipped projection
//        is close enough; minimum bound moves up.
//      - Else C is still too large; maximum bound moves down.
//   4. Return the clipped projection of the final converged C.
//
// L is never reduced (preserves contrast guarantees). Result is the
// closest in-gamut sRGB triple to the original OKLCh target by OKLab
// Euclidean distance.
package gma

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/oklab"
)

// JND — "just noticeable difference" threshold in OKLab Euclidean
// distance. CSS Color 4 §13 recommends 0.02.
const JND = 0.02

// Epsilon — bisection-termination tolerance on C.
const eps = 1e-4

// inGamut reports whether the linear-sRGB triple is within [0, 1]^3
// with a tiny tolerance for floating-point rounding.
func inGamut(r, g, b float64) (in bool) {
	const tol = 1e-6
	in = r >= -tol && r <= 1+tol &&
		g >= -tol && g <= 1+tol &&
		b >= -tol && b <= 1+tol
	return
}

// clip clamps each linear-sRGB channel to [0, 1].
func clip(r, g, b float64) (rOut, gOut, bOut float64) {
	rOut = clamp01(r)
	gOut = clamp01(g)
	bOut = clamp01(b)
	return
}

func clamp01(v float64) (out float64) {
	switch {
	case v < 0:
		out = 0
	case v > 1:
		out = 1
	default:
		out = v
	}
	return
}

// deltaE returns the OKLab Euclidean distance between two linear-sRGB
// triples. Used as the perceptual distance metric per CSS Color 4 §13.
func deltaE(r1, g1, b1, r2, g2, b2 float64) (de float64) {
	L1, A1, B1 := oklab.LinearSrgbToOklab(r1, g1, b1)
	L2, A2, B2 := oklab.LinearSrgbToOklab(r2, g2, b2)
	dL := L1 - L2
	dA := A1 - A2
	dB := B1 - B2
	de = math.Sqrt(dL*dL + dA*dA + dB*dB)
	return
}

// MapToSrgbU8 maps an OKLCh target to the closest in-gamut sRGB triple
// via the CSS Color 4 §13 bisection algorithm. Returns gamma-encoded
// uint8s plus the post-mapping C actually used.
//
// L is held constant (never reduced — preserves contrast guarantees).
// h is held constant. C is searched in [0, originalC].
func MapToSrgbU8(L, C, hDeg float64) (r, g, b uint8, postC float64) {
	// Step 1: trivially in-gamut?
	_, A, B := oklab.OklchToOklab(L, C, hDeg)
	lr, lg, lb := oklab.OklabToLinearSrgb(L, A, B)
	if inGamut(lr, lg, lb) {
		r = oklab.SrgbU8(oklab.LinearToSrgb(lr))
		g = oklab.SrgbU8(oklab.LinearToSrgb(lg))
		b = oklab.SrgbU8(oklab.LinearToSrgb(lb))
		postC = C
		return
	}

	// Step 2: is the clipped projection itself within JND?
	cr, cg, cb := clip(lr, lg, lb)
	if deltaE(lr, lg, lb, cr, cg, cb) < JND {
		r = oklab.SrgbU8(oklab.LinearToSrgb(cr))
		g = oklab.SrgbU8(oklab.LinearToSrgb(cg))
		b = oklab.SrgbU8(oklab.LinearToSrgb(cb))
		postC = C
		return
	}

	// Step 3: bisect on C.
	cMin := 0.0
	cMax := C
	var finalR, finalG, finalB float64
	finalR, finalG, finalB = cr, cg, cb

	for cMax-cMin > eps {
		c := 0.5 * (cMin + cMax)
		_, a, bb := oklab.OklchToOklab(L, c, hDeg)
		rr, gg, b3 := oklab.OklabToLinearSrgb(L, a, bb)

		if inGamut(rr, gg, b3) {
			// We can afford more chroma.
			cMin = c
			finalR, finalG, finalB = rr, gg, b3
		} else {
			// Clip and check ΔE.
			cr2, cg2, cb2 := clip(rr, gg, b3)
			e := deltaE(rr, gg, b3, cr2, cg2, cb2)
			if e < JND {
				// Close enough; this C is acceptable but the clipped
				// projection is what we'll emit.
				cMin = c
				finalR, finalG, finalB = cr2, cg2, cb2
			} else {
				cMax = c
			}
		}
	}

	postC = cMin
	r = oklab.SrgbU8(oklab.LinearToSrgb(finalR))
	g = oklab.SrgbU8(oklab.LinearToSrgb(finalG))
	b = oklab.SrgbU8(oklab.LinearToSrgb(finalB))
	return
}
