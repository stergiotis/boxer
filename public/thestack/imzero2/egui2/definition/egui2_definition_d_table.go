//go:build llm_generated_opus46

package definition

// =============================================================================
// TABLE WIDGET — egui_extras::TableBuilder binding via FFFI2
// =============================================================================
//
// Architecture: REGISTER-DRAIN pattern (same as tree widget)
//
//   1. Go pushes column defs into self.table_columns      (via tableColumn)
//   2. Go pushes header texts into self.table_header_texts (via tableHeaderText)
//   3. Go pushes cell content into self.table_cells        (via tableCellText / tableCellRichText)
//   4. The table node (Immediate+Retained, NOT BlockIterator) drains and renders
//
// No interpret_outer inside any closure. No recursion. No stack overflow.
// Row virtualization works via body.rows() indexing into the pre-collected cells.
//
// Go usage:
//
//   c.TableColumn().Initial(150.0).Resizable(true).Send()
//   c.TableColumn().Initial(120.0).Resizable(true).Send()
//   c.TableColumn().Remainder().Send()
//
//   c.TableHeaderText("Name").Send()
//   c.TableHeaderText("Department").Send()
//   c.TableHeaderText("Salary").Send()
//
//   for _, emp := range employees {
//       c.TableCellText(emp.Name).Send()
//       c.TableCellText(emp.Department).Send()
//       c.TableCellText(fmt.Sprintf("$%d", emp.Salary)).Send()
//   }
//
//   c.Table(ids.PrepareStr("my-table"), 20.0, uint64(len(employees))).Striped(true).Send()
//
// =============================================================================

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

// ---------------------------------------------------------------------------
// Registered nodes (additions to egui2_definition_d_registered.go)
// ---------------------------------------------------------------------------

func definitionsTableRegistered() []*ir.BuilderFactoryNode {
	registered := make([]*ir.BuilderFactoryNode, 0, 8)

	// tableColumn — accumulated column definition
	registered = append(registered, idl.NewBuilderFactoryNode("tableColumn").
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
		WithApplyCodeClientRust(rustClientCode("self.table_columns.push({{Instance}});\n")).
		WithSettingRetained(true).
		WithSettingImmediate(true).
		WithReturnType(structTableColumn()).
		Build())

	// tableHeaderText — accumulated header label
	registered = append(registered, idl.NewBuilderFactoryNode("tableHeaderText").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("text", ctabb.S).Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode("self.table_header_texts.push(text);\n")).
		WithSettingRetained(true).
		WithSettingImmediate(true).
		WithReturnType(structTableHeaderText()).
		Build())

	// tableCellText — accumulated cell content (plain text)
	registered = append(registered, idl.NewBuilderFactoryNode("tableCellText").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("text", ctabb.S).Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode("self.table_cells.push(TableCell::Text(text));\n")).
		WithSettingRetained(true).
		WithSettingImmediate(true).
		WithReturnType(structTableCell()).
		Build())

	// tableCellRichText — accumulated cell content (rich text via WidgetText)
	//
	// Go usage:
	//   c.WidgetText().Text("Alice").Send()
	//   c.TableCellRichText(c.WidgetText().Keep()).Send()
	registered = append(registered, idl.NewBuilderFactoryNode("tableCellRichText").
		AddArguments(idl.NewArgumentsBuilder().EvaluatedArg("widgetText", structWidgetText()).Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode("self.table_cells.push(TableCell::RichText(widget_text));\n")).
		WithSettingRetained(true).
		WithSettingImmediate(true).
		WithReturnType(structTableCell()).
		Build())

	return registered
}

// ---------------------------------------------------------------------------
// Table node (addition to egui2_definition_d_blocks.go)
// Immediate+Retained, NOT BlockIterator.
// ---------------------------------------------------------------------------

func definitionsTableBlock() []*ir.BuilderFactoryNode {
	blocks := make([]*ir.BuilderFactoryNode, 0, 4)

	blocks = append(blocks, idl.NewBuilderFactoryNode("table").
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("rowHeight", ctabb.F32).
			PlainArg("numRows", ctabb.U64).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("striped").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("{{Instance}}.striped = val;\n")).EndMethod().
			BeginMethod("vscroll").Arg("val", ctabb.B).
			CodeClientRust(rustClientCode("{{Instance}}.vscroll = val;\n")).EndMethod().
			BeginMethod("scrollToRow").Arg("row", ctabb.U64).
			CodeClientRust(rustClientCode("{{Instance}}.scroll_to_row = Some(row as usize);\n")).EndMethod().
			BeginMethod("minScrolledHeight").Arg("val", ctabb.F32).
			CodeClientRust(rustClientCode("{{Instance}}.min_scrolled_height = val;\n")).EndMethod().
			BeginMethod("maxScrollHeight").Arg("val", ctabb.F32).
			CodeClientRust(rustClientCode("{{Instance}}.max_scroll_height = val;\n")).EndMethod().
			Build()...).
		WithSettingImmediate(true).
		WithSettingRetained(true).
		WithConstructionCodeClientRust(rustClientCode("TableConfig::new(row_height, num_rows);\n")).
		WithApplyCodeClientRust(rustClientCode(`
if {{EguiUiOptionalOuter}}.is_some() {
	let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
	let col_count = self.table_columns.len();
	let num_rows = {{Instance}}.num_rows;
	let row_height = {{Instance}}.row_height;

	let mut builder = egui_extras::TableBuilder::new(ui);
	for col in self.table_columns.drain(..) {
		builder = builder.column(col);
	}
	if {{Instance}}.striped {
		builder = builder.striped(true);
	}
	if {{Instance}}.vscroll {
		builder = builder.vscroll(true);
	}
	if let Some(row) = {{Instance}}.scroll_to_row {
		builder = builder.scroll_to_row(row, None);
	}
	if {{Instance}}.min_scrolled_height > 0.0 {
		builder = builder.min_scrolled_height({{Instance}}.min_scrolled_height);
	}
	if {{Instance}}.max_scroll_height > 0.0 {
		builder = builder.max_scroll_height({{Instance}}.max_scroll_height);
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
		builder.header(row_height, |mut header| {
			for ht in header_texts.iter() {
				header.col(|ui| {
					ui.heading(ht.as_str());
				});
			}
		}).body(|body| {
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
} else {
	self.table_columns.clear();
	self.table_header_texts.clear();
	self.table_cells.clear();
}
`)).
		Build())

	return blocks
}
