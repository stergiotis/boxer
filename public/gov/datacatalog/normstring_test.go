package datacatalog_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
)

func TestNormalizeType(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"String", "String"},
		{"UInt64", "UInt64"},
		{"DateTime64(3)", "DateTime64(3)"},
		{"DateTime64(3, 'Europe/Berlin')", "DateTime64(3, 'Europe/Berlin')"},
		{"LowCardinality(String)", "String"},
		{"Nullable(String)", "String?"},
		// The nesting ClickHouse actually writes, and its mirror.
		{"LowCardinality(Nullable(String))", "String?"},
		{"Nullable(LowCardinality(String))", "String?"},
		// Doubly wrapped: the strip loops, so one pass is not assumed.
		{"LowCardinality(LowCardinality(String))", "String"},
		// Top-level only — descending would need a real CH type parser.
		{"Array(Nullable(String))", "Array(Nullable(String))"},
		{"Array(LowCardinality(String))", "Array(LowCardinality(String))"},
		// The trailing `)` does not close the leading `(`: not a Nullable.
		{"Tuple(a Nullable(UInt8), b UInt8)", "Tuple(a Nullable(UInt8), b UInt8)"},
		// A parenthesis inside an Enum literal must not unbalance the scan.
		{"LowCardinality(Enum8('a(' = 1, 'b' = 2))", "Enum8('a(' = 1, 'b' = 2)"},
		{"Enum8('a)' = 1)", "Enum8('a)' = 1)"},
		// Not a constructor call, merely a prefix.
		{"NullableThing", "NullableThing"},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equalf(t, c.want, datacatalog.NormalizeType(c.in), "NormalizeType(%q)", c.in)
	}
}

func TestNormalizedSchema_Shape(t *testing.T) {
	cols := []datacatalog.ColumnMeta{
		{Name: "ts", Type: "DateTime64(3)", Position: 1},
		{Name: "label", Type: "LowCardinality(Nullable(String))", Position: 2},
		{Name: "value", Type: "Float64", Position: 3},
	}
	assert.Equal(t, ";ts:DateTime64(3);label:String?;value:Float64;", datacatalog.NormalizedSchema(cols))
}

// A table with no columns is the bare sentinel rather than the empty string, so
// a pattern anchored on `;` still has something to fail against.
func TestNormalizedSchema_NoColumns(t *testing.T) {
	assert.Equal(t, ";", datacatalog.NormalizedSchema(nil))
	assert.Equal(t, ";", datacatalog.NormalizedSchema([]datacatalog.ColumnMeta{}))
}

func TestNormalizedSchema_Escaping(t *testing.T) {
	cols := []datacatalog.ColumnMeta{
		{Name: "geoPoint:lat", Type: "Float64"},
		{Name: "a;b", Type: "String"},
		{Name: `back\slash`, Type: "String"},
	}
	assert.Equal(t, `;geoPoint\:lat:Float64;a\;b:String;back\\slash:String;`, datacatalog.NormalizedSchema(cols))
}

// The escape rule reaches the type too: an Enum literal carrying a `;` would
// otherwise split the string into a column that does not exist.
func TestNormalizedSchema_EscapesTypeSeparators(t *testing.T) {
	cols := []datacatalog.ColumnMeta{{Name: "k", Type: "Enum8('a;b' = 1)"}}
	assert.Equal(t, `;k:Enum8('a\;b' = 1);`, datacatalog.NormalizedSchema(cols))
}

// Position order is the caller's to establish; NormalizedSchema renders what it
// is given, because for an opaque table column order is part of the shape.
func TestNormalizedSchema_PreservesGivenOrder(t *testing.T) {
	cols := []datacatalog.ColumnMeta{
		{Name: "b", Type: "String", Position: 2},
		{Name: "a", Type: "String", Position: 1},
	}
	assert.Equal(t, ";b:String;a:String;", datacatalog.NormalizedSchema(cols))
}
