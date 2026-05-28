//! Rounding tokens (ADR-0032 §SD3).
//!
//! Density-independent — rounding is aesthetic identity, not perceptual
//! magnitude. Swiss-minimalist defaults lean toward `NONE` and `SM`; `LG`
//! is reserved for floating windows that need visual separation from
//! screen edges.

/// Sharp corners. The Swiss default for most surfaces.
pub const NONE: f32 = 0.0;
/// Subtle softening: buttons, badges, inline chips.
pub const SM: f32 = 2.0;
/// Cards, dialogs, panels.
pub const MD: f32 = 4.0;
/// Floating windows, modals, popovers.
pub const LG: f32 = 6.0;
