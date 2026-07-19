package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oneRowRec builds a single-row record from (name, array) pairs. Each array
// must have exactly one element; the field type is read off the array.
func oneRowRec(pairs ...any) (arrow.RecordBatch, *arrow.Schema) {
	if len(pairs)%2 != 0 {
		panic("oneRowRec: pairs must be (name, array)*")
	}
	fields := make([]arrow.Field, 0, len(pairs)/2)
	cols := make([]arrow.Array, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		name := pairs[i].(string)
		arr := pairs[i+1].(arrow.Array)
		fields = append(fields, arrow.Field{Name: name, Type: arr.DataType(), Nullable: true})
		cols = append(cols, arr)
	}
	schema := arrow.NewSchema(fields, nil)
	return array.NewRecordBatch(schema, cols, 1), schema
}

func tsCol(mem memory.Allocator, unit arrow.TimeUnit, present bool, v int64) arrow.Array {
	b := array.NewTimestampBuilder(mem, &arrow.TimestampType{Unit: unit, TimeZone: "UTC"})
	if present {
		b.Append(arrow.Timestamp(v))
	} else {
		b.AppendNull()
	}
	return b.NewArray()
}

func date32Col(mem memory.Allocator, days int32) arrow.Array {
	b := array.NewDate32Builder(mem)
	b.Append(arrow.Date32(days))
	return b.NewArray()
}

func uint32Col(mem memory.Allocator, v uint32) arrow.Array {
	b := array.NewUint32Builder(mem)
	b.Append(v)
	return b.NewArray()
}

func int64Col(mem memory.Allocator, v int64) arrow.Array {
	b := array.NewInt64Builder(mem)
	b.Append(v)
	return b.NewArray()
}

// TestDetectTemporalAttrs covers the three detection surfaces at once: an Arrow
// Timestamp and Date32 (by type), a leeway tv:time: uint32 read as epoch
// seconds, a non-leeway uint32 and an int64 that must NOT be mistaken for time,
// and a null temporal cell that is skipped. Output order is physical order.
func TestDetectTemporalAttrs(t *testing.T) {
	mem := memory.NewGoAllocator()
	rec, schema := oneRowRec(
		"when", tsCol(mem, arrow.Millisecond, true, 1_700_000_000_000),
		"day", date32Col(mem, 20000),
		"tv:time:seen", uint32Col(mem, 1_700_000_500),
		"count", uint32Col(mem, 42),
		"n", int64Col(mem, 7),
		"tv:time:gone", tsCol(mem, arrow.Millisecond, false, 0),
	)

	got := detectTemporalAttrs(rec, schema, 0, nil)
	require.Len(t, got, 3, "when + day + tv:time:seen; count/n/null-gone excluded")

	assert.Equal(t, "when", got[0].label)
	assert.Equal(t, int64(1_700_000_000_000), got[0].epochMS)
	assert.Equal(t, formatEpochMS(1_700_000_000_000), got[0].valueS)

	assert.Equal(t, "day", got[1].label)
	assert.Equal(t, int64(20000)*msPerDay, got[1].epochMS)

	// tv:time: → "time.seen" (shortColumnLabel), uint32 read as epoch seconds.
	assert.Equal(t, "time.seen", got[2].label)
	assert.Equal(t, int64(1_700_000_500)*1000, got[2].epochMS)
}

// TestDetectTemporalAttrsBackboneTimestamp is the leeway width-32 case the
// type-only detector misses: a bare uint32 is not temporal on its own, but the
// backbone entity-timestamp classification identifies it (read as seconds).
func TestDetectTemporalAttrsBackboneTimestamp(t *testing.T) {
	mem := memory.NewGoAllocator()
	rec, schema := oneRowRec("ts", uint32Col(mem, 1_700_000_000))

	assert.Empty(t, detectTemporalAttrs(rec, schema, 0, nil),
		"a bare uint32 with no leeway signal is not temporal")

	classes := []streamreadaccess.ColumnClass{
		{ArrowIdx: 0, PlainItemType: common.PlainItemTypeEntityTimestamp},
	}
	got := detectTemporalAttrs(rec, schema, 0, classes)
	require.Len(t, got, 1)
	assert.Equal(t, "ts", got[0].label)
	assert.Equal(t, int64(1_700_000_000)*1000, got[0].epochMS)
}

func TestTemporalCellMS(t *testing.T) {
	mem := memory.NewGoAllocator()

	// Date64 is already epoch ms.
	d64 := array.NewDate64Builder(mem)
	d64.Append(arrow.Date64(1_700_000_000_000))
	ms, ok := temporalCellMS(d64.NewArray(), 0, false)
	require.True(t, ok)
	assert.Equal(t, int64(1_700_000_000_000), ms)

	// Second-unit timestamp scales up to ms.
	ms, ok = temporalCellMS(tsCol(mem, arrow.Second, true, 1_700_000_000), 0, false)
	require.True(t, ok)
	assert.Equal(t, int64(1_700_000_000_000), ms)

	// A bare int64 is temporal only when the leeway flag is set (epoch seconds).
	ic := int64Col(mem, 5)
	_, ok = temporalCellMS(ic, 0, false)
	assert.False(t, ok)
	ms, ok = temporalCellMS(ic, 0, true)
	require.True(t, ok)
	assert.Equal(t, int64(5000), ms)
}

func TestFitRange(t *testing.T) {
	_, _, ok := fitRange(nil)
	assert.False(t, ok, "no attributes → no range")

	// Single instant → fixed ±12h window; t1 strictly after t0.
	lo, hi, ok := fitRange([]temporalAttr{{epochMS: 1_700_000_000_000}})
	require.True(t, ok)
	assert.True(t, hi.After(lo))
	assert.Equal(t, int64(2*singleInstantPadMS), hi.UnixMilli()-lo.UnixMilli())

	// Span present → 10% margin each side, tight around the data.
	lo, hi, ok = fitRange([]temporalAttr{
		{epochMS: 1_000_000_000_000},
		{epochMS: 1_000_000_100_000},
	})
	require.True(t, ok)
	pad := int64(100_000) / 10
	assert.Equal(t, int64(1_000_000_000_000)-pad, lo.UnixMilli())
	assert.Equal(t, int64(1_000_000_100_000)+pad, hi.UnixMilli())
}

func TestBuildDetailAnnotations(t *testing.T) {
	assert.Nil(t, buildDetailAnnotations(nil))

	anns := buildDetailAnnotations([]temporalAttr{
		{label: "created", epochMS: 111, valueS: "2026-01-01 00:00:00"},
		{label: "updated", epochMS: 222, valueS: "2026-02-02 00:00:00"},
	})
	require.Len(t, anns, 2)
	assert.Equal(t, int64(111), anns[0].TMS)
	assert.Equal(t, int32(1), anns[0].Number, "1-based, matches the legend")
	assert.Equal(t, int32(0), anns[0].PaletteIdx)
	assert.Equal(t, "created: 2026-01-01 00:00:00", anns[0].Label)
	assert.Equal(t, int32(2), anns[1].Number)
	assert.Equal(t, int32(1), anns[1].PaletteIdx)
}

// TestDetailTimelineSyncEarlyCutoff verifies re-detection is gated on the
// (result, row) identity: same rec+row is a no-op, a new row re-detects.
func TestDetailTimelineSyncEarlyCutoff(t *testing.T) {
	mem := memory.NewGoAllocator()
	dt := NewDetailTimeline(c.NewWidgetIdStack())

	recA, schemaA := oneRowRec("when", tsCol(mem, arrow.Millisecond, true, 1_700_000_000_000))
	dt.sync(recA, schemaA, 0, nil)
	require.Len(t, dt.attrs, 1)
	firstElem := &dt.attrs[0]

	dt.sync(recA, schemaA, 0, nil)
	assert.Same(t, firstElem, &dt.attrs[0], "same (rec,row): attrs slice not rebuilt")

	// A record with no temporal columns clears the set.
	recB, schemaB := oneRowRec("n", int64Col(mem, 7))
	dt.sync(recB, schemaB, 0, nil)
	assert.Empty(t, dt.attrs)
}
