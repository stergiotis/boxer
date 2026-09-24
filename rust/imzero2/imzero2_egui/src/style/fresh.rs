//! The `fresh` theme (ADR-0258): light, friendly, a little playful.
//!
//! The alternative to the IDS dark overlay, chosen at launch with
//! `IMZERO2_THEME=fresh` (`tokens::theme`). Everything the IDS shares across
//! themes — spacing ladder, type scale, fonts, data-encoding palettes — is
//! untouched; this module only re-decides colour, rounding, stroke and
//! shadow. Its palette is `palette_fresh_generated`, emitted from
//! `assets/colors/palette-fresh.toml` by the same generator and graded by
//! the same APCA gate as the IDS palette.
//!
//! What it does differently from the IDS overlay, in one place:
//!
//! - **Light spine with a cool tint.** Panels are lavender-white paper,
//!   windows and cards a near-white step up, text a blue-black ink of the
//!   same hue rather than grey — the spine is one hue at ten lightnesses.
//! - **An orchid accent** (h = 315), far from all four status hues, so a
//!   hover ring or a selection never reads as info / success / warning /
//!   error.
//! - **Rounder.** Widgets 8 px, windows and menus 12 px.
//! - **A hard offset shadow** on windows and popups — no blur — which is
//!   the one thing meant to be recognisable from across the room.
//! - **Sliders fill their trailing rail** in the accent tint.
//!
//! One egui coupling shapes the widget table: `Visuals::strong_text_color`
//! *is* `widgets.active.fg_stroke.color`, so a pressed control's text
//! colour is also every `RichText::strong` heading. It stays ink here, and
//! the pressed state is told by its fill and ring instead.

use egui::style::{Selection, Style, Visuals, WidgetVisuals, Widgets};
use egui::{Color32, CornerRadius, Shadow, Stroke};

use super::tokens::density::Density;
use super::tokens::palette_fresh_generated as p;
use super::tokens::stroke as s;

// ---- Rounding (px) ----

/// Buttons, text fields, checkboxes, chips.
pub const ROUND_WIDGET: u8 = 8;
/// Floating windows, menus, popups.
pub const ROUND_WINDOW: u8 = 12;

/// The hard shadow's offset, in px, right and down.
pub const SHADOW_OFFSET: i8 = 4;

/// Ink at a quarter strength: the shadow colour.
fn shadow_color() -> Color32 {
    let c = p::NEUTRAL_TEXT_PRIMARY;
    Color32::from_rgba_unmultiplied(c.r(), c.g(), c.b(), 56)
}

/// A pressed control's fill: a third of the way from the accent tint to
/// the accent. Ink text keeps its contrast on it.
fn pressed_fill() -> Color32 {
    p::ACCENT_SUBTLE.lerp_to_gamma(p::ACCENT_DEFAULT, 0.35)
}

/// The slider rail at rest — see `style::slider` and `style::slider_rail`.
/// Clears WCAG 1.4.11's 3:1 against both the panel and the surface fill.
pub const RAIL: Color32 = p::NEUTRAL_BORDER_DEFAULT;

/// Full colour + rounding + stroke + shadow overlay for the fresh theme.
/// Replaces the IDS `apply_visuals` / `apply_rounding` / `apply_stroke`
/// trio; spacing and typography are applied by the caller as usual.
pub fn apply(style: &mut Style, density: Density) {
    // Start from egui's light visuals so every field this overlay does not
    // name (text cursor, disabled alpha, striping, …) is already tuned for
    // a light background.
    let mut v = Visuals::light();
    v.dark_mode = false;

    v.panel_fill = p::NEUTRAL_BG_PANEL;
    v.window_fill = p::NEUTRAL_BG_SURFACE;
    v.faint_bg_color = p::NEUTRAL_BG_FAINT;
    v.extreme_bg_color = p::NEUTRAL_BG_EXTREME;
    v.text_edit_bg_color = Some(p::NEUTRAL_BG_EXTREME);
    v.code_bg_color = p::NEUTRAL_BG_FAINT;

    // Text: no global override, so a pressed button can switch to light
    // text on its accent fill. Labels read `widgets.noninteractive.fg_stroke`.
    v.override_text_color = None;
    v.weak_text_color = Some(p::NEUTRAL_TEXT_SECONDARY);
    v.hyperlink_color = p::INFO_STRONG;
    v.warn_fg_color = p::WARNING_STRONG;
    v.error_fg_color = p::ERROR_STRONG;

    v.selection = Selection {
        bg_fill: p::ACCENT_SUBTLE,
        stroke: Stroke::new(s::STRONG, p::ACCENT_STRONG),
    };

    v.window_stroke = Stroke::new(s::REGULAR, p::NEUTRAL_BORDER_DEFAULT);
    v.window_corner_radius = CornerRadius::same(ROUND_WINDOW);
    v.menu_corner_radius = CornerRadius::same(ROUND_WINDOW);
    v.window_shadow = Shadow {
        offset: [SHADOW_OFFSET, SHADOW_OFFSET],
        blur: 0,
        spread: 0,
        color: shadow_color(),
    };
    v.popup_shadow = Shadow {
        offset: [SHADOW_OFFSET - 1, SHADOW_OFFSET - 1],
        blur: 0,
        spread: 0,
        color: shadow_color(),
    };
    v.slider_trailing_fill = true;

    apply_widgets(&mut v.widgets);
    style.visuals = v;

    // A touch more air inside buttons than the IDS ladder gives, still on
    // the 2 px grid at every density. Everything else in `Spacing` is the
    // shared ladder, which the caller applied for `density` already.
    let _ = density;
    style.spacing.button_padding += egui::vec2(2.0, 2.0);
}

fn apply_widgets(w: &mut Widgets) {
    let round = CornerRadius::same(ROUND_WIDGET);
    w.noninteractive = WidgetVisuals {
        bg_fill: p::NEUTRAL_BG_PANEL,
        weak_bg_fill: p::NEUTRAL_BG_FAINT,
        bg_stroke: Stroke::new(s::HAIR, p::NEUTRAL_BORDER_FAINT),
        // Body text colour, since override_text_color is None.
        fg_stroke: Stroke::new(s::HAIR, p::NEUTRAL_TEXT_PRIMARY),
        corner_radius: round,
        expansion: 0.0,
    };
    w.inactive = WidgetVisuals {
        bg_fill: p::NEUTRAL_BG_SURFACE,
        weak_bg_fill: p::NEUTRAL_BG_FAINT,
        bg_stroke: Stroke::new(s::HAIR, p::NEUTRAL_BORDER_DEFAULT),
        fg_stroke: Stroke::new(s::HAIR, p::NEUTRAL_TEXT_PRIMARY),
        corner_radius: round,
        expansion: 0.0,
    };
    w.hovered = WidgetVisuals {
        bg_fill: p::ACCENT_SUBTLE,
        weak_bg_fill: p::ACCENT_SUBTLE,
        bg_stroke: Stroke::new(s::STRONG, p::ACCENT_DEFAULT),
        fg_stroke: Stroke::new(s::HAIR, p::NEUTRAL_TEXT_EXTREME),
        corner_radius: round,
        expansion: 1.0,
    };
    // egui reads `active.fg_stroke.color` as `strong_text_color()` — every
    // `RichText::strong()` heading, not just a pressed button's label — so
    // it must stay ink (module doc). The pressed fill is therefore a deeper
    // accent tint that ink still reads on, not the accent itself.
    w.active = WidgetVisuals {
        bg_fill: pressed_fill(),
        weak_bg_fill: p::ACCENT_SUBTLE,
        bg_stroke: Stroke::new(s::STRONG, p::ACCENT_STRONG),
        fg_stroke: Stroke::new(s::HAIR, p::NEUTRAL_TEXT_EXTREME),
        corner_radius: round,
        expansion: 1.0,
    };
    w.open = WidgetVisuals {
        bg_fill: p::NEUTRAL_BG_FAINT,
        weak_bg_fill: p::NEUTRAL_BG_FAINT,
        bg_stroke: Stroke::new(s::REGULAR, p::NEUTRAL_BORDER_DEFAULT),
        fg_stroke: Stroke::new(s::HAIR, p::NEUTRAL_TEXT_PRIMARY),
        corner_radius: round,
        expansion: 0.0,
    };
}

#[cfg(test)]
mod tests {
    use super::*;
    use egui::style::Style;

    fn applied() -> Visuals {
        let mut style = Style::default();
        apply(&mut style, Density::Standard);
        style.visuals
    }

    /// WCAG relative luminance of an opaque colour.
    fn luminance(c: Color32) -> f32 {
        let lin = |v: u8| {
            let s = f32::from(v) / 255.0;
            if s <= 0.04045 {
                s / 12.92
            } else {
                ((s + 0.055) / 1.055).powf(2.4)
            }
        };
        0.2126 * lin(c.r()) + 0.7152 * lin(c.g()) + 0.0722 * lin(c.b())
    }

    fn contrast(a: Color32, b: Color32) -> f32 {
        let (la, lb) = (luminance(a), luminance(b));
        (la.max(lb) + 0.05) / (la.min(lb) + 0.05)
    }

    #[test]
    fn strong_text_is_ink_not_the_pressed_fill_colour() {
        // The coupling the module doc names: strong text reads the active
        // fg stroke, so it must stay dark on this light theme.
        let v = applied();
        assert_eq!(v.strong_text_color(), p::NEUTRAL_TEXT_EXTREME);
        assert!(contrast(v.strong_text_color(), v.panel_fill) >= 7.0);
    }

    #[test]
    fn body_text_reads_on_every_surface() {
        let v = applied();
        let text = v.text_color();
        for bg in [
            v.panel_fill,
            v.window_fill,
            v.faint_bg_color,
            v.extreme_bg_color,
        ] {
            assert!(
                contrast(text, bg) >= 7.0,
                "text on {bg:?}: {}",
                contrast(text, bg)
            );
        }
    }

    #[test]
    fn pressed_fill_still_carries_ink() {
        let v = applied();
        assert!(contrast(v.widgets.active.fg_stroke.color, v.widgets.active.bg_fill) >= 4.5);
        assert!(contrast(v.widgets.hovered.fg_stroke.color, v.widgets.hovered.bg_fill) >= 4.5);
    }

    #[test]
    fn the_rail_can_be_seen_on_a_panel_and_in_a_window() {
        assert!(contrast(RAIL, p::NEUTRAL_BG_PANEL) >= 3.0);
        assert!(contrast(RAIL, p::NEUTRAL_BG_SURFACE) >= 3.0);
    }

    #[test]
    fn hover_and_active_strokes_stay_integer_wide() {
        // `tokens::apply_stroke` documents why: a fractional hovered stroke
        // with expansion 1.0 makes egui allocate +1 px on hover.
        let v = applied();
        assert_eq!(v.widgets.hovered.bg_stroke.width.fract(), 0.0);
        assert_eq!(v.widgets.active.bg_stroke.width.fract(), 0.0);
        assert_eq!(v.widgets.hovered.expansion, 1.0);
    }

    #[test]
    fn it_is_a_light_theme_with_a_hard_shadow() {
        let v = applied();
        assert!(!v.dark_mode);
        assert!(luminance(v.panel_fill) > 0.8);
        assert_eq!(v.window_shadow.blur, 0);
        assert_eq!(v.window_shadow.offset, [SHADOW_OFFSET, SHADOW_OFFSET]);
        assert_eq!(v.window_corner_radius, CornerRadius::same(ROUND_WINDOW));
    }
}
