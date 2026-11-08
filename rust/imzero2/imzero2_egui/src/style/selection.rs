//! Label text selection that survives scrolling and sweeps across columns
//! (ADR-0363 §M6, proposed).
//!
//! egui drops a selection at the end of any pass in which it did not see
//! both of its ends, and its labels take part in selection only while they
//! are on screen, so a selection whose start has scrolled away is lost.
//! [`register`] keeps an off-screen label taking part while a selection
//! exists. egui also treats two galleys whose bounds do not overlap
//! horizontally as separate columns and will not sweep a drag from one to
//! the other; [`register`] can give a galley the full width of its clip
//! rect for that test, leaving every glyph where it was drawn.
//!
//! The shared-column transform follows zapfast's `markup::paint_selectable`
//! (<https://github.com/crmne/zapfast>, commit
//! 421d194fb45530ad0811e2b5d81be8f805c47748), under this notice:
//!
//! MIT License
//!
//! Copyright (c) 2026 Carmine Paolino
//!
//! Permission is hereby granted, free of charge, to any person obtaining a copy
//! of this software and associated documentation files (the "Software"), to deal
//! in the Software without restriction, including without limitation the rights
//! to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//! copies of the Software, and to permit persons to whom the Software is
//! furnished to do so, subject to the following conditions:
//!
//! The above copyright notice and this permission notice shall be included in all
//! copies or substantial portions of the Software.
//!
//! THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//! IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//! FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//! AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//! LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//! OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//! SOFTWARE.

use std::sync::Arc;

use egui::text_selection::LabelSelectionState;
use egui::{Color32, Galley, Pos2, Response, Stroke, Ui};

/// Whether a label that is not on screen should still take part in
/// selection: only while some selection exists, since taking part costs a
/// text shape that is clipped away.
pub fn wanted_off_screen(ui: &Ui) -> bool {
    ui.ctx().plugin::<LabelSelectionState>().lock().has_selection()
}

/// Hands a selectable label's galley to egui's selection, which also paints
/// it. With `shared_column`, the galley's bounds span the clip rect's width
/// for egui's column test while its rows keep their screen positions, so a
/// drag sweeps from this label into a neighbour that does not overlap it
/// horizontally (left- and right-aligned bubbles, side-by-side cells).
pub fn register(
    ui: &Ui,
    response: &Response,
    galley_pos: Pos2,
    galley: Arc<Galley>,
    color: Color32,
    underline: Stroke,
    shared_column: bool,
) {
    let (pos, galley) = if shared_column {
        widen_to_column(ui, galley_pos, &galley)
    } else {
        (galley_pos, galley)
    };
    LabelSelectionState::label_text_selection(ui, response, pos, galley, color, underline);
}

/// The galley moved into a frame whose origin is the clip rect's left edge
/// and whose width is the clip rect's, with each row shifted so it paints
/// where it did. Both ends land on whole physical pixels: egui rounds where
/// it paints the galley, not this shift, and a fractional offset leaves
/// every glyph between pixels.
fn widen_to_column(ui: &Ui, galley_pos: Pos2, galley: &Galley) -> (Pos2, Arc<Galley>) {
    let ppp = ui.pixels_per_point();
    let snap = |x: f32| (x * ppp).round() / ppp;
    let column = ui.clip_rect().x_range();
    let left = snap(column.min);
    let offset = snap(galley_pos.x) - left;
    let mut widened = galley.clone();
    for row in &mut widened.rows {
        row.pos.x += offset;
    }
    widened.rect.min.x = 0.0;
    widened.rect.max.x = column.span();
    widened.mesh_bounds = widened.mesh_bounds.translate(egui::vec2(offset, 0.0));
    (Pos2::new(left, galley_pos.y), Arc::new(widened))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::style::linked::LinkedLabel;
    use egui::text::LayoutJob;
    use egui::{Event, PointerButton, RawInput, Rect, TextFormat, pos2, vec2};

    fn job(text: &str) -> LayoutJob {
        let mut job = LayoutJob::default();
        job.append(text, 0.0, TextFormat::default());
        job
    }

    fn input(events: Vec<Event>) -> RawInput {
        RawInput {
            screen_rect: Some(Rect::from_min_size(Pos2::ZERO, vec2(400.0, 300.0))),
            events,
            ..Default::default()
        }
    }

    /// Two selectable paragraphs, the second 120 points below the first.
    /// With `scrolled`, the clip rect leaves the first one out, as a scroll
    /// area does once it has scrolled past it. Returns both label rects.
    fn pass(ctx: &egui::Context, events: Vec<Event>, scrolled: bool) -> (Rect, Rect) {
        let mut rects = (Rect::NOTHING, Rect::NOTHING);
        let mut out = ctx.run_ui(input(events), |ui| {
            if scrolled {
                let mut clip = ui.clip_rect();
                clip.min.y = 100.0;
                ui.set_clip_rect(clip);
            }
            rects.0 = LinkedLabel::new(job("the first paragraph"), vec![None])
                .selectable(true)
                .show(ui)
                .response
                .rect;
            ui.add_space(120.0);
            rects.1 = LinkedLabel::new(job("the second paragraph"), vec![None])
                .selectable(true)
                .show(ui)
                .response
                .rect;
        });
        out.textures_delta.clear();
        rects
    }

    fn has_selection(ctx: &egui::Context) -> bool {
        ctx.plugin::<LabelSelectionState>().lock().has_selection()
    }

    /// A drag from the start of the first paragraph to the end of the second.
    fn select_across(ctx: &egui::Context) {
        let (a, b) = pass(ctx, vec![], false);
        let from = a.left_center() + vec2(1.0, 0.0);
        let to = b.right_center() - vec2(1.0, 0.0);
        let press = |pos, pressed| Event::PointerButton {
            pos,
            button: PointerButton::Primary,
            pressed,
            modifiers: Default::default(),
        };
        pass(ctx, vec![Event::PointerMoved(from)], false);
        pass(ctx, vec![press(from, true)], false);
        pass(
            ctx,
            vec![Event::PointerMoved(pos2(from.x + 5.0, from.y))],
            false,
        );
        pass(ctx, vec![Event::PointerMoved(to)], false);
        pass(ctx, vec![press(to, false)], false);
        pass(ctx, vec![], false);
        assert!(has_selection(ctx), "the drag selected text");
    }

    /// A paragraph on the left and, below it, one on the right, as an
    /// incoming and an outgoing message; their bounds do not overlap
    /// horizontally. Returns the copied text after a drag that starts in the
    /// right one and ends above the left one. egui reaches the left one only
    /// through its column test, since the pointer never lands in it.
    fn copy_upward_across_columns(shared_column: bool) -> String {
        let ctx = egui::Context::default();
        let label = |text: &str| {
            let l = LinkedLabel::new(job(text), vec![None]).selectable(true);
            if shared_column {
                l.selection_column()
            } else {
                l
            }
        };
        let run = |events: Vec<Event>| {
            let mut rects = (Rect::NOTHING, Rect::NOTHING);
            let mut out = ctx.run_ui(input(events), |ui| {
                ui.add_space(40.0);
                rects.0 = label("incoming").show(ui).response.rect;
                ui.add_space(20.0);
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Min), |ui| {
                    rects.1 = label("outgoing").show(ui).response.rect;
                });
            });
            out.textures_delta.clear();
            (rects, out.platform_output)
        };
        let ((left, right), _) = run(vec![]);
        assert!(
            !left.x_range().intersects(right.x_range()),
            "the fixture's columns overlap"
        );
        let from = right.right_center() - vec2(1.0, 0.0);
        let to = pos2(left.center().x, left.top() - 20.0);
        let press = |pos, pressed| Event::PointerButton {
            pos,
            button: PointerButton::Primary,
            pressed,
            modifiers: Default::default(),
        };
        run(vec![Event::PointerMoved(from)]);
        run(vec![press(from, true)]);
        run(vec![Event::PointerMoved(pos2(from.x - 5.0, from.y))]);
        run(vec![Event::PointerMoved(to)]);
        run(vec![Event::PointerMoved(to)]);
        run(vec![press(to, false)]);
        run(vec![]);
        let (_, output) = run(vec![Event::Copy]);
        output
            .commands
            .iter()
            .find_map(|c| match c {
                egui::OutputCommand::CopyText(t) => Some(t.clone()),
                _ => None,
            })
            .unwrap_or_default()
    }

    fn swept_both(copied: &str) -> bool {
        copied.contains("incoming") && copied.contains("outgoing")
    }

    #[test]
    fn an_upward_drag_sweeps_across_columns_with_a_shared_column() {
        let copied = copy_upward_across_columns(true);
        assert!(swept_both(&copied), "copied {copied:?}");
    }

    /// The case the shared column exists for. If this starts failing, egui
    /// has changed its column test and the shared column may no longer be
    /// needed.
    #[test]
    fn without_a_shared_column_the_other_column_is_left_out() {
        let copied = copy_upward_across_columns(false);
        assert!(!swept_both(&copied), "copied {copied:?}");
    }

    #[test]
    fn a_selection_survives_its_start_scrolling_away() {
        let ctx = egui::Context::default();
        select_across(&ctx);
        pass(&ctx, vec![], true);
        pass(&ctx, vec![], true);
        assert!(
            has_selection(&ctx),
            "the selection outlived its start leaving the view"
        );
    }
}
