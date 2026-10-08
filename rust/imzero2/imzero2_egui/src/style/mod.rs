//! IDS style overlay: tokens + apply entries.
//!
//! `apply(ctx, density)` is the startup call apps make. It writes IDS
//! spacing, rounding, stroke, font, and color tokens into the active
//! `egui::Context`. Which colour, rounding and stroke tokens is the theme's
//! decision (`tokens::theme`, ADR-0258): the IDS dark palette, or `fresh`. Per ADR-0029 §SD13 no design-system code runs in the
//! render path: the apply is event-driven, once at startup and again only
//! when something asks for a different density — the `setIdsDensity` opcode
//! behind the host chrome's Layout ▸ Density menu (ADR-0032 §SD1, Update
//! 2026-08-23).

pub mod button;
pub mod data_encoding;
pub mod fresh;
pub mod slider;
pub mod tokens;

use egui::Context;

use self::tokens::density::Density;

/// Full one-shot startup overlay: spacing + visuals + rounding + stroke +
/// IDS-default fonts (replaces the host app's `FontDefinitions`).
///
/// Apps that manage their own `FontDefinitions` (e.g. the carousel demo, which
/// loads `MAIN_FONT` / `MONO_FONT` / `NERD_FONT` / `FALLBACK_FONT` from env)
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
        if tokens::theme::active() == tokens::Theme::Fresh {
            // ADR-0258: the light theme replaces the colour / rounding /
            // stroke trio below; spacing and typography stay shared.
            fresh::apply(style, density);
        } else {
            // Apply visuals first — it overwrites widget-table corner_radius,
            // which apply_rounding then sets to IDS values.
            tokens::apply_visuals(&mut style.visuals);
            tokens::apply_rounding(&mut style.visuals);
            tokens::apply_stroke(&mut style.visuals);
        }
        // ADR-0030 §SD3 type-scale binding: rewrite style.text_styles so
        // egui's Body/Heading/Small/Monospace/Button slots resolve to
        // IDS pt sizes. Display + Micro land in Name slots (no built-in
        // egui tier matches them).
        tokens::apply_typography(style, density);
    });
}

/// The accent at the emphasis the IDS binds construction-time fills to —
/// `ProgressBar::fill` and the etable selection stripe, which egui does
/// not read from `Visuals` — for the active theme.
pub fn accent_default() -> egui::Color32 {
    match tokens::theme::active() {
        tokens::Theme::Fresh => tokens::palette_fresh_generated::ACCENT_DEFAULT,
        tokens::Theme::Dark => tokens::palette_generated::ACCENT_DEFAULT,
    }
}

/// The slider rail's colour for the active theme; see [`slider`].
pub fn slider_rail() -> egui::Color32 {
    match tokens::theme::active() {
        tokens::Theme::Fresh => fresh::RAIL,
        tokens::Theme::Dark => slider::RAIL,
    }
}

/// Run `add` — a checkbox — with the widget table's corner radius narrowed
/// to the active theme's checkbox radius. egui has no checkbox radius of its
/// own: `Style::checkbox_style` borrows `widgets.*.corner_radius`, so a theme
/// that rounds its buttons generously rounds the checkbox's box into a
/// circle. The dark theme's radius is small enough that `add` runs as is.
pub fn with_checkbox_radius<R>(ui: &mut egui::Ui, add: impl FnOnce(&mut egui::Ui) -> R) -> R {
    let radius = match tokens::theme::active() {
        tokens::Theme::Fresh => egui::CornerRadius::same(fresh::ROUND_CHECKBOX),
        tokens::Theme::Dark => return add(ui),
    };
    let saved = set_widget_radius(&mut ui.style_mut().visuals.widgets, [radius; 5]);
    let r = add(ui);
    set_widget_radius(&mut ui.style_mut().visuals.widgets, saved);
    r
}

/// Overwrite the five widget states' corner radii; returns the previous ones.
fn set_widget_radius(
    w: &mut egui::style::Widgets,
    radii: [egui::CornerRadius; 5],
) -> [egui::CornerRadius; 5] {
    let states = [
        &mut w.noninteractive,
        &mut w.inactive,
        &mut w.hovered,
        &mut w.active,
        &mut w.open,
    ];
    let mut saved = radii;
    for ((state, radius), old) in states.into_iter().zip(radii).zip(saved.iter_mut()) {
        *old = std::mem::replace(&mut state.corner_radius, radius);
    }
    saved
}

/// Tour-mode neutralization: make hover and active look like inactive.
///
/// Collapses `widgets.hovered.bg_stroke` and `widgets.active.bg_stroke` onto
/// `widgets.inactive.bg_stroke`, so that compositor-delivered focus and cursor
/// position cannot paint an accent stroke into a deterministic capture.
///
/// Without this the first 1-2 demos in a screenshot tour can drift run-to-run
/// depending on whether the WM has granted window focus / warped the cursor
/// before the settle phase completes. Call after [`apply`] /
/// [`apply_style_only`] when the host has decided it is in screenshot-capture
/// mode.
pub fn apply_tour_neutral_overrides(ctx: &Context) {
    ctx.global_style_mut(|style| {
        let inactive_stroke = style.visuals.widgets.inactive.bg_stroke;
        style.visuals.widgets.hovered.bg_stroke = inactive_stroke;
        style.visuals.widgets.active.bg_stroke = inactive_stroke;
    });
    button::set_tour_neutral();
}
