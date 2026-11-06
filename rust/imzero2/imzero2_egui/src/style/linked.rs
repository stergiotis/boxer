//! A label whose text carries links: one galley for the whole paragraph.
//!
//! A paragraph with a link used to be a row of widgets — a label, a
//! hyperlink, a label — in a wrapping row, and a hyperlink is one widget:
//! when it did not fit the rest of a row it moved whole to the next, and
//! the row before it ended short. Here the link's words are sections of
//! the paragraph's own layout job, so egui's shaper breaks the paragraph
//! as a paragraph, inside a link as readily as outside one. The label
//! senses clicks; a click or a hover is matched against the link
//! sections' glyph boxes, row by row, and reported as the link's index.
//! The host opens nothing itself — the caller decides what a click means.
//!
//! Inline backgrounds are painted as pills as `PillLabel` paints them; a
//! hovered link is underlined in its own colour and the pointer is a hand.

use std::sync::Arc;

use egui::emath::GuiRounding;
use egui::text::LayoutJob;
use egui::{
    Color32, CursorIcon, Label, Pos2, Rect, Response, Sense, Shape, Stroke, TextWrapMode, Ui, pos2,
};

use super::pill::{Pill, section_runs, take_pills};
use super::tokens;

pub struct LinkedLabel {
    label: Label,
    selectable: Option<bool>,
    /// One entry per job section; `None` where the section has no background.
    pills: Vec<Option<Pill>>,
    /// One entry per job section: the index of the link the section is
    /// part of, or `None` for prose.
    link_of_section: Vec<Option<usize>>,
}

/// What showing a [`LinkedLabel`] reports.
pub struct LinkedOut {
    pub response: Response,
    /// The link clicked this frame, if any.
    pub clicked: Option<usize>,
    /// Every row-run of every link: the link's index and the run's logical
    /// box — the line's full height, which is what a pointer over the
    /// line hits and what an exporter wrapping that row's text wants.
    pub zones: Vec<(usize, Rect)>,
}

impl LinkedLabel {
    /// `link_of_section[i]` names the link that section `i` of `job`
    /// belongs to; a job with no link section is a plain pill label with a
    /// click sense.
    pub fn new(mut job: LayoutJob, link_of_section: Vec<Option<usize>>) -> Self {
        let pills = take_pills(&mut job);
        Self {
            label: Label::new(job).sense(Sense::click()),
            selectable: None,
            pills,
            link_of_section,
        }
    }

    /// Mirrors `Label::selectable`.
    pub fn selectable(mut self, selectable: bool) -> Self {
        self.selectable = Some(selectable);
        self.label = self.label.selectable(selectable);
        self
    }

    /// Mirrors `Label::wrap_mode`.
    pub fn wrap_mode(mut self, mode: TextWrapMode) -> Self {
        self.label = self.label.wrap_mode(mode);
        self
    }

    pub fn show(self, ui: &mut Ui) -> LinkedOut {
        let Self {
            label,
            selectable,
            pills,
            link_of_section,
        } = self;
        let selectable = selectable.unwrap_or_else(|| ui.style().interaction.selectable_labels);
        // Reserve a slot below the text for the pills and underlines: shapes
        // added after the label would paint over its glyphs.
        let bg_idx = ui.painter().add(Shape::Noop);
        let (galley_pos, galley, response) = label.layout_in_ui(ui);
        response.widget_info(|| {
            egui::WidgetInfo::labeled(egui::WidgetType::Label, ui.is_enabled(), galley.text())
        });
        let mut clicked = None;
        let mut zones = Vec::new();
        if ui.is_rect_visible(response.rect) {
            let radius = egui::CornerRadius::same(tokens::rounding::SM as u8);
            let ppp = ui.pixels_per_point();
            let mut shapes = Vec::new();
            // (link, section, logical box, font box, baseline) per link run
            let mut links: Vec<(usize, usize, Rect, Rect, f32)> = Vec::new();
            for run in section_runs(&galley, galley_pos) {
                if let Some(pill) = pills.get(run.section).copied().flatten() {
                    let rect = run.rect.expand2(pill.pad).round_to_pixels(ppp);
                    shapes.push(Shape::rect_filled(rect, radius, pill.color));
                }
                if let Some(k) = link_of_section.get(run.section).copied().flatten() {
                    links.push((k, run.section, run.logical, run.rect, run.baseline));
                }
            }
            let hit = |p: Pos2| links.iter().find(|l| l.2.contains(p)).map(|l| l.0);
            let hovered = response.hover_pos().and_then(hit);
            if response.clicked() {
                clicked = response.interact_pointer_pos().and_then(hit);
            }
            if let Some(k) = hovered {
                ui.output_mut(|o| o.cursor_icon = CursorIcon::PointingHand);
                for &(link, section, _, rect, baseline) in &links {
                    if link != k {
                        continue;
                    }
                    let color = galley
                        .job
                        .sections
                        .get(section)
                        .map(|s| s.format.color)
                        .unwrap_or(Color32::PLACEHOLDER);
                    let y = (baseline + 2.0).round_to_pixels(ppp);
                    shapes.push(Shape::line_segment(
                        [pos2(rect.left(), y), pos2(rect.right(), y)],
                        Stroke::new(1.0, color),
                    ));
                }
            }
            // Half a point around the box, so an exporter's own reading of
            // the same glyphs lands inside it.
            zones = links.iter().map(|l| (l.0, l.2.expand(0.5))).collect();
            ui.painter().set(bg_idx, Shape::Vec(shapes));

            let color = ui.style().visuals.text_color();
            let underline = if response.has_focus() || response.highlighted() {
                Stroke::new(1.0, color)
            } else {
                Stroke::NONE
            };
            if selectable {
                egui::text_selection::LabelSelectionState::label_text_selection(
                    ui, &response, galley_pos, galley, color, underline,
                );
            } else {
                ui.painter().add(
                    egui::epaint::TextShape::new(galley_pos, Arc::clone(&galley), color)
                        .with_underline(underline),
                );
            }
        }
        LinkedOut {
            response,
            clicked,
            zones,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use egui::TextFormat;

    #[test]
    fn link_sections_and_pills_are_kept_apart() {
        let mut job = LayoutJob::default();
        job.append("see ", 0.0, TextFormat::default());
        job.append(
            "the docs",
            0.0,
            TextFormat {
                color: Color32::LIGHT_BLUE,
                ..Default::default()
            },
        );
        job.append(
            " and code",
            0.0,
            TextFormat {
                background: Color32::DARK_GRAY,
                ..Default::default()
            },
        );
        let label = LinkedLabel::new(job, vec![None, Some(0), None]);
        assert_eq!(label.link_of_section, vec![None, Some(0), None]);
        assert_eq!(label.pills.len(), 3);
        assert!(label.pills[2].is_some(), "the code section is a pill");
        assert!(label.pills[1].is_none(), "the link section is not");
    }

    fn two_links() -> LinkedLabel {
        let mut job = LayoutJob::default();
        let link = TextFormat {
            color: Color32::LIGHT_BLUE,
            ..Default::default()
        };
        job.append("see ", 0.0, TextFormat::default());
        job.append("the docs", 0.0, link.clone());
        job.append(" or ", 0.0, TextFormat::default());
        job.append("the source", 0.0, link);
        job.append(" for details", 0.0, TextFormat::default());
        LinkedLabel::new(job, vec![None, Some(0), None, Some(1), None])
    }

    fn frame(ctx: &egui::Context, input: egui::RawInput) -> LinkedOut {
        let mut out = None;
        let output = ctx.run_ui(input, |ui| {
            out = Some(two_links().show(ui));
        });
        output.drop_without_applying_deltas();
        out.expect("the root ui showed the label")
    }

    #[test]
    fn a_click_on_a_link_reports_its_index_and_a_click_beside_it_nothing() {
        let ctx = egui::Context::default();
        let first = frame(&ctx, egui::RawInput::default());
        assert_eq!(first.clicked, None);
        let zones = first.zones;
        assert_eq!(zones.len(), 2, "one row, one run per link: {zones:?}");
        assert_eq!(zones[0].0, 0);
        assert_eq!(zones[1].0, 1);
        assert!(
            zones[0].1.right() < zones[1].1.left(),
            "links in text order"
        );

        let click = |ctx: &egui::Context, pos: Pos2| -> Option<usize> {
            let press = |pressed| egui::Event::PointerButton {
                pos,
                button: egui::PointerButton::Primary,
                pressed,
                modifiers: Default::default(),
            };
            let mut input = egui::RawInput::default();
            input.events.push(egui::Event::PointerMoved(pos));
            frame(ctx, input);
            let mut input = egui::RawInput::default();
            input.events.push(press(true));
            frame(ctx, input);
            let mut input = egui::RawInput::default();
            input.events.push(press(false));
            frame(ctx, input).clicked
        };
        assert_eq!(click(&ctx, zones[1].1.center()), Some(1));
        assert_eq!(click(&ctx, zones[0].1.center()), Some(0));
        let beside = pos2(zones[1].1.right() + 40.0, zones[1].1.center().y);
        assert_eq!(
            click(&ctx, beside),
            None,
            "prose after the last link is not a link"
        );
    }
}
