//! Accessibility semantics a widget's response does not carry: spoken
//! announcements and list semantics (ADR-0363 §M2, proposed).
//!
//! The announcer is two hidden live-region nodes that take turns, so a
//! repeated announcement is still a change a screen reader speaks. A list is
//! one `ListBox` node whose rows are `ListBoxOption`s at their position in the
//! set, with the current row named as the list's active descendant. Both reach
//! `imzero2 drive` through the same tree a screen reader reads.
//!
//! The announcer and the list semantics follow zapfast's `announcer.rs` and
//! `ui/list_nav.rs` (<https://github.com/crmne/zapfast>, commit
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

use egui::accesskit::{Live, Role};
use egui::{Context, Id, Rect, Ui, UiBuilder};

/// Wire values of `accessibleListItem`'s `selected` argument.
pub const SELECTED_NONE: u8 = 0;
pub const SELECTED_NO: u8 = 1;
pub const SELECTED_YES: u8 = 2;

/// The two announcer nodes and what waits to be spoken in this pass.
#[derive(Default)]
pub struct Announcer {
    pending: Vec<(String, bool)>,
    /// Per slot: the text and whether it is assertive.
    slots: [(String, bool); 2],
    next: usize,
}

pub fn announcer_id(slot: usize) -> Id {
    Id::new(("imzero2_announcer", slot))
}

impl Announcer {
    /// Queues `text` for this pass. `assertive` interrupts what the screen
    /// reader is saying; use it for errors only.
    pub fn say(&mut self, text: &str, assertive: bool) {
        if !text.trim().is_empty() {
            self.pending.push((text.to_owned(), assertive));
        }
    }

    /// Emits both nodes; call once per pass, after the body. What was said in
    /// the pass goes into the slot not used last time, assertive items first,
    /// as one text.
    pub fn show(&mut self, ctx: &Context) {
        if !self.pending.is_empty() {
            let mut items = std::mem::take(&mut self.pending);
            items.sort_by_key(|(_, assertive)| !*assertive);
            let assertive = items.iter().any(|(_, a)| *a);
            let mut text = items.into_iter().map(|(t, _)| t).collect::<Vec<_>>().join(". ");
            let slot = self.next;
            self.next = 1 - self.next;
            // A screen reader speaks a live region only when its text
            // changed, so an identical text gets a zero-width space.
            if self.slots[slot].0 == text {
                text.push('\u{200B}');
            }
            self.slots[slot] = (text, assertive);
        }
        for (slot, (text, assertive)) in self.slots.iter().enumerate() {
            ctx.accesskit_node_builder(announcer_id(slot), |node| {
                node.set_role(Role::Label);
                node.set_value(text.clone());
                node.set_live(if *assertive {
                    Live::Assertive
                } else {
                    Live::Polite
                });
                node.set_bounds(egui::accesskit::Rect {
                    x0: 0.0,
                    y0: 0.0,
                    x1: 1.0,
                    y1: 1.0,
                });
            });
        }
    }
}

/// A list being drawn: its node and the row that is current.
pub struct ListFrame {
    id: Id,
    len: u32,
    current: Option<Id>,
}

/// Turns the `Ui` a list's rows are drawn in into a labelled `ListBox` of
/// `len` rows and returns the frame its rows report to.
pub fn begin_list(ui: &Ui, name: &str, len: u32) -> ListFrame {
    let id = ui.unique_id();
    ui.ctx().accesskit_node_builder(id, |node| {
        node.set_role(Role::ListBox);
        node.set_label(name);
        node.set_size_of_set(len as usize);
    });
    ListFrame {
        id,
        len,
        current: None,
    }
}

/// Names the current row as the list's active descendant. Only a row whose
/// node was made in this pass is named: AccessKit must not be told about a
/// node missing from the tree it receives.
pub fn end_list(ctx: &Context, frame: ListFrame, rect: Rect) {
    ctx.accesskit_node_builder(frame.id, |node| {
        node.set_bounds(bounds(rect));
        if let Some(row) = frame.current {
            node.set_active_descendant(row.accesskit_id());
        }
    });
}

/// The builder of a row's `Ui`. The row's id comes from `key` alone, so a list
/// that draws only the rows near the view does not rename its current row
/// when the rows before it change, and the row's node hangs from the list,
/// not from the containers in between.
pub fn row_builder(ui: &Ui, key: u64, list: Option<&ListFrame>) -> UiBuilder {
    let b = UiBuilder::new().id(ui.id().with(("imzero2_a11y_row", key)));
    match list {
        Some(l) => b.accessibility_parent(l.id),
        None => b,
    }
}

/// Marks a row's node as the list option at `index`, and records it as the
/// list's current row when `current`.
pub fn mark_row(
    ctx: &Context,
    list: Option<&mut ListFrame>,
    row: Id,
    rect: Rect,
    index: u32,
    selected: u8,
    current: bool,
) {
    let len = list.as_ref().map(|l| l.len);
    ctx.accesskit_node_builder(row, |node| {
        node.set_role(Role::ListBoxOption);
        node.set_position_in_set(index as usize + 1);
        if let Some(len) = len {
            node.set_size_of_set(len as usize);
        }
        node.set_bounds(bounds(rect));
        match selected {
            SELECTED_NO => node.set_selected(false),
            SELECTED_YES => node.set_selected(true),
            _ => {}
        }
    });
    if current && let Some(l) = list {
        l.current = Some(row);
    }
}

fn bounds(rect: Rect) -> egui::accesskit::Rect {
    egui::accesskit::Rect {
        x0: rect.min.x.into(),
        y0: rect.min.y.into(),
        x1: rect.max.x.into(),
        y1: rect.max.y.into(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use egui::accesskit::{Node, NodeId, TreeUpdate};

    fn pass(ctx: &Context, body: impl FnMut(&mut Ui)) -> TreeUpdate {
        ctx.enable_accesskit();
        let mut out = ctx.run_ui(egui::RawInput::default(), body);
        out.textures_delta.clear();
        out.platform_output.accesskit_update.expect("accesskit is on")
    }

    fn node(update: &TreeUpdate, id: Id) -> &Node {
        &update.nodes.iter().find(|(n, _)| *n == id.accesskit_id()).expect("node in the tree").1
    }

    #[test]
    fn announcements_alternate_slots_and_repeat() {
        let ctx = Context::default();
        let mut a = Announcer::default();
        a.say("Query done", false);
        let u = pass(&ctx, |ui| a.show(ui.ctx()));
        assert_eq!(node(&u, announcer_id(0)).value(), Some("Query done"));
        assert_eq!(node(&u, announcer_id(0)).live(), Some(Live::Polite));

        a.say("Query done", false);
        let u = pass(&ctx, |ui| a.show(ui.ctx()));
        assert_eq!(node(&u, announcer_id(1)).value(), Some("Query done"));

        // The third goes back to slot 0, whose text is the same: it must
        // change to be spoken.
        a.say("Query done", false);
        let u = pass(&ctx, |ui| a.show(ui.ctx()));
        assert_eq!(
            node(&u, announcer_id(0)).value(),
            Some("Query done\u{200B}")
        );
    }

    #[test]
    fn errors_are_assertive_and_spoken_first() {
        let ctx = Context::default();
        let mut a = Announcer::default();
        a.say("Saved", false);
        a.say("Could not export", true);
        let u = pass(&ctx, |ui| a.show(ui.ctx()));
        let n = node(&u, announcer_id(0));
        assert_eq!(n.value(), Some("Could not export. Saved"));
        assert_eq!(n.live(), Some(Live::Assertive));
    }

    #[test]
    fn a_list_names_its_rows_and_its_current_row() {
        let ctx = Context::default();
        let mut list_id = Id::NULL;
        let mut row_ids = Vec::new();
        let u = pass(&ctx, |ui| {
            ui.scope(|ui| {
                let mut frame = begin_list(ui, "Results", 3);
                list_id = frame.id;
                for (i, key) in [10u64, 11, 12].into_iter().enumerate() {
                    let r = ui.scope_builder(row_builder(ui, key, Some(&frame)), |ui| {
                        ui.label(format!("row {i}"));
                        ui.unique_id()
                    });
                    row_ids.push(r.inner);
                    mark_row(
                        ui.ctx(),
                        Some(&mut frame),
                        r.inner,
                        r.response.rect,
                        i as u32,
                        if i == 1 { SELECTED_YES } else { SELECTED_NO },
                        i == 1,
                    );
                }
                end_list(ui.ctx(), frame, ui.min_rect());
            });
        });
        let list = node(&u, list_id);
        assert_eq!(list.role(), Role::ListBox);
        assert_eq!(list.label(), Some("Results"));
        assert_eq!(list.active_descendant(), Some(row_ids[1].accesskit_id()));
        let ids: Vec<NodeId> = row_ids.iter().map(|r| r.accesskit_id()).collect();
        assert_eq!(
            list.children(),
            ids.as_slice(),
            "the rows hang from the list"
        );
        let middle = node(&u, row_ids[1]);
        assert_eq!(middle.role(), Role::ListBoxOption);
        assert_eq!(middle.position_in_set(), Some(2));
        assert_eq!(middle.size_of_set(), Some(3));
        assert_eq!(middle.is_selected(), Some(true));
    }

    #[test]
    fn a_rows_node_id_does_not_depend_on_the_rows_before_it() {
        let ctx = Context::default();
        let id_of = |skip: usize| {
            let mut id = Id::NULL;
            pass(&ctx, |ui| {
                for _ in 0..skip {
                    ui.scope(|ui| ui.label("before"));
                }
                id = ui.scope_builder(row_builder(ui, 42, None), |ui| ui.unique_id()).inner;
            });
            id
        };
        assert_eq!(id_of(0), id_of(5));
    }
}
