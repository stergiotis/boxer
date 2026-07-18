package definition

// =============================================================================
// EGUI_GRAPHS binding — register-drain + retained-state pattern
// =============================================================================
//
// Mix of plot's register-drain model (pending Vec for nodes/edges,
// drained inside the graph opcode) with dockArea's retained-state model
// (egui_graphs::Graph<…> owns layout positions, stored in a HashMap
// keyed by the graph widget id). Every frame Go re-declares the full
// set of nodes+edges; the apply code reconciles against the persisted
// state — new entries inserted, vanished entries removed, existing
// entries updated in place so positions users dragged to stay put.
//
// Node identity is Go-assigned u64. A bidirectional u64 ↔ NodeIndex map
// lives inside GraphState so reconcile and edge lookup are O(1). Edges
// are keyed by (from, to); multigraph parallel edges are not supported
// in v1.
//
// Layout is pinned to LayoutRandom because the GraphState is stored in
// a HashMap and so must be a concrete non-generic type; switching layout
// at runtime would require rebuilding the state.
//
// Nodes:  graphNode(id u64, label string) + .Color(rgba u32)
// Edges:  graphEdge(fromId u64, toId u64) + .Color(rgba u32) + .Label(text)
// Drain:  graph(id) + many settings methods (dimensions, interactions,
//         navigation, style)

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

// --- Registered nodes (graph element accumulators) ---

func definitionsGraphRegistered() []*ir.BuilderFactoryNode {
	registered := make([]*ir.BuilderFactoryNode, 0, 2)

	// graphNode — one entry in the next frame's node list.
	registered = append(registered, idl.NewBuilderFactoryNode("graphNode").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("nodeId", ctabb.U64).
			PlainArg("label", ctabb.S).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("color").Arg("col", ctabb.U32).AsColor().
			CodeClientRust(rustClientCode("color = Some(color32_from_rgba_u32(col));\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut color: Option<egui::Color32> = None;
`)).
		WithApplyCodeClientRust(rustClientCode(`self.graph_pending_nodes.push(GraphNodeData { id: node_id, label, color });
`)).
		WithSettingImmediate(true).
		WithReturnType(structGraphNode()).
		Build())

	// graphEdge — one entry in the next frame's edge list, keyed by (from,to).
	registered = append(registered, idl.NewBuilderFactoryNode("graphEdge").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("fromId", ctabb.U64).
			PlainArg("toId", ctabb.U64).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("color").Arg("col", ctabb.U32).AsColor().
			CodeClientRust(rustClientCode("color = Some(color32_from_rgba_u32(col));\n")).EndMethod().
			BeginMethod("label").Arg("text", ctabb.S).
			CodeClientRust(rustClientCode("label = Some(text);\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut color: Option<egui::Color32> = None;
let mut label: Option<String> = None;
`)).
		WithApplyCodeClientRust(rustClientCode(`self.graph_pending_edges.push(GraphEdgeData { from: from_id, to: to_id, label, color });
`)).
		WithSettingImmediate(true).
		WithReturnType(structGraphEdge()).
		Build())

	return registered
}

// --- Drain node (renders egui_graphs GraphView with persisted state) ---

func definitionsGraphBlock() []*ir.BuilderFactoryNode {
	blocks := make([]*ir.BuilderFactoryNode, 0, 1)

	blocks = append(blocks, idl.NewBuilderFactoryNode("graph").
		WithIdentityId(true).
		WithReturnType(structGraphDrain()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("width").Arg("wi", ctabb.F32).
			CodeClientRust(rustClientCode("gv_width = wi;\n")).EndMethod().
			BeginMethod("height").Arg("he", ctabb.F32).
			CodeClientRust(rustClientCode("gv_height = he;\n")).EndMethod().
			BeginMethod("draggingEnabled").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("dragging_enabled = vl;\n")).EndMethod().
			BeginMethod("hoverEnabled").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("hover_enabled = vl;\n")).EndMethod().
			BeginMethod("nodeClickingEnabled").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("node_clicking_enabled = vl;\n")).EndMethod().
			BeginMethod("nodeSelectionEnabled").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("node_selection_enabled = vl;\n")).EndMethod().
			BeginMethod("nodeSelectionMultiEnabled").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("node_selection_multi_enabled = vl;\n")).EndMethod().
			BeginMethod("edgeClickingEnabled").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("edge_clicking_enabled = vl;\n")).EndMethod().
			BeginMethod("edgeSelectionEnabled").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("edge_selection_enabled = vl;\n")).EndMethod().
			BeginMethod("edgeSelectionMultiEnabled").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("edge_selection_multi_enabled = vl;\n")).EndMethod().
			// fitToScreen forces *continuous* fit (re-fit every frame). Off
			// by default. Prefer fitNow() for a one-shot fit — continuous
			// fit fights manual pan/zoom and makes a still-settling
			// force-directed layout visibly "breathe".
			BeginMethod("fitToScreen").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("fit_to_screen = vl;\n")).EndMethod().
			// fitNow requests a single fit pass that re-arms the one-shot
			// latch: the view fits while the layout settles, then latches
			// off so manual pan/zoom sticks. Fire once (e.g. on a button),
			// like resetLayout.
			BeginMethod("fitNow").
			CodeClientRust(rustClientCode("fit_now_flag = true;\n")).EndMethod().
			BeginMethod("zoomAndPan").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("zoom_and_pan = vl;\n")).EndMethod().
			BeginMethod("fitPadding").Arg("pd", ctabb.F32).
			CodeClientRust(rustClientCode("fit_padding = pd;\n")).EndMethod().
			BeginMethod("zoomSpeed").Arg("sp", ctabb.F32).
			CodeClientRust(rustClientCode("zoom_speed = sp;\n")).EndMethod().
			BeginMethod("labelsAlways").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("labels_always = vl;\n")).EndMethod().
			// Layout selector — 0=LayoutRandom (default), 1=ForceDirected
			// (Fruchterman-Reingold), 2=ForceDirected + center gravity,
			// 3=Hierarchical. See GraphLayout* constants in the Go helper.
			// Switching layout at runtime discards the previous layout's
			// positions (different egui-state types occupy the same id slot).
			BeginMethod("layout").Arg("kind", ctabb.U8).
			CodeClientRust(rustClientCode("layout_kind = kind;\n")).EndMethod().
			// Clear this graph's layout state before the next render —
			// nodes get re-placed by the layout algo from scratch. Fires
			// once per call; the user decides whether to emit it every
			// frame (continuous reset) or just once (e.g. on a button).
			BeginMethod("resetLayout").
			CodeClientRust(rustClientCode("reset_layout_flag = true;\n")).EndMethod().
			// Advance the active layout simulation by `steps` iterations
			// before rendering this frame. Useful for force-directed
			// layouts: emit .FastForwardSteps(200) once to pre-warm the
			// graph into a converged shape instead of watching it settle
			// over many frames.
			BeginMethod("fastForwardSteps").Arg("st", ctabb.U32).
			CodeClientRust(rustClientCode("fast_forward_steps = st;\n")).EndMethod().
			// ---- FruchtermanReingold tunables (apply to layout kinds 1 and 2).
			// Each method flips a twin `_set` flag so only the fields the user
			// actually touches this frame are overlaid onto the persisted FR
			// state; untouched fields keep whatever the simulation last wrote.
			// Semantics match FruchtermanReingoldState's public fields.
			BeginMethod("layoutDt").Arg("dt", ctabb.F32).
			CodeClientRust(rustClientCode("fr_dt = dt; fr_dt_set = true;\n")).EndMethod().
			BeginMethod("layoutDamping").Arg("dp", ctabb.F32).
			CodeClientRust(rustClientCode("fr_damping = dp; fr_damping_set = true;\n")).EndMethod().
			BeginMethod("layoutEpsilon").Arg("ep", ctabb.F32).
			CodeClientRust(rustClientCode("fr_epsilon = ep; fr_epsilon_set = true;\n")).EndMethod().
			BeginMethod("layoutMaxStep").Arg("ms", ctabb.F32).
			CodeClientRust(rustClientCode("fr_max_step = ms; fr_max_step_set = true;\n")).EndMethod().
			BeginMethod("layoutKScale").Arg("ks", ctabb.F32).
			CodeClientRust(rustClientCode("fr_k_scale = ks; fr_k_scale_set = true;\n")).EndMethod().
			BeginMethod("layoutCAttract").Arg("ca", ctabb.F32).
			CodeClientRust(rustClientCode("fr_c_attract = ca; fr_c_attract_set = true;\n")).EndMethod().
			BeginMethod("layoutCRepulse").Arg("cr", ctabb.F32).
			CodeClientRust(rustClientCode("fr_c_repulse = cr; fr_c_repulse_set = true;\n")).EndMethod().
			BeginMethod("layoutRunning").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("fr_is_running = vl; fr_is_running_set = true;\n")).EndMethod().
			// ---- Hierarchical tunables (apply to layout kind 3).
			BeginMethod("layoutRowDist").Arg("rd", ctabb.F32).
			CodeClientRust(rustClientCode("hi_row_dist = rd; hi_row_dist_set = true;\n")).EndMethod().
			BeginMethod("layoutColDist").Arg("cd", ctabb.F32).
			CodeClientRust(rustClientCode("hi_col_dist = cd; hi_col_dist_set = true;\n")).EndMethod().
			BeginMethod("layoutCenterParent").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("hi_center_parent = vl; hi_center_parent_set = true;\n")).EndMethod().
			BeginMethod("layoutOrientation").Arg("or", ctabb.U8).
			CodeClientRust(rustClientCode("hi_orientation = or; hi_orientation_set = true;\n")).EndMethod().
			Build()...).
		WithSettingImmediate(true).
		WithSettingRetained(true).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
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
let mut fr_dt: f32 = 0.0; let mut fr_dt_set = false;
let mut fr_damping: f32 = 0.0; let mut fr_damping_set = false;
let mut fr_epsilon: f32 = 0.0; let mut fr_epsilon_set = false;
let mut fr_max_step: f32 = 0.0; let mut fr_max_step_set = false;
let mut fr_k_scale: f32 = 0.0; let mut fr_k_scale_set = false;
let mut fr_c_attract: f32 = 0.0; let mut fr_c_attract_set = false;
let mut fr_c_repulse: f32 = 0.0; let mut fr_c_repulse_set = false;
let mut fr_is_running: bool = false; let mut fr_is_running_set = false;
// Hierarchical tunables (layout kind 3).
let mut hi_row_dist: f32 = 0.0; let mut hi_row_dist_set = false;
let mut hi_col_dist: f32 = 0.0; let mut hi_col_dist_set = false;
let mut hi_center_parent: bool = false; let mut hi_center_parent_set = false;
let mut hi_orientation: u8 = 0; let mut hi_orientation_set = false;
`)).
		WithApplyCodeClientRust(rustClientCode(`
if {{EguiUiOptionalOuter}}.is_some() {
    let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
    let pending_nodes: Vec<GraphNodeData> = self.graph_pending_nodes.drain(..).collect();
    let pending_edges: Vec<GraphEdgeData> = self.graph_pending_edges.drain(..).collect();

    let gid = {{Id}}.value();
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
    let do_fit = graph_fit_this_frame(state, fit_to_screen, fit_now_flag || reset_layout_flag, layout_settled);
    let navigation = egui_graphs::SettingsNavigation::default()
        .with_fit_to_screen_enabled(do_fit)
        .with_zoom_and_pan_enabled(zoom_and_pan)
        .with_fit_to_screen_padding(fit_padding)
        .with_zoom_speed(zoom_speed);
    let style = egui_graphs::SettingsStyle::default().with_labels_always(labels_always);

    // Zero along either axis → fill the container's available space for
    // that axis; non-zero → use the caller-supplied fixed dimension.
    let avail = ui.available_size();
    let size = egui::vec2(
        if gv_width  > 0.0 { gv_width  } else { avail.x },
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
        ui, gid, layout_kind,
        fr_dt, fr_dt_set,
        fr_damping, fr_damping_set,
        fr_epsilon, fr_epsilon_set,
        fr_max_step, fr_max_step_set,
        fr_k_scale, fr_k_scale_set,
        fr_c_attract, fr_c_attract_set,
        fr_c_repulse, fr_c_repulse_set,
        fr_is_running, fr_is_running_set,
    );
    apply_hierarchical_overrides(
        ui, gid, layout_kind,
        hi_row_dist, hi_row_dist_set,
        hi_col_dist, hi_col_dist_set,
        hi_center_parent, hi_center_parent_set,
        hi_orientation, hi_orientation_set,
    );
    let graph_resp = render_graph_with_layout(
        state, ui, size, gid, layout_kind,
        reset_layout_flag, fast_forward_steps,
        &interaction, &navigation, &style, &sink,
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
        gid, state,
        &mut self.graph_selection_graph_ids,
        &mut self.graph_selection_kind,
        &mut self.graph_selection_key_a,
        &mut self.graph_selection_key_b,
    );
    snapshot_graph_metrics(
        gid, layout_kind, state, ui,
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
`)).
		Build())

	return blocks
}
