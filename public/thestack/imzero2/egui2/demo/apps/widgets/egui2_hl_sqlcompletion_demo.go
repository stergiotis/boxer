package widgets

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/chtype"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlcomplete"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sqleditor"
)

// Demonstrates the completion pane (ADR-0190 §SD8) over a self-contained
// vocabulary: two component kinds and their fields, wired as in-process
// providers. Type inside the quotes and watch the match state move — prefix
// rows outlined in the accent, the exact row in the success tone, which is the
// same tone the editor uses to say the token resolves.

type sqlCompletionDemoState struct {
	ed   *sqleditor.Editor
	pane sqleditor.Pane
	eng  *sqlcomplete.Engine
	sql  string
}

var demoCompletionTypes = map[string]string{
	"SysMem": "Tuple(Id UInt64, Ts DateTime64(9, 'UTC'), TotalBytes UInt64, FreeBytes UInt64, SwapTotalBytes UInt64)",
	"SysCPU": "Tuple(Id UInt64, TotalPercent UInt8, LoadAvg1 Float32, LoadAvg5 Float32)",
}

func init() {
	registry.Register(registry.Demo{
		Name: "sqlcompletion", Category: "Layout & widgets", Title: icons.IconCheck + " SQL completion pane",
		Stage:       [2]float32{900, 520},
		Kind:        registry.DemoKindMixed,
		Description: "The completion pane beside a SQL editor (ADR-0190). The caret's position decides what the table shows: the component kinds inside `LW_COMPONENT('…')`, and the kind's own fields inside `tupleElement(…, '…')`. It is a table and not a popup — no focus, no captured key, so a click completes and a driver can assert the rows. Rows the typed text extends are outlined; the row it equals is outlined in the success tone. Where nothing can answer, the pane says why rather than showing an empty table.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			s := &sqlCompletionDemoState{
				ed:  sqleditor.New(),
				sql: "SELECT tupleElement(LW_COMPONENT('SysMem'), 'Tot",
			}
			r := sqlvocab.NewRegistry()
			r.MustRegister(sqlvocab.Function{
				Name: "LW_COMPONENT", Where: sqlvocab.WhereClient, Family: "leeway components", Available: true,
				Doc:    "read a whole component as a named tuple",
				Params: []sqlvocab.Param{sqlvocab.Lit("'Kind'", sqlvocab.DomainComponentKind)},
			})
			s.eng = &sqlcomplete.Engine{
				Vocab: r,
				Providers: sqlcomplete.Providers{
					ComponentKinds: func() ([]sqlcomplete.Item, bool) {
						return []sqlcomplete.Item{
							{Text: "SysCPU", Source: "registry", Doc: "per-core utilisation and load averages"},
							{Text: "SysMem", Source: "registry", Doc: "physical and swap memory, in bytes"},
						}, true
					},
					ComponentType: func(kind string) (chtype.Type, bool) {
						spec, hit := demoCompletionTypes[kind]
						if !hit {
							return chtype.Type{}, false
						}
						t, err := chtype.Parse(spec)
						return t, err == nil
					},
				},
			}
			return s
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoSqlCompletion(ids, state.(*sqlCompletionDemoState))
		},
		SourceFunc: demoSqlCompletion,
	})
}

func demoSqlCompletion(ids *c.WidgetIdStack, s *sqlCompletionDemoState) {
	res := s.ed.Bind(sqleditor.Frame{
		IDSlot: "demoSqlCompletion",
		Value:  &s.sql,
		Hint:   "-- type inside the quotes",
		Rows:   4,
	})

	comp := s.eng.Complete(sqlcomplete.Request{
		Site:      res.Site,
		Statement: res.Buffer,
		Caret:     res.Caret,
	})

	// The editor half of the two-way report: the token under the caret takes
	// the resolved tone when it names something the domain has.
	var deco sqleditor.Decoration
	if comp.Match == sqlcomplete.MatchExact {
		deco.Styled = append(deco.Styled, resolvedSection(comp.Partial))
	}
	s.ed.Render(ids, deco)

	c.Separator().Send()
	s.pane.Render(sqleditor.PaneInput{
		Ids:               ids,
		ScopeKey:          "demoCompletion",
		Result:            comp,
		Heading:           sqleditor.PaneHeading(comp),
		MaxHeight:         260,
		Width:             880,
		Typed:             res.Site.PartialText,
		CaretAtPartialEnd: res.Site.CaretAtPartialEnd(),
		OnAccept: func(_ sqlcomplete.Item, suffix string) {
			s.sql += suffix
		},
	})
}

func resolvedSection(r highlight.Range) codeview.StyledSection {
	return codeview.StyledSection{
		Start: uint32(r.Start), Stop: uint32(r.Stop),
		Flags: codeview.StyleUnderline,
		Color: sqleditor.ToneResolved,
	}
}
