package mdnotebook

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Value is a stored value as a `val:` link shows it.
type Value struct {
	// Face is the value through its column's gloss, or its plain text when
	// the column declares none.
	Face gloss.Inline
	// ColumnName is the column's full name, gloss token included.
	ColumnName string
}

// ValueOf reads the value a `val:` link names: the column of a record of
// one row. column is the column's label, the part of its name before any
// gloss token. token is the gloss the link asks for; empty, the value is
// shown through the gloss its column declares, or plainly. cat is the
// catalog both are resolved in, nil for [gloss.Default].
//
// A link that carries its own token keeps the record free of presentation:
// the record's statement names a column, and the sentence says how the
// figure reads.
func ValueOf(cat *gloss.Catalog, records map[string]arrow.RecordBatch, record string, column string, token string) (v Value, err error) {
	rec, found := records[record]
	if !found {
		return v, eb.Build().Str("record", record).Errorf("the notebook has no record of that name")
	}
	if rec.NumRows() != 1 {
		return v, eb.Build().Str("record", record).Int64("rows", rec.NumRows()).Errorf("a value is read from a record of one row")
	}
	if cat == nil {
		cat = gloss.Default()
	}
	for i, f := range rec.Schema().Fields() {
		d, declared := cat.ParseColumn(f.Name)
		label := f.Name
		if declared {
			label = d.Label
		}
		if label != column {
			continue
		}
		cell := gloss.ArrowCell{Arr: rec.Column(i), Row: 0}
		v.ColumnName = f.Name
		if token != "" {
			_, _, instance, bErr := cat.BindToken(token)
			if bErr != nil {
				return v, eb.Build().Str("gloss", token).Errorf("the link's gloss: %w", bErr)
			}
			if ok, reason := instance.Accepts(cell.Kind()); !ok {
				return v, eb.Build().Str("gloss", token).Str("reason", reason).Errorf("the link's gloss does not take the column's values")
			}
			v.Face = instance.Inline(cell)
			return v, nil
		}
		if declared && d.Instance != nil {
			if ok, _ := d.Instance.Accepts(cell.Kind()); ok {
				v.Face = d.Instance.Inline(cell)
				return v, nil
			}
		}
		v.Face = gloss.Inline{Text: cell.Text()}
		return v, nil
	}
	return v, eb.Build().Str("record", record).Str("column", column).Errorf("the record has no column of that label")
}
