//! The IDS progress bar's fill and label under the dark palette.
//!
//! egui paints a progress bar's label in one colour across the filled part
//! and the empty rail (`extreme_bg_color`), and the rail is near-black. On
//! the light `ACCENT_DEFAULT` fill no colour reads on both, so the bar takes
//! a darker accent that a near-white label clears 4.5:1 on, and that still
//! stands off the rail and a window by WCAG 1.4.11's 3:1. The fresh theme
//! has its own pair (`fresh::progress_fill`, `fresh::PROGRESS_LABEL`);
//! `style::progress_fill` and `style::with_progress_bar_label` pick.

use egui::Color32;

use super::tokens::palette_generated as p;

/// The fill: a little over half way from the accent tint to the accent.
pub fn fill() -> Color32 {
    p::ACCENT_SUBTLE.lerp_to_gamma(p::ACCENT_DEFAULT, 0.55)
}

/// The label, on the fill and on the empty rail alike.
pub const LABEL: Color32 = p::NEUTRAL_TEXT_EXTREME;

#[cfg(test)]
mod tests {
    use super::*;

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
    fn the_label_reads_on_the_fill_and_the_rail() {
        // The rail is `extreme_bg_color`, which the IDS maps to BG_EXTREME.
        assert!(contrast(LABEL, fill()) >= 4.5);
        assert!(contrast(LABEL, p::NEUTRAL_BG_EXTREME) >= 4.5);
    }

    #[test]
    fn the_fill_stands_off_the_rail_a_panel_and_a_window() {
        for bg in [
            p::NEUTRAL_BG_EXTREME,
            p::NEUTRAL_BG_PANEL,
            p::NEUTRAL_BG_SURFACE,
        ] {
            assert!(contrast(fill(), bg) >= 3.0, "fill on {bg:?}");
        }
    }
}
