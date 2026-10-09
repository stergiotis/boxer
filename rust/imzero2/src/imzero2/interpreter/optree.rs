//! Window trees (ADR-0301): which widgets each message of a replayed stream
//! drew, and where.
//!
//! While a `tree` capture replays, the interpreter brackets every message it
//! consumes with [`OpTreeRecorder::begin`] and [`OpTreeRecorder::end`], and
//! every deferred block it replays with [`OpTreeRecorder::begin_block`] and
//! [`OpTreeRecorder::end`]. A widget belongs to the innermost row open when
//! egui registered it. The recorder reads egui's per-pass widget list
//! (`ViewportState::this_pass.widgets`, which egui fills for every allocated
//! rect, interactive or not) at every bracket: widgets new since the last
//! read were registered by the row open in between, so a begin hands them to
//! the enclosing row and an end to the row that ends. Nothing here depends on
//! the order of egui's lists, only on ids.
//!
//! [`OpTreeRecorder::write_json`] joins the rows with the AccessKit tree of
//! the same pass by egui id, for roles, names and values. AccessKit is
//! switched on in the capture context only; the live context never builds
//! it for this.

use std::collections::{HashMap, HashSet};
use std::fmt::Write as _;

/// The wire version of the document `write_json` produces.
pub const TREE_VERSION: u32 = 1;

/// The `op` of a row standing for one replay of a deferred block — a table
/// cell or row, a popup's or tooltip's body — rather than a message.
pub const BLOCK_OP: &str = "DeferredBlock";

struct Row {
    name: String,
    parent: Option<usize>,
    /// Union of the visible rects of every widget in this row's subtree.
    subtree: Option<egui::Rect>,
    /// Widgets this row registered itself: egui id and visible rect.
    own: Vec<(u64, egui::Rect)>,
    /// Widgets this row registered that were clipped to nothing.
    clipped: u32,
    /// The io's deferred-block count when the row began.
    blocks_at_begin: u64,
    /// Deferred blocks read inside the row, less those its children read.
    blocks: u64,
    /// Deferred blocks the row's children read.
    blocks_in_children: u64,
}

#[derive(Default)]
pub struct OpTreeRecorder {
    rows: Vec<Row>,
    stack: Vec<usize>,
    claimed: HashSet<u64>,
    primed: bool,
}

/// Every widget registered so far this pass: id, its rect, and the part of
/// it on screen — egui's interact rect, the rect clipped to its ui — both
/// mapped through the layer's transform.
fn widgets(ctx: &egui::Context) -> Vec<(u64, egui::Rect, egui::Rect)> {
    let mut raw: Vec<(u64, egui::LayerId, egui::Rect, egui::Rect)> = Vec::new();
    ctx.viewport(|v| {
        for (layer, ws) in v.this_pass.widgets.layers() {
            for w in ws {
                raw.push((w.id.value(), *layer, w.rect, w.interact_rect));
            }
        }
    });
    raw.into_iter()
        .map(|(id, l, r, shown)| match ctx.layer_transform_to_global(l) {
            Some(t) => (id, t * r, t * shown),
            None => (id, r, shown),
        })
        .collect()
}

impl OpTreeRecorder {
    /// Opens a row for a message. `blocks_read` is the io's deferred-block
    /// count.
    pub fn begin(&mut self, ctx: &egui::Context, name: String, blocks_read: u64) {
        self.open(ctx, name, blocks_read);
    }

    /// Opens a row for one replay of a deferred block.
    pub fn begin_block(&mut self, ctx: &egui::Context, blocks_read: u64) {
        self.open(ctx, BLOCK_OP.to_string(), blocks_read);
    }

    fn open(&mut self, ctx: &egui::Context, name: String, blocks_read: u64) {
        if self.primed {
            // What was registered since the last bracket belongs to the row
            // still open: a container's own widgets, drawn before or between
            // its children.
            self.sweep(ctx, self.stack.last().copied());
        } else {
            // Widgets registered before the first message — the replay's
            // root ui — belong to no row.
            self.sweep(ctx, None);
            self.primed = true;
        }
        self.rows.push(Row {
            name,
            parent: self.stack.last().copied(),
            subtree: None,
            own: Vec::new(),
            clipped: 0,
            blocks_at_begin: blocks_read,
            blocks: 0,
            blocks_in_children: 0,
        });
        self.stack.push(self.rows.len() - 1);
    }

    /// Closes the innermost row. `discard` drops it: a block's end marker is
    /// a message of its own and stands for nothing on screen. Its widgets,
    /// if any, stay unclaimed and go to the enclosing row.
    pub fn end(&mut self, ctx: &egui::Context, discard: bool, blocks_read: u64) {
        let Some(row) = self.stack.pop() else {
            return;
        };
        if discard {
            if row + 1 == self.rows.len() {
                self.rows.pop();
            }
            return;
        }
        self.sweep(ctx, Some(row));
        let r = &mut self.rows[row];
        let read = blocks_read.saturating_sub(r.blocks_at_begin);
        r.blocks = read.saturating_sub(r.blocks_in_children);
        let (sub, parent) = (r.subtree, r.parent);
        if let Some(p) = parent {
            let p = &mut self.rows[p];
            p.blocks_in_children += read;
            if let Some(sub) = sub {
                p.subtree = Some(p.subtree.map_or(sub, |u| u.union(sub)));
            }
        }
    }

    /// Hands every widget no row has claimed yet to `row`, or to none.
    fn sweep(&mut self, ctx: &egui::Context, row: Option<usize>) {
        for (id, full, r) in widgets(ctx) {
            if !self.claimed.insert(id) {
                continue;
            }
            let Some(row) = row else {
                continue;
            };
            let row = &mut self.rows[row];
            // A widget clipped to nothing is not on screen; the tree shows
            // no more than the pixels (ADR-0301 §SD4), and counts it. One of
            // no size — a ui's placeholder — is not counted.
            if !r.is_positive() {
                if full.is_positive() {
                    row.clipped += 1;
                }
                continue;
            }
            row.own.push((id, r));
            row.subtree = Some(row.subtree.map_or(r, |u| u.union(r)));
        }
    }

    /// The document of ADR-0301 §SD5. Rows whose subtree holds no visible
    /// widget are left out; a kept row's parent is its nearest kept
    /// ancestor, which also takes the clipped widgets and undrawn blocks of
    /// the rows left out below it.
    pub fn write_json(&self, accesskit: Option<&egui::accesskit::TreeUpdate>, out: &mut String) {
        let mut nodes: HashMap<u64, &egui::accesskit::Node> = HashMap::new();
        if let Some(u) = accesskit {
            nodes.extend(u.nodes.iter().map(|(id, n)| (id.0, n)));
        }
        let kept_ancestor = |mut i: Option<usize>| {
            while let Some(p) = i {
                if self.rows[p].subtree.is_some() {
                    break;
                }
                i = self.rows[p].parent;
            }
            i
        };
        let mut index: Vec<Option<usize>> = vec![None; self.rows.len()];
        let mut clipped: Vec<u64> = vec![0; self.rows.len()];
        let mut blocks: Vec<u64> = vec![0; self.rows.len()];
        let mut kept = 0usize;
        for (i, r) in self.rows.iter().enumerate() {
            let at = if r.subtree.is_some() {
                index[i] = Some(kept);
                kept += 1;
                Some(i)
            } else {
                kept_ancestor(r.parent)
            };
            if let Some(at) = at {
                clipped[at] += u64::from(r.clipped);
                blocks[at] += r.blocks;
            }
        }
        let _ = write!(out, "{{\"v\":{TREE_VERSION},\"ops\":[");
        let mut first = true;
        for (i, r) in self.rows.iter().enumerate() {
            let Some(sub) = r.subtree else {
                continue;
            };
            if !first {
                out.push(',');
            }
            first = false;
            let parent = kept_ancestor(r.parent).and_then(|p| index[p]).map_or(-1, |p| p as i64);
            let _ = write!(
                out,
                "{{\"op\":\"{}\",\"parent\":{parent},\"rect\":",
                escape(&r.name)
            );
            write_rect(out, sub);
            if clipped[i] > 0 {
                let _ = write!(out, ",\"clipped\":{}", clipped[i]);
            }
            if blocks[i] > 0 {
                let _ = write!(out, ",\"blocks\":{}", blocks[i]);
            }
            out.push_str(",\"widgets\":[");
            for (j, (id, rect)) in r.own.iter().enumerate() {
                if j > 0 {
                    out.push(',');
                }
                let _ = write!(out, "{{\"id\":{id},\"rect\":");
                write_rect(out, *rect);
                if let Some(n) = nodes.get(id) {
                    let _ = write!(out, ",\"role\":\"{}\"", role_name(n.role()));
                    // egui gives a label's text as its value, not its name.
                    let is_label = n.role() == egui::accesskit::Role::Label;
                    let name = if is_label { n.value() } else { n.label() };
                    if let Some(l) = name.filter(|l| !l.is_empty()) {
                        let _ = write!(out, ",\"name\":\"{}\"", escape(l));
                    }
                    if !is_label {
                        if let Some(v) = n.value() {
                            let _ = write!(out, ",\"value\":\"{}\"", escape(v));
                        } else if let Some(v) = n.numeric_value() {
                            let _ = write!(out, ",\"value\":\"{v}\"");
                        }
                    }
                }
                out.push('}');
            }
            out.push_str("]}");
        }
        out.push_str("]}");
    }
}

/// An AccessKit role in lower snake case, the vocabulary the headless
/// driver's tree uses too (ADR-0154).
pub fn role_name(role: egui::accesskit::Role) -> String {
    let debug = format!("{role:?}");
    let mut out = String::with_capacity(debug.len() + 4);
    for (i, ch) in debug.char_indices() {
        if ch.is_ascii_uppercase() {
            if i != 0 {
                out.push('_');
            }
            out.push(ch.to_ascii_lowercase());
        } else {
            out.push(ch);
        }
    }
    out
}

fn write_rect(out: &mut String, r: egui::Rect) {
    let _ = write!(
        out,
        "[{},{},{},{}]",
        round1(r.min.x),
        round1(r.min.y),
        round1(r.width()),
        round1(r.height())
    );
}

/// One decimal: sub-point precision is noise to a reader of the tree.
fn round1(v: f32) -> f32 {
    (v * 10.0).round() / 10.0
}

fn escape(s: &str) -> String {
    let mut o = String::with_capacity(s.len());
    for ch in s.chars() {
        match ch {
            '"' => o.push_str("\\\""),
            '\\' => o.push_str("\\\\"),
            '\n' => o.push_str("\\n"),
            '\r' => o.push_str("\\r"),
            '\t' => o.push_str("\\t"),
            c if (c as u32) < 0x20 => {
                let _ = write!(o, "\\u{:04x}", c as u32);
            }
            c => o.push(c),
        }
    }
    o
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Runs one pass of a fresh context with AccessKit on, giving `f` the
    /// root ui and the recorder, and returns the document.
    fn record(f: impl FnOnce(&mut egui::Ui, &mut OpTreeRecorder)) -> doc::Doc {
        let ctx = egui::Context::default();
        ctx.enable_accesskit();
        let mut rec = OpTreeRecorder::default();
        let raw = egui::RawInput {
            screen_rect: Some(egui::Rect::from_min_size(
                egui::Pos2::ZERO,
                egui::vec2(400.0, 300.0),
            )),
            ..Default::default()
        };
        let mut f = Some(f);
        let out = ctx.run_ui(raw, |ui| {
            if let Some(f) = f.take() {
                f(ui, &mut rec);
            }
        });
        let mut s = String::new();
        rec.write_json(out.platform_output.accesskit_update.as_ref(), &mut s);
        doc::parse(&s)
    }

    #[test]
    fn a_widget_belongs_to_the_innermost_message() {
        let d = record(|ui, rec| {
            let ctx = ui.ctx().clone();
            rec.begin(&ctx, "Group".into(), 0);
            rec.begin(&ctx, "Button".into(), 0);
            let _ = ui.button("Ok");
            rec.end(&ctx, false, 0);
            rec.begin(&ctx, "Label".into(), 0);
            ui.label("note");
            rec.end(&ctx, false, 0);
            rec.end(&ctx, false, 0);
        });
        assert_eq!(d.ops.len(), 3);
        assert_eq!(d.ops[0].op, "Group");
        assert_eq!(d.ops[0].parent, -1);
        assert!(d.ops[0].widgets.is_empty());
        assert_eq!(d.ops[1].op, "Button");
        assert_eq!(d.ops[1].parent, 0);
        assert_eq!(d.ops[1].widgets.len(), 1);
        assert_eq!(d.ops[1].widgets[0].role, "button");
        assert_eq!(d.ops[1].widgets[0].name, "Ok");
        assert_eq!(d.ops[2].widgets[0].role, "label");
        assert_eq!(d.ops[2].widgets[0].name, "note");
        // The group's rect holds both children.
        let g = d.ops[0].rect;
        for c in [&d.ops[1], &d.ops[2]] {
            assert!(c.rect[0] >= g[0] && c.rect[1] >= g[1]);
            assert!(c.rect[0] + c.rect[2] <= g[0] + g[2] + 0.1);
            assert!(c.rect[1] + c.rect[3] <= g[1] + g[3] + 0.1);
        }
    }

    #[test]
    fn what_a_container_draws_before_or_between_its_children_is_the_containers() {
        let d = record(|ui, rec| {
            let ctx = ui.ctx().clone();
            rec.begin(&ctx, "Tabs".into(), 0);
            let _ = ui.button("first tab");
            rec.begin(&ctx, "AddSpace".into(), 0);
            ui.add_space(4.0);
            rec.end(&ctx, false, 0);
            let _ = ui.button("second tab");
            rec.begin(&ctx, "Body".into(), 0);
            ui.label("body");
            rec.end(&ctx, false, 0);
            rec.end(&ctx, false, 0);
        });
        let names: Vec<&str> = d.ops.iter().map(|o| o.op.as_str()).collect();
        assert_eq!(
            names,
            ["Tabs", "Body"],
            "AddSpace drew nothing and owns nothing"
        );
        let tabs: Vec<&str> = d.ops[0].widgets.iter().map(|w| w.name.as_str()).collect();
        assert_eq!(tabs, ["first tab", "second tab"]);
    }

    #[test]
    fn rows_without_widgets_are_left_out_and_parents_skip_them() {
        let d = record(|ui, rec| {
            let ctx = ui.ctx().clone();
            rec.begin(&ctx, "Outer".into(), 0);
            rec.begin(&ctx, "AddSpace".into(), 0);
            ui.add_space(4.0);
            rec.end(&ctx, false, 0);
            rec.begin(&ctx, "Middle".into(), 0);
            rec.begin(&ctx, "Button".into(), 0);
            let _ = ui.button("x");
            rec.end(&ctx, false, 0);
            rec.end(&ctx, false, 0);
            rec.end(&ctx, false, 0);
        });
        let names: Vec<&str> = d.ops.iter().map(|o| o.op.as_str()).collect();
        assert_eq!(names, ["Outer", "Middle", "Button"]);
        assert_eq!(d.ops[2].parent, 1);
    }

    #[test]
    fn an_end_marker_leaves_its_widgets_to_the_enclosing_message() {
        let d = record(|ui, rec| {
            let ctx = ui.ctx().clone();
            rec.begin(&ctx, "Block".into(), 0);
            rec.begin(&ctx, "End".into(), 0);
            let _ = ui.button("late");
            rec.end(&ctx, true, 0);
            rec.end(&ctx, false, 0);
        });
        assert_eq!(d.ops.len(), 1);
        assert_eq!(d.ops[0].op, "Block");
        assert_eq!(d.ops[0].widgets[0].name, "late");
    }

    #[test]
    fn a_clipped_widget_is_counted_on_the_nearest_row_kept() {
        let d = record(|ui, rec| {
            let ctx = ui.ctx().clone();
            rec.begin(&ctx, "Panel".into(), 0);
            let _ = ui.button("seen");
            rec.begin(&ctx, "Hidden".into(), 0);
            ui.scope(|ui| {
                ui.set_clip_rect(egui::Rect::NOTHING);
                let _ = ui.button("secret");
            });
            rec.end(&ctx, false, 0);
            rec.end(&ctx, false, 0);
        });
        assert_eq!(d.ops.len(), 1);
        assert_eq!(d.ops[0].op, "Panel");
        assert!(d.ops[0].clipped >= 1);
        assert!(!d.raw.contains("secret"));
    }

    #[test]
    fn a_replayed_block_is_a_row_and_undrawn_blocks_are_counted() {
        let d = record(|ui, rec| {
            let ctx = ui.ctx().clone();
            // A table message reads 5 blocks and replays 2 of them.
            rec.begin(&ctx, "Table".into(), 10);
            for cell in ["A320", "2005"] {
                rec.begin_block(&ctx, 15);
                rec.begin(&ctx, "Label".into(), 15);
                ui.label(cell);
                rec.end(&ctx, false, 15);
                rec.end(&ctx, false, 15);
            }
            rec.end(&ctx, false, 15);
        });
        let names: Vec<&str> = d.ops.iter().map(|o| o.op.as_str()).collect();
        assert_eq!(names, ["Table", BLOCK_OP, "Label", BLOCK_OP, "Label"]);
        assert_eq!(d.ops[0].blocks, 5);
        assert_eq!(d.ops[1].parent, 0);
        assert_eq!(d.ops[2].widgets[0].name, "A320");
    }

    #[test]
    fn role_names_are_lower_snake() {
        assert_eq!(role_name(egui::accesskit::Role::Button), "button");
        assert_eq!(role_name(egui::accesskit::Role::CheckBox), "check_box");
    }

    /// A reader for the tests, so the crate needs no JSON dependency.
    mod doc {
        #[derive(Debug, Default)]
        pub struct Widget {
            pub role: String,
            pub name: String,
        }
        #[derive(Debug, Default)]
        pub struct Op {
            pub op: String,
            pub parent: i64,
            pub rect: [f32; 4],
            pub clipped: u64,
            pub blocks: u64,
            pub widgets: Vec<Widget>,
        }
        #[derive(Debug, Default)]
        pub struct Doc {
            pub raw: String,
            pub ops: Vec<Op>,
        }

        /// Parses exactly the shape `write_json` writes, field by field.
        pub fn parse(s: &str) -> Doc {
            let mut d = Doc {
                raw: s.to_string(),
                ..Default::default()
            };
            let body = s.split_once("\"ops\":[").expect("ops").1;
            for chunk in body.split("{\"op\":\"").skip(1) {
                let mut op = Op {
                    op: chunk.split('"').next().unwrap().to_string(),
                    ..Default::default()
                };
                let head = chunk.split_once("\"widgets\":[").unwrap();
                op.parent = field(head.0, "\"parent\":").parse().unwrap();
                op.clipped = opt(head.0, "\"clipped\":");
                op.blocks = opt(head.0, "\"blocks\":");
                let rect = head.0.split_once("\"rect\":[").unwrap().1.split(']').next().unwrap();
                for (k, v) in rect.split(',').enumerate() {
                    op.rect[k] = v.parse().unwrap();
                }
                for w in head.1.split("{\"id\":").skip(1) {
                    op.widgets.push(Widget {
                        role: quoted(w, "\"role\":\""),
                        name: quoted(w, "\"name\":\""),
                    });
                }
                d.ops.push(op);
            }
            d
        }

        fn field<'a>(s: &'a str, key: &str) -> &'a str {
            let rest = s.split_once(key).unwrap().1;
            rest.split([',', '}']).next().unwrap()
        }

        fn opt(s: &str, key: &str) -> u64 {
            if s.contains(key) {
                field(s, key).parse().unwrap()
            } else {
                0
            }
        }

        fn quoted(s: &str, key: &str) -> String {
            let end = s.find('}').unwrap_or(s.len());
            let s = &s[..end];
            s.split_once(key).map_or(String::new(), |(_, r)| {
                r.split('"').next().unwrap().to_string()
            })
        }
    }
}
