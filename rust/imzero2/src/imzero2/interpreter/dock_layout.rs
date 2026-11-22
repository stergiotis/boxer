//! The dock's declared layout and its per-frame reconciliation (ADR-0404 §SD8).
//!
//! Go declares a dock's layout with `InitRoot` and `Split` and sends it every
//! frame as the `initialLayout` argument of `dockAreaRaw`, beside the list of
//! tabs that exist this frame. The layout is a preset: it builds the dock the
//! first time the host sees it, and after that the stored `DockState` wins, so
//! the person's drags survive. The declaration is still read on later frames,
//! for one purpose: a tab that exists now and is not in the stored state goes
//! where it was declared.
//!
//! - A tab that is in the stored state stays where it is.
//! - A tab no longer sent is removed. Its leaf, emptied, is removed with the
//!   subtree moved up in its place (`Tree::remove_leaf`), not by
//!   `retain_tabs`, whose rebalancing moves a sibling node without its
//!   children and loses a sibling that is itself split.
//! - A tab that appears joins the leaf holding another tab of its declared
//!   group (the tab list of its `InitRoot` or `Split`), in front.
//! - If none of its group is in the stored state, its declared split is made
//!   again: the node split is the smallest one holding every present tab of
//!   the region the declaration split (the parent leaf and the leaves split
//!   off it later), in the declared direction and fraction.
//! - A tab declared nowhere, or whose split cannot be resolved because
//!   nothing of its parent's region is present, goes to the first leaf, as it
//!   did before.
//!
//! Two per-tab options ride on the same frame. A bare tab alone in its leaf
//! is drawn without the leaf's tab strip (`hide_bare_tab_bars`). A closable
//! tab's close is reported to Go under `dock_tab_close_id` and not carried
//! out here.

use std::collections::HashSet;

use egui_dock::{DockState, NodeIndex, NodePath};

/// One `Split` of a declared layout.
#[derive(Debug, Clone, PartialEq)]
pub struct DockSplitDecl {
    /// The declared leaf split: 0 for the root, else an earlier `new_leaf`.
    pub parent: u8,
    /// The leaf this split declares.
    pub new_leaf: u8,
    /// 0=Above 1=Below 2=Left 3=Right.
    pub dir: u8,
    /// The share of the parent the old node keeps.
    pub fraction: f32,
    /// The tabs declared in the new leaf.
    pub tabs: Vec<u64>,
}

/// A dock's declared layout: the root leaf's tabs and the splits, in
/// declaration order.
#[derive(Debug, Clone, PartialEq, Default)]
pub struct DockLayoutDecl {
    pub root_tabs: Vec<u64>,
    pub splits: Vec<DockSplitDecl>,
}

impl DockLayoutDecl {
    /// Parse the Go-side descriptor. The wire format is little-endian:
    ///
    /// ```text
    /// u8  version = 1
    /// u32 num_root_tabs
    /// u64[num_root_tabs] root_tabs
    /// u8  num_splits
    /// for each split:
    ///   u8  parent_leaf_id   // refers to root (0) or a prior new_leaf_id
    ///   u8  new_leaf_id      // assigned by Go, unique within this layout
    ///   u8  direction        // 0=Above 1=Below 2=Left 3=Right
    ///   f32 fraction         // 0..=1, fraction the OLD node keeps
    ///   u32 num_new_leaf_tabs
    ///   u64[num_new_leaf_tabs] new_leaf_tabs
    /// ```
    ///
    /// None for an empty descriptor, an unknown version or a truncated root;
    /// a truncated split list keeps the splits read before it.
    pub fn parse(bytes: &[u8]) -> Option<Self> {
        use byteorder::{LittleEndian, ReadBytesExt as _};
        use std::io::Cursor;

        if bytes.is_empty() {
            return None;
        }
        let mut cur = Cursor::new(bytes);
        if cur.read_u8().ok()? != 1 {
            return None;
        }
        let num_root_tabs = cur.read_u32::<LittleEndian>().ok()?;
        let mut decl = Self::default();
        for _ in 0..num_root_tabs {
            decl.root_tabs.push(cur.read_u64::<LittleEndian>().ok()?);
        }
        let Ok(num_splits) = cur.read_u8() else {
            return Some(decl);
        };
        'splits: for _ in 0..num_splits {
            let (Ok(parent), Ok(new_leaf), Ok(dir), Ok(fraction), Ok(num_tabs)) = (
                cur.read_u8(),
                cur.read_u8(),
                cur.read_u8(),
                cur.read_f32::<LittleEndian>(),
                cur.read_u32::<LittleEndian>(),
            ) else {
                break;
            };
            let mut tabs = Vec::with_capacity(num_tabs as usize);
            for _ in 0..num_tabs {
                match cur.read_u64::<LittleEndian>() {
                    Ok(v) => tabs.push(v),
                    Err(_) => break 'splits,
                }
            }
            decl.splits.push(DockSplitDecl {
                parent,
                new_leaf,
                dir,
                fraction,
                tabs,
            });
        }
        Some(decl)
    }

    /// The declared leaf tab belongs to: 0 for the root, else a split's
    /// `new_leaf`. None when the declaration does not name the tab.
    fn leaf_of(&self, tab: u64) -> Option<u8> {
        if self.root_tabs.contains(&tab) {
            return Some(0);
        }
        self.splits.iter().find(|s| s.tabs.contains(&tab)).map(|s| s.new_leaf)
    }

    /// The tabs declared in leaf.
    fn tabs_of(&self, leaf: u8) -> &[u64] {
        if leaf == 0 {
            return &self.root_tabs;
        }
        self.splits.iter().find(|s| s.new_leaf == leaf).map(|s| s.tabs.as_slice()).unwrap_or(&[])
    }

    /// Build the declared layout. Splits with no tabs or an unknown parent
    /// or direction are skipped: `egui_dock` panics on a split with no tabs.
    pub fn build(&self) -> DockState<u64> {
        let mut state = DockState::new(self.root_tabs.clone());
        let mut leaf_nodes: std::collections::HashMap<u8, NodeIndex> =
            std::collections::HashMap::new();
        leaf_nodes.insert(0, NodeIndex::root());
        for s in &self.splits {
            if s.tabs.is_empty() || s.dir > 3 {
                continue;
            }
            let Some(parent_node) = leaf_nodes.get(&s.parent).copied() else {
                continue;
            };
            let [old_idx, new_idx] = split_node(
                state.main_surface_mut(),
                parent_node,
                s.dir,
                s.fraction,
                s.tabs.clone(),
            );
            leaf_nodes.insert(s.parent, old_idx);
            leaf_nodes.insert(s.new_leaf, new_idx);
        }
        state
    }
}

fn split_node(
    tree: &mut egui_dock::Tree<u64>,
    parent: NodeIndex,
    dir: u8,
    fraction: f32,
    tabs: Vec<u64>,
) -> [NodeIndex; 2] {
    let fraction = fraction.clamp(0.0, 1.0);
    match dir {
        0 => tree.split_above(parent, fraction, tabs),
        1 => tree.split_below(parent, fraction, tabs),
        2 => tree.split_left(parent, fraction, tabs),
        _ => tree.split_right(parent, fraction, tabs),
    }
}

/// Build a dock's first state from its descriptor; with none, every tab in
/// one leaf.
pub fn parse_dock_initial_layout(bytes: &[u8], fallback_tabs: &[u64]) -> DockState<u64> {
    match DockLayoutDecl::parse(bytes) {
        Some(decl) => decl.build(),
        None => DockState::new(fallback_tabs.to_vec()),
    }
}

/// Reconcile a stored dock state with the tabs Go sends this frame, placing
/// a tab that appears per the module comment.
pub fn reconcile_dock_tabs(state: &mut DockState<u64>, tab_ids: &[u64], layout: &[u8]) {
    let wanted: HashSet<u64> = tab_ids.iter().copied().collect();
    while let Some(path) = state.find_tab_from(|t| !wanted.contains(t)) {
        let _ = state.remove_tab(path);
    }
    let mut decl: Option<Option<DockLayoutDecl>> = None;
    for &id in tab_ids {
        if state.find_tab(&id).is_some() {
            continue;
        }
        // Parsed once, and only on a frame where a tab appears.
        let decl = decl.get_or_insert_with(|| DockLayoutDecl::parse(layout));
        let placed = decl.as_ref().is_some_and(|d| place_declared(state, d, id));
        if !placed {
            state.push_to_first_leaf(id);
        }
    }
}

/// Place tab per its declaration; false when it cannot be resolved.
fn place_declared(state: &mut DockState<u64>, decl: &DockLayoutDecl, tab: u64) -> bool {
    let Some(leaf) = decl.leaf_of(tab) else {
        return false;
    };
    // Another tab of the same declared group: join its leaf.
    if let Some(path) =
        decl.tabs_of(leaf).iter().filter(|&&t| t != tab).find_map(|t| state.find_tab(t))
    {
        let np = NodePath {
            surface: path.surface,
            node: path.node,
        };
        if let Ok(l) = state.leaf_mut(np) {
            l.append_tab(tab);
            return true;
        }
    }
    if leaf == 0 {
        return false;
    }
    // Make the declared split again, at the node that now holds what the
    // declaration split.
    let Some(k) = decl.splits.iter().position(|s| s.new_leaf == leaf) else {
        return false;
    };
    let s = &decl.splits[k];
    if s.dir > 3 {
        return false;
    }
    let mut region: Vec<u8> = vec![s.parent];
    for later in &decl.splits[k + 1..] {
        if region.contains(&later.parent) {
            region.push(later.new_leaf);
        }
    }
    let tree = state.main_surface_mut();
    let nodes: Vec<NodeIndex> = region
        .iter()
        .flat_map(|&l| decl.tabs_of(l).iter())
        .filter_map(|t| tree.find_tab(t).map(|(n, _)| n))
        .collect();
    let Some(anchor) = common_ancestor(&nodes) else {
        return false;
    };
    let _ = split_node(tree, anchor, s.dir, s.fraction, vec![tab]);
    true
}

/// The lowest node above (or at) every node of nodes, in `egui_dock`'s
/// heap-ordered tree. None for an empty slice.
fn common_ancestor(nodes: &[NodeIndex]) -> Option<NodeIndex> {
    let mut it = nodes.iter().copied();
    let mut acc = it.next()?;
    for mut n in it {
        let mut a = acc;
        while a != n {
            if a.0 > n.0 {
                a = a.parent()?;
            } else {
                n = n.parent()?;
            }
        }
        acc = a;
    }
    Some(acc)
}

/// Hide the tab strip of every leaf that holds one tab and that tab is bare,
/// and show it on every other leaf. `egui_dock` keeps the flag per leaf; it is
/// set from the rule each frame, so a tab dragged in beside a bare one brings
/// the strip back.
pub fn hide_bare_tab_bars(state: &mut DockState<u64>, bare: &HashSet<u64>) {
    for (_, node) in state.iter_all_nodes_mut() {
        if let Some(leaf) = node.get_leaf_mut() {
            leaf.tab_bar_hidden = leaf.tabs.len() == 1 && bare.contains(&leaf.tabs[0]);
        }
    }
}

/// Salt of a tab's close id, so it cannot equal the id of a widget the app
/// declares under the dock with the tab's id.
const CLOSE_SALT: u64 = 0x646f_636b_636c_6f73;

/// The r7 id under which a close of `tab` in dock `area_id` is reported. Go
/// derives the same id (`dockTabCloseId` in the bindings).
pub fn dock_tab_close_id(area_id: u64, tab: u64) -> u64 {
    let id = splitmix64(area_id) ^ splitmix64(tab ^ CLOSE_SALT);
    if id == 0 { CLOSE_SALT } else { id }
}

/// The `SplitMix64` finaliser, as the Go bindings' id stack applies it.
fn splitmix64(x: u64) -> u64 {
    let mut z = x.wrapping_add(0x9e37_79b9_7f4a_7c15);
    z = (z ^ (z >> 30)).wrapping_mul(0xbf58_476d_1ce4_e5b9);
    z = (z ^ (z >> 27)).wrapping_mul(0x94d0_49bb_1331_11eb);
    z ^ (z >> 31)
}

/// The tabs of each leaf of the main surface, in node order; for tests and
/// diagnostics.
pub fn main_surface_leaves(state: &DockState<u64>) -> Vec<(usize, Vec<u64>)> {
    state
        .main_surface()
        .iter()
        .enumerate()
        .filter_map(|(i, n)| n.get_leaf().map(|l| (i, l.tabs().to_vec())))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn encode(decl: &DockLayoutDecl) -> Vec<u8> {
        let mut out = vec![1u8];
        out.extend_from_slice(&(decl.root_tabs.len() as u32).to_le_bytes());
        for t in &decl.root_tabs {
            out.extend_from_slice(&t.to_le_bytes());
        }
        out.push(decl.splits.len() as u8);
        for s in &decl.splits {
            out.push(s.parent);
            out.push(s.new_leaf);
            out.push(s.dir);
            out.extend_from_slice(&s.fraction.to_le_bytes());
            out.extend_from_slice(&(s.tabs.len() as u32).to_le_bytes());
            for t in &s.tabs {
                out.extend_from_slice(&t.to_le_bytes());
            }
        }
        out
    }

    // The chat window's declaration: the transcript (1) at the root, a
    // column right of it holding the side views (3, 4, 6), the composer (2)
    // below the transcript only. Declared in that order, the column spans
    // the whole height.
    fn chat_layout() -> Vec<u8> {
        encode(&DockLayoutDecl {
            root_tabs: vec![1],
            splits: vec![
                DockSplitDecl {
                    parent: 0,
                    new_leaf: 1,
                    dir: 3,
                    fraction: 0.6,
                    tabs: vec![3, 4, 6],
                },
                DockSplitDecl {
                    parent: 0,
                    new_leaf: 2,
                    dir: 1,
                    fraction: 0.8,
                    tabs: vec![2],
                },
            ],
        })
    }

    fn frame(
        state: &mut Option<DockState<u64>>,
        layout: &[u8],
        tabs: &[u64],
    ) -> Vec<(usize, Vec<u64>)> {
        let s = state.get_or_insert_with(|| parse_dock_initial_layout(layout, tabs));
        reconcile_dock_tabs(s, tabs, layout);
        main_surface_leaves(s)
    }

    #[test]
    fn declared_but_absent_tabs_leave_no_column() {
        let layout = chat_layout();
        let mut st = None;
        let leaves = frame(&mut st, &layout, &[1, 2]);
        assert_eq!(
            leaves,
            vec![(1, vec![1]), (2, vec![2])],
            "transcript over composer, nothing beside"
        );
    }

    #[test]
    fn a_returning_tab_makes_its_column_again_at_full_height() {
        let layout = chat_layout();
        let mut st = None;
        frame(&mut st, &layout, &[1, 2]);
        let leaves = frame(&mut st, &layout, &[1, 2, 6]);
        // The root is split left/right; the left child is the transcript
        // over the composer, the right the column: the split was made at the
        // node holding both, as declared.
        assert_eq!(leaves, vec![(2, vec![6]), (3, vec![1]), (4, vec![2])]);
        assert!(st.as_ref().unwrap().main_surface()[NodeIndex::root()].is_horizontal());
    }

    #[test]
    fn a_tab_joins_the_leaf_of_its_group_in_front() {
        let layout = chat_layout();
        let mut st = None;
        frame(&mut st, &layout, &[1, 2, 6]);
        let leaves = frame(&mut st, &layout, &[1, 2, 6, 3]);
        assert_eq!(leaves, vec![(2, vec![6, 3]), (3, vec![1]), (4, vec![2])]);
        let s = st.as_ref().unwrap();
        let leaf = s.main_surface()[NodeIndex(2)].get_leaf().unwrap();
        assert_eq!(leaf.active.0, 1, "the tab that appeared is in front");
    }

    #[test]
    fn closing_the_column_keeps_the_split_beside_it() {
        // retain_tabs would move the transcript/composer split node up
        // without its children and lose both.
        let layout = chat_layout();
        let mut st = None;
        frame(&mut st, &layout, &[1, 2, 6]);
        let leaves = frame(&mut st, &layout, &[1, 2]);
        assert_eq!(leaves, vec![(1, vec![1]), (2, vec![2])]);
        let again = frame(&mut st, &layout, &[1, 2, 4]);
        assert_eq!(again, vec![(2, vec![4]), (3, vec![1]), (4, vec![2])]);
    }

    #[test]
    fn a_tab_the_person_moved_stays() {
        let layout = chat_layout();
        let mut st = None;
        frame(&mut st, &layout, &[1, 2, 6]);
        // The person drags 6 into the transcript's leaf.
        let s = st.as_mut().unwrap();
        let path = s.find_tab(&6).unwrap();
        let _ = s.remove_tab(path);
        s.main_surface_mut().push_to_first_leaf(6);
        let before = main_surface_leaves(s);
        let after = frame(&mut st, &layout, &[1, 2, 6]);
        assert_eq!(before, after);
        // A group-mate of 6 joins 6 where the person put it.
        let leaves = frame(&mut st, &layout, &[1, 2, 6, 3]);
        assert!(leaves.iter().any(|(_, t)| t.contains(&6) && t.contains(&3)));
    }

    #[test]
    fn an_undeclared_tab_goes_to_the_first_leaf() {
        let layout = chat_layout();
        let mut st = None;
        frame(&mut st, &layout, &[1, 2]);
        let leaves = frame(&mut st, &layout, &[1, 2, 99]);
        assert_eq!(leaves, vec![(1, vec![1, 99]), (2, vec![2])]);
        // And with no layout at all, every tab shares one leaf.
        let mut plain = None;
        frame(&mut plain, &[], &[1, 2]);
        assert_eq!(frame(&mut plain, &[], &[1, 2, 3]), vec![(0, vec![1, 2, 3])]);
    }

    #[test]
    fn an_empty_split_is_skipped_not_a_panic() {
        let layout = encode(&DockLayoutDecl {
            root_tabs: vec![1],
            splits: vec![DockSplitDecl {
                parent: 0,
                new_leaf: 1,
                dir: 3,
                fraction: 0.5,
                tabs: vec![],
            }],
        });
        let mut st = None;
        assert_eq!(frame(&mut st, &layout, &[1]), vec![(0, vec![1])]);
    }

    #[test]
    fn a_bare_tab_alone_has_no_strip_and_gets_it_back_with_company() {
        let layout = chat_layout();
        let mut st = None;
        frame(&mut st, &layout, &[1, 2, 6]);
        let bare: HashSet<u64> = [1, 2].into_iter().collect();
        let s = st.as_mut().unwrap();
        hide_bare_tab_bars(s, &bare);
        let hidden = |s: &DockState<u64>, tab: u64| {
            s.iter_leaves().find(|(_, l)| l.tabs.contains(&tab)).map(|(_, l)| l.tab_bar_hidden)
        };
        assert_eq!(hidden(s, 1), Some(true));
        assert_eq!(hidden(s, 2), Some(true));
        assert_eq!(
            hidden(s, 6),
            Some(false),
            "a tab that is not bare keeps its strip"
        );
        // A side view dragged in beside the transcript brings the strip back.
        let path = s.find_tab(&6).unwrap();
        let _ = s.remove_tab(path);
        s.main_surface_mut().push_to_first_leaf(6);
        hide_bare_tab_bars(s, &bare);
        assert_eq!(hidden(s, 1), Some(false));
    }

    /// `TestDockTabCloseId_matchesTheHost` in the Go bindings asserts it too.
    const GO_CLOSE_ID_VECTOR: u64 = 0x9c92_ca95_cc49_7b1c;

    #[test]
    fn the_close_id_matches_the_go_bindings() {
        assert_eq!(
            dock_tab_close_id(0x1234_5678_9abc_def0, 3),
            GO_CLOSE_ID_VECTOR
        );
        assert_ne!(dock_tab_close_id(1, 3), dock_tab_close_id(3, 1));
    }

    #[test]
    fn parse_round_trips() {
        let decl = DockLayoutDecl {
            root_tabs: vec![7, 8],
            splits: vec![DockSplitDecl {
                parent: 0,
                new_leaf: 1,
                dir: 2,
                fraction: 0.25,
                tabs: vec![9],
            }],
        };
        assert_eq!(DockLayoutDecl::parse(&encode(&decl)), Some(decl));
        assert_eq!(DockLayoutDecl::parse(&[]), None);
        assert_eq!(DockLayoutDecl::parse(&[2]), None);
    }
}
