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
		// Scalars — every name is backtick-quoted, even a bare one.
		{"utf8", []arrow.Field{field("a", arrow.BinaryTypes.String)}, "`a` String"},
		{"binary", []arrow.Field{field("a", arrow.BinaryTypes.Binary)}, "`a` String"},
		{"bool", []arrow.Field{field("a", arrow.FixedWidthTypes.Boolean)}, "`a` Bool"},
		{"int8", []arrow.Field{field("a", arrow.PrimitiveTypes.Int8)}, "`a` Int8"},
		{"int16", []arrow.Field{field("a", arrow.PrimitiveTypes.Int16)}, "`a` Int16"},
		{"int32", []arrow.Field{field("a", arrow.PrimitiveTypes.Int32)}, "`a` Int32"},
		{"int64", []arrow.Field{field("a", arrow.PrimitiveTypes.Int64)}, "`a` Int64"},
		{"uint8", []arrow.Field{field("a", arrow.PrimitiveTypes.Uint8)}, "`a` UInt8"},
		{"uint16", []arrow.Field{field("a", arrow.PrimitiveTypes.Uint16)}, "`a` UInt16"},
		{"uint32", []arrow.Field{field("a", arrow.PrimitiveTypes.Uint32)}, "`a` UInt32"},
		{"uint64", []arrow.Field{field("a", arrow.PrimitiveTypes.Uint64)}, "`a` UInt64"},
		{"float32", []arrow.Field{field("a", arrow.PrimitiveTypes.Float32)}, "`a` Float32"},
		{"float64", []arrow.Field{field("a", arrow.PrimitiveTypes.Float64)}, "`a` Float64"},
		{"date32", []arrow.Field{field("a", arrow.FixedWidthTypes.Date32)}, "`a` Date32"},
		{"ts_us", []arrow.Field{field("a", &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"})}, "`a` DateTime64(6,'UTC')"},
		{"ts_ns", []arrow.Field{field("a", &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"})}, "`a` DateTime64(9,'UTC')"},

		// Timezone-naive timestamps (empty zone) map to a bare DateTime64(N).
		{"ts_us_naive", []arrow.Field{field("a", &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: ""})}, "`a` DateTime64(6)"},
		{"ts_ns_naive", []arrow.Field{field("a", &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: ""})}, "`a` DateTime64(9)"},

		// Fixed-size binary (e.g. a 16-byte hash/correlator) → FixedString(N).
		{"fixed_size_binary", []arrow.Field{field("h", &arrow.FixedSizeBinaryType{ByteWidth: 16})}, "`h` FixedString(16)"},

		// Nullable scalar leaves wrap in Nullable.
		{"nullable_scalar", []arrow.Field{{Name: "a", Type: arrow.BinaryTypes.String, Nullable: true}}, "`a` Nullable(String)"},
		{"nullable_int", []arrow.Field{{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true}}, "`a` Nullable(Int64)"},

		// Names that are not bare identifiers survive via quoting.
		{"colon_name", []arrow.Field{field("id:kid:u64:g:1hW82H8FG:0:", arrow.PrimitiveTypes.Uint64)}, "`id:kid:u64:g:1hW82H8FG:0:` UInt64"},
		{"dash_name", []arrow.Field{field("a-b", arrow.PrimitiveTypes.Int64)}, "`a-b` Int64"},
		{"space_name", []arrow.Field{field("a b", arrow.PrimitiveTypes.Int64)}, "`a b` Int64"},
		{"leading_digit_name", []arrow.Field{field("1a", arrow.PrimitiveTypes.Int64)}, "`1a` Int64"},
		{"backtick_name", []arrow.Field{field("a`b", arrow.PrimitiveTypes.Int64)}, "`a``b` Int64"},

		// Repeated sections — the leeway shape — become Array(T). ListOf marks
		// its element nullable; ListOfNonNullable does not.
		{"array_string", []arrow.Field{field("sec:s", arrow.ListOfNonNullable(arrow.BinaryTypes.String))}, "`sec:s` Array(String)"},
		{"array_uint64", []arrow.Field{field("sec:u", arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64))}, "`sec:u` Array(UInt64)"},
		{"array_nullable_elem", []arrow.Field{field("sec:n", arrow.ListOf(arrow.PrimitiveTypes.Int64))}, "`sec:n` Array(Nullable(Int64))"},
		{"large_list", []arrow.Field{field("sec:l", arrow.LargeListOfNonNullable(arrow.BinaryTypes.String))}, "`sec:l` Array(String)"},
		{"fixed_size_list", []arrow.Field{field("sec:f", arrow.FixedSizeListOfNonNullable(3, arrow.PrimitiveTypes.Float64))}, "`sec:f` Array(Float64)"},
		{"array_naive_ts", []arrow.Field{field("sec:et", arrow.ListOfNonNullable(&arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: ""}))}, "`sec:et` Array(DateTime64(9))"},
		{"array_fixed_binary", []arrow.Field{field("sec:h", arrow.ListOfNonNullable(&arrow.FixedSizeBinaryType{ByteWidth: 16}))}, "`sec:h` Array(FixedString(16))"},

		// Struct → named Tuple; nested names are quoted too.
		{"struct", []arrow.Field{field("s", arrow.StructOf(
			arrow.Field{Name: "x", Type: arrow.PrimitiveTypes.Int64},
			arrow.Field{Name: "y:sub", Type: arrow.BinaryTypes.String},
		))}, "`s` Tuple(`x` Int64, `y:sub` String)"},
		{"struct_nullable_field", []arrow.Field{field("s", arrow.StructOf(
			arrow.Field{Name: "x", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		))}, "`s` Tuple(`x` Nullable(Int64))"},

		// Array of Struct — a co-section shape.
		{"array_of_struct", []arrow.Field{field("sec:t", arrow.ListOfNonNullable(arrow.StructOf(
			arrow.Field{Name: "k", Type: arrow.BinaryTypes.String},
			arrow.Field{Name: "v", Type: arrow.PrimitiveTypes.Uint64},
		)))}, "`sec:t` Array(Tuple(`k` String, `v` UInt64))"},

		// Map → Map(K,V); MapOf marks the value nullable.
		{"map", []arrow.Field{field("m", arrow.MapOf(arrow.BinaryTypes.String, arrow.PrimitiveTypes.Uint64))}, "`m` Map(String, Nullable(UInt64))"},

		// A representative multi-column leeway-shaped row.
		{"multi", []arrow.Field{
			field("id:kid:u64", arrow.PrimitiveTypes.Uint64),
			field("name", arrow.BinaryTypes.String),
			field("sec:tags", arrow.ListOfNonNullable(arrow.BinaryTypes.String)),
			{Name: "note", Type: arrow.BinaryTypes.String, Nullable: true},
			field("ts", &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}),
		}, "`id:kid:u64` UInt64, `name` String, `sec:tags` Array(String), `note` Nullable(String), `ts` DateTime64(6,'UTC')"},
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
		// Types outside the supported set — still refused at publish.
		{"dictionary", field("a", &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String})},
		{"large_string", field("a", arrow.BinaryTypes.LargeString)},
		{"ts_millis", field("a", &arrow.TimestampType{Unit: arrow.Millisecond, TimeZone: "UTC"})},
		{"ts_seconds", field("a", &arrow.TimestampType{Unit: arrow.Second, TimeZone: "UTC"})},
		{"ts_non_utc", field("a", &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "America/New_York"})},

		// Unsupported types nested inside a supported container are still
		// caught, naming the top-level column.
		{"array_of_unsupported", field("a", arrow.ListOfNonNullable(arrow.BinaryTypes.LargeString))},
		{"struct_of_unsupported", field("a", arrow.StructOf(arrow.Field{Name: "x", Type: arrow.BinaryTypes.LargeString}))},

		// Name hygiene — quoting handles arbitrary bytes, but an empty name
		// and an over-long name are still refused.
		{"name_empty", field("", arrow.PrimitiveTypes.Int64)},
		{"name_too_long", field(longName, arrow.PrimitiveTypes.Int64)},
		{"nested_name_empty", field("s", arrow.StructOf(arrow.Field{Name: "", Type: arrow.PrimitiveTypes.Int64}))},
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
