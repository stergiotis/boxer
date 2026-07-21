package play

import (
	"testing"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// recOfColumn wraps a single Arrow array in a one-column record batch.
func recOfColumn(name string, arr arrow.Array) arrow.RecordBatch {
	schema := arrow.NewSchema([]arrow.Field{{Name: name, Type: arr.DataType()}}, nil)
	rec := array.NewRecord(schema, []arrow.Array{arr}, int64(arr.Len()))
	return rec
}

func TestFormatCellScalars(t *testing.T) {
	mem := memory.NewGoAllocator()

	ib := array.NewInt64Builder(mem)
	ib.Append(42)
	ib.AppendNull()
	intArr := ib.NewArray()
	defer intArr.Release()
	rec := recOfColumn("n", intArr)
	defer rec.Release()

	if got := formatCell(rec, 0, 0); got != "42" {
		t.Errorf("int64 value = %q, want 42", got)
	}
	if got := formatCell(rec, 0, 1); got != "" {
		t.Errorf("null = %q, want empty", got)
	}
	if got := formatCell(rec, 0, 99); got != "" {
		t.Errorf("out-of-range row = %q, want empty", got)
	}

	bb := array.NewBooleanBuilder(mem)
	bb.Append(true)
	bb.Append(false)
	boolArr := bb.NewArray()
	defer boolArr.Release()
	brec := recOfColumn("b", boolArr)
	defer brec.Release()
	if got := formatCell(brec, 0, 0); got != "true" {
		t.Errorf("bool true = %q", got)
	}
	if got := formatCell(brec, 0, 1); got != "false" {
		t.Errorf("bool false = %q", got)
	}
}

// Regression for M1: a dictionary-encoded String (ClickHouse LowCardinality)
// whose value is not valid UTF-8 must come back sanitised, never raw — the
// raw bytes would break the FFI wire downstream of c.Label.
func TestFormatCellDictionaryInvalidUTF8(t *testing.T) {
	mem := memory.NewGoAllocator()
	dictType := &arrow.DictionaryType{
		IndexType: arrow.PrimitiveTypes.Int8,
		ValueType: arrow.BinaryTypes.String,
	}
	db := array.NewDictionaryBuilder(mem, dictType).(*array.BinaryDictionaryBuilder)
	if err := db.AppendString("ok"); err != nil {
		t.Fatal(err)
	}
	if err := db.Append([]byte{0xff, 0xfe}); err != nil { // invalid UTF-8
		t.Fatal(err)
	}
	arr := db.NewArray()
	defer arr.Release()
	rec := recOfColumn("d", arr)
	defer rec.Release()

	if got := formatCell(rec, 0, 0); got != "ok" {
		t.Errorf("valid dict value = %q, want ok", got)
	}
	got := formatCell(rec, 0, 1)
	if !utf8.ValidString(got) {
		t.Errorf("invalid-UTF-8 dict value rendered as %q — must be sanitised valid UTF-8", got)
	}
}

// stringLikeArrowType drives the Table pane's left-alignment: string-like
// columns line up under their left-aligned headers; everything else stays
// centered.
func TestStringLikeArrowType(t *testing.T) {
	dictStr := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int8, ValueType: arrow.BinaryTypes.String}
	dictLarge := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int8, ValueType: arrow.BinaryTypes.LargeString}
	dictInt := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int8, ValueType: arrow.PrimitiveTypes.Int64}

	cases := []struct {
		name string
		dt   arrow.DataType
		want bool
	}{
		{"string", arrow.BinaryTypes.String, true},
		{"large_string", arrow.BinaryTypes.LargeString, true},
		{"dict_string", dictStr, true}, // ClickHouse LowCardinality(String)
		{"dict_large_string", dictLarge, true},
		{"dict_int", dictInt, false},
		{"int64", arrow.PrimitiveTypes.Int64, false},
		{"float64", arrow.PrimitiveTypes.Float64, false},
		{"binary", arrow.BinaryTypes.Binary, false}, // rendered as hex, reads like a number
		// A List column shows a packed "[len=N]" marker in the per-DB-row grid,
		// not the strings themselves, so it is not left-aligned there.
		{"list_string", arrow.ListOf(arrow.BinaryTypes.String), false},
	}
	for _, tc := range cases {
		if got := stringLikeArrowType(tc.dt); got != tc.want {
			t.Errorf("stringLikeArrowType(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// listElemType unwraps a list to its element so the per-attribute view — which
// explodes each list value to its inner scalars — classifies alignment against
// what the exploded cell actually shows.
func TestListElemType(t *testing.T) {
	// A non-list type passes through unchanged.
	if got := listElemType(arrow.BinaryTypes.String); got.ID() != arrow.STRING {
		t.Errorf("non-list passthrough = %s, want string", got)
	}
	// Every list flavour unwraps to its element; a String value column reads
	// string-like once exploded.
	for _, lt := range []arrow.DataType{
		arrow.ListOf(arrow.BinaryTypes.String),
		arrow.LargeListOf(arrow.BinaryTypes.String),
		arrow.FixedSizeListOf(2, arrow.BinaryTypes.String),
	} {
		if !stringLikeArrowType(listElemType(lt)) {
			t.Errorf("listElemType(%s) not classified string-like", lt)
		}
	}
	// A List<Int64> value column stays numeric once exploded.
	if stringLikeArrowType(listElemType(arrow.ListOf(arrow.PrimitiveTypes.Int64))) {
		t.Errorf("List<int64> element classified string-like, want numeric")
	}
}
