package adhocdata

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
)

func field(name string, t arrow.DataType) arrow.Field {
	return arrow.Field{Name: name, Type: t}
}

func TestStructureForSupported(t *testing.T) {
	cases := []struct {
		name   string
		fields []arrow.Field
		want   string
	}{
		{"utf8", []arrow.Field{field("a", arrow.BinaryTypes.String)}, "a String"},
		{"binary", []arrow.Field{field("a", arrow.BinaryTypes.Binary)}, "a String"},
		{"bool", []arrow.Field{field("a", arrow.FixedWidthTypes.Boolean)}, "a Bool"},
		{"int8", []arrow.Field{field("a", arrow.PrimitiveTypes.Int8)}, "a Int8"},
		{"int16", []arrow.Field{field("a", arrow.PrimitiveTypes.Int16)}, "a Int16"},
		{"int32", []arrow.Field{field("a", arrow.PrimitiveTypes.Int32)}, "a Int32"},
		{"int64", []arrow.Field{field("a", arrow.PrimitiveTypes.Int64)}, "a Int64"},
		{"uint8", []arrow.Field{field("a", arrow.PrimitiveTypes.Uint8)}, "a UInt8"},
		{"uint16", []arrow.Field{field("a", arrow.PrimitiveTypes.Uint16)}, "a UInt16"},
		{"uint32", []arrow.Field{field("a", arrow.PrimitiveTypes.Uint32)}, "a UInt32"},
		{"uint64", []arrow.Field{field("a", arrow.PrimitiveTypes.Uint64)}, "a UInt64"},
		{"float32", []arrow.Field{field("a", arrow.PrimitiveTypes.Float32)}, "a Float32"},
		{"float64", []arrow.Field{field("a", arrow.PrimitiveTypes.Float64)}, "a Float64"},
		{"date32", []arrow.Field{field("a", arrow.FixedWidthTypes.Date32)}, "a Date32"},
		{"ts_us", []arrow.Field{field("a", &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"})}, "a DateTime64(6,'UTC')"},
		{"ts_ns", []arrow.Field{field("a", &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"})}, "a DateTime64(9,'UTC')"},
		{"multi", []arrow.Field{
			field("id", arrow.PrimitiveTypes.Int64),
			field("name", arrow.BinaryTypes.String),
			field("ts", &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}),
		}, "id Int64, name String, ts DateTime64(6,'UTC')"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := StructureFor(arrow.NewSchema(tc.fields, nil))
			if err != nil {
				t.Fatalf("StructureFor: %v", err)
			}
			if got != tc.want {
				t.Fatalf("StructureFor = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStructureForRejected(t *testing.T) {
	longName := strings.Repeat("x", maxColumnNameLen+1)
	cases := []struct {
		name  string
		field arrow.Field
	}{
		{"nullable", arrow.Field{Name: "a", Type: arrow.BinaryTypes.String, Nullable: true}},
		{"list", field("a", arrow.ListOf(arrow.PrimitiveTypes.Int64))},
		{"struct", field("a", arrow.StructOf(arrow.Field{Name: "x", Type: arrow.PrimitiveTypes.Int64}))},
		{"dictionary", field("a", &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String})},
		{"large_string", field("a", arrow.BinaryTypes.LargeString)},
		{"ts_millis", field("a", &arrow.TimestampType{Unit: arrow.Millisecond, TimeZone: "UTC"})},
		{"ts_seconds", field("a", &arrow.TimestampType{Unit: arrow.Second, TimeZone: "UTC"})},
		{"ts_non_utc", field("a", &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "America/New_York"})},
		{"ts_no_tz", field("a", &arrow.TimestampType{Unit: arrow.Microsecond})},
		{"name_leading_digit", field("1a", arrow.PrimitiveTypes.Int64)},
		{"name_dash", field("a-b", arrow.PrimitiveTypes.Int64)},
		{"name_space", field("a b", arrow.PrimitiveTypes.Int64)},
		{"name_empty", field("", arrow.PrimitiveTypes.Int64)},
		{"name_too_long", field(longName, arrow.PrimitiveTypes.Int64)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := StructureFor(arrow.NewSchema([]arrow.Field{tc.field}, nil)); err == nil {
				t.Fatalf("StructureFor(%s) must error", tc.name)
			}
		})
	}
}

func TestStructureForEmptyAndNil(t *testing.T) {
	if _, err := StructureFor(nil); err == nil {
		t.Fatal("nil schema must error")
	}
	if _, err := StructureFor(arrow.NewSchema(nil, nil)); err == nil {
		t.Fatal("empty schema must error")
	}
}
