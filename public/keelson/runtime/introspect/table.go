package introspect

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// colSpec is one column: its Arrow field plus a per-row appender that
// writes row i's value into the (already type-matched) builder.
type colSpec struct {
	field    arrow.Field
	appendFn func(b array.Builder, row int)
}

// Table is a column-oriented description of a provider's table: declare
// each column once with its Arrow type and a row getter, then Build
// emits an Arrow record for a given Projection (ADR-0094 §SD2). The
// same Table yields both the full Schema (for `SELECT *` expansion) and
// the projected record (for snapshotting), so the two never drift.
//
// A provider builds its Table from a snapshot of its data — the getter
// closures capture that data and are invoked only during Build. To
// expose the schema without live data, build the Table over a nil/empty
// dataset: Schema reads the declared fields and never calls a getter.
type Table struct {
	cols []colSpec
}

// NewTable returns an empty Table; chain the typed column declarations.
func NewTable() *Table { return &Table{} }

func (t *Table) add(field arrow.Field, appendFn func(array.Builder, int)) *Table {
	t.cols = append(t.cols, colSpec{field: field, appendFn: appendFn})
	return t
}

// String declares a non-nullable Utf8 column filled by get.
func (t *Table) String(name string, get func(row int) string) *Table {
	return t.add(arrow.Field{Name: name, Type: arrow.BinaryTypes.String},
		func(b array.Builder, i int) { b.(*array.StringBuilder).Append(get(i)) })
}

// Int32 declares a non-nullable Int32 column filled by get.
func (t *Table) Int32(name string, get func(row int) int32) *Table {
	return t.add(arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Int32},
		func(b array.Builder, i int) { b.(*array.Int32Builder).Append(get(i)) })
}

// Int64 declares a non-nullable Int64 column filled by get.
func (t *Table) Int64(name string, get func(row int) int64) *Table {
	return t.add(arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Int64},
		func(b array.Builder, i int) { b.(*array.Int64Builder).Append(get(i)) })
}

// Bool declares a non-nullable Boolean column filled by get.
func (t *Table) Bool(name string, get func(row int) bool) *Table {
	return t.add(arrow.Field{Name: name, Type: arrow.FixedWidthTypes.Boolean},
		func(b array.Builder, i int) { b.(*array.BooleanBuilder).Append(get(i)) })
}

// StringList declares an Array(String) column with non-nullable
// elements (so ClickHouse reads it as Array(String)) filled by get.
func (t *Table) StringList(name string, get func(row int) []string) *Table {
	return t.add(arrow.Field{Name: name, Type: arrow.ListOfNonNullable(arrow.BinaryTypes.String)},
		func(b array.Builder, i int) {
			lb := b.(*array.ListBuilder)
			lb.Append(true)
			vb := lb.ValueBuilder().(*array.StringBuilder)
			for _, s := range get(i) {
				vb.Append(s)
			}
		})
}

// Schema returns the full, unprojected Arrow schema in declaration
// order.
func (t *Table) Schema() *arrow.Schema {
	fields := make([]arrow.Field, len(t.cols))
	for i, c := range t.cols {
		fields[i] = c.field
	}
	return arrow.NewSchema(fields, nil)
}

// Build materialises nrows rows under proj. Columns the projection does
// not want are skipped; if the projection selects none of this table's
// columns (e.g. SELECT count(*)), every column is emitted so the result
// is never a zero-column table. The caller must Release the batch.
func (t *Table) Build(proj Projection, nrows int) arrow.RecordBatch {
	sel := make([]colSpec, 0, len(t.cols))
	for _, c := range t.cols {
		if proj.wants(c.field.Name) {
			sel = append(sel, c)
		}
	}
	if len(sel) == 0 {
		sel = t.cols
	}
	fields := make([]arrow.Field, len(sel))
	for i, c := range sel {
		fields[i] = c.field
	}
	rb := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema(fields, nil))
	defer rb.Release()
	for i := range nrows {
		for j := range sel {
			sel[j].appendFn(rb.Field(j), i)
		}
	}
	return rb.NewRecordBatch()
}
