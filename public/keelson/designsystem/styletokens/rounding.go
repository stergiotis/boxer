//go:build llm_generated_opus47

package styletokens

// Rounding scale (ADR-0032 §SD3). Density-independent.
//
// MIRROR INVARIANT: must equal Rust consts in
// src/rust/imzero2_egui/src/style/tokens/rounding.rs.
const (
	// RoundingNone — sharp corners, the Swiss default for most surfaces.
	RoundingNone float32 = 0.0
	// RoundingSm — subtle softening: buttons, badges, inline chips.
	RoundingSm float32 = 2.0
	// RoundingMd — cards, dialogs, panels.
	RoundingMd float32 = 4.0
	// RoundingLg — floating windows, modals, popovers.
	RoundingLg float32 = 6.0
)
