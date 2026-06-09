package gauge

import (
	"hash/fnv"
	"math"
	"strconv"
	"sync"
	"unicode/utf8"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Readout auto-fit. The centre value readout must sit inside the dial's inner
// opening; a wide readout — a multi-digit value plus a unit suffix, e.g.
// "8500 mAh" — at the display type size overruns the arc and collides with the
// interior tick labels. fitReadoutFont shrinks the readout font just enough to
// fit the available chord, measuring the real width with egui's MeasureText.
//
// MeasureText reports the width through a Go-side databinding populated on the
// next StateManager.Sync, so the value lands ONE FRAME after the call (the
// colorscale cachingMeasurer idiom). The first frame a given readout string is
// seen therefore uses an approximation and the dial settles to the exact fit
// on the next frame. State is held per gauge instance (keyed by the stable
// canvas-id hash), not per value, so the store holds one entry per live dial
// and does not grow as telemetry changes.

const (
	// readoutFitSafety keeps the readout clear of the band's inner edge and the
	// interior tick labels: the usable width is this fraction of the inner
	// chord at the readout's height.
	readoutFitSafety float32 = 0.82
	// readoutMinFontFrac floors the shrink so a very small dial still renders a
	// legible (if cramped) readout rather than a vanishing one.
	readoutMinFontFrac float32 = 0.55
	// approxGlyphFrac is the first-frame per-rune width estimate (as a fraction
	// of the font size), deliberately a touch generous so the settling frame
	// shrinks rather than overflows. Superseded by the real MeasureText width on
	// the next frame.
	approxGlyphFrac float64 = 0.62
)

// fitState is the cross-frame readout measurement for one gauge instance. width
// is pointer-bound: MeasureTextBind writes the measured width here on the next
// Sync. text/font record what the width was measured against, so a value change
// re-seeds the approximation until the real width arrives.
type fitState struct {
	text  string
	font  float32
	width float64
}

// fitStates maps a gauge readout's measureId to its fitState. Keyed by the
// stable per-instance canvas-id hash, so the map is bounded by the number of
// live dials (the distsummary/regexsummary instanceStates idiom); like those it
// is not evicted — a dial that stops rendering leaves a tiny stale entry. The
// egui2 render loop is single-threaded, so a given entry is only ever touched
// from one goroutine (Render and the between-frame Sync that writes width).
var fitStates sync.Map // map[uint64]*fitState

// readoutMeasureId derives a stable, collision-resistant measurement id from the
// gauge's canvas identity (idPrefix + callId), salted distinct from the canvas
// id itself so it never clashes with another widget's animation/measure slot.
func readoutMeasureId(idPrefix string, callId uint64) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(idPrefix))
	_, _ = h.Write([]byte("#"))
	_, _ = h.Write([]byte(strconv.FormatUint(callId, 16)))
	_, _ = h.Write([]byte("-gauge-readout"))
	return h.Sum64()
}

// readoutAvailWidth is the usable readout width: the chord of the band's inner
// edge at the readout's vertical offset, scaled by readoutFitSafety. Returns 0
// when the readout sits at or beyond the inner radius (a degenerate dial), in
// which case the caller leaves the font unshrunk.
func readoutAvailWidth(innerR, yOff float32) float32 {
	h := innerR*innerR - yOff*yOff
	if h <= 0 {
		return 0
	}
	return 2 * float32(math.Sqrt(float64(h))) * readoutFitSafety
}

// fitReadoutFont returns the font size at which text fits within availWidth,
// never larger than baseFont and never smaller than baseFont*readoutMinFontFrac.
// It registers/refreshes a MeasureText databinding so the exact width is known
// next frame; until then (or when availWidth is non-positive) it uses an
// approximation. Width scales linearly with font size, so the real width is
// always measured at baseFont and the result scaled arithmetically — measuring
// at the shrunk size instead would oscillate.
func fitReadoutFont(measureId uint64, text string, baseFont, availWidth float32) float32 {
	v, ok := fitStates.Load(measureId)
	if !ok {
		v, _ = fitStates.LoadOrStore(measureId, &fitState{})
	}
	s := v.(*fitState)
	if s.text != text || s.font != baseFont {
		s.text = text
		s.font = baseFont
		s.width = approxReadoutWidth(text, baseFont)
	}
	// Refresh the databinding every frame so the width stays current across
	// Sync's databind-reset semantics (the colorscale RenewBindings pattern).
	c.MeasureTextBind(measureId, text, baseFont, false, &s.width)

	return fitFontForWidth(s.width, baseFont, availWidth)
}

// fitFontForWidth is the pure scaling decision: the font at which a string whose
// width is measuredW at baseFont fits availWidth, clamped to
// [baseFont*readoutMinFontFrac, baseFont] and never upscaled past baseFont.
// Split out from the MeasureText plumbing so it is unit-testable without an
// egui app (the ADR-0068 §SD8 pure-function test ethos). A non-positive
// availWidth or measuredW (a degenerate dial, or width not yet measured) leaves
// the font at baseFont.
func fitFontForWidth(measuredW float64, baseFont, availWidth float32) float32 {
	if availWidth <= 0 || measuredW <= 0 {
		return baseFont
	}
	scale := float64(availWidth) / measuredW
	if scale >= 1 {
		return baseFont // already fits — never upscale past the design size
	}
	font := baseFont * float32(scale)
	if minFont := baseFont * readoutMinFontFrac; font < minFont {
		font = minFont
	}
	return font
}

// approxReadoutWidth is the first-frame width estimate (runes × approxGlyphFrac
// × font size) used until MeasureText returns the real width. Rune-counted so
// multibyte units ("°C") are not over-weighted.
func approxReadoutWidth(text string, font float32) float64 {
	return float64(utf8.RuneCountInString(text)) * approxGlyphFrac * float64(font)
}
