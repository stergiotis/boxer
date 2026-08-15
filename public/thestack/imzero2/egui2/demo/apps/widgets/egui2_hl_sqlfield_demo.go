package widgets

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sqleditor"
)

// Demonstrates the SQL field (ADR-0187 §SD7): the fragment-sized
// sibling of the editor above, for a panel control whose value is SQL rather
// than a statement.
//
// Deterministic, unlike the editor demo — the field has only the lex tier, so
// there is no background parse whose landing a screenshot could race.

type sqlFieldDemoState struct {
	predicate  sqleditor.Field
	predicated string

	list     sqleditor.Field
	listSQL  string
	source   sqleditor.Field
	table    string
	broken   sqleditor.Field
	brokenEx string
}

func init() {
	registry.Register(registry.Demo{
		Name: "sqlfield", Category: "Layout & widgets", Title: icons.IconTable + " SQL field",
		Stage:       [2]float32{760, 560},
		Flags:       registry.DemoFlagNone,
		Kind:        registry.DemoKindMixed,
		Description: "The fragment-sized SQL surface: a control whose value is a predicate, an expression list or a table source rather than a statement. Single-line by default — Enter inserts nothing, because it is egui's single-line form — with lexical syntax colour on every keystroke. A row count opts into the multi-line form for a control whose value genuinely is several lines, such as a colour block. The last field carries an embedder-supplied error mark: the widget owns the tone, so a field and the editor above cannot disagree about what an error looks like, and the range is clamped to the fragment because it can arrive one frame behind the text.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			return &sqlFieldDemoState{
				predicated: "status = 'error' AND ts > now() - INTERVAL 1 HOUR",
				listSQL: "transparency * 255 AS red,\n" +
					"transparency * 200 AS green,\n" +
					"transparency * 120 AS blue",
				table:    "system.numbers",
				brokenEx: "count(  ) FILTER",
			}
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoSqlField(ids, state.(*sqlFieldDemoState))
		},
		SourceFunc: demoSqlField,
	})
}

func demoSqlField(ids *c.WidgetIdStack, s *sqlFieldDemoState) {
	sqlFieldRow(ids, "predicate", &s.predicate, sqleditor.FieldFrame{
		IDSlot: "demoSqlFieldPredicate",
		Value:  &s.predicated,
		Hint:   "-- a WHERE fragment",
		Width:  520,
	})
	sqlFieldRow(ids, "table source", &s.source, sqleditor.FieldFrame{
		IDSlot: "demoSqlFieldSource",
		Value:  &s.table,
		Hint:   "-- a table or table function",
		Width:  520,
	})

	c.Separator().Send()
	for rt := range c.RichTextLabel("Rows > 1 takes the multi-line form — a colour block, the control this shape exists for:") {
		rt.Small().Weak()
	}
	sqlFieldRow(ids, "expression list", &s.list, sqleditor.FieldFrame{
		IDSlot: "demoSqlFieldList",
		Value:  &s.listSQL,
		Rows:   4,
		Width:  520,
	})

	c.Separator().Send()
	for rt := range c.RichTextLabel("An embedder-supplied error mark. Edit the text: the range is clamped to the fragment, so shortening it past the mark cannot describe bytes that are not there.") {
		rt.Small().Weak()
	}
	// The mark stands in for what a validating embedder would derive from a
	// parse of the substituted query; here it is a fixed range, which is enough
	// to show the tone and the clamp.
	sqlFieldRow(ids, "with a mark", &s.broken, sqleditor.FieldFrame{
		IDSlot: "demoSqlFieldBroken",
		Value:  &s.brokenEx,
		Width:  520,
		Mark:   nanopass.SourceRange{Start: 8, End: 16},
	})
}

// sqlFieldRow draws one captioned field. The caption is a plain label rather
// than a widget affordance: the field is the demo, and a heavier chrome would
// be the demo's own design rather than the widget's.
func sqlFieldRow(ids *c.WidgetIdStack, caption string, f *sqleditor.Field, frame sqleditor.FieldFrame) {
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel(caption) {
			rt.Small().Weak()
		}
		f.Render(ids, frame)
	}
}
