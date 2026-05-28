//go:build llm_generated_opus47

package definition

import (
	"fmt"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

func definitionsFetcher() (fetchers []ir.NodeI) {
	fetchers = make([]ir.NodeI, 0, 16)
	fetchers = append(fetchers, idl.NewFetcherNode("fetchR7").
		WithApplyCodeClientRust(rustClientCode(`
let len = self.r7_ids.len();
self.io.write_plain_u64h(len, self.r7_ids.drain(..))?;
self.io.write_plain_u32h(len, self.r7_responses.drain(..).map(|c| { c.bits() }))?;
{{SendMessage}}
`)).
		AddReturnValue("ids", ctabb.U64h).
		AddReturnValue("responses", ctabb.U32h).
		Build())

	{
		p := canonicaltypes.NewParser()
		v := append([]string{"s"}, r9Types...)
		for _, f := range v {
			ff := naming.MustBeValidStylableName(f)
			c := p.MustParsePrimitiveTypeAst(f)
			ch, _, _ := canonicaltypes.PromoteScalars(c, canonicaltypes.ScalarModifierHomogenousArray)
			fetchers = append(fetchers, idl.NewFetcherNode(naming.StylableName("fetchR9"+ff.Convert(naming.UpperCamelCase).String())).
				WithApplyCodeClientRust(rustClientCode(fmt.Sprintf(`
let len = self.r9_%s_ids.len();
self.io.write_plain_u64h(len, self.r9_%s_ids.drain(..))?;
self.io.write_plain_%sh(len, self.r9_%s_values.drain(..))?;
{{SendMessage}}
`, f, f, f, f))).
				AddReturnValue("ids", ctabb.U64h).
				AddReturnValue("values", ch.(canonicaltypes.PrimitiveAstNodeI)).
				Build())
		}
	}
	// egui_table prefetch feedback: per-table visible (row, col) ranges plus
	// num_sticky_columns (sticky cols are always visible regardless of the
	// scrolled range). 5 packed u64 values per id: rowBegin, rowEnd,
	// colBegin, colEnd, numStickyCols.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchR9EtPrefetch").
		WithApplyCodeClientRust(rustClientCode(`
let len = self.r9_et_prefetch_ids.len();
self.io.write_plain_u64h(len, self.r9_et_prefetch_ids.drain(..))?;
self.io.write_plain_u64h(len * 5, self.r9_et_prefetch_values.drain(..))?;
{{SendMessage}}
`)).
		AddReturnValue("ids", ctabb.U64h).
		AddReturnValue("values", ctabb.U64h).
		Build())

	fetchers = append(fetchers, idl.NewFetcherNode("fetchR10").
		WithApplyCodeClientRust(rustClientCode(`
self.io.write_plain_u64h(self.r10_true_ids.len(), self.r10_true_ids.drain(..))?;
self.io.write_plain_u64h(self.r10_false_ids.len(), self.r10_false_ids.drain(..))?;
{{SendMessage}}
`)).
		AddReturnValue("idsTrue", ctabb.U64h).
		AddReturnValue("idsFalse", ctabb.U64h).
		Build())

	// egui_graphs interaction events drained from the event sink installed
	// on each GraphView. Four parallel homogeneous arrays, all of equal
	// length — one "row" per event. `kinds` is one of the GRAPH_EV_*
	// constants (1..=11); `keyA`/`keyB` carry the Go-side u64 identifiers:
	//   * node events (1..=8): keyA = node id, keyB = 0
	//   * edge events (9..=11): keyA = fromId, keyB = toId
	// Pan/Zoom/NodeMove are intentionally dropped in v1 — they're per-
	// frame streams, not the useful subset.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchGraphEvents").
		WithApplyCodeClientRust(rustClientCode(`
let len = self.graph_events_pending.len();
let graph_ids: Vec<u64> = self.graph_events_pending.iter().map(|r| r.graph_id).collect();
let kinds:     Vec<u32> = self.graph_events_pending.iter().map(|r| r.kind as u32).collect();
let key_a:     Vec<u64> = self.graph_events_pending.iter().map(|r| r.key_a).collect();
let key_b:     Vec<u64> = self.graph_events_pending.iter().map(|r| r.key_b).collect();
self.graph_events_pending.clear();
self.io.write_plain_u64h(len, graph_ids)?;
self.io.write_plain_u32h(len, kinds)?;
self.io.write_plain_u64h(len, key_a)?;
self.io.write_plain_u64h(len, key_b)?;
{{SendMessage}}
`)).
		AddReturnValue("graphIds", ctabb.U64h).
		AddReturnValue("kinds", ctabb.U32h).
		AddReturnValue("keyA", ctabb.U64h).
		AddReturnValue("keyB", ctabb.U64h).
		Build())

	// Current per-graph selection snapshot — one entry per selected node
	// or edge. `kind` is 0 for a node (keyA = node id, keyB = 0) or 1 for
	// an edge (keyA = from, keyB = to). Rebuilt from scratch every frame
	// by snapshot_graph_selection.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchGraphSelection").
		WithApplyCodeClientRust(rustClientCode(`
let len = self.graph_selection_graph_ids.len();
self.io.write_plain_u64h(len, self.graph_selection_graph_ids.drain(..))?;
self.io.write_plain_u32h(len, self.graph_selection_kind.drain(..).map(|k| k as u32))?;
self.io.write_plain_u64h(len, self.graph_selection_key_a.drain(..))?;
self.io.write_plain_u64h(len, self.graph_selection_key_b.drain(..))?;
{{SendMessage}}
`)).
		AddReturnValue("graphIds", ctabb.U64h).
		AddReturnValue("kinds", ctabb.U32h).
		AddReturnValue("keyA", ctabb.U64h).
		AddReturnValue("keyB", ctabb.U64h).
		Build())

	// Per-graph metrics snapshot — one entry per graph widget rendered
	// this frame. Node/edge counts come from the StableGraph directly;
	// frStepCount and frLastDisp are non-zero/non-NaN only for graphs
	// whose layout is FR or FR+CG.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchGraphMetrics").
		WithApplyCodeClientRust(rustClientCode(`
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
{{SendMessage}}
`)).
		AddReturnValue("graphIds", ctabb.U64h).
		AddReturnValue("nodeCount", ctabb.U32h).
		AddReturnValue("edgeCount", ctabb.U32h).
		AddReturnValue("frSteps", ctabb.U64h).
		AddReturnValue("frLastDisp", ctabb.F32h).
		Build())

	// fetchFrameMetrics — returns Rust-measured per-frame timing for the
	// previous interpret_commands_outer call. Reported in microseconds with
	// one-frame display lag, since this fetcher executes inside the very
	// frame whose timing it cannot yet observe. Pair with Go-side render +
	// sync stamps to attribute the 16.6 ms vsync budget across (a) Go widget
	// code, (b) Rust interpret + paint, (c) vsync slack waiting for the next
	// frame boundary in continuous-rendering mode.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchFrameMetrics").
		WithApplyCodeClientRust(rustClientCode(`
self.io.write_plain_u64(self.last_interpret_us as u64)?;
self.io.write_plain_u64(self.last_pass_nr)?;
{{SendMessage}}
`)).
		AddReturnValue("interpretUs", ctabb.U64).
		AddReturnValue("passNr", ctabb.U64).
		Build())

	// Canvas pointer state — returns hover position (relative to canvas origin)
	// and clicked flag from the last PaintCanvas.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchR14CanvasPointer").
		WithApplyCodeClientRust(rustClientCode(`
self.io.write_plain_f32(self.r14_canvas_hover_x)?;
self.io.write_plain_f32(self.r14_canvas_hover_y)?;
self.io.write_plain_b(self.r14_canvas_clicked)?;
{{SendMessage}}
`)).
		AddReturnValue("hoverX", ctabb.F32).
		AddReturnValue("hoverY", ctabb.F32).
		AddReturnValue("clicked", ctabb.B).
		Build())

	// egui_plot click+hover pointer state — returns the most recent in-plot
	// primary click in plot-data coordinates, plus the plot widget id so
	// the caller can ignore stale clicks from a different plot. The hover
	// trio (hoverPlotId, hoverX, hoverY) is a non-consuming companion:
	// hoverX/hoverY are NaN when no plot is currently hovered. Click is
	// single-slot (drained on read); hover persists until the next plot
	// render flips it. One-frame lag (event happened during the previous
	// frame's plot.show); the pattern matches r7 / r10 / r14.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchR15PlotPointer").
		WithApplyCodeClientRust(rustClientCode(`
self.io.write_plain_u64(self.r15_plot_clicked_id)?;
self.io.write_plain_f64(self.r15_plot_clicked_x)?;
self.io.write_plain_f64(self.r15_plot_clicked_y)?;
self.io.write_plain_b(self.r15_plot_clicked)?;
self.io.write_plain_u64(self.r15_plot_hover_id)?;
self.io.write_plain_f64(self.r15_plot_hover_x)?;
self.io.write_plain_f64(self.r15_plot_hover_y)?;
self.r15_plot_clicked = false;
{{SendMessage}}
`)).
		AddReturnValue("plotId", ctabb.U64).
		AddReturnValue("x", ctabb.F64).
		AddReturnValue("y", ctabb.F64).
		AddReturnValue("clicked", ctabb.B).
		AddReturnValue("hoverPlotId", ctabb.U64).
		AddReturnValue("hoverX", ctabb.F64).
		AddReturnValue("hoverY", ctabb.F64).
		Build())

	// Smoothed scroll-wheel delta for the current frame, read directly from
	// egui's InputState. Use for pan / zoom gestures inside custom-drawn
	// canvases. Values are in egui's logical pixel space (positive Y =
	// scroll up, positive X = scroll right). No struct field needed —
	// egui's InputState already retains the smoothed value per frame.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchR16ScrollDelta").
		WithApplyCodeClientRust(rustClientCode(`
let d = {{EguiContext}}.input(|i| i.smooth_scroll_delta);
self.io.write_plain_f32(d.x)?;
self.io.write_plain_f32(d.y)?;
{{SendMessage}}
`)).
		AddReturnValue("x", ctabb.F32).
		AddReturnValue("y", ctabb.F32).
		Build())

	// Modifier-key state for the current frame, read directly from egui's
	// InputState. `command` is the platform-native primary modifier (Cmd on
	// macOS, Ctrl elsewhere); use it for shortcuts that follow OS convention.
	// `ctrl` and `mac_cmd` are the raw physical keys — prefer `command` when
	// you don't need to distinguish.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchR17Modifiers").
		WithApplyCodeClientRust(rustClientCode(`
let m = {{EguiContext}}.input(|i| i.modifiers);
self.io.write_plain_b(m.alt)?;
self.io.write_plain_b(m.ctrl)?;
self.io.write_plain_b(m.shift)?;
self.io.write_plain_b(m.mac_cmd)?;
self.io.write_plain_b(m.command)?;
{{SendMessage}}
`)).
		AddReturnValue("alt", ctabb.B).
		AddReturnValue("ctrl", ctabb.B).
		AddReturnValue("shift", ctabb.B).
		AddReturnValue("macCmd", ctabb.B).
		AddReturnValue("command", ctabb.B).
		Build())

	// Last captured ui.available_size — written by the captureAvailableSize
	// procedural op when called inside a Ui scope. One-frame lag relative
	// to the capture call (same pattern as r14 canvas-pointer). NaN means
	// no capture has occurred yet or the capture ran outside any Ui.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchR18AvailableSize").
		WithApplyCodeClientRust(rustClientCode(`
self.io.write_plain_f32(self.r18_avail_w)?;
self.io.write_plain_f32(self.r18_avail_h)?;
{{SendMessage}}
`)).
		AddReturnValue("w", ctabb.F32).
		AddReturnValue("h", ctabb.F32).
		Build())

	// Multiplicative zoom factor for the current frame — egui intercepts
	// Ctrl+scroll AND touchpad pinch AND keyboard +/- and exposes them
	// uniformly here. 1.0 = no zoom; >1.0 = zoom in; <1.0 = zoom out.
	// Prefer this over reading scroll_delta + checking modifiers because
	// Ctrl+scroll is consumed by egui before reaching smooth_scroll_delta.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchR19ZoomDelta").
		WithApplyCodeClientRust(rustClientCode(`
let z = {{EguiContext}}.input(|i| i.zoom_delta());
self.io.write_plain_f32(z)?;
{{SendMessage}}
`)).
		AddReturnValue("zoom", ctabb.F32).
		Build())

	// Drains the per-frame batch of ui.min_rect() snapshots stamped by
	// captureUiRect, keyed by the caller-supplied seq. Five homogeneous
	// arrays of equal length — one row per capture in registration order.
	// Empty when no capture op fired (the first frame, or a frame whose
	// Render skipped every captureUiRect call). Used by the bezier-
	// connector affordance to convert chip & inspector-window viewport
	// rects into the coords the absolute-overlay painter expects.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchR21UiRects").
		WithApplyCodeClientRust(rustClientCode(`
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
{{SendMessage}}
`)).
		AddReturnValue("seqs", ctabb.U64h).
		AddReturnValue("minX", ctabb.F32h).
		AddReturnValue("minY", ctabb.F32h).
		AddReturnValue("maxX", ctabb.F32h).
		AddReturnValue("maxY", ctabb.F32h).
		Build())

	// Latest known pointer position in egui logical pixels (viewport-
	// relative top-left origin), read from egui's InputState. `valid`
	// is false until the pointer has been observed at least once
	// (headless runs, first frame on a freshly-opened viewport); x/y
	// are NaN in that case. Use for click-anchored popups, contextual
	// menus, hover overlays — any "open near the pointer" affordance
	// that doesn't have a canvas / plot to anchor to. Mirrors r17 /
	// r19's "read directly from InputState, no stored field" pattern.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchR20Pointer").
		WithApplyCodeClientRust(rustClientCode(`
let pos = {{EguiContext}}.input(|i| i.pointer.latest_pos());
let (px, py, valid) = match pos {
    Some(p) => (p.x, p.y, true),
    None    => (f32::NAN, f32::NAN, false),
};
self.io.write_plain_f32(px)?;
self.io.write_plain_f32(py)?;
self.io.write_plain_b(valid)?;
{{SendMessage}}
`)).
		AddReturnValue("x", ctabb.F32).
		AddReturnValue("y", ctabb.F32).
		AddReturnValue("valid", ctabb.B).
		Build())

	// fetchF1KeyPressed — drains the per-frame F1 press event from
	// egui's input queue. Returns true exactly once per physical press
	// (consume_key removes the event so other widgets in the same
	// frame don't also see it) with no modifier requirement; Shift+F1,
	// Ctrl+F1, etc. are deliberately ignored, since they conventionally
	// belong to other shortcuts. The carousel polls this fetcher every
	// frame and opens HelpHost on a true result — a single hardcoded
	// binding rather than a parametric "any key" fetcher, since the
	// help shortcut is the only global keyboard affordance the runtime
	// owns today. Future runtime-level shortcuts (debugger, command
	// palette) would each add their own fetcher to keep the consumed-
	// event ownership explicit per binding.
	fetchers = append(fetchers, idl.NewFetcherNode("fetchF1KeyPressed").
		WithApplyCodeClientRust(rustClientCode(`
let pressed = {{EguiContext}}.input_mut(|i| i.consume_key(egui::Modifiers::NONE, egui::Key::F1));
self.io.write_plain_b(pressed)?;
{{SendMessage}}
`)).
		AddReturnValue("pressed", ctabb.B).
		Build())

	return
}
