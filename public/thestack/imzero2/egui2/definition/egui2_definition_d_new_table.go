//go:build llm_generated_opus47

package definition

// =============================================================================
// NEW_TABLE — egui_extras::TableBuilder binding via DeferredBlockMap
// =============================================================================
//
// Sister surface to c.Table. Where c.Table accumulates flat per-cell text
// content and renders via a fixed-shape body.rows() loop, newTable lets each
// cell carry arbitrary deferred opcodes — the cell body runs inside a real
// egui::Ui scope handed back by egui_extras.
//
// The Go-side API is iter-shaped:
//
//   c.NewTableColumn().Initial(200.0).Resizable(true).Send()
//   c.NewTableColumn().Remainder().AtLeast(240.0).Resizable(true).Send()
//
//   for tbl := range c.NewTable(id).Striped(true).Body() {
//       for hdr := range tbl.Header(28.0) {
//           for range hdr.Col() {
//               for rt := range c.RichTextLabel("name") { rt.Strong() }
//           }
//           for range hdr.Col() {
//               for rt := range c.RichTextLabel("value") { rt.Strong() }
//           }
//       }
//       for i := range rows {
//           for r := range tbl.Row(rowHeight(i)) {
//               for range r.Col() { c.Label(rows[i].name).Send() }
//               for range r.Col() { c.Label(rows[i].value).Send() }
//           }
//       }
//   }
//
// At wire level:
//   - newTableColumn pushes one egui_extras::Column into self.new_table_columns.
//   - newTableRowHeight pushes one f32 into self.new_table_row_heights (one per Row()).
//   - newTable opens two DeferredBlockMaps: ("headers", u32, u32) and ("rows", u64, u32).
//     The Go-side iter helpers translate Header().Col()/Row().Col() into
//     BeginHeaders/EndHeaders and BeginRows/EndRows scope calls under
//     auto-incrementing (header_idx, col_idx) / (row_idx, col_idx) keys.
//
// Apply (Rust):
//   - Reads both deferred block maps from the IPC stream.
//   - Constructs egui_extras::TableBuilder with drained columns + setters.
//   - Calls builder.header(h, |hdr| { for col in 0..n: hdr.col(|ui| replay) })
//     when header_height > 0. egui_extras 0.34 only supports one header row
//     (TableBuilder::header consumes self and returns Table<'_>).
//   - Calls table.body(|body| body.heterogeneous_rows(heights, |row| ...)).
//     Variable per-row heights work natively — no manual TableState fix-up.
//
// What this gets us over c.EndETable (egui_table):
//   - Drag persistence is native: dragging one column shifts width to the
//     remainder column, no auto_size cascade. No pre-show TableState fix-up.
//   - Per-cell content is a real Ui scope: labels render with sensible
//     defaults, cell padding is built into egui_extras, no Truncate dance
//     to keep min_size from snapping the column back to text width.
//   - egui_extras::Column::remainder() composes with .at_least()/.at_most();
//     multiple remainder columns split leftover evenly.

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

// --- Registered nodes (column + row-height accumulators) ---

func definitionsNewTableRegistered() []*ir.BuilderFactoryNode {
	registered := make([]*ir.BuilderFactoryNode, 0, 2)

	// newTableColumn — accumulated column definition for the next newTable call.
	// Mirrors the existing tableColumn but pushes into a separate vec so the
	// two surfaces (c.Table / c.NewTable) don't share state.
	registered = append(registered, idl.NewBuilderFactoryNode("newTableColumn").
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("auto").
			CodeClientRust(rustClientCode("{{Instance}} = egui_extras::Column::auto();\n")).EndMethod().
			BeginMethod("exact").Arg("width", ctabb.F32).
			CodeClientRust(rustClientCode("{{Instance}} = egui_extras::Column::exact(width);\n")).EndMethod().
			BeginMethod("initial").Arg("width", ctabb.F32).
			CodeClientRust(rustClientCode("{{Instance}} = egui_extras::Column::initial(width);\n")).EndMethod().
			BeginMethod("remainder").
			CodeClientRust(rustClientCode("{{Instance}} = egui_extras::Column::remainder();\n")).EndMethod().
			BeginMethod("atLeast").Arg("minWidth", ctabb.F32).
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.at_least(min_width);\n")).EndMethod().
			BeginMethod("atMost").Arg("maxWidth", ctabb.F32).
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.at_most(max_width);\n")).EndMethod().
			BeginMethod("resizable").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.resizable(val);\n")).EndMethod().
			BeginMethod("clipContents").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.clip(val);\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode("egui_extras::Column::auto();\n")).
		WithApplyCodeClientRust(rustClientCode("self.new_table_columns.push({{Instance}});\n")).
		WithSettingRetained(true).
		WithSettingImmediate(true).
		WithReturnType(structNewTableColumn()).
		Build())

	// newTableRowHeight — pushes one f32 onto self.new_table_row_heights.
	// One push per Row() iteration; the order matches the order in which
	// rows() will replay row content blocks (heterogeneous_rows).
	registered = append(registered, idl.NewBuilderFactoryNode("newTableRowHeight").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("height", ctabb.F32).Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode("self.new_table_row_heights.push(height);\n")).
		WithSettingImmediate(true).
		WithReturnType(structNewTableHeight()).
		Build())

	return registered
}

// --- Drain block (renders egui_extras::TableBuilder, draining the registers) ---

func definitionsNewTableBlock() []*ir.BuilderFactoryNode {
	blocks := make([]*ir.BuilderFactoryNode, 0, 1)

	blocks = append(blocks, idl.NewBuilderFactoryNode("newTable").
		WithIdentityId(true).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("striped").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("striped_flag = val;\n")).EndMethod().
			BeginMethod("vscroll").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("vscroll_flag = val;\n")).EndMethod().
			BeginMethod("minScrolledHeight").Arg("val", ctabb.F32).
			CodeClientRust(rustClientCode("min_scrolled_height = val;\n")).EndMethod().
			BeginMethod("maxScrollHeight").Arg("val", ctabb.F32).
			CodeClientRust(rustClientCode("max_scroll_height = val;\n")).EndMethod().
			BeginMethod("scrollToRow").Arg("row", ctabb.U64).
			CodeClientRust(rustClientCode("scroll_to_row = Some(row as usize);\n")).EndMethod().
			// headerHeight: 0 (default) means no header row (skip
			// TableBuilder::header entirely). Otherwise this is the single
			// header row's height. egui_extras 0.34's TableBuilder::header
			// consumes self and returns Table<'_>, so multiple header rows
			// are not supported at the crate level — at most one header().
			BeginMethod("headerHeight").Arg("val", ctabb.F32).
			CodeClientRust(rustClientCode("header_height = val;\n")).EndMethod().
			// autoShrink: forwards to the inner ScrollArea's auto_shrink
			// flag. egui_extras defaults to (true, true) which makes the
			// underlying ScrollArea shrink to its content width — meaning
			// the table won't fill the panel even with a Remainder column.
			// Pass (false, ?) to make the table greedily fill the parent's
			// horizontal extent so Remainder absorbs the slack.
			BeginMethod("autoShrink").Arg("horiz", ctabb.B).Arg("vert", ctabb.B).
			CodeClientRust(rustClientCode("auto_shrink_h = horiz;\nauto_shrink_v = vert;\nauto_shrink_set = true;\n")).EndMethod().
			Build()...).
		WithDeferredBlockMap("headers", ctabb.U32, ctabb.U32).
		WithDeferredBlockMap("rows", ctabb.U64, ctabb.U32).
		WithSettingImmediate(true).
		WithSettingRetained(true).
		WithReturnType(structNewTableDummy()).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut header_height: f32 = 0.0;
let mut striped_flag: bool = false;
let mut vscroll_flag: bool = false;
let mut min_scrolled_height: f32 = 0.0;
let mut max_scroll_height: f32 = 0.0;
let mut scroll_to_row: Option<usize> = None;
let mut auto_shrink_h: bool = true;
let mut auto_shrink_v: bool = true;
let mut auto_shrink_set: bool = false;
`)).
		WithApplyCodeClientRust(rustClientCode(`
if {{EguiUiOptionalOuter}}.is_some() {
	let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
	let ctx_cloned = ui.ctx().clone();
	let auto_shrink_opt = if auto_shrink_set { Some((auto_shrink_h, auto_shrink_v)) } else { None };
	let _ = self.render_new_table(
		{{Id}}.value(), &ctx_cloned, ui,
		header_height,
		striped_flag, vscroll_flag,
		min_scrolled_height, max_scroll_height,
		scroll_to_row,
		auto_shrink_opt,
	);
} else {
	self.new_table_columns.clear();
	self.new_table_row_heights.clear();
	self.io.skip_deferred_block_map_u32_u32()?;
	self.io.skip_deferred_block_map_u64_u32()?;
}
`)).
		Build())

	return blocks
}
