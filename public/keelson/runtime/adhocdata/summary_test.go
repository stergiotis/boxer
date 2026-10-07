package adhocdata

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/sealed"
)

// summarise seals the stream build writes, in batches of batch rows, and
// returns the summaries the seal produced.
func summarise(t *testing.T, schema *arrow.Schema, batches ...func(rb *array.RecordBuilder)) (cols ColumnSummaries) {
	t.Helper()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	for _, fill := range batches {
		rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
		fill(rb)
		rec := rb.NewRecordBatch()
		require.NoError(t, w.Write(rec))
		rec.Release()
		rb.Release()
	}
	require.NoError(t, w.Close())
	f, err := sealed.CreateIn(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	ss, err := sealStream(f, buf.Bytes())
	require.NoError(t, err)
	return ss.columns
}

func TestSummariesAcrossBatches(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "n", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "u", Type: arrow.PrimitiveTypes.Uint8},
		{Name: "s", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	cols := summarise(t, schema,
		func(rb *array.RecordBuilder) {
			rb.Field(0).(*array.Int64Builder).AppendValues([]int64{5, 0, -3}, []bool{true, false, true})
			rb.Field(1).(*array.Uint8Builder).AppendValues([]uint8{9, 9, 200}, nil)
			rb.Field(2).(*array.StringBuilder).AppendValues([]string{"b", "", "a\"q"}, []bool{true, true, true})
		},
		func(rb *array.RecordBuilder) {
			rb.Field(0).(*array.Int64Builder).AppendValues([]int64{12}, nil)
			rb.Field(1).(*array.Uint8Builder).AppendValues([]uint8{1}, nil)
			rb.Field(2).(*array.StringBuilder).AppendNull()
		})
	assert.Equal(t, []string{"n", "u", "s"}, cols.Names)
	assert.Equal(t, []string{"int64", "uint8", "utf8"}, cols.Types)
	assert.Equal(t, []uint64{1, 0, 1}, cols.Nulls)
	assert.Equal(t, []string{"-3", "1", `""`}, cols.Min, "an empty string is a minimum, spelt as JSON")
	assert.Equal(t, []string{"12", "200", `"b"`}, cols.Max)
	assert.Equal(t, []uint64{3, 3, 3}, cols.Distinct)
	assert.Equal(t, []string{"[5,-3,12]", "[9,9,200,1]", `["b","","a\"q"]`}, cols.Sample)
}

func TestSummariesSpellWhatJsonCannotAsStrings(t *testing.T) {
	ts := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "f", Type: arrow.PrimitiveTypes.Float64},
		{Name: "t", Type: ts},
		{Name: "b", Type: arrow.FixedWidthTypes.Boolean},
		{Name: "long", Type: arrow.BinaryTypes.String},
	}, nil)
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	long := strings.Repeat("é", SummaryValueRunes+10)
	cols := summarise(t, schema, func(rb *array.RecordBuilder) {
		rb.Field(0).(*array.Float64Builder).AppendValues([]float64{math.NaN(), 0.5, math.Inf(-1)}, nil)
		tb := rb.Field(1).(*array.TimestampBuilder)
		tb.Append(arrow.Timestamp(at.UnixMicro()))
		tb.Append(arrow.Timestamp(at.Add(time.Hour).UnixMicro()))
		tb.Append(arrow.Timestamp(at.Add(-time.Hour).UnixMicro()))
		rb.Field(2).(*array.BooleanBuilder).AppendValues([]bool{true, false, true}, nil)
		rb.Field(3).(*array.StringBuilder).AppendValues([]string{long, "x", "y"}, nil)
	})
	assert.Equal(t, `"-Inf"`, cols.Min[0], "NaN orders against nothing; -Inf is the minimum, spelt as a string")
	assert.Equal(t, "0.5", cols.Max[0])
	assert.Equal(t, `["NaN",0.5,"-Inf"]`, cols.Sample[0])
	assert.Equal(t, uint64(3), cols.Distinct[0])
	assert.Equal(t, `"2026-10-07T11:00:00Z"`, cols.Min[1], "a timestamp is its Arrow rendering")
	assert.Equal(t, `"2026-10-07T13:00:00Z"`, cols.Max[1])
	assert.Equal(t, "", cols.Min[2], "a boolean does not order")
	assert.Equal(t, `[true,false,true]`, cols.Sample[2])
	assert.Equal(t, uint64(2), cols.Distinct[2])
	assert.Equal(t, `"`+strings.Repeat("é", SummaryValueRunes)+`…"`, cols.Max[3], "a long value is cut where it is shown")
}

func TestSummariesOfAnEmptyStream(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64, Nullable: true}}, nil)
	cols := summarise(t, schema, func(rb *array.RecordBuilder) {
		rb.Field(0).(*array.Int64Builder).AppendNulls(4)
	})
	assert.Equal(t, []uint64{4}, cols.Nulls)
	assert.Equal(t, []string{""}, cols.Min, "no value, no minimum")
	assert.Equal(t, []uint64{0}, cols.Distinct)
	assert.Equal(t, []string{"[]"}, cols.Sample)

	cols = summarise(t, schema)
	assert.Equal(t, []string{"v"}, cols.Names, "a stream with no batches is still described")
}

func TestHyperLogLogIsWithinItsError(t *testing.T) {
	for _, n := range []int{10, 1000, 50_000, 400_000} {
		var c columnSummarizer
		for i := range n {
			c.hll.add(c.hashWord(uint64(i) * 2654435761))
		}
		got := float64(c.hll.estimate())
		assert.InEpsilon(t, float64(n), got, 0.05, "n=%d estimate=%v", n, got)
	}
}
