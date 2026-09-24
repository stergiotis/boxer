//! The IDS slider: egui's, with a rail that can be seen.
//!
//! egui paints a slider's rail as a filled rectangle in
//! `widgets.inactive.bg_fill` and gives it no stroke. The IDS overlay maps
//! that field to `NEUTRAL_BG_SURFACE` (ADR-0031 §SD6), which is what a window
//! is filled with and one step off a panel — so the rail was painted in its
//! own background's colour and a slider read as a lone handle beside a gap.
//! Every other widget that takes that field draws an outline around it, which
//! is why only the slider showed the defect and why the mapping itself stays:
//! changing it would refill every checkbox and radio button at rest.
//!
//! The rail is a mark a reader has to find, so it takes a colour that clears
//! WCAG 1.4.11's 3:1 against both fills. The override lasts for the slider's
//! own `ui` call and is put back, so nothing drawn after it sees it.

use egui::{Response, Slider, Ui, Widget};

use super::tokens::palette_generated as p;

/// Wraps a built [`egui::Slider`]; add it where the slider would be added.
pub struct IdsSlider<'a>(pub Slider<'a>);

impl Widget for IdsSlider<'_> {
    fn ui(self, ui: &mut Ui) -> Response {
        let saved = ui.visuals().widgets.inactive.bg_fill;
        ui.visuals_mut().widgets.inactive.bg_fill = super::slider_rail();
        let response = ui.add(self.0);
        ui.visuals_mut().widgets.inactive.bg_fill = saved;
        response
    }
}

/// The rail's colour under the IDS dark palette; the fresh theme has its
/// own (`fresh::RAIL`) and `style::slider_rail` picks. At rest the handle is
/// filled with it too — egui reads the same field for both — and is told
/// from the rail by its outline.
pub const RAIL: egui::Color32 = p::NEUTRAL_BORDER_DEFAULT;

#[cfg(test)]
mod tests {
    use super::*;

    /// WCAG relative luminance of an opaque colour.
    fn luminance(c: egui::Color32) -> f32 {
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

    fn contrast(a: egui::Color32, b: egui::Color32) -> f32 {
        let (la, lb) = (luminance(a), luminance(b));
        (la.max(lb) + 0.05) / (la.min(lb) + 0.05)
    }

    #[test]
    fn the_rail_can_be_seen_on_a_panel_and_in_a_window() {
        for (name, fill) in [
            ("panel", p::NEUTRAL_BG_PANEL),
            ("window", p::NEUTRAL_BG_SURFACE),
        ] {
            let ratio = contrast(RAIL, fill);
            assert!(ratio >= 3.0, "rail against the {name} fill is {ratio:.2}:1");
        }
    }

    /// The defect this module exists for: the overlay's own value for the
    /// field egui paints the rail with is the window's fill.
    #[test]
    fn the_overlays_inactive_fill_is_not_a_rail_colour() {
        let mut visuals = egui::Visuals::dark();
        super::super::tokens::apply_visuals(&mut visuals);
        assert!(contrast(visuals.widgets.inactive.bg_fill, visuals.window_fill) < 1.1);
    }

    #[test]
    fn the_override_does_not_outlive_the_slider() {
        let ctx = egui::Context::default();
        super::super::apply_style_only(&ctx, super::super::tokens::Density::Standard);
        let mut value = 0.4_f32;
        let _ = ctx.run_ui(egui::RawInput::default(), |ui| {
            let before = ui.visuals().widgets.inactive.bg_fill;
            ui.add(IdsSlider(Slider::new(&mut value, 0.0..=1.0)));
            assert_eq!(before, ui.visuals().widgets.inactive.bg_fill);
            assert_ne!(before, RAIL);
        });
    }
}
