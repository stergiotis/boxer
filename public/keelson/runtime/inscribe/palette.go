package inscribe

import "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"

// The overlay's colours (ADR-0297 §SD6). Marks are drawn in a neon palette
// outside the design system on purpose: a mark must not read as part of any
// app. They are named here and used nowhere else; each constructor carries
// the design lint's per-line exception with that decision as its reason.

type rgb struct{ r, g, b uint8 }

// neon are the task hues, assigned in order.
var neon = []rgb{
	{57, 255, 20},
	{255, 32, 121},
	{0, 240, 255},
	{255, 231, 0},
	{255, 95, 31},
	{188, 19, 254},
}

// swipeAlpha is a highlighter swipe's opacity: the content stays readable
// through it.
const swipeAlpha = 72

func hueOf(hue int) rgb { return neon[((hue%len(neon))+len(neon))%len(neon)] }

// hueColor is a hue's stroke colour; hues past the palette repeat.
func hueColor(hue int) color.Color {
	h := hueOf(hue)
	return color.RGB(h.r, h.g, h.b) // designlint:ignore=L2 (ADR-0297 §SD6: neon outside IDS)
}

// swipeColor is a hue as a highlighter's translucent ink.
func swipeColor(hue int) color.Color {
	h := hueOf(hue)
	return color.RGBA(h.r, h.g, h.b, swipeAlpha) // designlint:ignore=L2 (ADR-0297 §SD6: neon outside IDS)
}

var (
	// underlay goes faintly under every stroke, so neon keeps its edge on a
	// light theme without a border on a dark one.
	underlay = color.RGBA(0, 0, 0, 90) // designlint:ignore=L2 (ADR-0297 §SD6: underlay under neon)
	// shadow sits under a note, offset, so it reads as lying on the screen.
	shadow = color.RGBA(0, 0, 0, 70) // designlint:ignore=L2 (ADR-0297 §SD6: note shadow)
	// plate is a note's background, plateText its text, tagText its
	// attribution, dimmed.
	plate     = color.RGBA(16, 16, 20, 240)    // designlint:ignore=L2 (ADR-0297 §SD6: note plate)
	plateText = color.RGB(246, 246, 246)       // designlint:ignore=L2 (ADR-0297 §SD6: note text)
	tagText   = color.RGBA(246, 246, 246, 150) // designlint:ignore=L2 (ADR-0297 §SD6: note attribution)
	// dim covers what a spotlight leaves out.
	dim = color.RGBA(0, 0, 0, 120) // designlint:ignore=L2 (ADR-0297 §SD6: spotlight dim)
)
