package implot

import "unicode/utf8"

// Text-width estimation — the lane's shared answer to "how wide will this
// label be", for layout decisions that have to be made synchronously.
//
// Estimation is a choice, not the only option. Real measurement exists:
// `bindings.MeasureText` writes a width into a Go-side databinding that Sync
// populates, so the value arrives one frame after the call — `widgets/
// colorscale` drives it that way, seeding its cache with an estimate on the
// first frame and re-running its layout once the real widths land. What
// ADR-0149 §SD6 defers is a fetcher inside *this* package for its own tick and
// legend sizing, not the channel itself.
//
// So: reach for the estimate when a few percent of error is invisible (a
// gutter, a tooltip box, deciding whether a label fits its bar), and for
// MeasureText when a layout has to be exact. What this file exists to stop is
// every widget inventing its own ratio — label fitting that disagrees between
// two charts in the same pane reads as a bug.

// GlyphWidthRatio is the house estimate of a glyph's advance as a fraction of
// the font size, for the proportional UI font at Latin text. It is also close
// enough for the monospace face, whose digits this package's tick labels are.
const GlyphWidthRatio = 0.62

// EstimateTextWidth estimates the rendered width of s at fontSize, in the
// canvas pixels every Paint* opcode takes.
//
// It counts runes, not bytes. Charging a multi-byte glyph once per byte
// overstates every non-ASCII label by its encoding length — a three-byte CJK
// glyph billed at 1.86 em against a true advance near 1.0 — which is how a
// widget ends up hiding labels that would have fit.
//
// Kerning, ligatures and font fallback are not modelled, so treat the result
// as a budget rather than a measurement.
func EstimateTextWidth(s string, fontSize float32) float32 {
	var w float32
	for _, r := range s {
		w += EstimateRuneWidth(r, fontSize)
	}
	return w
}

// EstimateRuneWidth is EstimateTextWidth for one rune — the form an elision
// loop wants, so it can walk a string without building a candidate per step.
func EstimateRuneWidth(r rune, fontSize float32) float32 {
	if isWideRune(r) {
		return fontSize
	}
	return fontSize * GlyphWidthRatio
}

// Elide shortens s to fit availPx at fontSize, appending an ellipsis, or
// returns "" when not even the ellipsis and one glyph would fit — a
// one-glyph label is noise, not information.
//
// It lives here, beside the estimate, because it is the same decision: the
// string it returns fits the space it was cut for only because it was
// budgeted with the estimate the caller sized the space with. Two widgets
// eliding by different rules is the disagreement this file exists to stop.
//
// The budget is pixels rather than characters, which a character budget
// cannot approximate: it charges a CJK glyph the same as an "l", and those
// labels then overflow the box they were cut to fit.
//
// Not every widget wants this — a bar chart that drops a label it cannot fit
// is making a different, equally reasonable call. This is for the ones that
// would rather show a prefix.
func Elide(s string, availPx float32, fontSize float32) string {
	if EstimateTextWidth(s, fontSize) <= availPx {
		return s
	}
	budget := availPx - EstimateRuneWidth('…', fontSize)
	used, cut := float32(0), 0
	for i, r := range s {
		w := EstimateRuneWidth(r, fontSize)
		if used+w > budget {
			break
		}
		used, cut = used+w, i+utf8.RuneLen(r)
	}
	if cut == 0 {
		return "" // not even one glyph beside the ellipsis
	}
	return s[:cut] + "…"
}

// wideRuneRanges are the East Asian Wide and Fullwidth blocks, charged a full
// em because those glyphs really are about twice the Latin average this
// package's ratio describes.
//
// It is an abridged table kept local rather than a dependency on a width
// package: the blocks below are where the one-em error actually bites, and the
// result is an estimate either way. Ambiguous-width runes are treated as
// narrow, which is the right default outside an East Asian locale.
var wideRuneRanges = [...][2]rune{
	{0x1100, 0x115F},   // Hangul Jamo initials
	{0x2E80, 0x303E},   // CJK radicals, Kangxi, CJK symbols and punctuation
	{0x3041, 0x33FF},   // kana, Bopomofo, Hangul compatibility, CJK compat
	{0x3400, 0x4DBF},   // CJK unified ideographs extension A
	{0x4E00, 0x9FFF},   // CJK unified ideographs
	{0xA000, 0xA4CF},   // Yi syllables and radicals
	{0xAC00, 0xD7A3},   // Hangul syllables
	{0xF900, 0xFAFF},   // CJK compatibility ideographs
	{0xFE30, 0xFE6F},   // CJK compatibility forms, small form variants
	{0xFF00, 0xFF60},   // fullwidth forms
	{0xFFE0, 0xFFE6},   // fullwidth signs
	{0x1F300, 0x1F64F}, // emoji
	{0x1F900, 0x1F9FF}, // supplemental emoji
	{0x20000, 0x3FFFD}, // CJK unified ideographs extension B and beyond
}

func isWideRune(r rune) bool {
	if r < wideRuneRanges[0][0] {
		return false // the fast path: all of Latin, Greek, Cyrillic, symbols
	}
	for _, rg := range wideRuneRanges {
		if r >= rg[0] && r <= rg[1] {
			return true
		}
	}
	return false
}
