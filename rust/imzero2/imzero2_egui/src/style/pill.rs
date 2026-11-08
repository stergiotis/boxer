//! A label whose inline text backgrounds are pills.
//!
//! epaint paints a section's `TextFormat::background` as a flat rectangle
//! per run of glyphs (`add_row_backgrounds`), with no corner radius, so an
//! inline code span, a highlight or a tag reads as a hard box beside the
//! rounded chips the design system draws everywhere else. `PillLabel` takes
//! the backgrounds out of the job before layout and paints them itself,
//! behind the galley, as rounded rectangles at the inline-chip radius
//! (`tokens::rounding::SM`). Everything else — wrapping, selection, the
//! accessibility label — is egui's `Label`, which does the layout.
//!
//! The pill is the glyph box, not the row: epaint's `Glyph::logical_rect`
//! is as tall as the row's line height, so under a prose leading a
//! background would reach from one line's cap height almost to the next
//! line's. The pill spans the font's ascent and height and is padded like
//! the IDS stylesheet's inline code span (`0.12em 0.35em`); the side
//! padding takes room in the line through the section's `leading_space`,
//! and the next section's, so the glyphs after a pill start past its edge.
//!
//! A job with no background section is the plain `Label`, at no cost.

use std::sync::Arc;

use egui::emath::GuiRounding as _;
use egui::text::LayoutJob;
use egui::{Color32, Label, Rect, Response, Shape, Stroke, TextWrapMode, Ui, Vec2, Widget, vec2};

use super::tokens;

/// One background section: its colour and the padding around the glyph
/// box, in points.
#[derive(Clone, Copy)]
pub(crate) struct Pill {
    pub(crate) color: Color32,
    pub(crate) pad: Vec2,
}

/// One row's run of glyphs from one section: the section, the font box of
/// the run (ascent to row height, not the line's), the run's logical box
/// (the line's full height, as epaint lays it out), and the baseline.
pub(crate) struct SectionRun {
    pub(crate) section: usize,
    pub(crate) rect: Rect,
    pub(crate) logical: Rect,
    pub(crate) baseline: f32,
}

/// The runs of a laid-out galley, row by row, one per contiguous section.
/// A glyph's section is private in epaint, so glyphs are walked against
/// the job's byte ranges: the sections are contiguous and in text order,
/// and a row that ended with a newline consumed one byte the row's glyphs
/// do not carry. An elided galley's trailing "…" lies past the text and
/// belongs to no run.
pub(crate) fn section_runs(galley: &egui::Galley, galley_pos: egui::Pos2) -> Vec<SectionRun> {
    let sections = &galley.job.sections;
    let mut runs = Vec::new();
    let mut si = 0usize;
    let mut byte = 0usize;
    for placed in &galley.rows {
        let origin = galley_pos + placed.pos.to_vec2();
        let mut run: Option<SectionRun> = None;
        for glyph in &placed.row.glyphs {
            while si < sections.len() && byte >= usize::from(sections[si].byte_range.end) {
                si += 1;
            }
            let in_text = si < sections.len() && byte >= usize::from(sections[si].byte_range.start);
            byte += glyph.chr.len_utf8();
            if !in_text {
                if let Some(r) = run.take() {
                    runs.push(r);
                }
                continue;
            }
            // The font's box, not `logical_rect`, which is the row's line height tall.
            let rect = Rect::from_min_size(
                origin + vec2(glyph.pos.x, glyph.pos.y - glyph.font_ascent),
                vec2(glyph.advance_width, glyph.font_height),
            );
            let logical = glyph.logical_rect().translate(origin.to_vec2());
            match run.as_mut() {
                Some(r) if r.section == si => {
                    r.rect = r.rect.union(rect);
                    r.logical = r.logical.union(logical);
                }
                _ => {
                    if let Some(r) = run.take() {
                        runs.push(r);
                    }
                    run = Some(SectionRun {
                        section: si,
                        rect,
                        logical,
                        baseline: origin.y + glyph.pos.y,
                    });
                }
            }
        }
        if let Some(r) = run.take() {
            runs.push(r);
        }
        if placed.ends_with_newline {
            byte += 1;
        }
    }
    runs
}

/// The padding of an inline background around its glyph box, as a fraction
/// of the section's font size — the IDS stylesheet's inline code span.
const PAD_EM: Vec2 = vec2(0.35, 0.12);

fn pad_of(font_size: f32) -> Vec2 {
    (PAD_EM * font_size).round()
}

pub struct PillLabel {
    job: LayoutJob,
    wrap_mode: Option<TextWrapMode>,
    selectable: Option<bool>,
    /// One entry per job section; `None` where the section has no background.
    /// Empty when no section has one.
    pills: Vec<Option<Pill>>,
}

impl PillLabel {
    pub fn new(mut job: LayoutJob) -> Self {
        let pills = take_pills(&mut job);
        Self {
            job,
            wrap_mode: None,
            selectable: None,
            pills,
        }
    }

    /// The `Label`, with the job's baselines aligned (`style::baseline`),
    /// which needs the `Ui`'s fonts and so waits for `ui`.
    fn label(
        job: LayoutJob,
        wrap_mode: Option<TextWrapMode>,
        selectable: Option<bool>,
        ui: &Ui,
    ) -> Label {
        let mut job = job;
        super::baseline::align(ui, &mut job);
        let mut label = Label::new(job);
        if let Some(mode) = wrap_mode {
            label = label.wrap_mode(mode);
        }
        if let Some(selectable) = selectable {
            label = label.selectable(selectable);
        }
        label
    }

    /// Mirrors `Label::selectable`.
    pub fn selectable(mut self, selectable: bool) -> Self {
        self.selectable = Some(selectable);
        self
    }

    /// Mirrors `Label::wrap_mode`.
    pub fn wrap_mode(mut self, mode: TextWrapMode) -> Self {
        self.wrap_mode = Some(mode);
        self
    }
}

/// Takes the backgrounds out of the job's sections and makes room for the
/// side padding. One entry per section, `None` where the section has no
/// background; empty when no section has one.
pub(crate) fn take_pills(job: &mut LayoutJob) -> Vec<Option<Pill>> {
    let mut pills = Vec::with_capacity(job.sections.len());
    let mut any = false;
    for i in 0..job.sections.len() {
        let sec = &mut job.sections[i];
        if sec.format.background == Color32::TRANSPARENT {
            pills.push(None);
            continue;
        }
        let pad = pad_of(sec.format.font_id.size);
        pills.push(Some(Pill {
            color: sec.format.background,
            pad,
        }));
        sec.format.background = Color32::TRANSPARENT;
        sec.leading_space += pad.x;
        if let Some(next) = job.sections.get_mut(i + 1) {
            next.leading_space += pad.x;
        }
        any = true;
    }
    if !any {
        pills.clear();
    }
    pills
}

impl Widget for PillLabel {
    fn ui(self, ui: &mut Ui) -> Response {
        let Self {
            job,
            wrap_mode,
            selectable,
            pills,
        } = self;
        let label = Self::label(job, wrap_mode, selectable, ui);
        let selectable = selectable.unwrap_or_else(|| ui.style().interaction.selectable_labels);
        // egui's own Label is used where it does the same: no pills, and no
        // selection an off-screen label would have to keep alive.
        if pills.is_empty() && !(selectable && super::selection::wanted_off_screen(ui)) {
            return ui.add(label);
        }
        let pill_of = |section_index: usize| pills.get(section_index).copied().flatten();
        // Reserve a slot below the text for the pills: shapes added after the
        // label would paint over its glyphs.
        let bg_idx = ui.painter().add(Shape::Noop);
        let (galley_pos, galley, response) = label.layout_in_ui(ui);
        response.widget_info(|| {
            egui::WidgetInfo::labeled(egui::WidgetType::Label, ui.is_enabled(), galley.text())
        });
        let color = ui.style().visuals.text_color();
        if !ui.is_rect_visible(response.rect) {
            // Off screen, but it may hold one end of a selection, which egui
            // drops in any pass that does not see both ends.
            if selectable && super::selection::wanted_off_screen(ui) {
                super::selection::register(
                    ui,
                    &response,
                    galley_pos,
                    galley,
                    color,
                    Stroke::NONE,
                    false,
                );
            }
        } else {
            let radius = egui::CornerRadius::same(tokens::rounding::SM as u8);
            let ppp = ui.pixels_per_point();
            let mut shapes = Vec::new();
            for run in section_runs(&galley, galley_pos) {
                if let Some(pill) = pill_of(run.section) {
                    let rect = run.rect.expand2(pill.pad).round_to_pixels(ppp);
                    shapes.push(Shape::rect_filled(rect, radius, pill.color));
                }
            }
            ui.painter().set(bg_idx, Shape::Vec(shapes));

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
        response
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use egui::TextFormat;

    #[test]
    fn backgrounds_leave_the_job_and_become_pills() {
        let mut job = LayoutJob::default();
        job.append("plain ", 0.0, TextFormat::default());
        job.append(
            "code",
            0.0,
            TextFormat {
                background: Color32::DARK_GRAY,
                ..Default::default()
            },
        );
        job.append(" after", 0.0, TextFormat::default());
        let pills = take_pills(&mut job);
        assert_eq!(pills.len(), 3);
        assert!(pills[0].is_none() && pills[2].is_none());
        let pill = pills[1].expect("the code section is a pill");
        let lead: Vec<f32> = job.sections.iter().map(|s| s.leading_space).collect();
        assert_eq!(
            lead,
            vec![0.0, pill.pad.x, pill.pad.x],
            "the pill and the section after it make room for the side padding"
        );
        for sec in &job.sections {
            assert_eq!(sec.format.background, Color32::TRANSPARENT);
        }
        assert_eq!(pill.color, Color32::DARK_GRAY);
        let pad = pad_of(TextFormat::default().font_id.size);
        assert_eq!(pill.pad, pad);
        assert!(
            pad.x >= 1.0 && pad.y >= 1.0,
            "padding is whole points, not zero: {pad:?}"
        );
    }

    #[test]
    fn a_job_without_backgrounds_is_the_plain_label() {
        let mut job = LayoutJob::default();
        job.append("plain", 0.0, TextFormat::default());
        assert!(PillLabel::new(job).pills.is_empty());
    }
}
