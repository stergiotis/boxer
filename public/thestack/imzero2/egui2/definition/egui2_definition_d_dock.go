package definition

// =============================================================================
// EGUI_DOCK binding — tabbed docking with library-owned layout state
// =============================================================================
//
// Single factory node: dockArea.
//   - Identity id    — names the dock area; layout state is keyed by this.
//   - PlainArg tabIds:    []u64   — Go-assigned stable tab identifiers.
//   - PlainArg tabTitles: []string — one title per tab id (same length).
//   - PlainArg initialLayout: []u8 — the declared layout: the first frame
//     builds the dock from it, later frames place a tab that appears.
//   - PlainArg noScrollTabIds: []u64 — tabs whose body ScrollArea is
//     disabled on both axes (body overflow clips instead of scrolling).
//   - PlainArg activateTabId: u64 — a tab brought to the front this frame.
//   - PlainArg closableTabIds: []u64 — tabs that carry egui_dock's close
//     button; a close is reported, not carried out (below).
//   - PlainArg bareTabIds: []u64 — tabs drawn without a tab strip while
//     each is alone in its leaf.
//   - DeferredBlockMap("tabBody", u64) — one opcode body per tab id.
//
// State ownership:
//   - Rust persists the `DockState<u64>` across frames (split ratios, active
//     tab per group, drag-to-reorder). Stored on ImZeroFffi.dock_states,
//     keyed by the dock-area id.
//   - Go is authoritative about which tabs EXIST. Each frame Go sends the
//     full current tab id list and the declared layout; the apply code
//     reconciles stored state with reconcile_dock_tabs: a stale tab is
//     removed, a tab that appears goes to the leaf it was declared in, and
//     that leaf's split is made again when it is gone (ADR-0404 §SD8).
//   - The library never removes a tab on its own. A tab in closableTabIds
//     shows the close button; pressing it (or the tab's context menu Close,
//     or a middle click) keeps the tab in the layout and raises
//     SHOULD_CLOSE in r7 under dock_tab_close_id(dock id, tab id), which Go
//     reads one frame later as DockAreaFluid.CloseRequested. The app then
//     stops declaring the tab, and the reconciliation removes it. Go stays
//     the single source of truth for which tabs exist.
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
			PlainArg("noScrollTabIds", ctabb.U64h).
			PlainArg("activateTabId", ctabb.U64).
			PlainArg("closableTabIds", ctabb.U64h).
			PlainArg("bareTabIds", ctabb.U64h).
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

		// Tabs whose body must not be wrapped in a live ScrollArea (both axes
		// off — overflow clips). Viewport-style bodies (a slippy map, canvases)
		// read wheel/zoom input globally without consuming it, so the default
		// per-tab ScrollArea would scroll the panel in the same gesture that
		// pans/zooms the widget content.
		let no_scroll: HashSet<u64> = no_scroll_tab_ids.iter().copied().collect();
		let closable: HashSet<u64> = closable_tab_ids.iter().copied().collect();
		let bare: HashSet<u64> = bare_tab_ids.iter().copied().collect();

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

		// Reconcile stored layout with Go's authoritative tab list: a tab
		// that appears goes to its declared leaf (interpreter/dock_layout.rs,
		// ADR-0404 §SD8).
		reconcile_dock_tabs(&mut dock_state, &tab_ids, &initial_layout);

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

		// A bare tab alone in its leaf is drawn without the leaf's tab strip;
		// every other leaf shows its strip. Set every frame, so a tab dragged
		// in beside a bare one brings the strip back, and dragged out again
		// takes it away (interpreter/dock_layout.rs).
		let any_bare = !bare.is_empty();
		if any_bare {
			hide_bare_tab_bars(&mut dock_state, &bare);
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
		//
		// That bounds the bottom edge only. Scrolled so the dock's top passes
		// above the clip, the leaves still painted — and took clicks — over
		// whatever sits above the scroll area. So the dock itself is shown in
		// the visible part of the allocation only, its tab strip held at the
		// clip's top edge while the rest scrolls away, and the full
		// allocation is still reserved so the parent's layout does not move.
		let cursor = ui.cursor().min;
		let bound_corner = ui.max_rect().max.min(ui.clip_rect().max);
		let avail = (bound_corner - cursor).max(egui::Vec2::ZERO);
		let layout = *ui.layout();
		let ctx_cloned = ui.ctx().clone();
		let closed = ui.allocate_ui_with_layout(avail, layout, |child_ui| {
			let full = child_ui.max_rect();
			let visible = full.intersect(child_ui.clip_rect());
			child_ui.expand_to_include_rect(full);
			if !visible.is_positive() {
				return Vec::new();
			}
			let mut dock_ui = child_ui.new_child(egui::UiBuilder::new().max_rect(visible).layout(layout));
			let mut viewer = FffiDockTabViewer {
				interpreter: self,
				ctx: &ctx_cloned,
				bodies,
				titles,
				no_scroll,
				closable,
				closed: Vec::new(),
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
			// Only a closable tab shows the close button
			// (FffiDockTabViewer::is_closeable). The leaf's close-all stays
			// off: it would close tabs the app has not offered to close.
			//
			// A leaf whose strip is hidden takes no space for it: no "show
			// the strip" triangle and no drag handle in the body's top margin,
			// since the strip comes back by rule, not by a gesture. The tab
			// context menus are off in a dock with bare tabs, because their
			// "Hide tab bar" would be undone on the next frame.
			let mut area = egui_dock::DockArea::new(&mut dock_state)
				.id(egui::Id::new(area_id))
				.show_close_buttons(true)
				.show_leaf_close_all_buttons(false);
			if any_bare {
				let mut style = egui_dock::Style::from_egui(dock_ui.style());
				style.buttons.show_tab_bar_size = 0.0;
				style.tab.tab_body.hidden_tab_bar_drag_height = Some(0.0);
				area = area.style(style).hidable_tab_bars(true).tab_context_menus(false);
			}
			area.show_inside(&mut dock_ui, &mut viewer);
			viewer.closed
		}).inner;

		// A close the person asked for, reported to Go under the tab's close
		// id; the tab stays until Go stops declaring it.
		for tab in closed {
			self.r7_push(dock_tab_close_id(area_id, tab), ResponseFlags::SHOULD_CLOSE);
		}

		self.dock_states.insert(area_id, dock_state);
	}
`)).
		WithReturnType(structDockAreaDummy()).
		Build())
	return blocks
}
