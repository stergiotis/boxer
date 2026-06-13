package definition

// =============================================================================
// EGUI_DOCK binding — tabbed docking with library-owned layout state
// =============================================================================
//
// Single factory node: dockArea.
//   - Identity id    — names the dock area; layout state is keyed by this.
//   - PlainArg tabIds:    []u64   — Go-assigned stable tab identifiers.
//   - PlainArg tabTitles: []string — one title per tab id (same length).
//   - DeferredBlockMap("tabBody", u64) — one opcode body per tab id.
//
// State ownership:
//   - Rust persists the `DockState<u64>` across frames (split ratios, active
//     tab per group, drag-to-reorder). Stored on ImZeroFffi.dock_states,
//     keyed by the dock-area id.
//   - Go is authoritative about which tabs EXIST. Each frame Go sends the
//     full current tab id list; the apply code reconciles stored state via
//     retain_tabs (drop stale) + push_to_first_leaf (add new).
//   - Tabs are `closeable=false` on the TabViewer side so the library never
//     silently removes a tab — keeping Go as the single source of truth for
//     existence is what makes the reconciliation correct.
//
// Composition:
//   - A tab body is a deferred block. Anything that normally writes opcodes
//     via `.Send()` (kept holders, block iterators, other deferred widgets
//     like etable/plot) works unchanged inside a tab body closure. The
//     etable inside a tab is the key test: its own deferred block maps
//     travel inside the tab body bytes and the interpreter reads them
//     recursively during replay.

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

func definitionsDock() []*ir.BuilderFactoryNode {
	blocks := make([]*ir.BuilderFactoryNode, 0, 1)
	// The IDL node is named "dockAreaRaw" so the primary user-facing name
	// DockArea stays free for the hand-written iter-style wrapper in
	// egui2_methods.go, where (id, title) are grouped as iter-helper args
	// alongside the for-range body.
	blocks = append(blocks, idl.NewBuilderFactoryNode("dockAreaRaw").
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("tabIds", ctabb.U64h).
			PlainArg("tabTitles", ctabb.Sh).
			PlainArg("initialLayout", ctabb.U8h).
			Build()).
		WithDeferredBlockMap("tabBody", ctabb.U64).
		WithSettingImmediate(true).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`
	let bodies = self.io.read_deferred_block_map_u64()?;
	if {{EguiUiOptionalOuter}}.is_some() {
		use std::collections::HashSet;

		// Build title lookup. tab_ids and tab_titles have matching length by
		// construction (DockAreaBuilder.Send enforces this on the Go side).
		let mut titles: std::collections::HashMap<u64, String> =
			std::collections::HashMap::with_capacity(tab_ids.len());
		for (id, t) in tab_ids.iter().copied().zip(tab_titles.into_iter()) {
			titles.insert(id, t);
		}

		// Take DockState out of the map so the subsequent TabViewer can hold
		// &mut self.interpreter without aliasing the HashMap entry. When
		// the dock area is first seen (no entry yet), parse the Go-side
		// initialLayout descriptor to build the desired split tree; on
		// subsequent frames the stored DockState wins so user drag/drop
		// changes are preserved. Empty initialLayout falls back to the
		// "everything in one leaf" default.
		let area_id = {{Id}}.value();
		let mut dock_state = self.dock_states.remove(&area_id).unwrap_or_else(|| {
			parse_dock_initial_layout(&initial_layout, &tab_ids)
		});

		// Reconcile stored layout with Go's authoritative tab list.
		let wanted: HashSet<u64> = tab_ids.iter().copied().collect();
		dock_state.retain_tabs(|t| wanted.contains(t));
		let existing: HashSet<u64> = dock_state.iter_all_tabs().map(|(_, t)| *t).collect();
		for &id in &tab_ids {
			if !existing.contains(&id) {
				dock_state.push_to_first_leaf(id);
			}
		}

		let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
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
			};
			egui_dock::DockArea::new(&mut dock_state).show_inside(child_ui, &mut viewer);
		});

		self.dock_states.insert(area_id, dock_state);
	}
`)).
		WithReturnType(structDockAreaDummy()).
		Build())
	return blocks
}
