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
			// maxHeight caps the vertical region the table allocates. Non-zero
			// values switch the bounded-child-ui wrap to use exactly this
			// height; zero (the default) leaves the auto-fit heuristic in
			// charge — see the apply prelude for the heuristic itself.
			// Egui_table's SplitScroll otherwise greedily consumes
			// ui.available_size() (table.rs:468 in egui_table 0.8.0), which
			// silently pushes every sibling widget after the table off-screen
			// when the etable sits inside a vertically flowing parent.
			BeginMethod("maxHeight").Arg("height", ctabb.F32).
			CodeClientRust(rustClientCode("max_height_override = Some(height);\n")).EndMethod().
			Build()...).
		// Deferred block maps: cells keyed by (row, col), headers keyed by (header_row, col)
		WithReturnType(structEtDummy()).
		WithDeferredBlockMap("cells", ctabb.U64, ctabb.U32).
		WithDeferredBlockMap("headers", ctabb.U32, ctabb.U32).
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

	let columns: Vec<egui_table::Column> = self.et_columns.drain(..).collect();
	let header_texts: Vec<String> = self.et_header_texts.drain(..).collect();

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
	struct EtStripedDelegate<'sa, 'sb, 'sc, SR: std::io::BufRead, SW: std::io::Write> {
		inner: FffiTableDelegate<'sa, 'sb, 'sc, SR, SW>,
		table_id: u64,
		striped: bool,
		selected_row: Option<u64>,
	}
	impl<'sa, 'sb, 'sc, SR: std::io::BufRead, SW: std::io::Write> egui_table::TableDelegate
		for EtStripedDelegate<'sa, 'sb, 'sc, SR, SW>
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
	ui.allocate_ui_with_layout(bound_size, layout, |child_ui| {
		table.show(child_ui, &mut delegate);
	});
} else {
	self.et_columns.clear();
	self.et_header_texts.clear();
	self.et_row_heights.clear();
	self.io.skip_deferred_block_map_u64_u32()?;
	self.io.skip_deferred_block_map_u32_u32()?;
}
`)).
		Build())

	return blocks
}
