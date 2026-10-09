package introspectengine

import (
	"bytes"
	"context"
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/trivialsql"
)

// mixProvider holds a column of every type the trivial evaluator's text
// formats render, with the values whose spelling is easiest to get wrong.
type mixProvider struct{}

var mixSchema = arrow.NewSchema([]arrow.Field{
	{Name: "i8", Type: arrow.PrimitiveTypes.Int8},
	{Name: "u8", Type: arrow.PrimitiveTypes.Uint8},
	{Name: "i64", Type: arrow.PrimitiveTypes.Int64},
	{Name: "u64", Type: arrow.PrimitiveTypes.Uint64},
	{Name: "f32", Type: arrow.PrimitiveTypes.Float32},
	{Name: "f64", Type: arrow.PrimitiveTypes.Float64},
	{Name: "b", Type: arrow.FixedWidthTypes.Boolean},
	{Name: "s", Type: arrow.BinaryTypes.String},
	{Name: "ls", Type: arrow.BinaryTypes.LargeString},
	{Name: "bin", Type: arrow.BinaryTypes.Binary},
	{Name: "ns", Type: arrow.BinaryTypes.String, Nullable: true},
	{Name: "sl", Type: arrow.ListOf(arrow.BinaryTypes.String)},
	{Name: "fl", Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Float64)},
	{Name: "d", Type: &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String}},
}, nil)

var (
	mixFloats  = []float64{0.1, math.Copysign(0, -1), 1e20, 1e21, 1e-6, 1e-7, 2.0 / 3, 1.2345678901234567e25, 5e-324, math.Inf(1), math.Inf(-1), math.NaN()}
	mixStrings = []string{"", "it's \"q\"", "a\tb\nc\rd", "x\\y/z", "\x00\b\f\v\x1f\x7f", "é<>&", "plain", "a,b", "\xff\xfe", "LIKE 100\\%", "'", "\""}
)

func (mixProvider) Name() string                         { return "mix" }
func (mixProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (mixProvider) Schema() *arrow.Schema                { return mixSchema }
func (mixProvider) Snapshot(introspect.Projection) (arrow.RecordBatch, error) {
	b := array.NewRecordBuilder(memory.DefaultAllocator, mixSchema)
	defer b.Release()
	for i, f := range mixFloats {
		s := mixStrings[i]
		b.Field(0).(*array.Int8Builder).Append(int8(-128 + i))
		b.Field(1).(*array.Uint8Builder).Append(uint8(255 - i))
		b.Field(2).(*array.Int64Builder).Append(math.MinInt64 + int64(i))
		b.Field(3).(*array.Uint64Builder).Append(math.MaxUint64 - uint64(i))
		b.Field(4).(*array.Float32Builder).Append(float32(f))
		b.Field(5).(*array.Float64Builder).Append(f)
		b.Field(6).(*array.BooleanBuilder).Append(i%2 == 0)
		b.Field(7).(*array.StringBuilder).Append(s)
		b.Field(8).(*array.LargeStringBuilder).Append(s)
		b.Field(9).(*array.BinaryBuilder).Append([]byte(s))
		if i%3 == 0 {
			b.Field(10).(*array.StringBuilder).AppendNull()
		} else {
			b.Field(10).(*array.StringBuilder).Append(s)
		}
		lb := b.Field(11).(*array.ListBuilder)
		lb.Append(true)
		lb.ValueBuilder().(*array.StringBuilder).Append(s)
		lb.ValueBuilder().(*array.StringBuilder).AppendNull()
		fb := b.Field(12).(*array.ListBuilder)
		fb.Append(true)
		fb.ValueBuilder().(*array.Float64Builder).AppendValues([]float64{f, 1.5}, nil)
		if err := b.Field(13).(*array.BinaryDictionaryBuilder).AppendString([]string{"red", "it's"}[i%2]); err != nil {
			return nil, err
		}
	}
	return b.NewRecordBatch(), nil
}

// A statement the trivial evaluator accepts answers the same through it as
// through clickhouse-local (ADR-0290: one statement text in every host): the
// text formats byte for byte, ArrowStream in values.
func TestTrivialSQL_AnswersAsClickHouseDoes(t *testing.T) {
	e := newEngineWithBroker(t)
	require.NoError(t, e.reg.Register(seqProvider{}))
	require.NoError(t, e.reg.Register(mixProvider{}))
	params := map[string]string{"k": "4"}
	for _, sql := range []string{
		"SELECT * FROM keelson('seq', n = 3) FORMAT TabSeparated",
		"SELECT * FROM keelson('seq', n = {k:UInt64}) LIMIT 2 OFFSET 1 FORMAT TabSeparated",
		"WITH vf AS (SELECT * FROM keelson('seq', n = {k:UInt64})) SELECT * FROM vf LIMIT 1, 2 FORMAT TabSeparated",
		"SET param_k = 2; SELECT * FROM keelson('seq', n = {k:UInt64}) FORMAT TabSeparated",
		"SELECT * FROM keelson('seq', n = 3) LIMIT 1, 9223372036854775807 FORMAT TabSeparated",
		"SELECT * FROM keelson('seq', n = 2) FORMAT JSONEachRow",
		"SELECT * FROM keelson('mix') FORMAT TabSeparated",
		"SELECT * FROM keelson('mix') FORMAT TabSeparatedWithNames",
		"SELECT * FROM keelson('mix') FORMAT CSV",
		"SELECT * FROM keelson('mix') FORMAT CSVWithNames",
		"SELECT * FROM keelson('mix') FORMAT JSONEachRow",
		"SELECT * FROM keelson('mix') FORMAT tsv",
		// Inline constants and tables (ADR-0290 §SD3).
		"SELECT 1 AS a, 'x' AS b FORMAT TabSeparatedWithNames",
		"SELECT 1, 255, 256, 65536, 4294967296, -1, -128, -129, -0, 1.50, 1e3, 0x10, 'it''s \\t', TRUE, false, 0.1, -0.0, 1e21, 1e-7, 18446744073709551615, -9223372036854775808 FORMAT TabSeparatedWithNames",
		"SELECT 1, -1, 1.5, 'x', true FORMAT JSONEachRow",
		"SELECT 1 AS a, 'x' AS b, 2.5 AS c FORMAT CSVWithNames",
		"SELECT {k:UInt64} AS k, {k:String} AS s, {k:Float32} AS f FORMAT TabSeparatedWithNames",
		"SELECT *, 'storm' AS src, 7 AS run FROM keelson('seq', n = 2) FORMAT TabSeparatedWithNames",
		"SELECT 7 AS k, * FROM keelson('mix') LIMIT 2 FORMAT JSONEachRow",
		"SELECT 0 AS z FROM keelson('seq', n = 3) FORMAT TabSeparatedWithNames",
		"WITH 2 AS k SELECT * FROM keelson('seq', n = k) FORMAT TabSeparatedWithNames",
		"WITH 2 AS k, k AS j, 'w' AS s SELECT k, j, s, k AS kk FORMAT TabSeparatedWithNames",
		"WITH vf AS (SELECT *, 9 AS nine FROM keelson('seq', n = 1)) SELECT * FROM vf FORMAT TabSeparatedWithNames",
		"WITH a AS (SELECT * FROM keelson('seq', n = 3)), b AS (SELECT *, 1 AS one FROM a) SELECT * FROM b LIMIT 1, 1 FORMAT TabSeparatedWithNames",
		"SELECT 1 AS a LIMIT 0 FORMAT TabSeparated",
		"SELECT * FROM values('a UInt8, b String', (1, 'x'), (2, 'y')) FORMAT TabSeparatedWithNames",
		"SELECT * FROM values('a Nullable(UInt8)', NULL, 3, (4)) FORMAT JSONEachRow",
		"SELECT * FROM values('a Float32, b Bool, c String, d UInt8, e Int16', (1, 0, 5, '7', -300), (2.0, 1, 'q', 8.0, 0x10)) FORMAT TabSeparatedWithNames",
		"SELECT * FROM values((1, -1.5), (300, 3)) FORMAT TabSeparatedWithNames",
		"SELECT * FROM values(255, -1, NULL) FORMAT JSONEachRow",
		"SELECT * FROM values((4294967295, 'a'), (-1, 'b')) FORMAT TabSeparatedWithNames",
		"SELECT * FROM values(1.5, 2) FORMAT TabSeparatedWithNames",
		"SELECT * FROM values('it''s', 'x\\ty') FORMAT TabSeparatedWithNames",
		"WITH 'a UInt8' AS s, 4 AS k SELECT *, 'v' AS t FROM values(s, k, {k:UInt8}) FORMAT TabSeparatedWithNames",
		"WITH t AS (SELECT * FROM values('a String', 'p', 'q')) SELECT * FROM t LIMIT 1, 1 FORMAT TabSeparated",
		"SET param_k = 3; SELECT {k:UInt8} AS k, * FROM values('a UInt8', {k:UInt8}) FORMAT TabSeparatedWithNames",
	} {
		want, _, err := e.QueryParams(context.Background(), sql, "", params)
		require.NoError(t, err, sql)
		got, err := trivialsql.Run(context.Background(), e.reg, sql, params)
		require.NoError(t, err, sql)
		assert.Equal(t, string(want), string(got), sql)
	}

	// ArrowStream: ClickHouse maps the types (a String comes back
	// LargeBinary), so the two records are compared through one rendering.
	const sql = "SELECT * FROM keelson('mix') FORMAT ArrowStream"
	want, _, err := e.QueryParams(context.Background(), sql, "", nil)
	require.NoError(t, err)
	got, err := trivialsql.Run(context.Background(), e.reg, sql, nil)
	require.NoError(t, err)
	assert.Equal(t, asTSV(t, want), asTSV(t, got))
}

func asTSV(t *testing.T, stream []byte) string {
	t.Helper()
	r, err := ipc.NewReader(bytes.NewReader(stream))
	require.NoError(t, err)
	defer r.Release()
	var out bytes.Buffer
	for r.Next() {
		b, err := trivialsql.Encode(r.RecordBatch(), trivialsql.FormatTabSeparatedWithNames)
		require.NoError(t, err)
		out.Write(b)
	}
	require.NoError(t, r.Err())
	return out.String()
}
