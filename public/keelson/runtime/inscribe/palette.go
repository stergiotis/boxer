package inscribe

import "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"

// The overlay's colours (ADR-0297 §SD6). Marks are drawn in a neon
// palette outside the design system on purpose: a mark must not read as part
// of any app. They are named here and used nowhere else; each carries the
// design lint's per-line exception with that decision as its reason.

// neon are the task hues, assigned in order.
var neon = []color.Color{
	color.RGB(57, 255, 20),  // designlint:ignore=L2 (ADR-0297 §SD6: neon outside IDS)
	color.RGB(255, 32, 121), // designlint:ignore=L2 (ADR-0297 §SD6: neon outside IDS)
	color.RGB(0, 240, 255),  // designlint:ignore=L2 (ADR-0297 §SD6: neon outside IDS)
	color.RGB(255, 231, 0),  // designlint:ignore=L2 (ADR-0297 §SD6: neon outside IDS)
	color.RGB(255, 95, 31),  // designlint:ignore=L2 (ADR-0297 §SD6: neon outside IDS)
	color.RGB(188, 19, 254), // designlint:ignore=L2 (ADR-0297 §SD6: neon outside IDS)
}

var (
	// underlay goes faintly under every stroke, so neon keeps its edge on a
	// light theme without a border on a dark one.
	underlay = color.RGBA(0, 0, 0, 90) // designlint:ignore=L2 (ADR-0297 §SD6: underlay under neon)
	// plate is a note's background, plateText its text.
	plate     = color.RGBA(14, 14, 18, 238) // designlint:ignore=L2 (ADR-0297 §SD6: note plate)
	plateText = color.RGB(246, 246, 246)    // designlint:ignore=L2 (ADR-0297 §SD6: note text)
	// dim covers what a spotlight leaves out.
	dim = color.RGBA(0, 0, 0, 120) // designlint:ignore=L2 (ADR-0297 §SD6: spotlight dim)
)

// hueColor is a hue's colour; hues past the palette repeat.
func hueColor(hue int) color.Color {
	return neon[((hue%len(neon))+len(neon))%len(neon)]
}
