package styletokens

// Type scale (ADR-0030 §SD3) — five size steps at Standard density.
//
// MIRROR INVARIANT: must equal Rust consts in
// src/rust/imzero2_egui/src/style/tokens/typography.rs. The drift test
// in styletokens_drift_test.go parses the Rust source and asserts
// equality.
//
// The IDS apply path on the Rust side writes these into
// `egui::Style::text_styles` (see `imzero2_egui::style::tokens::apply_typography`):
//   - TextStyle::Heading → HeadingPt
//   - TextStyle::Body    → BodyPt
//   - TextStyle::Button  → BodyPt
//   - TextStyle::Small   → CaptionPt
//   - TextStyle::Monospace → BodyPt (with FontFamily::Monospace)
//   - TextStyle::Name("ids-display") → DisplayPt
//   - TextStyle::Name("ids-micro")   → MicroPt
//
// Go-side code that wants Display or Micro at the active density can
// call `ScaledPt(DisplayPt, density)` and pass the result to
// `RichTextScope.Size(...)` until the bindings expose a TextStyle::Name
// setter.
const (
	// DisplayPt — app-level title, prominent panel headers.
	DisplayPt float32 = 22.0
	// HeadingPt — sub-panel header, dialog title.
	HeadingPt float32 = 16.0
	// BodyPt — default UI text, button/menu labels, table rows.
	BodyPt float32 = 13.0
	// CaptionPt — plot axis labels, secondary text, badge content.
	CaptionPt float32 = 11.0
	// MicroPt — fine print, status-bar metrics, watermark.
	MicroPt float32 = 9.0

	// BodyLeading — line height of reading prose as a multiple of the body
	// size, the `--ids-line` of the IDS web stylesheet (ADR-0076): a
	// document set at the font's own line height (about 1.4 for the
	// bundled faces) read tighter than every HTML reference in the
	// markdown-rendering-quality trial. UI chrome keeps the font's height;
	// this is for runs of prose.
	BodyLeading float32 = 1.6
)

// ScaledPt applies the per-density adjustment to a base size per
// ADR-0030 §SD3: Tight subtracts 1 pt (floored at 9), Roomy adds 1 pt.
func ScaledPt(base float32, d DensityE) (v float32) {
	switch d {
	case DensityTight:
		v = base - 1.0
		if v < 9.0 {
			v = 9.0
		}
	case DensityRoomy:
		v = base + 1.0
	default:
		v = base
	}
	return
}
