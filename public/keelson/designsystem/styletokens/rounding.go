package styletokens

// Rounding scale (ADR-0032 §SD3). Density-independent.
//
// MIRROR INVARIANT: must equal Rust consts in
// src/rust/imzero2_egui/src/style/tokens/rounding.rs.
const (
	// RoundingNone — sharp corners, the Swiss default for most surfaces.
	RoundingNone float32 = 0.0
	// RoundingSm — subtle softening: buttons, inline chips. Not badges, which
	// are rounder so that they read apart from a button (ADR-0273 SD5).
	RoundingSm float32 = 2.0
	// RoundingMd — cards, dialogs, panels.
	RoundingMd float32 = 4.0
	// RoundingLg — floating windows, modals, popovers.
	RoundingLg float32 = 6.0
)

// RoundingButton returns the corner radius the active theme gives a button,
// a text field or a combo box at rest. A control composed from primitives
// around buttons (an outline enclosing them) takes its radius from here, so
// it follows the theme it is drawn in (ADR-0404 §SD8).
func RoundingButton() (v float32) { return roundingButton(ActiveTheme()) }

// roundingButton mirrors imzero2_egui's style: the IDS overlay sets every
// widget state to RoundingSm (tokens/mod.rs apply_rounding), and the fresh
// theme replaces it with freshRoundingWidget (fresh.rs ROUND_WIDGET).
func roundingButton(th ThemeE) (v float32) {
	v = RoundingSm
	if th == ThemeFresh {
		v = freshRoundingWidget
	}
	return
}

// freshRoundingWidget is the fresh theme's resting widget radius.
const freshRoundingWidget = 8
