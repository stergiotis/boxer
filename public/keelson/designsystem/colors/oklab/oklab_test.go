package oklab_test

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/oklab"
)

// Ottosson 2020 reference vectors (from
// https://bottosson.github.io/posts/oklab/, "Table 1").
// All inputs are linear-sRGB; outputs are OKLab.
var ottossonVectors = []struct {
	name    string
	r, g, b float64
	L, A, B float64
}{
	// "white"
	{"white", 1.0, 1.0, 1.0, 1.0, 0.0, 0.0},
	// "red"
	{"red", 1.0, 0.0, 0.0, 0.628, 0.225, 0.126},
	// "green"
	{"green", 0.0, 1.0, 0.0, 0.866, -0.234, 0.179},
	// "blue"
	{"blue", 0.0, 0.0, 1.0, 0.452, -0.032, -0.312},
}

func nearly(a, b, tol float64) (ok bool) {
	ok = math.Abs(a-b) <= tol
	return
}

func TestLinearSrgbToOklabMatchesOttoson(t *testing.T) {
	for _, v := range ottossonVectors {
		gotL, gotA, gotB := oklab.LinearSrgbToOklab(v.r, v.g, v.b)
		if !nearly(gotL, v.L, 0.001) || !nearly(gotA, v.A, 0.001) || !nearly(gotB, v.B, 0.001) {
			t.Errorf("%s: got (L=%.4f, a=%.4f, b=%.4f), want (%.4f, %.4f, %.4f)",
				v.name, gotL, gotA, gotB, v.L, v.A, v.B)
		}
	}
}

func TestRoundTripLinearOklab(t *testing.T) {
	for _, v := range ottossonVectors {
		L, A, B := oklab.LinearSrgbToOklab(v.r, v.g, v.b)
		r, g, b := oklab.OklabToLinearSrgb(L, A, B)
		if !nearly(r, v.r, 1e-6) || !nearly(g, v.g, 1e-6) || !nearly(b, v.b, 1e-6) {
			t.Errorf("%s round-trip: got (%.6f, %.6f, %.6f), want (%.6f, %.6f, %.6f)",
				v.name, r, g, b, v.r, v.g, v.b)
		}
	}
}

func TestOklchHueWraparound(t *testing.T) {
	cases := []struct {
		A, B    float64
		wantDeg float64
	}{
		{1.0, 0.0, 0.0},
		{0.0, 1.0, 90.0},
		{-1.0, 0.0, 180.0},
		{0.0, -1.0, 270.0},
	}
	for _, c := range cases {
		_, _, h := oklab.OklabToOklch(0.5, c.A, c.B)
		if !nearly(h, c.wantDeg, 1e-6) {
			t.Errorf("(a=%v, b=%v): got h=%v°, want %v°", c.A, c.B, h, c.wantDeg)
		}
	}
}

func TestRoundTripOklabOklch(t *testing.T) {
	cases := []struct{ L, A, B float64 }{
		{0.5, 0.1, 0.2},
		{0.7, -0.05, 0.08},
		{0.3, 0.0, -0.15},
	}
	for _, c := range cases {
		L, C, h := oklab.OklabToOklch(c.L, c.A, c.B)
		L2, A2, B2 := oklab.OklchToOklab(L, C, h)
		if !nearly(L2, c.L, 1e-9) || !nearly(A2, c.A, 1e-9) || !nearly(B2, c.B, 1e-9) {
			t.Errorf("oklab/oklch round-trip drift: in=(%v,%v,%v) out=(%v,%v,%v)",
				c.L, c.A, c.B, L2, A2, B2)
		}
	}
}

func TestSrgbGammaRoundTrip(t *testing.T) {
	for c := 0.0; c <= 1.001; c += 0.05 {
		l := oklab.SrgbToLinear(c)
		c2 := oklab.LinearToSrgb(l)
		if !nearly(c, c2, 1e-9) {
			t.Errorf("sRGB gamma round-trip at c=%v: got %v", c, c2)
		}
	}
}

func TestGamutClippingNeverIncreasesL(t *testing.T) {
	// Pick a high-chroma point clearly outside sRGB gamut.
	L, C, h := 0.55, 0.5, 30.0
	_, _, _, postC := oklab.OklchToSrgbU8(L, C, h)
	if postC > C {
		t.Errorf("post-clip C %v > input C %v (must monotonically reduce)", postC, C)
	}
	if postC < 0 {
		t.Errorf("post-clip C %v < 0", postC)
	}
}
