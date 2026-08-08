//#![feature(closure_lifetime_binder)]
//#![feature(explicit_tail_calls)]
use crate::fffi::common::{FffiError, FffiResult};
use crate::fffi::io::ImZeroFffiIo;

// Errors produced by the interpreter dispatch. Boundary between FFFI I/O
// (typed-error already) and the previously-panicking interpreter loop:
// graceful EOF (peer closed the pipe) is now distinguishable from a genuine
// I/O failure or an interpreter invariant violation.
#[derive(Debug, thiserror::Error)]
pub enum InterpretError {
    #[error("peer closed pipe (EOF on frame length)")]
    PeerClosed,
    #[error("FFFI error: {0}")]
    Fffi(#[from] FffiError),
    #[error("frame stack underflow during {0}")]
    FrameStackUnderflow(&'static str),
    #[error(
        "function {func_proc_id_raw} consumed beyond frame: {overshoot} B overshot, {consumed} B total"
    )]
    FrameOvershoot {
        func_proc_id_raw: u32,
        consumed: usize,
        overshoot: usize,
    },
}
pub type InterpretResult<T> = Result<T, InterpretError>;

use crate::imzero2::code_view;
use crate::imzero2::debugtools::DebugTools;
use crate::imzero2::enums_out::*;
use crate::imzero2::fenums::*;
use crate::imzero2::image::ImageCache;
use crate::imzero2::scrolling_texture::ScrollingTextureCache;
use crate::imzero2::svgexport::{
    ExportState, ExportStateHandle, LinkZonesHandle, TexturePixelCache, TexturePixelCacheHandle,
};
use crate::imzero2::text_edit_highlight;
use egui_ltreeview::{NodeBuilder, NodeConfig, TreeView};
use std::collections::HashMap;
use std::io::{BufReader, Cursor, Read};

// Payload attached via `egui::UserData` to a `ViewportCommand::Screenshot` issued
// by `requestScreenshotRect`. The screenshot event handler downcasts to this
// struct when a rect crop is requested; `requestScreenshot` (no crop) continues
// to carry a bare `String` path.
pub struct ScreenshotRequest {
    pub path: String,
    pub rect: Option<egui::Rect>,
}

enum NodeCommand<'a, NodeIdType> {
    NodeDir(NodeBuilder<'a, NodeIdType>),
    NodeLeaf(NodeBuilder<'a, NodeIdType>),
    NodeDirClose(usize),
}
// see https://www.reddit.com/r/rust/comments/18os4tg/optionmerge_with_as_a_useful_analogue_to/
fn merge_left<T>(left: Option<T>, right: Option<T>) -> Option<T> {
    match (left, right) {
        (None, None) => None,
        (Some(val1), None) => Some(val1),
        (None, Some(val2)) => Some(val2),
        (Some(val1), Some(_)) => Some(val1),
    }
}
/// Splice `ins` into `text` over the char range `range` — a selection to
/// replace, or an empty range used as a plain insertion point — returning the
/// new caret as a char index just past the inserted text. Char-indexed
/// throughout via egui's TextBuffer (UTF-8-safe); the caller maps egui's
/// CCursorRange to `range`. Backs TextEditFluid.InsertAtCursor (ADR-0063).
fn splice_text_at_cursor(text: &mut String, ins: &str, range: std::ops::Range<usize>) -> usize {
    use egui::TextBuffer as _;
    // egui 0.35 wraps text offsets in the `CharIndex` newtype; convert at the
    // TextBuffer boundary and keep this helper's own arithmetic in plain `usize`.
    use egui::text::CharIndex;
    let start = range.start;
    text.delete_char_range(CharIndex(range.start)..CharIndex(range.end));
    let n = text.insert_text(ins, CharIndex(start));
    start + n
}

#[cfg(test)]
mod insert_at_cursor_tests {
    use super::splice_text_at_cursor;

    #[test]
    fn inserts_at_caret() {
        let mut t = String::from("SELECT  FROM t");
        let caret = splice_text_at_cursor(&mut t, "*", 7..7);
        assert_eq!(t, "SELECT * FROM t");
        assert_eq!(caret, 8);
    }

    #[test]
    fn replaces_selection() {
        let mut t = String::from("SELECT a FROM t");
        // range 7..8 selects the 'a'
        let caret = splice_text_at_cursor(&mut t, "bb", 7..8);
        assert_eq!(t, "SELECT bb FROM t");
        assert_eq!(caret, 9);
    }

    #[test]
    fn appends_at_end() {
        let mut t = String::from("abc");
        let caret = splice_text_at_cursor(&mut t, "XYZ", 3..3);
        assert_eq!(t, "abcXYZ");
        assert_eq!(caret, 6);
    }

    #[test]
    fn char_indexed_not_byte_indexed() {
        // 'é' is one char but two UTF-8 bytes; a byte-indexed splice at 2
        // would land mid-codepoint. Char index 2 is right after "hé".
        let mut t = String::from("héllo");
        let caret = splice_text_at_cursor(&mut t, "!", 2..2);
        assert_eq!(t, "hé!llo");
        assert_eq!(caret, 3);
    }
}
fn color32_from_rgba_u32(v: u32) -> egui::Color32 {
    // Go-side packers (color.Hex, styletokens.AsHex) emit STRAIGHT alpha:
    // 0xRRGGBBAA where R/G/B are unscaled by A. Use from_rgba_unmultiplied
    // so egui pre-multiplies internally. The previous from_rgba_premultiplied
    // silently treated source RGB as already-scaled, leaving non-opaque
    // fills over-saturated (alpha only modulated the background blend).
    let r = ((v >> 24) & 0xff) as u8;
    let g = ((v >> 16) & 0xff) as u8;
    let b = ((v >> 8) & 0xff) as u8;
    let a = (v & 0xff) as u8;
    egui::Color32::from_rgba_unmultiplied(r, g, b, a)
}

// ---------------------------------------------------------------------------
// egui_graphs: per-frame pending lists + retained layout state
// ---------------------------------------------------------------------------
// Go sends the full node/edge set every frame; the `graph` opcode drains
// these pending Vecs and reconciles against the persistent GraphState
// (keyed by widget id in ImZeroFffi.graph_states). Reconciliation adds
// new entries, removes entries that vanished from Go's declaration,
// and updates labels/colors of existing entries in place so the
// egui_graphs library keeps its per-node layout positions.

pub struct GraphNodeData {
    pub id: u64,
    pub label: String,
    pub color: Option<egui::Color32>,
}

pub struct GraphEdgeData {
    pub from: u64,
    pub to: u64,
    pub label: Option<String>,
    pub color: Option<egui::Color32>,
}

// Payload types stored inside the egui_graphs Graph. Node color is also
// mirrored into the egui_graphs Node's own color slot by
// reconcile_graph_state (DefaultNodeShape reads node_props.color(), not the
// payload — see doc/howto/imzero2-graph-node-color.md); edge color is read
// from the payload by PayloadColorEdgeShape.
#[derive(Clone, Debug)]
pub struct GraphNodeUserData {
    pub key: u64,
    pub label: String,
    pub color: Option<egui::Color32>,
}

#[derive(Clone, Debug)]
pub struct GraphEdgeUserData {
    pub label: Option<String>,
    pub color: Option<egui::Color32>,
}

/// One row in the events register — flat representation of
/// egui_graphs::events::Event translated from internal petgraph indices
/// to Go's u64 node/edge keys. `kind` is the discriminator defined by
/// the GRAPH_EV_* constants (1..=11 in v1; Pan/Zoom/NodeMove intentionally
/// skipped — continuous high-volume streams, not the useful subset).
#[derive(Debug, Clone, Copy)]
pub struct GraphEventRecord {
    pub graph_id: u64,
    pub kind: u8,
    pub key_a: u64,
    pub key_b: u64,
}

pub const GRAPH_EV_NODE_CLICK: u8 = 1;
pub const GRAPH_EV_NODE_DOUBLE_CLICK: u8 = 2;
pub const GRAPH_EV_NODE_SELECT: u8 = 3;
pub const GRAPH_EV_NODE_DESELECT: u8 = 4;
pub const GRAPH_EV_NODE_DRAG_START: u8 = 5;
pub const GRAPH_EV_NODE_DRAG_END: u8 = 6;
pub const GRAPH_EV_NODE_HOVER_ENTER: u8 = 7;
pub const GRAPH_EV_NODE_HOVER_LEAVE: u8 = 8;
pub const GRAPH_EV_EDGE_CLICK: u8 = 9;
pub const GRAPH_EV_EDGE_SELECT: u8 = 10;
pub const GRAPH_EV_EDGE_DESELECT: u8 = 11;

// Retained per-widget graph. `graph` owns the egui_graphs Graph (with
// layout positions, drag state, selection). The two HashMaps reverse-
// lookup petgraph indices from Go's stable u64 keys so reconciliation
// avoids scanning. Edges are keyed by (from_u64, to_u64) — multigraphs
// (parallel edges) are not supported in v1.
pub struct GraphState {
    pub graph: egui_graphs::Graph<
        GraphNodeUserData,
        GraphEdgeUserData,
        petgraph::Directed,
        petgraph::stable_graph::DefaultIx,
        egui_graphs::DefaultNodeShape,
        PayloadColorEdgeShape,
    >,
    pub node_idx: std::collections::HashMap<u64, petgraph::stable_graph::NodeIndex>,
    pub edge_idx: std::collections::HashMap<(u64, u64), petgraph::stable_graph::EdgeIndex>,
    /// One-shot fit-to-screen latch. While true the GraphView renders with
    /// fit-to-screen enabled; it latches off once the layout settles so
    /// manual pan/zoom sticks and the view stops rescaling every frame.
    /// Armed on creation and re-armed by resetLayout / fitNow(). See
    /// `graph_fit_this_frame`.
    pub fit_pending: bool,
    /// Frames fitted since the latch was last armed. egui_graphs only knows
    /// the node bounds after it has rendered a frame, so we fit a few
    /// frames before trusting the settle signal — otherwise a deterministic
    /// layout fits once against empty bounds and latches off mis-framed.
    pub fit_frames: u32,
}

pub fn new_graph_state() -> GraphState {
    let sg: petgraph::stable_graph::StableGraph<GraphNodeUserData, GraphEdgeUserData> =
        petgraph::stable_graph::StableGraph::default();
    GraphState {
        graph: egui_graphs::Graph::from(&sg),
        node_idx: std::collections::HashMap::new(),
        edge_idx: std::collections::HashMap::new(),
        // Fit the freshly created graph, then latch off once it settles.
        fit_pending: true,
        fit_frames: 0,
    }
}

// Custom edge shape that respects the per-edge `payload.color` set by
// Go callers via `c.GraphEdge(...).Color(col)`. Wraps egui_graphs'
// `DefaultEdgeShape` for layout/labels and post-processes the returned
// shapes, replacing solid stroke colors and fills with the payload
// color when one is set. Falls back to the default's `current_color`
// (selected vs. inactive widget visuals) when the payload has no
// color, so unstyled edges still pick up the egui theme.
//
// `egui_graphs::DefaultEdgeShape` ignores the edge payload entirely:
// its `current_color` reads `style.fg_stroke.color` from the global
// visuals, so without this wrapper every edge renders in the same
// theme color regardless of what Go set on the payload.
#[derive(Clone, Debug)]
pub struct PayloadColorEdgeShape {
    inner: egui_graphs::DefaultEdgeShape,
    payload_color: Option<egui::Color32>,
}

impl From<egui_graphs::EdgeProps<GraphEdgeUserData>> for PayloadColorEdgeShape {
    fn from(edge: egui_graphs::EdgeProps<GraphEdgeUserData>) -> Self {
        let payload_color = edge.payload.color;
        Self {
            payload_color,
            inner: egui_graphs::DefaultEdgeShape::from(edge),
        }
    }
}

impl<N, Ty, Ix, D> egui_graphs::DisplayEdge<N, GraphEdgeUserData, Ty, Ix, D>
    for PayloadColorEdgeShape
where
    N: Clone,
    Ty: petgraph::EdgeType,
    Ix: petgraph::stable_graph::IndexType,
    D: egui_graphs::DisplayNode<N, GraphEdgeUserData, Ty, Ix>,
{
    fn shapes(
        &mut self,
        start: &egui_graphs::Node<N, GraphEdgeUserData, Ty, Ix, D>,
        end: &egui_graphs::Node<N, GraphEdgeUserData, Ty, Ix, D>,
        ctx: &egui_graphs::DrawContext<'_>,
    ) -> Vec<egui::Shape> {
        let mut shapes = self.inner.shapes(start, end, ctx);
        if let Some(c) = self.payload_color {
            for s in shapes.iter_mut() {
                recolor_edge_shape(s, c);
            }
        }
        shapes
    }
    fn update(&mut self, state: &egui_graphs::EdgeProps<GraphEdgeUserData>) {
        self.payload_color = state.payload.color;
        <egui_graphs::DefaultEdgeShape as egui_graphs::DisplayEdge<
            N,
            GraphEdgeUserData,
            Ty,
            Ix,
            D,
        >>::update(&mut self.inner, state);
    }
    fn is_inside(
        &self,
        start: &egui_graphs::Node<N, GraphEdgeUserData, Ty, Ix, D>,
        end: &egui_graphs::Node<N, GraphEdgeUserData, Ty, Ix, D>,
        pos: egui::Pos2,
    ) -> bool {
        <egui_graphs::DefaultEdgeShape as egui_graphs::DisplayEdge<
            N,
            GraphEdgeUserData,
            Ty,
            Ix,
            D,
        >>::is_inside(&self.inner, start, end, pos)
    }
    fn extra_bounds(
        &self,
        start: &egui_graphs::Node<N, GraphEdgeUserData, Ty, Ix, D>,
        end: &egui_graphs::Node<N, GraphEdgeUserData, Ty, Ix, D>,
    ) -> Option<(egui::Pos2, egui::Pos2)> {
        <egui_graphs::DefaultEdgeShape as egui_graphs::DisplayEdge<
            N,
            GraphEdgeUserData,
            Ty,
            Ix,
            D,
        >>::extra_bounds(&self.inner, start, end)
    }
}

// Walk an `egui::Shape` returned by `DefaultEdgeShape` and overwrite
// stroke / fill / text colors with `c`. The default's
// `EdgeShapeBuilder` produces:
//   - LineSegment       → straight body, color in stroke.color
//   - CubicBezier       → curved/looped body, color in stroke.color
//                         (fill is Color32::default(), transparent)
//   - Path (closed)     → arrow tip via Shape::convex_polygon — color
//                         lives in `fill`; the stroke is default-zero
//   - Text              → label, color baked into the galley
// Recoloring all fills + strokes + override_text_color covers the
// full set without us re-implementing the layout.
fn recolor_edge_shape(s: &mut egui::Shape, c: egui::Color32) {
    use egui::Shape;
    use egui::epaint::ColorMode;
    match s {
        Shape::Vec(v) => {
            for inner in v.iter_mut() {
                recolor_edge_shape(inner, c);
            }
        }
        Shape::LineSegment { stroke, .. } => {
            stroke.color = c;
        }
        Shape::Path(p) => {
            p.stroke.color = ColorMode::Solid(c);
            if p.fill != egui::Color32::TRANSPARENT {
                p.fill = c;
            }
        }
        Shape::CubicBezier(b) => {
            b.stroke.color = ColorMode::Solid(c);
            if b.fill != egui::Color32::TRANSPARENT {
                b.fill = c;
            }
        }
        Shape::QuadraticBezier(b) => {
            b.stroke.color = ColorMode::Solid(c);
            if b.fill != egui::Color32::TRANSPARENT {
                b.fill = c;
            }
        }
        Shape::Circle(circle) => {
            circle.stroke.color = c;
        }
        Shape::Ellipse(e) => {
            e.stroke.color = c;
        }
        Shape::Rect(r) => {
            r.stroke.color = c;
        }
        Shape::Text(t) => {
            t.override_text_color = Some(c);
        }
        Shape::Mesh(_) | Shape::Callback(_) | Shape::Noop => {}
    }
}

// Layout discriminator (matches Go-side GraphLayout* constants):
//   0 = LayoutRandom (default; fast, stable positions, no convergence)
//   1 = LayoutForceDirected<FruchtermanReingold>
//   2 = LayoutForceDirected<FruchtermanReingoldWithCenterGravity>
//   3 = LayoutHierarchical
// Switching at runtime discards the previous layout's state because each
// variant stores a different state type under the same egui id slot.
pub const GRAPH_LAYOUT_RANDOM: u8 = 0;
pub const GRAPH_LAYOUT_FORCE_DIRECTED: u8 = 1;
pub const GRAPH_LAYOUT_FORCE_DIRECTED_CG: u8 = 2;
pub const GRAPH_LAYOUT_HIERARCHICAL: u8 = 3;

/// Overlay any user-supplied FruchtermanReingold parameters onto the
/// persisted layout state in egui ctx memory. Each field only gets
/// written if its matching `_set` flag is true, so callers can tune one
/// parameter at a time without clobbering the simulation's running
/// values for the others. No-op unless `layout_kind` is FR or FR+CG.
#[allow(clippy::too_many_arguments)]
pub fn apply_fr_overrides(
    ui: &mut egui::Ui,
    gid: u64,
    layout_kind: u8,
    dt: f32,
    dt_set: bool,
    damping: f32,
    damping_set: bool,
    epsilon: f32,
    epsilon_set: bool,
    max_step: f32,
    max_step_set: bool,
    k_scale: f32,
    k_scale_set: bool,
    c_attract: f32,
    c_attract_set: bool,
    c_repulse: f32,
    c_repulse_set: bool,
    is_running: bool,
    is_running_set: bool,
) {
    let any_set = dt_set
        || damping_set
        || epsilon_set
        || max_step_set
        || k_scale_set
        || c_attract_set
        || c_repulse_set
        || is_running_set;
    if !any_set {
        return;
    }
    let id = Some(gid.to_string());
    match layout_kind {
        GRAPH_LAYOUT_FORCE_DIRECTED => {
            let mut s: egui_graphs::FruchtermanReingoldState =
                egui_graphs::get_layout_state(ui, id.clone());
            if dt_set {
                s.dt = dt;
            }
            if damping_set {
                s.damping = damping;
            }
            if epsilon_set {
                s.epsilon = epsilon;
            }
            if max_step_set {
                s.max_step = max_step;
            }
            if k_scale_set {
                s.k_scale = k_scale;
            }
            if c_attract_set {
                s.c_attract = c_attract;
            }
            if c_repulse_set {
                s.c_repulse = c_repulse;
            }
            if is_running_set {
                s.is_running = is_running;
            }
            egui_graphs::set_layout_state(ui, s, id);
        }
        GRAPH_LAYOUT_FORCE_DIRECTED_CG => {
            // The center-gravity variant wraps a base FruchtermanReingoldState
            // inside FruchtermanReingoldWithExtrasState; reach it via `.base`.
            let mut s: egui_graphs::FruchtermanReingoldWithCenterGravityState =
                egui_graphs::get_layout_state(ui, id.clone());
            if dt_set {
                s.base.dt = dt;
            }
            if damping_set {
                s.base.damping = damping;
            }
            if epsilon_set {
                s.base.epsilon = epsilon;
            }
            if max_step_set {
                s.base.max_step = max_step;
            }
            if k_scale_set {
                s.base.k_scale = k_scale;
            }
            if c_attract_set {
                s.base.c_attract = c_attract;
            }
            if c_repulse_set {
                s.base.c_repulse = c_repulse;
            }
            if is_running_set {
                s.base.is_running = is_running;
            }
            egui_graphs::set_layout_state(ui, s, id);
        }
        _ => {}
    }
}

/// Overlay any user-supplied Hierarchical parameters onto the persisted
/// hierarchical layout state. Orientation: 0 = TopDown (default), 1 =
/// LeftRight. No-op unless `layout_kind` is Hierarchical.
#[allow(clippy::too_many_arguments)]
pub fn apply_hierarchical_overrides(
    ui: &mut egui::Ui,
    gid: u64,
    layout_kind: u8,
    row_dist: f32,
    row_dist_set: bool,
    col_dist: f32,
    col_dist_set: bool,
    center_parent: bool,
    center_parent_set: bool,
    orientation: u8,
    orientation_set: bool,
) {
    if layout_kind != GRAPH_LAYOUT_HIERARCHICAL {
        return;
    }
    let any_set = row_dist_set || col_dist_set || center_parent_set || orientation_set;
    if !any_set {
        return;
    }
    let id = Some(gid.to_string());
    let mut s: egui_graphs::LayoutStateHierarchical = egui_graphs::get_layout_state(ui, id.clone());
    if row_dist_set {
        s.row_dist = row_dist;
    }
    if col_dist_set {
        s.col_dist = col_dist;
    }
    if center_parent_set {
        s.center_parent = center_parent;
    }
    if orientation_set {
        s.orientation = match orientation {
            1 => egui_graphs::LayoutHierarchicalOrientation::LeftRight,
            _ => egui_graphs::LayoutHierarchicalOrientation::TopDown,
        };
    }
    egui_graphs::set_layout_state(ui, s, id);
}

/// Append every currently-selected node and edge in `state` onto the
/// shared snapshot vectors on the interpreter, tagged with `gid`. Nodes
/// surface as (kind=0, keyA=node_id, keyB=0); edges as (kind=1,
/// keyA=from, keyB=to). Called once per graph per frame.
pub fn snapshot_graph_selection(
    graph_id: u64,
    state: &GraphState,
    out_graph_ids: &mut Vec<u64>,
    out_kind: &mut Vec<u8>,
    out_key_a: &mut Vec<u64>,
    out_key_b: &mut Vec<u64>,
) {
    for (_, node) in state.graph.nodes_iter() {
        if node.selected() {
            out_graph_ids.push(graph_id);
            out_kind.push(0);
            out_key_a.push(node.payload().key);
            out_key_b.push(0);
        }
    }
    for (ei, edge) in state.graph.edges_iter() {
        if edge.selected() {
            if let Some((a, b)) = state.graph.edge_endpoints(ei) {
                let ka = state.graph.node(a).map(|n| n.payload().key).unwrap_or(0);
                let kb = state.graph.node(b).map(|n| n.payload().key).unwrap_or(0);
                out_graph_ids.push(graph_id);
                out_kind.push(1);
                out_key_a.push(ka);
                out_key_b.push(kb);
            }
        }
    }
}

/// Append one metrics row for `state` onto the shared snapshot vectors.
/// `fr_steps` and `fr_last_disp` are meaningful only when the layout
/// stored in egui memory is an FR variant; otherwise they're 0 / NaN.
pub fn snapshot_graph_metrics(
    graph_id: u64,
    layout_kind: u8,
    state: &GraphState,
    ui: &egui::Ui,
    out_graph_ids: &mut Vec<u64>,
    out_node_count: &mut Vec<u32>,
    out_edge_count: &mut Vec<u32>,
    out_fr_steps: &mut Vec<u64>,
    out_fr_last_disp: &mut Vec<f32>,
) {
    let id = Some(graph_id.to_string());
    let (fr_steps, fr_last_disp) = match layout_kind {
        GRAPH_LAYOUT_FORCE_DIRECTED => {
            let s: egui_graphs::FruchtermanReingoldState = egui_graphs::get_layout_state(ui, id);
            (s.step_count, s.last_avg_displacement.unwrap_or(f32::NAN))
        }
        GRAPH_LAYOUT_FORCE_DIRECTED_CG => {
            let s: egui_graphs::FruchtermanReingoldWithCenterGravityState =
                egui_graphs::get_layout_state(ui, id);
            (
                s.base.step_count,
                s.base.last_avg_displacement.unwrap_or(f32::NAN),
            )
        }
        _ => (0, f32::NAN),
    };
    out_graph_ids.push(graph_id);
    out_node_count.push(state.graph.g().node_count() as u32);
    out_edge_count.push(state.graph.g().edge_count() as u32);
    out_fr_steps.push(fr_steps);
    out_fr_last_disp.push(fr_last_disp);
}

/// True once the graph's layout has stopped moving enough to latch the
/// one-shot fit off. Deterministic layouts (random / hierarchical) are
/// settled immediately; force-directed layouts settle once they have
/// taken at least one step and their average per-step displacement has
/// fallen to/under the convergence epsilon. Reads the layout state that
/// egui_graphs persists in `ui` memory, so it reflects the previous
/// frame's progress — exactly what we need to decide this frame's fit.
pub fn graph_layout_settled(ui: &egui::Ui, graph_id: u64, layout_kind: u8) -> bool {
    let id = Some(graph_id.to_string());
    match layout_kind {
        GRAPH_LAYOUT_FORCE_DIRECTED => {
            let s: egui_graphs::FruchtermanReingoldState = egui_graphs::get_layout_state(ui, id);
            s.step_count > 0 && s.last_avg_displacement.map_or(false, |d| d <= s.epsilon)
        }
        GRAPH_LAYOUT_FORCE_DIRECTED_CG => {
            let s: egui_graphs::FruchtermanReingoldWithCenterGravityState =
                egui_graphs::get_layout_state(ui, id);
            s.base.step_count > 0
                && s.base.last_avg_displacement.map_or(false, |d| d <= s.base.epsilon)
        }
        _ => true,
    }
}

/// Decide whether to fit-to-screen this frame and advance the one-shot
/// fit latch on `state`. `continuous` forces the legacy always-fit
/// behaviour. `refit` (creation / resetLayout / fitNow) re-arms the latch
/// and resets the frame counter, so a stale pre-reset settle signal can't
/// latch us off early. Otherwise we keep fitting while the latch is pending
/// until the layout has both `settled` and been fitted for a floor of
/// frames, then latch off.
pub fn graph_fit_this_frame(
    state: &mut GraphState,
    continuous: bool,
    refit: bool,
    settled: bool,
) -> bool {
    // egui_graphs only knows the node bounds after it has rendered a frame,
    // so a fit applied on the very first frame frames against empty bounds
    // and — because we latch off — never corrects. Fit for a floor of
    // frames before trusting `settled`. Deterministic layouts (random /
    // hierarchical) report settled immediately, so this floor is what
    // actually frames them; force-directed layouts stay unsettled well
    // past it, so it never shortens their settle.
    const GRAPH_FIT_MIN_FRAMES: u32 = 8;
    if continuous {
        return true;
    }
    if refit {
        state.fit_pending = true;
        state.fit_frames = 0;
    }
    if !state.fit_pending {
        return false;
    }
    state.fit_frames = state.fit_frames.saturating_add(1);
    if settled && state.fit_frames >= GRAPH_FIT_MIN_FRAMES {
        state.fit_pending = false;
    }
    true
}

/// Render a GraphView with the layout variant picked by `layout_kind`.
/// Extracted from the `graph` apply code so the match-over-kind stays
/// co-located with the other graph helpers. All generic bounds are
/// instantiated here — the caller passes only runtime values.
pub fn render_graph_with_layout(
    state: &mut GraphState,
    ui: &mut egui::Ui,
    size: egui::Vec2,
    gid: u64,
    layout_kind: u8,
    reset_layout_flag: bool,
    fast_forward_steps: u32,
    interaction: &egui_graphs::SettingsInteraction,
    navigation: &egui_graphs::SettingsNavigation,
    style: &egui_graphs::SettingsStyle,
    sink: &dyn egui_graphs::events::EventSink,
) -> egui::Response {
    // Each arm: (optional) reset_layout → (optional) fast_forward → add_sized.
    // The final `add_sized` is the tail expression of every arm, so the match
    // (and this function) yields the GraphView's Response — the caller reads
    // its `contains_pointer()` to decide whether to swallow the wheel.
    // The inner GraphView generic alias keeps each arm readable; macros were
    // an option but hide the type params that matter for maintenance here.
    let id = Some(gid.to_string());
    macro_rules! render_variant {
        ($S:ty, $L:ty) => {{
            if reset_layout_flag {
                egui_graphs::reset_layout::<$S>(ui, id.clone());
            }
            if fast_forward_steps > 0 {
                egui_graphs::GraphView::<
                    GraphNodeUserData,
                    GraphEdgeUserData,
                    petgraph::Directed,
                    petgraph::stable_graph::DefaultIx,
                    egui_graphs::DefaultNodeShape,
                    PayloadColorEdgeShape,
                    $S,
                    $L,
                >::fast_forward(ui, &mut state.graph, fast_forward_steps, id.clone());
            }
            let mut view: egui_graphs::GraphView<
                '_,
                GraphNodeUserData,
                GraphEdgeUserData,
                petgraph::Directed,
                petgraph::stable_graph::DefaultIx,
                egui_graphs::DefaultNodeShape,
                PayloadColorEdgeShape,
                $S,
                $L,
            > = egui_graphs::GraphView::new(&mut state.graph)
                .with_id(id.clone())
                .with_interactions(interaction)
                .with_navigations(navigation)
                .with_styles(style)
                .with_event_sink(sink);
            ui.add_sized(size, &mut view)
        }};
    }
    match layout_kind {
        GRAPH_LAYOUT_FORCE_DIRECTED => render_variant!(
            egui_graphs::FruchtermanReingoldState,
            egui_graphs::LayoutForceDirected<egui_graphs::FruchtermanReingold>
        ),
        GRAPH_LAYOUT_FORCE_DIRECTED_CG => render_variant!(
            egui_graphs::FruchtermanReingoldWithCenterGravityState,
            egui_graphs::LayoutForceDirected<egui_graphs::FruchtermanReingoldWithCenterGravity>
        ),
        GRAPH_LAYOUT_HIERARCHICAL => render_variant!(
            egui_graphs::LayoutStateHierarchical,
            egui_graphs::LayoutHierarchical
        ),
        _ /* GRAPH_LAYOUT_RANDOM */ => render_variant!(
            egui_graphs::LayoutStateRandom,
            egui_graphs::LayoutRandom
        ),
    }
}

/// Translate an `egui_graphs::events::Event` into the flat GraphEventRecord
/// that the FFFI register expects. Returns `None` for variants we don't
/// surface to Go in v1 (Pan, Zoom, NodeMove — continuous streams). Node
/// index → u64 key goes through the Node's user-data payload; edge index
/// → (from, to) goes through StableGraph::edge_endpoints.
pub fn translate_graph_event(
    graph_id: u64,
    state: &GraphState,
    e: &egui_graphs::events::Event,
) -> Option<GraphEventRecord> {
    use egui_graphs::events::Event as E;
    let node_key = |idx: usize| -> Option<u64> {
        let ni = petgraph::stable_graph::NodeIndex::new(idx);
        state.graph.node(ni).map(|n| n.payload().key)
    };
    let edge_pair = |idx: usize| -> Option<(u64, u64)> {
        let ei = petgraph::stable_graph::EdgeIndex::new(idx);
        let (a, b) = state.graph.edge_endpoints(ei)?;
        let ka = state.graph.node(a)?.payload().key;
        let kb = state.graph.node(b)?.payload().key;
        Some((ka, kb))
    };
    let mk_node = |kind: u8, idx: usize| -> Option<GraphEventRecord> {
        node_key(idx).map(|k| GraphEventRecord {
            graph_id,
            kind,
            key_a: k,
            key_b: 0,
        })
    };
    let mk_edge = |kind: u8, idx: usize| -> Option<GraphEventRecord> {
        edge_pair(idx).map(|(a, b)| GraphEventRecord {
            graph_id,
            kind,
            key_a: a,
            key_b: b,
        })
    };
    match e {
        E::NodeClick(p) => mk_node(GRAPH_EV_NODE_CLICK, p.id),
        E::NodeDoubleClick(p) => mk_node(GRAPH_EV_NODE_DOUBLE_CLICK, p.id),
        E::NodeSelect(p) => mk_node(GRAPH_EV_NODE_SELECT, p.id),
        E::NodeDeselect(p) => mk_node(GRAPH_EV_NODE_DESELECT, p.id),
        E::NodeDragStart(p) => mk_node(GRAPH_EV_NODE_DRAG_START, p.id),
        E::NodeDragEnd(p) => mk_node(GRAPH_EV_NODE_DRAG_END, p.id),
        E::NodeHoverEnter(p) => mk_node(GRAPH_EV_NODE_HOVER_ENTER, p.id),
        E::NodeHoverLeave(p) => mk_node(GRAPH_EV_NODE_HOVER_LEAVE, p.id),
        E::EdgeClick(p) => mk_edge(GRAPH_EV_EDGE_CLICK, p.id),
        E::EdgeSelect(p) => mk_edge(GRAPH_EV_EDGE_SELECT, p.id),
        E::EdgeDeselect(p) => mk_edge(GRAPH_EV_EDGE_DESELECT, p.id),
        // Pan, Zoom, NodeMove — continuous; intentionally not surfaced in v1.
        _ => None,
    }
}

/// Deterministic spawn location for a newly declared graph node. The default
/// add path (`add_node_with_label` → `Node::new`) places every new node at
/// `Pos2::default()`, i.e. exactly (0,0), and FR repulsion between exactly
/// coincident nodes is zero (`dir = delta/distance` with `delta == 0`) — so
/// nodes with identical neighbour sets receive identical net forces every step
/// and stay stacked forever. Scattering spawns over a disc breaks the tie;
/// hashing the Go-side key keeps the position stable across frames and
/// sessions instead of depending on insertion order. See
/// doc/howto/imzero2-graph-coincident-spawn.md for the full analysis.
fn graph_spawn_location(key: u64) -> egui::Pos2 {
    // splitmix64 finalizer — cheap, well distributed, no rand dependency.
    let mut z = key.wrapping_add(0x9e37_79b9_7f4a_7c15);
    z = (z ^ (z >> 30)).wrapping_mul(0xbf58_476d_1ce4_e5b9);
    z = (z ^ (z >> 27)).wrapping_mul(0x94d0_49bb_1331_11eb);
    z ^= z >> 31;
    let angle = ((z as u32) as f32 / u32::MAX as f32) * std::f32::consts::TAU;
    let radius = 30.0 + (((z >> 32) as u32) as f32 / u32::MAX as f32) * 120.0;
    egui::Pos2::new(radius * angle.cos(), radius * angle.sin())
}

pub fn reconcile_graph_state(
    state: &mut GraphState,
    pending_nodes: &[GraphNodeData],
    pending_edges: &[GraphEdgeData],
) {
    use std::collections::HashSet;

    // Remove nodes Go no longer declares + their incident edges.
    let wanted_nodes: HashSet<u64> = pending_nodes.iter().map(|n| n.id).collect();
    let stale_nodes: Vec<u64> =
        state.node_idx.keys().copied().filter(|k| !wanted_nodes.contains(k)).collect();
    for k in &stale_nodes {
        if let Some(idx) = state.node_idx.remove(k) {
            // petgraph::StableGraph::remove_node also drops all edges
            // incident to the removed node, so the EdgeIndex entries
            // in state.edge_idx for those edges become stale — we
            // filter them out below.
            state.graph.remove_node(idx);
        }
    }
    if !stale_nodes.is_empty() {
        let stale_set: HashSet<u64> = stale_nodes.into_iter().collect();
        state.edge_idx.retain(|(f, t), _| !stale_set.contains(f) && !stale_set.contains(t));
    }

    // Add new nodes / update existing. The Go-side color must be mirrored
    // into the egui_graphs Node's own color slot: DefaultNodeShape renders
    // node_props.color(), not the payload, so a payload-only color is
    // silently dropped and every node falls back to the theme stroke color
    // (doc/howto/imzero2-graph-node-color.md). A declaration without a color
    // leaves the previous slot value — Go apps either always or never color
    // a given graph's nodes.
    for n in pending_nodes {
        if let Some(&idx) = state.node_idx.get(&n.id) {
            if let Some(node) = state.graph.node_mut(idx) {
                let p = node.payload_mut();
                p.label = n.label.clone();
                p.color = n.color;
                if let Some(col) = n.color {
                    node.set_color(col);
                }
            }
        } else {
            let payload = GraphNodeUserData {
                key: n.id,
                label: n.label.clone(),
                color: n.color,
            };
            let idx = state.graph.add_node_with_label_and_location(
                payload,
                n.label.clone(),
                graph_spawn_location(n.id),
            );
            if let Some(col) = n.color {
                if let Some(node) = state.graph.node_mut(idx) {
                    node.set_color(col);
                }
            }
            state.node_idx.insert(n.id, idx);
        }
    }

    // Remove edges Go no longer declares.
    let wanted_edges: HashSet<(u64, u64)> = pending_edges.iter().map(|e| (e.from, e.to)).collect();
    let stale_edges: Vec<(u64, u64)> =
        state.edge_idx.keys().copied().filter(|k| !wanted_edges.contains(k)).collect();
    for k in stale_edges {
        if let Some(idx) = state.edge_idx.remove(&k) {
            state.graph.remove_edge(idx);
        }
    }

    // Add new edges / update existing.
    for e in pending_edges {
        if let Some(&idx) = state.edge_idx.get(&(e.from, e.to)) {
            if let Some(edge) = state.graph.edge_mut(idx) {
                let p = edge.payload_mut();
                p.label = e.label.clone();
                p.color = e.color;
            }
            continue;
        }
        let a = match state.node_idx.get(&e.from) {
            Some(x) => *x,
            None => continue,
        };
        let b = match state.node_idx.get(&e.to) {
            Some(x) => *x,
            None => continue,
        };
        let payload = GraphEdgeUserData {
            label: e.label.clone(),
            color: e.color,
        };
        let idx = match e.label.as_ref() {
            Some(lbl) => state.graph.add_edge_with_label(a, b, payload, lbl.clone()),
            None => state.graph.add_edge(a, b, payload),
        };
        state.edge_idx.insert((e.from, e.to), idx);
    }
}

// Painter drawing commands (accumulated via register-drain pattern)
// All coordinates are relative to canvas origin; translated at render time.
pub enum PaintCmd {
    CircleFilled {
        cx: f32,
        cy: f32,
        radius: f32,
        fill: egui::Color32,
    },
    CircleStroke {
        cx: f32,
        cy: f32,
        radius: f32,
        stroke: egui::Stroke,
    },
    RectFilled {
        min_x: f32,
        min_y: f32,
        max_x: f32,
        max_y: f32,
        rounding: f32,
        fill: egui::Color32,
    },
    RectStroke {
        min_x: f32,
        min_y: f32,
        max_x: f32,
        max_y: f32,
        rounding: f32,
        stroke: egui::Stroke,
    },
    Line {
        from_x: f32,
        from_y: f32,
        to_x: f32,
        to_y: f32,
        stroke: egui::Stroke,
    },
    DashedLine {
        from_x: f32,
        from_y: f32,
        to_x: f32,
        to_y: f32,
        dash_len: f32,
        gap_len: f32,
        stroke: egui::Stroke,
    },
    Arrow {
        ox: f32,
        oy: f32,
        dx: f32,
        dy: f32,
        stroke: egui::Stroke,
    },
    Text {
        px: f32,
        py: f32,
        anchor_h: u8,
        anchor_v: u8,
        text: String,
        font_size: f32,
        color: egui::Color32,
        monospace: bool,
    },
    Polyline {
        points: Vec<[f32; 2]>,
        stroke: egui::Stroke,
    },
    PolygonFilled {
        points: Vec<[f32; 2]>,
        fill: egui::Color32,
        stroke: egui::Stroke,
        concave: bool,
    },
    EllipseFilled {
        cx: f32,
        cy: f32,
        rx: f32,
        ry: f32,
        fill: egui::Color32,
    },
    EllipseStroke {
        cx: f32,
        cy: f32,
        rx: f32,
        ry: f32,
        stroke: egui::Stroke,
    },
    CubicBezier {
        x0: f32,
        y0: f32,
        x1: f32,
        y1: f32,
        x2: f32,
        y2: f32,
        x3: f32,
        y3: f32,
        stroke: egui::Stroke,
    },
    SenseRegion {
        id: egui::Id,
        px: f32,
        py: f32,
        sw: f32,
        sh: f32,
    },
    ClipPush {
        min_x: f32,
        min_y: f32,
        max_x: f32,
        max_y: f32,
    },
    ClipPop,
    Markers {
        xs: Vec<f32>,
        ys: Vec<f32>,
        shape: u8,
        radius: f32,
        color: egui::Color32,
        weight: f32,
    },
    RectsFilled {
        min_xs: Vec<f32>,
        min_ys: Vec<f32>,
        max_xs: Vec<f32>,
        max_ys: Vec<f32>,
        cols: Vec<u32>,
    },
    Image {
        id: u64,
        min_x: f32,
        min_y: f32,
        max_x: f32,
        max_y: f32,
        width_px: u32,
        height_px: u32,
        content_version: u64,
        pixels: Vec<u32>,
        nearest: bool,
        opacity: f32,
    },
}

// ===========================================================================
// WALKERS (slippy map) binding — pending data, retained state, overlay plugin
// ===========================================================================
// Companion to egui2_definition_d_walkers.go. Per-widget state (HttpTiles +
// MapMemory + h3Region outline cache) lives in `walkers_states` keyed by
// widget id and persists across frames. Per-frame overlays
// (markers, polylines, choropleth, regions) accumulate in the
// `walkers_pending_*` Vecs via register-drain and are drained by the
// `walkersMap` opcode each frame.

pub struct WalkersMarker {
    pub id: u64,
    pub lat: f64,
    pub lon: f64,
    pub label: Option<String>,
    pub color: Option<egui::Color32>,
    pub radius_px: f32,
}

pub struct WalkersPolyline {
    pub lats: Vec<f64>,
    pub lons: Vec<f64>,
    pub stroke: egui::Stroke,
    pub closed: bool,
}

pub struct H3Choropleth {
    pub cell_ids: Vec<u64>,
    pub rgbas: Vec<u32>,
    pub stroke_width: f32,
    pub stroke_color: egui::Color32,
}

pub struct H3Region {
    pub cell_ids: Vec<u64>,
    pub fill: Option<egui::Color32>,
    pub stroke: Option<egui::Stroke>,
    pub label: Option<String>,
}

// One in-DB-rendered geo raster pinned to a lat/lon bbox (ADR-0096). Drained
// by walkersMap into an upload + a WalkersRasterRenderable; the texture is
// cached by (rasterId, content_version) in `walkers_raster_cache`, so a
// version-unchanged frame ships an empty `pixels` and re-uses the cached GPU
// texture. Pixels are 0xRRGGBBAA, row-major, row 0 = north.
pub struct WalkersRaster {
    pub id: u64,
    pub min_lat: f64,
    pub min_lon: f64,
    pub max_lat: f64,
    pub max_lon: f64,
    pub width_px: u32,
    pub height_px: u32,
    pub content_version: u64,
    pub pixels: Vec<u32>,
    pub opacity: f32,
    pub nearest: bool,
}

// Pre-projected renderable form of an H3Choropleth: per-cell boundary in
// lat/lon + the color. Computed once per frame in render_walkers_map,
// projected per-frame inside the plugin.
pub struct H3ChoroplethRenderable {
    pub cells: Vec<Vec<walkers::Position>>, // one Vec per cell, 5 or 6 vertices
    pub rgbas: Vec<u32>,
    pub stroke_width: f32,
    pub stroke_color: egui::Color32,
}

// Pre-aggregated renderable form of an H3Region: the dissolved multipolygon
// rings in lat/lon, plus the styling. Rings are in insertion order — no
// exterior/interior distinction is needed here because we draw closed_line
// strokes per ring regardless. The `fill` path re-uses the raw cell list
// (drawn as individual hexes) to sidestep concave-polygon tessellation.
pub struct H3RegionRenderable {
    pub outline_rings: Vec<Vec<walkers::Position>>,
    pub cell_boundaries: Vec<Vec<walkers::Position>>, // one Vec per source cell
    pub fill: Option<egui::Color32>,
    pub stroke: Option<egui::Stroke>,
    pub label: Option<String>,
    pub label_position: Option<walkers::Position>, // centroid of first ring
}

// Uploaded form of a WalkersRaster: the texture id plus the two corner
// Positions (NW = min_lon/max_lat, SE = max_lon/min_lat). The plugin projects
// the corners and draws one textured quad — the raster is mercator-uniform, so
// a single stretched quad is geometrically exact (ADR-0096 SD5).
pub struct WalkersRasterRenderable {
    pub nw: walkers::Position,
    pub se: walkers::Position,
    pub tex: egui::TextureId,
    pub opacity: f32,
}

// CustomTileSource, its attribution leak, and the boxer-owned Tiles
// implementation live in imzero2::walkers_tiles.
use crate::imzero2::walkers_tiles::{BasemapTiles, TileTlsConfig, make_custom_tile_source};
use std::sync::Arc;

// Stable signature of a tile config so we can detect changes across frames
// and rebuild the tile source when the user switches tile servers. The TLS
// pair is part of it: flipping either knob has to rebuild, since the HTTP
// client is created once per tile source.
pub fn tile_config_signature(
    url_template: &str,
    attribution: &str,
    attribution_url: &str,
    tile_size: u32,
    max_zoom: u8,
    no_tiles: bool,
    tls: &TileTlsConfig,
) -> u64 {
    use std::collections::hash_map::DefaultHasher;
    use std::hash::{Hash, Hasher};
    let mut h = DefaultHasher::new();
    url_template.hash(&mut h);
    attribution.hash(&mut h);
    attribution_url.hash(&mut h);
    tile_size.hash(&mut h);
    max_zoom.hash(&mut h);
    no_tiles.hash(&mut h);
    tls.ca_file.hash(&mut h);
    tls.insecure.hash(&mut h);
    h.finish()
}

// Per-widget retained state. The tile source is `None` when the user
// constructed the map with `NoTiles()`, and otherwise a BasemapTiles wrapping
// whichever TileSource the config selected. The outline
// cache is keyed by a hash of the sorted cell set; it lets us skip dissolve
// work when a region's cell set doesn't change between frames.
// `tile_signature` tracks the current tile config so render_walkers_map can
// rebuild the tile source on change.
pub struct WalkersState {
    pub tiles: Option<BasemapTiles>,
    pub memory: walkers::MapMemory,
    pub region_outline_cache: std::collections::HashMap<u64, Vec<Vec<walkers::Position>>>,
    pub tile_signature: u64,
}

pub fn new_walkers_state(
    ctx: &egui::Context,
    url_template: &str,
    attribution: &str,
    attribution_url: &str,
    tile_size: u32,
    max_zoom: u8,
    no_tiles: bool,
    tls: &TileTlsConfig,
) -> WalkersState {
    let tiles = build_walkers_tiles(
        ctx,
        url_template,
        attribution,
        attribution_url,
        tile_size,
        max_zoom,
        no_tiles,
        tls,
    );
    WalkersState {
        tiles,
        memory: walkers::MapMemory::default(),
        region_outline_cache: std::collections::HashMap::new(),
        tile_signature: tile_config_signature(
            url_template,
            attribution,
            attribution_url,
            tile_size,
            max_zoom,
            no_tiles,
            tls,
        ),
    }
}

// Construct the tile source according to the supplied tile config.
// no_tiles → None (no tiles drawn); empty url template → built-in OSM source;
// non-empty → CustomTileSource, fetched by walkers' own client unless the TLS
// knobs are set, which walkers' client cannot honour (see walkers_tiles).
pub fn build_walkers_tiles(
    ctx: &egui::Context,
    url_template: &str,
    attribution: &str,
    attribution_url: &str,
    tile_size: u32,
    max_zoom: u8,
    no_tiles: bool,
    tls: &TileTlsConfig,
) -> Option<BasemapTiles> {
    if no_tiles {
        return None;
    }
    // Both branches build the same BasemapTiles; only the source differs.
    // walkers' own HttpTiles is never constructed — see walkers_tiles for why
    // one client beats two selected by an env knob.
    let no_tls = TileTlsConfig::default();
    let (source, effective_tls): (
        Arc<dyn walkers::sources::TileSource + Send + Sync>,
        &TileTlsConfig,
    ) = if url_template.is_empty() {
        // Reached only by a walkersMap built without .TileUrl — the widget
        // demo, not the basemap package, which always sends a URL. walkers'
        // own OpenStreetMap source supplies the endpoint and its
        // attribution, so nothing about the default is restated here.
        //
        // TLS settings are dropped rather than applied: this is the public
        // OpenStreetMap server under a publicly-issued certificate, and Go
        // gates the knobs on an explicitly-set URL for exactly this reason.
        if tls.is_configured() {
            tracing::warn!("tile TLS configuration ignored: it applies only to a custom tile URL");
        }
        (Arc::new(walkers::sources::OpenStreetMap), &no_tls)
    } else {
        let source = make_custom_tile_source(
            url_template.to_owned(),
            attribution,
            attribution_url,
            tile_size,
            max_zoom,
        );
        (Arc::new(source), tls)
    };

    // Logged here rather than deeper down because this is the one place that
    // has the URL, the config and a once-per-tile-config-change cadence. An
    // operator turning verification off should find it in the log.
    if effective_tls.insecure {
        if !effective_tls.ca_file.is_empty() {
            tracing::warn!(
                ca_file = %effective_tls.ca_file,
                "tile CA bundle ignored — certificate verification is disabled"
            );
        }
        tracing::warn!(
            url = %url_template,
            "TLS certificate verification is DISABLED for this tile server; any certificate is accepted"
        );
    }
    Some(BasemapTiles::new(source, effective_tls, ctx))
}

// Fetcher-facing snapshot of the last rendered walkersMap's viewport +
// pointer state. Written by OverlayPlugin::run, read (and taken) by
// fetchR15WalkersCamera.
pub struct WalkersCamera {
    pub map_id: u64,
    pub zoom: f64,
    pub center_lat: f64,
    pub center_lon: f64,
    pub min_lat: f64,
    pub min_lon: f64,
    pub max_lat: f64,
    pub max_lon: f64,
    pub screen_width_px: f32,
    pub screen_height_px: f32,
    pub hover_lat: f64,
    pub hover_lon: f64,
    pub hover_valid: bool,
    pub clicked: bool,
    pub view_hash: u64,
}

// Compute a stable quantized hash of the viewport (Go-facing key for cache
// invalidation). Quantization: 1e-6° (~0.1m) on lat/lon, 1/100 on zoom,
// 1 px on size. Imperceptibly-small camera jitter produces the same hash
// so Go's heatmap cache doesn't thrash.
pub fn compute_view_hash(center_lat: f64, center_lon: f64, zoom: f64, w: f32, h: f32) -> u64 {
    use std::collections::hash_map::DefaultHasher;
    use std::hash::{Hash, Hasher};
    let mut hasher = DefaultHasher::new();
    ((center_lat * 1_000_000.0).round() as i64).hash(&mut hasher);
    ((center_lon * 1_000_000.0).round() as i64).hash(&mut hasher);
    ((zoom * 100.0).round() as i64).hash(&mut hasher);
    (w as i64).hash(&mut hasher);
    (h as i64).hash(&mut hasher);
    hasher.finish()
}

// Hash a cell set (order-insensitive) into a stable key for the outline
// cache. Uses a commutative accumulator (xor of hashed cells) so caller
// doesn't need to sort.
pub fn hash_cell_set(cells: &[u64]) -> u64 {
    use std::collections::hash_map::DefaultHasher;
    use std::hash::{Hash, Hasher};
    let mut acc: u64 = 0x9e3779b97f4a7c15;
    for &c in cells {
        let mut hasher = DefaultHasher::new();
        c.hash(&mut hasher);
        acc ^= hasher.finish();
    }
    acc
}

// Compute the lat/lon boundary of a single H3 cell. Returns None if the
// cell_id doesn't parse as a valid H3 index.
pub fn cell_boundary_positions(cell_id: u64) -> Option<Vec<walkers::Position>> {
    let cell = h3o::CellIndex::try_from(cell_id).ok()?;
    let boundary = cell.boundary();
    let positions: Vec<walkers::Position> =
        boundary.iter().map(|ll| walkers::lon_lat(ll.lng(), ll.lat())).collect();
    Some(positions)
}

// Pre-project cell boundaries for a choropleth layer. Each cell that
// fails to parse as an H3 index is silently dropped (along with its color
// entry). Length mismatch between cells and rgbas is tolerated; we zip
// to the shorter.
pub fn prerender_choropleth(src: H3Choropleth) -> H3ChoroplethRenderable {
    let n = src.cell_ids.len().min(src.rgbas.len());
    let mut cells: Vec<Vec<walkers::Position>> = Vec::with_capacity(n);
    let mut rgbas: Vec<u32> = Vec::with_capacity(n);
    for i in 0..n {
        if let Some(b) = cell_boundary_positions(src.cell_ids[i]) {
            cells.push(b);
            rgbas.push(src.rgbas[i]);
        }
    }
    H3ChoroplethRenderable {
        cells,
        rgbas,
        stroke_width: src.stroke_width,
        stroke_color: src.stroke_color,
    }
}

// Aggregate an H3 region into outline rings (via h3o's SolventBuilder) and
// also collect per-cell boundaries (for fill-by-hex). Outline rings are
// cached keyed by cell-set hash so repeated frames with the same region
// skip the dissolve work. Per-cell boundaries are computed every frame
// (cheap: ~200ns/cell) since they're the fill primitives.
pub fn aggregate_h3_region(
    cache: &mut std::collections::HashMap<u64, Vec<Vec<walkers::Position>>>,
    src: H3Region,
) -> H3RegionRenderable {
    // Per-cell boundaries (always computed; used for fill)
    let mut cell_boundaries: Vec<Vec<walkers::Position>> = Vec::with_capacity(src.cell_ids.len());
    for &cid in &src.cell_ids {
        if let Some(b) = cell_boundary_positions(cid) {
            cell_boundaries.push(b);
        }
    }

    // Outline rings (cached)
    let key = hash_cell_set(&src.cell_ids);
    let outline_rings: Vec<Vec<walkers::Position>> = if let Some(cached) = cache.get(&key) {
        cached.clone()
    } else {
        let rings = dissolve_cells_to_rings(&src.cell_ids).unwrap_or_default();
        cache.insert(key, rings.clone());
        rings
    };

    // Label position: centroid of the first outline ring, else first cell
    let label_position = outline_rings
        .first()
        .and_then(|r| ring_centroid(r))
        .or_else(|| cell_boundaries.first().and_then(|b| ring_centroid(b)));

    H3RegionRenderable {
        outline_rings,
        cell_boundaries,
        fill: src.fill,
        stroke: src.stroke,
        label: src.label,
        label_position,
    }
}

// Dissolve a set of H3 cells into a flat list of rings (exterior first,
// interiors after). Returns an empty Vec on empty input or dissolve error.
pub fn dissolve_cells_to_rings(cell_ids: &[u64]) -> Option<Vec<Vec<walkers::Position>>> {
    if cell_ids.is_empty() {
        return Some(Vec::new());
    }
    let cells: Vec<h3o::CellIndex> =
        cell_ids.iter().filter_map(|&id| h3o::CellIndex::try_from(id).ok()).collect();
    if cells.is_empty() {
        return Some(Vec::new());
    }
    let solvent = h3o::geom::SolventBuilder::new().build();
    let multi = solvent.dissolve(cells.iter().copied()).ok()?;
    let mut rings: Vec<Vec<walkers::Position>> = Vec::new();
    for poly in multi.0.iter() {
        let ext = poly.exterior();
        let mut ring: Vec<walkers::Position> =
            ext.coords().map(|c| walkers::lon_lat(c.x, c.y)).collect();
        // geo-types exteriors are typically closed (last==first); drop the
        // duplicate so closed_line doesn't draw a zero-length edge.
        if ring.len() >= 2 {
            let first = ring[0];
            let last = ring[ring.len() - 1];
            if (first.lat() - last.lat()).abs() < 1e-12 && (first.lng() - last.lng()).abs() < 1e-12
            {
                ring.pop();
            }
        }
        rings.push(ring);
        for interior in poly.interiors() {
            let mut iring: Vec<walkers::Position> =
                interior.coords().map(|c| walkers::lon_lat(c.x, c.y)).collect();
            if iring.len() >= 2 {
                let first = iring[0];
                let last = iring[iring.len() - 1];
                if (first.lat() - last.lat()).abs() < 1e-12
                    && (first.lng() - last.lng()).abs() < 1e-12
                {
                    iring.pop();
                }
            }
            rings.push(iring);
        }
    }
    Some(rings)
}

// Centroid of a ring in lat/lon. Cheap average — good enough for label
// positioning (not mathematically area-weighted).
pub fn ring_centroid(ring: &[walkers::Position]) -> Option<walkers::Position> {
    if ring.is_empty() {
        return None;
    }
    let mut sum_lat = 0.0;
    let mut sum_lon = 0.0;
    for p in ring {
        sum_lat += p.lat();
        sum_lon += p.lng();
    }
    let n = ring.len() as f64;
    Some(walkers::lon_lat(sum_lon / n, sum_lat / n))
}

// OverlayPlugin — non-'static plugin that paints markers, polylines, H3
// choropleth layers, and H3 regions inside a walkers::Map render pass.
// Carries a `&'p mut Option<WalkersCamera>` that it populates at render
// time so the interpreter can stash the viewport for the fetcher.
pub struct OverlayPlugin<'p> {
    pub markers: Vec<WalkersMarker>,
    pub polylines: Vec<WalkersPolyline>,
    pub h3_choropleth: Vec<H3ChoroplethRenderable>,
    pub h3_regions: Vec<H3RegionRenderable>,
    pub rasters: Vec<WalkersRasterRenderable>,
    pub camera_out: &'p mut Option<WalkersCamera>,
    pub map_id: u64,
}

impl<'p> walkers::Plugin for OverlayPlugin<'p> {
    fn run(
        self: Box<Self>,
        ui: &mut egui::Ui,
        response: &egui::Response,
        projector: &walkers::Projector,
        memory: &walkers::MapMemory,
    ) {
        let me = *self;
        let rect = response.rect;

        // Clip overlay shapes to the map's bounding rect so strokes that
        // extend past the edge don't bleed onto surrounding UI.
        let painter = ui.painter_at(rect);

        // walkers' tile drawing anchors `map_center` to
        // `painter.clip_rect().center()` (walkers `tiles.rs:276`), which is the
        // intersection of the map widget rect with the parent ui's clip — i.e.
        // the *visible* portion when the map sits inside a partially-scrolled
        // ScrollArea. `walkers::Projector` instead anchors to the FULL
        // `response.rect.center()` (`projector.rs:38`), so without a correction
        // overlays drift away from the tiles as the user scrolls (visible as a
        // parallax on H3 hexagons, markers, polylines). Compute the offset
        // between the two anchors and apply it on every project / unproject so
        // overlays stay glued to the tiles regardless of clipping. When the
        // rect is fully visible the offset is zero and behaviour is unchanged.
        // Reported upstream as https://github.com/podusowski/walkers/issues/516
        // — drop the workaround once that lands and we bump walkers.
        let visible_clip = painter.clip_rect();
        let parallax_fix = visible_clip.center() - rect.center();

        let project = |pos: walkers::Position| -> egui::Pos2 {
            (projector.project(pos) + parallax_fix).to_pos2()
        };
        let unproject = |screen: egui::Vec2| -> walkers::Position {
            projector.unproject(screen - parallax_fix)
        };

        // ---- viewport bbox in lat/lon (used both for culling and for the
        // camera fetcher output) ----
        // Use the visible (clipped) corners so culling and the camera fetcher
        // report the lat/lon the user actually sees, not the off-screen
        // full-rect bbox.
        let tl = unproject(visible_clip.min.to_vec2());
        let tr = unproject(egui::pos2(visible_clip.max.x, visible_clip.min.y).to_vec2());
        let bl = unproject(egui::pos2(visible_clip.min.x, visible_clip.max.y).to_vec2());
        let br = unproject(visible_clip.max.to_vec2());
        let (vp_min_lat, vp_max_lat) = min_max_f64(&[tl.lat(), tr.lat(), bl.lat(), br.lat()]);
        let (vp_min_lon, vp_max_lon) = min_max_f64(&[tl.lng(), tr.lng(), bl.lng(), br.lng()]);

        // Slight margin so items straddling the edge still render their
        // on-screen parts without visible pop-in on pan.
        let margin_lat = (vp_max_lat - vp_min_lat) * 0.05;
        let margin_lon = (vp_max_lon - vp_min_lon) * 0.05;
        let cull_min_lat = vp_min_lat - margin_lat;
        let cull_max_lat = vp_max_lat + margin_lat;
        let cull_min_lon = vp_min_lon - margin_lon;
        let cull_max_lon = vp_max_lon + margin_lon;

        // bbox-vs-viewport intersection (AABB overlap in lat/lon space).
        // Assumes no antimeridian crossing — fine for Mercator-bound walkers.
        let bbox_in_viewport = |mn_lat: f64, mx_lat: f64, mn_lon: f64, mx_lon: f64| -> bool {
            mx_lat >= cull_min_lat
                && mn_lat <= cull_max_lat
                && mx_lon >= cull_min_lon
                && mn_lon <= cull_max_lon
        };

        // ---- layer 0: geo rasters (textured quad over the basemap, beneath
        // the vector overlays) ----
        for r in &me.rasters {
            // r.nw = (min_lon, max_lat) NW corner; r.se = (max_lon, min_lat) SE.
            if !bbox_in_viewport(r.se.lat(), r.nw.lat(), r.nw.lng(), r.se.lng()) {
                continue;
            }
            let q = egui::Rect::from_two_pos(project(r.nw), project(r.se));
            if q.width() <= 0.0 || q.height() <= 0.0 {
                continue;
            }
            let mut mesh = egui::Mesh::with_texture(r.tex);
            mesh.add_rect_with_uv(
                q,
                egui::Rect::from_min_max(egui::pos2(0.0, 0.0), egui::pos2(1.0, 1.0)),
                egui::Color32::WHITE.gamma_multiply(r.opacity),
            );
            painter.add(egui::Shape::mesh(mesh));
        }

        // ---- layer 1: H3 regions (fill hexes + stroke outline + label) ----
        for region in &me.h3_regions {
            // Cull: region bbox is the union of outline ring bboxes, or fall
            // back to per-cell bboxes when outlines are empty (dissolve fail).
            let region_bbox = bbox_of_rings(&region.outline_rings)
                .or_else(|| bbox_of_rings(&region.cell_boundaries));
            let Some((rmn_lat, rmx_lat, rmn_lon, rmx_lon)) = region_bbox else {
                continue;
            };
            if !bbox_in_viewport(rmn_lat, rmx_lat, rmn_lon, rmx_lon) {
                continue;
            }
            if let Some(fill) = region.fill {
                for cell in &region.cell_boundaries {
                    if cell.len() < 3 {
                        continue;
                    }
                    // Per-cell cull for sparse fills that leave the viewport.
                    if let Some((mn_lat, mx_lat, mn_lon, mx_lon)) = bbox_of_ring(cell) {
                        if !bbox_in_viewport(mn_lat, mx_lat, mn_lon, mx_lon) {
                            continue;
                        }
                    }
                    let pts: Vec<egui::Pos2> = cell.iter().map(|&p| project(p)).collect();
                    painter.add(egui::Shape::convex_polygon(pts, fill, egui::Stroke::NONE));
                }
            }
            if let Some(stroke) = region.stroke {
                for ring in &region.outline_rings {
                    if ring.len() < 2 {
                        continue;
                    }
                    // Per-ring cull for multi-polygon regions where only some
                    // rings are visible.
                    if let Some((mn_lat, mx_lat, mn_lon, mx_lon)) = bbox_of_ring(ring) {
                        if !bbox_in_viewport(mn_lat, mx_lat, mn_lon, mx_lon) {
                            continue;
                        }
                    }
                    let pts: Vec<egui::Pos2> = ring.iter().map(|&p| project(p)).collect();
                    painter.add(egui::Shape::closed_line(pts, stroke));
                }
            }
            if let (Some(label), Some(pos)) = (&region.label, region.label_position) {
                if pos.lat() >= cull_min_lat
                    && pos.lat() <= cull_max_lat
                    && pos.lng() >= cull_min_lon
                    && pos.lng() <= cull_max_lon
                {
                    let screen = project(pos);
                    painter.text(
                        screen,
                        egui::Align2::CENTER_CENTER,
                        label,
                        egui::FontId::default(),
                        egui::Color32::WHITE,
                    );
                }
            }
        }

        // ---- layer 2: H3 choropleth ----
        for layer in &me.h3_choropleth {
            let has_stroke =
                layer.stroke_width > 0.0 && layer.stroke_color != egui::Color32::TRANSPARENT;
            let stroke = if has_stroke {
                egui::Stroke::new(layer.stroke_width, layer.stroke_color)
            } else {
                egui::Stroke::NONE
            };
            for (cell_pts, &rgba) in layer.cells.iter().zip(layer.rgbas.iter()) {
                if cell_pts.len() < 3 {
                    continue;
                }
                if let Some((mn_lat, mx_lat, mn_lon, mx_lon)) = bbox_of_ring(cell_pts) {
                    if !bbox_in_viewport(mn_lat, mx_lat, mn_lon, mx_lon) {
                        continue;
                    }
                }
                let pts: Vec<egui::Pos2> = cell_pts.iter().map(|&p| project(p)).collect();
                painter.add(egui::Shape::convex_polygon(
                    pts,
                    color32_from_rgba_u32(rgba),
                    stroke,
                ));
            }
        }

        // ---- layer 3: polylines ----
        for pl in &me.polylines {
            let n = pl.lats.len().min(pl.lons.len());
            if n < 2 {
                continue;
            }
            let (pmn_lat, pmx_lat) = min_max_f64(&pl.lats[..n]);
            let (pmn_lon, pmx_lon) = min_max_f64(&pl.lons[..n]);
            if !bbox_in_viewport(pmn_lat, pmx_lat, pmn_lon, pmx_lon) {
                continue;
            }
            let pts: Vec<egui::Pos2> =
                (0..n).map(|i| project(walkers::lon_lat(pl.lons[i], pl.lats[i]))).collect();
            if pl.closed {
                painter.add(egui::Shape::closed_line(pts, pl.stroke));
            } else {
                painter.add(egui::Shape::line(pts, pl.stroke));
            }
        }

        // ---- layer 4: markers (point cull — single lat/lon per marker) ----
        for m in &me.markers {
            if m.lat < cull_min_lat
                || m.lat > cull_max_lat
                || m.lon < cull_min_lon
                || m.lon > cull_max_lon
            {
                continue;
            }
            let screen = project(walkers::lon_lat(m.lon, m.lat));
            let color = m.color.unwrap_or(egui::Color32::from_rgb(0xff, 0x33, 0x33));
            painter.circle_filled(screen, m.radius_px, color);
            painter.circle_stroke(
                screen,
                m.radius_px,
                egui::Stroke::new(1.0, egui::Color32::WHITE),
            );
            if let Some(label) = &m.label {
                painter.text(
                    screen + egui::vec2(m.radius_px + 2.0, 0.0),
                    egui::Align2::LEFT_CENTER,
                    label,
                    egui::FontId::default(),
                    egui::Color32::WHITE,
                );
            }
        }

        // ---- camera output for fetchR15WalkersCamera ----
        // Reuse the viewport bbox computed above.
        let (min_lat, max_lat) = (vp_min_lat, vp_max_lat);
        let (min_lon, max_lon) = (vp_min_lon, vp_max_lon);
        // Unproject the visible viewport's center — the lat/lon the user sees
        // in the middle of the map, even when the rect is partially clipped by
        // a parent ScrollArea. The local `unproject` helper applies
        // `parallax_fix`, so absolute screen coords go in and lat/lon comes
        // out matching what's actually drawn under that pixel.
        let center_pos = unproject(visible_clip.center().to_vec2());
        let zoom = memory.zoom();

        let (hover_lat, hover_lon, hover_valid) = match response.hover_pos() {
            Some(p) => {
                let pos = unproject(p.to_vec2());
                (pos.lat(), pos.lng(), true)
            }
            None => (f64::NAN, f64::NAN, false),
        };

        let view_hash = compute_view_hash(
            center_pos.lat(),
            center_pos.lng(),
            zoom,
            rect.width(),
            rect.height(),
        );

        *me.camera_out = Some(WalkersCamera {
            map_id: me.map_id,
            zoom,
            center_lat: center_pos.lat(),
            center_lon: center_pos.lng(),
            min_lat,
            min_lon,
            max_lat,
            max_lon,
            screen_width_px: rect.width(),
            screen_height_px: rect.height(),
            hover_lat,
            hover_lon,
            hover_valid,
            clicked: response.clicked(),
            view_hash,
        });
    }
}

// Tight bbox (min_lat, max_lat, min_lon, max_lon) of a single polygon ring
// given in walkers::Position lat/lon. Returns None for empty rings.
pub fn bbox_of_ring(ring: &[walkers::Position]) -> Option<(f64, f64, f64, f64)> {
    if ring.is_empty() {
        return None;
    }
    let mut min_lat = f64::INFINITY;
    let mut max_lat = f64::NEG_INFINITY;
    let mut min_lon = f64::INFINITY;
    let mut max_lon = f64::NEG_INFINITY;
    for p in ring {
        let la = p.lat();
        let lo = p.lng();
        if la < min_lat {
            min_lat = la;
        }
        if la > max_lat {
            max_lat = la;
        }
        if lo < min_lon {
            min_lon = lo;
        }
        if lo > max_lon {
            max_lon = lo;
        }
    }
    Some((min_lat, max_lat, min_lon, max_lon))
}

// Union bbox over a collection of rings. Returns None if every ring is empty.
pub fn bbox_of_rings(rings: &[Vec<walkers::Position>]) -> Option<(f64, f64, f64, f64)> {
    let mut agg: Option<(f64, f64, f64, f64)> = None;
    for ring in rings {
        if let Some((mn_la, mx_la, mn_lo, mx_lo)) = bbox_of_ring(ring) {
            agg = Some(match agg {
                None => (mn_la, mx_la, mn_lo, mx_lo),
                Some((a_mn_la, a_mx_la, a_mn_lo, a_mx_lo)) => (
                    a_mn_la.min(mn_la),
                    a_mx_la.max(mx_la),
                    a_mn_lo.min(mn_lo),
                    a_mx_lo.max(mx_lo),
                ),
            });
        }
    }
    agg
}

fn min_max_f64(vs: &[f64]) -> (f64, f64) {
    let mut lo = f64::INFINITY;
    let mut hi = f64::NEG_INFINITY;
    for &v in vs {
        if v.is_finite() {
            if v < lo {
                lo = v;
            }
            if v > hi {
                hi = v;
            }
        }
    }
    if !lo.is_finite() {
        lo = 0.0;
    }
    if !hi.is_finite() {
        hi = 0.0;
    }
    (lo, hi)
}

// ---------------------------------------------------------------------------
// 1. TableConfig — holds builder settings until apply time
//
// We can't use TableBuilder directly as {{Instance}} because
// TableBuilder::new(ui) requires &mut Ui at construction time.
// ---------------------------------------------------------------------------
pub struct TableConfig {
    pub row_height: f32,
    pub num_rows: usize,
    pub striped: bool,
    pub vscroll: bool,
    pub scroll_to_row: Option<usize>,
    pub min_scrolled_height: f32,
    pub max_scroll_height: f32,
}

impl TableConfig {
    pub fn new(row_height: f32, num_rows: u64) -> Self {
        Self {
            row_height,
            num_rows: num_rows as usize,
            striped: false,
            vscroll: true,
            scroll_to_row: None,
            min_scrolled_height: 0.0,
            max_scroll_height: 0.0,
        }
    }
}
// ---------------------------------------------------------------------------
// 2. TableCell — cell content enum
//
// Each variant holds pre-collected data that can be rendered without
// any IPC pipe reads. The render() method draws into the cell's Ui.
//
// Extend this enum to support more cell types (buttons, checkboxes, etc.)
// ---------------------------------------------------------------------------
pub enum TableCell {
    Text(String),
    RichText(egui::WidgetText), // from atoms register
}

impl TableCell {
    pub fn render(&self, ui: &mut egui::Ui) {
        match self {
            TableCell::Text(text) => {
                ui.label(text.as_str());
            }
            TableCell::RichText(widget_text) => {
                ui.label(widget_text.clone());
            }
        }
    }
}
/// Parse the Go-side initial-layout descriptor passed via the
/// `initialLayout` arg on `dockAreaRaw`. The wire format is
/// little-endian:
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
/// Used only the first time a dock_state is constructed. Subsequent
/// frames preserve the user's drag/drop changes in `dock_states`.
/// Any parse error or unknown version falls back to the default
/// `DockState::new(fallback_tabs)` single-leaf layout — the dock
/// stays functional, the user just loses their split preset.
pub fn parse_dock_initial_layout(bytes: &[u8], fallback_tabs: &[u64]) -> egui_dock::DockState<u64> {
    use byteorder::{LittleEndian, ReadBytesExt};
    use std::io::Cursor;

    let fallback = || egui_dock::DockState::new(fallback_tabs.to_vec());
    if bytes.is_empty() {
        return fallback();
    }
    let mut cur = Cursor::new(bytes);
    let version = match cur.read_u8() {
        Ok(v) => v,
        Err(_) => return fallback(),
    };
    if version != 1 {
        return fallback();
    }
    let num_root_tabs = match cur.read_u32::<LittleEndian>() {
        Ok(v) => v,
        Err(_) => return fallback(),
    };
    let mut root_tabs = Vec::with_capacity(num_root_tabs as usize);
    for _ in 0..num_root_tabs {
        match cur.read_u64::<LittleEndian>() {
            Ok(v) => root_tabs.push(v),
            Err(_) => return fallback(),
        }
    }
    let mut state = egui_dock::DockState::new(root_tabs);
    let mut leaf_nodes: std::collections::HashMap<u8, egui_dock::NodeIndex> =
        std::collections::HashMap::new();
    leaf_nodes.insert(0, egui_dock::NodeIndex::root());
    let num_splits = match cur.read_u8() {
        Ok(v) => v,
        Err(_) => return state,
    };
    for _ in 0..num_splits {
        let parent_id = match cur.read_u8() {
            Ok(v) => v,
            Err(_) => break,
        };
        let new_id = match cur.read_u8() {
            Ok(v) => v,
            Err(_) => break,
        };
        let dir = match cur.read_u8() {
            Ok(v) => v,
            Err(_) => break,
        };
        let fraction = match cur.read_f32::<LittleEndian>() {
            Ok(v) => v,
            Err(_) => break,
        };
        let num_tabs = match cur.read_u32::<LittleEndian>() {
            Ok(v) => v,
            Err(_) => break,
        };
        let mut tabs = Vec::with_capacity(num_tabs as usize);
        let mut ok = true;
        for _ in 0..num_tabs {
            match cur.read_u64::<LittleEndian>() {
                Ok(v) => tabs.push(v),
                Err(_) => {
                    ok = false;
                    break;
                }
            }
        }
        if !ok {
            break;
        }
        let parent_node = match leaf_nodes.get(&parent_id).copied() {
            Some(n) => n,
            None => continue,
        };
        let tree = state.main_surface_mut();
        let [old_idx, new_idx] = match dir {
            0 => tree.split_above(parent_node, fraction, tabs),
            1 => tree.split_below(parent_node, fraction, tabs),
            2 => tree.split_left(parent_node, fraction, tabs),
            3 => tree.split_right(parent_node, fraction, tabs),
            _ => continue,
        };
        leaf_nodes.insert(parent_id, old_idx);
        leaf_nodes.insert(new_id, new_idx);
    }
    state
}

/// Delegate that bridges egui_dock's TabViewer trait to FFFI2's deferred
/// opcode block replay. The Go side owns tab identity (u64) and emits each
/// tab's body as a deferred block; egui_dock owns the layout (splits, active
/// tab, drag-to-reorder) and calls `ui(&mut tab)` for the active tab only.
///
/// `closeable` is forced to false so the library never removes a tab on its
/// own — the Go side is the sole authority on which tabs exist. This lets
/// Go reconcile the dock state each frame by just sending an up-to-date tab
/// id list (see the dockArea apply code).
pub struct FffiDockTabViewer<'a, 'b, 'c, R: std::io::BufRead, W: std::io::Write> {
    pub interpreter: &'b mut ImZeroFffi<'a, R, W>,
    pub ctx: &'c egui::Context,
    pub bodies: std::collections::HashMap<u64, Vec<u8>>,
    pub titles: std::collections::HashMap<u64, String>,
    /// Tab ids whose body is NOT wrapped in a live ScrollArea (see
    /// `scroll_bars`). Populated from the Go-side `noScrollTabIds` arg.
    pub no_scroll: std::collections::HashSet<u64>,
}

impl<'a, 'b, 'c, R: std::io::BufRead, W: std::io::Write> egui_dock::TabViewer
    for FffiDockTabViewer<'a, 'b, 'c, R, W>
{
    type Tab = u64;

    fn title(&mut self, tab: &mut u64) -> egui::WidgetText {
        self.titles.get(tab).cloned().unwrap_or_else(|| format!("tab {tab}")).into()
    }

    fn ui(&mut self, ui: &mut egui::Ui, tab: &mut u64) {
        if let Some(block) = self.bodies.get(tab) {
            if !block.is_empty() {
                // Disjoint field access: bodies (immutable borrow via .get)
                // and interpreter (mutable reborrow) are separate fields of
                // self, so this compiles under NLL.
                let _ = self.interpreter.replay_deferred_block(self.ctx, ui, block);
            }
        }
    }

    fn closeable(&mut self, _tab: &mut u64) -> bool {
        false
    }

    /// Key the tab body's ui id on the Go-assigned tab id rather than
    /// egui_dock's default (`Id::new(self.title(tab).text())`). Titles here are
    /// per-frame view state — play appends graph marks and renames a bound tab
    /// after its node — so a title-derived id churns whenever the title does,
    /// silently resetting that body's ScrollArea offset, and two tabs that
    /// happen to share a title in one surface collide. The u64 is stable for
    /// the life of the tab by construction (Go owns tab existence).
    fn id(&mut self, tab: &mut u64) -> egui::Id {
        egui::Id::new(*tab)
    }

    /// egui_dock wraps every tab body in `ScrollArea::new(scroll_bars(tab))`
    /// (leaf.rs, default `[true, true]`). For a tab whose body owns its
    /// pointer/scroll interaction — e.g. a walkers map, which reads wheel and
    /// zoom input globally without consuming it — that wrapper reacts to the
    /// same wheel events as the widget, so one gesture scrolls the panel AND
    /// pans the map (the play Map-tab zoom flicker). Go marks such tabs via
    /// `noScrollTabIds`; their overflow clips instead of scrolling.
    ///
    /// The walkers half (read-without-consume) is reported upstream as
    /// https://github.com/podusowski/walkers/issues/544 — once a walkers that
    /// consumes the input it acts on lands, map-hosting tabs can drop the
    /// marker and return to default scroll bars.
    fn scroll_bars(&self, tab: &Self::Tab) -> [bool; 2] {
        if self.no_scroll.contains(tab) {
            [false, false]
        } else {
            [true, true]
        }
    }
}

/// Delegate that bridges egui_table's callback-driven API to FFFI2's
/// deferred opcode block replay. Each cell's buffered opcodes are replayed
/// via `interpreter.replay_deferred_block()` when egui_table calls `cell_ui`.
pub struct FffiTableDelegate<'a, 'b, 'c, R: std::io::BufRead, W: std::io::Write> {
    pub interpreter: &'b mut ImZeroFffi<'a, R, W>,
    pub cells: &'c crate::fffi::io::DenseBlockMap,
    pub header_blocks: &'c std::collections::HashMap<(u32, u32), Vec<u8>>,
    pub header_texts: &'c [String],
    pub row_offsets: &'c [f32],
    pub col_count: usize,
    pub default_row_height: f32,
}

impl<'a, 'b, 'c, R: std::io::BufRead, W: std::io::Write> egui_table::TableDelegate
    for FffiTableDelegate<'a, 'b, 'c, R, W>
{
    fn prepare(&mut self, _info: &egui_table::PrefetchInfo) {
        // Future: push info.visible_rows / info.visible_columns back to Go
        // via r9 so Go can optimize which cells it emits next frame.
    }

    fn header_cell_ui(&mut self, ui: &mut egui::Ui, cell: &egui_table::HeaderCellInfo) {
        let col = cell.col_range.start;
        let key = (cell.row_nr as u32, col as u32);
        if let Some(block) = self.header_blocks.get(&key) {
            if !block.is_empty() {
                let _ = self.interpreter.replay_deferred_block(&ui.ctx().clone(), ui, block);
                return;
            }
        }
        if col < self.header_texts.len() {
            ui.heading(self.header_texts[col].as_str());
        }
    }

    fn cell_ui(&mut self, ui: &mut egui::Ui, cell: &egui_table::CellInfo) {
        let block = self.cells.get(cell.row_nr, cell.col_nr as u32);
        if !block.is_empty() {
            let _ = self.interpreter.replay_deferred_block(&ui.ctx().clone(), ui, block);
        }
    }

    fn default_row_height(&self) -> f32 {
        self.default_row_height
    }

    fn row_top_offset(&self, _ctx: &egui::Context, _table_id: egui::Id, row_nr: u64) -> f32 {
        if !self.row_offsets.is_empty() && (row_nr as usize) < self.row_offsets.len() {
            self.row_offsets[row_nr as usize]
        } else {
            row_nr as f32 * self.default_row_height
        }
    }
}

pub struct ImZeroFffi<'a, R: std::io::BufRead, W: std::io::Write> {
    io: ImZeroFffiIo<R, W>,

    r0_atoms: egui::Atoms<'a>,
    r1_widget_text: egui::WidgetText,
    r3_node_cmds: Vec<NodeCommand<'a, u64>>,
    r5_id_set: roaring::RoaringTreemap, // inactive

    r7_ids: Vec<u64>,
    r7_responses: Vec<ResponseFlags>,
    r8_response_flags_filter: ResponseFlags,

    r9_u64_ids: Vec<u64>,
    r9_u64_values: Vec<u64>,
    r9_f64_ids: Vec<u64>,
    r9_f64_values: Vec<f64>,
    r9_i64_ids: Vec<u64>,
    r9_i64_values: Vec<i64>,
    r9_s_ids: Vec<u64>,
    r9_s_values: Vec<String>,
    // Texture ids interpreted this frame while starved — no usable cache
    // entry AND no pixels in the payload to (re)build one (a send-once
    // uploader whose full send went into a discarded hidden-tab buffer, or
    // whose entry the idle LRU evicted). Drained by fetchR22StarvedTextures;
    // the Go sender re-arms and re-ships. Ids are in the sender's own id
    // space (widget ids; the walkers mapRaster rasterId).
    r22_starved_texture_ids: Vec<u64>,
    // Scratch slot for TextEditFluid.InsertAtCursor: the `insertAtCursor`
    // builder-method arm stashes the snippet here; the TextEdit apply code
    // splices it at the caret and clears the slot within the same handler
    // invocation for the same widget id (so it never leaks across widgets,
    // even when the editor is culled). See ADR-0063.
    text_edit_pending_insert: Option<String>,
    // Scratch slot for TextEditFluid.HighlightJob (ADR-0130): the
    // `highlightJob` builder-method arm stashes the evaluated CodeViewJob
    // here; the TextEdit apply code turns it into a layouter for the same
    // widget and clears the slot within the same handler invocation.
    // Sections are advisory — text_edit_highlight reconciles them against
    // the live buffer and gap-fills, so a stale or malformed job degrades
    // to plain text, never to dropped glyphs.
    text_edit_pending_highlight: Option<code_view::CodeViewJobData>,
    // Scratch slots for TextEditFluid.SectionStyled / .NoWrapLayout
    // (ADR-0130 L3), consumed by the same TextEdit apply as the highlight
    // job above. Styled sections are the sparse overlay channel (underline,
    // background, strikethrough, italics); no-wrap is the gutter's alignment
    // contract — galley rows equal logical lines.
    text_edit_pending_styled: Option<Vec<text_edit_highlight::StyledSection>>,
    text_edit_pending_no_wrap: bool,
    // Scratch slot for TextEditFluid.ReportCursor (ADR-0130 L3): the apply
    // block reads the persisted TextEditState it already loads for
    // insertAtCursor and pushes the sorted cursor char range as a packed u64.
    text_edit_pending_report_cursor: bool,
    // Scratch slot for TextEditFluid.SetCursor: the inbound half of the caret
    // channel, packed exactly like ReportCursor's outbound value (low half
    // start, high half end, CHAR offsets). One-shot like insertAtCursor —
    // applied only on frames the method is present, because a range re-applied
    // every frame would pin the caret and make the editor unusable.
    text_edit_pending_set_cursor: Option<(u64, bool)>,
    // ADR-0088 runtime codec pipeline: `setVideoPipeline` stashes the
    // requested codec here (0=H.264, 1=VP9, 2=AV1); the headless host drains
    // it after dispatch and re-points the encoder. `video_cap_*` are pushed
    // by the host each frame (browser-decode ∩ host-encode capabilities) and
    // drained to Go by `fetchVideoCapabilities`.
    video_pipeline_request: Option<u8>,
    video_cap_ids: Vec<u64>,
    video_cap_flags: Vec<u32>,
    video_stream_info: Vec<u64>,
    // egui_table prefetch feedback: per-table visible range, filled by
    // EtStripedDelegate::prepare (declared in the endETable IDL apply
    // code) and drained by fetchR9EtPrefetch. 5 values per id:
    // (rowBegin, rowEnd, colBegin, colEnd, numStickyCols) — all u64 for
    // a single uniform column stream on the wire.
    pub r9_et_prefetch_ids: Vec<u64>,
    pub r9_et_prefetch_values: Vec<u64>,

    r10_true_ids: Vec<u64>,
    r10_false_ids: Vec<u64>,

    // window_open_bindings tracks the egui::Window "open" bool per
    // user-supplied binding id (Window.openBound method). Frame-stable
    // state, mutated by egui when the user clicks the title-bar X;
    // the Window apply code pushes the new value to r10_* on
    // transition so Go's StateManager.Sync writes through to the
    // bound *bool on the Go side. Entries are not actively garbage-
    // collected — Go-side window keys are monotonic-and-never-reused,
    // so a long-lived process accumulates one entry per ever-opened
    // window. Acceptable: a u64→bool entry is ~24 bytes and apps
    // don't churn windows at high rates.
    pub window_open_bindings: std::collections::HashMap<u64, bool>,
    // Scratch slot for the current Window arm's OpenBound method.
    // The codegen wraps construction code on the RHS of `let mut w = …`
    // which has no room for an outer-scope scratch let; the openBound
    // builder method writes here and the Window apply block drains it
    // with std::mem::take so each invocation starts from zero.
    scratch_open_binding_id: u64,

    r11_color32: egui::Color32,

    debug_tools: DebugTools,
    pub animation_freeze: bool,

    message_offsets: Vec<usize>,
    message_lengths: Vec<u32>,
    message_func_proc_ids_raw: Vec<u32>,
    // Most recent frame that successfully parsed and completed (matched
    // its declared msg_len exactly). Logged alongside any wire-desync
    // error so the operator can see the last *good* point of the
    // protocol — the bad frame is almost always emitted by the opcode
    // immediately after it (Go-side encoder writes more/less than the
    // Rust apply reads, leaving extra/missing bytes in the pipe).
    last_good_func_proc_id_raw: Option<u32>,
    last_good_msg_len: Option<u32>,
    last_good_byte_offset: Option<usize>,
    // Ring buffer of the most recent FRAME_TRAIL_LEN cleanly-parsed
    // frames (oldest at trail_head, newest at trail_head-1 mod len).
    // Dumped once per desync event so the operator can read the full
    // approach. Combined with desync_already_logged to suppress the
    // cascade — `let _ = self.interpret_outer(...)` call sites (e.g.
    // line 2362) silently drop inner errors, so a single Go-side
    // encoder bug fires every nested closure body and floods the log
    // with downstream noise. We log the trail exactly once per error
    // and reset the flag on the next clean frame.
    frame_trail: [(u32, u32, usize); 16],
    frame_trail_head: usize,
    frame_trail_count: u64,
    desync_already_logged: bool,

    pub table_columns: Vec<egui_extras::Column>,
    pub table_header_texts: Vec<String>,
    pub table_cells: Vec<TableCell>,
    pub et_columns: Vec<egui_table::Column>,
    pub et_header_texts: Vec<String>,
    pub et_row_heights: Vec<f32>,
    // newTable (egui_extras::TableBuilder via deferred block maps).
    // Independent of table_* registers so c.Table and c.NewTable can coexist
    // in one frame without aliasing.
    pub new_table_columns: Vec<egui_extras::Column>,
    pub new_table_row_heights: Vec<f32>,
    // Painter registers
    pub paint_cmds: Vec<PaintCmd>,

    // CodeView register + cache
    pub r12_code_view_job: code_view::CodeViewJobData,
    pub code_view_cache: code_view::CodeViewCache,

    // Styled-section register (ADR-0130 L3): the parallel overlay channel a
    // `styledSections` evaluated arg accumulates into and a consuming node
    // takes. Separate from r12 so the color-only Section the read-only
    // codeview producers share does not move.
    pub r24_styled_sections: Vec<text_edit_highlight::StyledSection>,

    // r14 (single-slot PaintCanvas pointer) is retired — see the note where
    // its fetcher used to be, in the fetchers IDL. Per-canvas pointer state is
    // r24; the canvas-local hover the wheel row carries is a local in the
    // paintCanvas apply code.

    // ADR-0140 canvas wheel capture: per-id scroll/zoom (and canvas-local
    // hover) a paintCanvas owned this frame while the pointer was over it, via
    // .CaptureZoom() / .CaptureScroll(). Parallel arrays keyed by the canvas
    // widget id, drained by FetchR23CanvasWheel; cleared in prepare_next_frame.
    r23_canvas_wheel_ids: Vec<u64>,
    r23_canvas_wheel_scroll_x: Vec<f32>,
    r23_canvas_wheel_scroll_y: Vec<f32>,
    r23_canvas_wheel_zoom: Vec<f32>,
    r23_canvas_wheel_hover_x: Vec<f32>,
    r23_canvas_wheel_hover_y: Vec<f32>,

    // ADR-0149 M1 canvas pointer, per id: every paintCanvas contributes one
    // row per frame — its screen origin plus the pointer in canvas-relative
    // coordinates (NaN when the pointer is neither over it nor dragging it).
    // Unlike the single-slot r14 registers this survives multiple canvases
    // per frame; unlike r23 it is unconditional, not capture-gated. The
    // pointer is interact_pointer_pos-first so a drag that leaves the canvas
    // keeps reporting (pan beyond the edge). Parallel arrays keyed by the
    // canvas widget id, drained by FetchR24CanvasPointers; cleared in
    // prepare_next_frame.
    r24_canvas_pointer_ids: Vec<u64>,
    r24_canvas_pointer_origin_x: Vec<f32>,
    r24_canvas_pointer_origin_y: Vec<f32>,
    r24_canvas_pointer_pos_x: Vec<f32>,
    r24_canvas_pointer_pos_y: Vec<f32>,
    r24_canvas_pointer_mods: Vec<u8>,

    // Ui::available_size snapshot — set by the captureAvailableSize
    // procedural op when called inside a Ui scope, read by Go via
    // fetchR18AvailableSize. One-frame lag (capture this frame, read
    // next frame) — same pattern as r14 canvas-pointer. NaN sentinels
    // mean no capture has occurred yet or the most recent capture ran
    // outside any Ui.
    pub r18_avail_w: f32,
    pub r18_avail_h: f32,

    // Per-frame ui.min_rect() snapshots stamped by the captureUiRect
    // procedural op, drained by fetchR21UiRects. Parallel arrays of
    // equal length — one row per capture in registration order. Used
    // by the bezier-connector affordance to learn viewport-absolute
    // rects of named Ui scopes (host, inspector window content)
    // without exposing a per-widget rect query.
    pub r21_ui_rect_seqs: Vec<u64>,
    pub r21_ui_rect_min_x: Vec<f32>,
    pub r21_ui_rect_min_y: Vec<f32>,
    pub r21_ui_rect_max_x: Vec<f32>,
    pub r21_ui_rect_max_y: Vec<f32>,

    // egui_table settled column widths (ADR-0151 §SD4), pushed after
    // Table::show by tables that opted in via applyWidths. ids[i] owns
    // counts[i] entries in values, laid end to end — the column count
    // varies per table, so this cannot use the fixed stride r9 uses.
    pub r25_et_colwidth_ids: Vec<u64>,
    pub r25_et_colwidth_counts: Vec<u64>,
    pub r25_et_colwidth_values: Vec<f32>,

    // Last width epoch applied per table widget id, across every table
    // surface (egui_table's endETable, egui_extras' table and newTable).
    // One map because widget ids are globally unique, so the surfaces
    // cannot collide. Retained across frames (the "library owns state that
    // must survive frames" bucket): it is what makes an apply happen
    // exactly once per Go-side width change instead of every frame, which
    // is what leaves a live drag alone.
    pub width_epochs: std::collections::HashMap<u64, u32>,

    // egui_dock layout state keyed by dock-area id. Persisted across frames
    // (splits, active tab per group, drag-to-reorder live here). Go sends
    // the tab id list each frame; the apply code reconciles it against the
    // stored DockState via retain_tabs + push_to_first_leaf.
    pub dock_states: std::collections::HashMap<u64, egui_dock::DockState<u64>>,

    // egui_graphs — per-frame pending lists (drained by the `graph` opcode)
    // and retained layout state (one egui_graphs::Graph per widget id,
    // preserving node positions / drag state across frames).
    pub graph_pending_nodes: Vec<GraphNodeData>,
    pub graph_pending_edges: Vec<GraphEdgeData>,
    pub graph_states: std::collections::HashMap<u64, GraphState>,
    // Accumulated per-frame graph interaction events. Drained by the
    // fetchGraphEvents fetcher into Go; cleared in prepare_next_frame as
    // a safety net if Go doesn't fetch.
    pub graph_events_pending: Vec<GraphEventRecord>,
    // Per-frame snapshot of the current selection; rebuilt in the graph
    // apply code from Node/Edge::selected(). Parallel arrays with equal
    // length; `kind` = 0 for nodes (keyA=node id, keyB=0), 1 for edges
    // (keyA=from, keyB=to).
    pub graph_selection_graph_ids: Vec<u64>,
    pub graph_selection_kind: Vec<u8>,
    pub graph_selection_key_a: Vec<u64>,
    pub graph_selection_key_b: Vec<u64>,
    // Per-frame snapshot of per-graph metrics: one entry per graph widget
    // that rendered this frame. fr_step_count / fr_last_disp are 0/NaN
    // for non-FR layouts.
    pub graph_metrics_graph_ids: Vec<u64>,
    pub graph_metrics_node_count: Vec<u32>,
    pub graph_metrics_edge_count: Vec<u32>,
    pub graph_metrics_fr_steps: Vec<u64>,
    pub graph_metrics_fr_last_disp: Vec<f32>,

    // walkers (slippy map) — per-frame pending overlays drained by the
    // `walkersMap` opcode, and retained per-widget state (HttpTiles + MapMemory
    // + h3Region outline cache) persisted across frames keyed by widget id.
    pub walkers_pending_markers: Vec<WalkersMarker>,
    pub walkers_pending_polylines: Vec<WalkersPolyline>,
    pub walkers_pending_h3_choropleth: Vec<H3Choropleth>,
    pub walkers_pending_h3_regions: Vec<H3Region>,
    pub walkers_pending_rasters: Vec<WalkersRaster>,
    pub walkers_states: std::collections::HashMap<u64, WalkersState>,
    /// Last camera per map id, retained until that map renders again (a reader
    /// running a frame behind the viewport still needs one). Keyed rather than
    /// single-slot since 2026-08-04: one slot meant the last map to render in a
    /// frame was the only one any reader could see.
    pub walkers_cameras: std::collections::HashMap<u64, WalkersCamera>,

    // scrollingTexture (ADR-0009) — ring-buffer pixel widget; texture cache
    // keyed by widget id, caller-owned scroll head. Module: scrolling_texture.
    pub scrolling_texture: ScrollingTextureCache,

    // image — RGBA pixel widget; texture cache keyed by widget id, upload
    // gated by Go-supplied content_version. Module: image.
    pub image_cache: ImageCache,

    // walkers mapRaster (ADR-0096) — a second ImageCache instance for geo
    // raster overlays, keyed by rasterId. Separate from image_cache to avoid
    // sharing the widget-id keyspace; same content-version + tour-capture
    // plumbing. The overlay paints the texture itself (ImageCache::ensure).
    pub walkers_raster_cache: ImageCache,

    // ADR-0149 M4 paintImage — a third ImageCache for painter-lane textured
    // rects (heatmap texture route and future raster underlays), keyed by a
    // Go-derived image id. Same content-version + starved-report protocol
    // as the walkers raster cache.
    pub paint_image_cache: ImageCache,

    // egui-snarl (ADR-0021) — node-editor binding. Per-frame pending
    // accumulators (drained by the `snarlEditor` opcode) + retained
    // Snarl<u64> per editor id (positions, drag/connect state, viewport
    // transform). Edit events accumulated for fetchSnarlEvents.
    pub snarl_pending_nodes: Vec<SnarlNodeData>,
    pub snarl_pending_connections: Vec<SnarlConnectionData>,
    pub snarl_pending_pins: Vec<SnarlPinData>,
    pub snarl_states: std::collections::HashMap<u64, SnarlState>,
    pub snarl_events_pending: Vec<SnarlEvent>,

    // Frame-metrics introspection — captured at the bottom of
    // interpret_commands_outer, drained by the fetchFrameMetrics fetcher
    // with one-frame display lag. Reported in microseconds; saturates at
    // u32::MAX (~71 minutes per frame, well past anything we want to see).
    pub last_interpret_us: u32,
    pub last_pass_nr: u64,

    // SVG export hand-off — written by the `exportSvg` IDL opcode (during
    // FFFI dispatch), read by `SvgExportPlugin::on_end_pass` later the same
    // pass. The plugin is registered in `App::new` with a clone of this
    // handle, so the path Go writes here becomes the file the plugin
    // produces before tessellation.
    pub export_state: ExportStateHandle,

    // CPU-side mirror of texture pixels for SVG `<image>` embedding.
    // Populated by `ImageCache::upload` whenever a Go-supplied image is
    // uploaded; read by the SVG visitor when `Shape::Mesh.texture_id`
    // points at a known texture. Shared with the plugin via a clone.
    pub texture_cache: TexturePixelCacheHandle,

    // Per-frame hyperlink registry. The Hyperlink / HyperlinkTo apply
    // code pushes (rect, url) after the widget renders; the SVG visitor
    // drains it at export time and wraps overlapping text shapes in
    // `<a href="…">`. Cleared in `prepare_next_frame`. Future: the Go
    // side can rewrite URLs (e.g. resolve relative-paths for SVG
    // viewers) by mutating the underlying String before the next frame.
    pub link_zones: LinkZonesHandle,
}

impl<'a, R: std::io::BufRead, W: std::io::Write> ImZeroFffi<'a, R, W> {
    /// ADR-0088: take a pending runtime codec-pipeline request (set by the
    /// `setVideoPipeline` opcode); the headless host applies it to the encoder.
    pub fn take_video_pipeline_request(&mut self) -> Option<u8> {
        self.video_pipeline_request.take()
    }

    /// ADR-0088: publish the video-pipeline capabilities for Go to drain via
    /// `fetchVideoCapabilities`. Each entry is (codecId, flags); flags bit0 =
    /// host-can-encode, bit1 = browser-decode-supported, bit2 = smooth,
    /// bit3 = power-efficient. Replaces any not-yet-fetched set.
    pub fn set_video_capabilities(&mut self, caps: &[(u8, u32)]) {
        self.video_cap_ids.clear();
        self.video_cap_flags.clear();
        for &(id, flags) in caps {
            self.video_cap_ids.push(id as u64);
            self.video_cap_flags.push(flags);
        }
    }

    /// ADR-0088: publish the active stream telemetry for Go to drain via
    /// `fetchVideoStreamInfo`. The headless host packs `[width, height, fps,
    /// cadence, bitrate_kbps, frames_sent, frames_dropped, frames_in_flight]`.
    pub fn set_video_stream_info(&mut self, values: &[u64]) {
        self.video_stream_info.clear();
        self.video_stream_info.extend_from_slice(values);
    }
}

impl<'a, R: std::io::BufRead, W: std::io::Write> ImZeroFffi<'a, R, W> {
    pub fn new(r: R, w: W) -> Self {
        //let default_resp = || {
        //    let rect = egui::Rect {
        //        min: (egui::Pos2 {
        //            x: 0.0f32,
        //            y: 0.0f32,
        //        }),
        //        max: egui::Pos2 {
        //            x: 0.0f32,
        //            y: 0.0f32,
        //        },
        //    };
        //    return egui::response::Response {
        //        ctx: egui::Context::default(),
        //        layer_id: egui::LayerId {
        //            order: egui::Order::Background,
        //            id: egui::Id::NULL,
        //        },
        //        id: egui::Id::NULL,
        //        rect: rect,
        //        interact_rect: rect,
        //        sense: egui::Sense::all(),
        //        interact_pointer_pos: Option::None,
        //        intrinsic_size: Option::None,
        //        flags: egui::response::Flags::empty(),
        //    };
        //};
        const STACK_INITIAL_CAPACITY: usize = 16;
        let mut r8 = ResponseFlags::all();
        r8.remove(ResponseFlags::ENABLED);
        //r8.remove(ResponseFlags::CLICKED_ELSEWHERE);
        let mut s = Self {
            io: ImZeroFffiIo::new(r, w),
            r0_atoms: egui::Atoms::default(),
            r1_widget_text: egui::WidgetText::default(),
            r3_node_cmds: Vec::with_capacity(STACK_INITIAL_CAPACITY),
            r5_id_set: roaring::RoaringTreemap::new(),
            r7_ids: Vec::with_capacity(1024),
            r7_responses: Vec::with_capacity(1024),
            r8_response_flags_filter: r8,
            r9_u64_ids: Vec::with_capacity(1024),
            r9_u64_values: Vec::with_capacity(1024),
            r9_f64_ids: Vec::with_capacity(1024),
            r9_f64_values: Vec::with_capacity(1024),
            r9_i64_ids: Vec::with_capacity(1024),
            r9_i64_values: Vec::with_capacity(1024),
            r9_s_ids: Vec::with_capacity(1024),
            r9_s_values: Vec::with_capacity(1024),
            r22_starved_texture_ids: Vec::with_capacity(8),
            text_edit_pending_insert: None,
            text_edit_pending_highlight: None,
            text_edit_pending_styled: None,
            text_edit_pending_no_wrap: false,
            text_edit_pending_report_cursor: false,
            text_edit_pending_set_cursor: None,
            r9_et_prefetch_ids: Vec::with_capacity(8),
            r9_et_prefetch_values: Vec::with_capacity(32),
            r10_true_ids: Vec::with_capacity(1024),
            r10_false_ids: Vec::with_capacity(1024),
            window_open_bindings: std::collections::HashMap::with_capacity(32),
            scratch_open_binding_id: 0,
            debug_tools: DebugTools::new(),
            animation_freeze: false,
            message_offsets: vec![],
            message_lengths: vec![],
            message_func_proc_ids_raw: vec![],
            last_good_func_proc_id_raw: None,
            last_good_msg_len: None,
            last_good_byte_offset: None,
            frame_trail: [(0u32, 0u32, 0usize); 16],
            frame_trail_head: 0,
            frame_trail_count: 0,
            desync_already_logged: false,
            r11_color32: egui::Color32::TRANSPARENT,
            table_columns: Vec::with_capacity(16),
            table_header_texts: Vec::with_capacity(16),
            table_cells: Vec::with_capacity(256),
            et_columns: Vec::with_capacity(16),
            et_header_texts: Vec::with_capacity(16),
            et_row_heights: Vec::new(),
            new_table_columns: Vec::with_capacity(16),
            new_table_row_heights: Vec::with_capacity(64),
            paint_cmds: Vec::with_capacity(32),
            r12_code_view_job: code_view::CodeViewJobData::default(),
            code_view_cache: code_view::CodeViewCache::new(),
            r24_styled_sections: Vec::with_capacity(8),
            r23_canvas_wheel_ids: Vec::with_capacity(16),
            r23_canvas_wheel_scroll_x: Vec::with_capacity(16),
            r23_canvas_wheel_scroll_y: Vec::with_capacity(16),
            r23_canvas_wheel_zoom: Vec::with_capacity(16),
            r23_canvas_wheel_hover_x: Vec::with_capacity(16),
            r23_canvas_wheel_hover_y: Vec::with_capacity(16),
            r24_canvas_pointer_ids: Vec::with_capacity(16),
            r24_canvas_pointer_origin_x: Vec::with_capacity(16),
            r24_canvas_pointer_origin_y: Vec::with_capacity(16),
            r24_canvas_pointer_pos_x: Vec::with_capacity(16),
            r24_canvas_pointer_pos_y: Vec::with_capacity(16),
            r24_canvas_pointer_mods: Vec::with_capacity(16),
            r18_avail_w: f32::NAN,
            r18_avail_h: f32::NAN,
            r21_ui_rect_seqs: Vec::with_capacity(8),
            r21_ui_rect_min_x: Vec::with_capacity(8),
            r21_ui_rect_min_y: Vec::with_capacity(8),
            r21_ui_rect_max_x: Vec::with_capacity(8),
            r21_ui_rect_max_y: Vec::with_capacity(8),
            r25_et_colwidth_ids: Vec::with_capacity(4),
            r25_et_colwidth_counts: Vec::with_capacity(4),
            r25_et_colwidth_values: Vec::with_capacity(32),
            width_epochs: std::collections::HashMap::new(),
            dock_states: std::collections::HashMap::new(),
            video_pipeline_request: None,
            video_cap_ids: Vec::new(),
            video_cap_flags: Vec::new(),
            video_stream_info: Vec::new(),
            graph_pending_nodes: Vec::with_capacity(64),
            graph_pending_edges: Vec::with_capacity(64),
            graph_states: std::collections::HashMap::new(),
            graph_events_pending: Vec::with_capacity(32),
            graph_selection_graph_ids: Vec::with_capacity(32),
            graph_selection_kind: Vec::with_capacity(32),
            graph_selection_key_a: Vec::with_capacity(32),
            graph_selection_key_b: Vec::with_capacity(32),
            graph_metrics_graph_ids: Vec::with_capacity(8),
            graph_metrics_node_count: Vec::with_capacity(8),
            graph_metrics_edge_count: Vec::with_capacity(8),
            graph_metrics_fr_steps: Vec::with_capacity(8),
            graph_metrics_fr_last_disp: Vec::with_capacity(8),
            walkers_pending_markers: Vec::with_capacity(64),
            walkers_pending_polylines: Vec::with_capacity(16),
            walkers_pending_h3_choropleth: Vec::with_capacity(4),
            walkers_pending_h3_regions: Vec::with_capacity(8),
            walkers_pending_rasters: Vec::with_capacity(4),
            walkers_states: std::collections::HashMap::new(),
            walkers_cameras: std::collections::HashMap::new(),
            scrolling_texture: ScrollingTextureCache::new(),
            image_cache: ImageCache::new(),
            walkers_raster_cache: ImageCache::new(),
            paint_image_cache: ImageCache::new(),
            snarl_pending_nodes: Vec::with_capacity(64),
            snarl_pending_connections: Vec::with_capacity(64),
            snarl_pending_pins: Vec::with_capacity(64),
            snarl_states: std::collections::HashMap::new(),
            snarl_events_pending: Vec::with_capacity(32),
            last_interpret_us: 0,
            last_pass_nr: 0,
            export_state: std::sync::Arc::new(std::sync::Mutex::new(ExportState::default())),
            texture_cache: std::sync::Arc::new(std::sync::Mutex::new(TexturePixelCache::default())),
            link_zones: std::sync::Arc::new(std::sync::Mutex::new(Vec::new())),
        };
        // Wire ImageCache + ScrollingTextureCache so both mirror their
        // RGBA pixels into the shared texture_cache. This lets the SVG
        // visitor embed `Shape::Mesh.texture_id` as an `<image>` data
        // URL for image widgets, heatmaps, and scrolling textures alike.
        s.image_cache.attach_texture_cache(s.texture_cache.clone());
        s.walkers_raster_cache.attach_texture_cache(s.texture_cache.clone());
        s.paint_image_cache.attach_texture_cache(s.texture_cache.clone());
        s.scrolling_texture.attach_texture_cache(s.texture_cache.clone());
        s
    }
    pub fn render_debug_tools(&mut self, ctx: &egui::Context, ui: &mut egui::Ui) {
        self.debug_tools.render_debug_tools(&self.io, ctx, ui);
    }
    pub fn prepare_next_frame(&mut self) {
        self.io.reset_counts();
        {
            if !self.r3_node_cmds.is_empty() {
                tracing::debug!(
                    len = self.r3_node_cmds.len(),
                    "r3_node_cmds is not empty (unused tree commands), clearing"
                );
            }
            self.r3_node_cmds.clear();
        }

        {
            if !self.r7_ids.is_empty() {
                tracing::debug!(
                    len = self.r7_ids.len(),
                    "r7 is not empty (widget responses not fetched), clearing"
                );
            }
            self.r7_ids.clear();
            self.r7_responses.clear();
        }

        {
            if !self.r23_canvas_wheel_ids.is_empty() {
                tracing::debug!(
                    len = self.r23_canvas_wheel_ids.len(),
                    "r23 canvas-wheel not empty (captures not fetched), clearing"
                );
            }
            self.r23_canvas_wheel_ids.clear();
            self.r23_canvas_wheel_scroll_x.clear();
            self.r23_canvas_wheel_scroll_y.clear();
            self.r23_canvas_wheel_zoom.clear();
            self.r23_canvas_wheel_hover_x.clear();
            self.r23_canvas_wheel_hover_y.clear();
        }

        {
            // r24 rows are re-stamped by every canvas every frame; an unfetched
            // frame is normal (no reader registered), so no debug log here.
            self.r24_canvas_pointer_ids.clear();
            self.r24_canvas_pointer_origin_x.clear();
            self.r24_canvas_pointer_origin_y.clear();
            self.r24_canvas_pointer_pos_x.clear();
            self.r24_canvas_pointer_pos_y.clear();
            self.r24_canvas_pointer_mods.clear();
        }

        {
            // r25 is re-stamped every frame by every opted-in etable, so an
            // unfetched frame is normal (a table using applyWidths without a
            // resolver reading back). Cleared without a log, as r24 is.
            //
            // width_epochs is deliberately NOT cleared: it is retained
            // state, and forgetting it would make every frame look like an
            // epoch change and re-apply widths on top of a live drag.
            self.r25_et_colwidth_ids.clear();
            self.r25_et_colwidth_counts.clear();
            self.r25_et_colwidth_values.clear();
        }

        // Hyperlink zones — cleared so a removed link doesn't carry into
        // the next frame's SVG export. Cheap: typically <10 entries per
        // frame and most demos have zero.
        if let Ok(mut zones) = self.link_zones.lock() {
            zones.clear();
        }

        self.table_columns.clear();
        self.table_header_texts.clear();
        self.table_cells.clear();
        self.et_columns.clear();
        self.et_header_texts.clear();
        self.new_table_columns.clear();
        self.new_table_row_heights.clear();
        self.paint_cmds.clear();
        self.r21_ui_rect_seqs.clear();
        self.r21_ui_rect_min_x.clear();
        self.r21_ui_rect_min_y.clear();
        self.r21_ui_rect_max_x.clear();
        self.r21_ui_rect_max_y.clear();
        self.r12_code_view_job.text.clear();
        self.r12_code_view_job.sections.clear();
        self.r24_styled_sections.clear();
        self.graph_pending_nodes.clear();
        self.graph_pending_edges.clear();
        // graph_states NOT cleared — persists layout positions across frames
        if !self.graph_events_pending.is_empty() {
            tracing::debug!(
                len = self.graph_events_pending.len(),
                "graph_events_pending is not empty (unfetched graph events), clearing"
            );
            self.graph_events_pending.clear();
        }
        // Selection + metrics snapshots are per-frame — repopulated by the
        // next graph apply pass. Cleared here so a stale frame's snapshot
        // is never returned by a late fetcher call.
        self.graph_selection_graph_ids.clear();
        self.graph_selection_kind.clear();
        self.graph_selection_key_a.clear();
        self.graph_selection_key_b.clear();
        self.graph_metrics_graph_ids.clear();
        self.graph_metrics_node_count.clear();
        self.graph_metrics_edge_count.clear();
        self.graph_metrics_fr_steps.clear();
        self.graph_metrics_fr_last_disp.clear();

        // walkers — pending overlays leak into next frame if the map was
        // culled. Clear them here; walkers_states and walkers_cameras are NOT
        // cleared — both persist per map id, and the camera fetch is a
        // non-consuming read of what each map last reported.
        if !self.walkers_pending_markers.is_empty()
            || !self.walkers_pending_polylines.is_empty()
            || !self.walkers_pending_h3_choropleth.is_empty()
            || !self.walkers_pending_h3_regions.is_empty()
            || !self.walkers_pending_rasters.is_empty()
        {
            tracing::debug!(
                "walkers pending overlays leaked (no walkersMap drained them), clearing"
            );
            self.walkers_pending_markers.clear();
            self.walkers_pending_polylines.clear();
            self.walkers_pending_h3_choropleth.clear();
            self.walkers_pending_h3_regions.clear();
            self.walkers_pending_rasters.clear();
        }

        // snarl — same shape as walkers/graphs. Pending accumulators are
        // drained by snarlEditor; if no snarlEditor ran the accumulators
        // leak into the next frame. snarl_states persists across frames
        // (retained Snarl<u64>); snarl_events_pending is drained by
        // fetchSnarlEvents but cleared as a safety net.
        if !self.snarl_pending_nodes.is_empty()
            || !self.snarl_pending_connections.is_empty()
            || !self.snarl_pending_pins.is_empty()
        {
            tracing::debug!("snarl pending leaked (no snarlEditor drained them), clearing");
            self.snarl_pending_nodes.clear();
            self.snarl_pending_connections.clear();
            self.snarl_pending_pins.clear();
        }
        if !self.snarl_events_pending.is_empty() {
            tracing::debug!(
                len = self.snarl_events_pending.len(),
                "snarl_events_pending unfetched, clearing"
            );
            self.snarl_events_pending.clear();
        }
    }
    pub fn interpret_commands_outer(&mut self, ctx: &egui::Context) -> InterpretResult<()> {
        let t0 = std::time::Instant::now();
        self.handle_screenshot_event(ctx);
        // Advance per-frame state for widgets that need it. Must run exactly
        // once per real frame; `interpret_outer` alone does not qualify
        // because it is re-entered from `replay_deferred_block`.
        self.scrolling_texture.tick();
        self.image_cache.tick();
        self.walkers_raster_cache.tick();
        self.paint_image_cache.tick();
        // egui 0.35: root panels (`Panel::{top,bottom,left,right}`, `CentralPanel`)
        // now render *inside* a `Ui` — the `Panel::show(&Context)` that attached
        // straight to the screen in 0.34 was removed. Every host drives us from a
        // `run_ui`/`logic` scope that owns the real root `Ui` but only hands us its
        // `Context` (the desktop host deliberately runs from `logic()` so it keeps
        // draining Go's stream while the window is startup-hidden — see app.rs).
        // So we build our own background-layer `Ui` spanning the viewport, with an
        // id distinct from egui's own `__top_ui` to avoid a clash, and thread it as
        // the root — exactly what `Panel::show(ctx)` did internally. Widgets never
        // dispatch at the root (Go always opens a container first, and
        // `apply_widget` late-culls when `u` is `None`), so a `Some` root only
        // supplies root panels the `Ui` they now require.
        let mut root_ui = egui::Ui::new(
            ctx.clone(),
            egui::Id::new((ctx.viewport_id(), "__imzero2_root")),
            egui::UiBuilder::new()
                .layer_id(egui::LayerId::background())
                .max_rect(ctx.viewport_rect()),
        );
        let mut root = Some(&mut root_ui);
        let result = self.interpret_outer(ctx, &mut root);
        // Capture even on error so the overlay keeps reporting the time spent
        // before the failure rather than freezing on a stale value.
        let micros = t0.elapsed().as_micros();
        self.last_interpret_us = if micros > u32::MAX as u128 {
            u32::MAX
        } else {
            micros as u32
        };
        self.last_pass_nr = ctx.cumulative_pass_nr();
        result
    }
    /// Check for Screenshot events and write images to disk as PNG.
    /// The event arrives one frame after `ViewportCommand::Screenshot` was sent.
    /// `UserData` carries either a bare `String` path (from `requestScreenshot`
    /// — full viewport capture) or a `ScreenshotRequest { path, rect }` (from
    /// `requestScreenshotRect` — cropped to `rect` before PNG encode).
    fn handle_screenshot_event(&self, ctx: &egui::Context) {
        ctx.input(|i| {
            for e in &i.events {
                if let egui::Event::Screenshot {
                    image, user_data, ..
                } = e
                {
                    let data = match user_data.data.as_ref() {
                        Some(d) => d,
                        None => continue,
                    };
                    if let Some(req) = data.downcast_ref::<ScreenshotRequest>() {
                        self.write_screenshot_png(image, &req.path, req.rect, i.pixels_per_point);
                    } else if let Some(path) = data.downcast_ref::<String>() {
                        self.write_screenshot_png(image, path, None, i.pixels_per_point);
                    }
                }
            }
        });
    }
    /// Encode an (optionally cropped) `ColorImage` to PNG. Rect is in logical
    /// points; converted to physical pixels via `pixels_per_point`. A clamp
    /// guards against off-viewport rects.
    fn write_screenshot_png(
        &self,
        image: &egui::ColorImage,
        path: &str,
        rect: Option<egui::Rect>,
        pixels_per_point: f32,
    ) {
        let full_w = image.size[0] as u32;
        let full_h = image.size[1] as u32;
        let (rgba, width, height) = match rect {
            Some(r) => {
                let x = (r.min.x * pixels_per_point).round().max(0.0) as u32;
                let y = (r.min.y * pixels_per_point).round().max(0.0) as u32;
                let w = (r.width() * pixels_per_point).round().max(1.0) as u32;
                let h = (r.height() * pixels_per_point).round().max(1.0) as u32;
                let x_end = (x + w).min(full_w);
                let y_end = (y + h).min(full_h);
                let cw = x_end.saturating_sub(x);
                let ch = y_end.saturating_sub(y);
                let mut buf = Vec::with_capacity((cw * ch * 4) as usize);
                for row in y..y_end {
                    let row_start = (row * full_w + x) as usize;
                    let row_end = (row * full_w + x_end) as usize;
                    for px in &image.pixels[row_start..row_end] {
                        buf.extend_from_slice(&px.to_array());
                    }
                }
                (buf, cw, ch)
            }
            None => {
                let buf: Vec<u8> = image.pixels.iter().flat_map(|c| c.to_array()).collect();
                (buf, full_w, full_h)
            }
        };
        match Self::write_png(path, &rgba, width, height) {
            Ok(()) => {
                tracing::info!(%path, width, height, "screenshot saved");
            }
            Err(e) => {
                tracing::error!(%path, error = %e, "failed to save screenshot");
            }
        }
    }
    /// Encode RGBA pixels as PNG and write to the given file path.
    fn write_png(path: &str, rgba: &[u8], width: u32, height: u32) -> std::io::Result<()> {
        let file = std::fs::File::create(path)?;
        let w = std::io::BufWriter::new(file);
        let mut encoder = png::Encoder::new(w, width, height);
        encoder.set_color(png::ColorType::Rgba);
        encoder.set_depth(png::BitDepth::Eight);
        let mut writer = encoder
            .write_header()
            .map_err(|e| std::io::Error::new(std::io::ErrorKind::Other, e))?;
        writer
            .write_image_data(rgba)
            .map_err(|e| std::io::Error::new(std::io::ErrorKind::Other, e))?;
        Ok(())
    }
    pub fn begin_consume_message<T: std::fmt::Debug>(
        &mut self,
        from_repr: fn(discriminator: u32) -> Option<T>,
    ) -> InterpretResult<Option<T>> {
        match self.io.read_plain_u32() {
            Ok(msg_len) => {
                let msg_len_offset = self.io.read_bytes_count - 4;
                tracing::trace!(msg_len = msg_len, "read msg");
                let offset = self.io.read_bytes_count; // position of this line matters
                let raw = self.io.read_plain_u32()?;
                let func_proc_id = match from_repr(raw) {
                    Some(v) => v,
                    None => {
                        // Wire desync: the 4 bytes we just decoded don't map to any
                        // known opcode. Log the last frame that DID parse cleanly —
                        // the suspect Go-side encoder is the opcode IMMEDIATELY
                        // AFTER that one (it wrote more or fewer bytes than the
                        // Rust apply consumed, so the cursor landed in the middle
                        // of a payload or past the end of a frame, and the next
                        // msg_len read picked up garbage). The 16-deep trail
                        // below shows the approach. The desync_already_logged
                        // flag suppresses the cascade from `let _ =
                        // self.interpret_outer(...)` closures (e.g. line 2362)
                        // that silently drop inner errors and re-enter the
                        // broken stream — exactly one rich log per failure.
                        if !self.desync_already_logged {
                            let last_good = FuncProcId::from_repr(
                                self.last_good_func_proc_id_raw.unwrap_or(u32::MAX),
                            );
                            let trail = self.render_frame_trail();
                            let stack = self.render_message_stack();
                            tracing::error!(
                                raw_opcode = raw,
                                raw_opcode_hex = format!("0x{:08x}", raw),
                                msg_len = msg_len,
                                msg_len_byte_offset = msg_len_offset,
                                opcode_byte_offset = offset,
                                live_cursor = self.io.read_bytes_count,
                                in_flight_depth = self.message_offsets.len(),
                                last_good_func_proc_id_raw = ?self.last_good_func_proc_id_raw,
                                last_good_msg_len = ?self.last_good_msg_len,
                                last_good_byte_offset = ?self.last_good_byte_offset,
                                last_good_name = ?last_good,
                                frame_trail_count = self.frame_trail_count,
                                "FFFI wire desync: opcode 0x{:08x} ({}) does not decode to a known FuncProcId. \
                                 The Go-side encoder of the opcode AFTER {:?} (the trail's last entry) is the suspect.\n  Recent clean frames (oldest → newest):{}\n  In-flight frames (begun but not ended — non-empty = an earlier apply errored mid-frame and is the real cause):{}",
                                raw, raw, last_good, trail, stack,
                            );
                            self.desync_already_logged = true;
                        }
                        return Err(InterpretError::Fffi(FffiError::FromRepr(raw)));
                    }
                };
                let func_proc_id_raw = raw;

                self.message_lengths.push(msg_len);
                self.message_offsets.push(offset);
                self.message_func_proc_ids_raw.push(func_proc_id_raw);
                let depth = self.message_offsets.len();
                tracing::trace!(
                    msg_len = msg_len,
                    depth = depth,
                    func_proc_id_raw = func_proc_id_raw,
                    "handling msg func_proc_id {:?}({})",
                    func_proc_id,
                    func_proc_id_raw
                );
                Ok(Some(func_proc_id))
            }
            Err(FffiError::Io(ref e))
                if self.io.is_replaying() && e.kind() == std::io::ErrorKind::UnexpectedEof =>
            {
                // Clean exit: deferred block buffer exhausted
                Ok(None)
            }
            Err(FffiError::Io(ref e)) if e.kind() == std::io::ErrorKind::UnexpectedEof => {
                // Peer closed the pipe — graceful shutdown signal, not a panic.
                Err(InterpretError::PeerClosed)
            }
            Err(e) => Err(InterpretError::Fffi(e)),
        }
    }
    pub fn end_consume_message(&mut self) -> InterpretResult<()> {
        let after = self.io.read_bytes_count;
        let func_proc_id_raw = self
            .message_func_proc_ids_raw
            .pop()
            .ok_or(InterpretError::FrameStackUnderflow("func_proc_id_raw"))?;
        let frame_len =
            self.message_lengths.pop().ok_or(InterpretError::FrameStackUnderflow("frame_len"))?
                as usize;
        let frame_offset = self
            .message_offsets
            .pop()
            .ok_or(InterpretError::FrameStackUnderflow("frame_offset"))?;
        let consumed = after - frame_offset;
        if consumed != frame_len {
            let func_proc_id = FuncProcId::from_repr(func_proc_id_raw);
            if consumed < frame_len {
                let delta = frame_len - consumed;
                if !self.desync_already_logged {
                    let trail = self.render_frame_trail();
                    let stack = self.render_message_stack();
                    tracing::warn!(
                        func_proc_id_raw = func_proc_id_raw,
                        consumed = consumed,
                        frame_len = frame_len,
                        frame_offset = frame_offset,
                        delta_skipped = delta,
                        live_cursor = self.io.read_bytes_count,
                        in_flight_depth = self.message_offsets.len(),
                        last_good_func_proc_id_raw = ?self.last_good_func_proc_id_raw,
                        last_good_msg_len = ?self.last_good_msg_len,
                        frame_trail_count = self.frame_trail_count,
                        "FFFI underread: {:?} consumed {} of {} declared bytes — skipping {} unread bytes. \
                         Go-side encoder for {:?} writes more bytes than the Rust apply reads.\n  Recent clean frames (oldest → newest):{}\n  In-flight frames (begun but not ended):{}",
                        func_proc_id, consumed, frame_len, delta, func_proc_id, trail, stack,
                    );
                    self.desync_already_logged = true;
                }
                self.io.skip(delta)?;
            } else {
                let overshoot = consumed - frame_len;
                if !self.desync_already_logged {
                    let trail = self.render_frame_trail();
                    let stack = self.render_message_stack();
                    tracing::error!(
                        func_proc_id_raw = func_proc_id_raw,
                        consumed = consumed,
                        frame_len = frame_len,
                        frame_offset = frame_offset,
                        overshoot = overshoot,
                        live_cursor = self.io.read_bytes_count,
                        in_flight_depth = self.message_offsets.len(),
                        last_good_func_proc_id_raw = ?self.last_good_func_proc_id_raw,
                        last_good_msg_len = ?self.last_good_msg_len,
                        frame_trail_count = self.frame_trail_count,
                        "FFFI overshoot: {:?} consumed {} bytes past its {}-byte frame. \
                         Either the Go-side encoder writes fewer bytes than the Rust apply reads, \
                         or this is downstream junk after a desync that started at last_good={:?}.\n  Recent clean frames (oldest → newest):{}\n  In-flight frames (begun but not ended — non-empty = an earlier apply errored mid-frame; THAT opcode is the real cause):{}",
                        func_proc_id, overshoot, frame_len, FuncProcId::from_repr(
                            self.last_good_func_proc_id_raw.unwrap_or(u32::MAX),
                        ),
                        trail, stack,
                    );
                    self.desync_already_logged = true;
                }
                return Err(InterpretError::FrameOvershoot {
                    func_proc_id_raw,
                    consumed,
                    overshoot,
                });
            }
        } else {
            // Clean match: this frame parsed exactly. Record it as the
            // last known-good landmark for diagnostics on the next
            // desync (see begin_consume_message) AND push to the ring
            // buffer so we can dump the recent trail on the next error.
            self.last_good_func_proc_id_raw = Some(func_proc_id_raw);
            self.last_good_msg_len = Some(frame_len as u32);
            self.last_good_byte_offset = Some(frame_offset);
            self.frame_trail[self.frame_trail_head] =
                (func_proc_id_raw, frame_len as u32, frame_offset);
            self.frame_trail_head = (self.frame_trail_head + 1) % self.frame_trail.len();
            self.frame_trail_count += 1;
            // A clean frame after a previous desync re-arms the
            // cascade-suppression flag so the next genuine desync
            // dumps a fresh trail.
            self.desync_already_logged = false;
        }
        Ok(())
    }
    /// Format the frame_trail ring buffer in chronological order
    /// (oldest first). Called from desync sites to give the operator
    /// a 16-deep view of what was parsed cleanly just before the
    /// failure — the Go-side encoder of the opcode IMMEDIATELY AFTER
    /// the last entry is the suspect.
    fn render_frame_trail(&self) -> String {
        let n = self.frame_trail.len();
        let total = self.frame_trail_count as usize;
        let count = total.min(n);
        if count == 0 {
            return "(empty — desync occurred before any frame parsed cleanly)".to_string();
        }
        let start = if total < n { 0 } else { self.frame_trail_head };
        let mut out = String::with_capacity(count * 64);
        for i in 0..count {
            let idx = (start + i) % n;
            let (raw, msg_len, offset) = self.frame_trail[idx];
            let name = FuncProcId::from_repr(raw);
            out.push_str(&format!(
                "\n    [{:>2}] offset={:<7} msg_len={:<6} opcode={:<3} {:?}",
                total - count + i,
                offset,
                msg_len,
                raw,
                name,
            ));
        }
        out
    }
    /// Format the begin/end_consume_message stack — frames that have
    /// been BEGUN but not yet ENDED. Healthy steady state is 1 entry
    /// (the currently-dispatching opcode) or 0 (between top-level
    /// frames). Anything deeper means an apply errored before
    /// `if d == 0 { self.end_consume_message()? }` ran, leaving a
    /// stale entry — that earlier opcode is the actual cause of the
    /// gap between last_good and the reported desync site.
    fn render_message_stack(&self) -> String {
        let depth = self.message_offsets.len();
        if depth == 0 {
            return "(empty)".to_string();
        }
        let mut out = String::with_capacity(depth * 64);
        for i in 0..depth {
            let raw = self.message_func_proc_ids_raw[i];
            let msg_len = self.message_lengths[i];
            let offset = self.message_offsets[i];
            let name = FuncProcId::from_repr(raw);
            out.push_str(&format!(
                "\n    [{}] offset={:<7} msg_len={:<6} opcode={:<3} {:?}",
                i, offset, msg_len, raw, name,
            ));
        }
        out
    }
    pub fn interpret_outer(
        &mut self,
        c: &egui::Context,
        u: &mut Option<&mut egui::Ui>,
    ) -> InterpretResult<()> {
        loop {
            let Some(func_proc_id) = self.begin_consume_message(FuncProcId::from_repr)? else {
                return Ok(());
            };
            if self.interpret_inner(c, u, &func_proc_id, 0)? {
                return Ok(());
            }
        }
    }
    /// Wrapper around `interpret_outer` for the ~30 block-iterator call
    /// sites whose return value cannot propagate (`let _ = self
    /// .interpret_outer(...)` — egui APIs like `ui.horizontal(|ui| {…})`,
    /// `Window::show(c, |ui| {…})`, `parent_ui.allocate_ui_at_rect(…,
    /// |ui| {…})` etc. take closures with no `Result` return). Without
    /// this wrapper any inner protocol error is dropped silently and
    /// the next outer iteration reads a misaligned cursor — exactly the
    /// failure mode the boxer.facts desync exposed. Codegen
    /// (`egui2_definition_d_blocks.go`) emits this wrapper at every
    /// such site so the inner error always surfaces.
    ///
    /// Cascade-suppression is shared with `begin/end_consume_message`:
    /// if the inner failure already logged a rich diagnostic (FromRepr
    /// / overshoot / underread), we don't duplicate it. `PeerClosed` is
    /// an expected shutdown signal and never logs.
    pub fn interpret_outer_logged(
        &mut self,
        c: &egui::Context,
        u: &mut Option<&mut egui::Ui>,
    ) -> InterpretResult<()> {
        let r = self.interpret_outer(c, u);
        if let Err(ref e) = r {
            match e {
                InterpretError::PeerClosed => {}
                _ => {
                    if !self.desync_already_logged {
                        tracing::error!(
                            error = %e,
                            in_flight_depth = self.message_offsets.len(),
                            live_cursor = self.io.read_bytes_count,
                            "interpret_outer error in a silent-drop closure (let _ =) — \
                             the caller cannot propagate; logging here so the cause is visible. \
                             For protocol-level context see the preceding FFFI desync log."
                        );
                        self.desync_already_logged = true;
                    }
                }
            }
        }
        r
    }
    /// Replay a captured deferred opcode block in the given Ui context.
    ///
    /// Sets the io overlay reader to a Cursor over the block bytes,
    /// runs interpret_outer (which reads opcodes from the Cursor and
    /// renders widgets into the provided Ui), then clears the overlay.
    ///
    /// Widget responses (r7, r9, r10) accumulate in the normal registers
    /// and are returned to Go via the standard Fetch* mechanism.
    pub fn replay_deferred_block(
        &mut self,
        ctx: &egui::Context,
        ui: &mut egui::Ui,
        block: &[u8],
    ) -> InterpretResult<()> {
        if block.is_empty() {
            return Ok(());
        }
        self.io.begin_replay(block);
        // Capture so end_replay() runs even on Err — the replay overlay state
        // must be cleaned up regardless of whether dispatch propagated an error.
        let r = self.interpret_outer(ctx, &mut Some(ui));
        self.io.end_replay();
        r
    }
    /// Logged variant of `replay_deferred_block` — same wrapping rationale
    /// as `interpret_outer_logged`, for the on-hover-tip / collapsible
    /// callback sites that emit `let _ = self.replay_deferred_block(...)`.
    pub fn replay_deferred_block_logged(
        &mut self,
        ctx: &egui::Context,
        ui: &mut egui::Ui,
        block: &[u8],
    ) -> InterpretResult<()> {
        let r = self.replay_deferred_block(ctx, ui, block);
        if let Err(ref e) = r {
            match e {
                InterpretError::PeerClosed => {}
                _ => {
                    if !self.desync_already_logged {
                        tracing::error!(
                            error = %e,
                            block_len = block.len(),
                            "replay_deferred_block error in a silent-drop closure (let _ =) — \
                             the captured opcode block did not parse cleanly."
                        );
                        self.desync_already_logged = true;
                    }
                }
            }
        }
        r
    }

    // TODO use https://docs.rs/recursive/latest/recursive/?
    pub fn interpret_inner<'b>(
        &mut self,
        c: &egui::Context,
        u: &mut Option<&mut egui::Ui>,
        f: &FuncProcId,
        d: u32,
    ) -> InterpretResult<bool> {
        #[cfg(feature = "puffin")]
        puffin::profile_function!();
        let mut r = false;
        //IMZERO2_INCLUDE_FFFI_DISPATCH_OUT
        /*--------------------- //IMZERO2_INCLUDE_FFFI_DISPATCH_OUT -----------------------*/
        // Code generated by TheStack (github.com/stergiotis/boxer/public/app); DO NOT EDIT.

        match f {
            FuncProcId::AddSpace => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::AddSpace");
                // arguments
                #[allow(unused_mut)]
                let mut amount = self.io.read_plain_f32()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().add_space(amount);
                }
            }
            FuncProcId::AllocateUiAtRect => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::AllocateUiAtRect");
                // arguments
                #[allow(unused_mut)]
                let mut min_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut min_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut max_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut max_y = self.io.read_plain_f32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let parent_ui = u.as_mut().unwrap();
                    let origin = parent_ui.min_rect().min;
                    parent_ui.scope_builder(
                        egui::UiBuilder::new().max_rect(egui::Rect::from_min_max(
                            egui::pos2(origin.x + min_x, origin.y + min_y),
                            egui::pos2(origin.x + max_x, origin.y + max_y),
                        )),
                        |ui| {
                            let _ = self.interpret_outer_logged(c, &mut Some(ui));
                        },
                    );
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::AnimateBoolResponsive => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::AnimateBoolResponsive");
                // arguments
                #[allow(unused_mut)]
                let mut anim_id = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut target = self.io.read_plain_b()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let aid = egui::Id::new(anim_id);
                let val = if self.animation_freeze {
                    if target { 1.0f32 } else { 0.0f32 }
                } else {
                    c.animate_bool_responsive(aid, target)
                };
                self.r9_f64_push(anim_id, val as f64);
            }
            FuncProcId::AnimateBoolWithTime => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::AnimateBoolWithTime");
                // arguments
                #[allow(unused_mut)]
                let mut anim_id = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut target = self.io.read_plain_b()?;
                #[allow(unused_mut)]
                let mut dur_secs = self.io.read_plain_f32()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let aid = egui::Id::new(anim_id);
                let val = if self.animation_freeze {
                    if target { 1.0f32 } else { 0.0f32 }
                } else {
                    c.animate_bool_with_time(aid, target, dur_secs)
                };
                self.r9_f64_push(anim_id, val as f64);
            }
            FuncProcId::AnimateValueWithTime => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::AnimateValueWithTime");
                // arguments
                #[allow(unused_mut)]
                let mut anim_id = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut target = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut dur_secs = self.io.read_plain_f32()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let aid = egui::Id::new(anim_id);
                let val = if self.animation_freeze {
                    target
                } else {
                    c.animate_value_with_time(aid, target, dur_secs)
                };
                self.r9_f64_push(anim_id, val as f64);
            }
            FuncProcId::Atoms => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Atoms");
                // arguments
                // construct
                // methods
                loop {
                    let (m, _) = self.read_from_repr(AtomsBuilderMethodId::from_repr)?;
                    match m {
                        AtomsBuilderMethodId::Build => {
                            break;
                        }
                        AtomsBuilderMethodId::Text => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::Text");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_s()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            self.r0_atoms.push_right(val);
                        }
                        AtomsBuilderMethodId::RichText => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::RichText");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_s()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                            {
                                let mut rt = egui::RichText::new(val);
                                loop {
                                    let (m2, _) =
                                        self.read_from_repr(AtomsBuilderMethodId::from_repr)?;
                                    match m2 {
                                        AtomsBuilderMethodId::EndRichText => {
                                            self.r0_atoms.push_right(rt);
                                            break;
                                        }
                                        AtomsBuilderMethodId::Size => {
                                            let sz = self.io.read_plain_f32()?;
                                            rt = rt.size(sz);
                                        }
                                        AtomsBuilderMethodId::ExtraLetterSpacing => {
                                            let sp = self.io.read_plain_f32()?;
                                            rt = rt.extra_letter_spacing(sp);
                                        }
                                        AtomsBuilderMethodId::LineHeight => {
                                            let lh = self.io.read_plain_f32()?;
                                            rt = rt.line_height(Some(lh));
                                        }
                                        AtomsBuilderMethodId::LineHeightDefault => {
                                            rt = rt.line_height(None);
                                        }
                                        AtomsBuilderMethodId::Heading => {
                                            rt = rt.heading();
                                        }
                                        AtomsBuilderMethodId::Monospace => {
                                            rt = rt.monospace();
                                        }
                                        AtomsBuilderMethodId::Code => {
                                            rt = rt.code();
                                        }
                                        AtomsBuilderMethodId::Strong => {
                                            rt = rt.strong();
                                        }
                                        AtomsBuilderMethodId::Weak => {
                                            rt = rt.weak();
                                        }
                                        AtomsBuilderMethodId::Underline => {
                                            rt = rt.underline();
                                        }
                                        AtomsBuilderMethodId::Strikethrough => {
                                            rt = rt.strikethrough();
                                        }
                                        AtomsBuilderMethodId::Italics => {
                                            rt = rt.italics();
                                        }
                                        AtomsBuilderMethodId::Small => {
                                            rt = rt.small();
                                        }
                                        AtomsBuilderMethodId::SmallRaised => {
                                            rt = rt.small_raised();
                                        }
                                        AtomsBuilderMethodId::Raised => {
                                            rt = rt.raised();
                                        }
                                        AtomsBuilderMethodId::TextStyleName => {
                                            // egui's TextStyle::Name(Arc<str>) slot — addresses any
                                            // custom text style the host's apply path may have written
                                            // into Style::text_styles (e.g., IDS's "ids-display" /
                                            // "ids-micro" tiers per ADR-0030 §SD3).
                                            let name = self.io.read_plain_s()?;
                                            rt = rt.text_style(egui::TextStyle::Name(name.into()));
                                        }
                                        AtomsBuilderMethodId::Text => {
                                            // Self-heal: a plain-text atom arriving inside an unclosed rich
                                            // segment means the caller skipped endRichText (the classic
                                            // RichTextColored(...).Text(...) desync footgun). Close the
                                            // segment implicitly so its content is not lost, then push the
                                            // text as its own atom. Byte-safe: we consume Text's string arg
                                            // here, so the outer loop stays aligned and the frame does not
                                            // crash. The Go-side sub-protocol methods are unexported, so this
                                            // is unreachable from normal code — it guards hand-written raw
                                            // streams and future regressions.
                                            self.r0_atoms.push_right(rt);
                                            let v = self.io.read_plain_s()?;
                                            self.r0_atoms.push_right(v);
                                            break;
                                        }
                                        _ => {
                                            // Any other out-of-scope method: preserve the accumulated segment
                                            // and stop. Not guaranteed byte-aligned if the method carried args,
                                            // but this path is unreachable from the (unexported) safe Go API.
                                            self.r0_atoms.push_right(rt);
                                            tracing::warn!(
                                                "unexpected method {:?} inside richText sub-loop — closing segment",
                                                m2
                                            );
                                            break;
                                        }
                                    }
                                }
                            }
                        }
                        AtomsBuilderMethodId::RichTextColored => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::RichTextColored");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_s()?;

                            let cl = {
                                let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                                let u2: &mut Option<&mut egui::Ui> = &mut None;
                                if u2.is_some() {
                                    self.interpret_inner(c, u2, &f2, d + 1)?;
                                } else {
                                    self.interpret_inner(c, u, &f2, d + 1)?;
                                }

                                self.r11_color32
                            };

                            let bk = {
                                let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                                let u2: &mut Option<&mut egui::Ui> = &mut None;
                                if u2.is_some() {
                                    self.interpret_inner(c, u2, &f2, d + 1)?;
                                } else {
                                    self.interpret_inner(c, u, &f2, d + 1)?;
                                }

                                self.r11_color32
                            };
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                            {
                                let mut rt =
                                    egui::RichText::new(val).color(cl).background_color(bk);
                                loop {
                                    let (m2, _) =
                                        self.read_from_repr(AtomsBuilderMethodId::from_repr)?;
                                    match m2 {
                                        AtomsBuilderMethodId::EndRichText => {
                                            self.r0_atoms.push_right(rt);
                                            break;
                                        }
                                        AtomsBuilderMethodId::Size => {
                                            let sz = self.io.read_plain_f32()?;
                                            rt = rt.size(sz);
                                        }
                                        AtomsBuilderMethodId::ExtraLetterSpacing => {
                                            let sp = self.io.read_plain_f32()?;
                                            rt = rt.extra_letter_spacing(sp);
                                        }
                                        AtomsBuilderMethodId::LineHeight => {
                                            let lh = self.io.read_plain_f32()?;
                                            rt = rt.line_height(Some(lh));
                                        }
                                        AtomsBuilderMethodId::LineHeightDefault => {
                                            rt = rt.line_height(None);
                                        }
                                        AtomsBuilderMethodId::Heading => {
                                            rt = rt.heading();
                                        }
                                        AtomsBuilderMethodId::Monospace => {
                                            rt = rt.monospace();
                                        }
                                        AtomsBuilderMethodId::Code => {
                                            rt = rt.code();
                                        }
                                        AtomsBuilderMethodId::Strong => {
                                            rt = rt.strong();
                                        }
                                        AtomsBuilderMethodId::Weak => {
                                            rt = rt.weak();
                                        }
                                        AtomsBuilderMethodId::Underline => {
                                            rt = rt.underline();
                                        }
                                        AtomsBuilderMethodId::Strikethrough => {
                                            rt = rt.strikethrough();
                                        }
                                        AtomsBuilderMethodId::Italics => {
                                            rt = rt.italics();
                                        }
                                        AtomsBuilderMethodId::Small => {
                                            rt = rt.small();
                                        }
                                        AtomsBuilderMethodId::SmallRaised => {
                                            rt = rt.small_raised();
                                        }
                                        AtomsBuilderMethodId::Raised => {
                                            rt = rt.raised();
                                        }
                                        AtomsBuilderMethodId::TextStyleName => {
                                            // egui's TextStyle::Name(Arc<str>) slot — addresses any
                                            // custom text style the host's apply path may have written
                                            // into Style::text_styles (e.g., IDS's "ids-display" /
                                            // "ids-micro" tiers per ADR-0030 §SD3).
                                            let name = self.io.read_plain_s()?;
                                            rt = rt.text_style(egui::TextStyle::Name(name.into()));
                                        }
                                        AtomsBuilderMethodId::Text => {
                                            // Self-heal: see the richText sub-loop above. Close the colored
                                            // segment implicitly (don't lose it), consume Text's string arg
                                            // to stay byte-aligned, and push it as its own atom instead of
                                            // crashing the frame.
                                            self.r0_atoms.push_right(rt);
                                            let v = self.io.read_plain_s()?;
                                            self.r0_atoms.push_right(v);
                                            break;
                                        }
                                        _ => {
                                            // Any other out-of-scope method: preserve the accumulated segment
                                            // and stop. Not guaranteed byte-aligned if the method carried args,
                                            // but this path is unreachable from the (unexported) safe Go API.
                                            self.r0_atoms.push_right(rt);
                                            tracing::warn!(
                                                "unexpected method {:?} inside richTextColored sub-loop — closing segment",
                                                m2
                                            );
                                            break;
                                        }
                                    }
                                }
                            }
                        }
                        AtomsBuilderMethodId::EndRichText => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::EndRichText");
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::Size => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::Size");
                            #[allow(unused_mut)]
                            let mut sz = self.io.read_plain_f32()?;
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::ExtraLetterSpacing => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match AtomsBuilderMethodId::ExtraLetterSpacing"
                            );
                            #[allow(unused_mut)]
                            let mut sp = self.io.read_plain_f32()?;
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::LineHeight => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::LineHeight");
                            #[allow(unused_mut)]
                            let mut lh = self.io.read_plain_f32()?;
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::LineHeightDefault => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::LineHeightDefault");
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::Heading => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::Heading");
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::Monospace => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::Monospace");
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::Code => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::Code");
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::Strong => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::Strong");
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::Weak => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::Weak");
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::Underline => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::Underline");
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::Strikethrough => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::Strikethrough");
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::Italics => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::Italics");
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::Small => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::Small");
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::SmallRaised => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::SmallRaised");
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::Raised => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::Raised");
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                        AtomsBuilderMethodId::TextStyleName => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match AtomsBuilderMethodId::TextStyleName");
                            #[allow(unused_mut)]
                            let mut name = self.io.read_plain_s()?;
                            tracing::warn!(
                                "rich text style method called outside richText/endRichText scope, ignoring"
                            );
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
            }
            FuncProcId::Button => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Button");
                // arguments
                let i = self.read_id()?;

                let atoms = {
                    let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                    let u2: &mut Option<&mut egui::Ui> = &mut None;
                    if u2.is_some() {
                        self.interpret_inner(c, u2, &f2, d + 1)?;
                    } else {
                        self.interpret_inner(c, u, &f2, d + 1)?;
                    }

                    std::mem::take(&mut self.r0_atoms)
                };
                // construct

                #[allow(unused_mut)]
                let mut w = egui::Button::new(atoms);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(ButtonBuilderMethodId::from_repr)?;
                    match m {
                        ButtonBuilderMethodId::Build => {
                            break;
                        }
                        ButtonBuilderMethodId::Frame => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ButtonBuilderMethodId::Frame");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.frame(val);
                        }
                        ButtonBuilderMethodId::Small => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ButtonBuilderMethodId::Small");
                            w = w.small();
                        }
                        ButtonBuilderMethodId::Wrap => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ButtonBuilderMethodId::Wrap");
                            w = w.wrap();
                        }
                        ButtonBuilderMethodId::Truncate => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ButtonBuilderMethodId::Truncate");
                            w = w.truncate();
                        }
                        ButtonBuilderMethodId::Selected => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ButtonBuilderMethodId::Selected");
                            #[allow(unused_mut)]
                            let mut selected = self.io.read_plain_b()?;
                            w = w.selected(selected);
                        }
                        ButtonBuilderMethodId::FrameWhenInactive => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match ButtonBuilderMethodId::FrameWhenInactive"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.frame_when_inactive(val);
                        }
                        ButtonBuilderMethodId::RightText => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ButtonBuilderMethodId::RightText");
                            #[allow(unused_mut)]
                            let mut text = self.io.read_plain_s()?;
                            w = w.right_text(text);
                        }
                        ButtonBuilderMethodId::ShortcutText => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ButtonBuilderMethodId::ShortcutText");
                            #[allow(unused_mut)]
                            let mut text = self.io.read_plain_s()?;
                            w = w.shortcut_text(text);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.apply_widget(w, u, f, Some(i));
            }
            FuncProcId::CaptureAvailableSize => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::CaptureAvailableSize");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let ui = u.as_mut().unwrap();
                    let s = ui.available_size();
                    self.r18_avail_w = s.x;
                    self.r18_avail_h = s.y;
                } else {
                    self.r18_avail_w = f32::NAN;
                    self.r18_avail_h = f32::NAN;
                }
            }
            FuncProcId::CaptureUiAvailableRect => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::CaptureUiAvailableRect");
                // arguments
                #[allow(unused_mut)]
                let mut seq = self.io.read_plain_u64()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let ui = u.as_mut().unwrap();
                    let r = ui.available_rect_before_wrap();
                    self.r21_ui_rect_seqs.push(seq);
                    self.r21_ui_rect_min_x.push(r.min.x);
                    self.r21_ui_rect_min_y.push(r.min.y);
                    self.r21_ui_rect_max_x.push(r.max.x);
                    self.r21_ui_rect_max_y.push(r.max.y);
                    debug_assert_eq!(self.r21_ui_rect_seqs.len(), self.r21_ui_rect_min_x.len());
                    debug_assert_eq!(self.r21_ui_rect_seqs.len(), self.r21_ui_rect_min_y.len());
                    debug_assert_eq!(self.r21_ui_rect_seqs.len(), self.r21_ui_rect_max_x.len());
                    debug_assert_eq!(self.r21_ui_rect_seqs.len(), self.r21_ui_rect_max_y.len());
                }
            }
            FuncProcId::CaptureUiRect => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::CaptureUiRect");
                // arguments
                #[allow(unused_mut)]
                let mut seq = self.io.read_plain_u64()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let ui = u.as_mut().unwrap();
                    let r = ui.min_rect();
                    self.r21_ui_rect_seqs.push(seq);
                    self.r21_ui_rect_min_x.push(r.min.x);
                    self.r21_ui_rect_min_y.push(r.min.y);
                    self.r21_ui_rect_max_x.push(r.max.x);
                    self.r21_ui_rect_max_y.push(r.max.y);
                    debug_assert_eq!(self.r21_ui_rect_seqs.len(), self.r21_ui_rect_min_x.len());
                    debug_assert_eq!(self.r21_ui_rect_seqs.len(), self.r21_ui_rect_min_y.len());
                    debug_assert_eq!(self.r21_ui_rect_seqs.len(), self.r21_ui_rect_max_x.len());
                    debug_assert_eq!(self.r21_ui_rect_seqs.len(), self.r21_ui_rect_max_y.len());
                }
            }
            FuncProcId::Checkbox => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Checkbox");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut checked = self.io.read_plain_b()?;
                #[allow(unused_mut)]
                let mut text = self.io.read_plain_s()?;
                // construct

                #[allow(unused_mut)]
                let mut w = egui::Checkbox::new(&mut checked, text);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(CheckboxBuilderMethodId::from_repr)?;
                    match m {
                        CheckboxBuilderMethodId::Build => {
                            break;
                        }
                        CheckboxBuilderMethodId::Indeterminate => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match CheckboxBuilderMethodId::Indeterminate");
                            #[allow(unused_mut)]
                            let mut indeterminate = self.io.read_plain_b()?;
                            w = w.indeterminate(indeterminate);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                let resp =
// generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
self.apply_widget(w,u,f,Some(i));
                if resp.is_some() && resp.unwrap().changed() {
                    // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                    self.r10_push(i.value(), checked);
                }
            }
            FuncProcId::CodeView => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::CodeView");
                // arguments
                let i = self.read_id()?;

                let job = {
                    let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                    let u2: &mut Option<&mut egui::Ui> = &mut None;
                    if u2.is_some() {
                        self.interpret_inner(c, u2, &f2, d + 1)?;
                    } else {
                        self.interpret_inner(c, u, &f2, d + 1)?;
                    }

                    std::mem::take(&mut self.r12_code_view_job)
                };
                // construct

                #[allow(unused_mut)]
                let mut w = {
                    let layout_job =
                        code_view::get_or_build_layout_job(&mut self.code_view_cache, &job, c);
                    egui::Label::new(layout_job).selectable(true)
                };
                // methods
                loop {
                    let (m, _) = self.read_from_repr(CodeViewBuilderMethodId::from_repr)?;
                    match m {
                        CodeViewBuilderMethodId::Build => {
                            break;
                        }
                        CodeViewBuilderMethodId::Selectable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match CodeViewBuilderMethodId::Selectable");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.selectable(val);
                        }
                        CodeViewBuilderMethodId::Wrap => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match CodeViewBuilderMethodId::Wrap");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.wrap_mode(egui::TextWrapMode::Wrap);
                        }
                        CodeViewBuilderMethodId::Truncate => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match CodeViewBuilderMethodId::Truncate");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.wrap_mode(egui::TextWrapMode::Truncate);
                        }
                        CodeViewBuilderMethodId::Extend => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match CodeViewBuilderMethodId::Extend");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.wrap_mode(egui::TextWrapMode::Extend);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.apply_widget(w, u, f, Some(i));
            }
            FuncProcId::CodeViewJob => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::CodeViewJob");
                // arguments
                #[allow(unused_mut)]
                let mut text = self.io.read_plain_s()?;
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
{
	self.r12_code_view_job.sections.clear();
	self.r12_code_view_job.text = text;
	()
};
                // methods
                loop {
                    let (m, _) = self.read_from_repr(CodeViewJobBuilderMethodId::from_repr)?;
                    match m {
                        CodeViewJobBuilderMethodId::Build => {
                            break;
                        }
                        CodeViewJobBuilderMethodId::Section => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match CodeViewJobBuilderMethodId::Section");
                            #[allow(unused_mut)]
                            let mut byte_start = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut byte_stop = self.io.read_plain_u32()?;

                            let col = {
                                let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                                let u2: &mut Option<&mut egui::Ui> = &mut None;
                                if u2.is_some() {
                                    self.interpret_inner(c, u2, &f2, d + 1)?;
                                } else {
                                    self.interpret_inner(c, u, &f2, d + 1)?;
                                }

                                self.r11_color32
                            };
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            self.r12_code_view_job.sections.push(code_view::Section {
                                byte_start,
                                byte_stop,
                                color: col,
                            });
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
            }
            FuncProcId::CollapsingHeader => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::CollapsingHeader");
                // arguments
                let i = self.read_id()?;

                let label = {
                    let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                    let u2: &mut Option<&mut egui::Ui> = &mut None;
                    if u2.is_some() {
                        self.interpret_inner(c, u2, &f2, d + 1)?;
                    } else {
                        self.interpret_inner(c, u, &f2, d + 1)?;
                    }

                    std::mem::take(&mut self.r1_widget_text)
                };
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
egui::CollapsingHeader::new(label).id_salt(i);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(CollapsingHeaderBuilderMethodId::from_repr)?;
                    match m {
                        CollapsingHeaderBuilderMethodId::Build => {
                            break;
                        }
                        CollapsingHeaderBuilderMethodId::DefaultOpen => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match CollapsingHeaderBuilderMethodId::DefaultOpen"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.default_open(val);
                        }
                        CollapsingHeaderBuilderMethodId::Open => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match CollapsingHeaderBuilderMethodId::Open");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.open(Some(true))
                        }
                        CollapsingHeaderBuilderMethodId::Close => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match CollapsingHeaderBuilderMethodId::Close");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.open(Some(false))
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    if w.show(u.as_mut().unwrap(), |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    })
                    .body_returned
                    .is_none()
                    {
                        self.r7_push(i.value(), ResponseFlags::BLOCK_SKIPPED);
                        self.interpret_outer(c, &mut None)?;
                    }
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::Color => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Color");
                // arguments
                // construct

                #[allow(unused_mut)]
                let mut w = egui::Color32::TRANSPARENT;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(ColorBuilderMethodId::from_repr)?;
                    match m {
                        ColorBuilderMethodId::Build => {
                            break;
                        }
                        ColorBuilderMethodId::FromRgb => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::FromRgb");
                            #[allow(unused_mut)]
                            let mut rv = self.io.read_plain_u8()?;
                            #[allow(unused_mut)]
                            let mut gv = self.io.read_plain_u8()?;
                            #[allow(unused_mut)]
                            let mut bv = self.io.read_plain_u8()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::from_rgb(rv, gv, bv);
                        }
                        ColorBuilderMethodId::FromRgbaUnmultiplied => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match ColorBuilderMethodId::FromRgbaUnmultiplied"
                            );
                            #[allow(unused_mut)]
                            let mut rv = self.io.read_plain_u8()?;
                            #[allow(unused_mut)]
                            let mut gv = self.io.read_plain_u8()?;
                            #[allow(unused_mut)]
                            let mut bv = self.io.read_plain_u8()?;
                            #[allow(unused_mut)]
                            let mut av = self.io.read_plain_u8()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::from_rgba_unmultiplied(rv, gv, bv, av);
                        }
                        ColorBuilderMethodId::FromRgbaPremultiplied => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match ColorBuilderMethodId::FromRgbaPremultiplied"
                            );
                            #[allow(unused_mut)]
                            let mut rv = self.io.read_plain_u8()?;
                            #[allow(unused_mut)]
                            let mut gv = self.io.read_plain_u8()?;
                            #[allow(unused_mut)]
                            let mut bv = self.io.read_plain_u8()?;
                            #[allow(unused_mut)]
                            let mut av = self.io.read_plain_u8()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::from_rgba_premultiplied(rv, gv, bv, av);
                        }
                        ColorBuilderMethodId::FromGray => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::FromGray");
                            #[allow(unused_mut)]
                            let mut lv = self.io.read_plain_u8()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::from_gray(lv);
                        }
                        ColorBuilderMethodId::FromBlackAlpha => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::FromBlackAlpha");
                            #[allow(unused_mut)]
                            let mut av = self.io.read_plain_u8()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::from_black_alpha(av);
                        }
                        ColorBuilderMethodId::GammaMultiplyU8 => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::GammaMultiplyU8");
                            #[allow(unused_mut)]
                            let mut factor = self.io.read_plain_u8()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.gamma_multiply_u8(factor);
                        }
                        ColorBuilderMethodId::GammaMultiplyF32 => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::GammaMultiplyF32");
                            #[allow(unused_mut)]
                            let mut factor = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.gamma_multiply(factor);
                        }
                        ColorBuilderMethodId::LinearMultiplyF32 => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::LinearMultiplyF32");
                            #[allow(unused_mut)]
                            let mut factor = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.linear_multiply(factor);
                        }
                        ColorBuilderMethodId::ToOpaque => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ToOpaque");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.to_opaque();
                        }
                        ColorBuilderMethodId::ColorTransparent => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorTransparent");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::TRANSPARENT;
                        }
                        ColorBuilderMethodId::ColorBlack => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorBlack");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::BLACK;
                        }
                        ColorBuilderMethodId::ColorDarkGray => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorDarkGray");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::DARK_GRAY;
                        }
                        ColorBuilderMethodId::ColorGray => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorGray");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::GRAY;
                        }
                        ColorBuilderMethodId::ColorLightGray => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorLightGray");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::LIGHT_GRAY;
                        }
                        ColorBuilderMethodId::ColorWhite => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorWhite");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::WHITE;
                        }
                        ColorBuilderMethodId::ColorBrown => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorBrown");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::BROWN;
                        }
                        ColorBuilderMethodId::ColorDarkRed => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorDarkRed");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::DARK_RED;
                        }
                        ColorBuilderMethodId::ColorLightRed => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorLightRed");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::LIGHT_RED;
                        }
                        ColorBuilderMethodId::ColorCyan => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorCyan");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::CYAN;
                        }
                        ColorBuilderMethodId::ColorMagenta => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorMagenta");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::MAGENTA;
                        }
                        ColorBuilderMethodId::ColorYellow => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorYellow");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::YELLOW;
                        }
                        ColorBuilderMethodId::ColorOrange => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorOrange");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::ORANGE;
                        }
                        ColorBuilderMethodId::ColorLightYellow => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorLightYellow");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::LIGHT_YELLOW;
                        }
                        ColorBuilderMethodId::ColorKhaki => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorKhaki");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::KHAKI;
                        }
                        ColorBuilderMethodId::ColorDarkGreen => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorDarkGreen");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::DARK_GREEN;
                        }
                        ColorBuilderMethodId::ColorGreen => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorGreen");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::GREEN;
                        }
                        ColorBuilderMethodId::ColorLightGreen => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorLightGreen");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::LIGHT_GREEN;
                        }
                        ColorBuilderMethodId::ColorDarkBlue => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorDarkBlue");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::DARK_BLUE;
                        }
                        ColorBuilderMethodId::ColorBlue => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorBlue");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::BLUE;
                        }
                        ColorBuilderMethodId::ColorLightBlue => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorLightBlue");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::LIGHT_BLUE;
                        }
                        ColorBuilderMethodId::ColorPurple => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorPurple");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::PURPLE;
                        }
                        ColorBuilderMethodId::ColorGold => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorGold");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::GOLD;
                        }
                        ColorBuilderMethodId::ColorDebugColor => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorDebugColor");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::DEBUG_COLOR;
                        }
                        ColorBuilderMethodId::ColorPlaceholder => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ColorBuilderMethodId::ColorPlaceholder");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Color32::PLACEHOLDER;
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.r11_color32 = w;
            }
            FuncProcId::ComboBox => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::ComboBox");
                // arguments
                let i = self.read_id()?;

                let label = {
                    let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                    let u2: &mut Option<&mut egui::Ui> = &mut None;
                    if u2.is_some() {
                        self.interpret_inner(c, u2, &f2, d + 1)?;
                    } else {
                        self.interpret_inner(c, u, &f2, d + 1)?;
                    }

                    std::mem::take(&mut self.r1_widget_text)
                };

                let selected_text = {
                    let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                    let u2: &mut Option<&mut egui::Ui> = &mut None;
                    if u2.is_some() {
                        self.interpret_inner(c, u2, &f2, d + 1)?;
                    } else {
                        self.interpret_inner(c, u, &f2, d + 1)?;
                    }

                    std::mem::take(&mut self.r1_widget_text)
                };
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
egui::ComboBox::new(i,label).selected_text(selected_text);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(ComboBoxBuilderMethodId::from_repr)?;
                    match m {
                        ComboBoxBuilderMethodId::Build => {
                            break;
                        }
                        ComboBoxBuilderMethodId::Width => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ComboBoxBuilderMethodId::Width");
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            w = w.width(width);
                        }
                        ComboBoxBuilderMethodId::Height => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ComboBoxBuilderMethodId::Height");
                            #[allow(unused_mut)]
                            let mut height = self.io.read_plain_f32()?;
                            w = w.height(height);
                        }
                        ComboBoxBuilderMethodId::Wrap => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ComboBoxBuilderMethodId::Wrap");
                            w = w.wrap();
                        }
                        ComboBoxBuilderMethodId::Truncate => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ComboBoxBuilderMethodId::Truncate");
                            w = w.truncate();
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    if w.show_ui(u.as_mut().unwrap(), |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                        return Some(true);
                    })
                    .inner
                    .is_none()
                    {
                        self.r7_push(i.value(), ResponseFlags::BLOCK_SKIPPED);
                        self.interpret_outer(c, &mut None)?;
                    }
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::ContextInspectionUi => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::ContextInspectionUi");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    c.inspection_ui(u.as_mut().unwrap());
                }
            }
            FuncProcId::ContextMenu => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::ContextMenu");
                // arguments
                // construct
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let mut menu_blocks = self.io.read_deferred_block_map_u32()?;
                    let mut target_blocks = self.io.read_deferred_block_map_u32()?;
                    let menu = menu_blocks.drain().next().map(|(_, v)| v);
                    let target = target_blocks.drain().next().map(|(_, v)| v);
                    let ui = u.as_mut().unwrap();
                    let scope_resp = ui
                        .scope(|ui| {
                            if let Some(block) = &target {
                                let _ = self.replay_deferred_block_logged(c, ui, block);
                            }
                        })
                        .response;
                    let anchor = ui.interact(
                        scope_resp.rect,
                        scope_resp.id.with("imzero2_context_menu"),
                        egui::Sense::hover(),
                    );
                    if let Some(block) = menu {
                        let (secondary, primary) = anchor.ctx.input(|i| {
                            (i.pointer.secondary_clicked(), i.pointer.primary_clicked())
                        });
                        let hovered = anchor.hovered();
                        let cmd = if hovered && secondary {
                            Some(egui::SetOpenCommand::Bool(true))
                        } else if hovered && primary {
                            Some(egui::SetOpenCommand::Bool(false))
                        } else {
                            None
                        };
                        let ctx_cloned = anchor.ctx.clone();
                        let _ = egui::Popup::menu(&anchor)
                            .open_memory(cmd)
                            .at_pointer_fixed()
                            .show(|ui| {
                                let _ = self.replay_deferred_block_logged(&ctx_cloned, ui, &block);
                            });
                    }
                } else {
                    self.io.skip_deferred_block_map_u32()?;
                    self.io.skip_deferred_block_map_u32()?;
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
            }
            FuncProcId::ContextSendViewPortCommandClose => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::ContextSendViewPortCommandClose");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                c.send_viewport_cmd(egui::ViewportCommand::Close);
            }
            FuncProcId::CopyTextToClipboard => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::CopyTextToClipboard");
                // arguments
                #[allow(unused_mut)]
                let mut text = self.io.read_plain_s()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                c.copy_text(text);
            }
            FuncProcId::DatePickerButton => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::DatePickerButton");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut packed_ymd = self.io.read_plain_u64()?;
                // construct

                #[allow(unused_mut)]
                let mut w = crate::imzero2::date_picker_button::DatePickerButtonRequest::default();
                // methods
                loop {
                    let (m, _) = self.read_from_repr(DatePickerButtonBuilderMethodId::from_repr)?;
                    match m {
                        DatePickerButtonBuilderMethodId::Build => {
                            break;
                        }
                        DatePickerButtonBuilderMethodId::Format => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DatePickerButtonBuilderMethodId::Format");
                            #[allow(unused_mut)]
                            let mut format = self.io.read_plain_s()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.format = Some(format);
                        }
                        DatePickerButtonBuilderMethodId::HighlightWeekends => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DatePickerButtonBuilderMethodId::HighlightWeekends"
                            );
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.highlight_weekends = Some(enabled);
                        }
                        DatePickerButtonBuilderMethodId::ShowIcon => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DatePickerButtonBuilderMethodId::ShowIcon"
                            );
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.show_icon = Some(enabled);
                        }
                        DatePickerButtonBuilderMethodId::Calendar => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DatePickerButtonBuilderMethodId::Calendar"
                            );
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.calendar = Some(enabled);
                        }
                        DatePickerButtonBuilderMethodId::CalendarWeek => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DatePickerButtonBuilderMethodId::CalendarWeek"
                            );
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.calendar_week = Some(enabled);
                        }
                        DatePickerButtonBuilderMethodId::StartEndYears => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DatePickerButtonBuilderMethodId::StartEndYears"
                            );
                            #[allow(unused_mut)]
                            let mut start_year = self.io.read_plain_i16()?;
                            #[allow(unused_mut)]
                            let mut end_year = self.io.read_plain_i16()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.start_end_years = Some((start_year, end_year));
                        }
                        DatePickerButtonBuilderMethodId::Arrows => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DatePickerButtonBuilderMethodId::Arrows");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.arrows = Some(enabled);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.apply_date_picker_button(w, u, f, i, packed_ymd);
            }
            FuncProcId::DateTimePickerButton => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::DateTimePickerButton");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut packed_epoch_ms = self.io.read_plain_u64()?;
                // construct

                #[allow(unused_mut)]
                let mut w = crate::imzero2::datetime_picker::DateTimePickerButtonRequest::default();
                // methods
                loop {
                    let (m, _) =
                        self.read_from_repr(DateTimePickerButtonBuilderMethodId::from_repr)?;
                    match m {
                        DateTimePickerButtonBuilderMethodId::Build => {
                            break;
                        }
                        DateTimePickerButtonBuilderMethodId::Format => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DateTimePickerButtonBuilderMethodId::Format"
                            );
                            #[allow(unused_mut)]
                            let mut format = self.io.read_plain_s()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.format = Some(format);
                        }
                        DateTimePickerButtonBuilderMethodId::HighlightWeekends => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DateTimePickerButtonBuilderMethodId::HighlightWeekends"
                            );
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.highlight_weekends = Some(enabled);
                        }
                        DateTimePickerButtonBuilderMethodId::ShowIcon => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DateTimePickerButtonBuilderMethodId::ShowIcon"
                            );
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.show_icon = Some(enabled);
                        }
                        DateTimePickerButtonBuilderMethodId::Calendar => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DateTimePickerButtonBuilderMethodId::Calendar"
                            );
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.calendar = Some(enabled);
                        }
                        DateTimePickerButtonBuilderMethodId::CalendarWeek => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DateTimePickerButtonBuilderMethodId::CalendarWeek"
                            );
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.calendar_week = Some(enabled);
                        }
                        DateTimePickerButtonBuilderMethodId::StartEndYears => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DateTimePickerButtonBuilderMethodId::StartEndYears"
                            );
                            #[allow(unused_mut)]
                            let mut start_year = self.io.read_plain_i16()?;
                            #[allow(unused_mut)]
                            let mut end_year = self.io.read_plain_i16()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.start_end_years = Some((start_year, end_year));
                        }
                        DateTimePickerButtonBuilderMethodId::Arrows => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DateTimePickerButtonBuilderMethodId::Arrows"
                            );
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.arrows = Some(enabled);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.apply_date_time_picker_button(w, u, f, i, packed_epoch_ms);
            }
            FuncProcId::DockAreaRaw => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::DockAreaRaw");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut tab_ids = self.io.read_plain_u64h()?;
                #[allow(unused_mut)]
                let mut tab_titles = self.io.read_plain_sh()?;
                #[allow(unused_mut)]
                let mut initial_layout = self.io.read_plain_u8h()?;
                #[allow(unused_mut)]
                let mut no_scroll_tab_ids = self.io.read_plain_u64h()?;
                #[allow(unused_mut)]
                let mut activate_tab_id = self.io.read_plain_u64()?;
                // construct
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let bodies = self.io.read_deferred_block_map_u64()?;
                if u.is_some() {
                    use std::collections::HashSet;

                    // Build title lookup. tab_ids and tab_titles have matching length by
                    // construction (DockAreaBuilder.Send enforces this on the Go side).
                    let mut titles: std::collections::HashMap<u64, String> =
                        std::collections::HashMap::with_capacity(tab_ids.len());
                    for (id, t) in tab_ids.iter().copied().zip(tab_titles.into_iter()) {
                        titles.insert(id, t);
                    }

                    // Tabs whose body must not be wrapped in a live ScrollArea (both axes
                    // off — overflow clips). Viewport-style bodies (walkers map, canvases)
                    // read wheel/zoom input globally without consuming it, so the default
                    // per-tab ScrollArea would scroll the panel in the same gesture that
                    // pans/zooms the widget content.
                    let no_scroll: HashSet<u64> = no_scroll_tab_ids.iter().copied().collect();

                    // Take DockState out of the map so the subsequent TabViewer can hold
                    // &mut self.interpreter without aliasing the HashMap entry. When
                    // the dock area is first seen (no entry yet), parse the Go-side
                    // initialLayout descriptor to build the desired split tree; on
                    // subsequent frames the stored DockState wins so user drag/drop
                    // changes are preserved. Empty initialLayout falls back to the
                    // "everything in one leaf" default.
                    let area_id = i.value();
                    let mut dock_state = self
                        .dock_states
                        .remove(&area_id)
                        .unwrap_or_else(|| parse_dock_initial_layout(&initial_layout, &tab_ids));

                    // Reconcile stored layout with Go's authoritative tab list.
                    let wanted: HashSet<u64> = tab_ids.iter().copied().collect();
                    dock_state.retain_tabs(|t| wanted.contains(t));
                    let existing: HashSet<u64> =
                        dock_state.iter_all_tabs().map(|(_, t)| *t).collect();
                    for &id in &tab_ids {
                        if !existing.contains(&id) {
                            dock_state.push_to_first_leaf(id);
                        }
                    }

                    // Programmatic tab activation (0 = none). Go-side affordances that
                    // deliver content INTO a tab body (the snippet-library Insert splicing
                    // into the editor) focus that tab first: a hidden tab's body buffer is
                    // discarded uninterpreted, so the delivery op would be silently lost —
                    // and the user could not see the result anyway.
                    if activate_tab_id != 0 {
                        if let Some(loc) = dock_state.find_tab(&activate_tab_id) {
                            let _ = dock_state.set_active_tab(loc);
                        }
                    }

                    let ui = u.as_mut().unwrap();
                    // egui_dock 0.19 quirk: DockArea::show_inside takes
                    // ui.available_rect_before_wrap() greedily, and its per-leaf renderer
                    // overrides — not intersects — the parent clip via ui.set_clip_rect.
                    // Inside an unbounded parent (ScrollArea, auto-resize Window) this
                    // lets dock content paint past the visible region.
                    //
                    // Allocate a child ui whose max_rect is the visible region from the
                    // cursor down — this both bounds the dock for clip purposes AND lets
                    // the parent advance its cursor past the dock cleanly, so widgets
                    // declared above the dock keep their reserved space. (Earlier
                    // attempts used ui.set_max_size on the parent; Placer::set_max_height
                    // in egui 0.34 placer.rs:255 ends with cursor.min = max_rect.min,
                    // which silently teleports the parent cursor back to the top of the
                    // panel and clobbers everything placed above the dock.)
                    let cursor = ui.cursor().min;
                    let bound_corner = ui.max_rect().max.min(ui.clip_rect().max);
                    let avail = (bound_corner - cursor).max(egui::Vec2::ZERO);
                    let layout = *ui.layout();
                    let ctx_cloned = ui.ctx().clone();
                    ui.allocate_ui_with_layout(avail, layout, |child_ui| {
                        let mut viewer = FffiDockTabViewer {
                            interpreter: self,
                            ctx: &ctx_cloned,
                            bodies,
                            titles,
                            no_scroll,
                        };
                        // Key the library's own id derivation on the Go-side dock id, not
                        // egui_dock's default Id::new("egui_dock::DockArea") constant. A tab
                        // body is built with Ui::new(tab_body_id(dock_area_id, path, tab_id))
                        // (egui_dock show/leaf.rs), which deliberately does NOT mix in the
                        // parent ui id — so with the default constant two windows of the same
                        // app derive the SAME body ui, and the ScrollArea egui_dock wraps
                        // every body in (auto-id off that ui) shares one offset across them:
                        // scrolling one window scrolled the other. area_id is already the
                        // per-instance-salted widget id, so this separates every id below the
                        // dock as well.
                        egui_dock::DockArea::new(&mut dock_state)
                            .id(egui::Id::new(area_id))
                            .show_inside(child_ui, &mut viewer);
                    });

                    self.dock_states.insert(area_id, dock_state);
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
            }
            FuncProcId::DragValueF64 => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::DragValueF64");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut val = self.io.read_plain_f64()?;
                // construct

                #[allow(unused_mut)]
                let mut w = egui::DragValue::new(&mut val);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(DragValueF64BuilderMethodId::from_repr)?;
                    match m {
                        DragValueF64BuilderMethodId::Build => {
                            break;
                        }
                        DragValueF64BuilderMethodId::Speed => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DragValueF64BuilderMethodId::Speed");
                            #[allow(unused_mut)]
                            let mut speed = self.io.read_plain_f64()?;
                            w = w.speed(speed);
                        }
                        DragValueF64BuilderMethodId::Prefix => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DragValueF64BuilderMethodId::Prefix");
                            #[allow(unused_mut)]
                            let mut prefix = self.io.read_plain_s()?;
                            w = w.prefix(prefix);
                        }
                        DragValueF64BuilderMethodId::Suffix => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DragValueF64BuilderMethodId::Suffix");
                            #[allow(unused_mut)]
                            let mut suffix = self.io.read_plain_s()?;
                            w = w.suffix(suffix);
                        }
                        DragValueF64BuilderMethodId::MinDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DragValueF64BuilderMethodId::MinDecimals"
                            );
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.min_decimals(digits as usize);
                        }
                        DragValueF64BuilderMethodId::MaxDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DragValueF64BuilderMethodId::MaxDecimals"
                            );
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.max_decimals(digits as usize);
                        }
                        DragValueF64BuilderMethodId::FixedDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DragValueF64BuilderMethodId::FixedDecimals"
                            );
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.fixed_decimals(digits as usize);
                        }
                        DragValueF64BuilderMethodId::Binary => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DragValueF64BuilderMethodId::Binary");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.binary(min_width as usize, twos_complement);
                        }
                        DragValueF64BuilderMethodId::Octal => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DragValueF64BuilderMethodId::Octal");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.octal(min_width as usize, twos_complement);
                        }
                        DragValueF64BuilderMethodId::Hexadecimal => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DragValueF64BuilderMethodId::Hexadecimal"
                            );
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            #[allow(unused_mut)]
                            let mut upper = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.hexadecimal(min_width as usize, twos_complement, upper);
                        }
                        DragValueF64BuilderMethodId::UpdateWhileEditing => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DragValueF64BuilderMethodId::UpdateWhileEditing"
                            );
                            #[allow(unused_mut)]
                            let mut update = self.io.read_plain_b()?;
                            w = w.update_while_editing(update);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                let resp =
// generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
self.apply_widget(w,u,f,Some(i));
                if resp.is_some() && resp.unwrap().changed() {
                    // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                    self.r9_f64_push(i.value(), val);
                }
            }
            FuncProcId::DragValueI64 => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::DragValueI64");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut val = self.io.read_plain_i64()?;
                // construct

                #[allow(unused_mut)]
                let mut w = egui::DragValue::new(&mut val);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(DragValueI64BuilderMethodId::from_repr)?;
                    match m {
                        DragValueI64BuilderMethodId::Build => {
                            break;
                        }
                        DragValueI64BuilderMethodId::Speed => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DragValueI64BuilderMethodId::Speed");
                            #[allow(unused_mut)]
                            let mut speed = self.io.read_plain_f64()?;
                            w = w.speed(speed);
                        }
                        DragValueI64BuilderMethodId::Prefix => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DragValueI64BuilderMethodId::Prefix");
                            #[allow(unused_mut)]
                            let mut prefix = self.io.read_plain_s()?;
                            w = w.prefix(prefix);
                        }
                        DragValueI64BuilderMethodId::Suffix => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DragValueI64BuilderMethodId::Suffix");
                            #[allow(unused_mut)]
                            let mut suffix = self.io.read_plain_s()?;
                            w = w.suffix(suffix);
                        }
                        DragValueI64BuilderMethodId::MinDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DragValueI64BuilderMethodId::MinDecimals"
                            );
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.min_decimals(digits as usize);
                        }
                        DragValueI64BuilderMethodId::MaxDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DragValueI64BuilderMethodId::MaxDecimals"
                            );
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.max_decimals(digits as usize);
                        }
                        DragValueI64BuilderMethodId::FixedDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DragValueI64BuilderMethodId::FixedDecimals"
                            );
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.fixed_decimals(digits as usize);
                        }
                        DragValueI64BuilderMethodId::Binary => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DragValueI64BuilderMethodId::Binary");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.binary(min_width as usize, twos_complement);
                        }
                        DragValueI64BuilderMethodId::Octal => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DragValueI64BuilderMethodId::Octal");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.octal(min_width as usize, twos_complement);
                        }
                        DragValueI64BuilderMethodId::Hexadecimal => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DragValueI64BuilderMethodId::Hexadecimal"
                            );
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            #[allow(unused_mut)]
                            let mut upper = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.hexadecimal(min_width as usize, twos_complement, upper);
                        }
                        DragValueI64BuilderMethodId::UpdateWhileEditing => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DragValueI64BuilderMethodId::UpdateWhileEditing"
                            );
                            #[allow(unused_mut)]
                            let mut update = self.io.read_plain_b()?;
                            w = w.update_while_editing(update);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                let resp =
// generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
self.apply_widget(w,u,f,Some(i));
                if resp.is_some() && resp.unwrap().changed() {
                    // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                    self.r9_i64_push(i.value(), val);
                }
            }
            FuncProcId::DragValueU64 => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::DragValueU64");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut val = self.io.read_plain_u64()?;
                // construct

                #[allow(unused_mut)]
                let mut w = egui::DragValue::new(&mut val);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(DragValueU64BuilderMethodId::from_repr)?;
                    match m {
                        DragValueU64BuilderMethodId::Build => {
                            break;
                        }
                        DragValueU64BuilderMethodId::Speed => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DragValueU64BuilderMethodId::Speed");
                            #[allow(unused_mut)]
                            let mut speed = self.io.read_plain_f64()?;
                            w = w.speed(speed);
                        }
                        DragValueU64BuilderMethodId::Prefix => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DragValueU64BuilderMethodId::Prefix");
                            #[allow(unused_mut)]
                            let mut prefix = self.io.read_plain_s()?;
                            w = w.prefix(prefix);
                        }
                        DragValueU64BuilderMethodId::Suffix => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DragValueU64BuilderMethodId::Suffix");
                            #[allow(unused_mut)]
                            let mut suffix = self.io.read_plain_s()?;
                            w = w.suffix(suffix);
                        }
                        DragValueU64BuilderMethodId::MinDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DragValueU64BuilderMethodId::MinDecimals"
                            );
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.min_decimals(digits as usize);
                        }
                        DragValueU64BuilderMethodId::MaxDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DragValueU64BuilderMethodId::MaxDecimals"
                            );
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.max_decimals(digits as usize);
                        }
                        DragValueU64BuilderMethodId::FixedDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DragValueU64BuilderMethodId::FixedDecimals"
                            );
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.fixed_decimals(digits as usize);
                        }
                        DragValueU64BuilderMethodId::Binary => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DragValueU64BuilderMethodId::Binary");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.binary(min_width as usize, twos_complement);
                        }
                        DragValueU64BuilderMethodId::Octal => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match DragValueU64BuilderMethodId::Octal");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.octal(min_width as usize, twos_complement);
                        }
                        DragValueU64BuilderMethodId::Hexadecimal => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DragValueU64BuilderMethodId::Hexadecimal"
                            );
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            #[allow(unused_mut)]
                            let mut upper = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.hexadecimal(min_width as usize, twos_complement, upper);
                        }
                        DragValueU64BuilderMethodId::UpdateWhileEditing => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match DragValueU64BuilderMethodId::UpdateWhileEditing"
                            );
                            #[allow(unused_mut)]
                            let mut update = self.io.read_plain_b()?;
                            w = w.update_while_editing(update);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                let resp =
// generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
self.apply_widget(w,u,f,Some(i));
                if resp.is_some() && resp.unwrap().changed() {
                    // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                    self.r9_u64_push(i.value(), val);
                }
            }
            FuncProcId::EnabledUi => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::EnabledUi");
                // arguments
                #[allow(unused_mut)]
                let mut enabled = self.io.read_plain_b()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().add_enabled_ui(enabled, |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::End => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::End");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                r = true;
            }
            FuncProcId::EndETable => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::EndETable");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut num_rows = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut default_row_height = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut num_sticky_headers = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut num_sticky_cols = self.io.read_plain_u32()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut scroll_to_row: Option<(u64, Option<egui::Align>)> = None;
                let mut scroll_to_column: Option<(usize, Option<egui::Align>)> = None;
                let mut scroll_to_row_range: Option<(
                    std::ops::RangeInclusive<u64>,
                    Option<egui::Align>,
                )> = None;
                let mut scroll_to_col_range: Option<(
                    std::ops::RangeInclusive<usize>,
                    Option<egui::Align>,
                )> = None;
                let mut auto_size_mode = egui_table::AutoSizeMode::Never;
                let mut striped_flag = false;
                let mut selected_row_opt: Option<u64> = None;
                let mut max_height_override: Option<f32> = None;
                let mut apply_widths_epoch: Option<u32> = None;
                fn decode_scroll_align(v: u8) -> Option<egui::Align> {
                    match v {
                        1 => Some(egui::Align::TOP),
                        2 => Some(egui::Align::Center),
                        3 => Some(egui::Align::BOTTOM),
                        _ => None,
                    }
                }
                // methods
                loop {
                    let (m, _) = self.read_from_repr(EndETableBuilderMethodId::from_repr)?;
                    match m {
                        EndETableBuilderMethodId::Build => {
                            break;
                        }
                        EndETableBuilderMethodId::ScrollToRow => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match EndETableBuilderMethodId::ScrollToRow");
                            #[allow(unused_mut)]
                            let mut row = self.io.read_plain_u64()?;
                            #[allow(unused_mut)]
                            let mut align = self.io.read_plain_u8()?;
                            scroll_to_row = Some((row, decode_scroll_align(align)));
                        }
                        EndETableBuilderMethodId::ScrollToColumn => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match EndETableBuilderMethodId::ScrollToColumn"
                            );
                            #[allow(unused_mut)]
                            let mut col = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut align = self.io.read_plain_u8()?;
                            scroll_to_column = Some((col as usize, decode_scroll_align(align)));
                        }
                        EndETableBuilderMethodId::ScrollToRows => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match EndETableBuilderMethodId::ScrollToRows");
                            #[allow(unused_mut)]
                            let mut row_begin = self.io.read_plain_u64()?;
                            #[allow(unused_mut)]
                            let mut row_end = self.io.read_plain_u64()?;
                            #[allow(unused_mut)]
                            let mut align = self.io.read_plain_u8()?;
                            scroll_to_row_range =
                                Some((row_begin..=row_end, decode_scroll_align(align)));
                        }
                        EndETableBuilderMethodId::ScrollToColumns => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match EndETableBuilderMethodId::ScrollToColumns"
                            );
                            #[allow(unused_mut)]
                            let mut col_begin = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut col_end = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut align = self.io.read_plain_u8()?;
                            scroll_to_col_range = Some((
                                col_begin as usize..=col_end as usize,
                                decode_scroll_align(align),
                            ));
                        }
                        EndETableBuilderMethodId::AutoSizeMode => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match EndETableBuilderMethodId::AutoSizeMode");
                            #[allow(unused_mut)]
                            let mut mode = self.io.read_plain_u8()?;
                            auto_size_mode = match mode {
                                1 => egui_table::AutoSizeMode::Always,
                                2 => egui_table::AutoSizeMode::OnParentResize,
                                _ => egui_table::AutoSizeMode::Never,
                            };
                        }
                        EndETableBuilderMethodId::Striped => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match EndETableBuilderMethodId::Striped");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            striped_flag = val;
                        }
                        EndETableBuilderMethodId::SelectedRow => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match EndETableBuilderMethodId::SelectedRow");
                            #[allow(unused_mut)]
                            let mut row = self.io.read_plain_u64()?;
                            selected_row_opt = Some(row);
                        }
                        EndETableBuilderMethodId::MaxHeight => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match EndETableBuilderMethodId::MaxHeight");
                            #[allow(unused_mut)]
                            let mut height = self.io.read_plain_f32()?;
                            max_height_override = Some(height);
                        }
                        EndETableBuilderMethodId::ApplyWidths => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match EndETableBuilderMethodId::ApplyWidths");
                            #[allow(unused_mut)]
                            let mut epoch = self.io.read_plain_u32()?;
                            apply_widths_epoch = Some(epoch);
                        }
                    }
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let ui = u.as_mut().unwrap();

                    let col_count = self.et_columns.len();

                    // Read deferred block maps from the IPC stream.
                    // Cell blocks use a dense flat layout (single slab + O(1) indexed lookup)
                    // instead of a HashMap — eliminates per-block Vec allocations and hashing.
                    let cells =
                        self.io.read_deferred_block_map_dense_u64_u32(num_rows, col_count)?;
                    let header_blocks = self.io.read_deferred_block_map_u32_u32()?;
                    // ADR-0176 SD5. Sparse HashMap rather than the dense slab cells use: a
                    // row block is optional (most tables emit none at all), so a slab of
                    // num_rows entries would cost more than it saves.
                    let row_blocks = self.io.read_deferred_block_map_u64()?;

                    let columns: Vec<egui_table::Column> = self.et_columns.drain(..).collect();
                    let header_texts: Vec<String> = self.et_header_texts.drain(..).collect();

                    // Snapshot the Go-supplied widths before the columns move into the
                    // table. They are the read-back fallback for columns egui_table never
                    // records: its store-back loop skips non-resizable columns, so their
                    // entry in TableState.col_widths never exists. Reporting 0.0 for those
                    // would read Go-side as "the user changed it to zero" and be captured
                    // as an override; reporting what we supplied reads as "unchanged",
                    // which for a column that cannot be dragged is simply true.
                    let col_currents: Vec<f32> = columns.iter().map(|cc| cc.current).collect();

                    // Compute cumulative row offsets from per-row heights (prefix sum).
                    // Pushes N+1 entries: offsets[i] is the top of row i, and offsets[N] is
                    // the bottom of the last row — egui_table queries row_top_offset(num_rows)
                    // to compute the total scroll content height. Without the trailing entry
                    // the FffiTableDelegate falls through to the default linear formula and
                    // reports a content height that ignores per-row variability, clipping
                    // the tail when the actual cumulative height exceeds num_rows × default.
                    let row_heights: Vec<f32> = self.et_row_heights.drain(..).collect();
                    let row_offsets: Vec<f32> = if row_heights.is_empty() {
                        Vec::new()
                    } else {
                        let mut offsets = Vec::with_capacity(row_heights.len() + 1);
                        let mut acc = 0.0f32;
                        for h in &row_heights {
                            offsets.push(acc);
                            acc += h;
                        }
                        offsets.push(acc);
                        offsets
                    };

                    // Create header rows if we have either text headers or deferred header blocks
                    let has_headers = !header_texts.is_empty() || !header_blocks.is_empty();
                    let mut headers = Vec::new();
                    if has_headers {
                        for _ in 0..(num_sticky_headers as usize).max(1) {
                            headers.push(egui_table::HeaderRow::new(default_row_height));
                        }
                    }

                    let mut table = egui_table::Table::new()
                        .id_salt(i)
                        .num_rows(num_rows)
                        .columns(columns)
                        .num_sticky_cols(num_sticky_cols as usize)
                        .auto_size_mode(auto_size_mode);

                    if !headers.is_empty() {
                        table = table.headers(headers);
                    }
                    if let Some((row, align)) = scroll_to_row {
                        table = table.scroll_to_row(row, align);
                    }
                    if let Some((col, align)) = scroll_to_column {
                        table = table.scroll_to_column(col, align);
                    }
                    if let Some((rows, align)) = scroll_to_row_range {
                        table = table.scroll_to_rows(rows, align);
                    }
                    if let Some((cols, align)) = scroll_to_col_range {
                        table = table.scroll_to_columns(cols, align);
                    }

                    // Striping + selection live in a locally-scoped decorator so the feature
                    // stays in this IDL rather than in interpreter.rs (which is regenerated).
                    // Stripes use the active visuals; the selection stripe is anchored to
                    // ACCENT_DEFAULT (L=0.80) instead of visuals.selection.bg_fill because
                    // IDS pins that token at ACCENT_SUBTLE (L=0.20) for SelectableLabel
                    // contrast (ADR-0037) — 0.35× of L=0.20 is invisible against
                    // extreme_bg_color (L=0.06). Same fix pattern as ProgressBar's default
                    // fill in egui2_definition_d_widgets.go.
                    // prepare() also pushes the visible (row, col) ranges into the
                    // r9_et_prefetch register so the Go side can, on the next frame, skip
                    // emitting cells that egui_table will immediately cull.
                    struct EtStripedDelegate<
                        'sa,
                        'sb,
                        'sc,
                        'sd,
                        SR: std::io::BufRead,
                        SW: std::io::Write,
                    > {
                        inner: FffiTableDelegate<'sa, 'sb, 'sc, SR, SW>,
                        table_id: u64,
                        striped: bool,
                        selected_row: Option<u64>,
                        // ADR-0176 SD5/SD6: the row blocks, and the per-frame set of rows
                        // already replayed. The delegate is constructed fresh each frame, so
                        // the set needs no explicit reset.
                        rows: &'sd std::collections::HashMap<u64, Vec<u8>>,
                        replayed_rows: std::collections::HashSet<u64>,
                    }
                    impl<'sa, 'sb, 'sc, 'sd, SR: std::io::BufRead, SW: std::io::Write>
                        egui_table::TableDelegate
                        for EtStripedDelegate<'sa, 'sb, 'sc, 'sd, SR, SW>
                    {
                        fn prepare(&mut self, info: &egui_table::PrefetchInfo) {
                            let interp = &mut self.inner.interpreter;
                            interp.r9_et_prefetch_ids.push(self.table_id);
                            interp.r9_et_prefetch_values.push(info.visible_rows.start);
                            interp.r9_et_prefetch_values.push(info.visible_rows.end);
                            interp.r9_et_prefetch_values.push(info.visible_columns.start as u64);
                            interp.r9_et_prefetch_values.push(info.visible_columns.end as u64);
                            interp.r9_et_prefetch_values.push(info.num_sticky_columns as u64);
                            self.inner.prepare(info);
                        }
                        fn header_cell_ui(
                            &mut self,
                            ui: &mut egui::Ui,
                            cell: &egui_table::HeaderCellInfo,
                        ) {
                            self.inner.header_cell_ui(ui, cell);
                        }
                        // ADR-0176 SD5/SD6. row_ui hands us a Ui spanning the whole row across
                        // every column, before that row's cells run — the seam a full-row
                        // background, hover or click sense needs, and the one that makes a row
                        // read as continuous across egui_table's inter-column gutters.
                        //
                        // The guard is not defensive: egui_table calls row_ui once per REGION,
                        // not once per row. region_ui runs for both the fully-scrollable half
                        // (right_bottom_ui) and the sticky-column half (left_bottom_ui), and
                        // its row_range is computed from the vertical extent WITHOUT
                        // consulting col_range. With num_sticky_cols = 0 the sticky region is
                        // zero-width but full-height, so its row loop still runs, and
                        // split_scroll.rs calls left_bottom_ui unconditionally.
                        //
                        // Replaying the block in both would emit every widget id inside it
                        // twice. That breaks nothing visible — egui hit-tests on its own auto
                        // ids — but r7 read-back is a flat map compacted newest-wins, so the
                        // first copy would read NilResponseFlags forever: a row that still
                        // renders and still accepts clicks while its handler never fires.
                        //
                        // First call wins, which is the right one: split_scroll.rs runs
                        // right_bottom_ui before left_bottom_ui, so the block lands in the
                        // region that is actually visible rather than the degenerate one.
                        fn row_ui(&mut self, ui: &mut egui::Ui, row_nr: u64) {
                            if !self.replayed_rows.insert(row_nr) {
                                return;
                            }
                            if let Some(block) = self.rows.get(&row_nr) {
                                if !block.is_empty() {
                                    let ctx = ui.ctx().clone();
                                    let _ = self
                                        .inner
                                        .interpreter
                                        .replay_deferred_block(&ctx, ui, block);
                                }
                            }
                        }
                        fn cell_ui(&mut self, ui: &mut egui::Ui, cell: &egui_table::CellInfo) {
                            let visuals = ui.style().visuals.clone();
                            let rect = ui.max_rect();
                            if self.selected_row == Some(cell.row_nr) {
                                let bg =
                                    imzero2_egui::style::tokens::palette_generated::ACCENT_DEFAULT
                                        .gamma_multiply(0.35);
                                ui.painter().rect_filled(rect, 0.0, bg);
                            } else if self.striped && cell.row_nr % 2 == 1 {
                                ui.painter().rect_filled(rect, 0.0, visuals.faint_bg_color);
                            }
                            self.inner.cell_ui(ui, cell);
                        }
                        fn default_row_height(&self) -> f32 {
                            self.inner.default_row_height()
                        }
                        fn row_top_offset(
                            &self,
                            ctx: &egui::Context,
                            table_id: egui::Id,
                            row_nr: u64,
                        ) -> f32 {
                            self.inner.row_top_offset(ctx, table_id, row_nr)
                        }
                    }

                    let mut delegate = EtStripedDelegate {
                        inner: FffiTableDelegate {
                            interpreter: self,
                            cells: &cells,
                            header_blocks: &header_blocks,
                            header_texts: &header_texts,
                            row_offsets: &row_offsets,
                            col_count,
                            default_row_height,
                        },
                        table_id: i.value(),
                        striped: striped_flag,
                        selected_row: selected_row_opt,
                        rows: &row_blocks,
                        replayed_rows: std::collections::HashSet::new(),
                    };

                    // Bound table.show() inside a child ui so egui_table's SplitScroll
                    // doesn't greedily eat ui.available_size() — see table.rs:468 in
                    // egui_table 0.8.0. Without this wrap, the cursor advances past
                    // "all remaining vertical space" after table.show() returns, and
                    // every subsequent widget in a vertically flowing parent
                    // (CollapsingHeader / ScrollArea / Vertical) gets pushed off-screen.
                    // Pattern mirrors the egui_dock 0.19 wrap in d_dock.go (DockArea's
                    // show_inside has the same greedy-alloc bug).
                    //
                    // Height heuristic:
                    //   - natural = header_rows × default_row_height + body_rows × default_row_height + ~16px scrollbar margin
                    //     (uses the row_offsets prefix sum when per-row heights are set)
                    //   - if maxHeight was set explicitly via .MaxHeight(f32), use exactly that
                    //   - otherwise auto-fit: cap natural by the parent's remaining height
                    //     when the parent is finite; cap by ETABLE_AUTOFIT_CAP_PX when the
                    //     parent is unbounded (i.e. inside a Vscroll=true ScrollArea — there
                    //     the table would otherwise demand an absurd 10_000-row content
                    //     size from the outer scroll)
                    const ETABLE_AUTOFIT_CAP_PX: f32 = 400.0;
                    const ETABLE_SCROLLBAR_MARGIN_PX: f32 = 16.0;

                    let header_height = if has_headers {
                        (num_sticky_headers as f32).max(1.0) * default_row_height
                    } else {
                        0.0
                    };
                    let body_height = if let Some(last) = row_offsets.last() {
                        *last
                    } else {
                        num_rows as f32 * default_row_height
                    };
                    let natural_height = header_height + body_height + ETABLE_SCROLLBAR_MARGIN_PX;

                    // Don't compute "remaining height" from ui.max_rect() or ui.clip_rect():
                    //  - ui.clip_rect() is the visible viewport. Inside a ScrollArea the
                    //    cursor legitimately advances past clip_rect.max.y (the scroll
                    //    area renders off-viewport content so the user can scroll to it).
                    //  - ui.max_rect() is the parent's currently-allocated layout area,
                    //    which inside content-sized containers (ScrollArea content, the
                    //    body of a CollapsingHeader, a Window's auto-sized inner ui) is
                    //    flush with the cursor — max_rect.max.y == cursor.y at the moment
                    //    we're about to render. "remaining = max_y - cursor.y" then
                    //    collapses to zero and bounded_height to zero, so only the table
                    //    header paints (sticky_size, ~20px) and the body region (which
                    //    needs scroll_outer_size > 0) renders nothing.
                    //
                    // Pick natural_height (or max_height_override), clamp at the autofit
                    // cap to keep 10k-row tables from demanding 180000px of content from
                    // the outer ScrollArea. The parent extends itself to fit our
                    // allocation — that's exactly the behaviour ScrollArea / Window /
                    // CollapsingHeader / AllocateUiAtRect all share. For a tightly
                    // bounded parent that's smaller than natural, the table simply
                    // scrolls internally — that's the same outcome egui_table picks for
                    // MaxHeight overrides anyway.
                    let avail_x = (ui.max_rect().max.x - ui.cursor().min.x).max(0.0);
                    let bounded_height = match max_height_override {
                        Some(h) if h > 0.0 => h,
                        _ => natural_height.min(ETABLE_AUTOFIT_CAP_PX),
                    };
                    let bound_size = egui::Vec2::new(avail_x, bounded_height);
                    let layout = *ui.layout();
                    let table_gid = i.value();
                    ui.allocate_ui_with_layout(bound_size, layout, |child_ui| {
                        // The state id must be derived from the SAME ui table.show() will
                        // receive: TableState::id is ui.make_persistent_id(salt), so
                        // computing it from the outer ui would address a different slot.
                        let state_id = egui_table::TableState::id(child_ui, egui::IdSalt::new(i));

                        if let Some(epoch) = apply_widths_epoch {
                            let seen =
                                delegate.inner.interpreter.width_epochs.get(&table_gid).copied();
                            if seen != Some(epoch) {
                                // Seeding TableState does two things at once, and both are
                                // wanted. egui_table copies col_widths over each column's
                                // current width before laying out (table.rs:390), so ours
                                // win; and because it treats a present state as "not new"
                                // (is_new = state.is_none(), table.rs:384), storing one also
                                // suppresses the first-show force-autofit that would
                                // otherwise overwrite them on the very first frame.
                                let mut st = egui_table::TableState::load(child_ui, state_id)
                                    .unwrap_or_default();
                                for (idx, wpt) in col_currents.iter().enumerate() {
                                    if *wpt > 0.0 {
                                        // Key must match Column::id_for(i), which is
                                        // egui::Id::new(col_idx) over a usize. Taken from the
                                        // crate rather than from whatever integer is in hand;
                                        // see etable_widths.rs, which asserts the match.
                                        st.col_widths.insert(egui::Id::new(idx), *wpt);
                                    }
                                }
                                st.store(child_ui.ctx(), state_id);
                                delegate.inner.interpreter.width_epochs.insert(table_gid, epoch);
                            }
                        }

                        table.show(child_ui, &mut delegate);

                        if apply_widths_epoch.is_some() {
                            // Read back AFTER show: this is the width egui_table settled on
                            // once it had reconciled stored state, the user's drag, and its
                            // own grow-to-fit pass.
                            let after = egui_table::TableState::load(child_ui, state_id);
                            let interp = &mut delegate.inner.interpreter;
                            interp.r25_et_colwidth_ids.push(table_gid);
                            interp.r25_et_colwidth_counts.push(col_currents.len() as u64);
                            for (idx, fallback) in col_currents.iter().enumerate() {
                                let wpt = after
                                    .as_ref()
                                    .and_then(|st| st.col_widths.get(&egui::Id::new(idx)).copied())
                                    .unwrap_or(*fallback);
                                interp.r25_et_colwidth_values.push(wpt);
                            }
                        }
                    });
                } else {
                    self.et_columns.clear();
                    self.et_header_texts.clear();
                    self.et_row_heights.clear();
                    self.io.skip_deferred_block_map_u64_u32()?;
                    self.io.skip_deferred_block_map_u32_u32()?;
                    self.io.skip_deferred_block_map_u64()?;
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
            }
            FuncProcId::EndRow => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::EndRow");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().end_row();
                }
            }
            FuncProcId::EtColumn => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::EtColumn");
                // arguments
                #[allow(unused_mut)]
                let mut current_width = self.io.read_plain_f32()?;
                // construct

                #[allow(unused_mut)]
                let mut w = egui_table::Column::new(current_width);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(EtColumnBuilderMethodId::from_repr)?;
                    match m {
                        EtColumnBuilderMethodId::Build => {
                            break;
                        }
                        EtColumnBuilderMethodId::Resizable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match EtColumnBuilderMethodId::Resizable");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.resizable(val);
                        }
                        EtColumnBuilderMethodId::RangeMinMax => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match EtColumnBuilderMethodId::RangeMinMax");
                            #[allow(unused_mut)]
                            let mut min = self.io.read_plain_f32()?;
                            #[allow(unused_mut)]
                            let mut max = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.range(egui::Rangef::new(min, max));
                        }
                        EtColumnBuilderMethodId::AutoSizeThisFrame => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match EtColumnBuilderMethodId::AutoSizeThisFrame"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.auto_size_this_frame(val);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.et_columns.push(w);
            }
            FuncProcId::EtHeaderText => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::EtHeaderText");
                // arguments
                #[allow(unused_mut)]
                let mut text = self.io.read_plain_s()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.et_header_texts.push(text);
            }
            FuncProcId::EtRowHeight => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::EtRowHeight");
                // arguments
                #[allow(unused_mut)]
                let mut height = self.io.read_plain_f32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.et_row_heights.push(height);
            }
            FuncProcId::ExportSvg => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::ExportSvg");
                // arguments
                #[allow(unused_mut)]
                let mut path = self.io.read_plain_s()?;
                #[allow(unused_mut)]
                let mut embed_fonts = self.io.read_plain_b()?;
                #[allow(unused_mut)]
                let mut bg_rgba = self.io.read_plain_u32()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply

                let bg = if (bg_rgba & 0xff) == 0 {
                    None
                } else {
                    Some(egui::Color32::from_rgba_unmultiplied(
                        ((bg_rgba >> 24) & 0xff) as u8,
                        ((bg_rgba >> 16) & 0xff) as u8,
                        ((bg_rgba >> 8) & 0xff) as u8,
                        (bg_rgba & 0xff) as u8,
                    ))
                };
                self.export_state.lock().expect("svg_export state poisoned").pending =
                    Some(crate::imzero2::svgexport::ExportRequest {
                        path: std::path::PathBuf::from(path),
                        embed_fonts,
                        scope: crate::imzero2::svgexport::ExportScope::Viewport,
                        bg,
                    });
            }
            FuncProcId::ExportSvgWindow => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::ExportSvgWindow");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut path = self.io.read_plain_s()?;
                #[allow(unused_mut)]
                let mut embed_fonts = self.io.read_plain_b()?;
                #[allow(unused_mut)]
                let mut mode = self.io.read_plain_u8()?;
                #[allow(unused_mut)]
                let mut bg_rgba = self.io.read_plain_u32()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let window_mode = if mode == 1 {
                    crate::imzero2::svgexport::WindowMode::ContentOnly
                } else {
                    crate::imzero2::svgexport::WindowMode::Faithful
                };
                let bg = if (bg_rgba & 0xff) == 0 {
                    None
                } else {
                    Some(egui::Color32::from_rgba_unmultiplied(
                        ((bg_rgba >> 24) & 0xff) as u8,
                        ((bg_rgba >> 16) & 0xff) as u8,
                        ((bg_rgba >> 8) & 0xff) as u8,
                        (bg_rgba & 0xff) as u8,
                    ))
                };
                self.export_state.lock().expect("svg_export state poisoned").pending =
                    Some(crate::imzero2::svgexport::ExportRequest {
                        path: std::path::PathBuf::from(path),
                        embed_fonts,
                        scope: crate::imzero2::svgexport::ExportScope::Window {
                            id: i,
                            mode: window_mode,
                        },
                        bg,
                    });
            }
            FuncProcId::FetchCommandEnterPressed => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchCommandEnterPressed");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let shift = c.input_mut(|i| {
                    i.consume_key(
                        egui::Modifiers::COMMAND | egui::Modifiers::SHIFT,
                        egui::Key::Enter,
                    )
                });
                let plain =
                    c.input_mut(|i| i.consume_key(egui::Modifiers::COMMAND, egui::Key::Enter));
                self.io.write_plain_b(plain)?;
                self.io.write_plain_b(shift)?;
                self.io.flush()?;
            }
            FuncProcId::FetchF1KeyPressed => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchF1KeyPressed");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let pressed = c.input_mut(|i| i.consume_key(egui::Modifiers::NONE, egui::Key::F1));
                self.io.write_plain_b(pressed)?;
                self.io.flush()?;
            }
            FuncProcId::FetchFrameMetrics => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchFrameMetrics");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                self.io.write_plain_u64(self.last_interpret_us as u64)?;
                self.io.write_plain_u64(self.last_pass_nr)?;
                self.io.flush()?;
            }
            FuncProcId::FetchGraphEvents => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchGraphEvents");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.graph_events_pending.len();
                let graph_ids: Vec<u64> =
                    self.graph_events_pending.iter().map(|r| r.graph_id).collect();
                let kinds: Vec<u32> =
                    self.graph_events_pending.iter().map(|r| r.kind as u32).collect();
                let key_a: Vec<u64> = self.graph_events_pending.iter().map(|r| r.key_a).collect();
                let key_b: Vec<u64> = self.graph_events_pending.iter().map(|r| r.key_b).collect();
                self.graph_events_pending.clear();
                self.io.write_plain_u64h(len, graph_ids)?;
                self.io.write_plain_u32h(len, kinds)?;
                self.io.write_plain_u64h(len, key_a)?;
                self.io.write_plain_u64h(len, key_b)?;
                self.io.flush()?;
            }
            FuncProcId::FetchGraphMetrics => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchGraphMetrics");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.graph_metrics_graph_ids.len();
                self.io.write_plain_u64h(len, self.graph_metrics_graph_ids.drain(..))?;
                self.io.write_plain_u32h(len, self.graph_metrics_node_count.drain(..))?;
                self.io.write_plain_u32h(len, self.graph_metrics_edge_count.drain(..))?;
                self.io.write_plain_u64h(len, self.graph_metrics_fr_steps.drain(..))?;
                let last_disp_count = self.graph_metrics_fr_last_disp.len();
                self.io.write_plain_u32(last_disp_count as u32)?;
                for v in self.graph_metrics_fr_last_disp.drain(..) {
                    self.io.write_plain_f32(v)?;
                }
                self.io.flush()?;
            }
            FuncProcId::FetchGraphSelection => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchGraphSelection");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.graph_selection_graph_ids.len();
                self.io.write_plain_u64h(len, self.graph_selection_graph_ids.drain(..))?;
                self.io
                    .write_plain_u32h(len, self.graph_selection_kind.drain(..).map(|k| k as u32))?;
                self.io.write_plain_u64h(len, self.graph_selection_key_a.drain(..))?;
                self.io.write_plain_u64h(len, self.graph_selection_key_b.drain(..))?;
                self.io.flush()?;
            }
            FuncProcId::FetchR10 => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR10");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                self.io.write_plain_u64h(self.r10_true_ids.len(), self.r10_true_ids.drain(..))?;
                self.io.write_plain_u64h(self.r10_false_ids.len(), self.r10_false_ids.drain(..))?;
                self.io.flush()?;
            }
            FuncProcId::FetchR15WalkersCameras => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR15WalkersCameras");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                // Non-consuming read: several readers per frame (e.g. an overlay emitter and
                // an on-screen camera readout) must see the same value.
                //
                // RETAINED, not drained: `walkers_cameras` keeps each map's last camera
                // until that map renders again, so a reader between renders — Go-side heatmap
                // work runs a frame behind the viewport — still gets a valid camera. One entry
                // per map id ever rendered; the count is the number of walkers maps, so it is
                // bounded the way the dock's state map is.
                //
                // KEYED by map id. This was a single slot until 2026-08-04, which meant the
                // last map to render in a frame was the only one anyone could read: two maps
                // (two windows of one app, or play beside terrainscope) and a reader either
                // got the wrong camera or, once it checked the id, none at all. Callers now
                // look up their own map and cannot be taken over.
                //
                // Collected once so all 14 arrays iterate in the SAME order — HashMap order is
                // arbitrary but stable for an unmutated map, and this makes that explicit
                // rather than load-bearing.
                let cams: Vec<&WalkersCamera> = self.walkers_cameras.values().collect();
                let len = cams.len();
                self.io.write_plain_u64h(len, cams.iter().map(|c| c.map_id))?;
                self.io.write_plain_f64h(len, cams.iter().map(|c| c.zoom))?;
                self.io.write_plain_f64h(len, cams.iter().map(|c| c.center_lat))?;
                self.io.write_plain_f64h(len, cams.iter().map(|c| c.center_lon))?;
                self.io.write_plain_f64h(len, cams.iter().map(|c| c.min_lat))?;
                self.io.write_plain_f64h(len, cams.iter().map(|c| c.min_lon))?;
                self.io.write_plain_f64h(len, cams.iter().map(|c| c.max_lat))?;
                self.io.write_plain_f64h(len, cams.iter().map(|c| c.max_lon))?;
                self.io.write_plain_f32h(len, cams.iter().map(|c| c.screen_width_px))?;
                self.io.write_plain_f32h(len, cams.iter().map(|c| c.screen_height_px))?;
                self.io.write_plain_f64h(len, cams.iter().map(|c| c.hover_lat))?;
                self.io.write_plain_f64h(len, cams.iter().map(|c| c.hover_lon))?;
                // Two bools per row as a flag byte (bit0 hoverValid, bit1 clicked), the same
                // shape r24 uses for its modifiers.
                self.io.write_plain_u8h(
                    len,
                    cams.iter().map(|c| (c.hover_valid as u8) | ((c.clicked as u8) << 1)),
                )?;
                self.io.write_plain_u64h(len, cams.iter().map(|c| c.view_hash))?;
                self.io.flush()?;
            }
            FuncProcId::FetchR16ScrollDelta => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR16ScrollDelta");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let d = c.input(|i| i.smooth_scroll_delta);
                self.io.write_plain_f32(d.x)?;
                self.io.write_plain_f32(d.y)?;
                self.io.flush()?;
            }
            FuncProcId::FetchR17Modifiers => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR17Modifiers");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let m = c.input(|i| i.modifiers);
                self.io.write_plain_b(m.alt)?;
                self.io.write_plain_b(m.ctrl)?;
                self.io.write_plain_b(m.shift)?;
                self.io.write_plain_b(m.mac_cmd)?;
                self.io.write_plain_b(m.command)?;
                self.io.flush()?;
            }
            FuncProcId::FetchR18AvailableSize => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR18AvailableSize");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                self.io.write_plain_f32(self.r18_avail_w)?;
                self.io.write_plain_f32(self.r18_avail_h)?;
                self.io.flush()?;
            }
            FuncProcId::FetchR19ZoomDelta => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR19ZoomDelta");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let z = c.input(|i| i.zoom_delta());
                self.io.write_plain_f32(z)?;
                self.io.flush()?;
            }
            FuncProcId::FetchR20Pointer => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR20Pointer");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let pos = c.input(|i| i.pointer.latest_pos());
                let (px, py, valid) = match pos {
                    Some(p) => (p.x, p.y, true),
                    None => (f32::NAN, f32::NAN, false),
                };
                self.io.write_plain_f32(px)?;
                self.io.write_plain_f32(py)?;
                self.io.write_plain_b(valid)?;
                self.io.flush()?;
            }
            FuncProcId::FetchR21UiRects => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR21UiRects");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.r21_ui_rect_seqs.len();
                debug_assert_eq!(len, self.r21_ui_rect_min_x.len());
                debug_assert_eq!(len, self.r21_ui_rect_min_y.len());
                debug_assert_eq!(len, self.r21_ui_rect_max_x.len());
                debug_assert_eq!(len, self.r21_ui_rect_max_y.len());
                self.io.write_plain_u64h(len, self.r21_ui_rect_seqs.drain(..))?;
                self.io.write_plain_f32h(len, self.r21_ui_rect_min_x.drain(..))?;
                self.io.write_plain_f32h(len, self.r21_ui_rect_min_y.drain(..))?;
                self.io.write_plain_f32h(len, self.r21_ui_rect_max_x.drain(..))?;
                self.io.write_plain_f32h(len, self.r21_ui_rect_max_y.drain(..))?;
                self.io.flush()?;
            }
            FuncProcId::FetchR22StarvedTextures => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR22StarvedTextures");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.r22_starved_texture_ids.len();
                self.io.write_plain_u64h(len, self.r22_starved_texture_ids.drain(..))?;
                self.io.flush()?;
            }
            FuncProcId::FetchR23CanvasWheel => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR23CanvasWheel");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.r23_canvas_wheel_ids.len();
                debug_assert_eq!(len, self.r23_canvas_wheel_scroll_x.len());
                debug_assert_eq!(len, self.r23_canvas_wheel_scroll_y.len());
                debug_assert_eq!(len, self.r23_canvas_wheel_zoom.len());
                debug_assert_eq!(len, self.r23_canvas_wheel_hover_x.len());
                debug_assert_eq!(len, self.r23_canvas_wheel_hover_y.len());
                self.io.write_plain_u64h(len, self.r23_canvas_wheel_ids.drain(..))?;
                self.io.write_plain_f32h(len, self.r23_canvas_wheel_scroll_x.drain(..))?;
                self.io.write_plain_f32h(len, self.r23_canvas_wheel_scroll_y.drain(..))?;
                self.io.write_plain_f32h(len, self.r23_canvas_wheel_zoom.drain(..))?;
                self.io.write_plain_f32h(len, self.r23_canvas_wheel_hover_x.drain(..))?;
                self.io.write_plain_f32h(len, self.r23_canvas_wheel_hover_y.drain(..))?;
                self.io.flush()?;
            }
            FuncProcId::FetchR24CanvasPointers => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR24CanvasPointers");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.r24_canvas_pointer_ids.len();
                debug_assert_eq!(len, self.r24_canvas_pointer_origin_x.len());
                debug_assert_eq!(len, self.r24_canvas_pointer_origin_y.len());
                debug_assert_eq!(len, self.r24_canvas_pointer_pos_x.len());
                debug_assert_eq!(len, self.r24_canvas_pointer_pos_y.len());
                debug_assert_eq!(len, self.r24_canvas_pointer_mods.len());
                self.io.write_plain_u64h(len, self.r24_canvas_pointer_ids.drain(..))?;
                self.io.write_plain_f32h(len, self.r24_canvas_pointer_origin_x.drain(..))?;
                self.io.write_plain_f32h(len, self.r24_canvas_pointer_origin_y.drain(..))?;
                self.io.write_plain_f32h(len, self.r24_canvas_pointer_pos_x.drain(..))?;
                self.io.write_plain_f32h(len, self.r24_canvas_pointer_pos_y.drain(..))?;
                self.io.write_plain_u8h(len, self.r24_canvas_pointer_mods.drain(..))?;
                self.io.flush()?;
            }
            FuncProcId::FetchR25EtColWidths => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR25EtColWidths");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.r25_et_colwidth_ids.len();
                let vlen = self.r25_et_colwidth_values.len();
                self.io.write_plain_u64h(len, self.r25_et_colwidth_ids.drain(..))?;
                self.io.write_plain_u64h(len, self.r25_et_colwidth_counts.drain(..))?;
                self.io.write_plain_f32h(vlen, self.r25_et_colwidth_values.drain(..))?;
                self.io.flush()?;
            }
            FuncProcId::FetchR7 => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR7");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.r7_ids.len();
                self.io.write_plain_u64h(len, self.r7_ids.drain(..))?;
                self.io.write_plain_u32h(len, self.r7_responses.drain(..).map(|c| c.bits()))?;
                self.io.flush()?;
            }
            FuncProcId::FetchR9EtPrefetch => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR9EtPrefetch");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.r9_et_prefetch_ids.len();
                self.io.write_plain_u64h(len, self.r9_et_prefetch_ids.drain(..))?;
                self.io.write_plain_u64h(len * 5, self.r9_et_prefetch_values.drain(..))?;
                self.io.flush()?;
            }
            FuncProcId::FetchR9F64 => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR9F64");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.r9_f64_ids.len();
                self.io.write_plain_u64h(len, self.r9_f64_ids.drain(..))?;
                self.io.write_plain_f64h(len, self.r9_f64_values.drain(..))?;
                self.io.flush()?;
            }
            FuncProcId::FetchR9I64 => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR9I64");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.r9_i64_ids.len();
                self.io.write_plain_u64h(len, self.r9_i64_ids.drain(..))?;
                self.io.write_plain_i64h(len, self.r9_i64_values.drain(..))?;
                self.io.flush()?;
            }
            FuncProcId::FetchR9S => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR9S");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.r9_s_ids.len();
                self.io.write_plain_u64h(len, self.r9_s_ids.drain(..))?;
                self.io.write_plain_sh(len, self.r9_s_values.drain(..))?;
                self.io.flush()?;
            }
            FuncProcId::FetchR9U64 => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchR9U64");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.r9_u64_ids.len();
                self.io.write_plain_u64h(len, self.r9_u64_ids.drain(..))?;
                self.io.write_plain_u64h(len, self.r9_u64_values.drain(..))?;
                self.io.flush()?;
            }
            FuncProcId::FetchSnarlEvents => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchSnarlEvents");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.snarl_events_pending.len();
                let editor_ids: Vec<u64> =
                    self.snarl_events_pending.iter().map(|r| r.editor_id).collect();
                let kinds: Vec<u32> =
                    self.snarl_events_pending.iter().map(|r| r.kind as u32).collect();
                let node_ids: Vec<u64> =
                    self.snarl_events_pending.iter().map(|r| r.node_id).collect();
                let ports_a: Vec<u32> =
                    self.snarl_events_pending.iter().map(|r| r.port_a).collect();
                let node_ids_b: Vec<u64> =
                    self.snarl_events_pending.iter().map(|r| r.node_id_b).collect();
                let ports_b: Vec<u32> =
                    self.snarl_events_pending.iter().map(|r| r.port_b).collect();
                let xs: Vec<f32> = self.snarl_events_pending.iter().map(|r| r.x).collect();
                let ys: Vec<f32> = self.snarl_events_pending.iter().map(|r| r.y).collect();
                self.snarl_events_pending.clear();
                self.io.write_plain_u64h(len, editor_ids)?;
                self.io.write_plain_u32h(len, kinds)?;
                self.io.write_plain_u64h(len, node_ids)?;
                self.io.write_plain_u32h(len, ports_a)?;
                self.io.write_plain_u64h(len, node_ids_b)?;
                self.io.write_plain_u32h(len, ports_b)?;
                self.io.write_plain_f32h(len, xs)?;
                self.io.write_plain_f32h(len, ys)?;
                self.io.flush()?;
            }
            FuncProcId::FetchVideoCapabilities => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchVideoCapabilities");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.video_cap_ids.len();
                self.io.write_plain_u64h(len, self.video_cap_ids.drain(..))?;
                self.io.write_plain_u32h(len, self.video_cap_flags.drain(..))?;
                self.io.flush()?;
            }
            FuncProcId::FetchVideoStreamInfo => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::FetchVideoStreamInfo");
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let len = self.video_stream_info.len();
                self.io.write_plain_u64h(len, self.video_stream_info.drain(..))?;
                self.io.flush()?;
            }
            FuncProcId::Frame => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Frame");
                // arguments
                let i = self.read_id()?;
                // construct

                #[allow(unused_mut)]
                let mut w = egui::Frame::new();
                let mut sense_click = false;
                let mut sense_drag = false;
                let mut hover_cursor_pointer = false;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(FrameBuilderMethodId::from_repr)?;
                    match m {
                        FrameBuilderMethodId::Build => {
                            break;
                        }
                        FrameBuilderMethodId::InnerMargin => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::InnerMargin");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.inner_margin(val);
                        }
                        FrameBuilderMethodId::OuterMargin => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::OuterMargin");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.outer_margin(val);
                        }
                        FrameBuilderMethodId::CornerRadius => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::CornerRadius");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.corner_radius(val);
                        }
                        FrameBuilderMethodId::InnerMarginSides => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::InnerMarginSides");
                            #[allow(unused_mut)]
                            let mut left = self.io.read_plain_f32()?;
                            #[allow(unused_mut)]
                            let mut right = self.io.read_plain_f32()?;
                            #[allow(unused_mut)]
                            let mut top = self.io.read_plain_f32()?;
                            #[allow(unused_mut)]
                            let mut bottom = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.inner_margin(egui::Margin {
                                left: left as i8,
                                right: right as i8,
                                top: top as i8,
                                bottom: bottom as i8,
                            });
                        }
                        FrameBuilderMethodId::OuterMarginSides => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::OuterMarginSides");
                            #[allow(unused_mut)]
                            let mut left = self.io.read_plain_f32()?;
                            #[allow(unused_mut)]
                            let mut right = self.io.read_plain_f32()?;
                            #[allow(unused_mut)]
                            let mut top = self.io.read_plain_f32()?;
                            #[allow(unused_mut)]
                            let mut bottom = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.outer_margin(egui::Margin {
                                left: left as i8,
                                right: right as i8,
                                top: top as i8,
                                bottom: bottom as i8,
                            });
                        }
                        FrameBuilderMethodId::CornerRadiusSides => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::CornerRadiusSides");
                            #[allow(unused_mut)]
                            let mut nw = self.io.read_plain_u8()?;
                            #[allow(unused_mut)]
                            let mut ne = self.io.read_plain_u8()?;
                            #[allow(unused_mut)]
                            let mut sw = self.io.read_plain_u8()?;
                            #[allow(unused_mut)]
                            let mut se = self.io.read_plain_u8()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.corner_radius(egui::CornerRadius { nw, ne, sw, se });
                        }
                        FrameBuilderMethodId::Fill => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::Fill");

                            let col = {
                                let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                                let u2: &mut Option<&mut egui::Ui> = &mut None;
                                if u2.is_some() {
                                    self.interpret_inner(c, u2, &f2, d + 1)?;
                                } else {
                                    self.interpret_inner(c, u, &f2, d + 1)?;
                                }

                                self.r11_color32
                            };
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.fill(col);
                        }
                        FrameBuilderMethodId::Stroke => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::Stroke");
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;

                            let col = {
                                let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                                let u2: &mut Option<&mut egui::Ui> = &mut None;
                                if u2.is_some() {
                                    self.interpret_inner(c, u2, &f2, d + 1)?;
                                } else {
                                    self.interpret_inner(c, u, &f2, d + 1)?;
                                }

                                self.r11_color32
                            };
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.stroke(egui::Stroke::new(width, col));
                        }
                        FrameBuilderMethodId::Shadow => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::Shadow");
                            #[allow(unused_mut)]
                            let mut offset_x = self.io.read_plain_f32()?;
                            #[allow(unused_mut)]
                            let mut offset_y = self.io.read_plain_f32()?;
                            #[allow(unused_mut)]
                            let mut blur = self.io.read_plain_u8()?;
                            #[allow(unused_mut)]
                            let mut spread = self.io.read_plain_u8()?;

                            let col = {
                                let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                                let u2: &mut Option<&mut egui::Ui> = &mut None;
                                if u2.is_some() {
                                    self.interpret_inner(c, u2, &f2, d + 1)?;
                                } else {
                                    self.interpret_inner(c, u, &f2, d + 1)?;
                                }

                                self.r11_color32
                            };
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.shadow(egui::Shadow {
                                offset: [offset_x as i8, offset_y as i8],
                                blur,
                                spread,
                                color: col,
                            });
                        }
                        FrameBuilderMethodId::MultiplyWithOpacity => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match FrameBuilderMethodId::MultiplyWithOpacity"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.multiply_with_opacity(val);
                        }
                        FrameBuilderMethodId::SenseClick => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::SenseClick");
                            sense_click = true;
                        }
                        FrameBuilderMethodId::SenseDrag => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::SenseDrag");
                            sense_drag = true;
                        }
                        FrameBuilderMethodId::HoverCursorPointer => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match FrameBuilderMethodId::HoverCursorPointer"
                            );
                            hover_cursor_pointer = true;
                        }
                        FrameBuilderMethodId::PresetGroup => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::PresetGroup");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Frame::group(c.style_of(c.theme()).as_ref());
                        }
                        FrameBuilderMethodId::PresetWindow => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::PresetWindow");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Frame::window(c.style_of(c.theme()).as_ref());
                        }
                        FrameBuilderMethodId::PresetPopup => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::PresetPopup");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Frame::popup(c.style_of(c.theme()).as_ref());
                        }
                        FrameBuilderMethodId::PresetMenu => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::PresetMenu");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Frame::menu(c.style_of(c.theme()).as_ref());
                        }
                        FrameBuilderMethodId::PresetCanvas => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::PresetCanvas");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Frame::canvas(c.style_of(c.theme()).as_ref());
                        }
                        FrameBuilderMethodId::PresetDarkCanvas => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match FrameBuilderMethodId::PresetDarkCanvas");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Frame::dark_canvas(c.style_of(c.theme()).as_ref());
                        }
                        FrameBuilderMethodId::PresetSideTopPanel => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match FrameBuilderMethodId::PresetSideTopPanel"
                            );
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Frame::side_top_panel(c.style_of(c.theme()).as_ref());
                        }
                        FrameBuilderMethodId::PresetCentralPanel => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match FrameBuilderMethodId::PresetCentralPanel"
                            );
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui::Frame::central_panel(c.style_of(c.theme()).as_ref());
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let ui = u.as_mut().unwrap();
                    let r2 = w.show(ui, |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                    let mut resp2 = ResponseFlags::empty();
                    if sense_click || sense_drag {
                        let mut sense = egui::Sense::hover();
                        if sense_click {
                            sense = sense | egui::Sense::click();
                        }
                        if sense_drag {
                            sense = sense | egui::Sense::drag();
                        }
                        let response = ui.interact(
                            r2.response.rect,
                            egui::Id::new(i.value()).with("sense"),
                            sense,
                        );
                        let response = if hover_cursor_pointer {
                            response.on_hover_cursor(egui::CursorIcon::PointingHand)
                        } else {
                            response
                        };
                        resp2.populate(&response);
                    } else {
                        resp2.populate(&r2.response);
                    }
                    self.r7_push(i.value(), resp2);
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::Graph => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Graph");
                // arguments
                let i = self.read_id()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                // Dimensions: 0 (unset) = fill available space on that axis; any
                // positive value = fixed pixel size. With both axes left at 0 the
                // graph flows with its container on window resizes, which is the
                // natural default for a canvas widget.
                let mut gv_width: f32 = 0.0;
                let mut gv_height: f32 = 0.0;
                let mut dragging_enabled = true;
                let mut hover_enabled = true;
                let mut node_clicking_enabled = false;
                let mut node_selection_enabled = false;
                let mut node_selection_multi_enabled = false;
                let mut edge_clicking_enabled = false;
                let mut edge_selection_enabled = false;
                let mut edge_selection_multi_enabled = false;
                // fit_to_screen here means *continuous* fit — re-fit every frame. Off by
                // default; the one-shot fit latch (GraphState.fit_pending) handles the
                // initial framing and fitNow()/resetLayout re-fit on demand. Set true
                // only to force the legacy always-fit behaviour.
                let mut fit_to_screen = false;
                let mut fit_now_flag: bool = false;
                let mut zoom_and_pan = true;
                let mut fit_padding: f32 = 0.1;
                let mut zoom_speed: f32 = 0.1;
                let mut labels_always = false;
                let mut layout_kind: u8 = 0;
                let mut reset_layout_flag: bool = false;
                let mut fast_forward_steps: u32 = 0;
                // FR tunables (layout kinds 1 and 2) — twin _set flags so only fields
                // the user actually touched this frame get overlaid onto the persisted
                // state.
                let mut fr_dt: f32 = 0.0;
                let mut fr_dt_set = false;
                let mut fr_damping: f32 = 0.0;
                let mut fr_damping_set = false;
                let mut fr_epsilon: f32 = 0.0;
                let mut fr_epsilon_set = false;
                let mut fr_max_step: f32 = 0.0;
                let mut fr_max_step_set = false;
                let mut fr_k_scale: f32 = 0.0;
                let mut fr_k_scale_set = false;
                let mut fr_c_attract: f32 = 0.0;
                let mut fr_c_attract_set = false;
                let mut fr_c_repulse: f32 = 0.0;
                let mut fr_c_repulse_set = false;
                let mut fr_is_running: bool = false;
                let mut fr_is_running_set = false;
                // Hierarchical tunables (layout kind 3).
                let mut hi_row_dist: f32 = 0.0;
                let mut hi_row_dist_set = false;
                let mut hi_col_dist: f32 = 0.0;
                let mut hi_col_dist_set = false;
                let mut hi_center_parent: bool = false;
                let mut hi_center_parent_set = false;
                let mut hi_orientation: u8 = 0;
                let mut hi_orientation_set = false;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(GraphBuilderMethodId::from_repr)?;
                    match m {
                        GraphBuilderMethodId::Build => {
                            break;
                        }
                        GraphBuilderMethodId::Width => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::Width");
                            #[allow(unused_mut)]
                            let mut wi = self.io.read_plain_f32()?;
                            gv_width = wi;
                        }
                        GraphBuilderMethodId::Height => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::Height");
                            #[allow(unused_mut)]
                            let mut he = self.io.read_plain_f32()?;
                            gv_height = he;
                        }
                        GraphBuilderMethodId::DraggingEnabled => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::DraggingEnabled");
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            dragging_enabled = vl;
                        }
                        GraphBuilderMethodId::HoverEnabled => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::HoverEnabled");
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            hover_enabled = vl;
                        }
                        GraphBuilderMethodId::NodeClickingEnabled => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match GraphBuilderMethodId::NodeClickingEnabled"
                            );
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            node_clicking_enabled = vl;
                        }
                        GraphBuilderMethodId::NodeSelectionEnabled => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match GraphBuilderMethodId::NodeSelectionEnabled"
                            );
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            node_selection_enabled = vl;
                        }
                        GraphBuilderMethodId::NodeSelectionMultiEnabled => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match GraphBuilderMethodId::NodeSelectionMultiEnabled"
                            );
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            node_selection_multi_enabled = vl;
                        }
                        GraphBuilderMethodId::EdgeClickingEnabled => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match GraphBuilderMethodId::EdgeClickingEnabled"
                            );
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            edge_clicking_enabled = vl;
                        }
                        GraphBuilderMethodId::EdgeSelectionEnabled => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match GraphBuilderMethodId::EdgeSelectionEnabled"
                            );
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            edge_selection_enabled = vl;
                        }
                        GraphBuilderMethodId::EdgeSelectionMultiEnabled => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match GraphBuilderMethodId::EdgeSelectionMultiEnabled"
                            );
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            edge_selection_multi_enabled = vl;
                        }
                        GraphBuilderMethodId::FitToScreen => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::FitToScreen");
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            fit_to_screen = vl;
                        }
                        GraphBuilderMethodId::FitNow => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::FitNow");
                            fit_now_flag = true;
                        }
                        GraphBuilderMethodId::ZoomAndPan => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::ZoomAndPan");
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            zoom_and_pan = vl;
                        }
                        GraphBuilderMethodId::FitPadding => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::FitPadding");
                            #[allow(unused_mut)]
                            let mut pd = self.io.read_plain_f32()?;
                            fit_padding = pd;
                        }
                        GraphBuilderMethodId::ZoomSpeed => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::ZoomSpeed");
                            #[allow(unused_mut)]
                            let mut sp = self.io.read_plain_f32()?;
                            zoom_speed = sp;
                        }
                        GraphBuilderMethodId::LabelsAlways => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::LabelsAlways");
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            labels_always = vl;
                        }
                        GraphBuilderMethodId::Layout => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::Layout");
                            #[allow(unused_mut)]
                            let mut kind = self.io.read_plain_u8()?;
                            layout_kind = kind;
                        }
                        GraphBuilderMethodId::ResetLayout => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::ResetLayout");
                            reset_layout_flag = true;
                        }
                        GraphBuilderMethodId::FastForwardSteps => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::FastForwardSteps");
                            #[allow(unused_mut)]
                            let mut st = self.io.read_plain_u32()?;
                            fast_forward_steps = st;
                        }
                        GraphBuilderMethodId::LayoutDt => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::LayoutDt");
                            #[allow(unused_mut)]
                            let mut dt = self.io.read_plain_f32()?;
                            fr_dt = dt;
                            fr_dt_set = true;
                        }
                        GraphBuilderMethodId::LayoutDamping => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::LayoutDamping");
                            #[allow(unused_mut)]
                            let mut dp = self.io.read_plain_f32()?;
                            fr_damping = dp;
                            fr_damping_set = true;
                        }
                        GraphBuilderMethodId::LayoutEpsilon => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::LayoutEpsilon");
                            #[allow(unused_mut)]
                            let mut ep = self.io.read_plain_f32()?;
                            fr_epsilon = ep;
                            fr_epsilon_set = true;
                        }
                        GraphBuilderMethodId::LayoutMaxStep => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::LayoutMaxStep");
                            #[allow(unused_mut)]
                            let mut ms = self.io.read_plain_f32()?;
                            fr_max_step = ms;
                            fr_max_step_set = true;
                        }
                        GraphBuilderMethodId::LayoutKScale => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::LayoutKScale");
                            #[allow(unused_mut)]
                            let mut ks = self.io.read_plain_f32()?;
                            fr_k_scale = ks;
                            fr_k_scale_set = true;
                        }
                        GraphBuilderMethodId::LayoutCAttract => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::LayoutCAttract");
                            #[allow(unused_mut)]
                            let mut ca = self.io.read_plain_f32()?;
                            fr_c_attract = ca;
                            fr_c_attract_set = true;
                        }
                        GraphBuilderMethodId::LayoutCRepulse => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::LayoutCRepulse");
                            #[allow(unused_mut)]
                            let mut cr = self.io.read_plain_f32()?;
                            fr_c_repulse = cr;
                            fr_c_repulse_set = true;
                        }
                        GraphBuilderMethodId::LayoutRunning => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::LayoutRunning");
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            fr_is_running = vl;
                            fr_is_running_set = true;
                        }
                        GraphBuilderMethodId::LayoutRowDist => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::LayoutRowDist");
                            #[allow(unused_mut)]
                            let mut rd = self.io.read_plain_f32()?;
                            hi_row_dist = rd;
                            hi_row_dist_set = true;
                        }
                        GraphBuilderMethodId::LayoutColDist => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::LayoutColDist");
                            #[allow(unused_mut)]
                            let mut cd = self.io.read_plain_f32()?;
                            hi_col_dist = cd;
                            hi_col_dist_set = true;
                        }
                        GraphBuilderMethodId::LayoutCenterParent => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match GraphBuilderMethodId::LayoutCenterParent"
                            );
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            hi_center_parent = vl;
                            hi_center_parent_set = true;
                        }
                        GraphBuilderMethodId::LayoutOrientation => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphBuilderMethodId::LayoutOrientation");
                            #[allow(unused_mut)]
                            let mut or = self.io.read_plain_u8()?;
                            hi_orientation = or;
                            hi_orientation_set = true;
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let ui = u.as_mut().unwrap();
                    let pending_nodes: Vec<GraphNodeData> =
                        self.graph_pending_nodes.drain(..).collect();
                    let pending_edges: Vec<GraphEdgeData> =
                        self.graph_pending_edges.drain(..).collect();

                    let gid = i.value();
                    let state = self.graph_states.entry(gid).or_insert_with(new_graph_state);
                    reconcile_graph_state(state, &pending_nodes, &pending_edges);

                    let interaction = egui_graphs::SettingsInteraction::default()
                        .with_dragging_enabled(dragging_enabled)
                        .with_hover_enabled(hover_enabled)
                        .with_node_clicking_enabled(node_clicking_enabled)
                        .with_node_selection_enabled(node_selection_enabled)
                        .with_node_selection_multi_enabled(node_selection_multi_enabled)
                        .with_edge_clicking_enabled(edge_clicking_enabled)
                        .with_edge_selection_enabled(edge_selection_enabled)
                        .with_edge_selection_multi_enabled(edge_selection_multi_enabled);
                    // One-shot fit: fit only while a freshly (re)laid-out graph settles,
                    // then latch off so manual pan/zoom sticks and the view stops
                    // rescaling every frame. fit_to_screen forces legacy continuous
                    // fit; fitNow()/resetLayout re-arm the latch. See graph_fit_this_frame.
                    let layout_settled = graph_layout_settled(ui, gid, layout_kind);
                    let do_fit = graph_fit_this_frame(
                        state,
                        fit_to_screen,
                        fit_now_flag || reset_layout_flag,
                        layout_settled,
                    );
                    let navigation = egui_graphs::SettingsNavigation::default()
                        .with_fit_to_screen_enabled(do_fit)
                        .with_zoom_and_pan_enabled(zoom_and_pan)
                        .with_fit_to_screen_padding(fit_padding)
                        .with_zoom_speed(zoom_speed);
                    let style =
                        egui_graphs::SettingsStyle::default().with_labels_always(labels_always);

                    // Zero along either axis → fill the container's available space for
                    // that axis; non-zero → use the caller-supplied fixed dimension.
                    let avail = ui.available_size();
                    let size = egui::vec2(
                        if gv_width > 0.0 { gv_width } else { avail.x },
                        if gv_height > 0.0 { gv_height } else { avail.y },
                    );
                    // Collect interaction events via a local sink so the borrow stays
                    // scoped to this frame; after the GraphView drops we translate
                    // petgraph indices back to Go's u64 keys and push to the global
                    // graph_events_pending register for fetchGraphEvents to drain.
                    let frame_events: std::cell::RefCell<Vec<egui_graphs::events::Event>> =
                        std::cell::RefCell::new(Vec::new());
                    let sink = |e: egui_graphs::events::Event| {
                        frame_events.borrow_mut().push(e);
                    };
                    // Overlay any user-set layout tunables onto the persisted state,
                    // before the render call uses it.
                    apply_fr_overrides(
                        ui,
                        gid,
                        layout_kind,
                        fr_dt,
                        fr_dt_set,
                        fr_damping,
                        fr_damping_set,
                        fr_epsilon,
                        fr_epsilon_set,
                        fr_max_step,
                        fr_max_step_set,
                        fr_k_scale,
                        fr_k_scale_set,
                        fr_c_attract,
                        fr_c_attract_set,
                        fr_c_repulse,
                        fr_c_repulse_set,
                        fr_is_running,
                        fr_is_running_set,
                    );
                    apply_hierarchical_overrides(
                        ui,
                        gid,
                        layout_kind,
                        hi_row_dist,
                        hi_row_dist_set,
                        hi_col_dist,
                        hi_col_dist_set,
                        hi_center_parent,
                        hi_center_parent_set,
                        hi_orientation,
                        hi_orientation_set,
                    );
                    let graph_resp = render_graph_with_layout(
                        state,
                        ui,
                        size,
                        gid,
                        layout_kind,
                        reset_layout_flag,
                        fast_forward_steps,
                        &interaction,
                        &navigation,
                        &style,
                        &sink,
                    );
                    // Capture-scroll (widget-gallery composition): a navigable
                    // graph owns the wheel while the pointer is over it. Swallow
                    // any plain scroll delta so a parent ScrollArea — the gallery
                    // stacks every demo in one Vscroll — does not also scroll the
                    // page out from under the cursor. egui routes the wheel to
                    // zoom XOR scroll, so Ctrl/Cmd+scroll zoom already leaves
                    // smooth_scroll_delta at zero and is unaffected; a static
                    // graph (zoom_and_pan == false) lets the wheel pass through so
                    // it can still be scrolled into view.
                    if zoom_and_pan && graph_resp.contains_pointer() {
                        ui.input_mut(|i| i.smooth_scroll_delta = egui::Vec2::ZERO);
                    }
                    // Snapshot selection + metrics AFTER the render so they reflect
                    // anything egui_graphs did this frame (drag, click-select, layout
                    // step increment). Fetchers drain these vecs on the Go side.
                    snapshot_graph_selection(
                        gid,
                        state,
                        &mut self.graph_selection_graph_ids,
                        &mut self.graph_selection_kind,
                        &mut self.graph_selection_key_a,
                        &mut self.graph_selection_key_b,
                    );
                    snapshot_graph_metrics(
                        gid,
                        layout_kind,
                        state,
                        ui,
                        &mut self.graph_metrics_graph_ids,
                        &mut self.graph_metrics_node_count,
                        &mut self.graph_metrics_edge_count,
                        &mut self.graph_metrics_fr_steps,
                        &mut self.graph_metrics_fr_last_disp,
                    );
                    let evs = frame_events.into_inner();
                    if !evs.is_empty() {
                        for e in evs {
                            if let Some(rec) = translate_graph_event(gid, state, &e) {
                                self.graph_events_pending.push(rec);
                            }
                        }
                    }
                } else {
                    self.graph_pending_nodes.clear();
                    self.graph_pending_edges.clear();
                }
            }
            FuncProcId::GraphEdge => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::GraphEdge");
                // arguments
                #[allow(unused_mut)]
                let mut from_id = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut to_id = self.io.read_plain_u64()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut color: Option<egui::Color32> = None;
                let mut label: Option<String> = None;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(GraphEdgeBuilderMethodId::from_repr)?;
                    match m {
                        GraphEdgeBuilderMethodId::Build => {
                            break;
                        }
                        GraphEdgeBuilderMethodId::Color => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphEdgeBuilderMethodId::Color");
                            #[allow(unused_mut)]
                            let mut col = self.io.read_plain_u32()?;
                            color = Some(color32_from_rgba_u32(col));
                        }
                        GraphEdgeBuilderMethodId::Label => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphEdgeBuilderMethodId::Label");
                            #[allow(unused_mut)]
                            let mut text = self.io.read_plain_s()?;
                            label = Some(text);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.graph_pending_edges.push(GraphEdgeData {
                    from: from_id,
                    to: to_id,
                    label,
                    color,
                });
            }
            FuncProcId::GraphNode => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::GraphNode");
                // arguments
                #[allow(unused_mut)]
                let mut node_id = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut label = self.io.read_plain_s()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut color: Option<egui::Color32> = None;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(GraphNodeBuilderMethodId::from_repr)?;
                    match m {
                        GraphNodeBuilderMethodId::Build => {
                            break;
                        }
                        GraphNodeBuilderMethodId::Color => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GraphNodeBuilderMethodId::Color");
                            #[allow(unused_mut)]
                            let mut col = self.io.read_plain_u32()?;
                            color = Some(color32_from_rgba_u32(col));
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.graph_pending_nodes.push(GraphNodeData {
                    id: node_id,
                    label,
                    color,
                });
            }
            FuncProcId::Grid => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Grid");
                // arguments
                let i = self.read_id()?;
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
egui::Grid::new(i);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(GridBuilderMethodId::from_repr)?;
                    match m {
                        GridBuilderMethodId::Build => {
                            break;
                        }
                        GridBuilderMethodId::NumColumns => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GridBuilderMethodId::NumColumns");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.num_columns(val as usize);
                        }
                        GridBuilderMethodId::Striped => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GridBuilderMethodId::Striped");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.striped(val);
                        }
                        GridBuilderMethodId::MinColWidth => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GridBuilderMethodId::MinColWidth");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.min_col_width(val);
                        }
                        GridBuilderMethodId::MinRowHeight => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GridBuilderMethodId::MinRowHeight");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.min_row_height(val);
                        }
                        GridBuilderMethodId::MaxColWidth => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GridBuilderMethodId::MaxColWidth");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.max_col_width(val);
                        }
                        GridBuilderMethodId::StartRow => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match GridBuilderMethodId::StartRow");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_u64()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.start_row(val as usize);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    w.show(u.as_mut().unwrap(), |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::Group => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Group");
                // arguments
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().group(|ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::GuiZoomZoomMenuButtons => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::GuiZoomZoomMenuButtons");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    egui::gui_zoom::zoom_menu_buttons(u.as_mut().unwrap());
                }
            }
            FuncProcId::H3CellsColored => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::H3CellsColored");
                // arguments
                #[allow(unused_mut)]
                let mut cell_ids = self.io.read_plain_u64h()?;
                #[allow(unused_mut)]
                let mut cols = self.io.read_plain_u32h()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut stroke_width: f32 = 0.0;
                let mut stroke_color: egui::Color32 = egui::Color32::TRANSPARENT;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(H3CellsColoredBuilderMethodId::from_repr)?;
                    match m {
                        H3CellsColoredBuilderMethodId::Build => {
                            break;
                        }
                        H3CellsColoredBuilderMethodId::StrokeWidth => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match H3CellsColoredBuilderMethodId::StrokeWidth"
                            );
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            stroke_width = width;
                        }
                        H3CellsColoredBuilderMethodId::StrokeColor => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match H3CellsColoredBuilderMethodId::StrokeColor"
                            );
                            #[allow(unused_mut)]
                            let mut col = self.io.read_plain_u32()?;
                            stroke_color = color32_from_rgba_u32(col);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.walkers_pending_h3_choropleth.push(H3Choropleth {
                    cell_ids,
                    rgbas: cols,
                    stroke_width,
                    stroke_color,
                });
            }
            FuncProcId::H3Region => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::H3Region");
                // arguments
                #[allow(unused_mut)]
                let mut cell_ids = self.io.read_plain_u64h()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut fill: Option<egui::Color32> = None;
                let mut stroke: Option<egui::Stroke> = None;
                let mut label: Option<String> = None;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(H3RegionBuilderMethodId::from_repr)?;
                    match m {
                        H3RegionBuilderMethodId::Build => {
                            break;
                        }
                        H3RegionBuilderMethodId::Fill => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match H3RegionBuilderMethodId::Fill");
                            #[allow(unused_mut)]
                            let mut col = self.io.read_plain_u32()?;
                            fill = Some(color32_from_rgba_u32(col));
                        }
                        H3RegionBuilderMethodId::Stroke => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match H3RegionBuilderMethodId::Stroke");
                            #[allow(unused_mut)]
                            let mut col = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            stroke = Some(egui::Stroke::new(width, color32_from_rgba_u32(col)));
                        }
                        H3RegionBuilderMethodId::Label => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match H3RegionBuilderMethodId::Label");
                            #[allow(unused_mut)]
                            let mut text = self.io.read_plain_s()?;
                            label = Some(text);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.walkers_pending_h3_regions.push(H3Region {
                    cell_ids,
                    fill,
                    stroke,
                    label,
                });
            }
            FuncProcId::Horizontal => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Horizontal");
                // arguments
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().horizontal(|ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::HorizontalCentered => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::HorizontalCentered");
                // arguments
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().horizontal_centered(|ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::HorizontalTop => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::HorizontalTop");
                // arguments
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().horizontal_top(|ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::HorizontalWrapped => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::HorizontalWrapped");
                // arguments
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().horizontal_wrapped(|ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::HoverText => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::HoverText");
                // arguments
                #[allow(unused_mut)]
                let mut text = self.io.read_plain_s()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let ui = u.as_mut().unwrap();
                    let scope_resp = ui
                        .scope(|ui| {
                            let _ = self.interpret_outer_logged(c, &mut Some(ui));
                        })
                        .response;
                    let hover_resp = ui.interact(
                        scope_resp.rect,
                        scope_resp.id.with("imzero2_hover_text"),
                        egui::Sense::hover(),
                    );
                    let _ = hover_resp.on_hover_text(text);
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::HoverUi => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::HoverUi");
                // arguments
                // construct
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let mut tip_blocks = self.io.read_deferred_block_map_u32()?;
                    let mut target_blocks = self.io.read_deferred_block_map_u32()?;
                    let tip = tip_blocks.drain().next().map(|(_, v)| v);
                    let target = target_blocks.drain().next().map(|(_, v)| v);
                    let ui = u.as_mut().unwrap();
                    let scope_resp = ui
                        .scope(|ui| {
                            if let Some(block) = &target {
                                let _ = self.replay_deferred_block_logged(c, ui, block);
                            }
                        })
                        .response;
                    let hover_resp = ui.interact(
                        scope_resp.rect,
                        scope_resp.id.with("imzero2_hover_ui"),
                        egui::Sense::hover(),
                    );
                    if let Some(block) = tip {
                        let ctx_cloned = hover_resp.ctx.clone();
                        let _ = hover_resp.on_hover_ui(|ui| {
                            let _ = self.replay_deferred_block_logged(&ctx_cloned, ui, &block);
                        });
                    }
                } else {
                    self.io.skip_deferred_block_map_u32()?;
                    self.io.skip_deferred_block_map_u32()?;
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
            }
            FuncProcId::Hyperlink => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Hyperlink");
                // arguments
                #[allow(unused_mut)]
                let mut url = self.io.read_plain_s()?;
                // construct

                #[allow(unused_mut)]
                let mut w = egui::Hyperlink::from_label_and_url(url.clone(), url.clone());
                // methods
                loop {
                    let (m, _) = self.read_from_repr(HyperlinkBuilderMethodId::from_repr)?;
                    match m {
                        HyperlinkBuilderMethodId::Build => {
                            break;
                        }
                        HyperlinkBuilderMethodId::OpenInNewTab => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match HyperlinkBuilderMethodId::OpenInNewTab");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            w = w.open_in_new_tab(enabled);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply

                let resp = self.apply_widget(w, u, f, None);
                if let Some(r) = resp {
                    if let Ok(mut zones) = self.link_zones.lock() {
                        zones.push(crate::imzero2::svgexport::LinkZone {
                            rect: r.rect,
                            url: url.clone(),
                        });
                    }
                }
            }
            FuncProcId::HyperlinkTo => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::HyperlinkTo");
                // arguments
                #[allow(unused_mut)]
                let mut label = self.io.read_plain_s()?;
                #[allow(unused_mut)]
                let mut url = self.io.read_plain_s()?;
                // construct

                #[allow(unused_mut)]
                let mut w = egui::Hyperlink::from_label_and_url(label, url.clone());
                // methods
                loop {
                    let (m, _) = self.read_from_repr(HyperlinkToBuilderMethodId::from_repr)?;
                    match m {
                        HyperlinkToBuilderMethodId::Build => {
                            break;
                        }
                        HyperlinkToBuilderMethodId::OpenInNewTab => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match HyperlinkToBuilderMethodId::OpenInNewTab"
                            );
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            w = w.open_in_new_tab(enabled);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply

                let resp = self.apply_widget(w, u, f, None);
                if let Some(r) = resp {
                    if let Ok(mut zones) = self.link_zones.lock() {
                        zones.push(crate::imzero2::svgexport::LinkZone {
                            rect: r.rect,
                            url: url.clone(),
                        });
                    }
                }
            }
            FuncProcId::Image => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Image");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut width_px = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut height_px = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut content_version = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut fit = self.io.read_plain_u8()?;
                #[allow(unused_mut)]
                let mut fixed_w = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut fixed_h = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut filter = self.io.read_plain_u8()?;
                #[allow(unused_mut)]
                let mut tint_rgba = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut pixels = self.io.read_plain_u32h()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let ui = u.as_mut().unwrap();
                    let (resp, hover_rc, starved) = self.image_cache.show(
                        ui,
                        c,
                        i.value(),
                        width_px,
                        height_px,
                        content_version,
                        fit,
                        fixed_w,
                        fixed_h,
                        filter,
                        tint_rgba,
                        &pixels,
                    );
                    if starved {
                        // Interpreted with no pixels and no cache entry — report so the
                        // Go-side sender re-arms and re-ships (fetchR22StarvedTextures).
                        self.r22_starved_texture_ids.push(i.value());
                    }
                    if self.r8_response_flags_filter.match_response_any(&resp) {
                        let mut res = ResponseFlags::empty();
                        res.populate(&resp);
                        self.r7_push(i.value(), res);
                    }
                    self.r9_u64_push(i.value(), hover_rc);
                }
            }
            FuncProcId::ImageRelease => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::ImageRelease");
                // arguments
                let i = self.read_id()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.image_cache.release(i.value());
            }
            FuncProcId::Indent => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Indent");
                // arguments
                let i = self.read_id()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().indent(i, |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::Label => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Label");
                // arguments
                #[allow(unused_mut)]
                let mut text = self.io.read_plain_s()?;
                // construct

                #[allow(unused_mut)]
                let mut w = egui::Label::new(text);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(LabelBuilderMethodId::from_repr)?;
                    match m {
                        LabelBuilderMethodId::Build => {
                            break;
                        }
                        LabelBuilderMethodId::Selectable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match LabelBuilderMethodId::Selectable");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.selectable(val);
                        }
                        LabelBuilderMethodId::Wrap => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match LabelBuilderMethodId::Wrap");
                            w = w.wrap();
                        }
                        LabelBuilderMethodId::Truncate => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match LabelBuilderMethodId::Truncate");
                            w = w.truncate();
                        }
                        LabelBuilderMethodId::Extend => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match LabelBuilderMethodId::Extend");
                            w = w.extend();
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.apply_widget(w, u, f, None);
            }
            FuncProcId::LabelAtoms => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::LabelAtoms");
                // arguments

                let atoms = {
                    let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                    let u2: &mut Option<&mut egui::Ui> = &mut None;
                    if u2.is_some() {
                        self.interpret_inner(c, u2, &f2, d + 1)?;
                    } else {
                        self.interpret_inner(c, u, &f2, d + 1)?;
                    }

                    std::mem::take(&mut self.r0_atoms)
                };
                // construct

                #[allow(unused_mut)]
                let mut w = {
                    // Flatten atoms into a single LayoutJob so egui's text shaper word-wraps
                    // across style boundaries. Atoms' native AtomLayout only lets one atom
                    // (the first text atom, auto-shrunk) wrap inside itself; every other
                    // atom is sized to its intrinsic width. In paragraphs whose non-shrink
                    // atoms exceed the available width, the shrink atom collapses to ~0
                    // and the shaper falls back to character-by-character wrapping. A
                    // LayoutJob with one section per styled span sidesteps that — the
                    // shaper sees one continuous run and breaks on word boundaries.
                    let style = c.style_of(c.theme());
                    let mut lj = egui::text::LayoutJob::default();
                    for atom in atoms.into_iter() {
                        if let egui::AtomKind::Text(wt) = atom.kind {
                            match wt {
                                egui::WidgetText::RichText(rt) => {
                                    std::sync::Arc::unwrap_or_clone(rt).append_to(
                                        &mut lj,
                                        &style,
                                        egui::FontSelection::Default,
                                        egui::Align::Center,
                                    );
                                }
                                egui::WidgetText::Text(s) => {
                                    let format = egui::TextFormat {
                                        font_id: egui::FontSelection::Default.resolve(&style),
                                        color: style.visuals.text_color(),
                                        ..Default::default()
                                    };
                                    lj.append(&s, 0.0, format);
                                }
                                egui::WidgetText::LayoutJob(j) => {
                                    let mut j = std::sync::Arc::unwrap_or_clone(j);
                                    let base = lj.text.len();
                                    lj.text.push_str(&j.text);
                                    for mut sec in j.sections.drain(..) {
                                        sec.byte_range.start += base;
                                        sec.byte_range.end += base;
                                        lj.sections.push(sec);
                                    }
                                }
                                egui::WidgetText::Galley(_) => {}
                            }
                        }
                    }
                    egui::Label::new(lj)
                };
                // methods
                loop {
                    let (m, _) = self.read_from_repr(LabelAtomsBuilderMethodId::from_repr)?;
                    match m {
                        LabelAtomsBuilderMethodId::Build => {
                            break;
                        }
                        LabelAtomsBuilderMethodId::Selectable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match LabelAtomsBuilderMethodId::Selectable");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.selectable(val);
                        }
                        LabelAtomsBuilderMethodId::Wrap => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match LabelAtomsBuilderMethodId::Wrap");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.wrap_mode(egui::TextWrapMode::Wrap);
                        }
                        LabelAtomsBuilderMethodId::Truncate => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match LabelAtomsBuilderMethodId::Truncate");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.wrap_mode(egui::TextWrapMode::Truncate);
                        }
                        LabelAtomsBuilderMethodId::Extend => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match LabelAtomsBuilderMethodId::Extend");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.wrap_mode(egui::TextWrapMode::Extend);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.apply_widget(w, u, f, None);
            }
            FuncProcId::LabelWidgetText => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::LabelWidgetText");
                // arguments

                let widget_text = {
                    let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                    let u2: &mut Option<&mut egui::Ui> = &mut None;
                    if u2.is_some() {
                        self.interpret_inner(c, u2, &f2, d + 1)?;
                    } else {
                        self.interpret_inner(c, u, &f2, d + 1)?;
                    }

                    std::mem::take(&mut self.r1_widget_text)
                };
                // construct

                #[allow(unused_mut)]
                let mut w = egui::Label::new(widget_text);
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.apply_widget(w, u, f, None);
            }
            FuncProcId::MapMarker => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::MapMarker");
                // arguments
                #[allow(unused_mut)]
                let mut marker_id = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut lat = self.io.read_plain_f64()?;
                #[allow(unused_mut)]
                let mut lon = self.io.read_plain_f64()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut label: Option<String> = None;
                let mut color: Option<egui::Color32> = None;
                let mut radius_px: f32 = 6.0;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(MapMarkerBuilderMethodId::from_repr)?;
                    match m {
                        MapMarkerBuilderMethodId::Build => {
                            break;
                        }
                        MapMarkerBuilderMethodId::Label => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match MapMarkerBuilderMethodId::Label");
                            #[allow(unused_mut)]
                            let mut text = self.io.read_plain_s()?;
                            label = Some(text);
                        }
                        MapMarkerBuilderMethodId::Color => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match MapMarkerBuilderMethodId::Color");
                            #[allow(unused_mut)]
                            let mut col = self.io.read_plain_u32()?;
                            color = Some(color32_from_rgba_u32(col));
                        }
                        MapMarkerBuilderMethodId::Radius => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match MapMarkerBuilderMethodId::Radius");
                            #[allow(unused_mut)]
                            let mut radius = self.io.read_plain_f32()?;
                            radius_px = radius;
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.walkers_pending_markers.push(WalkersMarker {
                    id: marker_id,
                    lat,
                    lon,
                    label,
                    color,
                    radius_px,
                });
            }
            FuncProcId::MapPolyline => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::MapPolyline");
                // arguments
                #[allow(unused_mut)]
                let mut lats = self.io.read_plain_f64h()?;
                #[allow(unused_mut)]
                let mut lons = self.io.read_plain_f64h()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut stroke: egui::Stroke =
                    egui::Stroke::new(2.0, egui::Color32::from_rgb(0x33, 0x88, 0xff));
                let mut closed: bool = false;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(MapPolylineBuilderMethodId::from_repr)?;
                    match m {
                        MapPolylineBuilderMethodId::Build => {
                            break;
                        }
                        MapPolylineBuilderMethodId::Stroke => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match MapPolylineBuilderMethodId::Stroke");
                            #[allow(unused_mut)]
                            let mut col = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            stroke = egui::Stroke::new(width, color32_from_rgba_u32(col));
                        }
                        MapPolylineBuilderMethodId::Closed => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match MapPolylineBuilderMethodId::Closed");
                            #[allow(unused_mut)]
                            let mut closed = self.io.read_plain_b()?;
                            closed = closed;
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.walkers_pending_polylines.push(WalkersPolyline {
                    lats,
                    lons,
                    stroke,
                    closed,
                });
            }
            FuncProcId::MapRaster => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::MapRaster");
                // arguments
                #[allow(unused_mut)]
                let mut raster_id = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut min_lat = self.io.read_plain_f64()?;
                #[allow(unused_mut)]
                let mut min_lon = self.io.read_plain_f64()?;
                #[allow(unused_mut)]
                let mut max_lat = self.io.read_plain_f64()?;
                #[allow(unused_mut)]
                let mut max_lon = self.io.read_plain_f64()?;
                #[allow(unused_mut)]
                let mut width_px = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut height_px = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut content_version = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut pixels = self.io.read_plain_u32h()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut opacity: f32 = 1.0;
                let mut nearest: bool = false;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(MapRasterBuilderMethodId::from_repr)?;
                    match m {
                        MapRasterBuilderMethodId::Build => {
                            break;
                        }
                        MapRasterBuilderMethodId::Opacity => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match MapRasterBuilderMethodId::Opacity");
                            #[allow(unused_mut)]
                            let mut op = self.io.read_plain_f32()?;
                            opacity = op;
                        }
                        MapRasterBuilderMethodId::Nearest => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match MapRasterBuilderMethodId::Nearest");
                            #[allow(unused_mut)]
                            let mut on = self.io.read_plain_b()?;
                            nearest = on;
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.walkers_pending_rasters.push(WalkersRaster {
                    id: raster_id,
                    min_lat,
                    min_lon,
                    max_lat,
                    max_lon,
                    width_px,
                    height_px,
                    content_version,
                    pixels,
                    opacity,
                    nearest,
                });
            }
            FuncProcId::MeasureText => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::MeasureText");
                // arguments
                #[allow(unused_mut)]
                let mut measure_id = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut text = self.io.read_plain_s()?;
                #[allow(unused_mut)]
                let mut font_size = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut monospace = self.io.read_plain_b()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let font_id = if monospace {
                    egui::FontId::monospace(font_size)
                } else {
                    egui::FontId::proportional(font_size)
                };
                // layout_no_wrap needs &mut FontsView (galley cache is mutable);
                // hence fonts_mut rather than fonts.
                let width = c.fonts_mut(|f| {
                    f.layout_no_wrap(text, font_id, egui::Color32::WHITE).rect.width()
                });
                self.r9_f64_push(measure_id, width as f64);
            }
            FuncProcId::MeasureTextSize => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::MeasureTextSize");
                // arguments
                #[allow(unused_mut)]
                let mut width_measure_id = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut height_measure_id = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut text = self.io.read_plain_s()?;
                #[allow(unused_mut)]
                let mut font_size = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut monospace = self.io.read_plain_b()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let font_id = if monospace {
                    egui::FontId::monospace(font_size)
                } else {
                    egui::FontId::proportional(font_size)
                };
                // layout_no_wrap needs &mut FontsView (galley cache is mutable);
                // hence fonts_mut rather than fonts.
                let size = c.fonts_mut(|f| {
                    f.layout_no_wrap(text, font_id, egui::Color32::WHITE).rect.size()
                });
                self.r9_f64_push(width_measure_id, size.x as f64);
                self.r9_f64_push(height_measure_id, size.y as f64);
            }
            FuncProcId::MemoryResetAreas => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::MemoryResetAreas");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                c.memory_mut(|mem| mem.reset_areas());
            }
            FuncProcId::MenuBar => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::MenuBar");
                // arguments
                // construct

                #[allow(unused_mut)]
                let mut w = egui::MenuBar::new();
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    w.ui(u.as_mut().unwrap(), |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::MenuButton => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::MenuButton");
                // arguments

                let atoms = {
                    let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                    let u2: &mut Option<&mut egui::Ui> = &mut None;
                    if u2.is_some() {
                        self.interpret_inner(c, u2, &f2, d + 1)?;
                    } else {
                        self.interpret_inner(c, u, &f2, d + 1)?;
                    }

                    std::mem::take(&mut self.r0_atoms)
                };
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let retr = u.as_mut().unwrap().menu_button(atoms, |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                    if retr.inner.is_none() {
                        self.interpret_outer(c, &mut None)?;
                    }
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::MoveWindowToTop => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::MoveWindowToTop");
                // arguments
                let i = self.read_id()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                c.move_to_top(egui::LayerId::new(egui::Order::Middle, i));
            }
            FuncProcId::NewTable => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::NewTable");
                // arguments
                let i = self.read_id()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut header_height: f32 = 0.0;
                let mut striped_flag: bool = false;
                let mut vscroll_flag: bool = false;
                let mut min_scrolled_height: f32 = 0.0;
                let mut max_scroll_height: f32 = 0.0;
                let mut scroll_to_row: Option<usize> = None;
                let mut auto_shrink_h: bool = true;
                let mut auto_shrink_v: bool = true;
                let mut auto_shrink_set: bool = false;
                let mut apply_widths_epoch: Option<u32> = None;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(NewTableBuilderMethodId::from_repr)?;
                    match m {
                        NewTableBuilderMethodId::Build => {
                            break;
                        }
                        NewTableBuilderMethodId::Striped => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match NewTableBuilderMethodId::Striped");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            striped_flag = val;
                        }
                        NewTableBuilderMethodId::Vscroll => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match NewTableBuilderMethodId::Vscroll");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            vscroll_flag = val;
                        }
                        NewTableBuilderMethodId::MinScrolledHeight => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match NewTableBuilderMethodId::MinScrolledHeight"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            min_scrolled_height = val;
                        }
                        NewTableBuilderMethodId::MaxScrollHeight => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match NewTableBuilderMethodId::MaxScrollHeight"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            max_scroll_height = val;
                        }
                        NewTableBuilderMethodId::ScrollToRow => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match NewTableBuilderMethodId::ScrollToRow");
                            #[allow(unused_mut)]
                            let mut row = self.io.read_plain_u64()?;
                            scroll_to_row = Some(row as usize);
                        }
                        NewTableBuilderMethodId::HeaderHeight => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match NewTableBuilderMethodId::HeaderHeight");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            header_height = val;
                        }
                        NewTableBuilderMethodId::ApplyWidths => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match NewTableBuilderMethodId::ApplyWidths");
                            #[allow(unused_mut)]
                            let mut epoch = self.io.read_plain_u32()?;
                            apply_widths_epoch = Some(epoch);
                        }
                        NewTableBuilderMethodId::AutoShrink => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match NewTableBuilderMethodId::AutoShrink");
                            #[allow(unused_mut)]
                            let mut horiz = self.io.read_plain_b()?;
                            #[allow(unused_mut)]
                            let mut vert = self.io.read_plain_b()?;
                            auto_shrink_h = horiz;
                            auto_shrink_v = vert;
                            auto_shrink_set = true;
                        }
                    }
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let ui = u.as_mut().unwrap();
                    let ctx_cloned = ui.ctx().clone();
                    let auto_shrink_opt = if auto_shrink_set {
                        Some((auto_shrink_h, auto_shrink_v))
                    } else {
                        None
                    };
                    let _ = self.render_new_table(
                        i.value(),
                        &ctx_cloned,
                        ui,
                        header_height,
                        striped_flag,
                        vscroll_flag,
                        min_scrolled_height,
                        max_scroll_height,
                        scroll_to_row,
                        auto_shrink_opt,
                        apply_widths_epoch,
                    );
                } else {
                    self.new_table_columns.clear();
                    self.new_table_row_heights.clear();
                    self.io.skip_deferred_block_map_u32_u32()?;
                    self.io.skip_deferred_block_map_u64_u32()?;
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
            }
            FuncProcId::NewTableColumn => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::NewTableColumn");
                // arguments
                // construct

                #[allow(unused_mut)]
                let mut w = egui_extras::Column::auto();
                // methods
                loop {
                    let (m, _) = self.read_from_repr(NewTableColumnBuilderMethodId::from_repr)?;
                    match m {
                        NewTableColumnBuilderMethodId::Build => {
                            break;
                        }
                        NewTableColumnBuilderMethodId::Auto => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match NewTableColumnBuilderMethodId::Auto");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui_extras::Column::auto();
                        }
                        NewTableColumnBuilderMethodId::Exact => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match NewTableColumnBuilderMethodId::Exact");
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui_extras::Column::exact(width);
                        }
                        NewTableColumnBuilderMethodId::Initial => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match NewTableColumnBuilderMethodId::Initial");
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui_extras::Column::initial(width);
                        }
                        NewTableColumnBuilderMethodId::Remainder => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match NewTableColumnBuilderMethodId::Remainder"
                            );
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui_extras::Column::remainder();
                        }
                        NewTableColumnBuilderMethodId::AtLeast => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match NewTableColumnBuilderMethodId::AtLeast");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.at_least(min_width);
                        }
                        NewTableColumnBuilderMethodId::AtMost => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match NewTableColumnBuilderMethodId::AtMost");
                            #[allow(unused_mut)]
                            let mut max_width = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.at_most(max_width);
                        }
                        NewTableColumnBuilderMethodId::Resizable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match NewTableColumnBuilderMethodId::Resizable"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.resizable(val);
                        }
                        NewTableColumnBuilderMethodId::ClipContents => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match NewTableColumnBuilderMethodId::ClipContents"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.clip(val);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.new_table_columns.push(w);
            }
            FuncProcId::NewTableRowHeight => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::NewTableRowHeight");
                // arguments
                #[allow(unused_mut)]
                let mut height = self.io.read_plain_f32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.new_table_row_heights.push(height);
            }
            FuncProcId::NodeDir => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::NodeDir");
                // arguments
                let i = self.read_id()?;

                let label = {
                    let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                    let u2: &mut Option<&mut egui::Ui> = &mut None;
                    if u2.is_some() {
                        self.interpret_inner(c, u2, &f2, d + 1)?;
                    } else {
                        self.interpret_inner(c, u, &f2, d + 1)?;
                    }

                    std::mem::take(&mut self.r1_widget_text)
                };
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
egui_ltreeview::NodeBuilder::dir(i.value()).label(label);
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.r3_node_cmds.push(NodeCommand::NodeDir(w));
            }
            FuncProcId::NodeDirClose => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::NodeDirClose");
                // arguments
                #[allow(unused_mut)]
                let mut child_count = self.io.read_plain_u32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.r3_node_cmds.push(NodeCommand::NodeDirClose(child_count as usize));
            }
            FuncProcId::NodeLeaf => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::NodeLeaf");
                // arguments
                let i = self.read_id()?;

                let label = {
                    let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                    let u2: &mut Option<&mut egui::Ui> = &mut None;
                    if u2.is_some() {
                        self.interpret_inner(c, u2, &f2, d + 1)?;
                    } else {
                        self.interpret_inner(c, u, &f2, d + 1)?;
                    }

                    std::mem::take(&mut self.r1_widget_text)
                };
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
egui_ltreeview::NodeBuilder::leaf(i.value()).label(label);
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.r3_node_cmds.push(NodeCommand::NodeLeaf(w));
            }
            FuncProcId::PaintAbsoluteOverlay => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintAbsoluteOverlay");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                {
                    let screen = c.viewport_rect();
                    let layer_id = egui::LayerId::new(
                        egui::Order::Foreground,
                        egui::Id::new("imzero-absolute-overlay"),
                    );
                    let painter = egui::Painter::new(c.clone(), layer_id, screen);
                    self.drain_paint_cmds_to_painter(&painter, egui::Pos2::ZERO, None);
                }
            }
            FuncProcId::PaintArrow => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintArrow");
                // arguments
                #[allow(unused_mut)]
                let mut ox = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut oy = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut dx = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut dy = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut col = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut stroke_width = self.io.read_plain_f32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::Arrow {
                    ox,
                    oy,
                    dx,
                    dy,
                    stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)),
                });
            }
            FuncProcId::PaintCanvas => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintCanvas");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut canvas_width = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut canvas_height = self.io.read_plain_f32()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut bg_color: Option<egui::Color32> = None;
                let mut opacity: Option<f32> = None;
                let mut sense_click = false;
                let mut sense_drag = false;
                let mut sense_hover = false;
                let mut capture_zoom = false;
                let mut capture_scroll = false;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(PaintCanvasBuilderMethodId::from_repr)?;
                    match m {
                        PaintCanvasBuilderMethodId::Build => {
                            break;
                        }
                        PaintCanvasBuilderMethodId::Background => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PaintCanvasBuilderMethodId::Background");
                            #[allow(unused_mut)]
                            let mut col = self.io.read_plain_u32()?;
                            bg_color = Some(color32_from_rgba_u32(col));
                        }
                        PaintCanvasBuilderMethodId::Opacity => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PaintCanvasBuilderMethodId::Opacity");
                            #[allow(unused_mut)]
                            let mut op = self.io.read_plain_f32()?;
                            opacity = Some(op);
                        }
                        PaintCanvasBuilderMethodId::Sense => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PaintCanvasBuilderMethodId::Sense");
                            #[allow(unused_mut)]
                            let mut click = self.io.read_plain_b()?;
                            #[allow(unused_mut)]
                            let mut drag = self.io.read_plain_b()?;
                            #[allow(unused_mut)]
                            let mut hover = self.io.read_plain_b()?;
                            sense_click = click;
                            sense_drag = drag;
                            sense_hover = hover;
                        }
                        PaintCanvasBuilderMethodId::CaptureZoom => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PaintCanvasBuilderMethodId::CaptureZoom");
                            capture_zoom = true;
                        }
                        PaintCanvasBuilderMethodId::CaptureScroll => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match PaintCanvasBuilderMethodId::CaptureScroll"
                            );
                            capture_scroll = true;
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let ui = u.as_mut().unwrap();
                    let desired = egui::Vec2::new(canvas_width, canvas_height);
                    let mut sense = egui::Sense::empty();
                    if sense_click {
                        sense = sense.union(egui::Sense::click());
                    }
                    if sense_drag {
                        sense = sense.union(egui::Sense::drag());
                    }
                    if sense_hover {
                        sense = sense.union(egui::Sense::hover());
                    }
                    let (resp, mut painter) = ui.allocate_painter(desired, sense);
                    let origin = resp.rect.min;
                    // Canvas-local hover, in canvas coordinates, NaN when the pointer is
                    // elsewhere. A LOCAL: its only reader is this canvas's own r23 wheel row
                    // below. It used to be written to the r14 register, which was one slot for
                    // the whole frame — every canvas overwrote the previous one's, so what a
                    // reader got depended on render order, and r14 is retired.
                    let (hover_x, hover_y) = match resp.hover_pos() {
                        Some(hp) => (hp.x - origin.x, hp.y - origin.y),
                        None => (f32::NAN, f32::NAN),
                    };
                    // ADR-0149 M1: the per-id pointer row (r24), keyed by canvas id.
                    // interact_pointer_pos first, so an active drag keeps reporting after
                    // the pointer leaves the canvas (edge-crossing pan); hover_pos otherwise.
                    {
                        let pp = resp.interact_pointer_pos().or_else(|| resp.hover_pos());
                        let (ppx, ppy) = match pp {
                            Some(p) => (p.x - origin.x, p.y - origin.y),
                            None => (f32::NAN, f32::NAN),
                        };
                        let m = ui.input(|inp| inp.modifiers);
                        let mods = (m.shift as u8)
                            | ((m.ctrl as u8) << 1)
                            | ((m.alt as u8) << 2)
                            | ((m.command as u8) << 3);
                        self.r24_canvas_pointer_push(i.value(), origin.x, origin.y, ppx, ppy, mods);
                    }
                    // ADR-0140 hover-scoped wheel capture: own the wheel only while the pointer
                    // is over this canvas (egui's own topmost-under-pointer hit-test). Scroll is
                    // consumed (zeroed) so egui-native ScrollAreas and later readers this frame
                    // do not also scroll; zoom needs no global mutation. Delivered per canvas id
                    // with the canvas-local hover, so the Go side anchors zoom without the racy
                    // single-slot global r14 pointer.
                    if (capture_zoom || capture_scroll) && resp.contains_pointer() {
                        let mut wheel_scroll_x = 0.0f32;
                        let mut wheel_scroll_y = 0.0f32;
                        let mut wheel_zoom = 1.0f32;
                        if capture_zoom {
                            wheel_zoom = ui.input(|inp| inp.zoom_delta());
                        }
                        if capture_scroll {
                            let sd = ui.input(|inp| inp.smooth_scroll_delta);
                            wheel_scroll_x = sd.x;
                            wheel_scroll_y = sd.y;
                            ui.input_mut(|inp| inp.smooth_scroll_delta = egui::Vec2::ZERO);
                        }
                        self.r23_canvas_wheel_push(
                            i.value(),
                            wheel_scroll_x,
                            wheel_scroll_y,
                            wheel_zoom,
                            hover_x,
                            hover_y,
                        );
                    }
                    if let Some(op) = opacity {
                        painter.set_opacity(op);
                    }
                    if let Some(bg) = bg_color {
                        painter.rect_filled(resp.rect, 0.0, bg);
                    }
                    self.drain_paint_cmds_to_painter(&painter, origin, Some(ui));
                    let mut resp_flags = ResponseFlags::empty();
                    resp_flags.populate(&resp);
                    self.r7_push(i.value(), resp_flags);
                } else {
                    self.paint_cmds.clear();
                }
            }
            FuncProcId::PaintCircleFilled => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintCircleFilled");
                // arguments
                #[allow(unused_mut)]
                let mut cx = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut cy = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut radius = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut col = self.io.read_plain_u32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::CircleFilled {
                    cx,
                    cy,
                    radius,
                    fill: color32_from_rgba_u32(col),
                });
            }
            FuncProcId::PaintCircleStroke => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintCircleStroke");
                // arguments
                #[allow(unused_mut)]
                let mut cx = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut cy = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut radius = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut col = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut stroke_width = self.io.read_plain_f32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::CircleStroke {
                    cx,
                    cy,
                    radius,
                    stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)),
                });
            }
            FuncProcId::PaintClipPop => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintClipPop");
                // arguments
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::ClipPop);
            }
            FuncProcId::PaintClipPush => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintClipPush");
                // arguments
                #[allow(unused_mut)]
                let mut min_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut min_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut max_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut max_y = self.io.read_plain_f32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::ClipPush {
                    min_x,
                    min_y,
                    max_x,
                    max_y,
                });
            }
            FuncProcId::PaintCubicBezier => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintCubicBezier");
                // arguments
                #[allow(unused_mut)]
                let mut start_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut start_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut cp1x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut cp1y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut cp2x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut cp2y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut end_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut end_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut col = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut stroke_width = self.io.read_plain_f32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::CubicBezier {
                    x0: start_x,
                    y0: start_y,
                    x1: cp1x,
                    y1: cp1y,
                    x2: cp2x,
                    y2: cp2y,
                    x3: end_x,
                    y3: end_y,
                    stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)),
                });
            }
            FuncProcId::PaintDashedLine => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintDashedLine");
                // arguments
                #[allow(unused_mut)]
                let mut from_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut from_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut to_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut to_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut dash_len = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut gap_len = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut col = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut stroke_width = self.io.read_plain_f32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::DashedLine {
                    from_x,
                    from_y,
                    to_x,
                    to_y,
                    dash_len,
                    gap_len,
                    stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)),
                });
            }
            FuncProcId::PaintEllipseFilled => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintEllipseFilled");
                // arguments
                #[allow(unused_mut)]
                let mut cx = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut cy = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut rx = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut ry = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut col = self.io.read_plain_u32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::EllipseFilled {
                    cx,
                    cy,
                    rx,
                    ry,
                    fill: color32_from_rgba_u32(col),
                });
            }
            FuncProcId::PaintEllipseStroke => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintEllipseStroke");
                // arguments
                #[allow(unused_mut)]
                let mut cx = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut cy = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut rx = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut ry = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut col = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut stroke_width = self.io.read_plain_f32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::EllipseStroke {
                    cx,
                    cy,
                    rx,
                    ry,
                    stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)),
                });
            }
            FuncProcId::PaintImage => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintImage");
                // arguments
                #[allow(unused_mut)]
                let mut image_id = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut min_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut min_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut max_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut max_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut width_px = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut height_px = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut content_version = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut pixels = self.io.read_plain_u32h()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut opacity: f32 = 1.0;
                let mut nearest: bool = false;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(PaintImageBuilderMethodId::from_repr)?;
                    match m {
                        PaintImageBuilderMethodId::Build => {
                            break;
                        }
                        PaintImageBuilderMethodId::Opacity => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PaintImageBuilderMethodId::Opacity");
                            #[allow(unused_mut)]
                            let mut op = self.io.read_plain_f32()?;
                            opacity = op;
                        }
                        PaintImageBuilderMethodId::Nearest => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PaintImageBuilderMethodId::Nearest");
                            #[allow(unused_mut)]
                            let mut on = self.io.read_plain_b()?;
                            nearest = on;
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::Image {
                    id: image_id,
                    min_x,
                    min_y,
                    max_x,
                    max_y,
                    width_px,
                    height_px,
                    content_version,
                    pixels,
                    nearest,
                    opacity,
                });
            }
            FuncProcId::PaintLine => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintLine");
                // arguments
                #[allow(unused_mut)]
                let mut from_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut from_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut to_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut to_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut col = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut stroke_width = self.io.read_plain_f32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::Line {
                    from_x,
                    from_y,
                    to_x,
                    to_y,
                    stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)),
                });
            }
            FuncProcId::PaintMarkers => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintMarkers");
                // arguments
                #[allow(unused_mut)]
                let mut xs = self.io.read_plain_f32h()?;
                #[allow(unused_mut)]
                let mut ys = self.io.read_plain_f32h()?;
                #[allow(unused_mut)]
                let mut shape = self.io.read_plain_u8()?;
                #[allow(unused_mut)]
                let mut radius = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut col = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut weight = self.io.read_plain_f32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::Markers {
                    xs,
                    ys,
                    shape,
                    radius,
                    color: color32_from_rgba_u32(col),
                    weight,
                });
            }
            FuncProcId::PaintPolygonFilled => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintPolygonFilled");
                // arguments
                #[allow(unused_mut)]
                let mut xs = self.io.read_plain_f32h()?;
                #[allow(unused_mut)]
                let mut ys = self.io.read_plain_f32h()?;
                #[allow(unused_mut)]
                let mut col = self.io.read_plain_u32()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut concave = false;
                let mut stroke_col: u32 = 0;
                let mut stroke_width: f32 = 0.0;
                // methods
                loop {
                    let (m, _) =
                        self.read_from_repr(PaintPolygonFilledBuilderMethodId::from_repr)?;
                    match m {
                        PaintPolygonFilledBuilderMethodId::Build => {
                            break;
                        }
                        PaintPolygonFilledBuilderMethodId::Concave => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match PaintPolygonFilledBuilderMethodId::Concave"
                            );
                            concave = true;
                        }
                        PaintPolygonFilledBuilderMethodId::Stroke => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match PaintPolygonFilledBuilderMethodId::Stroke"
                            );
                            #[allow(unused_mut)]
                            let mut sc = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut sw = self.io.read_plain_f32()?;
                            stroke_col = sc;
                            stroke_width = sw;
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                {
                    let n = xs.len().min(ys.len());
                    let mut points: Vec<[f32; 2]> = Vec::with_capacity(n);
                    for i in 0..n {
                        points.push([xs[i], ys[i]]);
                    }
                    self.paint_cmds.push(PaintCmd::PolygonFilled {
                        points,
                        fill: color32_from_rgba_u32(col),
                        stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(stroke_col)),
                        concave,
                    });
                }
            }
            FuncProcId::PaintPolyline => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintPolyline");
                // arguments
                #[allow(unused_mut)]
                let mut xs = self.io.read_plain_f32h()?;
                #[allow(unused_mut)]
                let mut ys = self.io.read_plain_f32h()?;
                #[allow(unused_mut)]
                let mut col = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut stroke_width = self.io.read_plain_f32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                {
                    let n = xs.len().min(ys.len());
                    let mut points: Vec<[f32; 2]> = Vec::with_capacity(n);
                    for i in 0..n {
                        points.push([xs[i], ys[i]]);
                    }
                    self.paint_cmds.push(PaintCmd::Polyline {
                        points,
                        stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)),
                    });
                }
            }
            FuncProcId::PaintRectFilled => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintRectFilled");
                // arguments
                #[allow(unused_mut)]
                let mut min_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut min_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut max_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut max_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut rounding = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut col = self.io.read_plain_u32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::RectFilled {
                    min_x,
                    min_y,
                    max_x,
                    max_y,
                    rounding,
                    fill: color32_from_rgba_u32(col),
                });
            }
            FuncProcId::PaintRectStroke => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintRectStroke");
                // arguments
                #[allow(unused_mut)]
                let mut min_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut min_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut max_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut max_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut rounding = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut col = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut stroke_width = self.io.read_plain_f32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::RectStroke {
                    min_x,
                    min_y,
                    max_x,
                    max_y,
                    rounding,
                    stroke: egui::Stroke::new(stroke_width, color32_from_rgba_u32(col)),
                });
            }
            FuncProcId::PaintRectsFilled => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintRectsFilled");
                // arguments
                #[allow(unused_mut)]
                let mut min_xs = self.io.read_plain_f32h()?;
                #[allow(unused_mut)]
                let mut min_ys = self.io.read_plain_f32h()?;
                #[allow(unused_mut)]
                let mut max_xs = self.io.read_plain_f32h()?;
                #[allow(unused_mut)]
                let mut max_ys = self.io.read_plain_f32h()?;
                #[allow(unused_mut)]
                let mut cols = self.io.read_plain_u32h()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::RectsFilled {
                    min_xs,
                    min_ys,
                    max_xs,
                    max_ys,
                    cols,
                });
            }
            FuncProcId::PaintSenseRegion => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintSenseRegion");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut px = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut py = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut sw = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut sh = self.io.read_plain_f32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.paint_cmds.push(PaintCmd::SenseRegion {
                    id: i,
                    px,
                    py,
                    sw,
                    sh,
                });
            }
            FuncProcId::PaintText => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PaintText");
                // arguments
                #[allow(unused_mut)]
                let mut px = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut py = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut anchor_h = self.io.read_plain_u8()?;
                #[allow(unused_mut)]
                let mut anchor_v = self.io.read_plain_u8()?;
                #[allow(unused_mut)]
                let mut text = self.io.read_plain_s()?;
                #[allow(unused_mut)]
                let mut font_size = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut col = self.io.read_plain_u32()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut monospace = false;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(PaintTextBuilderMethodId::from_repr)?;
                    match m {
                        PaintTextBuilderMethodId::Build => {
                            break;
                        }
                        PaintTextBuilderMethodId::Monospace => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PaintTextBuilderMethodId::Monospace");
                            monospace = true;
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.paint_cmds.push(PaintCmd::Text {
                    px,
                    py,
                    anchor_h,
                    anchor_v,
                    text,
                    font_size,
                    color: color32_from_rgba_u32(col),
                    monospace,
                });
            }
            FuncProcId::PanelBottom => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PanelBottom");
                // arguments
                let i = self.read_id()?;
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
egui::Panel::bottom(i);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(PanelBottomBuilderMethodId::from_repr)?;
                    match m {
                        PanelBottomBuilderMethodId::Build => {
                            break;
                        }
                        PanelBottomBuilderMethodId::Resizable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PanelBottomBuilderMethodId::Resizable");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.resizable(val);
                        }
                        PanelBottomBuilderMethodId::DefaultSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PanelBottomBuilderMethodId::DefaultSize");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.default_size(val);
                        }
                        PanelBottomBuilderMethodId::ExactSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PanelBottomBuilderMethodId::ExactSize");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.exact_size(val);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    w.show(u.as_mut().unwrap(), |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::PanelBottomInside => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PanelBottomInside");
                // arguments
                let i = self.read_id()?;
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
egui::Panel::bottom(i);
                // methods
                loop {
                    let (m, _) =
                        self.read_from_repr(PanelBottomInsideBuilderMethodId::from_repr)?;
                    match m {
                        PanelBottomInsideBuilderMethodId::Build => {
                            break;
                        }
                        PanelBottomInsideBuilderMethodId::Resizable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match PanelBottomInsideBuilderMethodId::Resizable"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.resizable(val);
                        }
                        PanelBottomInsideBuilderMethodId::DefaultSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match PanelBottomInsideBuilderMethodId::DefaultSize"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.default_size(val);
                        }
                        PanelBottomInsideBuilderMethodId::ExactSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match PanelBottomInsideBuilderMethodId::ExactSize"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.exact_size(val);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    w.show(u.as_mut().unwrap(), |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::PanelCentral => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PanelCentral");
                // arguments
                // construct

                #[allow(unused_mut)]
                let mut w = egui::CentralPanel::default();
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    w.show(u.as_mut().unwrap(), |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::PanelCentralInside => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PanelCentralInside");
                // arguments
                // construct

                #[allow(unused_mut)]
                let mut w = egui::CentralPanel::default();
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    w.show(u.as_mut().unwrap(), |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::PanelLeft => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PanelLeft");
                // arguments
                let i = self.read_id()?;
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
egui::Panel::left(i);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(PanelLeftBuilderMethodId::from_repr)?;
                    match m {
                        PanelLeftBuilderMethodId::Build => {
                            break;
                        }
                        PanelLeftBuilderMethodId::Resizable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PanelLeftBuilderMethodId::Resizable");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.resizable(val);
                        }
                        PanelLeftBuilderMethodId::DefaultSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PanelLeftBuilderMethodId::DefaultSize");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.default_size(val);
                        }
                        PanelLeftBuilderMethodId::ExactSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PanelLeftBuilderMethodId::ExactSize");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.exact_size(val);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    w.show(u.as_mut().unwrap(), |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::PanelLeftInside => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PanelLeftInside");
                // arguments
                let i = self.read_id()?;
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
egui::Panel::left(i);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(PanelLeftInsideBuilderMethodId::from_repr)?;
                    match m {
                        PanelLeftInsideBuilderMethodId::Build => {
                            break;
                        }
                        PanelLeftInsideBuilderMethodId::Resizable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match PanelLeftInsideBuilderMethodId::Resizable"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.resizable(val);
                        }
                        PanelLeftInsideBuilderMethodId::DefaultSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match PanelLeftInsideBuilderMethodId::DefaultSize"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.default_size(val);
                        }
                        PanelLeftInsideBuilderMethodId::ExactSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match PanelLeftInsideBuilderMethodId::ExactSize"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.exact_size(val);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    w.show(u.as_mut().unwrap(), |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::PanelRight => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PanelRight");
                // arguments
                let i = self.read_id()?;
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
egui::Panel::right(i);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(PanelRightBuilderMethodId::from_repr)?;
                    match m {
                        PanelRightBuilderMethodId::Build => {
                            break;
                        }
                        PanelRightBuilderMethodId::Resizable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PanelRightBuilderMethodId::Resizable");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.resizable(val);
                        }
                        PanelRightBuilderMethodId::DefaultSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PanelRightBuilderMethodId::DefaultSize");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.default_size(val);
                        }
                        PanelRightBuilderMethodId::ExactSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PanelRightBuilderMethodId::ExactSize");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.exact_size(val);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    w.show(u.as_mut().unwrap(), |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::PanelRightInside => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PanelRightInside");
                // arguments
                let i = self.read_id()?;
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
egui::Panel::right(i);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(PanelRightInsideBuilderMethodId::from_repr)?;
                    match m {
                        PanelRightInsideBuilderMethodId::Build => {
                            break;
                        }
                        PanelRightInsideBuilderMethodId::Resizable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match PanelRightInsideBuilderMethodId::Resizable"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.resizable(val);
                        }
                        PanelRightInsideBuilderMethodId::DefaultSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match PanelRightInsideBuilderMethodId::DefaultSize"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.default_size(val);
                        }
                        PanelRightInsideBuilderMethodId::ExactSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match PanelRightInsideBuilderMethodId::ExactSize"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.exact_size(val);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    w.show(u.as_mut().unwrap(), |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::PanelTop => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PanelTop");
                // arguments
                let i = self.read_id()?;
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
egui::Panel::top(i);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(PanelTopBuilderMethodId::from_repr)?;
                    match m {
                        PanelTopBuilderMethodId::Build => {
                            break;
                        }
                        PanelTopBuilderMethodId::Resizable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PanelTopBuilderMethodId::Resizable");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.resizable(val);
                        }
                        PanelTopBuilderMethodId::DefaultSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PanelTopBuilderMethodId::DefaultSize");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.default_size(val);
                        }
                        PanelTopBuilderMethodId::ExactSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match PanelTopBuilderMethodId::ExactSize");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.exact_size(val);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    w.show(u.as_mut().unwrap(), |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::PanelTopInside => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PanelTopInside");
                // arguments
                let i = self.read_id()?;
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
egui::Panel::top(i);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(PanelTopInsideBuilderMethodId::from_repr)?;
                    match m {
                        PanelTopInsideBuilderMethodId::Build => {
                            break;
                        }
                        PanelTopInsideBuilderMethodId::Resizable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match PanelTopInsideBuilderMethodId::Resizable"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.resizable(val);
                        }
                        PanelTopInsideBuilderMethodId::DefaultSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match PanelTopInsideBuilderMethodId::DefaultSize"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.default_size(val);
                        }
                        PanelTopInsideBuilderMethodId::ExactSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match PanelTopInsideBuilderMethodId::ExactSize"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            w = w.exact_size(val);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    w.show(u.as_mut().unwrap(), |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::Passthrough => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Passthrough");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut input = self.io.read_plain_u64()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.r9_u64_push(i.value(), input + 1);
            }
            FuncProcId::PrepareNextFrame => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PrepareNextFrame");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.prepare_next_frame();
            }
            FuncProcId::ProgressBar => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::ProgressBar");
                // arguments
                #[allow(unused_mut)]
                let mut progress = self.io.read_plain_f32()?;
                // construct

                #[allow(unused_mut)]
                let mut w = egui::ProgressBar::new(progress)
                    .fill(imzero2_egui::style::tokens::palette_generated::ACCENT_DEFAULT);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(ProgressBarBuilderMethodId::from_repr)?;
                    match m {
                        ProgressBarBuilderMethodId::Build => {
                            break;
                        }
                        ProgressBarBuilderMethodId::Text => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ProgressBarBuilderMethodId::Text");
                            #[allow(unused_mut)]
                            let mut text = self.io.read_plain_s()?;
                            w = w.text(text);
                        }
                        ProgressBarBuilderMethodId::Animate => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ProgressBarBuilderMethodId::Animate");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.animate(enabled && !self.animation_freeze);
                        }
                        ProgressBarBuilderMethodId::ShowPercentage => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match ProgressBarBuilderMethodId::ShowPercentage"
                            );
                            w = w.show_percentage();
                        }
                        ProgressBarBuilderMethodId::DesiredWidth => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match ProgressBarBuilderMethodId::DesiredWidth"
                            );
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            w = w.desired_width(width);
                        }
                        ProgressBarBuilderMethodId::DesiredHeight => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match ProgressBarBuilderMethodId::DesiredHeight"
                            );
                            #[allow(unused_mut)]
                            let mut height = self.io.read_plain_f32()?;
                            w = w.desired_height(height);
                        }
                        ProgressBarBuilderMethodId::CornerRadius => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match ProgressBarBuilderMethodId::CornerRadius"
                            );
                            #[allow(unused_mut)]
                            let mut radius = self.io.read_plain_u8()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.corner_radius(radius);
                        }
                        ProgressBarBuilderMethodId::Fill => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ProgressBarBuilderMethodId::Fill");

                            let col = {
                                let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                                let u2: &mut Option<&mut egui::Ui> = &mut None;
                                if u2.is_some() {
                                    self.interpret_inner(c, u2, &f2, d + 1)?;
                                } else {
                                    self.interpret_inner(c, u, &f2, d + 1)?;
                                }

                                self.r11_color32
                            };
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.fill(col);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.apply_widget(w, u, f, None);
            }
            FuncProcId::PushId => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::PushId");
                // arguments
                let i = self.read_id()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().push_id(i, |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::RadioButton => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::RadioButton");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut checked = self.io.read_plain_b()?;

                let atoms = {
                    let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                    let u2: &mut Option<&mut egui::Ui> = &mut None;
                    if u2.is_some() {
                        self.interpret_inner(c, u2, &f2, d + 1)?;
                    } else {
                        self.interpret_inner(c, u, &f2, d + 1)?;
                    }

                    std::mem::take(&mut self.r0_atoms)
                };
                // construct

                #[allow(unused_mut)]
                let mut w = egui::RadioButton::new(checked, atoms);
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                let resp =
// generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
self.apply_widget(w,u,f,Some(i));
                if resp.is_some() && resp.unwrap().clicked() {
                    // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                    self.r10_push(i.value(), true);
                }
            }
            FuncProcId::RequestRepaint => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::RequestRepaint");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                c.request_repaint();
            }
            FuncProcId::RequestRepaintAfter => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::RequestRepaintAfter");
                // arguments
                #[allow(unused_mut)]
                let mut dur_secs = self.io.read_plain_f64()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                c.request_repaint_after(std::time::Duration::from_secs_f64(dur_secs));
            }
            FuncProcId::RequestScreenshot => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::RequestScreenshot");
                // arguments
                #[allow(unused_mut)]
                let mut path = self.io.read_plain_s()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                c.send_viewport_cmd(egui::ViewportCommand::Screenshot(egui::UserData::new(path)));
            }
            FuncProcId::RequestScreenshotRect => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::RequestScreenshotRect");
                // arguments
                #[allow(unused_mut)]
                let mut path = self.io.read_plain_s()?;
                #[allow(unused_mut)]
                let mut rect_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut rect_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut rect_w = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut rect_h = self.io.read_plain_f32()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let req = crate::imzero2::interpreter::ScreenshotRequest {
                    path,
                    rect: Some(egui::Rect::from_min_size(
                        egui::pos2(rect_x, rect_y),
                        egui::vec2(rect_w, rect_h),
                    )),
                };
                c.send_viewport_cmd(egui::ViewportCommand::Screenshot(egui::UserData::new(req)));
            }
            FuncProcId::ScalarSize => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::ScalarSize");
                // arguments
                // construct

                #[allow(unused_mut)]
                let mut w = 0.0f32; // methods
                loop {
                    let (m, _) = self.read_from_repr(ScalarSizeBuilderMethodId::from_repr)?;
                    match m {
                        ScalarSizeBuilderMethodId::Build => {
                            break;
                        }
                        ScalarSizeBuilderMethodId::AvailableWidth => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match ScalarSizeBuilderMethodId::AvailableWidth"
                            );
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = if u.is_some() {
                                u.as_mut().unwrap().available_width()
                            } else {
                                0.0
                            };
                        }
                        ScalarSizeBuilderMethodId::AvailableHeight => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match ScalarSizeBuilderMethodId::AvailableHeight"
                            );
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = if u.is_some() {
                                u.as_mut().unwrap().available_height()
                            } else {
                                0.0
                            };
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
            }
            FuncProcId::Scope => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Scope");
                // arguments
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().scope(|ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::ScrollArea => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::ScrollArea");
                // arguments
                // construct

                #[allow(unused_mut)]
                let mut w = egui::ScrollArea::neither();
                // methods
                loop {
                    let (m, _) = self.read_from_repr(ScrollAreaBuilderMethodId::from_repr)?;
                    match m {
                        ScrollAreaBuilderMethodId::Build => {
                            break;
                        }
                        ScrollAreaBuilderMethodId::Hscroll => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ScrollAreaBuilderMethodId::Hscroll");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.hscroll(val);
                        }
                        ScrollAreaBuilderMethodId::Vscroll => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ScrollAreaBuilderMethodId::Vscroll");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.vscroll(val);
                        }
                        ScrollAreaBuilderMethodId::Animated => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ScrollAreaBuilderMethodId::Animated");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.animated(val && !self.animation_freeze);
                        }
                        ScrollAreaBuilderMethodId::AutoShrink => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match ScrollAreaBuilderMethodId::AutoShrink");
                            #[allow(unused_mut)]
                            let mut horiz = self.io.read_plain_b()?;
                            #[allow(unused_mut)]
                            let mut vert = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.auto_shrink([horiz, vert]);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    w.show(u.as_mut().unwrap(), |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::ScrollToCursor => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::ScrollToCursor");
                // arguments
                #[allow(unused_mut)]
                let mut align = self.io.read_plain_u8()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let a = match align {
                        0 => egui::Align::Min,
                        1 => egui::Align::Center,
                        _ => egui::Align::Max,
                    };
                    u.as_mut().unwrap().scroll_to_cursor(Some(a));
                }
            }
            FuncProcId::ScrollingTexture => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::ScrollingTexture");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut width_slots = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut height_slots = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut orientation = self.io.read_plain_u8()?;
                #[allow(unused_mut)]
                let mut filter = self.io.read_plain_u8()?;
                #[allow(unused_mut)]
                let mut head = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut new_count = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut new_columns = self.io.read_plain_u32h()?;
                #[allow(unused_mut)]
                let mut display_width_px = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut display_height_px = self.io.read_plain_f32()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let ui = u.as_mut().unwrap();
                    let resp = self.scrolling_texture.push_and_draw(
                        ui,
                        c,
                        i.value(),
                        width_slots,
                        height_slots,
                        orientation,
                        filter,
                        head,
                        new_count,
                        &new_columns,
                        display_width_px,
                        display_height_px,
                    );
                    if resp.fresh_texture {
                        // The ring texture was (re)created this frame: any columns the
                        // sender pushed while this widget went uninterpreted (hidden dock
                        // tab) or while the idle LRU held its entry are gone. Report so
                        // the Go side can reset its head honestly instead of desyncing
                        // (fetchR22StarvedTextures).
                        self.r22_starved_texture_ids.push(i.value());
                    }
                    self.r9_u64_push(i.value(), resp.hover_rc);
                    self.r10_push(i.value(), resp.clicked);
                }
            }
            FuncProcId::ScrollingTextureRelease => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::ScrollingTextureRelease");
                // arguments
                let i = self.read_id()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.scrolling_texture.release(i.value());
            }
            FuncProcId::SelectableLabel => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::SelectableLabel");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut checked = self.io.read_plain_b()?;
                #[allow(unused_mut)]
                let mut text = self.io.read_plain_s()?;
                // construct

                #[allow(unused_mut)]
                let mut w = egui::Button::selectable(checked, text);
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.apply_widget(w, u, f, Some(i));
            }
            FuncProcId::Separator => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Separator");
                // arguments
                // construct

                #[allow(unused_mut)]
                let mut w = egui::Separator::default();
                // methods
                loop {
                    let (m, _) = self.read_from_repr(SeparatorBuilderMethodId::from_repr)?;
                    match m {
                        SeparatorBuilderMethodId::Build => {
                            break;
                        }
                        SeparatorBuilderMethodId::Horizontal => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SeparatorBuilderMethodId::Horizontal");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.horizontal();
                        }
                        SeparatorBuilderMethodId::Vertical => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SeparatorBuilderMethodId::Vertical");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.vertical();
                        }
                        SeparatorBuilderMethodId::Spacing => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SeparatorBuilderMethodId::Spacing");
                            #[allow(unused_mut)]
                            let mut spacing = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.spacing(spacing);
                        }
                        SeparatorBuilderMethodId::Grow => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SeparatorBuilderMethodId::Grow");
                            #[allow(unused_mut)]
                            let mut extra = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.grow(extra);
                        }
                        SeparatorBuilderMethodId::Shrink => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SeparatorBuilderMethodId::Shrink");
                            #[allow(unused_mut)]
                            let mut shrink = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.shrink(shrink);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.apply_widget(w, u, f, None);
            }
            FuncProcId::SetAnimationFreeze => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::SetAnimationFreeze");
                // arguments
                #[allow(unused_mut)]
                let mut freeze = self.io.read_plain_b()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply

                self.animation_freeze = freeze;
            }
            FuncProcId::SetVideoPipeline => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::SetVideoPipeline");
                // arguments
                #[allow(unused_mut)]
                let mut codec = self.io.read_plain_u32()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.video_pipeline_request = Some(codec as u8);
            }
            FuncProcId::SetWindowCollapsed => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::SetWindowCollapsed");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut collapsed = self.io.read_plain_b()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let collapsing_id = i.with("collapsing");
                let mut state = egui::collapsing_header::CollapsingState::load_with_default_open(
                    c,
                    collapsing_id,
                    true,
                );
                state.set_open(!collapsed);
                state.store(c);
            }
            FuncProcId::ShowDebugTools => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::ShowDebugTools");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    self.render_debug_tools(c, u.as_mut().unwrap());
                }
            }
            FuncProcId::ShowPuffinProfiler => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::ShowPuffinProfiler");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                //#[cfg(feature = "puffin")]
                //puffin_egui::profiler_window(c); // FIXME problem with egui version in puffin_egui crate
            }
            FuncProcId::SliderF64 => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::SliderF64");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut val = self.io.read_plain_f64()?;
                #[allow(unused_mut)]
                let mut range_begin_incl = self.io.read_plain_f64()?;
                #[allow(unused_mut)]
                let mut range_end_incl = self.io.read_plain_f64()?;
                // construct

                #[allow(unused_mut)]
                let mut w = egui::Slider::new(&mut val, range_begin_incl..=range_end_incl);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(SliderF64BuilderMethodId::from_repr)?;
                    match m {
                        SliderF64BuilderMethodId::Build => {
                            break;
                        }
                        SliderF64BuilderMethodId::ShowValue => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::ShowValue");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            w = w.show_value(enabled);
                        }
                        SliderF64BuilderMethodId::Prefix => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::Prefix");
                            #[allow(unused_mut)]
                            let mut prefix = self.io.read_plain_s()?;
                            w = w.prefix(prefix);
                        }
                        SliderF64BuilderMethodId::Suffix => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::Suffix");
                            #[allow(unused_mut)]
                            let mut suffix = self.io.read_plain_s()?;
                            w = w.suffix(suffix);
                        }
                        SliderF64BuilderMethodId::Text => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::Text");
                            #[allow(unused_mut)]
                            let mut text = self.io.read_plain_s()?;
                            w = w.text(text);
                        }
                        SliderF64BuilderMethodId::Vertical => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::Vertical");
                            w = w.vertical();
                        }
                        SliderF64BuilderMethodId::Logarithmic => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::Logarithmic");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            w = w.logarithmic(enabled);
                        }
                        SliderF64BuilderMethodId::SmallestPositive => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match SliderF64BuilderMethodId::SmallestPositive"
                            );
                            #[allow(unused_mut)]
                            let mut smallest_num = self.io.read_plain_f64()?;
                            w = w.smallest_positive(smallest_num);
                        }
                        SliderF64BuilderMethodId::LargestFinite => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::LargestFinite");
                            #[allow(unused_mut)]
                            let mut largest_num = self.io.read_plain_f64()?;
                            w = w.largest_finite(largest_num);
                        }
                        SliderF64BuilderMethodId::SmartAim => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::SmartAim");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            w = w.smart_aim(enabled);
                        }
                        SliderF64BuilderMethodId::DragValueSpeed => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match SliderF64BuilderMethodId::DragValueSpeed"
                            );
                            #[allow(unused_mut)]
                            let mut speed = self.io.read_plain_f64()?;
                            w = w.drag_value_speed(speed);
                        }
                        SliderF64BuilderMethodId::MinDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::MinDecimals");
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.min_decimals(digits as usize);
                        }
                        SliderF64BuilderMethodId::MaxDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::MaxDecimals");
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.max_decimals(digits as usize);
                        }
                        SliderF64BuilderMethodId::FixedDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::FixedDecimals");
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.fixed_decimals(digits as usize);
                        }
                        SliderF64BuilderMethodId::TrailingFill => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::TrailingFill");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            w = w.trailing_fill(enabled);
                        }
                        SliderF64BuilderMethodId::Binary => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::Binary");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.binary(min_width as usize, twos_complement);
                        }
                        SliderF64BuilderMethodId::Octal => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::Octal");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.octal(min_width as usize, twos_complement);
                        }
                        SliderF64BuilderMethodId::Hexadecimal => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::Hexadecimal");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            #[allow(unused_mut)]
                            let mut upper = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.hexadecimal(min_width as usize, twos_complement, upper);
                        }
                        SliderF64BuilderMethodId::Integer => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderF64BuilderMethodId::Integer");
                            w = w.integer();
                        }
                        SliderF64BuilderMethodId::UpdateWhileEditing => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match SliderF64BuilderMethodId::UpdateWhileEditing"
                            );
                            #[allow(unused_mut)]
                            let mut update = self.io.read_plain_b()?;
                            w = w.update_while_editing(update);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                let resp =
// generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
self.apply_widget(w,u,f,Some(i));
                if resp.is_some() && resp.unwrap().changed() {
                    // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                    self.r9_f64_push(i.value(), val);
                }
            }
            FuncProcId::SliderI64 => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::SliderI64");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut val = self.io.read_plain_i64()?;
                #[allow(unused_mut)]
                let mut range_begin_incl = self.io.read_plain_i64()?;
                #[allow(unused_mut)]
                let mut range_end_incl = self.io.read_plain_i64()?;
                // construct

                #[allow(unused_mut)]
                let mut w = egui::Slider::new(&mut val, range_begin_incl..=range_end_incl);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(SliderI64BuilderMethodId::from_repr)?;
                    match m {
                        SliderI64BuilderMethodId::Build => {
                            break;
                        }
                        SliderI64BuilderMethodId::ShowValue => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::ShowValue");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            w = w.show_value(enabled);
                        }
                        SliderI64BuilderMethodId::Prefix => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::Prefix");
                            #[allow(unused_mut)]
                            let mut prefix = self.io.read_plain_s()?;
                            w = w.prefix(prefix);
                        }
                        SliderI64BuilderMethodId::Suffix => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::Suffix");
                            #[allow(unused_mut)]
                            let mut suffix = self.io.read_plain_s()?;
                            w = w.suffix(suffix);
                        }
                        SliderI64BuilderMethodId::Text => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::Text");
                            #[allow(unused_mut)]
                            let mut text = self.io.read_plain_s()?;
                            w = w.text(text);
                        }
                        SliderI64BuilderMethodId::Vertical => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::Vertical");
                            w = w.vertical();
                        }
                        SliderI64BuilderMethodId::Logarithmic => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::Logarithmic");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            w = w.logarithmic(enabled);
                        }
                        SliderI64BuilderMethodId::SmallestPositive => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match SliderI64BuilderMethodId::SmallestPositive"
                            );
                            #[allow(unused_mut)]
                            let mut smallest_num = self.io.read_plain_f64()?;
                            w = w.smallest_positive(smallest_num);
                        }
                        SliderI64BuilderMethodId::LargestFinite => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::LargestFinite");
                            #[allow(unused_mut)]
                            let mut largest_num = self.io.read_plain_f64()?;
                            w = w.largest_finite(largest_num);
                        }
                        SliderI64BuilderMethodId::SmartAim => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::SmartAim");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            w = w.smart_aim(enabled);
                        }
                        SliderI64BuilderMethodId::DragValueSpeed => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match SliderI64BuilderMethodId::DragValueSpeed"
                            );
                            #[allow(unused_mut)]
                            let mut speed = self.io.read_plain_f64()?;
                            w = w.drag_value_speed(speed);
                        }
                        SliderI64BuilderMethodId::MinDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::MinDecimals");
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.min_decimals(digits as usize);
                        }
                        SliderI64BuilderMethodId::MaxDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::MaxDecimals");
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.max_decimals(digits as usize);
                        }
                        SliderI64BuilderMethodId::FixedDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::FixedDecimals");
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.fixed_decimals(digits as usize);
                        }
                        SliderI64BuilderMethodId::TrailingFill => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::TrailingFill");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            w = w.trailing_fill(enabled);
                        }
                        SliderI64BuilderMethodId::Binary => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::Binary");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.binary(min_width as usize, twos_complement);
                        }
                        SliderI64BuilderMethodId::Octal => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::Octal");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.octal(min_width as usize, twos_complement);
                        }
                        SliderI64BuilderMethodId::Hexadecimal => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::Hexadecimal");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            #[allow(unused_mut)]
                            let mut upper = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.hexadecimal(min_width as usize, twos_complement, upper);
                        }
                        SliderI64BuilderMethodId::Integer => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderI64BuilderMethodId::Integer");
                            w = w.integer();
                        }
                        SliderI64BuilderMethodId::UpdateWhileEditing => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match SliderI64BuilderMethodId::UpdateWhileEditing"
                            );
                            #[allow(unused_mut)]
                            let mut update = self.io.read_plain_b()?;
                            w = w.update_while_editing(update);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                let resp =
// generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
self.apply_widget(w,u,f,Some(i));
                if resp.is_some() && resp.unwrap().changed() {
                    // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                    self.r9_i64_push(i.value(), val);
                }
            }
            FuncProcId::SliderU64 => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::SliderU64");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut val = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut range_begin_incl = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut range_end_incl = self.io.read_plain_u64()?;
                // construct

                #[allow(unused_mut)]
                let mut w = egui::Slider::new(&mut val, range_begin_incl..=range_end_incl);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(SliderU64BuilderMethodId::from_repr)?;
                    match m {
                        SliderU64BuilderMethodId::Build => {
                            break;
                        }
                        SliderU64BuilderMethodId::ShowValue => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::ShowValue");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            w = w.show_value(enabled);
                        }
                        SliderU64BuilderMethodId::Prefix => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::Prefix");
                            #[allow(unused_mut)]
                            let mut prefix = self.io.read_plain_s()?;
                            w = w.prefix(prefix);
                        }
                        SliderU64BuilderMethodId::Suffix => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::Suffix");
                            #[allow(unused_mut)]
                            let mut suffix = self.io.read_plain_s()?;
                            w = w.suffix(suffix);
                        }
                        SliderU64BuilderMethodId::Text => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::Text");
                            #[allow(unused_mut)]
                            let mut text = self.io.read_plain_s()?;
                            w = w.text(text);
                        }
                        SliderU64BuilderMethodId::Vertical => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::Vertical");
                            w = w.vertical();
                        }
                        SliderU64BuilderMethodId::Logarithmic => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::Logarithmic");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            w = w.logarithmic(enabled);
                        }
                        SliderU64BuilderMethodId::SmallestPositive => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match SliderU64BuilderMethodId::SmallestPositive"
                            );
                            #[allow(unused_mut)]
                            let mut smallest_num = self.io.read_plain_f64()?;
                            w = w.smallest_positive(smallest_num);
                        }
                        SliderU64BuilderMethodId::LargestFinite => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::LargestFinite");
                            #[allow(unused_mut)]
                            let mut largest_num = self.io.read_plain_f64()?;
                            w = w.largest_finite(largest_num);
                        }
                        SliderU64BuilderMethodId::SmartAim => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::SmartAim");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            w = w.smart_aim(enabled);
                        }
                        SliderU64BuilderMethodId::DragValueSpeed => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match SliderU64BuilderMethodId::DragValueSpeed"
                            );
                            #[allow(unused_mut)]
                            let mut speed = self.io.read_plain_f64()?;
                            w = w.drag_value_speed(speed);
                        }
                        SliderU64BuilderMethodId::MinDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::MinDecimals");
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.min_decimals(digits as usize);
                        }
                        SliderU64BuilderMethodId::MaxDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::MaxDecimals");
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.max_decimals(digits as usize);
                        }
                        SliderU64BuilderMethodId::FixedDecimals => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::FixedDecimals");
                            #[allow(unused_mut)]
                            let mut digits = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.fixed_decimals(digits as usize);
                        }
                        SliderU64BuilderMethodId::TrailingFill => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::TrailingFill");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            w = w.trailing_fill(enabled);
                        }
                        SliderU64BuilderMethodId::Binary => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::Binary");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.binary(min_width as usize, twos_complement);
                        }
                        SliderU64BuilderMethodId::Octal => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::Octal");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.octal(min_width as usize, twos_complement);
                        }
                        SliderU64BuilderMethodId::Hexadecimal => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::Hexadecimal");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut twos_complement = self.io.read_plain_b()?;
                            #[allow(unused_mut)]
                            let mut upper = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.hexadecimal(min_width as usize, twos_complement, upper);
                        }
                        SliderU64BuilderMethodId::Integer => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SliderU64BuilderMethodId::Integer");
                            w = w.integer();
                        }
                        SliderU64BuilderMethodId::UpdateWhileEditing => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match SliderU64BuilderMethodId::UpdateWhileEditing"
                            );
                            #[allow(unused_mut)]
                            let mut update = self.io.read_plain_b()?;
                            w = w.update_while_editing(update);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                let resp =
// generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
self.apply_widget(w,u,f,Some(i));
                if resp.is_some() && resp.unwrap().changed() {
                    // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                    self.r9_u64_push(i.value(), val);
                }
            }
            FuncProcId::SnarlConnection => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::SnarlConnection");
                // arguments
                #[allow(unused_mut)]
                let mut src_node_id = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut src_port = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut dst_node_id = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut dst_port = self.io.read_plain_u32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.snarl_pending_connections.push(SnarlConnectionData {
                    src_node: src_node_id,
                    src_port,
                    dst_node: dst_node_id,
                    dst_port,
                });
            }
            FuncProcId::SnarlEditor => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::SnarlEditor");
                // arguments
                let i = self.read_id()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                // Zero along either axis → fill the container's available space; any
                // positive value → fixed pixel size on that axis.
                let mut ed_width: f32 = 0.0;
                let mut ed_height: f32 = 0.0;
                let mut persist_positions: bool = false;
                let mut wire_style: u8 = 0;
                let mut bg_pattern: u8 = 1;
                let mut min_scale: f32 = 0.0;
                let mut max_scale: f32 = 0.0;
                let mut centering: bool = true;
                let mut crisp_magnified_text: bool = false;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(SnarlEditorBuilderMethodId::from_repr)?;
                    match m {
                        SnarlEditorBuilderMethodId::Build => {
                            break;
                        }
                        SnarlEditorBuilderMethodId::Width => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SnarlEditorBuilderMethodId::Width");
                            #[allow(unused_mut)]
                            let mut wi = self.io.read_plain_f32()?;
                            ed_width = wi;
                        }
                        SnarlEditorBuilderMethodId::Height => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SnarlEditorBuilderMethodId::Height");
                            #[allow(unused_mut)]
                            let mut he = self.io.read_plain_f32()?;
                            ed_height = he;
                        }
                        SnarlEditorBuilderMethodId::PersistPositions => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match SnarlEditorBuilderMethodId::PersistPositions"
                            );
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            persist_positions = vl;
                        }
                        SnarlEditorBuilderMethodId::WireStyle => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SnarlEditorBuilderMethodId::WireStyle");
                            #[allow(unused_mut)]
                            let mut ws = self.io.read_plain_u8()?;
                            wire_style = ws;
                        }
                        SnarlEditorBuilderMethodId::BgPattern => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SnarlEditorBuilderMethodId::BgPattern");
                            #[allow(unused_mut)]
                            let mut bp = self.io.read_plain_u8()?;
                            bg_pattern = bp;
                        }
                        SnarlEditorBuilderMethodId::MinScale => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SnarlEditorBuilderMethodId::MinScale");
                            #[allow(unused_mut)]
                            let mut ms = self.io.read_plain_f32()?;
                            min_scale = ms;
                        }
                        SnarlEditorBuilderMethodId::MaxScale => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SnarlEditorBuilderMethodId::MaxScale");
                            #[allow(unused_mut)]
                            let mut ms = self.io.read_plain_f32()?;
                            max_scale = ms;
                        }
                        SnarlEditorBuilderMethodId::Centering => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SnarlEditorBuilderMethodId::Centering");
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            centering = vl;
                        }
                        SnarlEditorBuilderMethodId::CrispMagnifiedText => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match SnarlEditorBuilderMethodId::CrispMagnifiedText"
                            );
                            #[allow(unused_mut)]
                            let mut vl = self.io.read_plain_b()?;
                            crisp_magnified_text = vl;
                        }
                    }
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let bodies = self.io.read_deferred_block_map_u64()?;
                if u.is_some() {
                    let editor_id = i.value();
                    let ctx_cloned = u.as_ref().unwrap().ctx().clone();
                    let ui = u.as_mut().unwrap();
                    let _ = self.render_snarl_editor(
                        editor_id,
                        &ctx_cloned,
                        ui,
                        bodies,
                        ed_width,
                        ed_height,
                        persist_positions,
                        wire_style,
                        bg_pattern,
                        min_scale,
                        max_scale,
                        centering,
                        crisp_magnified_text,
                    );
                } else {
                    // Drain-on-cull (ADR-0012): clear pending accumulators so a culled
                    // editor doesn't leak its frame's nodes/connections/pins into the
                    // next live editor.
                    self.snarl_pending_nodes.clear();
                    self.snarl_pending_connections.clear();
                    self.snarl_pending_pins.clear();
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
            }
            FuncProcId::SnarlNode => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::SnarlNode");
                // arguments
                #[allow(unused_mut)]
                let mut node_id = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut pos_x = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut pos_y = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut kind = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut title = self.io.read_plain_s()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut num_inputs: u32 = 0;
                let mut num_outputs: u32 = 0;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(SnarlNodeBuilderMethodId::from_repr)?;
                    match m {
                        SnarlNodeBuilderMethodId::Build => {
                            break;
                        }
                        SnarlNodeBuilderMethodId::NumInputs => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SnarlNodeBuilderMethodId::NumInputs");
                            #[allow(unused_mut)]
                            let mut ni = self.io.read_plain_u32()?;
                            num_inputs = ni;
                        }
                        SnarlNodeBuilderMethodId::NumOutputs => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SnarlNodeBuilderMethodId::NumOutputs");
                            #[allow(unused_mut)]
                            let mut no = self.io.read_plain_u32()?;
                            num_outputs = no;
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.snarl_pending_nodes.push(SnarlNodeData {
                    id: node_id,
                    x: pos_x,
                    y: pos_y,
                    kind,
                    title,
                    num_inputs,
                    num_outputs,
                });
            }
            FuncProcId::SnarlPin => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::SnarlPin");
                // arguments
                #[allow(unused_mut)]
                let mut node_id = self.io.read_plain_u64()?;
                #[allow(unused_mut)]
                let mut side = self.io.read_plain_u8()?;
                #[allow(unused_mut)]
                let mut pin_idx = self.io.read_plain_u32()?;
                #[allow(unused_mut)]
                let mut label = self.io.read_plain_s()?;
                #[allow(unused_mut)]
                let mut kind = self.io.read_plain_u32()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.snarl_pending_pins.push(SnarlPinData {
                    node_id,
                    side,
                    pin_idx,
                    label,
                    kind,
                });
            }
            FuncProcId::Spinner => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Spinner");
                // arguments
                // construct

                #[allow(unused_mut)]
                let mut w = egui::Spinner::new();
                // methods
                loop {
                    let (m, _) = self.read_from_repr(SpinnerBuilderMethodId::from_repr)?;
                    match m {
                        SpinnerBuilderMethodId::Build => {
                            break;
                        }
                        SpinnerBuilderMethodId::Size => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match SpinnerBuilderMethodId::Size");
                            #[allow(unused_mut)]
                            let mut size = self.io.read_plain_f32()?;
                            w = w.size(size);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.apply_widget(w, u, f, None);
            }
            FuncProcId::StyledSections => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::StyledSections");
                // arguments
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
{
	self.r24_styled_sections.clear();
	()
};
                // methods
                loop {
                    let (m, _) = self.read_from_repr(StyledSectionsBuilderMethodId::from_repr)?;
                    match m {
                        StyledSectionsBuilderMethodId::Build => {
                            break;
                        }
                        StyledSectionsBuilderMethodId::Section => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match StyledSectionsBuilderMethodId::Section");
                            #[allow(unused_mut)]
                            let mut byte_start = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut byte_stop = self.io.read_plain_u32()?;
                            #[allow(unused_mut)]
                            let mut flags = self.io.read_plain_u32()?;

                            let col = {
                                let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                                let u2: &mut Option<&mut egui::Ui> = &mut None;
                                if u2.is_some() {
                                    self.interpret_inner(c, u2, &f2, d + 1)?;
                                } else {
                                    self.interpret_inner(c, u, &f2, d + 1)?;
                                }

                                self.r11_color32
                            };
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            self.r24_styled_sections.push(text_edit_highlight::StyledSection {
                                byte_start,
                                byte_stop,
                                flags,
                                color: col,
                            });
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
            }
            FuncProcId::Table => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Table");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut row_height = self.io.read_plain_f32()?;
                #[allow(unused_mut)]
                let mut num_rows = self.io.read_plain_u64()?;
                // construct

                #[allow(unused_mut)]
                let mut w = TableConfig::new(row_height, num_rows);
                let mut apply_widths_epoch: Option<u32> = None;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(TableBuilderMethodId::from_repr)?;
                    match m {
                        TableBuilderMethodId::Build => {
                            break;
                        }
                        TableBuilderMethodId::Striped => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TableBuilderMethodId::Striped");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.striped = val;
                        }
                        TableBuilderMethodId::Vscroll => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TableBuilderMethodId::Vscroll");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.vscroll = val;
                        }
                        TableBuilderMethodId::ScrollToRow => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TableBuilderMethodId::ScrollToRow");
                            #[allow(unused_mut)]
                            let mut row = self.io.read_plain_u64()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.scroll_to_row = Some(row as usize);
                        }
                        TableBuilderMethodId::MinScrolledHeight => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TableBuilderMethodId::MinScrolledHeight");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.min_scrolled_height = val;
                        }
                        TableBuilderMethodId::MaxScrollHeight => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TableBuilderMethodId::MaxScrollHeight");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.max_scroll_height = val;
                        }
                        TableBuilderMethodId::ApplyWidths => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TableBuilderMethodId::ApplyWidths");
                            #[allow(unused_mut)]
                            let mut epoch = self.io.read_plain_u32()?;
                            apply_widths_epoch = Some(epoch);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let ui = u.as_mut().unwrap();
                    // The node's widget id becomes an egui Ui id scope for the duration of
                    // the table. egui_extras derives its per-table state — column widths,
                    // resize drags, scroll offset — from ui.id() plus a CONSTANT salt
                    // ("__table_state"), so two tables drawn under one Ui land on one
                    // state id: check_for_id_clash fires every frame, and because
                    // TableState::load discards state whose column count does not match,
                    // two tables of differing widths wipe each other's widths on every
                    // frame and no resize can ever stick. TableBuilder::id_salt would
                    // separate the state but not the double-click-to-autosize probe,
                    // which reads an unsalted ui.id().with("resize_column") — push_id
                    // moves ui.id() itself and so covers both.
                    ui.push_id(i, |ui| {
                        let col_count = self.table_columns.len();
                        let num_rows = w.num_rows;
                        let row_height = w.row_height;

                        let mut builder = egui_extras::TableBuilder::new(ui);
                        for col in self.table_columns.drain(..) {
                            builder = builder.column(col);
                        }
                        if w.striped {
                            builder = builder.striped(true);
                        }
                        if w.vscroll {
                            builder = builder.vscroll(true);
                        }
                        if let Some(row) = w.scroll_to_row {
                            builder = builder.scroll_to_row(row, None);
                        }
                        if w.min_scrolled_height > 0.0 {
                            builder = builder.min_scrolled_height(w.min_scrolled_height);
                        }
                        if w.max_scroll_height > 0.0 {
                            builder = builder.max_scroll_height(w.max_scroll_height);
                        }

                        // Width apply (ADR-0151 §SD4). Must sit before header()/body(): those
                        // are what call TableState::load, and reset() only has an effect if the
                        // state is already gone by then. reset() addresses ui.id().with(id_salt),
                        // the same id load() reads, so the push_id above covers both.
                        if let Some(epoch) = apply_widths_epoch {
                            let gid = i.value();
                            if self.width_epochs.get(&gid).copied() != Some(epoch) {
                                builder.reset();
                                self.width_epochs.insert(gid, epoch);
                            }
                        }

                        let cells: Vec<TableCell> = self.table_cells.drain(..).collect();
                        let header_texts: Vec<String> = self.table_header_texts.drain(..).collect();

                        if header_texts.is_empty() {
                            builder.body(|body| {
                                body.rows(row_height, num_rows, |mut row| {
                                    let row_idx = row.index();
                                    let cell_offset = row_idx * col_count;
                                    for col_idx in 0..col_count {
                                        let cell_idx = cell_offset + col_idx;
                                        row.col(|ui| {
                                            if cell_idx < cells.len() {
                                                cells[cell_idx].render(ui);
                                            }
                                        });
                                    }
                                });
                            });
                        } else {
                            builder
                                .header(row_height, |mut header| {
                                    for ht in header_texts.iter() {
                                        header.col(|ui| {
                                            ui.heading(ht.as_str());
                                        });
                                    }
                                })
                                .body(|body| {
                                    body.rows(row_height, num_rows, |mut row| {
                                        let row_idx = row.index();
                                        let cell_offset = row_idx * col_count;
                                        for col_idx in 0..col_count {
                                            let cell_idx = cell_offset + col_idx;
                                            row.col(|ui| {
                                                if cell_idx < cells.len() {
                                                    cells[cell_idx].render(ui);
                                                }
                                            });
                                        }
                                    });
                                });
                        }
                    });
                } else {
                    self.table_columns.clear();
                    self.table_header_texts.clear();
                    self.table_cells.clear();
                }
            }
            FuncProcId::TableCellRichText => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::TableCellRichText");
                // arguments

                let widget_text = {
                    let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                    let u2: &mut Option<&mut egui::Ui> = &mut None;
                    if u2.is_some() {
                        self.interpret_inner(c, u2, &f2, d + 1)?;
                    } else {
                        self.interpret_inner(c, u, &f2, d + 1)?;
                    }

                    std::mem::take(&mut self.r1_widget_text)
                };
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.table_cells.push(TableCell::RichText(widget_text));
            }
            FuncProcId::TableCellText => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::TableCellText");
                // arguments
                #[allow(unused_mut)]
                let mut text = self.io.read_plain_s()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.table_cells.push(TableCell::Text(text));
            }
            FuncProcId::TableColumn => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::TableColumn");
                // arguments
                // construct

                #[allow(unused_mut)]
                let mut w = egui_extras::Column::auto();
                // methods
                loop {
                    let (m, _) = self.read_from_repr(TableColumnBuilderMethodId::from_repr)?;
                    match m {
                        TableColumnBuilderMethodId::Build => {
                            break;
                        }
                        TableColumnBuilderMethodId::Auto => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TableColumnBuilderMethodId::Auto");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui_extras::Column::auto();
                        }
                        TableColumnBuilderMethodId::Exact => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TableColumnBuilderMethodId::Exact");
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui_extras::Column::exact(width);
                        }
                        TableColumnBuilderMethodId::Initial => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TableColumnBuilderMethodId::Initial");
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui_extras::Column::initial(width);
                        }
                        TableColumnBuilderMethodId::Remainder => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TableColumnBuilderMethodId::Remainder");
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = egui_extras::Column::remainder();
                        }
                        TableColumnBuilderMethodId::AtLeast => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TableColumnBuilderMethodId::AtLeast");
                            #[allow(unused_mut)]
                            let mut min_width = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.at_least(min_width);
                        }
                        TableColumnBuilderMethodId::AtMost => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TableColumnBuilderMethodId::AtMost");
                            #[allow(unused_mut)]
                            let mut max_width = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.at_most(max_width);
                        }
                        TableColumnBuilderMethodId::Resizable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TableColumnBuilderMethodId::Resizable");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.resizable(val);
                        }
                        TableColumnBuilderMethodId::ClipContents => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match TableColumnBuilderMethodId::ClipContents"
                            );
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.clip(val);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.table_columns.push(w);
            }
            FuncProcId::TableHeaderText => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::TableHeaderText");
                // arguments
                #[allow(unused_mut)]
                let mut text = self.io.read_plain_s()?;
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                self.table_header_texts.push(text);
            }
            FuncProcId::TextEdit => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::TextEdit");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut text = self.io.read_plain_s()?;
                #[allow(unused_mut)]
                let mut multiline = self.io.read_plain_b()?;
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
if multiline { egui::TextEdit::multiline(&mut text).id(i) } else { egui::TextEdit::singleline(&mut text).id(i) };
                // methods
                loop {
                    let (m, _) = self.read_from_repr(TextEditBuilderMethodId::from_repr)?;
                    match m {
                        TextEditBuilderMethodId::Build => {
                            break;
                        }
                        TextEditBuilderMethodId::CodeEditor => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::CodeEditor");
                            w = w.code_editor();
                        }
                        TextEditBuilderMethodId::Frame => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::Frame");
                            #[allow(unused_mut)]
                            let mut frame = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            if !frame {
                                w = w.frame(egui::Frame::NONE);
                            }
                        }
                        TextEditBuilderMethodId::HintText => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::HintText");
                            #[allow(unused_mut)]
                            let mut hint = self.io.read_plain_s()?;
                            w = w.hint_text(hint);
                        }
                        TextEditBuilderMethodId::Password => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::Password");
                            #[allow(unused_mut)]
                            let mut password = self.io.read_plain_b()?;
                            w = w.password(password);
                        }
                        TextEditBuilderMethodId::Interactive => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::Interactive");
                            #[allow(unused_mut)]
                            let mut interactive = self.io.read_plain_b()?;
                            w = w.interactive(interactive);
                        }
                        TextEditBuilderMethodId::DesiredWidth => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::DesiredWidth");
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            w = w.desired_width(width);
                        }
                        TextEditBuilderMethodId::DesiredRows => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::DesiredRows");
                            #[allow(unused_mut)]
                            let mut rows = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.desired_rows(rows as usize);
                        }
                        TextEditBuilderMethodId::LockFocus => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::LockFocus");
                            #[allow(unused_mut)]
                            let mut lock = self.io.read_plain_b()?;
                            w = w.lock_focus(lock);
                        }
                        TextEditBuilderMethodId::CursorAtEnd => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::CursorAtEnd");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.cursor_at_end(val);
                        }
                        TextEditBuilderMethodId::ClipText => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::ClipText");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.clip_text(val);
                        }
                        TextEditBuilderMethodId::CharLimit => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::CharLimit");
                            #[allow(unused_mut)]
                            let mut chars = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.char_limit(chars as usize);
                        }
                        TextEditBuilderMethodId::InsertAtCursor => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::InsertAtCursor");
                            #[allow(unused_mut)]
                            let mut snippet = self.io.read_plain_s()?;
                            self.text_edit_pending_insert = Some(snippet);
                        }
                        TextEditBuilderMethodId::HighlightJob => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::HighlightJob");

                            let job = {
                                let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                                let u2: &mut Option<&mut egui::Ui> = &mut None;
                                if u2.is_some() {
                                    self.interpret_inner(c, u2, &f2, d + 1)?;
                                } else {
                                    self.interpret_inner(c, u, &f2, d + 1)?;
                                }

                                std::mem::take(&mut self.r12_code_view_job)
                            };
                            self.text_edit_pending_highlight = Some(job);
                        }
                        TextEditBuilderMethodId::SectionStyled => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::SectionStyled");

                            let styled = {
                                let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                                let u2: &mut Option<&mut egui::Ui> = &mut None;
                                if u2.is_some() {
                                    self.interpret_inner(c, u2, &f2, d + 1)?;
                                } else {
                                    self.interpret_inner(c, u, &f2, d + 1)?;
                                }

                                std::mem::take(&mut self.r24_styled_sections)
                            };
                            self.text_edit_pending_styled = Some(styled);
                        }
                        TextEditBuilderMethodId::NoWrapLayout => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::NoWrapLayout");
                            self.text_edit_pending_no_wrap = true;
                        }
                        TextEditBuilderMethodId::ReportCursor => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::ReportCursor");
                            self.text_edit_pending_report_cursor = true;
                        }
                        TextEditBuilderMethodId::SetCursor => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TextEditBuilderMethodId::SetCursor");
                            #[allow(unused_mut)]
                            let mut sel = self.io.read_plain_u64()?;
                            #[allow(unused_mut)]
                            let mut focus = self.io.read_plain_b()?;
                            self.text_edit_pending_set_cursor = Some((sel, focus));
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                // ADR-0130: builder methods stashed an evaluated CodeViewJob on
                // self.text_edit_pending_highlight, the L3 overlay list on
                // self.text_edit_pending_styled, and the no-wrap switch on
                // self.text_edit_pending_no_wrap. Build the layouter closure as a stack
                // local — closure and widget are same-scope locals, and the widget is
                // consumed by apply below, so the &mut FnMut borrow stays sound.
                //
                // The styled list and the no-wrap switch are taken unconditionally so
                // neither leaks into the next TextEdit; they only reach a layouter when a
                // highlight job is present, which is the only thing that installs one.
                let styled_sections = self.text_edit_pending_styled.take().unwrap_or_default();
                let no_wrap_layout = std::mem::take(&mut self.text_edit_pending_no_wrap);
                let mut hl_layouter = self.text_edit_pending_highlight.take().map(|job| {
                    crate::imzero2::text_edit_highlight::make_layouter(
                        job,
                        styled_sections,
                        no_wrap_layout,
                    )
                });
                if let Some(cl) = hl_layouter.as_mut() {
                    w = w.layouter(cl);
                }
                let resp =
// generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
self.apply_widget(w,u,f,Some(i));
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let mut changed = resp.is_some() && resp.unwrap().changed();
                // A builder method stashed the snippet on self.text_edit_pending_insert.
                // Splice it at the editor's persisted caret (replacing any selection) and
                // force the push: a programmatic edit never sets egui's .changed(). With no
                // stored cursor (editor never focused) we append at end.
                if let Some(ins) = self.text_edit_pending_insert.take() {
                    let ctx_opt = u.as_deref().map(|ui| ui.ctx().clone());
                    let end = text.chars().count();
                    let range = ctx_opt
                        .as_ref()
                        .and_then(|ctx| egui::text_edit::TextEditState::load(ctx, i))
                        .and_then(|st| st.cursor.char_range())
                        // egui 0.35 returns a Range<CharIndex>; splice_text_at_cursor works in
                        // plain usize, so unwrap the newtype at this boundary.
                        .map(|cr| {
                            let r = cr.as_sorted_char_range();
                            r.start.0..r.end.0
                        })
                        .unwrap_or(end..end);
                    let caret = splice_text_at_cursor(&mut text, &ins, range);
                    if let Some(ctx) = ctx_opt {
                        if let Some(mut st) = egui::text_edit::TextEditState::load(&ctx, i) {
                            st.cursor.set_char_range(Some(egui::text::CCursorRange::one(
                                egui::text::CCursor::new(caret),
                            )));
                            st.store(&ctx, i);
                        }
                    }
                    changed = true;
                }
                // setCursor: write the caret / selection the caller asked for.
                //
                // Placed HERE deliberately, between the two blocks it sits among. After the
                // insert, so an explicit position wins over the caret the splice would have
                // left behind when a frame carries both. Before the report, so this frame's
                // report already reflects it and Go is never told a caret it just replaced.
                //
                // Both halves are clamped to the buffer and then sorted, so a stale range
                // from a longer buffer lands at the end rather than panicking the char
                // splice — this arrives from Go one frame after the text it described.
                //
                // What this does NOT do is scroll the caret into view. egui only scrolls when
                // the widget has focus AND either the response changed or its selection
                // changed (text_edit/builder.rs). That selection-changed test compares two
                // reads of the same stored state within one frame, so a range stored from
                // outside reads back equal and is invisible to it. Revealing an off-screen
                // caret needs the galley, which lives on the other side of this seam.
                if let Some((packed, want_focus)) = self.text_edit_pending_set_cursor.take() {
                    if let Some(ctx) = u.as_deref().map(|ui| ui.ctx().clone()) {
                        let end = text.chars().count();
                        let a = ((packed & 0xffff_ffff) as usize).min(end);
                        let b = ((packed >> 32) as usize).min(end);
                        let (lo, hi) = if a <= b { (a, b) } else { (b, a) };
                        let mut st =
                            egui::text_edit::TextEditState::load(&ctx, i).unwrap_or_default();
                        st.cursor.set_char_range(Some(egui::text::CCursorRange::two(
                            egui::text::CCursor::new(lo),
                            egui::text::CCursor::new(hi),
                        )));
                        st.store(&ctx, i);
                        if want_focus {
                            ctx.memory_mut(|m| m.request_focus(i));
                        }
                    }
                }
                // ADR-0130 L3 caret report: push the persisted cursor's sorted CHAR range
                // packed low=start / high=end. Runs BEFORE the text push below, which moves
                // the buffer out. Unconditional while the method is present — a caret move
                // with no text change must still be reported, and change detection around
                // end-of-frame value application never fires. No stored state (the editor
                // was never focused) reports (end, end), matching the insert path's
                // convention above; Go clamps against its own copy of the buffer.
                if std::mem::take(&mut self.text_edit_pending_report_cursor) {
                    const CARET_HALF_MAX: u64 = 0xffff_ffff;
                    let end = text.chars().count() as u64;
                    let (cs, ce) = u
                        .as_deref()
                        .map(|ui| ui.ctx().clone())
                        .and_then(|ctx| egui::text_edit::TextEditState::load(&ctx, i))
                        .and_then(|st| st.cursor.char_range())
                        .map(|cr| {
                            let r = cr.as_sorted_char_range();
                            (r.start.0 as u64, r.end.0 as u64)
                        })
                        .unwrap_or((end, end));
                    self.r9_u64_push(
                        i.value(),
                        cs.min(CARET_HALF_MAX) | (ce.min(CARET_HALF_MAX) << 32),
                    );
                }
                if changed {
                    self.r9_s_push(i.value(), text);
                }
            }
            FuncProcId::TimeRangePicker => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::TimeRangePicker");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut from_initial = self.io.read_plain_s()?;
                #[allow(unused_mut)]
                let mut to_initial = self.io.read_plain_s()?;
                // construct

                #[allow(unused_mut)]
                let mut w = crate::imzero2::time_range_picker::TimeRangePickerRequest::default();
                // methods
                loop {
                    let (m, _) = self.read_from_repr(TimeRangePickerBuilderMethodId::from_repr)?;
                    match m {
                        TimeRangePickerBuilderMethodId::Build => {
                            break;
                        }
                        TimeRangePickerBuilderMethodId::AddPreset => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match TimeRangePickerBuilderMethodId::AddPreset"
                            );
                            #[allow(unused_mut)]
                            let mut label = self.io.read_plain_s()?;
                            #[allow(unused_mut)]
                            let mut from_sql = self.io.read_plain_s()?;
                            #[allow(unused_mut)]
                            let mut to_sql = self.io.read_plain_s()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.presets.push(crate::imzero2::time_range_picker::PresetEntry {
                                label,
                                from_sql,
                                to_sql,
                            });
                        }
                        TimeRangePickerBuilderMethodId::Tz => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TimeRangePickerBuilderMethodId::Tz");
                            #[allow(unused_mut)]
                            let mut zone = self.io.read_plain_s()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.tz = Some(zone);
                        }
                        TimeRangePickerBuilderMethodId::RefreshInterval => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match TimeRangePickerBuilderMethodId::RefreshInterval"
                            );
                            #[allow(unused_mut)]
                            let mut interval_ms = self.io.read_plain_u32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.refresh_interval_ms = Some(interval_ms);
                        }
                        TimeRangePickerBuilderMethodId::EvaluatedBounds => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match TimeRangePickerBuilderMethodId::EvaluatedBounds"
                            );
                            #[allow(unused_mut)]
                            let mut from_ms = self.io.read_plain_i64()?;
                            #[allow(unused_mut)]
                            let mut to_ms = self.io.read_plain_i64()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w.evaluated_from_ms = Some(from_ms);
                            w.evaluated_to_ms = Some(to_ms);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                self.apply_time_range_picker(w, u, f, i, from_initial, to_initial);
            }
            FuncProcId::TintedScope => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::TintedScope");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut col = self.io.read_plain_u32()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut sense_click: bool = false;
                let mut stroke_set: bool = false;
                let mut stroke_width: f32 = 0.0;
                let mut stroke_col_u32: u32 = 0;
                let mut outer_margin: f32 = 0.0;
                let mut inner_margin: f32 = 0.0;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(TintedScopeBuilderMethodId::from_repr)?;
                    match m {
                        TintedScopeBuilderMethodId::Build => {
                            break;
                        }
                        TintedScopeBuilderMethodId::SenseClick => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TintedScopeBuilderMethodId::SenseClick");
                            sense_click = true;
                        }
                        TintedScopeBuilderMethodId::Stroke => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TintedScopeBuilderMethodId::Stroke");
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            #[allow(unused_mut)]
                            let mut stroke_col = self.io.read_plain_u32()?;
                            stroke_width = width;
                            stroke_col_u32 = stroke_col;
                            stroke_set = true;
                        }
                        TintedScopeBuilderMethodId::OuterMargin => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TintedScopeBuilderMethodId::OuterMargin");
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            outer_margin = width;
                        }
                        TintedScopeBuilderMethodId::InnerMargin => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match TintedScopeBuilderMethodId::InnerMargin");
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            inner_margin = width;
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let ui = u.as_mut().unwrap();
                    let cell_rect = ui.max_rect();
                    let outer_rect = cell_rect.shrink(outer_margin);
                    let fill_col = color32_from_rgba_u32(col);
                    if fill_col.a() > 0 {
                        ui.painter().rect_filled(outer_rect, 0.0, fill_col);
                    }
                    if stroke_set && stroke_width > 0.0 {
                        let stroke =
                            egui::Stroke::new(stroke_width, color32_from_rgba_u32(stroke_col_u32));
                        ui.painter().rect_stroke(outer_rect, 0.0, stroke, egui::StrokeKind::Inside);
                    }
                    if outer_margin == 0.0 && inner_margin == 0.0 {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    } else {
                        let inner_rect = outer_rect.shrink(inner_margin);
                        ui.scope_builder(egui::UiBuilder::new().max_rect(inner_rect), |ui| {
                            let _ = self.interpret_outer_logged(c, &mut Some(ui));
                        });
                    }
                    if sense_click {
                        let response = ui.interact(
                            outer_rect,
                            egui::Id::new(i.value()).with("tinted_scope_sense"),
                            egui::Sense::click() | egui::Sense::hover(),
                        );
                        let mut resp_flags = ResponseFlags::empty();
                        resp_flags.populate(&response);
                        self.r7_push(i.value(), resp_flags);
                    }
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::Tree => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Tree");
                // arguments
                let i = self.read_id()?;
                // construct

                #[allow(unused_mut)]
                let mut w = egui_ltreeview::TreeViewSettings::default();
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                if u.is_some() {
                    let ui = u.as_mut().unwrap();
                    // egui_ltreeview 0.7 quirk: TreeViewBuilder::close_dir reads ui.clip_rect()
                    // and calls Rect::clamp on it (builder.rs draw_indent_hint). Rect::clamp
                    // panics in f32::clamp when the rect is negative (min > max) or NaN. A
                    // negative clip can appear inside nested ScrollAreas when the inner
                    // content is pinched horizontally by the outer scrollbar's reservation.
                    // Skip the tree in that case; queued node commands are drained by
                    // prepare_next_frame so the protocol stream stays balanced.
                    let clip = ui.clip_rect();
                    if clip.is_finite() && !clip.is_negative() {
                        let tree = TreeView::new(i);
                        let mut closed_ids = Vec::with_capacity(32); // NOTE: necessary as state.node_states is private
                        let mut state =
                            egui_ltreeview::TreeViewState::load(ui, i).unwrap_or_default();
                        let (response, _actions) =
                            tree.with_settings(w).show_state(ui, &mut state, |tv| {
                                for cmd in self.r3_node_cmds.drain(..) {
                                    match cmd {
                                        NodeCommand::NodeDir(node) => {
                                            let id = *node.id();
                                            if !tv.node(node) {
                                                closed_ids.push(id);
                                            }
                                        }
                                        NodeCommand::NodeLeaf(node) => {
                                            tv.node(node);
                                        }
                                        NodeCommand::NodeDirClose(child_count) => {
                                            tv.close_dir_in(child_count);
                                        }
                                    }
                                }
                            });
                        for s in state.selected().iter() {
                            self.r7_push(*s, ResponseFlags::NODELIKE_SELECTED);
                        }
                        for id in closed_ids.drain(..) {
                            self.r7_push(id, ResponseFlags::BLOCK_SKIPPED);
                        }
                        state.store(ui, i);
                        //for action in actions {
                        //	match action {
                        //		egui_ltreeview::Action::Activate(egui_ltreeview::Activate {selected , modifiers: _}) => {
                        //			for s in selected.iter() {
                        //				self.r7_push(*s, ResponseFlags::NODELIKE_ACTIVATED);
                        //			}
                        //		},
                        //		egui_ltreeview::Action::SetSelected(selected) => {
                        //			for s in selected.iter() {
                        //				self.r7_push(*s, ResponseFlags::NODELIKE_ACTIVATED);
                        //			}
                        //		},
                        //		_ => {
                        //		}
                        //	}
                        //}
                        let _ = response;
                    }
                }
            }
            FuncProcId::UiClipToMaxRect => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::UiClipToMaxRect");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let ui = u.as_mut().unwrap();
                    let r = ui.max_rect();
                    ui.shrink_clip_rect(r);
                }
            }
            FuncProcId::UiDisable => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::UiDisable");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().disable();
                }
            }
            FuncProcId::UiSetHeight => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::UiSetHeight");
                // arguments
                #[allow(unused_mut)]
                let mut height = self.io.read_plain_f32()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().set_height(height);
                }
            }
            FuncProcId::UiSetMaxHeight => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::UiSetMaxHeight");
                // arguments
                #[allow(unused_mut)]
                let mut height = self.io.read_plain_f32()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().set_max_height(height);
                }
            }
            FuncProcId::UiSetMaxWidth => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::UiSetMaxWidth");
                // arguments
                #[allow(unused_mut)]
                let mut width = self.io.read_plain_f32()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().set_max_width(width);
                }
            }
            FuncProcId::UiSetMinHeight => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::UiSetMinHeight");
                // arguments
                #[allow(unused_mut)]
                let mut height = self.io.read_plain_f32()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().set_min_height(height);
                }
            }
            FuncProcId::UiSetMinWidth => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::UiSetMinWidth");
                // arguments
                #[allow(unused_mut)]
                let mut width = self.io.read_plain_f32()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().set_min_width(width);
                }
            }
            FuncProcId::UiSetMinWidthAvailable => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::UiSetMinWidthAvailable");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    let ui = u.as_mut().unwrap();
                    let aw = ui.available_width();
                    ui.set_min_width(aw);
                }
            }
            FuncProcId::UiSetWidth => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::UiSetWidth");
                // arguments
                #[allow(unused_mut)]
                let mut width = self.io.read_plain_f32()?;
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().set_width(width);
                }
            }
            FuncProcId::UiWithLayout => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::UiWithLayout");
                // arguments
                // construct

                #[allow(unused_mut)]
                let mut w = false;
                let mut layout = egui::Layout::default(); // methods
                loop {
                    let (m, _) = self.read_from_repr(UiWithLayoutBuilderMethodId::from_repr)?;
                    match m {
                        UiWithLayoutBuilderMethodId::Build => {
                            break;
                        }
                        UiWithLayoutBuilderMethodId::MainDirLeftToRight => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match UiWithLayoutBuilderMethodId::MainDirLeftToRight"
                            );
                            layout.main_dir = egui::Direction::LeftToRight;
                        }
                        UiWithLayoutBuilderMethodId::MainDirRightToLeft => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match UiWithLayoutBuilderMethodId::MainDirRightToLeft"
                            );
                            layout.main_dir = egui::Direction::RightToLeft;
                        }
                        UiWithLayoutBuilderMethodId::MainDirTopDown => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match UiWithLayoutBuilderMethodId::MainDirTopDown"
                            );
                            layout.main_dir = egui::Direction::TopDown;
                        }
                        UiWithLayoutBuilderMethodId::MainDirBottomUp => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match UiWithLayoutBuilderMethodId::MainDirBottomUp"
                            );
                            layout.main_dir = egui::Direction::BottomUp;
                        }
                        UiWithLayoutBuilderMethodId::MainWrap => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match UiWithLayoutBuilderMethodId::MainWrap");
                            #[allow(unused_mut)]
                            let mut wrap = self.io.read_plain_b()?;
                            layout.main_wrap = wrap;
                        }
                        UiWithLayoutBuilderMethodId::MainJustify => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match UiWithLayoutBuilderMethodId::MainJustify"
                            );
                            #[allow(unused_mut)]
                            let mut justify = self.io.read_plain_b()?;
                            layout.main_justify = justify;
                        }
                        UiWithLayoutBuilderMethodId::CrossAlignMin => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match UiWithLayoutBuilderMethodId::CrossAlignMin"
                            );
                            layout.cross_align = egui::Align::Min;
                        }
                        UiWithLayoutBuilderMethodId::CrossAlignCenter => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match UiWithLayoutBuilderMethodId::CrossAlignCenter"
                            );
                            layout.cross_align = egui::Align::Center;
                        }
                        UiWithLayoutBuilderMethodId::CrossAlignMax => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match UiWithLayoutBuilderMethodId::CrossAlignMax"
                            );
                            layout.cross_align = egui::Align::Max;
                        }
                        UiWithLayoutBuilderMethodId::CrossJustify => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match UiWithLayoutBuilderMethodId::CrossJustify"
                            );
                            #[allow(unused_mut)]
                            let mut justify = self.io.read_plain_b()?;
                            layout.cross_justify = justify;
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().with_layout(layout, |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::VectorSize => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::VectorSize");
                // arguments
                // construct

                #[allow(unused_mut)]
                let mut w = egui::emath::Vec2::new(0.0f32, 0.0f32);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(VectorSizeBuilderMethodId::from_repr)?;
                    match m {
                        VectorSizeBuilderMethodId::Build => {
                            break;
                        }
                        VectorSizeBuilderMethodId::AvailableSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match VectorSizeBuilderMethodId::AvailableSize"
                            );
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = if u.is_some() {
                                u.as_mut().unwrap().available_size()
                            } else {
                                egui::emath::Vec2::new(0.0f32, 0.0f32)
                            };
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
            }
            FuncProcId::Vertical => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Vertical");
                // arguments
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().vertical(|ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::VerticalCentered => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::VerticalCentered");
                // arguments
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().vertical_centered(|ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::VerticalCenteredJustified => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::VerticalCenteredJustified");
                // arguments
                // construct
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    u.as_mut().unwrap().vertical_centered_justified(|ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    });
                } else {
                    self.interpret_outer(c, &mut None)?;
                }
            }
            FuncProcId::WalkersMap => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::WalkersMap");
                // arguments
                let i = self.read_id()?;
                #[allow(unused_mut)]
                let mut init_lat = self.io.read_plain_f64()?;
                #[allow(unused_mut)]
                let mut init_lon = self.io.read_plain_f64()?;
                #[allow(unused_mut)]
                let mut no_tiles = self.io.read_plain_b()?;
                // construct

                #[allow(unused_mut)]
                let mut w = 0u8;
                let mut width: f32 = 600.0;
                let mut height: f32 = 400.0;
                let mut fill_available: bool = false;
                let mut override_zoom: Option<f64> = None;
                let mut override_center: Option<(f64, f64)> = None;
                let mut zoom_gesture: bool = true;
                let mut panning: bool = true;
                let mut tile_url_template: String = String::new();
                let mut tile_attribution_text: String = String::new();
                let mut tile_attribution_url: String = String::new();
                let mut tile_max_zoom: u8 = 0;
                let mut tile_size: u32 = 0;
                let mut tile_ca_file: String = String::new();
                let mut tile_insecure_tls: bool = false;
                // methods
                loop {
                    let (m, _) = self.read_from_repr(WalkersMapBuilderMethodId::from_repr)?;
                    match m {
                        WalkersMapBuilderMethodId::Build => {
                            break;
                        }
                        WalkersMapBuilderMethodId::Width => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WalkersMapBuilderMethodId::Width");
                            #[allow(unused_mut)]
                            let mut wi = self.io.read_plain_f32()?;
                            width = wi;
                        }
                        WalkersMapBuilderMethodId::Height => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WalkersMapBuilderMethodId::Height");
                            #[allow(unused_mut)]
                            let mut he = self.io.read_plain_f32()?;
                            height = he;
                        }
                        WalkersMapBuilderMethodId::FillAvailable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match WalkersMapBuilderMethodId::FillAvailable"
                            );
                            #[allow(unused_mut)]
                            let mut on = self.io.read_plain_b()?;
                            fill_available = on;
                        }
                        WalkersMapBuilderMethodId::SetZoom => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WalkersMapBuilderMethodId::SetZoom");
                            #[allow(unused_mut)]
                            let mut zoom = self.io.read_plain_f64()?;
                            override_zoom = Some(zoom);
                        }
                        WalkersMapBuilderMethodId::CenterAt => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WalkersMapBuilderMethodId::CenterAt");
                            #[allow(unused_mut)]
                            let mut lat = self.io.read_plain_f64()?;
                            #[allow(unused_mut)]
                            let mut lon = self.io.read_plain_f64()?;
                            override_center = Some((lat, lon));
                        }
                        WalkersMapBuilderMethodId::ZoomGesture => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WalkersMapBuilderMethodId::ZoomGesture");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            zoom_gesture = enabled;
                        }
                        WalkersMapBuilderMethodId::Panning => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WalkersMapBuilderMethodId::Panning");
                            #[allow(unused_mut)]
                            let mut enabled = self.io.read_plain_b()?;
                            panning = enabled;
                        }
                        WalkersMapBuilderMethodId::TileUrl => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WalkersMapBuilderMethodId::TileUrl");
                            #[allow(unused_mut)]
                            let mut url = self.io.read_plain_s()?;
                            tile_url_template = url;
                        }
                        WalkersMapBuilderMethodId::TileAttribution => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match WalkersMapBuilderMethodId::TileAttribution"
                            );
                            #[allow(unused_mut)]
                            let mut text = self.io.read_plain_s()?;
                            tile_attribution_text = text;
                        }
                        WalkersMapBuilderMethodId::TileAttributionUrl => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match WalkersMapBuilderMethodId::TileAttributionUrl"
                            );
                            #[allow(unused_mut)]
                            let mut url = self.io.read_plain_s()?;
                            tile_attribution_url = url;
                        }
                        WalkersMapBuilderMethodId::TileMaxZoom => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WalkersMapBuilderMethodId::TileMaxZoom");
                            #[allow(unused_mut)]
                            let mut zoom = self.io.read_plain_u8()?;
                            tile_max_zoom = zoom;
                        }
                        WalkersMapBuilderMethodId::TileSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WalkersMapBuilderMethodId::TileSize");
                            #[allow(unused_mut)]
                            let mut size = self.io.read_plain_u32()?;
                            tile_size = size;
                        }
                        WalkersMapBuilderMethodId::TileCaFile => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WalkersMapBuilderMethodId::TileCaFile");
                            #[allow(unused_mut)]
                            let mut path = self.io.read_plain_s()?;
                            tile_ca_file = path;
                        }
                        WalkersMapBuilderMethodId::TileInsecureTls => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!(
                                "match WalkersMapBuilderMethodId::TileInsecureTls"
                            );
                            #[allow(unused_mut)]
                            let mut on = self.io.read_plain_b()?;
                            tile_insecure_tls = on;
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                self.render_walkers_map(
                    u,
                    f,
                    i,
                    init_lat,
                    init_lon,
                    no_tiles,
                    width,
                    height,
                    fill_available,
                    override_zoom,
                    override_center,
                    zoom_gesture,
                    panning,
                    tile_url_template,
                    tile_attribution_text,
                    tile_attribution_url,
                    tile_max_zoom,
                    tile_size,
                    tile_ca_file,
                    tile_insecure_tls,
                );
            }
            FuncProcId::WarnIfDebugBuild => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::WarnIfDebugBuild");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    egui::warn_if_debug_build(u.as_mut().unwrap());
                }
            }
            FuncProcId::WidgetText => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::WidgetText");
                // arguments
                // construct
                // methods
                loop {
                    let (m, _) = self.read_from_repr(WidgetTextBuilderMethodId::from_repr)?;
                    match m {
                        WidgetTextBuilderMethodId::Build => {
                            break;
                        }
                        WidgetTextBuilderMethodId::Text => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WidgetTextBuilderMethodId::Text");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_s()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            self.r1_widget_text = egui::WidgetText::Text(val);
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
            }
            FuncProcId::WidgetsGlobalThemePreferenceButtons => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::WidgetsGlobalThemePreferenceButtons");
                // arguments
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                if u.is_some() {
                    egui::widgets::global_theme_preference_buttons(u.as_mut().unwrap());
                }
            }
            FuncProcId::Window => {
                #[cfg(feature = "puffin")]
                puffin::profile_scope!("match FuncProcId::Window");
                // arguments
                let i = self.read_id()?;

                let label = {
                    let (f2, _) = self.read_from_repr(FuncProcId::from_repr)?;
                    let u2: &mut Option<&mut egui::Ui> = &mut None;
                    if u2.is_some() {
                        self.interpret_inner(c, u2, &f2, d + 1)?;
                    } else {
                        self.interpret_inner(c, u, &f2, d + 1)?;
                    }

                    std::mem::take(&mut self.r1_widget_text)
                };
                // construct

                #[allow(unused_mut)]
let mut w = // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
egui::Window::new(label).id(i);
                // methods
                loop {
                    let (m, _) = self.read_from_repr(WindowBuilderMethodId::from_repr)?;
                    match m {
                        WindowBuilderMethodId::Build => {
                            break;
                        }
                        WindowBuilderMethodId::DefaultOpen => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WindowBuilderMethodId::DefaultOpen");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.default_open(val);
                        }
                        WindowBuilderMethodId::Enabled => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WindowBuilderMethodId::Enabled");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.enabled(val);
                        }
                        WindowBuilderMethodId::Interactable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WindowBuilderMethodId::Interactable");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.interactable(val);
                        }
                        WindowBuilderMethodId::Movable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WindowBuilderMethodId::Movable");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.movable(val);
                        }
                        WindowBuilderMethodId::Resizable => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WindowBuilderMethodId::Resizable");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.resizable(val);
                        }
                        WindowBuilderMethodId::Collapsible => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WindowBuilderMethodId::Collapsible");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.collapsible(val);
                        }
                        WindowBuilderMethodId::TitleBar => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WindowBuilderMethodId::TitleBar");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            w = w.title_bar(val);
                        }
                        WindowBuilderMethodId::DefaultWidth => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WindowBuilderMethodId::DefaultWidth");
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.default_width(width);
                        }
                        WindowBuilderMethodId::DefaultHeight => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WindowBuilderMethodId::DefaultHeight");
                            #[allow(unused_mut)]
                            let mut height = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.default_height(height);
                        }
                        WindowBuilderMethodId::DefaultSize => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WindowBuilderMethodId::DefaultSize");
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            #[allow(unused_mut)]
                            let mut height = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.default_size(egui::vec2(width, height));
                        }
                        WindowBuilderMethodId::DefaultPos => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WindowBuilderMethodId::DefaultPos");
                            #[allow(unused_mut)]
                            let mut pos_x = self.io.read_plain_f32()?;
                            #[allow(unused_mut)]
                            let mut pos_y = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.default_pos(egui::pos2(pos_x, pos_y));
                        }
                        WindowBuilderMethodId::MinWidth => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WindowBuilderMethodId::MinWidth");
                            #[allow(unused_mut)]
                            let mut width = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.min_width(width);
                        }
                        WindowBuilderMethodId::MinHeight => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WindowBuilderMethodId::MinHeight");
                            #[allow(unused_mut)]
                            let mut height = self.io.read_plain_f32()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            w = w.min_height(height);
                        }
                        WindowBuilderMethodId::AlwaysOnTop => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WindowBuilderMethodId::AlwaysOnTop");
                            #[allow(unused_mut)]
                            let mut val = self.io.read_plain_b()?;
                            // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)
                            if val {
                                w = w.order(egui::Order::Foreground);
                            }
                        }
                        WindowBuilderMethodId::OpenBound => {
                            #[cfg(feature = "puffin")]
                            puffin::profile_scope!("match WindowBuilderMethodId::OpenBound");
                            #[allow(unused_mut)]
                            let mut binding_id = self.io.read_plain_u64()?;
                            self.scratch_open_binding_id = binding_id;
                        }
                    }
                }
                if d == 0 {
                    self.end_consume_message()?;
                }
                // apply
                // generating location: egui2_definition_templating.go:67 github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition.rustClientCode(...)

                let open_binding_id = std::mem::take(&mut self.scratch_open_binding_id);
                // window_open always defaults to true: Go re-emitting this
                // opcode IS the "I want to be open" signal. egui itself
                // doesn't persist window visibility across frames (only
                // position/size/collapsed are stored in egui::Memory keyed
                // by .id()), so reseeding to true every apply matches the
                // caller's intent. If the user clicks the title-bar X
                // inside the show() body, egui mutates window_open to false
                // via the &mut bool; the post-show transition check pushes
                // that change to r10 so Go's *openFlag flips to false and
                // the next frame skips this opcode entirely. Without the
                // cache reseed, the X-then-toggle reopen path was broken:
                // the stale "false" persisted and w.open(&mut false)
                // silently returned None on every subsequent emit.
                let mut window_open: bool = true;
                let was_open = window_open;
                let retr = if open_binding_id != 0 {
                    w.open(&mut window_open).show(c, |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    })
                } else {
                    w.show(c, |ui| {
                        let _ = self.interpret_outer_logged(c, &mut Some(ui));
                    })
                };
                if open_binding_id != 0 && was_open != window_open {
                    self.r10_push(open_binding_id, window_open);
                }
                let mut resp2 = ResponseFlags::empty();
                if retr.is_none() {
                    // closed (egui::Window::open(false), or the user
                    // clicked the title-bar X on this frame)
                    resp2.insert(ResponseFlags::BLOCK_SKIPPED);
                    self.interpret_outer(c, &mut None)?;
                } else {
                    let inner = retr.unwrap();
                    resp2.populate(&inner.response);
                    // WINDOW_TOPMOST: this window's Area is the top layer of
                    // egui's Middle order — the shell notion of "the active
                    // window" (egui raises a window's layer on any press
                    // inside it). Computed here rather than in populate()
                    // because it is a fact about the window's LAYER, not its
                    // response. Tooltips and combo popups live in Foreground
                    // and cannot take it; another egui::Window can — including
                    // a collapsed one, whose title bar still holds the layer,
                    // which is why this sits above the collapsed check.
                    let topmost = c.memory(|m| {
                        m.areas().top_layer_id(egui::Order::Middle) == Some(inner.response.layer_id)
                    });
                    resp2.set(ResponseFlags::WINDOW_TOPMOST, topmost);
                    if inner.inner.is_none() {
                        // collapsed
                        resp2.insert(ResponseFlags::BLOCK_SKIPPED);
                        self.interpret_outer(c, &mut None)?;
                    } else {
                        // open
                    }
                }
                self.r7_push(i.value(), resp2);
            }

            _ => {
                tracing::warn!("received unhandled procedure {:?}", f);
                if d == 0 {
                    self.end_consume_message()?;
                }
            }
        }

        /*===================== //IMZERO2_INCLUDE_FFFI_DISPATCH_OUT =======================*/
        Ok(r)
    }
    //pub fn build_panel_top_bottom<'b>(&mut self, func_proc_id: FuncProcId) -> egui::panel::TopBottomPanel {
    //    let (id,_) = self.read_low_entropy_id().expect("unable to read");
    //    let mut w;
    //    if func_proc_id == FuncProcId::BeginPanelTop {
    //        w = egui::TopBottomPanel::top(id);
    //    } else {
    //        w = egui::TopBottomPanel::bottom(id);
    //    }
    //    loop {
    //        let m = self.read_from_repr(TopBottomBuilderMethodId::from_repr).expect("unable to read builder method id");
    //        match m {
    //            TopBottomBuilderMethodId::Build => {
    //                break;
    //            },
    //            TopBottomBuilderMethodId::Resizeable => {
    //                let b = self.io.read_bool().expect("argument");
    //                w = w.resizable(b);
    //            }
    //            TopBottomBuilderMethodId::DefaultHeight => {
    //                let h = self.io.read_f32().expect("argument");
    //                w = w.default_height(h);
    //            }
    //            TopBottomBuilderMethodId::ExactHeight => {
    //                let h = self.io.read_f32().expect("argument");
    //                w = w.exact_height(h);
    //            }
    //        }
    //    }
    //    return w;
    //}
    //pub fn build_panel_side<'b>(&mut self, func_proc_id: FuncProcId) -> egui::panel::SidePanel {
    //    let (id,_) = self.read_low_entropy_id().expect("unable to read");
    //    let mut w;
    //    if func_proc_id == FuncProcId::BeginPanelLeft {
    //        w = egui::SidePanel::left(id);
    //    } else {
    //        w = egui::SidePanel::right(id);
    //    }
    //    loop {
    //        let m = self.read_from_repr(SidePanelBuilderMethodId::from_repr).expect("unable to read builder method id");
    //        match m {
    //            SidePanelBuilderMethodId::Build => {
    //                break;
    //            },
    //            SidePanelBuilderMethodId::Resizeable => {
    //                let b = self.io.read_bool().expect("argument");
    //                w = w.resizable(b);
    //            }
    //            SidePanelBuilderMethodId::DefaultWidth => {
    //                let h = self.io.read_f32().expect("argument");
    //                w = w.default_width(h);
    //            }
    //            SidePanelBuilderMethodId::ExactWidth => {
    //                let h = self.io.read_f32().expect("argument");
    //                w = w.exact_width(h);
    //            }
    //        }
    //    }
    //    return w;
    //}
    pub fn r7_push(&mut self, i: u64, r: ResponseFlags) {
        self.r7_ids.push(i);
        self.r7_responses.push(r);
    }
    // ADR-0140 canvas wheel capture push — one row per canvas that owned the
    // wheel this frame (see paintCanvas apply). Drained by FetchR23CanvasWheel.
    pub fn r23_canvas_wheel_push(&mut self, i: u64, sx: f32, sy: f32, zoom: f32, hx: f32, hy: f32) {
        self.r23_canvas_wheel_ids.push(i);
        self.r23_canvas_wheel_scroll_x.push(sx);
        self.r23_canvas_wheel_scroll_y.push(sy);
        self.r23_canvas_wheel_zoom.push(zoom);
        self.r23_canvas_wheel_hover_x.push(hx);
        self.r23_canvas_wheel_hover_y.push(hy);
    }
    pub fn r24_canvas_pointer_push(
        &mut self,
        i: u64,
        ox: f32,
        oy: f32,
        px: f32,
        py: f32,
        mods: u8,
    ) {
        self.r24_canvas_pointer_ids.push(i);
        self.r24_canvas_pointer_origin_x.push(ox);
        self.r24_canvas_pointer_origin_y.push(oy);
        self.r24_canvas_pointer_pos_x.push(px);
        self.r24_canvas_pointer_pos_y.push(py);
        self.r24_canvas_pointer_mods.push(mods);
    }
    pub fn r9_u64_push(&mut self, i: u64, r: u64) {
        self.r9_u64_ids.push(i);
        self.r9_u64_values.push(r);
    }
    pub fn r9_f64_push(&mut self, i: u64, r: f64) {
        self.r9_f64_ids.push(i);
        self.r9_f64_values.push(r);
    }
    pub fn r9_i64_push(&mut self, i: u64, r: i64) {
        self.r9_i64_ids.push(i);
        self.r9_i64_values.push(r);
    }
    pub fn r9_s_push(&mut self, i: u64, r: String) {
        self.r9_s_ids.push(i);
        self.r9_s_values.push(r);
    }
    pub fn r10_push(&mut self, i: u64, r: bool) {
        if r {
            self.r10_true_ids.push(i);
        } else {
            self.r10_false_ids.push(i);
        }
    }
    // render_walkers_map — apply-time entry point for the `walkersMap` opcode.
    // Drains all pending overlay Vecs, creates/reuses per-id WalkersState
    // (HttpTiles + MapMemory), renders the Map widget with OverlayPlugin,
    // captures camera+pointer state into `walkers_cameras` for the
    // fetcher, and pushes response flags into r7_* for fetchR7.
    #[allow(clippy::too_many_arguments)]
    pub fn render_walkers_map(
        &mut self,
        u: &mut Option<&mut egui::Ui>,
        f: &FuncProcId,
        id: egui::Id,
        init_lat: f64,
        init_lon: f64,
        no_tiles: bool,
        width: f32,
        height: f32,
        fill_available: bool,
        override_zoom: Option<f64>,
        override_center: Option<(f64, f64)>,
        zoom_gesture: bool,
        panning: bool,
        tile_url_template: String,
        tile_attribution: String,
        tile_attribution_url: String,
        tile_max_zoom: u8,
        tile_size: u32,
        tile_ca_file: String,
        tile_insecure_tls: bool,
    ) {
        let _ = f;
        if u.is_none() {
            // Late-culled: drop pending overlays so they don't leak to next frame.
            self.walkers_pending_markers.clear();
            self.walkers_pending_polylines.clear();
            self.walkers_pending_h3_choropleth.clear();
            self.walkers_pending_h3_regions.clear();
            self.walkers_pending_rasters.clear();
            //tracing::debug!("late culled walkers map {:?}", f);
            return;
        }

        let map_id = id.value();
        let ctx_clone = u.as_ref().unwrap().ctx().clone();

        // Ensure per-widget state exists — construct with the supplied tile
        // config on first sight. If the tile config signature changes
        // on a later frame (Go switched tile servers) we rebuild the tile
        // source in place without touching MapMemory or the outline cache.
        let tls = TileTlsConfig {
            ca_file: tile_ca_file,
            insecure: tile_insecure_tls,
        };
        let wanted_sig = tile_config_signature(
            &tile_url_template,
            &tile_attribution,
            &tile_attribution_url,
            tile_size,
            tile_max_zoom,
            no_tiles,
            &tls,
        );
        if !self.walkers_states.contains_key(&map_id) {
            let st = new_walkers_state(
                &ctx_clone,
                &tile_url_template,
                &tile_attribution,
                &tile_attribution_url,
                tile_size,
                tile_max_zoom,
                no_tiles,
                &tls,
            );
            self.walkers_states.insert(map_id, st);
        } else {
            let st = self.walkers_states.get_mut(&map_id).expect("present");
            if st.tile_signature != wanted_sig {
                st.tiles = build_walkers_tiles(
                    &ctx_clone,
                    &tile_url_template,
                    &tile_attribution,
                    &tile_attribution_url,
                    tile_size,
                    tile_max_zoom,
                    no_tiles,
                    &tls,
                );
                st.tile_signature = wanted_sig;
                tracing::info!(
                    map_id,
                    url = %tile_url_template,
                    "walkers tile config changed — rebuilt the tile source"
                );
            }
        }

        // Drain pending overlays from the interpreter's registers.
        let markers = std::mem::take(&mut self.walkers_pending_markers);
        let polylines = std::mem::take(&mut self.walkers_pending_polylines);
        let h3_choropleth_raw = std::mem::take(&mut self.walkers_pending_h3_choropleth);
        let h3_regions_raw = std::mem::take(&mut self.walkers_pending_h3_regions);

        // Pre-project choropleth cell boundaries (stateless — no cache lookup).
        let h3_choropleth_renderables: Vec<H3ChoroplethRenderable> =
            h3_choropleth_raw.into_iter().map(prerender_choropleth).collect();

        // mapRaster (ADR-0096): drain pending geo rasters, upload each into the
        // dedicated raster texture cache (content-version gated; tour-captured
        // via the shared texture_cache), and keep the texture id + corner
        // Positions for the plugin to project. Runs before the walkers_states
        // borrow below — walkers_raster_cache is a sibling field of self.
        let rasters_raw = std::mem::take(&mut self.walkers_pending_rasters);
        let mut raster_renderables: Vec<WalkersRasterRenderable> =
            Vec::with_capacity(rasters_raw.len());
        for r in rasters_raw {
            let opts = if r.nearest {
                egui::TextureOptions::NEAREST
            } else {
                egui::TextureOptions::LINEAR
            };
            if let Some(tex) = self.walkers_raster_cache.ensure(
                &ctx_clone,
                r.id,
                r.width_px,
                r.height_px,
                r.content_version,
                opts,
                &r.pixels,
            ) {
                raster_renderables.push(WalkersRasterRenderable {
                    nw: walkers::lon_lat(r.min_lon, r.max_lat),
                    se: walkers::lon_lat(r.max_lon, r.min_lat),
                    tex,
                    opacity: r.opacity,
                });
            } else {
                // Nothing drawable: no cache entry and no pixels in the
                // payload — the sender's full upload went into a discarded
                // hidden-tab buffer, or the idle LRU evicted the texture.
                // Report on the starved register so Go re-ships
                // (fetchR22StarvedTextures; ids are rasterIds here).
                self.r22_starved_texture_ids.push(r.id);
            }
        }

        // Take a scoped mutable borrow of the retained state. We need it
        // for outline-cache lookup and for the memory/tiles borrow into
        // Map::new; we drop it before we touch self.r7_push to avoid
        // overlapping borrows.
        let resp_and_camera: (egui::Response, Option<WalkersCamera>) = {
            let state =
                self.walkers_states.get_mut(&map_id).expect("walkers state was just inserted");

            // Aggregate H3 regions using the per-state outline cache.
            let h3_region_renderables: Vec<H3RegionRenderable> = h3_regions_raw
                .into_iter()
                .map(|r| aggregate_h3_region(&mut state.region_outline_cache, r))
                .collect();

            // Apply programmatic overrides.
            if let Some(z) = override_zoom {
                let _ = state.memory.set_zoom(z);
            }
            if let Some((lat, lon)) = override_center {
                state.memory.center_at(walkers::lon_lat(lon, lat));
            }

            let mut camera_captured: Option<WalkersCamera> = None;

            // Plugin must be constructed inside this inner scope so its
            // borrow of `camera_captured` ends before we return from the
            // outer block; otherwise the &mut would escape.
            let resp = {
                let plugin = OverlayPlugin {
                    markers,
                    polylines,
                    h3_choropleth: h3_choropleth_renderables,
                    h3_regions: h3_region_renderables,
                    rasters: raster_renderables,
                    camera_out: &mut camera_captured,
                    map_id,
                };

                let initial = walkers::lon_lat(init_lon, init_lat);
                // FillAvailable sizes the map to the parent's remaining space
                // so it exactly fills a bounded host (e.g. a no-scroll dock
                // tab — nothing overflows, nothing clips). Only meaningful in
                // a bounded parent: a scrolling ScrollArea reports an
                // effectively unbounded remainder — use Width/Height there.
                // The floor guards a collapsed splitter (degenerate ~0-size
                // leaf) so walkers never sees a zero-area viewport.
                let size = if fill_available {
                    u.as_ref().unwrap().available_size().max(egui::vec2(32.0, 32.0))
                } else {
                    egui::vec2(width, height)
                };

                // Split mutable borrow of `state` — tiles and memory are
                // both needed by Map::new simultaneously.
                let tiles_opt = &mut state.tiles;
                let memory = &mut state.memory;
                let tiles_dyn: Option<&mut dyn walkers::Tiles> =
                    tiles_opt.as_mut().map(|t| t as &mut dyn walkers::Tiles);

                let map_widget = walkers::Map::new(tiles_dyn, memory, initial)
                    .zoom_gesture(zoom_gesture)
                    .panning(panning)
                    .with_plugin(plugin);

                let ui = u.as_mut().unwrap();
                ui.add_sized(size, map_widget)
            };

            (resp, camera_captured)
        };

        let (resp, camera_captured) = resp_and_camera;

        // Push widget response flags for fetchR7 — lets Go see clicks, hovers,
        // drags on the map body itself (not on overlay items).
        if self.r8_response_flags_filter.match_response_any(&resp) {
            let mut flags = ResponseFlags::empty();
            flags.populate(&resp);
            self.r7_push(map_id, flags);
        }

        if let Some(cam) = camera_captured {
            self.walkers_cameras.insert(cam.map_id, cam);
        }
    }

    pub fn apply_widget(
        &mut self,
        w: impl egui::Widget,
        u: &mut Option<&mut egui::Ui>,
        f: &FuncProcId,
        i: Option<egui::Id>,
    ) -> Option<egui::Response> {
        if u.is_some() {
            let r = w.ui(u.as_mut().unwrap());
            if i.is_some() && self.r8_response_flags_filter.match_response_any(&r) {
                let mut res = ResponseFlags::empty();
                res.populate(&r);
                //tracing::debug!(
                //    "sending response {} {:?}",
                //    i.unwrap().value(),
                //    res.iter_names().map(|p| p.0).collect::<Vec<&'static str>>(),
                //);
                self.r7_push(i.unwrap().value(), res);
            }
            return Some(r);
        }
        //tracing::debug!("late culled widget {:?}", f);
        return None;
    }
    pub fn read_from_repr<T>(
        &mut self,
        from_repr: fn(discriminator: u32) -> Option<T>,
    ) -> FffiResult<(T, u32)> {
        let f = self.io.read_plain_u32()?;
        let r = from_repr(f).ok_or(FffiError::FromRepr(f))?;
        return Ok((r, f));
    }
    #[allow(unsafe_code)]
    pub fn read_id(&mut self) -> FffiResult<egui::Id> {
        let id = self.io.read_plain_u64()?;
        unsafe {
            return Ok(egui::Id::from_high_entropy_bits(id));
        }
    }
    pub fn write_roaring_treemap_r5(&mut self) -> Result<(), FffiError> {
        let r = &self.r5_id_set;
        let raw_sz = r.serialized_size();
        let sz: u32 = raw_sz.try_into().map_err(|_| FffiError::SerializedSizeOverflow(raw_sz))?;
        self.io.write_plain_u32(sz)?;
        r.serialize_into(&mut self.io.w)?;
        return Ok(());
    }
}

// =============================================================================
// EGUI-SNARL (ADR-0021) — node-editor binding
// =============================================================================
// Companion to egui2_definition_d_snarl.go. Three accumulator types
// (snarlNode / snarlConnection / snarlPin) drained by the snarlEditor
// opcode; per-editor retained Snarl<u64> in `snarl_states` keyed by
// editor id. Edit events accumulated in `snarl_events_pending` and
// drained by fetchSnarlEvents.

pub struct SnarlNodeData {
    pub id: u64,
    pub x: f32,
    pub y: f32,
    pub kind: u32,
    pub title: String,
    pub num_inputs: u32,
    pub num_outputs: u32,
}

pub struct SnarlConnectionData {
    pub src_node: u64,
    pub src_port: u32,
    pub dst_node: u64,
    pub dst_port: u32,
}

pub struct SnarlPinData {
    pub node_id: u64,
    pub side: u8,
    pub pin_idx: u32,
    pub label: String,
    pub kind: u32,
}

// One row in the events register. Drained by fetchSnarlEvents.
// `kind` is one of the SNARL_EV_* constants below; per-kind field
// usage documented at the constants.
#[derive(Debug, Clone)]
pub struct SnarlEvent {
    pub editor_id: u64,
    pub kind: u8,
    pub node_id: u64,
    pub port_a: u32,
    pub node_id_b: u64,
    pub port_b: u32,
    pub x: f32,
    pub y: f32,
}

pub const SNARL_EV_NODE_MOVED: u8 = 1;
pub const SNARL_EV_NODE_REMOVED: u8 = 2;
pub const SNARL_EV_CONNECTION_ADDED: u8 = 3;
pub const SNARL_EV_CONNECTION_REMOVED: u8 = 4;
pub const SNARL_EV_NODE_SELECTED: u8 = 5;
pub const SNARL_EV_NODE_DESELECTED: u8 = 6;
pub const SNARL_EV_NODE_OPEN_CHANGED: u8 = 7;

// Retained per-editor state. The bidirectional (Go u64) ↔ (Snarl
// NodeId) maps stay in lock-step with the Snarl's actual node set;
// any code path that inserts or removes a node must update both.
// `value: u64` inside each Snarl Node carries the same Go id, which
// the viewer reads via `snarl[node_id]` to colour pins / look up
// per-pin metadata.
pub struct SnarlState {
    pub snarl: egui_snarl::Snarl<u64>,
    pub go_to_snarl: std::collections::HashMap<u64, egui_snarl::NodeId>,
    pub snarl_to_go: std::collections::HashMap<egui_snarl::NodeId, u64>,
}

pub fn new_snarl_state() -> SnarlState {
    SnarlState {
        snarl: egui_snarl::Snarl::new(),
        go_to_snarl: std::collections::HashMap::new(),
        snarl_to_go: std::collections::HashMap::new(),
    }
}

// Per-pin metadata captured from snarlPin entries, keyed by
// (go_node_id, side, pin_idx). Drives default pin rendering in
// FffiSnarlViewer's show_input / show_output.
pub struct SnarlPinMeta {
    pub label: String,
    pub kind: u32,
}

// Stable cheap hash from `kind` to a circle pin colour. kind=0 picks
// a neutral grey (the egui default text colour family); non-zero kinds
// use a Knuth-multiplicative hash with the low bits clamped to keep
// any one channel from going below 0x60, which is dark enough to read
// against the light Frame background but doesn't blow out highlights
// against the dark theme.
fn snarl_kind_color(kind: u32) -> egui::Color32 {
    if kind == 0 {
        return egui::Color32::from_rgb(0xb0, 0xb0, 0xb0);
    }
    let h = kind.wrapping_mul(2654435761);
    let r = ((h >> 16) & 0xff) as u8 | 0x60;
    let g = ((h >> 8) & 0xff) as u8 | 0x60;
    let b = (h & 0xff) as u8 | 0x60;
    egui::Color32::from_rgb(r, g, b)
}

// Bridge between egui-snarl's SnarlViewer trait (per-node callbacks)
// and FFFI2's deferred-block + accumulator model. Owns the per-frame
// node-body bytes, title / pin-count / pin-meta lookups, and a
// non-aliasing event sink (`&RefCell<Vec<SnarlEvent>>` lives in the
// caller's stack frame so the viewer's borrow on `interpreter` stays
// disjoint from the events vector).
pub struct FffiSnarlViewer<'a, 'b, 'c, 'd, R: std::io::BufRead, W: std::io::Write> {
    pub interpreter: &'b mut ImZeroFffi<'a, R, W>,
    pub ctx: &'c egui::Context,
    pub editor_id: u64,
    pub node_bodies: std::collections::HashMap<u64, Vec<u8>>,
    pub titles: std::collections::HashMap<u64, String>,
    pub pin_counts: std::collections::HashMap<u64, (u32, u32)>,
    pub pin_meta: std::collections::HashMap<(u64, u8, u32), SnarlPinMeta>,
    pub events: &'d std::cell::RefCell<Vec<SnarlEvent>>,
}

impl<'a, 'b, 'c, 'd, R: std::io::BufRead, W: std::io::Write> egui_snarl::ui::SnarlViewer<u64>
    for FffiSnarlViewer<'a, 'b, 'c, 'd, R, W>
{
    fn title(&mut self, node: &u64) -> String {
        self.titles.get(node).cloned().unwrap_or_else(|| format!("node {node}"))
    }

    fn inputs(&mut self, node: &u64) -> usize {
        self.pin_counts.get(node).map_or(0, |(i, _)| *i as usize)
    }

    fn outputs(&mut self, node: &u64) -> usize {
        self.pin_counts.get(node).map_or(0, |(_, o)| *o as usize)
    }

    fn show_input(
        &mut self,
        pin: &egui_snarl::InPin,
        ui: &mut egui::Ui,
        snarl: &mut egui_snarl::Snarl<u64>,
    ) -> impl egui_snarl::ui::SnarlPin + 'static {
        let go_id = snarl[pin.id.node];
        let key = (go_id, 0u8, pin.id.input as u32);
        let (label, kind) =
            self.pin_meta.get(&key).map(|m| (m.label.as_str(), m.kind)).unwrap_or(("", 0));
        if !label.is_empty() {
            ui.label(label);
        }
        egui_snarl::ui::PinInfo::circle().with_fill(snarl_kind_color(kind))
    }

    fn show_output(
        &mut self,
        pin: &egui_snarl::OutPin,
        ui: &mut egui::Ui,
        snarl: &mut egui_snarl::Snarl<u64>,
    ) -> impl egui_snarl::ui::SnarlPin + 'static {
        let go_id = snarl[pin.id.node];
        let key = (go_id, 1u8, pin.id.output as u32);
        let (label, kind) =
            self.pin_meta.get(&key).map(|m| (m.label.as_str(), m.kind)).unwrap_or(("", 0));
        if !label.is_empty() {
            ui.label(label);
        }
        egui_snarl::ui::PinInfo::circle().with_fill(snarl_kind_color(kind))
    }

    fn has_body(&mut self, node: &u64) -> bool {
        self.node_bodies.get(node).is_some_and(|b| !b.is_empty())
    }

    fn show_body(
        &mut self,
        node: egui_snarl::NodeId,
        _inputs: &[egui_snarl::InPin],
        _outputs: &[egui_snarl::OutPin],
        ui: &mut egui::Ui,
        snarl: &mut egui_snarl::Snarl<u64>,
    ) {
        let go_id = snarl[node];
        if let Some(block) = self.node_bodies.get(&go_id) {
            if !block.is_empty() {
                let _ = self.interpreter.replay_deferred_block(self.ctx, ui, block);
            }
        }
    }

    fn connect(
        &mut self,
        from: &egui_snarl::OutPin,
        to: &egui_snarl::InPin,
        snarl: &mut egui_snarl::Snarl<u64>,
    ) {
        let src_go = snarl[from.id.node];
        let dst_go = snarl[to.id.node];
        snarl.connect(from.id, to.id);
        self.events.borrow_mut().push(SnarlEvent {
            editor_id: self.editor_id,
            kind: SNARL_EV_CONNECTION_ADDED,
            node_id: src_go,
            port_a: from.id.output as u32,
            node_id_b: dst_go,
            port_b: to.id.input as u32,
            x: 0.0,
            y: 0.0,
        });
    }

    fn disconnect(
        &mut self,
        from: &egui_snarl::OutPin,
        to: &egui_snarl::InPin,
        snarl: &mut egui_snarl::Snarl<u64>,
    ) {
        let src_go = snarl[from.id.node];
        let dst_go = snarl[to.id.node];
        snarl.disconnect(from.id, to.id);
        self.events.borrow_mut().push(SnarlEvent {
            editor_id: self.editor_id,
            kind: SNARL_EV_CONNECTION_REMOVED,
            node_id: src_go,
            port_a: from.id.output as u32,
            node_id_b: dst_go,
            port_b: to.id.input as u32,
            x: 0.0,
            y: 0.0,
        });
    }

    /// Right-click on an output pin removes every wire attached to it.
    /// The default impl just calls `snarl.drop_outputs` and emits no
    /// events, so Go's authoritative model keeps the connections and the
    /// next frame's reconcile re-creates them. Override to emit one
    /// ConnectionRemoved per dropped wire so Go can update its state.
    fn drop_outputs(&mut self, pin: &egui_snarl::OutPin, snarl: &mut egui_snarl::Snarl<u64>) {
        let src_go = snarl[pin.id.node];
        let src_port = pin.id.output as u32;
        for in_pin_id in &pin.remotes {
            let dst_go = snarl[in_pin_id.node];
            self.events.borrow_mut().push(SnarlEvent {
                editor_id: self.editor_id,
                kind: SNARL_EV_CONNECTION_REMOVED,
                node_id: src_go,
                port_a: src_port,
                node_id_b: dst_go,
                port_b: in_pin_id.input as u32,
                x: 0.0,
                y: 0.0,
            });
        }
        snarl.drop_outputs(pin.id);
    }

    /// Right-click on an input pin — symmetric to drop_outputs above.
    fn drop_inputs(&mut self, pin: &egui_snarl::InPin, snarl: &mut egui_snarl::Snarl<u64>) {
        let dst_go = snarl[pin.id.node];
        let dst_port = pin.id.input as u32;
        for out_pin_id in &pin.remotes {
            let src_go = snarl[out_pin_id.node];
            self.events.borrow_mut().push(SnarlEvent {
                editor_id: self.editor_id,
                kind: SNARL_EV_CONNECTION_REMOVED,
                node_id: src_go,
                port_a: out_pin_id.output as u32,
                node_id_b: dst_go,
                port_b: dst_port,
                x: 0.0,
                y: 0.0,
            });
        }
        snarl.drop_inputs(pin.id);
    }
}

impl<'a, R: std::io::BufRead, W: std::io::Write> ImZeroFffi<'a, R, W> {
    /// Drains the per-frame paint_cmds buffer into the given painter,
    /// offsetting every coordinate by `origin`. Single source of truth
    /// for the cmd-match loop — used by PaintCanvas (with origin =
    /// canvas's resp.rect.min and ui_for_sense = Some(ui), so
    /// SenseRegion can fire) and by paintAbsoluteOverlay (origin =
    /// Pos2::ZERO since the overlay painter already paints in
    /// viewport-absolute coords, ui_for_sense = None so SenseRegion
    /// logs and skips).
    ///
    /// Adding a new PaintCmd variant: extend the match here and both
    /// call sites pick it up automatically — no drift risk between
    /// canvas and overlay drains.
    pub fn drain_paint_cmds_to_painter(
        &mut self,
        painter: &egui::Painter,
        origin: egui::Pos2,
        mut ui_for_sense: Option<&mut egui::Ui>,
    ) {
        let cmds: Vec<PaintCmd> = self.paint_cmds.drain(..).collect();
        // ClipPush swaps the active painter for a clip-intersected clone; the
        // stack restores the previous clip on ClipPop, and an underflowing pop
        // restores the canvas painter.
        let mut cur = painter.clone();
        let mut clip_stack: Vec<egui::Painter> = Vec::new();
        for cmd in &cmds {
            match cmd {
                PaintCmd::CircleFilled {
                    cx,
                    cy,
                    radius,
                    fill,
                } => {
                    cur.circle_filled(
                        egui::Pos2::new(origin.x + cx, origin.y + cy),
                        *radius,
                        *fill,
                    );
                }
                PaintCmd::CircleStroke {
                    cx,
                    cy,
                    radius,
                    stroke,
                } => {
                    cur.circle_stroke(
                        egui::Pos2::new(origin.x + cx, origin.y + cy),
                        *radius,
                        *stroke,
                    );
                }
                PaintCmd::RectFilled {
                    min_x,
                    min_y,
                    max_x,
                    max_y,
                    rounding,
                    fill,
                } => {
                    cur.rect_filled(
                        egui::Rect::from_min_max(
                            egui::Pos2::new(origin.x + min_x, origin.y + min_y),
                            egui::Pos2::new(origin.x + max_x, origin.y + max_y),
                        ),
                        *rounding,
                        *fill,
                    );
                }
                PaintCmd::RectStroke {
                    min_x,
                    min_y,
                    max_x,
                    max_y,
                    rounding,
                    stroke,
                } => {
                    cur.rect_stroke(
                        egui::Rect::from_min_max(
                            egui::Pos2::new(origin.x + min_x, origin.y + min_y),
                            egui::Pos2::new(origin.x + max_x, origin.y + max_y),
                        ),
                        *rounding,
                        *stroke,
                        egui::StrokeKind::Outside,
                    );
                }
                PaintCmd::Line {
                    from_x,
                    from_y,
                    to_x,
                    to_y,
                    stroke,
                } => {
                    cur.line_segment(
                        [
                            egui::Pos2::new(origin.x + from_x, origin.y + from_y),
                            egui::Pos2::new(origin.x + to_x, origin.y + to_y),
                        ],
                        *stroke,
                    );
                }
                PaintCmd::DashedLine {
                    from_x,
                    from_y,
                    to_x,
                    to_y,
                    dash_len,
                    gap_len,
                    stroke,
                } => {
                    let path = [
                        egui::Pos2::new(origin.x + from_x, origin.y + from_y),
                        egui::Pos2::new(origin.x + to_x, origin.y + to_y),
                    ];
                    let shapes = egui::Shape::dashed_line(&path, *stroke, *dash_len, *gap_len);
                    for shape in shapes {
                        cur.add(shape);
                    }
                }
                PaintCmd::Arrow {
                    ox,
                    oy,
                    dx,
                    dy,
                    stroke,
                } => {
                    cur.arrow(
                        egui::Pos2::new(origin.x + ox, origin.y + oy),
                        egui::Vec2::new(*dx, *dy),
                        *stroke,
                    );
                }
                PaintCmd::Polyline { points, stroke } => {
                    let pts: Vec<egui::Pos2> = points
                        .iter()
                        .map(|p| egui::Pos2::new(origin.x + p[0], origin.y + p[1]))
                        .collect();
                    cur.line(pts, *stroke);
                }
                PaintCmd::PolygonFilled {
                    points,
                    fill,
                    stroke,
                    concave,
                } => {
                    let pts: Vec<egui::Pos2> = points
                        .iter()
                        .map(|p| egui::Pos2::new(origin.x + p[0], origin.y + p[1]))
                        .collect();
                    if *concave && pts.len() >= 3 {
                        // epaint fan-fills assuming convexity; ear-clip into a
                        // mesh instead. A raw mesh gets no feathering — the
                        // optional outline stroke below covers the hard edge.
                        let flat: Vec<f64> =
                            pts.iter().flat_map(|p| [p.x as f64, p.y as f64]).collect();
                        match earcutr::earcut(&flat, &[], 2) {
                            Ok(indices) => {
                                let mut mesh = egui::Mesh::default();
                                mesh.vertices.reserve(pts.len());
                                for p in &pts {
                                    mesh.colored_vertex(*p, *fill);
                                }
                                mesh.indices = indices.into_iter().map(|ix| ix as u32).collect();
                                cur.add(egui::Shape::mesh(mesh));
                            }
                            Err(err) => {
                                tracing::warn!(
                                    ?err,
                                    n = pts.len(),
                                    "paintPolygonFilled: earcut failed; dropping fill"
                                );
                            }
                        }
                        if stroke.width > 0.0 {
                            cur.add(egui::Shape::closed_line(pts, *stroke));
                        }
                    } else if *concave {
                        // < 3 points: nothing fillable; honour a stroke if any.
                        if stroke.width > 0.0 {
                            cur.add(egui::Shape::closed_line(pts, *stroke));
                        }
                    } else {
                        cur.add(egui::Shape::convex_polygon(pts, *fill, *stroke));
                    }
                }
                PaintCmd::EllipseFilled {
                    cx,
                    cy,
                    rx,
                    ry,
                    fill,
                } => {
                    cur.add(egui::Shape::ellipse_filled(
                        egui::Pos2::new(origin.x + cx, origin.y + cy),
                        egui::Vec2::new(*rx, *ry),
                        *fill,
                    ));
                }
                PaintCmd::EllipseStroke {
                    cx,
                    cy,
                    rx,
                    ry,
                    stroke,
                } => {
                    cur.add(egui::Shape::ellipse_stroke(
                        egui::Pos2::new(origin.x + cx, origin.y + cy),
                        egui::Vec2::new(*rx, *ry),
                        *stroke,
                    ));
                }
                PaintCmd::Text {
                    px,
                    py,
                    anchor_h,
                    anchor_v,
                    text,
                    font_size,
                    color,
                    monospace,
                } => {
                    let align2 = match (anchor_h, anchor_v) {
                        (0, 0) => egui::Align2::LEFT_TOP,
                        (0, 1) => egui::Align2::LEFT_CENTER,
                        (0, 2) => egui::Align2::LEFT_BOTTOM,
                        (1, 0) => egui::Align2::CENTER_TOP,
                        (1, 1) => egui::Align2::CENTER_CENTER,
                        (1, 2) => egui::Align2::CENTER_BOTTOM,
                        (2, 0) => egui::Align2::RIGHT_TOP,
                        (2, 1) => egui::Align2::RIGHT_CENTER,
                        (2, 2) => egui::Align2::RIGHT_BOTTOM,
                        _ => egui::Align2::LEFT_TOP,
                    };
                    let font = if *monospace {
                        egui::FontId::monospace(*font_size)
                    } else {
                        egui::FontId::proportional(*font_size)
                    };
                    cur.text(
                        egui::Pos2::new(origin.x + px, origin.y + py),
                        align2,
                        text,
                        font,
                        *color,
                    );
                }
                PaintCmd::CubicBezier {
                    x0,
                    y0,
                    x1,
                    y1,
                    x2,
                    y2,
                    x3,
                    y3,
                    stroke,
                } => {
                    let shape = egui::epaint::CubicBezierShape::from_points_stroke(
                        [
                            egui::Pos2::new(origin.x + x0, origin.y + y0),
                            egui::Pos2::new(origin.x + x1, origin.y + y1),
                            egui::Pos2::new(origin.x + x2, origin.y + y2),
                            egui::Pos2::new(origin.x + x3, origin.y + y3),
                        ],
                        false,
                        egui::Color32::TRANSPARENT,
                        *stroke,
                    );
                    cur.add(shape);
                }
                PaintCmd::SenseRegion { id, px, py, sw, sh } => {
                    if let Some(ui) = ui_for_sense.as_deref_mut() {
                        let rect = egui::Rect::from_min_size(
                            egui::Pos2::new(origin.x + *px, origin.y + *py),
                            egui::Vec2::new(*sw, *sh),
                        );
                        let sub_resp = ui.interact(rect, *id, egui::Sense::click_and_drag());
                        let mut flags = ResponseFlags::empty();
                        flags.populate(&sub_resp);
                        self.r7_push(id.value(), flags);
                        // ADR-0149 M1: the region's own r24 pointer row. On the
                        // drag-started frame report the press origin, not the
                        // end-of-frame pointer — fast input batches several
                        // move events into the press frame, and a box/pan
                        // anchor read one frame later would otherwise start
                        // mid-gesture (canvas-relative, like the canvas row).
                        {
                            let pp = if sub_resp.drag_started() {
                                ui.input(|inp| inp.pointer.press_origin())
                            } else if sub_resp.dragged() {
                                sub_resp.interact_pointer_pos()
                            } else {
                                sub_resp.hover_pos()
                            };
                            let (ppx, ppy) = match pp {
                                Some(p) => (p.x - origin.x, p.y - origin.y),
                                None => (f32::NAN, f32::NAN),
                            };
                            // Modifiers travel with the row. On the drag-started
                            // frame take them from the press event itself — the
                            // frame-end modifier state misses a modifier that was
                            // pressed and released within one batched frame
                            // (machine-speed input); the press event's modifiers
                            // are exact at any speed.
                            let m = ui.input(|inp| {
                                if sub_resp.drag_started() {
                                    inp.events
                                        .iter()
                                        .rev()
                                        .find_map(|e| match e {
                                            egui::Event::PointerButton {
                                                pressed: true,
                                                modifiers,
                                                ..
                                            } => Some(*modifiers),
                                            _ => None,
                                        })
                                        .unwrap_or(inp.modifiers)
                                } else {
                                    inp.modifiers
                                }
                            });
                            let mods = (m.shift as u8)
                                | ((m.ctrl as u8) << 1)
                                | ((m.alt as u8) << 2)
                                | ((m.command as u8) << 3);
                            self.r24_canvas_pointer_push(
                                id.value(),
                                origin.x,
                                origin.y,
                                ppx,
                                ppy,
                                mods,
                            );
                        }
                    } else {
                        tracing::warn!(
                            id = id.value(),
                            "PaintCmd::SenseRegion drained without a Ui scope (paintAbsoluteOverlay?); skipped"
                        );
                    }
                }
                PaintCmd::ClipPush {
                    min_x,
                    min_y,
                    max_x,
                    max_y,
                } => {
                    let clip = egui::Rect::from_min_max(
                        egui::Pos2::new(origin.x + min_x, origin.y + min_y),
                        egui::Pos2::new(origin.x + max_x, origin.y + max_y),
                    )
                    .intersect(cur.clip_rect());
                    clip_stack.push(cur.clone());
                    cur = cur.with_clip_rect(clip);
                }
                PaintCmd::ClipPop => {
                    cur = clip_stack.pop().unwrap_or_else(|| painter.clone());
                }
                PaintCmd::Markers {
                    xs,
                    ys,
                    shape,
                    radius,
                    color,
                    weight,
                } => {
                    // Shape numbering is the IDL/ImPlot contract: 0=circle
                    // 1=square 2=diamond 3=up 4=down 5=left 6=right 7=cross
                    // 8=plus 9=asterisk; 0-6 filled, 7-9 stroked at weight.
                    // Polygon vertices are inscribed in the radius-ra circle.
                    const SIN60: f32 = 0.866_025_4;
                    const INV_SQRT2: f32 = 0.707_106_77;
                    let n = xs.len().min(ys.len());
                    let ra = *radius;
                    let st = egui::Stroke::new(*weight, *color);
                    for k in 0..n {
                        let p = egui::Pos2::new(origin.x + xs[k], origin.y + ys[k]);
                        match *shape {
                            1 => {
                                let hs = ra * INV_SQRT2;
                                cur.rect_filled(
                                    egui::Rect::from_center_size(p, egui::Vec2::splat(2.0 * hs)),
                                    0.0,
                                    *color,
                                );
                            }
                            2 => {
                                cur.add(egui::Shape::convex_polygon(
                                    vec![
                                        egui::Pos2::new(p.x - ra, p.y),
                                        egui::Pos2::new(p.x, p.y - ra),
                                        egui::Pos2::new(p.x + ra, p.y),
                                        egui::Pos2::new(p.x, p.y + ra),
                                    ],
                                    *color,
                                    egui::Stroke::NONE,
                                ));
                            }
                            3 => {
                                cur.add(egui::Shape::convex_polygon(
                                    vec![
                                        egui::Pos2::new(p.x, p.y - ra),
                                        egui::Pos2::new(p.x - ra * SIN60, p.y + ra * 0.5),
                                        egui::Pos2::new(p.x + ra * SIN60, p.y + ra * 0.5),
                                    ],
                                    *color,
                                    egui::Stroke::NONE,
                                ));
                            }
                            4 => {
                                cur.add(egui::Shape::convex_polygon(
                                    vec![
                                        egui::Pos2::new(p.x, p.y + ra),
                                        egui::Pos2::new(p.x + ra * SIN60, p.y - ra * 0.5),
                                        egui::Pos2::new(p.x - ra * SIN60, p.y - ra * 0.5),
                                    ],
                                    *color,
                                    egui::Stroke::NONE,
                                ));
                            }
                            5 => {
                                cur.add(egui::Shape::convex_polygon(
                                    vec![
                                        egui::Pos2::new(p.x - ra, p.y),
                                        egui::Pos2::new(p.x + ra * 0.5, p.y - ra * SIN60),
                                        egui::Pos2::new(p.x + ra * 0.5, p.y + ra * SIN60),
                                    ],
                                    *color,
                                    egui::Stroke::NONE,
                                ));
                            }
                            6 => {
                                cur.add(egui::Shape::convex_polygon(
                                    vec![
                                        egui::Pos2::new(p.x + ra, p.y),
                                        egui::Pos2::new(p.x - ra * 0.5, p.y + ra * SIN60),
                                        egui::Pos2::new(p.x - ra * 0.5, p.y - ra * SIN60),
                                    ],
                                    *color,
                                    egui::Stroke::NONE,
                                ));
                            }
                            7 => {
                                let dd = ra * INV_SQRT2;
                                cur.line_segment(
                                    [
                                        egui::Pos2::new(p.x - dd, p.y - dd),
                                        egui::Pos2::new(p.x + dd, p.y + dd),
                                    ],
                                    st,
                                );
                                cur.line_segment(
                                    [
                                        egui::Pos2::new(p.x - dd, p.y + dd),
                                        egui::Pos2::new(p.x + dd, p.y - dd),
                                    ],
                                    st,
                                );
                            }
                            8 => {
                                cur.line_segment(
                                    [
                                        egui::Pos2::new(p.x - ra, p.y),
                                        egui::Pos2::new(p.x + ra, p.y),
                                    ],
                                    st,
                                );
                                cur.line_segment(
                                    [
                                        egui::Pos2::new(p.x, p.y - ra),
                                        egui::Pos2::new(p.x, p.y + ra),
                                    ],
                                    st,
                                );
                            }
                            9 => {
                                cur.line_segment(
                                    [
                                        egui::Pos2::new(p.x, p.y - ra),
                                        egui::Pos2::new(p.x, p.y + ra),
                                    ],
                                    st,
                                );
                                cur.line_segment(
                                    [
                                        egui::Pos2::new(p.x - ra * SIN60, p.y - ra * 0.5),
                                        egui::Pos2::new(p.x + ra * SIN60, p.y + ra * 0.5),
                                    ],
                                    st,
                                );
                                cur.line_segment(
                                    [
                                        egui::Pos2::new(p.x - ra * SIN60, p.y + ra * 0.5),
                                        egui::Pos2::new(p.x + ra * SIN60, p.y - ra * 0.5),
                                    ],
                                    st,
                                );
                            }
                            _ => {
                                cur.circle_filled(p, ra, *color);
                            }
                        }
                    }
                }
                PaintCmd::Image {
                    id,
                    min_x,
                    min_y,
                    max_x,
                    max_y,
                    width_px,
                    height_px,
                    content_version,
                    pixels,
                    nearest,
                    opacity,
                } => {
                    let opts = if *nearest {
                        egui::TextureOptions::NEAREST
                    } else {
                        egui::TextureOptions::LINEAR
                    };
                    let ctx2 = cur.ctx().clone();
                    if let Some(tex) = self.paint_image_cache.ensure(
                        &ctx2,
                        *id,
                        *width_px,
                        *height_px,
                        *content_version,
                        opts,
                        pixels,
                    ) {
                        let rect = egui::Rect::from_min_max(
                            egui::Pos2::new(origin.x + min_x, origin.y + min_y),
                            egui::Pos2::new(origin.x + max_x, origin.y + max_y),
                        );
                        let tint = egui::Color32::WHITE.gamma_multiply(*opacity);
                        cur.image(
                            tex,
                            rect,
                            egui::Rect::from_min_max(
                                egui::Pos2::new(0.0, 0.0),
                                egui::Pos2::new(1.0, 1.0),
                            ),
                            tint,
                        );
                    } else {
                        // No cache entry and no pixels: the upload went into a
                        // discarded buffer or the LRU evicted the texture.
                        // Report starved so Go re-ships (fetchR22).
                        self.r22_starved_texture_ids.push(*id);
                    }
                }
                PaintCmd::RectsFilled {
                    min_xs,
                    min_ys,
                    max_xs,
                    max_ys,
                    cols,
                } => {
                    let n = min_xs
                        .len()
                        .min(min_ys.len())
                        .min(max_xs.len())
                        .min(max_ys.len())
                        .min(cols.len());
                    for k in 0..n {
                        cur.rect_filled(
                            egui::Rect::from_min_max(
                                egui::Pos2::new(origin.x + min_xs[k], origin.y + min_ys[k]),
                                egui::Pos2::new(origin.x + max_xs[k], origin.y + max_ys[k]),
                            ),
                            0.0,
                            color32_from_rgba_u32(cols[k]),
                        );
                    }
                }
            }
        }
    }

    /// SD11 — thick-client apply for the `snarlEditor` opcode. Drains
    /// the per-frame accumulators, reconciles topology against the
    /// retained Snarl<u64>, runs the SnarlViewer-backed render, and
    /// captures NodeMoved / connect / disconnect events for
    /// fetchSnarlEvents.
    #[allow(clippy::too_many_arguments)]
    pub fn render_snarl_editor(
        &mut self,
        editor_id: u64,
        ctx: &egui::Context,
        ui: &mut egui::Ui,
        node_bodies: std::collections::HashMap<u64, Vec<u8>>,
        ed_width: f32,
        ed_height: f32,
        persist_positions: bool,
        wire_style: u8,
        bg_pattern: u8,
        min_scale: f32,
        max_scale: f32,
        centering: bool,
        crisp_magnified_text: bool,
    ) -> FffiResult<()> {
        let pending_nodes: Vec<SnarlNodeData> = self.snarl_pending_nodes.drain(..).collect();
        let pending_connections: Vec<SnarlConnectionData> =
            self.snarl_pending_connections.drain(..).collect();
        let pending_pins: Vec<SnarlPinData> = self.snarl_pending_pins.drain(..).collect();

        // Take SnarlState out so the FffiSnarlViewer can hold &mut self
        // without aliasing the HashMap entry; re-inserted at the end.
        let mut state = self.snarl_states.remove(&editor_id).unwrap_or_else(new_snarl_state);

        // --- Reconcile nodes against Go's authoritative declaration -----
        let mut titles: std::collections::HashMap<u64, String> =
            std::collections::HashMap::with_capacity(pending_nodes.len());
        let mut pin_counts: std::collections::HashMap<u64, (u32, u32)> =
            std::collections::HashMap::with_capacity(pending_nodes.len());
        let wanted: std::collections::HashSet<u64> = pending_nodes.iter().map(|n| n.id).collect();

        // Drop nodes that vanished from Go's declaration. Bidi maps are
        // updated in lock-step so the invariant
        //   go_to_snarl[id] = sid  ⟺  snarl_to_go[sid] = id
        // is preserved.
        let stale: Vec<u64> =
            state.go_to_snarl.keys().filter(|id| !wanted.contains(id)).copied().collect();
        for go_id in stale {
            if let Some(snarl_id) = state.go_to_snarl.remove(&go_id) {
                state.snarl_to_go.remove(&snarl_id);
                if state.snarl.get_node(snarl_id).is_some() {
                    state.snarl.remove_node(snarl_id);
                }
            }
        }

        // SD6 — Position authority. Default (persist_positions=false):
        // copy Go-supplied (x,y) onto the retained node every frame.
        // Opt-in (true): honour Go-supplied position only on first
        // insertion of a given id.
        for n in &pending_nodes {
            titles.insert(n.id, n.title.clone());
            pin_counts.insert(n.id, (n.num_inputs, n.num_outputs));
            if let Some(&snarl_id) = state.go_to_snarl.get(&n.id) {
                if !persist_positions {
                    if let Some(node_info) = state.snarl.get_node_info_mut(snarl_id) {
                        node_info.pos = egui::pos2(n.x, n.y);
                    }
                }
            } else {
                let snarl_id = state.snarl.insert_node(egui::pos2(n.x, n.y), n.id);
                state.go_to_snarl.insert(n.id, snarl_id);
                state.snarl_to_go.insert(snarl_id, n.id);
            }
        }

        // --- Reconcile connections (SD7 — full diff per frame) ---------
        let mut wanted_wires: std::collections::HashSet<(
            egui_snarl::OutPinId,
            egui_snarl::InPinId,
        )> = std::collections::HashSet::with_capacity(pending_connections.len());
        for c in &pending_connections {
            let (Some(&src_id), Some(&dst_id)) = (
                state.go_to_snarl.get(&c.src_node),
                state.go_to_snarl.get(&c.dst_node),
            ) else {
                continue;
            };
            let out = egui_snarl::OutPinId {
                node: src_id,
                output: c.src_port as usize,
            };
            let inp = egui_snarl::InPinId {
                node: dst_id,
                input: c.dst_port as usize,
            };
            wanted_wires.insert((out, inp));
        }
        let current_wires: Vec<(egui_snarl::OutPinId, egui_snarl::InPinId)> =
            state.snarl.wires().collect();
        for (out, inp) in &current_wires {
            if !wanted_wires.contains(&(*out, *inp)) {
                state.snarl.disconnect(*out, *inp);
            }
        }
        for (out, inp) in wanted_wires {
            // Snarl::connect is idempotent — duplicate (out,inp) tuples
            // are silently dropped.
            state.snarl.connect(out, inp);
        }

        // --- Pin metadata lookup ---------------------------------------
        let mut pin_meta: std::collections::HashMap<(u64, u8, u32), SnarlPinMeta> =
            std::collections::HashMap::with_capacity(pending_pins.len());
        for p in pending_pins {
            pin_meta.insert(
                (p.node_id, p.side, p.pin_idx),
                SnarlPinMeta {
                    label: p.label,
                    kind: p.kind,
                },
            );
        }

        // --- SnarlStyle from IDL setters -------------------------------
        let mut style = egui_snarl::ui::SnarlStyle::new();
        style.wire_style = Some(match wire_style {
            1 => egui_snarl::ui::WireStyle::AxisAligned { corner_radius: 5.0 },
            2 => egui_snarl::ui::WireStyle::Bezier3,
            3 => egui_snarl::ui::WireStyle::Line,
            _ => egui_snarl::ui::WireStyle::Bezier5,
        });
        style.bg_pattern = Some(match bg_pattern {
            0 => egui_snarl::ui::BackgroundPattern::NoPattern,
            _ => egui_snarl::ui::BackgroundPattern::new(),
        });
        if min_scale > 0.0 {
            style.min_scale = Some(min_scale);
        }
        if max_scale > 0.0 {
            style.max_scale = Some(max_scale);
        }
        style.centering = Some(centering);
        style.crisp_magnified_text = Some(crisp_magnified_text);

        // --- Snapshot pre-show positions for NodeMoved diff ------------
        let pre_positions: std::collections::HashMap<egui_snarl::NodeId, egui::Pos2> =
            state.snarl.nodes_pos_ids().map(|(id, pos, _)| (id, pos)).collect();

        let events_cell: std::cell::RefCell<Vec<SnarlEvent>> = std::cell::RefCell::new(Vec::new());

        // Bound the editor by allocating a child ui at the desired size
        // (same shape as the dock binding) and call Snarl::show on the
        // child. SnarlWidget min_size/max_size is the equivalent path,
        // but on egui 0.34 + glow it leaves the editor area blank for
        // reasons we haven't yet diagnosed; the child-ui pattern matches
        // how the official snarl demo runs inside CentralPanel and works.
        let avail = ui.available_size();
        let size = egui::vec2(
            if ed_width > 0.0 { ed_width } else { avail.x },
            if ed_height > 0.0 { ed_height } else { avail.y },
        );
        // KNOWN ISSUE — snarl's sublayer painting (Scene + set_sublayer
        // pattern at egui-snarl/src/ui.rs:958-979) renders blank in this
        // pipeline: the Frame::canvas paint that goes to the parent
        // layer is visible, but the new sublayer that hosts the grid /
        // nodes / wires never reaches the framebuffer. Same with
        // SnarlWidget::min_size/max_size and with Snarl::show on the
        // parent ui. egui_dock works fine (it doesn't use a sublayer),
        // so this is not a general layer-ordering regression — it's
        // specific to the Scene + sublayer combo. Diagnosis deferred;
        // the FFI surface, reconciliation, event capture, and demo all
        // function end-to-end below this line.
        let mut viewer = FffiSnarlViewer {
            interpreter: self,
            ctx,
            editor_id,
            node_bodies,
            titles,
            pin_counts,
            pin_meta,
            events: &events_cell,
        };
        egui_snarl::ui::SnarlWidget::new()
            .id_salt(("snarl", editor_id))
            .style(style)
            .min_size(size)
            .max_size(size)
            .show(&mut state.snarl, &mut viewer, ui);

        // --- Drain events: connect/disconnect from viewer overrides,
        //     NodeMoved from post-show position diff. ---------------------
        let mut frame_events = events_cell.into_inner();
        for (snarl_id, pos, _val) in state.snarl.nodes_pos_ids() {
            let pre = pre_positions.get(&snarl_id).copied();
            if pre != Some(pos) {
                if let Some(&go_id) = state.snarl_to_go.get(&snarl_id) {
                    frame_events.push(SnarlEvent {
                        editor_id,
                        kind: SNARL_EV_NODE_MOVED,
                        node_id: go_id,
                        port_a: 0,
                        node_id_b: 0,
                        port_b: 0,
                        x: pos.x,
                        y: pos.y,
                    });
                }
            }
        }
        self.snarl_events_pending.extend(frame_events);

        self.snarl_states.insert(editor_id, state);
        Ok(())
    }

    /// Renders an egui_extras::TableBuilder driven by `newTable` IDL state.
    ///
    /// Drains self.new_table_columns and self.new_table_row_heights, reads
    /// the two deferred block maps (header cells / row cells) from the IPC
    /// stream, then drives builder.header() (zero or one) followed by
    /// builder.body(). Cell content is replayed via replay_deferred_block.
    ///
    /// Borrow strategy: the closures egui_extras takes don't have a
    /// delegate trait to lean on, so each col() callback re-borrows
    /// `self` by name (the closure captures `&mut self` once and re-uses
    /// it on each col() call — the borrow is local to each FnOnce
    /// invocation, no overlap). The cell HashMaps are also captured by
    /// shared ref since lookups happen inside col().
    pub fn render_new_table(
        &mut self,
        table_id: u64,
        ctx: &egui::Context,
        ui: &mut egui::Ui,
        header_height: f32,
        striped: bool,
        vscroll: bool,
        min_scrolled_height: f32,
        max_scroll_height: f32,
        scroll_to_row: Option<usize>,
        auto_shrink: Option<(bool, bool)>,
        apply_widths_epoch: Option<u32>,
    ) -> InterpretResult<()> {
        // Read deferred block maps in declaration order (matches
        // SpliceDeferredBlockMap order on the Go side). These reads
        // happen unconditionally so the IPC stream stays balanced —
        // including on the sizing-pass bail-out below.
        let header_blocks = self.io.read_deferred_block_map_u32_u32()?;
        let row_blocks = self.io.read_deferred_block_map_u64_u32()?;

        let columns: Vec<egui_extras::Column> = self.new_table_columns.drain(..).collect();
        let row_heights: Vec<f32> = self.new_table_row_heights.drain(..).collect();
        let col_count = columns.len();

        if col_count == 0 {
            return Ok(());
        }

        // Sizing-pass bail-out.
        //
        // egui_extras' column-resize loop (table.rs line 814+) checks
        // `ui.is_sizing_pass()`. When true (the parent egui::Window is
        // running its initial content-sizing pass), every column is
        // forcibly shrunk to its content's max_used_width and then
        // clamped against width_range — and the resulting tiny widths
        // are persisted into TableState. Subsequent non-sizing-pass
        // frames load that persisted state and use Size::exact(prev)
        // for resizable columns, locking the table at the shrunken
        // size.
        //
        // Skipping the table render on sizing-pass frames avoids
        // poisoning the persisted state. Allocate a placeholder rect
        // of the full available space so the parent's content_size
        // sizing reads "table fills the panel", and let the first
        // non-sizing-pass frame initialize TableState cleanly via
        // egui_extras' to_lengths.
        if ui.is_sizing_pass() {
            let avail = ui.available_size_before_wrap();
            ui.allocate_exact_size(avail, egui::Sense::hover());
            drop((columns, row_heights, header_blocks, row_blocks));
            return Ok(());
        }
        // Suppress "unused" diagnostics on parameters only consumed
        // by the closures below — they're meaningful builder inputs.
        let _ = (table_id, header_height, ctx, auto_shrink);

        let mut builder = egui_extras::TableBuilder::new(ui).id_salt(egui::Id::new(table_id));
        for col in columns {
            builder = builder.column(col);
        }

        // Width apply (ADR-0151 §SD4), before header()/body() call
        // TableState::load. reset() addresses ui.id().with(id_salt) — the
        // same id_salt set above — so it drops exactly this table's state
        // and the next layout rebuilds from each column's initial width.
        if let Some(epoch) = apply_widths_epoch {
            if self.width_epochs.get(&table_id).copied() != Some(epoch) {
                builder.reset();
                self.width_epochs.insert(table_id, epoch);
            }
        }
        if striped {
            builder = builder.striped(true);
        }
        if vscroll {
            builder = builder.vscroll(true);
        }
        if min_scrolled_height > 0.0 {
            builder = builder.min_scrolled_height(min_scrolled_height);
        }
        if max_scroll_height > 0.0 {
            builder = builder.max_scroll_height(max_scroll_height);
        }
        if let Some(row) = scroll_to_row {
            builder = builder.scroll_to_row(row, None);
        }
        if let Some((h, v)) = auto_shrink {
            builder = builder.auto_shrink([h, v]);
        }

        // egui_extras 0.34 TableBuilder::header consumes self and returns
        // Table<'_>. So either: (a) header_height > 0 → call header() once,
        // proceed with the returned Table::body(); or (b) call body()
        // directly on the builder.
        let ctx_cloned = ctx.clone();
        let interp = self;
        if header_height > 0.0 {
            let table = builder.header(header_height, |mut header| {
                for col_idx in 0..col_count as u32 {
                    let block = header_blocks.get(&(0u32, col_idx));
                    header.col(|ui| {
                        if let Some(block) = block {
                            if !block.is_empty() {
                                let _ = interp.replay_deferred_block(&ctx_cloned, ui, block);
                            }
                        }
                    });
                }
            });
            new_table_render_body(
                table,
                &row_heights,
                &row_blocks,
                col_count,
                interp,
                &ctx_cloned,
            );
        } else {
            new_table_render_body_builder(
                builder,
                &row_heights,
                &row_blocks,
                col_count,
                interp,
                &ctx_cloned,
            );
        }

        Ok(())
    }
}

/// Drains row content blocks against an egui_extras::Table (post-header).
fn new_table_render_body<'a, R: std::io::BufRead, W: std::io::Write>(
    table: egui_extras::Table<'a>,
    row_heights: &[f32],
    row_blocks: &std::collections::HashMap<(u64, u32), Vec<u8>>,
    col_count: usize,
    interp: &mut ImZeroFffi<'_, R, W>,
    ctx: &egui::Context,
) {
    table.body(|mut body| {
        body.heterogeneous_rows(row_heights.iter().copied(), |mut row| {
            let row_idx = row.index() as u64;
            for col_idx in 0..col_count as u32 {
                let block = row_blocks.get(&(row_idx, col_idx));
                row.col(|ui| {
                    if let Some(block) = block {
                        if !block.is_empty() {
                            let _ = interp.replay_deferred_block(ctx, ui, block);
                        }
                    }
                });
            }
        });
    });
}

/// Drains row content blocks against an egui_extras::TableBuilder (no header).
fn new_table_render_body_builder<'a, R: std::io::BufRead, W: std::io::Write>(
    builder: egui_extras::TableBuilder<'a>,
    row_heights: &[f32],
    row_blocks: &std::collections::HashMap<(u64, u32), Vec<u8>>,
    col_count: usize,
    interp: &mut ImZeroFffi<'_, R, W>,
    ctx: &egui::Context,
) {
    builder.body(|mut body| {
        body.heterogeneous_rows(row_heights.iter().copied(), |mut row| {
            let row_idx = row.index() as u64;
            for col_idx in 0..col_count as u32 {
                let block = row_blocks.get(&(row_idx, col_idx));
                row.col(|ui| {
                    if let Some(block) = block {
                        if !block.is_empty() {
                            let _ = interp.replay_deferred_block(ctx, ui, block);
                        }
                    }
                });
            }
        });
    });
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_write_png_creates_valid_file() {
        let dir = std::env::temp_dir();
        let path = dir.join("imzero2_test_screenshot.png");
        let path_str = path.to_str().expect("valid path");

        // 2x2 red/green/blue/white RGBA image
        #[allow(clippy::indexing_slicing)]
        let rgba: Vec<u8> = vec![
            255, 0, 0, 255, // red
            0, 255, 0, 255, // green
            0, 0, 255, 255, // blue
            255, 255, 255, 255, // white
        ];

        ImZeroFffi::<std::io::BufReader<std::io::Empty>, std::io::Sink>::write_png(
            path_str, &rgba, 2, 2,
        )
        .expect("write_png should succeed");

        // Verify file exists and is a valid PNG
        let file_data = std::fs::read(&path).expect("should read file");
        assert!(file_data.len() > 8, "PNG file should not be empty");
        // PNG magic bytes
        assert_eq!(&file_data[..8], &[137, 80, 78, 71, 13, 10, 26, 10]);

        // Decode and verify dimensions + pixels
        let decoder = png::Decoder::new(std::io::Cursor::new(&file_data));
        let mut reader = decoder.read_info().expect("valid PNG header");
        let info = reader.info();
        assert_eq!(info.width, 2);
        assert_eq!(info.height, 2);
        assert_eq!(info.color_type, png::ColorType::Rgba);
        assert_eq!(info.bit_depth, png::BitDepth::Eight);

        let mut decoded = vec![0u8; reader.output_buffer_size()];
        let frame = reader.next_frame(&mut decoded).expect("decode frame");
        let decoded = &decoded[..frame.buffer_size()];
        assert_eq!(decoded, &rgba[..], "decoded pixels must match input");

        // Cleanup
        std::fs::remove_file(&path).ok();
    }

    #[test]
    fn test_write_png_fails_on_invalid_path() {
        let result = ImZeroFffi::<std::io::BufReader<std::io::Empty>, std::io::Sink>::write_png(
            "/nonexistent/dir/screenshot.png",
            &[0, 0, 0, 255],
            1,
            1,
        );
        assert!(result.is_err());
    }
}
