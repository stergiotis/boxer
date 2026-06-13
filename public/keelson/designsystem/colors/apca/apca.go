// Package apca implements the Accessible Perceptual Contrast Algorithm
// (APCA) per Andrew Somers' Myndex SAPC-APCA Beta 0.1.9 reference.
//
// APCA is the proposed contrast algorithm for WCAG 3.0 (Silver) and is
// the technically-correct choice for IDS's dark-theme-only palette
// (per ADR-0031 §SD5 — replacing the previous WCAG 2.1 binding via the
// M0b Amendment to ADR-0033).
//
// The algorithm:
//   1. Convert sRGB to a per-APCA luminance (linear-sRGB ^ 2.4 then
//      coefficient-weighted; *not* the WCAG 2.1 relative-luminance form).
//   2. Soft-clamp very dark luminances (< 0.022) per the blkThrs / blkClmp
//      parameters to model halation against true black.
//   3. Apply the SAPC S-curve, signed by orientation (dark-on-light vs
//      light-on-dark), with separate exponents per direction.
//   4. Scale and offset; return Lc as a signed value in approximately
//      [-108, 106]. Magnitude is what gates accessibility; sign carries
//      orientation.
//
// Reference: https://github.com/Myndex/SAPC-APCA (Beta 0.1.9 constants
// quoted verbatim below). The APCA algorithm and constants are
// copyright Andrew Somers (Myndex Research) — algorithm in the public
// domain; the W3C draft uses these values.
//
// Threshold guidance (Lc magnitude):
//   - 90: minimum body text (~13 pt, regular weight)
//   - 75: ideal body text
//   - 60: large text + meaningful UI components
//   - 45: large bold text + ambient UI
//   - 30: minimum non-text UI elements
//   - 15: minimum floating UI / decorative
package apca

import "math"

// APCA constants per Myndex Beta 0.1.9.
const (
	mainTRC = 2.4 // gamma exponent

	rCoef = 0.2126729
	gCoef = 0.7151522
	bCoef = 0.0721750

	// SAPC S-curve exponents
	normBG  = 0.56 // dark text on light bg, bg side
	normTXT = 0.57 // dark text on light bg, text side
	revTXT  = 0.62 // light text on dark bg, text side
	revBG   = 0.65 // light text on dark bg, bg side

	// Black-soft-clamp threshold + curve
	blkThrs = 0.022
	blkClmp = 1.414

	// Scale + offset
	scaleBoW    = 1.14
	scaleWoB    = 1.14
	loBoWOffset = 0.027
	loWoBOffset = 0.027
	loClip      = 0.001

	deltaYmin = 0.0005
)

// Luminance computes the APCA per-channel luminance.
// Input r/g/b are gamma-encoded sRGB [0, 255].
func Luminance(r, g, b uint8) (y float64) {
	rf := float64(r) / 255.0
	gf := float64(g) / 255.0
	bf := float64(b) / 255.0
	y = rCoef*math.Pow(rf, mainTRC) + gCoef*math.Pow(gf, mainTRC) + bCoef*math.Pow(bf, mainTRC)
	return
}

// softClamp applies the blkThrs / blkClmp soft-clamp curve for very dark
// luminance values. Used before the S-curve evaluation.
func softClamp(y float64) (out float64) {
	if y >= blkThrs {
		out = y
		return
	}
	out = y + math.Pow(blkThrs-y, blkClmp)
	return
}

// Lc returns the APCA Lightness Contrast value, signed.
// Positive ≈ dark text on light background (BoW).
// Negative ≈ light text on dark background (WoB — what IDS uses).
// Magnitude is what matters for accessibility thresholds.
//
// Inputs are gamma-encoded sRGB uint8s for text and background.
func Lc(textR, textG, textB, bgR, bgG, bgB uint8) (lc float64) {
	yText := softClamp(Luminance(textR, textG, textB))
	yBg := softClamp(Luminance(bgR, bgG, bgB))

	// Below the minimum luminance delta — return 0 (no perceivable contrast).
	if math.Abs(yBg-yText) < deltaYmin {
		return
	}

	var sapc, out float64
	if yBg > yText {
		// Dark text on light background (BoW = Black on White polarity).
		sapc = (math.Pow(yBg, normBG) - math.Pow(yText, normTXT)) * scaleBoW
		if sapc < loClip {
			out = 0
		} else {
			out = sapc - loBoWOffset
		}
	} else {
		// Light text on dark background (WoB) — IDS's primary case.
		sapc = (math.Pow(yBg, revBG) - math.Pow(yText, revTXT)) * scaleWoB
		if sapc > -loClip {
			out = 0
		} else {
			out = sapc + loWoBOffset
		}
	}

	lc = out * 100.0
	// APCA reference rounds to one decimal place for display; we keep
	// full precision and let the caller decide.
	return
}

// Threshold returns the minimum Lc magnitude required for a given
// (font size in pt, font weight as 400/500/600/700) combination.
//
// Derived from the Myndex Bronze-Simple lookup table (the conservative
// public-facing thresholds). Returns 0 for combinations outside the
// table; callers should treat "untyped" UI elements as needing Lc ≥ 60
// (ambient UI minimum).
//
// Source: https://git.apcacontrast.com/documentation/APCAlookup —
// "Bronze simple table" reduced to (size, weight) → Lc.
func Threshold(sizePt float64, weight int) (lc float64) {
	// Conservative bronze simple thresholds — dense table compressed
	// into a stepwise function over (sizePt, weight). The reference
	// table has 7 sizes × 7 weights with per-cell values; we model the
	// dominant break-points.
	switch {
	case sizePt >= 24 && weight >= 700:
		lc = 45 // large + bold — most permissive
	case sizePt >= 18 && weight >= 700:
		lc = 60
	case sizePt >= 18:
		lc = 70
	case sizePt >= 14 && weight >= 500:
		lc = 75
	case sizePt >= 14:
		lc = 80
	case sizePt >= 12 && weight >= 500:
		lc = 85
	case sizePt >= 12:
		lc = 90 // minimum body text
	case weight >= 700:
		lc = 90
	default:
		lc = 100 // sub-12pt regular weight — APCA strongly discourages
	}
	return
}

// UIThreshold returns the minimum Lc for non-text UI elements.
//   - "meaningful": Lc ≥ 60 (icons that convey state)
//   - "ambient": Lc ≥ 30 (decorative borders, faint dividers)
//   - "floating": Lc ≥ 15 (rarely-seen UI; very permissive)
func UIThreshold(category string) (lc float64) {
	switch category {
	case "meaningful":
		lc = 60
	case "ambient":
		lc = 30
	case "floating":
		lc = 15
	default:
		lc = 60
	}
	return
}
