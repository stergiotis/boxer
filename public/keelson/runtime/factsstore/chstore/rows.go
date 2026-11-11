package chstore

import (
	"context"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The reads of this store arrive as Arrow (ADR-0367, update of 2026-11-11):
// a statement carries no FORMAT, the executor asks for record batches, and a
// value is read from a typed column. Nothing here splits or unescapes text.

// row is one row of a result batch. A column read that finds a type it does
// not understand records the first such error and yields the zero value;
// eachRow reports it, so a caller reads a whole row without checking each
// field.
type row struct {
	rec arrow.RecordBatch
	i   int
	err *error
}

func (inst row) fail(c int, want string) {
	if *inst.err == nil {
		*inst.err = eb.Build().Int("column", c).Str("name", inst.rec.ColumnName(c)).
			Str("type", inst.rec.Column(c).DataType().String()).Str("want", want).
			Errorf("chstore: result column has an unexpected type")
	}
}

// str reads a string column; null reads as "".
func (inst row) str(c int) (v string) {
	v, ok := stringAt(inst.rec.Column(c), inst.i)
	if !ok {
		inst.fail(c, "string")
	}
	return
}

// u64 reads an unsigned integer column of any width; null reads as 0.
func (inst row) u64(c int) (v uint64) {
	arr := inst.rec.Column(c)
	if arr.IsNull(inst.i) {
		return
	}
	switch a := arr.(type) {
	case *array.Uint64:
		v = a.Value(inst.i)
	case *array.Uint32:
		v = uint64(a.Value(inst.i))
	case *array.Uint16:
		v = uint64(a.Value(inst.i))
	case *array.Uint8:
		v = uint64(a.Value(inst.i))
	default:
		inst.fail(c, "unsigned integer")
	}
	return
}

// i64 reads an integer column of any width and signedness; null reads as 0.
func (inst row) i64(c int) (v int64) {
	arr := inst.rec.Column(c)
	if arr.IsNull(inst.i) {
		return
	}
	switch a := arr.(type) {
	case *array.Int64:
		v = a.Value(inst.i)
	case *array.Int32:
		v = int64(a.Value(inst.i))
	case *array.Int16:
		v = int64(a.Value(inst.i))
	case *array.Int8:
		v = int64(a.Value(inst.i))
	case *array.Uint64:
		v = int64(a.Value(inst.i))
	case *array.Uint32:
		v = int64(a.Value(inst.i))
	case *array.Uint16:
		v = int64(a.Value(inst.i))
	case *array.Uint8:
		v = int64(a.Value(inst.i))
	default:
		inst.fail(c, "integer")
	}
	return
}

// f64 reads a floating-point column; null reads as 0.
func (inst row) f64(c int) (v float64) {
	arr := inst.rec.Column(c)
	if arr.IsNull(inst.i) {
		return
	}
	switch a := arr.(type) {
	case *array.Float64:
		v = a.Value(inst.i)
	case *array.Float32:
		v = float64(a.Value(inst.i))
	default:
		inst.fail(c, "float")
	}
	return
}

// unix reads unix seconds; 0 is the zero time, not the epoch, because the
// aggregates write 0 for "no such row".
func (inst row) unix(c int) (t time.Time) {
	if s := inst.i64(c); s != 0 {
		t = time.Unix(s, 0).UTC()
	}
	return
}

// stringLists reads a column of lists of string lists.
func (inst row) stringLists(c int) (out [][]string) {
	outer, ok := inst.rec.Column(c).(*array.List)
	if !ok {
		inst.fail(c, "list of string lists")
		return
	}
	inner, ok := outer.ListValues().(*array.List)
	if !ok {
		inst.fail(c, "list of string lists")
		return
	}
	lo, hi := outer.ValueOffsets(inst.i)
	out = make([][]string, 0, hi-lo)
	for j := int(lo); j < int(hi); j++ {
		a, b := inner.ValueOffsets(j)
		item := make([]string, 0, b-a)
		for k := int(a); k < int(b); k++ {
			s, isStr := stringAt(inner.ListValues(), k)
			if !isStr {
				inst.fail(c, "list of string lists")
				return
			}
			item = append(item, s)
		}
		out = append(out, item)
	}
	return
}

// stringAt reads a string from the encodings a server may choose for one:
// string or binary, either offset width, or a dictionary of those.
func stringAt(arr arrow.Array, i int) (v string, ok bool) {
	if arr.IsNull(i) {
		return "", true
	}
	switch a := arr.(type) {
	case *array.String:
		return a.Value(i), true
	case *array.LargeString:
		return a.Value(i), true
	case *array.Binary:
		return string(a.Value(i)), true
	case *array.LargeBinary:
		return string(a.Value(i)), true
	case *array.Dictionary:
		return stringAt(a.Dictionary(), a.GetValueIndex(i))
	}
	return "", false
}

// eachRow calls fn for every row of rec, which must have exactly cols
// columns. A wrong column count or a column of an unexpected type fails the
// read: a table silently missing rows is worse than one that says it could
// not be read.
func eachRow(rec arrow.RecordBatch, what string, cols int, fn func(r row)) (err error) {
	if int(rec.NumCols()) != cols {
		return eb.Build().Str("read", what).Int64("got", rec.NumCols()).Int("want", cols).
			Errorf("chstore: result has the wrong column count")
	}
	for i := 0; i < int(rec.NumRows()) && err == nil; i++ {
		fn(row{rec: rec, i: i, err: &err})
	}
	if err != nil {
		err = eb.Build().Str("read", what).Errorf("chstore: read row: %w", err)
	}
	return
}

// readRows expands authored, runs it and calls fn for every row.
func (inst *Store) readRows(ctx context.Context, what string, authored string, cols int, fn func(r row)) (err error) {
	sql, err := prepare(authored, inst.qualifiedTable())
	if err != nil {
		return eb.Build().Str("read", what).Errorf("chstore: compose: %w", err)
	}
	for rec, qerr := range inst.exec.QueryArrow(ctx, sql) {
		if qerr != nil {
			return eb.Build().Str("read", what).Errorf("chstore: query: %w", qerr)
		}
		err = eachRow(rec, what, cols, fn)
		rec.Release()
		if err != nil {
			return
		}
	}
	return
}
