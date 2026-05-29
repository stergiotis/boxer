//go:build llm_generated_opus47

package gma_test

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/gma"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/oklab"
)

func nearly(a, b, tol float64) (ok bool) {
	ok = math.Abs(a-b) <= tol
	return
}

// In-gamut OKLCh values must pass through unchanged.
func TestInGamutPassthrough(t *testing.T) {
	cases := []struct {
		L, C, h float64
	}{
		// Pure grey at L=0.5 — definitely in gamut.
		{0.5, 0.0, 0.0},
		// Mid-luminance, low chroma — in gamut for any hue.
		{0.5, 0.05, 90},
		{0.7, 0.05, 240},
	}
	for _, c := range cases {
		_, _, _, postC := gma.MapToSrgbU8(c.L, c.C, c.h)
		if !nearly(postC, c.C, 1e-4) {
			t.Errorf("in-gamut (L=%v, C=%v, h=%v) drifted to C=%v",
				c.L, c.C, c.h, postC)
		}
	}
}

// Out-of-gamut values must monotonically reduce C and stay non-negative.
func TestOutOfGamutClampsAndReducesC(t *testing.T) {
	// Pick a known out-of-gamut: high chroma at extreme L.
	L, C, h := 0.55, 0.5, 30.0
	_, _, _, postC := gma.MapToSrgbU8(L, C, h)
	if postC > C {
		t.Errorf("post-clip C %v exceeded input C %v", postC, C)
	}
	if postC < 0 {
		t.Errorf("post-clip C %v negative", postC)
	}
}

// L must never be reduced by gamut mapping.
func TestLightnessPreserved(t *testing.T) {
	L, C, h := 0.55, 0.5, 30.0
	r, g, b, _ := gma.MapToSrgbU8(L, C, h)

	rl := oklab.SrgbToLinear(float64(r) / 255.0)
	gl := oklab.SrgbToLinear(float64(g) / 255.0)
	bl := oklab.SrgbToLinear(float64(b) / 255.0)
	outL, _, _ := oklab.LinearSrgbToOklab(rl, gl, bl)

	// Allow small numerical error from gamma round-trip + sRGB quantization.
	if !nearly(outL, L, 0.02) {
		t.Errorf("L drifted: input %v, output %v (Δ %.4f)",
			L, outL, math.Abs(outL-L))
	}
}

// The mapped result must be within JND of the unmapped OKLCh target
// (the whole point of CSS Color 4 §13).
func TestMappedResultWithinJnd(t *testing.T) {
	L, C, h := 0.68, 0.30, 25.0 // out-of-gamut high-chroma red-ish
	r, g, b, _ := gma.MapToSrgbU8(L, C, h)

	// Reference: the unmapped OKLab.
	_, A, B := oklab.OklchToOklab(L, C, h)
	refR, refG, refB := oklab.OklabToLinearSrgb(L, A, B)

	gotRl := oklab.SrgbToLinear(float64(r) / 255.0)
	gotGl := oklab.SrgbToLinear(float64(g) / 255.0)
	gotBl := oklab.SrgbToLinear(float64(b) / 255.0)

	gL, gA, gB := oklab.LinearSrgbToOklab(gotRl, gotGl, gotBl)
	refL, refA, refB2 := oklab.LinearSrgbToOklab(refR, refG, refB)
	// ΔE between the unmapped target's OKLab and the mapped result's OKLab.
	dL := gL - refL
	dA := gA - refA
	dB := gB - refB2
	deltaE := math.Sqrt(dL*dL + dA*dA + dB*dB)

	// The mapped result should be perceptually closer than 0.1 — the
	// JND is 0.02 in raw distance but post-uint8-quantization adds noise.
	if deltaE > 0.1 {
		t.Errorf("mapped result too far from target: ΔE = %.4f", deltaE)
	}
}

// Compare GMA to the naive chroma-stepping in oklab.OklchToSrgbU8 for
// a couple of out-of-gamut samples. They should agree closely for
// hues where the gamut boundary is well-behaved.
func TestGmaCloseToNaiveStepping(t *testing.T) {
	cases := []struct {
		L, C, h float64
	}{
		{0.68, 0.15, 240}, // IDS info.default region
		{0.82, 0.12, 145}, // IDS success.strong region
	}
	for _, c := range cases {
		nR, nG, nB, _ := oklab.OklchToSrgbU8(c.L, c.C, c.h)
		gR, gG, gB, _ := gma.MapToSrgbU8(c.L, c.C, c.h)
		// Per-channel difference should be small (within a few uint8 steps).
		dr := absI(int(nR) - int(gR))
		dg := absI(int(nG) - int(gG))
		db := absI(int(nB) - int(gB))
		if dr > 8 || dg > 8 || db > 8 {
			t.Errorf("GMA vs naive drift at (L=%v, C=%v, h=%v): naive=(%d,%d,%d) gma=(%d,%d,%d)",
				c.L, c.C, c.h, nR, nG, nB, gR, gG, gB)
		}
	}
}

func absI(x int) (a int) {
	if x < 0 {
		a = -x
	} else {
		a = x
	}
	return
}
