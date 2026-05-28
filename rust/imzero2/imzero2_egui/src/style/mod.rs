//! IDS style overlay: tokens + apply entries.
//!
//! `apply(ctx, density)` is the one-shot startup call apps make. It writes
//! IDS spacing, rounding, stroke, font, and color tokens into the active
//! `egui::Context`. Per ADR-0029 §SD13 the apply runs once and never touches
//! the render path again.

pub mod data_encoding;
pub mod tokens;

use egui::Context;

use self::tokens::density::Density;

/// Full one-shot startup overlay: spacing + visuals + rounding + stroke +
/// IDS-default fonts (replaces the host app's `FontDefinitions`).
///
/// Apps that manage their own `FontDefinitions` (e.g. the carousel demo,
/// which loads MAIN_FONT / MONO_FONT / NERD_FONT / FALLBACK_FONT from env)
/// should call [`apply_style_only`] instead — same overlay, fonts untouched.
pub fn apply(ctx: &Context, density: Density) {
    tokens::apply_fonts(ctx);
    apply_style_only(ctx, density);
}

/// Style-only overlay: spacing + visuals + rounding + stroke. Fonts are
/// left to the host app.
///
/// Use this when the app already has a font-loading pipeline you don't
/// want to displace. Visual character (palette, spacing, rounding)
/// converges with the IDS fleet; font choice stays the app's.
pub fn apply_style_only(ctx: &Context, density: Density) {
    ctx.global_style_mut(|style| {
        tokens::apply_spacing(&mut style.spacing, density);
        // Apply visuals first — it overwrites widget-table corner_radius,
        // which apply_rounding then sets to IDS values.
        tokens::apply_visuals(&mut style.visuals);
        tokens::apply_rounding(&mut style.visuals);
        tokens::apply_stroke(&mut style.visuals);
        // ADR-0030 §SD3 type-scale binding: rewrite style.text_styles so
        // egui's Body/Heading/Small/Monospace/Button slots resolve to
        // IDS pt sizes. Display + Micro land in Name slots (no built-in
        // egui tier matches them).
        tokens::apply_typography(style, density);
    });
}

/// Tour-mode neutralization: collapse `widgets.hovered.bg_stroke` and
/// `widgets.active.bg_stroke` onto `widgets.inactive.bg_stroke` so that
/// compositor-delivered focus and cursor position cannot paint an accent
/// stroke into a deterministic capture. Without this the first 1-2 demos
/// in a screenshot tour can drift run-to-run depending on whether the WM
/// has granted window focus / warped the cursor before the settle phase
/// completes. Call after [`apply`] / [`apply_style_only`] when the host
/// has decided it is in screenshot-capture mode.
pub fn apply_tour_neutral_overrides(ctx: &Context) {
    ctx.global_style_mut(|style| {
        let inactive_stroke = style.visuals.widgets.inactive.bg_stroke;
        style.visuals.widgets.hovered.bg_stroke = inactive_stroke;
        style.visuals.widgets.active.bg_stroke = inactive_stroke;
    });
}
