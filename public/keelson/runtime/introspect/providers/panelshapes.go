// panel_shapes — the vocabulary of result contracts a play panel can render,
// as a keelson table (ADR-0170 §SD5).
//
// One row per (shape, pattern), because a shape is a *battery*: RE2 has no
// lookahead, so "has a `lane` column and a `title` column" is two patterns that
// must both match, not one that expresses the conjunction. `ordinal` orders the
// patterns within a shape and is otherwise meaningless — a battery is an AND,
// so no pattern is more important than another.
//
// The catalog CLI imports
// [github.com/stergiotis/boxer/public/gov/datacatalog/panelshapes] and
// evaluates the same list in Go; this is the other face of that one definition.
// Because the live-server expansion of `keelson(…)` is a url() source, a
// session can join this table against boxer.tables_catalog ad hoc, while
// boxer.tables_opaque_shapes is that join already materialized for sessions
// with no introspection plane up.
//
// ADR-0170 §SD5 placed the provider inside the panelshapes package. It is here
// instead, with every other provider: capmap, adr and codevol all keep their
// data in a gov package and their provider in this one, and a reader looking
// for where a keelson table comes from looks here. The §SD5 point — one
// definition, two faces — is unaffected; only the file moved.

package providers

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/gov/datacatalog/panelshapes"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// panelShapesProvider exposes the compiled-in shape battery.
type panelShapesProvider struct{}

func (panelShapesProvider) Name() string { return "panel_shapes" }

// FreshnessStatic: the battery is a compile-time constant of this binary. It
// changes when the binary does, which is exactly what static means.
func (panelShapesProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }

func (panelShapesProvider) Schema() *arrow.Schema { return panelShapesTable(nil).Schema() }

func (panelShapesProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	rows := panelShapeRows()
	return panelShapesTable(rows).Build(proj, len(rows)), nil
}

type panelShapeRow struct {
	shape   string
	pattern string
	ordinal int32
	note    string
}

// panelShapeRows flattens the batteries into (shape, pattern) rows. The note is
// repeated on every row of a shape rather than split into a second table: a
// shape has at most a handful of patterns, and one table a reader can select
// from beats two they have to join.
func panelShapeRows() (rows []panelShapeRow) {
	shapes := panelshapes.Shapes()
	rows = make([]panelShapeRow, 0, 3*len(shapes))
	for _, s := range shapes {
		for i, p := range s.Patterns {
			rows = append(rows, panelShapeRow{
				shape:   s.Name,
				pattern: p,
				ordinal: int32(i),
				note:    s.Note,
			})
		}
	}
	return
}

func panelShapesTable(rows []panelShapeRow) *introspect.Table {
	return introspect.NewTable().
		String("shape", func(i int) string { return rows[i].shape }).
		String("pattern", func(i int) string { return rows[i].pattern }).
		Int32("ordinal", func(i int) int32 { return rows[i].ordinal }).
		String("note", func(i int) string { return rows[i].note })
}
