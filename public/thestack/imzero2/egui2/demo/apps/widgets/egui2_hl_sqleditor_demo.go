package widgets

import (
	"fmt"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sqleditor"
)

// Demonstrates the SQL editing surface (ADR-0147): no-wrap monospace editor,
// line-number gutter and marks lane, lexical colour upgrading to semantic once
// the buffer sits quiescent, the widget's own active-statement tint, and an
// embedder-contributed decoration.
//
// Non-deterministic: the L2 semantic tier lands asynchronously, so the colours
// a screenshot catches depend on when it was taken.

type sqlEditorDemoState struct {
	ed  *sqleditor.Editor
	sql string
}

func init() {
	registry.Register(registry.Demo{
		Name: "sqleditor", Category: "Layout & widgets", Title: icons.IconTable + " SQL editor",
		Stage:       [2]float32{860, 460},
		Flags:       registry.DemoFlagNonDeterministic,
		Kind:        registry.DemoKindMixed,
		Description: "The reusable SQL editing surface: a no-wrap monospace editor with a line-number gutter and marks lane beside it. Type to see lexical colour, pause to see it upgrade to semantic (table/column/alias names) on a background parse. The buffer holds three statements — move the caret between them and the active one takes a faint tint and a `>` mark in the gutter, both derived by the widget from buffer and caret alone. The underline on the middle statement is an embedder-contributed decoration, the channel a host uses for what only it can know. Below the editor, the widget's published Result: caret offset, which statement it is in, and what a run-under-cursor would ship.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			return &sqlEditorDemoState{
				ed: sqleditor.New(),
				sql: "SELECT number, number * 2 AS doubled\nFROM system.numbers\nLIMIT 10;\n\n" +
					"SELECT nonsuch FROM system.one;\n\n" +
					"WITH t AS (SELECT 1 AS a)\nSELECT a FROM t",
			}
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoSqlEditor(ids, state.(*sqlEditorDemoState))
		},
		SourceFunc: demoSqlEditor,
	})
}

func demoSqlEditor(ids *c.WidgetIdStack, s *sqlEditorDemoState) {
	// Bind first: the decoration below reads the caret it publishes, and the
	// widget's own tint is derived in the same step.
	res := s.ed.Bind(sqleditor.Frame{
		IDSlot: "demoSqlEditor",
		Value:  &s.sql,
		Hint:   "-- type SQL",
		Rows:   12,
	})

	// One embedder-contributed decoration, standing in for what a host would
	// derive from its own analysis: an underline on a column the catalog does
	// not have. Byte range into the canonical buffer, as the seam requires.
	var deco sqleditor.Decoration
	if start := indexOfToken(s.sql, "nonsuch"); start >= 0 {
		deco.Styled = []codeview.StyledSection{{
			Start: uint32(start), Stop: uint32(start + len("nonsuch")),
			Flags: codeview.StyleUnderline,
			Color: sqleditor.ToneError,
		}}
	}
	// The subquery mark rides its own channel — drawn whether or not the
	// embedder tinted the range.
	if start := indexOfToken(s.sql, "SELECT 1 AS a"); start >= 0 {
		deco.SubqueryMark = nanopass.SourceRange{Start: start, End: start + len("SELECT 1 AS a")}
	}

	s.ed.Render(ids, deco)

	c.Separator().Send()
	stmt := "none"
	if res.Ok {
		stmt = fmt.Sprintf("%d of %d — %q", res.Number, res.Total,
			truncate(res.Buffer[res.Statement.Src.Start:res.Statement.Src.End], 48))
	}
	for rt := range c.RichTextLabel(fmt.Sprintf("caret byte %d · statement %s", res.Caret, stmt)) {
		rt.Small().Weak().Monospace()
	}
	for rt := range c.RichTextLabel("run-under-cursor would ship: " + truncate(res.Run, 72)) {
		rt.Small().Weak().Monospace()
	}
}

// indexOfToken is the demo's stand-in for a host's own analysis: a plain
// substring search, which is enough to place a decoration in a fixed buffer.
func indexOfToken(s, tok string) int {
	for i := 0; i+len(tok) <= len(s); i++ {
		if s[i:i+len(tok)] == tok {
			return i
		}
	}
	return -1
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
