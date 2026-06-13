// Package oklab implements OKLab / OKLCh ↔ sRGB conversions per
// Björn Ottosson, "A perceptual color space for image processing" (2020):
// https://bottosson.github.io/posts/oklab/
//
// Used by the IDS color generator (ADR-0033 §SD2) to construct the
// semantic palette in OKLCh and emit sRGB Color32 constants. Pure Go,
// zero deps, deterministic; mirrored to Rust by hand at
// src/rust/imzero2_egui/src/style/tokens/oklab.rs.
//
// All sRGB values are in [0, 1] (gamma-encoded) unless noted as "linear".
package oklab

import "math"

// SrgbToLinear applies the sRGB transfer function (IEC 61966-2-1).
func SrgbToLinear(c float64) (l float64) {
	if c <= 0.04045 {
		l = c / 12.92
	} else {
		l = math.Pow((c+0.055)/1.055, 2.4)
	}
	return
}

// LinearToSrgb is the inverse of SrgbToLinear.
func LinearToSrgb(l float64) (c float64) {
	if l <= 0.0031308 {
		c = 12.92 * l
	} else {
		c = 1.055*math.Pow(l, 1.0/2.4) - 0.055
	}
	return
}

// LinearSrgbToOklab — Ottosson 2020 forward transform.
// Inputs are linear-sRGB; outputs are OKLab (L, a, b).
func LinearSrgbToOklab(r, g, b float64) (L, A, B float64) {
	l := 0.4122214708*r + 0.5363325363*g + 0.0514459929*b
	m := 0.2119034982*r + 0.6806995451*g + 0.1073969566*b
	s := 0.0883024619*r + 0.2817188376*g + 0.6299787005*b

	l_ := math.Cbrt(l)
	m_ := math.Cbrt(m)
	s_ := math.Cbrt(s)

	L = 0.2104542553*l_ + 0.7936177850*m_ - 0.0040720468*s_
	A = 1.9779984951*l_ - 2.4285922050*m_ + 0.4505937099*s_
	B = 0.0259040371*l_ + 0.7827717662*m_ - 0.8086757660*s_
	return
}

// OklabToLinearSrgb is the inverse of LinearSrgbToOklab.
func OklabToLinearSrgb(L, A, B float64) (r, g, b float64) {
	l_ := L + 0.3963377774*A + 0.2158037573*B
	m_ := L - 0.1055613458*A - 0.0638541728*B
	s_ := L - 0.0894841775*A - 1.2914855480*B

	l := l_ * l_ * l_
	m := m_ * m_ * m_
	s := s_ * s_ * s_

	r = +4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	g = -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	b = -0.0041960863*l - 0.7034186147*m + 1.7076147010*s
	return
}

// OklabToOklch converts (L, a, b) → (L, C, h°). Hue in degrees [0, 360).
func OklabToOklch(L, A, B float64) (Lout, C, hDeg float64) {
	C = math.Hypot(A, B)
	hDeg = math.Atan2(B, A) * 180.0 / math.Pi
	if hDeg < 0 {
		hDeg += 360.0
	}
	Lout = L
	return
}

// OklchToOklab converts (L, C, h°) → (L, a, b). Inverse of OklabToOklch.
func OklchToOklab(L, C, hDeg float64) (Lout, A, B float64) {
	hRad := hDeg * math.Pi / 180.0
	A = C * math.Cos(hRad)
	B = C * math.Sin(hRad)
	Lout = L
	return
}

// SrgbU8 converts a [0, 1] sRGB component to a clamped uint8.
func SrgbU8(c float64) (u uint8) {
	if c < 0 {
		c = 0
	} else if c > 1 {
		c = 1
	}
	u = uint8(math.Round(c * 255.0))
	return
}

// OklchToSrgbU8 takes an OKLCh target, performs gamut clipping by chroma
// reduction (ADR-0033 §SD5: "L is never reduced"), and returns the
// gamma-encoded sRGB triple plus the post-clip C actually used.
//
// Stops at chromaReductionStep granularity (default 0.005 per ADR-0033).
func OklchToSrgbU8(L, C, hDeg float64) (r, g, b uint8, postClipC float64) {
	const step = 0.005
	c := C
	for {
		_, A, B := OklchToOklab(L, c, hDeg)
		lr, lg, lb := OklabToLinearSrgb(L, A, B)
		if inUnitCube(lr, lg, lb) || c <= 0 {
			r = SrgbU8(LinearToSrgb(lr))
			g = SrgbU8(LinearToSrgb(lg))
			b = SrgbU8(LinearToSrgb(lb))
			postClipC = c
			return
		}
		c -= step
		if c < 0 {
			c = 0
		}
	}
}

func inUnitCube(r, g, b float64) (in bool) {
	const eps = 1e-6
	in = r >= -eps && r <= 1+eps &&
		g >= -eps && g <= 1+eps &&
		b >= -eps && b <= 1+eps
	return
}
