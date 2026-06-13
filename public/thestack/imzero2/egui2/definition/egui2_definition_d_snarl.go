package definition

// =============================================================================
// EGUI-SNARL binding — node-editor with Go-authoritative topology
// =============================================================================
//
// ADR-0021. Mixes dock's deferred-block-map model (per-node body keyed by
// u64 node id) with graphs' register-drain accumulators + events fetcher
// (snarlNode / snarlConnection / snarlPin pushed each frame; user edits
// drained via fetchSnarlEvents).
//
// Position authority is Go by default — every frame the apply method
// copies x/y from the supplied snarlNode entries into the retained
// `Snarl<u64>`. User drags surface as NodeMoved events for Go to consume
// and re-emit. Opt-in Rust-persisted layout via .PersistPositions(true)
// on the editor: when set, Go-supplied positions are honoured only on
// first insertion of a given node id, subsequent pushes leave (x,y)
// alone (kind / pin updates still applied).
//
// Three accumulator nodes:
//   snarlNode(nodeId, x, y, kind, title) + .NumInputs/.NumOutputs
//   snarlConnection(srcNode, srcPort, dstNode, dstPort)
//   snarlPin(nodeId, side, pinIdx, label, kind)        — pin metadata
//
// One drain widget:
//   snarlEditor(id) + .Width/.Height/.PersistPositions/.WireStyle/...
//                   + DeferredBlockMap("nodeBody", u64)
//
// One fetcher:
//   fetchSnarlEvents — eight parallel homogeneous arrays. Event-kind
//   constants (SNARL_EV_*) live in the high-level Go helper alongside
//   the GraphEv* set.
//
// Identity model:
//   - Node ids on the wire are Go-assigned u64. Snarl's internal NodeId
//     (a wrapper around usize) is never exposed; the apply method holds
//     a bidirectional map u64 ↔ NodeId on SnarlState so reconcile and
//     event translation are O(1).
//   - Pin ids on the wire are (nodeId u64, side u8 [0=in, 1=out],
//     pinIdx u32). Multi-input is permitted on the Snarl side; v1 wire
//     does not carry multi-input ordering hints — Snarl's own ordering
//     is exposed verbatim in events.
//
// Pin rendering at M1:
//   - Pin labels and per-pin colour `kind` come from snarlPin entries.
//     The viewer's show_input/show_output draws PinInfo (default circle
//     coloured by kind) plus the pin label as a ui.label. Custom pin
//     bodies (icons, inline widgets, dropdowns) are deferred; SD3 in
//     ADR-0021 admits them additively via a second deferred-block map
//     keyed by (nodeId, side, pinIdx) once a real consumer needs them.

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

// --- Registered nodes (snarl element accumulators) ---

func definitionsSnarlRegistered() []*ir.BuilderFactoryNode {
	registered := make([]*ir.BuilderFactoryNode, 0, 3)

	// snarlNode — one entry in the next frame's node list.
	//
	// (x,y) is the desired screen-space position of the node's top-left
	// corner. In default mode (.PersistPositions(false) on the editor)
	// every frame's value overwrites the retained position. In opt-in
	// persistence mode, (x,y) is honoured only on first insertion.
	//
	// `kind` is an opaque u32 the viewer uses to colour-code the node /
	// its pins; semantics are the consumer's. `title` is rendered in the
	// node's header by default; an optional nodeBody deferred-block (see
	// snarlEditor) renders below the pins.
	//
	// .NumInputs / .NumOutputs declare the pin count; the viewer's
	// inputs() / outputs() impls read these from a per-node table on
	// FffiSnarlViewer. Pin labels + kinds come from snarlPin entries.
	registered = append(registered, idl.NewBuilderFactoryNode("snarlNode").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("nodeId", ctabb.U64).
			PlainArg("posX", ctabb.F32).
			PlainArg("posY", ctabb.F32).
			PlainArg("kind", ctabb.U32).
			PlainArg("title", ctabb.S).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("numInputs").Arg("ni", ctabb.U32).
			CodeClientRust(rustClientCode("num_inputs = ni;\n")).EndMethod().
			BeginMethod("numOutputs").Arg("no", ctabb.U32).
			CodeClientRust(rustClientCode("num_outputs = no;\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut num_inputs: u32 = 0;
let mut num_outputs: u32 = 0;
`)).
		WithApplyCodeClientRust(rustClientCode(`self.snarl_pending_nodes.push(SnarlNodeData {
    id: node_id, x: pos_x, y: pos_y, kind, title,
    num_inputs, num_outputs,
});
`)).
		WithSettingImmediate(true).
		WithReturnType(structSnarlNode()).
		Build())

	// snarlConnection — one wire in the next frame's connection list.
	// Direction is source-output → destination-input. Duplicate
	// connections (same (src,dst) tuple) are silently deduplicated by
	// Snarl on the connect side.
	registered = append(registered, idl.NewBuilderFactoryNode("snarlConnection").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("srcNodeId", ctabb.U64).
			PlainArg("srcPort", ctabb.U32).
			PlainArg("dstNodeId", ctabb.U64).
			PlainArg("dstPort", ctabb.U32).
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.snarl_pending_connections.push(SnarlConnectionData {
    src_node: src_node_id, src_port,
    dst_node: dst_node_id, dst_port,
});
`)).
		WithSettingImmediate(true).
		WithReturnType(structSnarlConnection()).
		Build())

	// snarlPin — one pin metadata entry. side: 0=input, 1=output.
	// Pins outside the range declared by snarlNode.NumInputs /
	// .NumOutputs are silently ignored at apply time. `kind` colours the
	// default PinInfo circle; `label` is drawn next to it.
	registered = append(registered, idl.NewBuilderFactoryNode("snarlPin").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("nodeId", ctabb.U64).
			PlainArg("side", ctabb.U8).
			PlainArg("pinIdx", ctabb.U32).
			PlainArg("label", ctabb.S).
			PlainArg("kind", ctabb.U32).
			Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.snarl_pending_pins.push(SnarlPinData {
    node_id, side, pin_idx, label, kind,
});
`)).
		WithSettingImmediate(true).
		WithReturnType(structSnarlPin()).
		Build())

	return registered
}

// --- Drain node (renders egui-snarl with persisted state) ---

func definitionsSnarlBlock() []*ir.BuilderFactoryNode {
	blocks := make([]*ir.BuilderFactoryNode, 0, 1)

	blocks = append(blocks, idl.NewBuilderFactoryNode("snarlEditor").
		WithIdentityId(true).
		WithReturnType(structSnarlEditor()).
		AddArguments(idl.NewArgumentsBuilder().Build()).
		WithDeferredBlockMap("nodeBody", ctabb.U64).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("width").Arg("wi", ctabb.F32).
			CodeClientRust(rustClientCode("ed_width = wi;\n")).EndMethod().
			BeginMethod("height").Arg("he", ctabb.F32).
			CodeClientRust(rustClientCode("ed_height = he;\n")).EndMethod().
			// SD6 — Position authority. Default (false): every frame's
			// snarlNode (x,y) is copied into the retained Snarl<u64>;
			// drags fire NodeMoved events for Go to round-trip. Opt-in
			// (true): Go-supplied (x,y) is honoured only on first
			// insertion of a given node id; Rust-side drags persist
			// across frames without Go round-trip.
			BeginMethod("persistPositions").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("persist_positions = vl;\n")).EndMethod().
			// 0 = Bezier5 (default), 1 = AxisAligned, 2 = Bezier3,
			// 3 = Line. Maps onto egui_snarl::ui::WireStyle.
			BeginMethod("wireStyle").Arg("ws", ctabb.U8).
			CodeClientRust(rustClientCode("wire_style = ws;\n")).EndMethod().
			// 0 = none, 1 = grid (default). Maps onto
			// egui_snarl::ui::BackgroundPattern.
			BeginMethod("bgPattern").Arg("bp", ctabb.U8).
			CodeClientRust(rustClientCode("bg_pattern = bp;\n")).EndMethod().
			// Min/Max viewport zoom scale. 0 (default) leaves
			// SnarlStyle defaults in place (0.2 .. 2.0).
			BeginMethod("minScale").Arg("ms", ctabb.F32).
			CodeClientRust(rustClientCode("min_scale = ms;\n")).EndMethod().
			BeginMethod("maxScale").Arg("ms", ctabb.F32).
			CodeClientRust(rustClientCode("max_scale = ms;\n")).EndMethod().
			// Enable double-click-on-background to recenter the
			// viewport on the bounding box of all nodes. Default
			// true (matches SnarlStyle default).
			BeginMethod("centering").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("centering = vl;\n")).EndMethod().
			// Pre-scale UI style by max_scale and collapse Scene zoom
			// range to [min/max, 1.0]. Trades the ability to zoom past
			// 1.0x for sharp text at every visible zoom level (font
			// atlas is rasterised at the larger size, then downsampled
			// rather than upsampled). Default false.
			BeginMethod("crispMagnifiedText").Arg("vl", ctabb.B).
			CodeClientRust(rustClientCode("crisp_magnified_text = vl;\n")).EndMethod().
			Build()...).
		WithSettingImmediate(true).
		WithSettingRetained(true).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
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
`)).
		WithApplyCodeClientRust(rustClientCode(`
let bodies = self.io.read_deferred_block_map_u64()?;
if {{EguiUiOptionalOuter}}.is_some() {
    let editor_id = {{Id}}.value();
    let ctx_cloned = {{EguiUiOptionalOuter}}.as_ref().unwrap().ctx().clone();
    let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
    let _ = self.render_snarl_editor(
        editor_id, &ctx_cloned, ui, bodies,
        ed_width, ed_height,
        persist_positions, wire_style, bg_pattern,
        min_scale, max_scale, centering, crisp_magnified_text,
    );
} else {
    // Drain-on-cull (ADR-0012): clear pending accumulators so a culled
    // editor doesn't leak its frame's nodes/connections/pins into the
    // next live editor.
    self.snarl_pending_nodes.clear();
    self.snarl_pending_connections.clear();
    self.snarl_pending_pins.clear();
}
`)).
		Build())

	return blocks
}

// --- Fetcher (user edit events drained by Go) ---

func definitionsSnarlFetchers() []ir.NodeI {
	fetchers := make([]ir.NodeI, 0, 1)

	// fetchSnarlEvents — drains user-edit events generated since the last
	// fetch. Eight parallel homogeneous arrays, all of equal length —
	// one "row" per event. `kinds` is one of the SNARL_EV_* constants
	// (1..=7) defined in the high-level Go helper:
	//
	//   1 NodeMoved          nodeIds[i] = node id
	//                        xs[i],ys[i] = new position
	//   2 NodeRemoved        nodeIds[i] = node id  (set sent only when
	//                        the user invoked a context-menu remove —
	//                        Go-side authoritative removes don't echo)
	//   3 ConnectionAdded    nodeIds[i] = src node id, portsA[i] = src port
	//                        nodeIdsB[i] = dst node id, portsB[i] = dst port
	//   4 ConnectionRemoved  same shape as 3
	//   5 NodeSelected       nodeIds[i] = node id
	//   6 NodeDeselected     nodeIds[i] = node id
	//   7 NodeOpenChanged    nodeIds[i] = node id; portsA[i] = 0/1 (open flag)
	//
	// Unused fields per kind are 0 / NaN. editorIds carries the editor
	// the event came from so a Go consumer with multiple snarlEditors in
	// the same frame can dispatch correctly.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchSnarlEvents").
		WithApplyCodeClientRust(rustClientCode(`
let len = self.snarl_events_pending.len();
let editor_ids: Vec<u64> = self.snarl_events_pending.iter().map(|r| r.editor_id).collect();
let kinds:      Vec<u32> = self.snarl_events_pending.iter().map(|r| r.kind as u32).collect();
let node_ids:   Vec<u64> = self.snarl_events_pending.iter().map(|r| r.node_id).collect();
let ports_a:    Vec<u32> = self.snarl_events_pending.iter().map(|r| r.port_a).collect();
let node_ids_b: Vec<u64> = self.snarl_events_pending.iter().map(|r| r.node_id_b).collect();
let ports_b:    Vec<u32> = self.snarl_events_pending.iter().map(|r| r.port_b).collect();
let xs:         Vec<f32> = self.snarl_events_pending.iter().map(|r| r.x).collect();
let ys:         Vec<f32> = self.snarl_events_pending.iter().map(|r| r.y).collect();
self.snarl_events_pending.clear();
self.io.write_plain_u64h(len, editor_ids)?;
self.io.write_plain_u32h(len, kinds)?;
self.io.write_plain_u64h(len, node_ids)?;
self.io.write_plain_u32h(len, ports_a)?;
self.io.write_plain_u64h(len, node_ids_b)?;
self.io.write_plain_u32h(len, ports_b)?;
self.io.write_plain_f32h(len, xs)?;
self.io.write_plain_f32h(len, ys)?;
{{SendMessage}}
`)).
		AddReturnValue("editorIds", ctabb.U64h).
		AddReturnValue("kinds", ctabb.U32h).
		AddReturnValue("nodeIds", ctabb.U64h).
		AddReturnValue("portsA", ctabb.U32h).
		AddReturnValue("nodeIdsB", ctabb.U64h).
		AddReturnValue("portsB", ctabb.U32h).
		AddReturnValue("xs", ctabb.F32h).
		AddReturnValue("ys", ctabb.F32h).
		Build())

	return fetchers
}
