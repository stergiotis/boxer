//! IDS token modules.
//!
//! Per ADR-0029 §SD2: tokens are `const`. Apps materialise them once at
//! startup via `style::apply(ctx, density)` (or, for finer-grained control,
//! the per-subsystem `apply_*` helpers re-exported below).
//!
//! Adding, removing, or renaming a token requires a Tier 3 ADR.

pub mod branding;
pub mod density;
pub mod motion;
pub mod oklab;
pub mod palette_generated;
pub mod rounding;
pub mod spacing;
pub mod stroke;
pub mod typography;
pub mod visuals;

pub use branding::{body_mono_family, mode_from_env, ScreenshotMode};
pub use typography::{apply_fonts, install_fonts};
pub use visuals::{apply_visuals, diverging, qualitative_cycle, sequential, DivergingE, SequentialE};

pub use density::Density;

use egui::style::{Spacing, Style, Visuals};
use egui::{FontFamily, FontId, TextStyle};

/// Write IDS spacing tokens into `egui::Spacing`. ADR-0032 §SD7 mapping.
pub fn apply_spacing(spacing: &mut Spacing, density: Density) {
    spacing.button_padding = egui::vec2(
        self::spacing::padding_default(density),
        self::spacing::padding_inner(density),
    );
    // ADR-0032 §SD7 amendment (2026-05-16): window_margin uses padding_default
    // rather than padding_outer to tighten the title-bar surround.
    spacing.window_margin = egui::Margin::same(self::spacing::padding_default(density) as i8);
    spacing.menu_margin = egui::Margin::same(self::spacing::padding_inner(density) as i8);
    spacing.item_spacing = egui::vec2(
        self::spacing::gap_items(density),
        self::spacing::gap_items(density),
    );
    spacing.icon_spacing = self::spacing::gap_inline(density);
    spacing.indent = self::spacing::padding_outer(density);
    spacing.interact_size = egui::vec2(
        self::spacing::px(density, 6),
        self::spacing::padding_outer(density),
    );
}

/// Write IDS rounding tokens into `egui::Visuals`. ADR-0032 §SD7.
///
/// Swiss-restrained default: most surfaces sharp; cards / dialogs softened by
/// `Md`; floating windows by `Lg`.
pub fn apply_rounding(visuals: &mut Visuals) {
    let md = egui::CornerRadius::same(self::rounding::MD as u8);
    let sm = egui::CornerRadius::same(self::rounding::SM as u8);
    // ADR-0032 §SD7: ROUNDING_MD → window_corner_radius / menu_corner_radius.
    // (Earlier code used LG for windows; recovered to MD per the §SD7 spec
    // in the 2026-05-16 amendment.)
    visuals.window_corner_radius = md;
    visuals.menu_corner_radius = md;
    visuals.widgets.noninteractive.corner_radius = sm;
    visuals.widgets.inactive.corner_radius = sm;
    visuals.widgets.hovered.corner_radius = sm;
    visuals.widgets.active.corner_radius = sm;
    visuals.widgets.open.corner_radius = sm;
}

/// Write IDS type-scale tokens into `egui::Style::text_styles`.
/// ADR-0030 §SD3 / §SD6 — five size steps plus a monospace body slot.
///
/// Mapping decisions:
///
/// - `TextStyle::Heading` → IDS Heading (16 pt). Direct match.
/// - `TextStyle::Body` → IDS Body (13 pt). Direct match.
/// - `TextStyle::Monospace` → IDS Body.Mono (13 pt, monospace family).
/// - `TextStyle::Button` → IDS Body (13 pt). ADR-0030 §SD3 lists
///   "button/menu labels" under Body, so this stays at Body size.
/// - `TextStyle::Small` → IDS Caption (11 pt). Egui's `Small` maps to
///   IDS's secondary-text slot.
/// - `TextStyle::Name("ids-display")` → IDS Display (22 pt). Egui has no
///   built-in "display" tier, so it lands in a Name slot. Reach for it
///   with `RichText::new(t).text_style(TextStyle::Name("ids-display".into()))`
///   (or `.size(tokens::typography::DISPLAY_PT)` for the inline path the
///   Go-side bindings can already emit).
/// - `TextStyle::Name("ids-micro")` → IDS Micro (9 pt). Same shape.
///
/// All sizes are scaled by the active density per ADR-0030 §SD3:
/// Tight subtracts 1 pt (floored at 9), Roomy adds 1 pt.
pub fn apply_typography(style: &mut Style, density: Density) {
    use typography::{
        scaled, BODY_PT, CAPTION_PT, DISPLAY_PT, HEADING_PT, MICRO_PT,
    };

    let proportional = FontFamily::Proportional;
    let monospace = FontFamily::Monospace;

    style.text_styles = std::collections::BTreeMap::from([
        (
            TextStyle::Small,
            FontId::new(scaled(CAPTION_PT, density), proportional.clone()),
        ),
        (
            TextStyle::Body,
            FontId::new(scaled(BODY_PT, density), proportional.clone()),
        ),
        (
            TextStyle::Monospace,
            FontId::new(scaled(BODY_PT, density), monospace),
        ),
        (
            TextStyle::Button,
            FontId::new(scaled(BODY_PT, density), proportional.clone()),
        ),
        (
            TextStyle::Heading,
            FontId::new(scaled(HEADING_PT, density), proportional.clone()),
        ),
        (
            TextStyle::Name("ids-display".into()),
            FontId::new(scaled(DISPLAY_PT, density), proportional.clone()),
        ),
        (
            TextStyle::Name("ids-micro".into()),
            FontId::new(scaled(MICRO_PT, density), proportional),
        ),
    ]);
}

/// Write IDS stroke widths into `egui::Visuals`. ADR-0032 §SD7.
///
/// Note on `widgets.hovered.bg_stroke.width`: egui's `Style::button_style()`
/// allocates the button frame via `Margin` (i8) and rounds
/// `inner = round(button_padding + expansion - stroke)` and
/// `outer = round(-expansion)`. The total-rect-stable invariant only holds
/// when `expansion` and `bg_stroke.width` are both integer-valued; with
/// `expansion=1.0` and `stroke=REGULAR=1.5` the rounding errors fail to
/// cancel and hovered widgets allocate +1 px per axis vs. inactive — visible
/// as layout jitter on hover-in / hover-out. Pinning hovered to `STRONG`
/// (2.0) keeps the math integer-clean; hover vs. active distinction now
/// rests on the bg_stroke color (ACCENT_DEFAULT vs. ACCENT_STRONG).
pub fn apply_stroke(visuals: &mut Visuals) {
    visuals.window_stroke.width = self::stroke::REGULAR;
    visuals.widgets.noninteractive.bg_stroke.width = self::stroke::HAIR;
    visuals.widgets.inactive.bg_stroke.width = self::stroke::HAIR;
    visuals.widgets.hovered.bg_stroke.width = self::stroke::STRONG;
    visuals.widgets.active.bg_stroke.width = self::stroke::STRONG;
    visuals.widgets.open.bg_stroke.width = self::stroke::REGULAR;
    visuals.selection.stroke.width = self::stroke::STRONG;
}
