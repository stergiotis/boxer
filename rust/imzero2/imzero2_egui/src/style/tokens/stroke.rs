//! Stroke widths (ADR-0032 §SD4).
//!
//! Density-independent — strokes are perceptual constants (≥ 1 px or they
//! vanish). Hairline (1 px) carries separation without weight; the 1.5 px
//! regular is the typical UI border; 2 px strong is reserved for state and
//! focus.

/// Subtle dividers, table grid lines, faint borders.
pub const HAIR: f32 = 1.0;
/// Standard borders, panel outlines, control borders.
pub const REGULAR: f32 = 1.5;
/// Focus rings, active-state outlines, emphasised dividers.
pub const STRONG: f32 = 2.0;
