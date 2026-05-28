//! `egui::Visuals` binding (ADR-0031 §SD6).
//!
//! Writes IDS color tokens into `egui::Visuals` fields once at startup.
//! No runtime cost beyond the initial overlay; per ADR-0029 §SD13 the
//! generated `Color32` constants are loaded directly.

use egui::style::{Visuals, WidgetVisuals, Widgets};
use egui::{Color32, Stroke};

use super::palette_generated as p;
use super::stroke as s;

/// Apply IDS dark-theme color tokens to `egui::Visuals`. Per ADR-0031 §SD6
/// mapping table — `dark = true` is the only theme in v1.
///
/// Apps that need to deviate (e.g. a custom dialog with a non-standard
/// `panel_fill`) escalate via Tier 3.
pub fn apply_visuals(visuals: &mut Visuals) {
    visuals.dark_mode = true;
    visuals.panel_fill = p::NEUTRAL_BG_PANEL;
    visuals.faint_bg_color = p::NEUTRAL_BG_FAINT;
    visuals.extreme_bg_color = p::NEUTRAL_BG_EXTREME;
    visuals.window_fill = p::NEUTRAL_BG_SURFACE;
    visuals.window_stroke = Stroke::new(s::REGULAR, p::NEUTRAL_BORDER_DEFAULT);
    visuals.menu_corner_radius = visuals.window_corner_radius;
    visuals.override_text_color = Some(p::NEUTRAL_TEXT_PRIMARY);
    visuals.hyperlink_color = p::INFO_DEFAULT;
    visuals.warn_fg_color = p::WARNING_DEFAULT;
    visuals.error_fg_color = p::ERROR_DEFAULT;
    // ADR-0037 follow-on: ACCENT_DEFAULT (L=0.80) gave APCA Lc≈30 against
    // text_primary (L=0.93), invisible on selected SelectableLabels (egui's
    // theme-preference buttons, in particular). ACCENT_SUBTLE (L=0.20)
    // restores readable contrast and matches the dark-theme convention.
    visuals.selection.bg_fill = p::ACCENT_SUBTLE;
    visuals.selection.stroke = Stroke::new(s::STRONG, p::ACCENT_STRONG);

    apply_widgets(&mut visuals.widgets);
}

fn apply_widgets(w: &mut Widgets) {
    w.noninteractive = WidgetVisuals {
        bg_fill: p::NEUTRAL_BG_PANEL,
        weak_bg_fill: p::NEUTRAL_BG_FAINT,
        bg_stroke: Stroke::new(s::HAIR, p::NEUTRAL_BORDER_FAINT),
        fg_stroke: Stroke::new(s::HAIR, p::NEUTRAL_TEXT_SECONDARY),
        corner_radius: w.noninteractive.corner_radius,
        expansion: 0.0,
    };
    w.inactive = WidgetVisuals {
        bg_fill: p::NEUTRAL_BG_SURFACE,
        weak_bg_fill: p::NEUTRAL_BG_FAINT,
        bg_stroke: Stroke::new(s::HAIR, p::NEUTRAL_BORDER_DEFAULT),
        fg_stroke: Stroke::new(s::HAIR, p::NEUTRAL_TEXT_PRIMARY),
        corner_radius: w.inactive.corner_radius,
        expansion: 0.0,
    };
    w.hovered = WidgetVisuals {
        bg_fill: p::NEUTRAL_BG_SURFACE,
        weak_bg_fill: p::NEUTRAL_BG_SURFACE,
        bg_stroke: Stroke::new(s::REGULAR, p::ACCENT_DEFAULT),
        fg_stroke: Stroke::new(s::HAIR, p::NEUTRAL_TEXT_EXTREME),
        corner_radius: w.hovered.corner_radius,
        expansion: 1.0,
    };
    w.active = WidgetVisuals {
        bg_fill: p::ACCENT_SUBTLE,
        weak_bg_fill: p::NEUTRAL_BG_SURFACE,
        bg_stroke: Stroke::new(s::STRONG, p::ACCENT_STRONG),
        fg_stroke: Stroke::new(s::HAIR, p::NEUTRAL_TEXT_EXTREME),
        corner_radius: w.active.corner_radius,
        expansion: 1.0,
    };
    w.open = WidgetVisuals {
        bg_fill: p::NEUTRAL_BG_SURFACE,
        weak_bg_fill: p::NEUTRAL_BG_FAINT,
        bg_stroke: Stroke::new(s::REGULAR, p::NEUTRAL_BORDER_DEFAULT),
        fg_stroke: Stroke::new(s::HAIR, p::NEUTRAL_TEXT_PRIMARY),
        corner_radius: w.open.corner_radius,
        expansion: 0.0,
    };
}

// ---- egui_plot data-encoding accessors (ADR-0031 §SD7) ----

/// Sequential palettes; index into `super::super::data_encoding`.
#[derive(Copy, Clone, Debug, PartialEq, Eq)]
pub enum SequentialE {
    Batlow,
    Lapaz,
    Oslo,
    Lajolla,
    Viridis,
    Magma,
    Plasma,
    Inferno,
}

/// Diverging palettes.
#[derive(Copy, Clone, Debug, PartialEq, Eq)]
pub enum DivergingE {
    Vik,
    Roma,
    Broc,
    Cork,
}

/// Qualitative cycle from `batlowS` (10 colors); `idx % 10`.
#[inline]
pub fn qualitative_cycle(idx: usize) -> Color32 {
    let lut = &super::super::data_encoding::BATLOW_S;
    let n = lut.len();
    let (r, g, b) = lut[idx % n];
    Color32::from_rgb(r, g, b)
}

/// Sequential lookup; `t` is clamped to `[0.0, 1.0]`.
#[inline]
pub fn sequential(palette: SequentialE, t: f32) -> Color32 {
    use super::super::data_encoding as de;
    let lut: &[(u8, u8, u8); 256] = match palette {
        SequentialE::Batlow => &de::BATLOW,
        SequentialE::Lapaz => &de::LAPAZ,
        SequentialE::Oslo => &de::OSLO,
        SequentialE::Lajolla => &de::LAJOLLA,
        SequentialE::Viridis => &de::VIRIDIS,
        SequentialE::Magma => &de::MAGMA,
        SequentialE::Plasma => &de::PLASMA,
        SequentialE::Inferno => &de::INFERNO,
    };
    let t = t.clamp(0.0, 1.0);
    let idx = (t * 255.0) as usize;
    let (r, g, b) = lut[idx];
    Color32::from_rgb(r, g, b)
}

/// Diverging lookup; `t` is in `[-1.0, 1.0]` (sign carries direction;
/// magnitude carries distance from neutral midpoint).
#[inline]
pub fn diverging(palette: DivergingE, t: f32) -> Color32 {
    use super::super::data_encoding as de;
    let lut: &[(u8, u8, u8); 256] = match palette {
        DivergingE::Vik => &de::VIK,
        DivergingE::Roma => &de::ROMA,
        DivergingE::Broc => &de::BROC,
        DivergingE::Cork => &de::CORK,
    };
    // Map [-1, 1] → [0, 255], midpoint at 127/128.
    let t = t.clamp(-1.0, 1.0);
    let mapped = (t * 0.5 + 0.5) * 255.0;
    let idx = mapped as usize;
    let idx = idx.min(255);
    let (r, g, b) = lut[idx];
    Color32::from_rgb(r, g, b)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn qualitative_cycle_wraps() {
        // 10-color batlowS — index 0 and 10 give the same color.
        assert_eq!(qualitative_cycle(0), qualitative_cycle(10));
        assert_eq!(qualitative_cycle(3), qualitative_cycle(13));
    }

    #[test]
    fn sequential_endpoints_match_lut_endpoints() {
        let lo = sequential(SequentialE::Batlow, 0.0);
        let hi = sequential(SequentialE::Batlow, 1.0);
        assert_ne!(lo, hi);
        // Re-querying with same t is deterministic.
        assert_eq!(lo, sequential(SequentialE::Batlow, 0.0));
    }

    #[test]
    fn diverging_midpoint_is_neutral() {
        // For vik (Crameri), the midpoint at t=0 should be near the
        // neutral L≈0.55 grey. We just assert it's not at either extreme.
        let mid = diverging(DivergingE::Vik, 0.0);
        let lo = diverging(DivergingE::Vik, -1.0);
        let hi = diverging(DivergingE::Vik, 1.0);
        assert_ne!(mid, lo);
        assert_ne!(mid, hi);
    }

    #[test]
    fn sequential_clamps_out_of_range_t() {
        let neg = sequential(SequentialE::Viridis, -0.5);
        let lo = sequential(SequentialE::Viridis, 0.0);
        assert_eq!(neg, lo);
        let big = sequential(SequentialE::Viridis, 2.0);
        let hi = sequential(SequentialE::Viridis, 1.0);
        assert_eq!(big, hi);
    }
}
