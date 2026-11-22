package chstore

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A batch as the server returns it is brought to the stores' schema by
// renaming types, values untouched; a type that is not a renaming is an
// error, as is a facts column the source lacks.
func TestConformBatch(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)

	// The server's rendering: bytes as text, a zoned timestamp, a list of
	// text, and a column the facts do not have.
	sb := array.NewStringBuilder(alloc)
	sb.AppendValues([]string{"a\x00b", "c"}, nil)
	key := sb.NewArray()
	sb.Release()
	tsType := &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"}
	tb := array.NewTimestampBuilder(alloc, tsType)
	tb.AppendValues([]arrow.Timestamp{1, 2}, nil)
	ts := tb.NewArray()
	tb.Release()
	lb := array.NewListBuilder(alloc, arrow.BinaryTypes.String)
	lb.Append(true)
	lb.ValueBuilder().(*array.StringBuilder).AppendValues([]string{"x", "yz"}, nil)
	lb.Append(true)
	list := lb.NewArray()
	lb.Release()
	ib := array.NewInt64Builder(alloc)
	ib.AppendValues([]int64{7, 8}, nil)
	extra := ib.NewArray()
	ib.Release()
	got := arrow.NewSchema([]arrow.Field{
		{Name: "extra", Type: arrow.PrimitiveTypes.Int64},
		{Name: "list", Type: list.DataType()},
		{Name: "ts", Type: tsType},
		{Name: "key", Type: arrow.BinaryTypes.String},
	}, nil)
	rec := array.NewRecordBatch(got, []arrow.Array{extra, list, ts, key}, 2)
	for _, a := range []arrow.Array{extra, list, ts, key} {
		a.Release()
	}
	defer rec.Release()

	want := arrow.NewSchema([]arrow.Field{
		{Name: "key", Type: arrow.BinaryTypes.Binary},
		{Name: "ts", Type: &arrow.TimestampType{Unit: arrow.Nanosecond}},
		{Name: "list", Type: arrow.ListOfNonNullable(arrow.BinaryTypes.Binary)},
	}, nil)
	out, err := conformBatch(rec, want)
	require.NoError(t, err)
	defer out.Release()
	require.True(t, out.Schema().Equal(want), out.Schema().String())
	assert.EqualValues(t, 2, out.NumRows())
	assert.Equal(t, []byte("a\x00b"), out.Column(0).(*array.Binary).Value(0))
	assert.EqualValues(t, 2, out.Column(1).(*array.Timestamp).Value(1))
	items := out.Column(2).(*array.List).ListValues().(*array.Binary)
	assert.Equal(t, []byte("yz"), items.Value(1))

	_, err = conformBatch(rec, arrow.NewSchema([]arrow.Field{{Name: "missing", Type: arrow.BinaryTypes.Binary}}, nil))
	require.Error(t, err)
	_, err = conformBatch(rec, arrow.NewSchema([]arrow.Field{{Name: "extra", Type: arrow.PrimitiveTypes.Int32}}, nil))
	require.ErrorContains(t, err, "not a renaming")
	_, err = conformBatch(rec, arrow.NewSchema([]arrow.Field{{Name: "ts", Type: &arrow.TimestampType{Unit: arrow.Millisecond}}}, nil))
	require.Error(t, err, "another unit is another value")
}

func TestLoadWindow(t *testing.T) {
	assert.Equal(t, "", loadWindow(timeZero, timeZero))
	a := mustTime(t, "2026-11-01T00:00:00Z")
	b := mustTime(t, "2026-12-01T00:00:00+01:00")
	assert.Equal(t, "`ts:ts:z64:47::0:` >= toDateTime64('2026-11-01 00:00:00.000000000', 9, 'UTC') AND `ts:ts:z64:47::0:` < toDateTime64('2026-11-30 23:00:00.000000000', 9, 'UTC')", loadWindow(a, b))
}

var timeZero time.Time

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return v
}
