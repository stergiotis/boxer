//! The IDS button kinds: egui's button, coloured by what it is for.
//!
//! A kind names the button's role — the one primary action of a view, a
//! destructive one, a quiet one — and resolves it to semantic palette tokens
//! (ADR-0273). egui's button reads its fill, stroke and text colour from
//! `widgets.{inactive,hovered,active}` for the state it is in, so the
//! wrapper writes the kind's colours into those three slots for the button's
//! own `ui` call and puts them back, the way [`super::slider::IdsSlider`]
//! does for the rail. Stroke widths, corner radii and the hover expansion
//! stay the theme's; a kind changes colours and, for the two ghost kinds,
//! whether a frame is drawn at rest.
//!
//! The colours are a table per theme rather than one rule over the role
//! tokens, because the themes disagree about which emphasis clears 4.5:1
//! under text on a fill: light text on `accent.default` does on the dark
//! spine and does not on `fresh`, which therefore rests a primary button on
//! `accent.strong`. The tests hold every kind, state and theme to the
//! contrast it needs.

use std::sync::atomic::{AtomicBool, Ordering};

use egui::style::WidgetVisuals;
use egui::{Button, Color32, Response, Ui, Widget};

use super::tokens::palette_fresh_generated as f;
use super::tokens::palette_generated as p;
use super::tokens::{self, Theme};

/// Button kinds. The discriminants are the wire values of the `kind` method
/// on the `button` IDL node and match `ButtonKindE` in the Go bindings.
pub const KIND_SECONDARY: u8 = 0;
pub const KIND_PRIMARY: u8 = 1;
pub const KIND_TERTIARY: u8 = 2;
pub const KIND_GHOST: u8 = 3;
pub const KIND_DANGER: u8 = 4;
pub const KIND_DANGER_GHOST: u8 = 5;

/// Set by [`super::apply_tour_neutral_overrides`]: a screenshot tour wants
/// hover and press strokes to look like rest, and a kind must not undo that.
static TOUR_NEUTRAL: AtomicBool = AtomicBool::new(false);

pub(super) fn set_tour_neutral() {
    TOUR_NEUTRAL.store(true, Ordering::Relaxed);
}

/// Wraps a built [`egui::Button`] with its kind; add it where the button
/// would be added. [`KIND_SECONDARY`] and unknown kinds add the button as is.
pub struct IdsButton<'a>(pub Button<'a>, pub u8);

impl Widget for IdsButton<'_> {
    fn ui(self, ui: &mut Ui) -> Response {
        let IdsButton(button, kind) = self;
        let Some(states) = states(tokens::theme::active(), kind) else {
            return ui.add(button);
        };
        let button = if matches!(kind, KIND_GHOST | KIND_DANGER_GHOST) {
            button.frame_when_inactive(false)
        } else {
            button
        };
        let visuals = ui.visuals_mut();
        let saved = (
            visuals.widgets.inactive,
            visuals.widgets.hovered,
            visuals.widgets.active,
            visuals.override_text_color,
        );
        // The overlay sets an override text colour, which egui prefers to
        // the per-state foreground; the kind's text colour needs it gone.
        visuals.override_text_color = None;
        paint(&mut visuals.widgets.inactive, states[0]);
        paint(&mut visuals.widgets.hovered, states[1]);
        paint(&mut visuals.widgets.active, states[2]);
        if TOUR_NEUTRAL.load(Ordering::Relaxed) {
            let rest = visuals.widgets.inactive.bg_stroke;
            visuals.widgets.hovered.bg_stroke = rest;
            visuals.widgets.active.bg_stroke = rest;
        }
        let response = ui.add(button);
        let visuals = ui.visuals_mut();
        visuals.widgets.inactive = saved.0;
        visuals.widgets.hovered = saved.1;
        visuals.widgets.active = saved.2;
        visuals.override_text_color = saved.3;
        response
    }
}

/// One state's colours. `None` leaves that slot as the theme has it.
#[derive(Clone, Copy, Debug, PartialEq)]
struct Paint {
    fill: Option<Color32>,
    stroke: Option<Color32>,
    text: Option<Color32>,
}

const fn all(fill: Color32, stroke: Color32, text: Color32) -> Paint {
    Paint {
        fill: Some(fill),
        stroke: Some(stroke),
        text: Some(text),
    }
}

/// A slot the kind leaves to the theme.
const THEME: Paint = Paint {
    fill: None,
    stroke: None,
    text: None,
};

fn paint(v: &mut WidgetVisuals, pt: Paint) {
    if let Some(c) = pt.fill {
        v.weak_bg_fill = c;
    }
    if let Some(c) = pt.stroke {
        v.bg_stroke.color = c;
    }
    if let Some(c) = pt.text {
        v.fg_stroke.color = c;
    }
}

/// Inactive, hovered and active colours of a kind under a theme; `None` for
/// the kinds that take the theme's button unchanged.
fn states(theme: Theme, kind: u8) -> Option<[Paint; 3]> {
    match theme {
        Theme::Dark => dark(kind),
        Theme::Fresh => fresh(kind),
    }
}

const CLEAR: Color32 = Color32::TRANSPARENT;

/// The IDS dark spine: role fills are light, so text on them is
/// `bg.extreme` and hover moves one emphasis lighter.
fn dark(kind: u8) -> Option<[Paint; 3]> {
    let ring = p::NEUTRAL_TEXT_EXTREME;
    Some(match kind {
        KIND_PRIMARY => [
            all(p::ACCENT_DEFAULT, p::ACCENT_DEFAULT, p::NEUTRAL_BG_EXTREME),
            all(p::ACCENT_STRONG, p::ACCENT_STRONG, p::NEUTRAL_BG_EXTREME),
            all(p::ACCENT_STRONG, ring, p::NEUTRAL_BG_EXTREME),
        ],
        KIND_DANGER => [
            all(p::ERROR_DEFAULT, p::ERROR_DEFAULT, p::NEUTRAL_BG_EXTREME),
            all(p::ERROR_STRONG, p::ERROR_STRONG, p::NEUTRAL_BG_EXTREME),
            all(p::ERROR_STRONG, ring, p::NEUTRAL_BG_EXTREME),
        ],
        KIND_TERTIARY => [
            all(CLEAR, p::ACCENT_DEFAULT, p::ACCENT_STRONG),
            all(p::ACCENT_SUBTLE, p::ACCENT_STRONG, p::ACCENT_STRONG),
            all(p::ACCENT_SUBTLE, ring, p::ACCENT_STRONG),
        ],
        // Hover without the theme's accent ring: the stroke takes the
        // theme's hover fill, so only the fill says "hovered".
        KIND_GHOST => [
            THEME,
            Paint {
                stroke: Some(p::NEUTRAL_BG_SURFACE),
                ..THEME
            },
            THEME,
        ],
        KIND_DANGER_GHOST => [
            all(CLEAR, CLEAR, p::ERROR_STRONG),
            all(p::ERROR_SUBTLE, p::ERROR_SUBTLE, p::ERROR_STRONG),
            all(p::ERROR_SUBTLE, ring, p::ERROR_STRONG),
        ],
        _ => return None,
    })
}

/// The `fresh` light theme (ADR-0258): role fills are dark, so text on
/// them is `bg.extreme` (near white) and the fill must be the emphasis
/// that clears 4.5:1 under it — `strong` for the accent, whose `default`
/// does not.
fn fresh(kind: u8) -> Option<[Paint; 3]> {
    let ring = f::NEUTRAL_TEXT_EXTREME;
    Some(match kind {
        KIND_PRIMARY => [
            all(f::ACCENT_STRONG, f::ACCENT_STRONG, f::NEUTRAL_BG_EXTREME),
            all(f::ACCENT_STRONG, f::ACCENT_DEFAULT, f::NEUTRAL_BG_EXTREME),
            all(f::ACCENT_STRONG, ring, f::NEUTRAL_BG_EXTREME),
        ],
        KIND_DANGER => [
            all(f::ERROR_DEFAULT, f::ERROR_DEFAULT, f::NEUTRAL_BG_EXTREME),
            all(f::ERROR_STRONG, f::ERROR_STRONG, f::NEUTRAL_BG_EXTREME),
            all(f::ERROR_STRONG, ring, f::NEUTRAL_BG_EXTREME),
        ],
        KIND_TERTIARY => [
            all(CLEAR, f::ACCENT_DEFAULT, f::ACCENT_STRONG),
            all(f::ACCENT_SUBTLE, f::ACCENT_STRONG, f::ACCENT_STRONG),
            all(f::ACCENT_SUBTLE, ring, f::ACCENT_STRONG),
        ],
        KIND_GHOST => [
            THEME,
            Paint {
                stroke: Some(f::ACCENT_SUBTLE),
                ..THEME
            },
            THEME,
        ],
        KIND_DANGER_GHOST => [
            all(CLEAR, CLEAR, f::ERROR_STRONG),
            all(f::ERROR_SUBTLE, f::ERROR_SUBTLE, f::ERROR_STRONG),
            all(f::ERROR_SUBTLE, ring, f::ERROR_STRONG),
        ],
        _ => return None,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

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

    const KINDS: [u8; 5] = [
        KIND_PRIMARY,
        KIND_TERTIARY,
        KIND_GHOST,
        KIND_DANGER,
        KIND_DANGER_GHOST,
    ];

    /// The surfaces a button is placed on, per theme.
    fn grounds(theme: Theme) -> [Color32; 2] {
        match theme {
            Theme::Dark => [p::NEUTRAL_BG_PANEL, p::NEUTRAL_BG_SURFACE],
            Theme::Fresh => [f::NEUTRAL_BG_PANEL, f::NEUTRAL_BG_SURFACE],
        }
    }

    /// Body text needs 4.5:1 (WCAG 1.4.3) against what is behind it: the
    /// kind's fill, or the ground when the fill is clear.
    #[test]
    fn every_label_is_readable_in_every_state() {
        for theme in [Theme::Dark, Theme::Fresh] {
            for kind in KINDS {
                for (si, st) in states(theme, kind).unwrap().iter().enumerate() {
                    let Some(text) = st.text else { continue };
                    let behind: Vec<Color32> = match st.fill {
                        Some(fill) if fill != CLEAR => vec![fill],
                        _ => grounds(theme).to_vec(),
                    };
                    for b in behind {
                        let r = contrast(text, b);
                        assert!(r >= 4.5, "{theme:?} kind {kind} state {si}: {r:.2}:1");
                    }
                }
            }
        }
    }

    /// A filled kind must be told from its ground (WCAG 1.4.11, 3:1) — by
    /// its fill, since its rest stroke is the fill's own colour.
    #[test]
    fn filled_kinds_stand_out_from_the_ground() {
        for theme in [Theme::Dark, Theme::Fresh] {
            for kind in [KIND_PRIMARY, KIND_DANGER] {
                let fill = states(theme, kind).unwrap()[0].fill.unwrap();
                for g in grounds(theme) {
                    let r = contrast(fill, g);
                    assert!(r >= 3.0, "{theme:?} kind {kind}: {r:.2}:1");
                }
            }
        }
    }

    /// An outline is the tertiary button's only boundary at rest.
    #[test]
    fn the_tertiary_outline_can_be_seen() {
        for theme in [Theme::Dark, Theme::Fresh] {
            let stroke = states(theme, KIND_TERTIARY).unwrap()[0].stroke.unwrap();
            for g in grounds(theme) {
                let r = contrast(stroke, g);
                assert!(r >= 3.0, "{theme:?}: {r:.2}:1");
            }
        }
    }

    #[test]
    fn secondary_and_unknown_kinds_are_the_theme_button() {
        for theme in [Theme::Dark, Theme::Fresh] {
            assert!(states(theme, KIND_SECONDARY).is_none());
            assert!(states(theme, 200).is_none());
        }
    }

    #[test]
    fn the_override_does_not_outlive_the_button() {
        let ctx = egui::Context::default();
        super::super::apply_style_only(&ctx, tokens::density::Density::Standard);
        let _ = ctx.run_ui(egui::RawInput::default(), |ui| {
            let before = ui.visuals().clone();
            ui.add(IdsButton(Button::new("x"), KIND_PRIMARY));
            ui.add(IdsButton(Button::new("y"), KIND_DANGER_GHOST));
            assert_eq!(before, *ui.visuals());
        });
    }
}
