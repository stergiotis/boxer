package definition

// =============================================================================
// console binding — see doc/adr/0359-egui-console-a-terminal-widget-over-a-vendored-vt-core.md
// =============================================================================
//
// A terminal screen whose VT core runs in the client. Go feeds the program's
// output bytes, addressed by their offset in the stream; the widget draws the
// screen, takes keyboard focus and encodes keys and pastes, and reports what
// the program should be sent and what happened as rows of the r29 register.
//
// Opcodes:
//   - console          — feed the bytes past the widget's acknowledged offset, draw, read keys
//   - consoleRelease   — drop the widget's core and screen (the LRU reaps otherwise)
//   - fetchR29Console  — the rows: (widget id, kind, payload)
//
// The Rust side is the hand-written module rust/imzero2/src/imzero2/console.rs.

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

func structConsole() ir.ConcreteType {
	return ir.NewConcreteType("console")
}

func definitionsConsole() (nodes []ir.NodeI) {
	nodes = append(nodes, idl.NewBuilderFactoryNode("console").
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().
			// The offset in the program's output stream of the first byte of
			// feed: the widget applies what lies past the bytes it has read
			// and ignores the overlap.
			PlainArg("offset", ctabb.U64).
			PlainArg("feed", ctabb.U8h).
			Build()).
		AddMethods(idl.NewMethodBuilder().
			// How many bytes of scrollback the core keeps; read when the
			// widget's core is made. Zero takes the default.
			BeginMethod("historyBytes").Arg("hb", ctabb.U32).
			CodeClientRust(rustClientCode("history_bytes = hb;\n")).EndMethod().
			// The widget's size in points; zero on an axis takes what the
			// parent has left on it.
			BeginMethod("size").Arg("wd", ctabb.F32).Arg("ht", ctabb.F32).
			CodeClientRust(rustClientCode("size_w = wd;\nsize_h = ht;\n")).EndMethod().
			// The monospace font size in points; zero takes the style's.
			BeginMethod("fontSize").Arg("fs", ctabb.F32).
			CodeClientRust(rustClientCode("font_size = fs;\n")).EndMethod().
			// A fixed grid of cols by rows, whatever room the widget is
			// given: a replay at a recorded size. The widget takes the grid's
			// size in points; zero on an axis follows the room.
			BeginMethod("grid").Arg("cols", ctabb.U16).Arg("rows", ctabb.U16).
			CodeClientRust(rustClientCode("grid_cols = cols;\ngrid_rows = rows;\n")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut history_bytes = 0u32;
let mut font_size = 0f32;
let mut size_w = 0f32;
let mut size_h = 0f32;
let mut grid_cols = 0u16;
let mut grid_rows = 0u16;
`)).
		WithApplyCodeClientRust(rustClientCode(`
if {{EguiUiOptionalOuter}}.is_some() {
    let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
    self.console_show(
        ui,
        crate::imzero2::console::ConsoleArgs {
            id: {{Id}}.value(),
            offset,
            feed: &feed,
            history_bytes,
            font_size,
            size: egui::vec2(size_w, size_h),
            grid: (grid_cols, grid_rows),
        },
    );
}
`)).
		WithSettingImmediate(true).
		WithReturnType(structConsole()).
		Build())

	nodes = append(nodes, idl.NewBuilderFactoryNode("consoleRelease").
		WithIdentityId(true).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`self.consoles.release({{Id}}.value());
`)).
		WithSettingImmediate(true).
		WithReturnType(structConsole()).
		Build())

	// Drains the rows every console produced last frame, in order: ids and
	// kinds per row, the payload lengths per row, and the payloads
	// concatenated. Go splits them per widget in StateManager.Sync.
	nodes = append(nodes, idl.NewFetcherNode("fetchR29Console").
		WithApplyCodeClientRust(rustClientCode(`
let len = self.r29_console_ids.len();
debug_assert_eq!(len, self.r29_console_kinds.len());
debug_assert_eq!(len, self.r29_console_lens.len());
self.io.write_plain_u64h(len, self.r29_console_ids.drain(..))?;
self.io.write_plain_u8h(len, self.r29_console_kinds.drain(..))?;
self.io.write_plain_u32h(len, self.r29_console_lens.drain(..))?;
self.io.write_plain_u8_slice(&self.r29_console_data)?;
self.r29_console_data.clear();
{{SendMessage}}
`)).
		AddReturnValue("ids", ctabb.U64h).
		AddReturnValue("kinds", ctabb.U8h).
		AddReturnValue("lens", ctabb.U32h).
		AddReturnValue("data", ctabb.U8h).
		Build())
	return
}
