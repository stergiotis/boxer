//go:build llm_generated_opus47

package styletokens

// Stroke widths (ADR-0032 §SD4). Density-independent — strokes are
// perceptual constants (≥ 1 px or they vanish).
//
// MIRROR INVARIANT: must equal Rust consts in
// src/rust/imzero2_egui/src/style/tokens/stroke.rs.
const (
	// StrokeHair — subtle dividers, table grid lines, faint borders.
	StrokeHair float32 = 1.0
	// StrokeRegular — standard borders, panel outlines, control borders.
	StrokeRegular float32 = 1.5
	// StrokeStrong — focus rings, active-state outlines, emphasised dividers.
	StrokeStrong float32 = 2.0
)
