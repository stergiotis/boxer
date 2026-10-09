//! Window trees (ADR-0301): which widgets each message of a replayed stream
//! drew, and where.
//!
//! While a `tree` capture replays, the interpreter brackets every message it
//! consumes with [`OpTreeRecorder::begin`] and [`OpTreeRecorder::end`]. A
//! widget is owned by the innermost message open when egui registered it:
//! at a message's end, every widget in egui's per-pass list
//! (`ViewportState::this_pass.widgets`, which egui fills for every allocated
//! rect, interactive or not) that no message has claimed yet becomes that
//! message's. Children end before their parent, so they claim first; a
//! widget a container registers after its body ran lands on the container.
//! Nothing here depends on the order of egui's lists, only on ids.
//!
//! [`OpTreeRecorder::write_json`] joins the rows with the AccessKit tree of
//! the same pass by egui id, for roles, names and values. AccessKit is
//! switched on in the capture context only; the live context never builds
//! it for this.

use std::collections::{HashMap, HashSet};
use std::fmt::Write as _;

/// The wire version of the document `write_json` produces.
pub const TREE_VERSION: u32 = 1;

struct Row {
    name: String,
    parent: Option<usize>,
    /// Union of the visible rects of every widget in this row's subtree.
    subtree: Option<egui::Rect>,
    /// Widgets this message registered itself: egui id and visible rect.
    own: Vec<(u64, egui::Rect)>,
}

#[derive(Default)]
pub struct OpTreeRecorder {
    rows: Vec<Row>,
    stack: Vec<usize>,
    claimed: HashSet<u64>,
    primed: bool,
}

/// Every widget registered so far this pass: id, and the rect it is shown
/// in — egui's interact rect (the widget's rect clipped to its ui) mapped
/// through its layer's transform.
fn widgets(ctx: &egui::Context) -> Vec<(u64, egui::Rect)> {
    let mut raw: Vec<(u64, egui::LayerId, egui::Rect)> = Vec::new();
    ctx.viewport(|v| {
        for (layer, ws) in v.this_pass.widgets.layers() {
            for w in ws {
                raw.push((w.id.value(), *layer, w.interact_rect));
            }
        }
    });
    raw.into_iter()
        .map(|(id, l, r)| (id, ctx.layer_transform_to_global(l).map_or(r, |t| t * r)))
        .collect()
}

impl OpTreeRecorder {
    pub fn begin(&mut self, ctx: &egui::Context, name: String) {
        if !self.primed {
            // Widgets registered before the first message — the replay's
            // root ui — belong to no message.
            self.claimed.extend(widgets(ctx).into_iter().map(|(id, _)| id));
            self.primed = true;
        }
        self.rows.push(Row {
            name,
            parent: self.stack.last().copied(),
            subtree: None,
            own: Vec::new(),
        });
        self.stack.push(self.rows.len() - 1);
    }

    /// Closes the innermost message. `discard` drops its row: a block's end
    /// marker is a message of its own and stands for nothing on screen. Its
    /// widgets, if any, stay unclaimed and go to the enclosing message.
    pub fn end(&mut self, ctx: &egui::Context, discard: bool) {
        let Some(row) = self.stack.pop() else {
            return;
        };
        if discard {
            if row + 1 == self.rows.len() {
                self.rows.pop();
            }
            return;
        }
        for (id, r) in widgets(ctx) {
            if !self.claimed.insert(id) {
                continue;
            }
            // A widget clipped to nothing is not on screen; the tree shows
            // no more than the pixels (ADR-0301 §SD4).
            if !r.is_positive() {
                continue;
            }
            let row = &mut self.rows[row];
            row.own.push((id, r));
            row.subtree = Some(row.subtree.map_or(r, |u| u.union(r)));
        }
        if let (Some(sub), Some(parent)) = (self.rows[row].subtree, self.rows[row].parent) {
            let p = &mut self.rows[parent];
            p.subtree = Some(p.subtree.map_or(sub, |u| u.union(sub)));
        }
    }

    /// The document of ADR-0301 §SD5. Rows whose subtree holds no visible
    /// widget are left out, and a kept row's parent is its nearest kept
    /// ancestor.
    pub fn write_json(&self, accesskit: Option<&egui::accesskit::TreeUpdate>, out: &mut String) {
        let mut nodes: HashMap<u64, &egui::accesskit::Node> = HashMap::new();
        if let Some(u) = accesskit {
            nodes.extend(u.nodes.iter().map(|(id, n)| (id.0, n)));
        }
        let mut index: Vec<Option<usize>> = vec![None; self.rows.len()];
        let mut kept = 0usize;
        for (i, r) in self.rows.iter().enumerate() {
            if r.subtree.is_some() {
                index[i] = Some(kept);
                kept += 1;
            }
        }
        let _ = write!(out, "{{\"v\":{TREE_VERSION},\"ops\":[");
        let mut first = true;
        for (i, r) in self.rows.iter().enumerate() {
            let Some(sub) = r.subtree else {
                continue;
            };
            if index[i].is_none() {
                continue;
            }
            let mut parent = r.parent;
            while let Some(p) = parent {
                if index[p].is_some() {
                    break;
                }
                parent = self.rows[p].parent;
            }
            if !first {
                out.push(',');
            }
            first = false;
            let parent = parent.and_then(|p| index[p]).map_or(-1, |p| p as i64);
            let _ = write!(
                out,
                "{{\"op\":\"{}\",\"parent\":{parent},\"rect\":",
                escape(&r.name)
            );
            write_rect(out, sub);
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
    fn record(f: impl FnOnce(&mut egui::Ui, &mut OpTreeRecorder)) -> serde_doc::Doc {
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
        serde_doc::parse(&s)
    }

    #[test]
    fn a_widget_belongs_to_the_innermost_message() {
        let d = record(|ui, rec| {
            let ctx = ui.ctx().clone();
            rec.begin(&ctx, "Group".into());
            rec.begin(&ctx, "Button".into());
            let _ = ui.button("Ok");
            rec.end(&ctx, false);
            rec.begin(&ctx, "Label".into());
            ui.label("note");
            rec.end(&ctx, false);
            rec.end(&ctx, false);
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
    fn rows_without_widgets_are_left_out_and_parents_skip_them() {
        let d = record(|ui, rec| {
            let ctx = ui.ctx().clone();
            rec.begin(&ctx, "Outer".into());
            rec.begin(&ctx, "AddSpace".into());
            ui.add_space(4.0);
            rec.end(&ctx, false);
            rec.begin(&ctx, "Middle".into());
            rec.begin(&ctx, "Button".into());
            let _ = ui.button("x");
            rec.end(&ctx, false);
            rec.end(&ctx, false);
            rec.end(&ctx, false);
        });
        let names: Vec<&str> = d.ops.iter().map(|o| o.op.as_str()).collect();
        assert_eq!(names, ["Outer", "Middle", "Button"]);
        assert_eq!(d.ops[2].parent, 1);
    }

    #[test]
    fn an_end_marker_leaves_its_widgets_to_the_enclosing_message() {
        let d = record(|ui, rec| {
            let ctx = ui.ctx().clone();
            rec.begin(&ctx, "Block".into());
            rec.begin(&ctx, "End".into());
            let _ = ui.button("late");
            rec.end(&ctx, true);
            rec.end(&ctx, false);
        });
        assert_eq!(d.ops.len(), 1);
        assert_eq!(d.ops[0].op, "Block");
        assert_eq!(d.ops[0].widgets[0].name, "late");
    }

    #[test]
    fn a_widget_clipped_to_nothing_is_not_in_the_tree() {
        let d = record(|ui, rec| {
            let ctx = ui.ctx().clone();
            rec.begin(&ctx, "Hidden".into());
            ui.scope(|ui| {
                ui.set_clip_rect(egui::Rect::NOTHING);
                let _ = ui.button("secret");
            });
            rec.end(&ctx, false);
            rec.begin(&ctx, "Shown".into());
            let _ = ui.button("seen");
            rec.end(&ctx, false);
        });
        assert_eq!(d.ops.len(), 1);
        assert_eq!(d.ops[0].op, "Shown");
    }

    #[test]
    fn role_names_are_lower_snake() {
        assert_eq!(role_name(egui::accesskit::Role::Button), "button");
        assert_eq!(role_name(egui::accesskit::Role::CheckBox), "check_box");
    }

    /// A reader for the tests, so the crate needs no JSON dependency.
    mod serde_doc {
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
            pub widgets: Vec<Widget>,
        }
        #[derive(Debug, Default)]
        pub struct Doc {
            pub ops: Vec<Op>,
        }

        /// Parses exactly the shape `write_json` writes, field by field.
        pub fn parse(s: &str) -> Doc {
            let mut d = Doc::default();
            let body = s.split_once("\"ops\":[").expect("ops").1;
            for chunk in body.split("{\"op\":\"").skip(1) {
                let mut op = Op::default();
                op.op = chunk.split('"').next().unwrap().to_string();
                op.parent = field(chunk, "\"parent\":").parse().unwrap();
                let rect = chunk.split_once("\"rect\":[").unwrap().1.split(']').next().unwrap();
                for (k, v) in rect.split(',').enumerate() {
                    op.rect[k] = v.parse().unwrap();
                }
                let widgets = chunk.split_once("\"widgets\":[").unwrap().1;
                for w in widgets.split("{\"id\":").skip(1) {
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

        fn quoted(s: &str, key: &str) -> String {
            let end = s.find('}').unwrap_or(s.len());
            let s = &s[..end];
            s.split_once(key).map_or(String::new(), |(_, r)| {
                r.split('"').next().unwrap().to_string()
            })
        }
    }
}
