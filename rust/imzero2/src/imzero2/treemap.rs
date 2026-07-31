//! AccessKit tree → wire snapshot (ADR-0154 SD2).
//!
//! The headless host cannot compile `egui_inspection` — that plugin needs
//! eframe, which the headless build exists to exclude — so this module is how a
//! non-browser client learns which widget is where. egui already builds an
//! AccessKit tree whenever `Context::enable_accesskit` is on; this maps it onto
//! the reduced [`pb::TreeNode`] the wire declares.
//!
//! The projection is deliberately lossy. Carrying `accesskit::Node` verbatim
//! would need no mapping code, but the wire would then be AccessKit's to change
//! across upstream releases and the Go side would parse an unschema'd document.
//! What survives here is what a driver resolves a locator against and asserts an
//! effect with: id, role, name, value, bounds, four state bits, and the parent
//! and child links needed to walk it.
//!
//! The mapping is pure, so it is unit-tested against hand-built nodes the same
//! way [`super::inputmap::cursor_shape_code`] pins its own codes.

use super::inputproto as pb;
// egui re-exports the accesskit it was built against; going through the
// re-export rather than declaring our own dependency keeps the types
// identical to the ones egui hands us, with no version to keep in step.
use egui::accesskit;

/// `TreeNode.flags` bits. Wire-stable: append, never renumber.
pub const FLAG_DISABLED: u32 = 1;
pub const FLAG_HIDDEN: u32 = 2;
pub const FLAG_FOCUSED: u32 = 4;
pub const FLAG_SELECTED: u32 = 8;

/// Wire codes for [`pb::AccessKitAction::action`] (ADR-0154 SD3). Pinned here
/// rather than taken from `accesskit::Action as u32`: that enum carries no
/// `#[repr]` and no ordering guarantee, so a variant inserted upstream would
/// silently renumber every action on the wire.
pub const ACTION_CLICK: u32 = 0;
pub const ACTION_FOCUS: u32 = 1;
pub const ACTION_SET_VALUE: u32 = 2;
pub const ACTION_SCROLL_INTO_VIEW: u32 = 3;

/// Translate a wire action code into the AccessKit action egui honours when the
/// request is injected. `None` for an unknown code — an unknown action is
/// dropped rather than guessed at, the same way `inputmap` drops an unknown
/// pointer button.
pub fn action_from_code(code: u32) -> Option<accesskit::Action> {
    match code {
        ACTION_CLICK => Some(accesskit::Action::Click),
        ACTION_FOCUS => Some(accesskit::Action::Focus),
        ACTION_SET_VALUE => Some(accesskit::Action::SetValue),
        ACTION_SCROLL_INTO_VIEW => Some(accesskit::Action::ScrollIntoView),
        _ => None,
    }
}

/// Flatten one `TreeUpdate` into the wire snapshot.
///
/// `pass` is the host's frame counter, so a client can tell two snapshots apart
/// and pair one with a capture. Parent links are derived from the child lists —
/// AccessKit stores the tree top-down and a driver wants to walk it both ways
/// (ancestry-scoped anchors are the third rung of the ADR-0127 ladder).
pub fn snapshot(update: &accesskit::TreeUpdate, pass: u64) -> pb::TreeSnapshot {
    let mut parents: std::collections::HashMap<u64, u64> =
        std::collections::HashMap::with_capacity(update.nodes.len());
    for (id, node) in &update.nodes {
        for child in node.children() {
            parents.insert(child.0, id.0);
        }
    }

    let nodes = update
        .nodes
        .iter()
        .map(|(id, node)| {
            let mut flags = 0u32;
            if node.is_disabled() {
                flags |= FLAG_DISABLED;
            }
            if node.is_hidden() {
                flags |= FLAG_HIDDEN;
            }
            if node.is_selected().unwrap_or(false) {
                flags |= FLAG_SELECTED;
            }
            if update.focus == *id {
                flags |= FLAG_FOCUSED;
            }
            // Bounds are optional upstream (a node that was never laid out has
            // none); a zero rect is the honest answer and keeps every node in
            // the snapshot, since a driver may still want to match its name.
            let (x, y, w, h) = node.bounds().map_or((0.0, 0.0, 0.0, 0.0), |r| {
                (
                    r.x0 as f32,
                    r.y0 as f32,
                    (r.x1 - r.x0) as f32,
                    (r.y1 - r.y0) as f32,
                )
            });
            pb::TreeNode {
                id: id.0,
                role: role_name(node.role()).to_owned(),
                name: node.label().unwrap_or_default().to_owned(),
                value: node.value().unwrap_or_default().to_owned(),
                x,
                y,
                w,
                h,
                flags,
                parent: parents.get(&id.0).copied().unwrap_or(0),
                children: node.children().iter().map(|c| c.0).collect(),
            }
        })
        .collect();

    pb::TreeSnapshot {
        nodes,
        focus: update.focus.0,
        pass,
    }
}

/// Role as a lower-snake string ("check_box", "text_input", …).
///
/// A string, not an enum: AccessKit's role list is open-ended upstream, and a
/// client matching on a role we have not heard of should see its name rather
/// than a zero. `Debug` is AccessKit's own spelling of the variant, lowercased
/// with word breaks — stable enough for matching and cheap enough to produce
/// here, where the tree is built only on request.
fn role_name(role: accesskit::Role) -> String {
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

#[cfg(test)]
mod tests {
    use super::*;

    fn node(role: accesskit::Role) -> accesskit::Node {
        accesskit::Node::new(role)
    }

    #[test]
    fn role_names_are_lower_snake() {
        assert_eq!(role_name(accesskit::Role::Button), "button");
        assert_eq!(role_name(accesskit::Role::CheckBox), "check_box");
        assert_eq!(role_name(accesskit::Role::Unknown), "unknown");
    }

    #[test]
    fn action_codes_are_pinned() {
        // The wire codes are frozen; this fails if anyone renumbers them.
        assert_eq!(action_from_code(0), Some(accesskit::Action::Click));
        assert_eq!(action_from_code(1), Some(accesskit::Action::Focus));
        assert_eq!(action_from_code(2), Some(accesskit::Action::SetValue));
        assert_eq!(action_from_code(3), Some(accesskit::Action::ScrollIntoView));
        assert_eq!(action_from_code(99), None);
    }

    #[test]
    fn maps_names_bounds_and_links() {
        let root_id = accesskit::NodeId(1);
        let child_id = accesskit::NodeId(2);
        let mut root = node(accesskit::Role::Window);
        root.set_children(vec![child_id]);
        let mut child = node(accesskit::Role::Button);
        child.set_label("Panes");
        child.set_bounds(accesskit::Rect {
            x0: 10.0,
            y0: 20.0,
            x1: 40.0,
            y1: 26.0,
        });
        let update = accesskit::TreeUpdate {
            nodes: vec![(root_id, root), (child_id, child)],
            tree: Some(accesskit::Tree::new(root_id)),
            tree_id: accesskit::TreeId::ROOT,
            focus: child_id,
        };

        let snap = snapshot(&update, 42);
        assert_eq!(snap.pass, 42);
        assert_eq!(snap.focus, child_id.0);
        assert_eq!(snap.nodes.len(), 2);

        let btn = snap.nodes.iter().find(|n| n.id == child_id.0).expect("child in snapshot");
        assert_eq!(btn.name, "Panes");
        assert_eq!(btn.role, "button");
        assert_eq!((btn.x, btn.y, btn.w, btn.h), (10.0, 20.0, 30.0, 6.0));
        assert_eq!(btn.parent, root_id.0, "parent derived from the child list");
        assert_eq!(btn.flags & FLAG_FOCUSED, FLAG_FOCUSED);

        let win = snap.nodes.iter().find(|n| n.id == root_id.0).expect("root in snapshot");
        assert_eq!(win.parent, 0, "root has no parent");
        assert_eq!(win.children, vec![child_id.0]);
        // Never laid out: a zero rect rather than dropping the node.
        assert_eq!((win.x, win.y, win.w, win.h), (0.0, 0.0, 0.0, 0.0));
    }

    #[test]
    fn maps_state_bits() {
        let id = accesskit::NodeId(7);
        let mut n = node(accesskit::Role::CheckBox);
        n.set_disabled();
        n.set_hidden();
        n.set_selected(true);
        let update = accesskit::TreeUpdate {
            nodes: vec![(id, n)],
            tree: Some(accesskit::Tree::new(id)),
            tree_id: accesskit::TreeId::ROOT,
            focus: accesskit::NodeId(0),
        };
        let snap = snapshot(&update, 0);
        let f = snap.nodes[0].flags;
        assert_eq!(f & FLAG_DISABLED, FLAG_DISABLED);
        assert_eq!(f & FLAG_HIDDEN, FLAG_HIDDEN);
        assert_eq!(f & FLAG_SELECTED, FLAG_SELECTED);
        assert_eq!(f & FLAG_FOCUSED, 0, "focus is elsewhere");
    }
}
