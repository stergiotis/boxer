package definition

// =============================================================================
// EGUI_TABLE binding — using FFFI2 DeferredBlockMap primitive
// =============================================================================
//
// binding reduces to:
//   - 2 registered nodes (etColumn, etHeaderText) — unchanged
//   - 1 consuming node (endETable) with WithDeferredBlockMap("cells", U64, U32)
//   - ~30 lines of Rust apply code
//   - 0 lines of Go application support code
//
// The framework handles:
//   - Writer swap (DeferredBlockScope.Begin/End)
//   - Block serialization (DeferredBlockScope.WriteToFixedKey)
//   - Block deserialization (self.read_deferred_block_map_u64_u32())
//   - Opcode replay (self.replay_deferred_block(ctx, ui, &block))
//   - EOF termination in interpret_outer
//
// Compare with the manual approach:
//   - etable_go_support.go (EtScope, writer stubs) — ELIMINATED
//   - Manual io.read_u64/read_u32/read_exact parsing — ELIMINATED
//   - Manual skip_bytes for skipped blocks — ELIMINATED
//   - et_capturing / et_buffering flags — NEVER EXISTED
//
// =============================================================================

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

// Registered nodes — column and header accumulation (unchanged)
func definitionsEtRegistered() []*ir.BuilderFactoryNode {
	registered := make([]*ir.BuilderFactoryNode, 0, 4)

	registered = append(registered, idl.NewBuilderFactoryNode("etColumn").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("currentWidth", ctabb.F32).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("resizable").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.resizable(val);\n")).EndMethod().
			BeginMethod("rangeMinMax").Arg("min", ctabb.F32).Arg("max", ctabb.F32).
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.range(egui::Rangef::new(min, max));\n")).EndMethod().
			BeginMethod("autoSizeThisFrame").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.auto_size_this_frame(val);\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode("egui_table::Column::new(current_width);\n")).
		WithApplyCodeClientRust(rustClientCode("self.et_columns.push({{Instance}});\n")).
		WithSettingRetained(true).
		WithSettingImmediate(true).
		WithReturnType(structEtColumn()).
		Build())

	registered = append(registered, idl.NewBuilderFactoryNode("etHeaderText").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("text", ctabb.S).Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode("self.et_header_texts.push(text);\n")).
		WithSettingRetained(true).
		WithSettingImmediate(true).
		WithReturnType(structEtHeaderText()).
		Build())

	registered = append(registered, idl.NewBuilderFactoryNode("etRowHeight").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("height", ctabb.F32).Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode("self.et_row_heights.push(height);\n")).
		WithSettingImmediate(true).
		WithReturnType(structEtHeaderText()). // reuse dummy type
		Build())

	return registered
}

// endETable — the consuming node with DeferredBlockMap
func definitionsEtBlock() []*ir.BuilderFactoryNode {
	blocks := make([]*ir.BuilderFactoryNode, 0, 4)

	blocks = append(blocks, idl.NewBuilderFactoryNode("endETable").
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("numRows", ctabb.U64).
			PlainArg("defaultRowHeight", ctabb.F32).
			PlainArg("numStickyHeaders", ctabb.U32).
			PlainArg("numStickyCols", ctabb.U32).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("scrollToRow").Arg("row", ctabb.U64).Arg("align", ctabb.U8).
			CodeClientRust(rustClientCode("scroll_to_row = Some((row, decode_scroll_align(align)));\n")).EndMethod().
			BeginMethod("scrollToColumn").Arg("col", ctabb.U32).Arg("align", ctabb.U8).
			CodeClientRust(rustClientCode("scroll_to_column = Some((col as usize, decode_scroll_align(align)));\n")).EndMethod().
			BeginMethod("scrollToRows").Arg("rowBegin", ctabb.U64).Arg("rowEnd", ctabb.U64).Arg("align", ctabb.U8).
			CodeClientRust(rustClientCode("scroll_to_row_range = Some((row_begin..=row_end, decode_scroll_align(align)));\n")).EndMethod().
			BeginMethod("scrollToColumns").Arg("colBegin", ctabb.U32).Arg("colEnd", ctabb.U32).Arg("align", ctabb.U8).
			CodeClientRust(rustClientCode("scroll_to_col_range = Some((col_begin as usize..=col_end as usize, decode_scroll_align(align)));\n")).EndMethod().
			BeginMethod("autoSizeMode").Arg("mode", ctabb.U8).
			CodeClientRust(rustClientCode(`auto_size_mode = match mode { 1 => egui_table::AutoSizeMode::Always, 2 => egui_table::AutoSizeMode::OnParentResize, _ => egui_table::AutoSizeMode::Never };
`)).EndMethod().
			BeginMethod("striped").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("striped_flag = val;\n")).EndMethod().
			BeginMethod("selectedRow").Arg("row", ctabb.U64).
			CodeClientRust(rustClientCode("selected_row_opt = Some(row);\n")).EndMethod().
			// maxHeight is a CEILING on the table's height, not the height
			// itself: the table takes what its rows need and stops here. A
			// table shorter than the ceiling renders at its own size, so a
			// three-row log stays three rows tall rather than reserving the
			// cap. Zero (the default) leaves the auto-fit heuristic in charge
			// — see the apply prelude for the heuristic itself.
			//
			// A caller that wants the table to take the whole pane wants
			// fillPane below, not a maxHeight equal to the pane. (It read as
			// an exact height until 2026-08-29, so a call site that hands
			// over min(natural, cap) is applying a min the binding now
			// applies itself.)
			//
			// Egui_table's SplitScroll otherwise greedily consumes
			// ui.available_size() (table.rs:468 in egui_table 0.8.0), which
			// silently pushes every sibling widget after the table off-screen
			// when the etable sits inside a vertically flowing parent.
			BeginMethod("maxHeight").Arg("height", ctabb.F32).
			CodeClientRust(rustClientCode("max_height_override = Some(height);\n")).EndMethod().
			// fillPane makes the table as tall as the room left in its parent
			// — the whole point being that the ROOM is what decides, not the
			// row count: the table keeps its size as rows arrive and leave,
			// and its gridlines run to the bottom of the pane.
			//
			// Use it for a table that owns its pane (a dock tab body, a
			// central panel, a leaf of a split) and is the last thing placed
			// in it. Do NOT use it in an unbounded parent — an auto-sized
			// Window's inner ui reaches to the bottom of the screen, and the
			// table would grow the window to match; that is what the auto-fit
			// cap in the apply prelude is for.
			//
			// This replaces the Go-side idiom of probing the pane with
			// CaptureUiAvailableRect and feeding the answer back as a height:
			// the rect is read here, at the point of allocation, so there is
			// no one-frame lag, no r21 slot to key, and no held value to
			// carry across the frames a probe is absent.
			//
			// Composes with maxHeight, which still caps: fillPane takes the
			// pane, maxHeight bounds what it may take.
			BeginMethod("fillPane").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("fill_pane = val;\n")).EndMethod().
			// applyWidths opts the table into the ADR-0151 width protocol.
			// The epoch is Go's "my resolved widths changed" generation: the
			// binding seeds egui_table's TableState from the etColumn widths
			// only when it differs from the epoch last seen for this table.
			// Between bumps TableState wins, which is what leaves the user's
			// drag alone — re-asserting every frame is the pathology §SD4
			// exists to avoid.
			//
			// Calling it also turns on the width read-back for this table, so
			// a table that never opts in pays nothing.
			BeginMethod("applyWidths").Arg("epoch", ctabb.U32).
			CodeClientRust(rustClientCode("apply_widths_epoch = Some(epoch);\n")).EndMethod().
			Build()...).
		// Deferred block maps: cells keyed by (row, col), headers keyed by
		// (header_row, col), rows keyed by row.
		//
		// The maps are spliced by the generated Send() in DECLARATION ORDER,
		// so the apply code below must read — and the culled else-arm must
		// skip — cells, then headers, then rows. Reordering these lines
		// without reordering both Rust sites desynchronises the stream.
		WithReturnType(structEtDummy()).
		WithDeferredBlockMap("cells", ctabb.U64, ctabb.U32).
		WithDeferredBlockMap("headers", ctabb.U32, ctabb.U32).
		WithDeferredBlockMap("rows", ctabb.U64).
		WithSettingImmediate(true).
		WithSettingRetained(true).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut scroll_to_row: Option<(u64, Option<egui::Align>)> = None;
let mut scroll_to_column: Option<(usize, Option<egui::Align>)> = None;
let mut scroll_to_row_range: Option<(std::ops::RangeInclusive<u64>, Option<egui::Align>)> = None;
let mut scroll_to_col_range: Option<(std::ops::RangeInclusive<usize>, Option<egui::Align>)> = None;
let mut auto_size_mode = egui_table::AutoSizeMode::Never;
let mut striped_flag = false;
let mut selected_row_opt: Option<u64> = None;
let mut max_height_override: Option<f32> = None;
let mut fill_pane = false;
let mut apply_widths_epoch: Option<u32> = None;
fn decode_scroll_align(v: u8) -> Option<egui::Align> {
    match v { 1 => Some(egui::Align::TOP), 2 => Some(egui::Align::Center), 3 => Some(egui::Align::BOTTOM), _ => None }
}
`)).
		WithApplyCodeClientRust(rustClientCode(`
if {{EguiUiOptionalOuter}}.is_some() {
	let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();

	let col_count = self.et_columns.len();

	// Read deferred block maps from the IPC stream.
	// Cell blocks use a dense flat layout (single slab + O(1) indexed lookup)
	// instead of a HashMap — eliminates per-block Vec allocations and hashing.
	let cells = self.io.read_deferred_block_map_dense_u64_u32(num_rows, col_count)?;
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
		.id_salt({{Id}})
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
	struct EtStripedDelegate<'sa, 'sb, 'sc, 'sd, SR: std::io::BufRead, SW: std::io::Write> {
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
	impl<'sa, 'sb, 'sc, 'sd, SR: std::io::BufRead, SW: std::io::Write> egui_table::TableDelegate
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
		fn header_cell_ui(&mut self, ui: &mut egui::Ui, cell: &egui_table::HeaderCellInfo) {
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
					let _ = self.inner.interpreter.replay_deferred_block(&ctx, ui, block);
				}
			}
		}
		fn cell_ui(&mut self, ui: &mut egui::Ui, cell: &egui_table::CellInfo) {
			let visuals = ui.style().visuals.clone();
			let rect = ui.max_rect();
			if self.selected_row == Some(cell.row_nr) {
				let bg = imzero2_egui::style::tokens::palette_generated::ACCENT_DEFAULT.gamma_multiply(0.35);
				ui.painter().rect_filled(rect, 0.0, bg);
			} else if self.striped && cell.row_nr % 2 == 1 {
				ui.painter().rect_filled(rect, 0.0, visuals.faint_bg_color);
			}
			self.inner.cell_ui(ui, cell);
		}
		fn default_row_height(&self) -> f32 {
			self.inner.default_row_height()
		}
		fn row_top_offset(&self, ctx: &egui::Context, table_id: egui::Id, row_nr: u64) -> f32 {
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
		table_id: {{Id}}.value(),
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
	// Height, in three cases:
	//   - fillPane: the room left in the parent, whatever the rows need.
	//   - otherwise: natural, capped. natural = header rows + body rows
	//     (from the row_offsets prefix sum when per-row heights are set,
	//     else num_rows × default) + a scrollbar margin. The cap is
	//     maxHeight when the caller set one, else ETABLE_AUTOFIT_CAP_PX.
	//   - both: the room left, capped by maxHeight.
	//
	// The default cap exists for the unbounded parent — an auto-sized Window
	// whose inner ui reaches the bottom of the screen, a Vscroll ScrollArea
	// that would happily accept an absurd 10_000-row content size. There is
	// no way to tell such a parent from a pane by measuring it, which is why
	// filling one is opt-in rather than the default.
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

	// The pane, for fillPane: available_rect_before_wrap() is the room from
	// the cursor to the parent's max_rect corner — the same rect the Go-side
	// CaptureUiAvailableRect probe reports, read here a frame earlier.
	//
	// Not clip_rect(): inside a ScrollArea the cursor legitimately advances
	// past clip_rect.max.y, since the area renders off-viewport content for
	// the user to scroll to. max_rect is the right one and is well-defined in
	// the parents fillPane is meant for — egui 0.35 gives a ScrollArea's
	// content ui a max_rect the size of the visible viewport
	// (scroll_area.rs, content_max_size = inner_size), so a dock tab body
	// measures its leaf rather than infinity.
	//
	// Floored at one header plus one row: a pane dragged shorter than that
	// gets a clipped table instead of a header over nothing, which at least
	// keeps a row and the scrollbar reachable.
	//
	// The parent extends itself to fit our allocation — the behaviour
	// ScrollArea / Window / CollapsingHeader / AllocateUiAtRect all share —
	// so a table larger than a tightly bounded parent simply scrolls
	// internally, which is what egui_table does for any bound anyway.
	let avail_x = (ui.max_rect().max.x - ui.cursor().min.x).max(0.0);
	let pane_height =
		ui.available_rect_before_wrap().height().max(header_height + default_row_height);
	let bounded_height = match (fill_pane, max_height_override) {
		(true, Some(h)) if h > 0.0 => pane_height.min(h),
		(true, _) => pane_height,
		(false, Some(h)) if h > 0.0 => natural_height.min(h),
		(false, _) => natural_height.min(ETABLE_AUTOFIT_CAP_PX),
	};
	let bound_size = egui::Vec2::new(avail_x, bounded_height);
	let layout = *ui.layout();
	let table_gid = {{Id}}.value();
	ui.allocate_ui_with_layout(bound_size, layout, |child_ui| {
		// The state id must be derived from the SAME ui table.show() will
		// receive: TableState::id is ui.make_persistent_id(salt), so
		// computing it from the outer ui would address a different slot.
		let state_id = egui_table::TableState::id(child_ui, egui::IdSalt::new({{Id}}));

		if let Some(epoch) = apply_widths_epoch {
			let seen = delegate.inner.interpreter.width_epochs.get(&table_gid).copied();
			if seen != Some(epoch) {
				// Seeding TableState does two things at once, and both are
				// wanted. egui_table copies col_widths over each column's
				// current width before laying out (table.rs:390), so ours
				// win; and because it treats a present state as "not new"
				// (is_new = state.is_none(), table.rs:384), storing one also
				// suppresses the first-show force-autofit that would
				// otherwise overwrite them on the very first frame.
				let mut st = egui_table::TableState::load(child_ui, state_id).unwrap_or_default();
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
`)).
		Build())

	return blocks
}
