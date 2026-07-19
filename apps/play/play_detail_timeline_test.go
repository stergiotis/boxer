package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oneRowRec builds a single-row record from (name, array) pairs. Each array
// must be one row long; the field type is read off the array.
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

// tsListCol builds a one-row List(Timestamp[ms]) whose single list holds items.
func tsListCol(mem memory.Allocator, items ...int64) arrow.Array {
	lb := array.NewListBuilder(mem, &arrow.TimestampType{Unit: arrow.Millisecond, TimeZone: "UTC"})
	vb := lb.ValueBuilder().(*array.TimestampBuilder)
	lb.Append(true)
	for _, v := range items {
		vb.Append(arrow.Timestamp(v))
	}
	return lb.NewArray()
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

// valClasses marks the given Arrow columns as leeway value columns of section.
func valClasses(section string, idxs ...int) (cs []streamreadaccess.ColumnClass) {
	for _, i := range idxs {
		cs = append(cs, streamreadaccess.ColumnClass{
			ArrowIdx:    i,
			Class:       streamreadaccess.ColumnRoleClassValue,
			SectionName: naming.StylableName(section),
		})
	}
	return cs
}

// TestDetectTemporalScalars covers the scalar surfaces: Arrow Timestamp and
// Date32 (by type), a leeway tv:time: uint32 read as epoch seconds, a
// non-leeway uint32 and an int64 that must NOT be temporal, and a null cell.
// Each scalar is an instants attribute with one point; order is physical order.
func TestDetectTemporalScalars(t *testing.T) {
	mem := memory.NewGoAllocator()
	rec, schema := oneRowRec(
		"when", tsCol(mem, arrow.Millisecond, true, 1_700_000_000_000),
		"day", date32Col(mem, 20000),
		"tv:time:seen", uint32Col(mem, 1_700_000_500),
		"count", uint32Col(mem, 42),
		"n", int64Col(mem, 7),
		"tv:time:gone", tsCol(mem, arrow.Millisecond, false, 0),
	)

	got, dropped := detectTemporalAttrs(rec, schema, 0, nil)
	require.Len(t, got, 3, "when + day + tv:time:seen; count/n/null-gone excluded")
	assert.Zero(t, dropped)

	assert.Equal(t, "when", got[0].label)
	assert.Equal(t, kindInstants, got[0].kind)
	require.Equal(t, []int64{1_700_000_000_000}, got[0].points)
	assert.Equal(t, 0, got[0].paletteIdx)

	assert.Equal(t, "day", got[1].label)
	require.Equal(t, []int64{int64(20000) * msPerDay}, got[1].points)

	assert.Equal(t, "time.seen", got[2].label)
	require.Equal(t, []int64{int64(1_700_000_500) * 1000}, got[2].points)
	assert.Equal(t, 2, got[2].paletteIdx)
}

// TestDetectTemporalArrayExplode: a List(Timestamp) column is one instants
// attribute carrying every item.
func TestDetectTemporalArrayExplode(t *testing.T) {
	mem := memory.NewGoAllocator()
	rec, schema := oneRowRec("stamps", tsListCol(mem, 100, 200, 300))
	got, _ := detectTemporalAttrs(rec, schema, 0, nil)
	require.Len(t, got, 1)
	assert.Equal(t, kindInstants, got[0].kind)
	assert.Equal(t, []int64{100, 200, 300}, got[0].points)
}

// TestDetectTemporalRangePairing: two temporal value columns sharing one leeway
// SectionName are a co-section begin/end pair → one interval attribute whose
// spans zip item-wise as [min,max]. Without the shared section they are two
// independent instant attributes.
func TestDetectTemporalRangePairing(t *testing.T) {
	mem := memory.NewGoAllocator()
	rec, schema := oneRowRec(
		"tv:time-range:beginIncl", tsListCol(mem, 100, 500),
		"tv:time-range:endExcl", tsListCol(mem, 300, 400), // 2nd span inverted on purpose
	)

	paired, _ := detectTemporalAttrs(rec, schema, 0, valClasses("time-range", 0, 1))
	require.Len(t, paired, 1, "the two co-section columns collapse to one range attr")
	assert.Equal(t, kindIntervals, paired[0].kind)
	assert.Equal(t, "time-range", paired[0].label)
	require.Equal(t, []temporalSpan{{100, 300}, {400, 500}}, paired[0].spans,
		"item-wise, ordered [min,max] so an inverted pair is still a valid span")

	// No classes → no section → two separate instant attrs, not a range.
	unpaired, _ := detectTemporalAttrs(rec, schema, 0, nil)
	require.Len(t, unpaired, 2)
	assert.Equal(t, kindInstants, unpaired[0].kind)
	assert.Equal(t, kindInstants, unpaired[1].kind)
}

// TestDetectTemporalArrayNotPaired: a section with just one temporal value
// column is instants, not a (degenerate) range.
func TestDetectTemporalArrayNotPaired(t *testing.T) {
	mem := memory.NewGoAllocator()
	rec, schema := oneRowRec("tv:time-array:value", tsListCol(mem, 10, 20))
	got, _ := detectTemporalAttrs(rec, schema, 0, valClasses("time-array", 0))
	require.Len(t, got, 1)
	assert.Equal(t, kindInstants, got[0].kind)
	assert.Equal(t, []int64{10, 20}, got[0].points)
}

// TestDetectTemporalBackboneTimestamp: a bare uint32 is not temporal on its
// own, but the backbone entity-timestamp classification identifies it.
func TestDetectTemporalBackboneTimestamp(t *testing.T) {
	mem := memory.NewGoAllocator()
	rec, schema := oneRowRec("ts", uint32Col(mem, 1_700_000_000))

	bare, _ := detectTemporalAttrs(rec, schema, 0, nil)
	assert.Empty(t, bare, "a bare uint32 with no leeway signal is not temporal")

	classes := []streamreadaccess.ColumnClass{
		{ArrowIdx: 0, PlainItemType: common.PlainItemTypeEntityTimestamp},
	}
	got, _ := detectTemporalAttrs(rec, schema, 0, classes)
	require.Len(t, got, 1)
	require.Equal(t, []int64{int64(1_700_000_000) * 1000}, got[0].points)
}

func TestTemporalCellMS(t *testing.T) {
	mem := memory.NewGoAllocator()

	d64 := array.NewDate64Builder(mem)
	d64.Append(arrow.Date64(1_700_000_000_000))
	ms, ok := temporalCellMS(d64.NewArray(), 0, false)
	require.True(t, ok)
	assert.Equal(t, int64(1_700_000_000_000), ms)

	ms, ok = temporalCellMS(tsCol(mem, arrow.Second, true, 1_700_000_000), 0, false)
	require.True(t, ok)
	assert.Equal(t, int64(1_700_000_000_000), ms)

	ic := int64Col(mem, 5)
	_, ok = temporalCellMS(ic, 0, false)
	assert.False(t, ok)
	ms, ok = temporalCellMS(ic, 0, true)
	require.True(t, ok)
	assert.Equal(t, int64(5000), ms)
}

func TestZipSpans(t *testing.T) {
	assert.Equal(t, []temporalSpan{{1, 2}, {3, 5}},
		zipSpans([]int64{1, 5}, []int64{2, 3}), "each span ordered [min,max]")
	assert.Equal(t, []temporalSpan{{1, 2}},
		zipSpans([]int64{1, 9}, []int64{2}), "mismatched cardinality drops the extra")
	assert.Empty(t, zipSpans(nil, []int64{1}))
}

func TestCapMarks(t *testing.T) {
	big := make([]int64, maxDetailMarks+5)
	out, dropped := capMarks([]temporalAttr{{kind: kindInstants, points: big}})
	require.Len(t, out, 1)
	assert.Len(t, out[0].points, maxDetailMarks)
	assert.Equal(t, 5, dropped)

	// An attribute wholly beyond the cap is dropped, not kept empty.
	out, dropped = capMarks([]temporalAttr{
		{kind: kindInstants, points: make([]int64, maxDetailMarks)},
		{kind: kindInstants, points: []int64{1, 2}},
	})
	require.Len(t, out, 1)
	assert.Equal(t, 2, dropped)
}

func TestFitRange(t *testing.T) {
	_, _, ok := fitRange(nil)
	assert.False(t, ok)

	lo, hi, ok := fitRange([]temporalAttr{{kind: kindInstants, points: []int64{1_700_000_000_000}}})
	require.True(t, ok)
	assert.Equal(t, int64(2*singleInstantPadMS), hi.UnixMilli()-lo.UnixMilli())

	// Spans participate in the extent alongside points.
	lo, hi, ok = fitRange([]temporalAttr{
		{kind: kindInstants, points: []int64{1_000_000_000_000}},
		{kind: kindIntervals, spans: []temporalSpan{{1_000_000_050_000, 1_000_000_100_000}}},
	})
	require.True(t, ok)
	pad := int64(100_000) / 10
	assert.Equal(t, int64(1_000_000_000_000)-pad, lo.UnixMilli())
	assert.Equal(t, int64(1_000_000_100_000)+pad, hi.UnixMilli())
}

func TestBuildAnnotationsAndIntervals(t *testing.T) {
	attrs := []temporalAttr{
		{label: "created", paletteIdx: 0, kind: kindInstants, points: []int64{111}},
		{label: "seen", paletteIdx: 1, kind: kindInstants, points: []int64{200, 300}},
		{label: "window", paletteIdx: 2, kind: kindIntervals, spans: []temporalSpan{{400, 900}}},
	}

	anns := buildDetailAnnotations(attrs)
	require.Len(t, anns, 3, "1 scalar + 2 array items; the range contributes no flag")
	assert.Equal(t, int32(1), anns[0].Number)
	assert.Equal(t, "created: "+formatEpochMS(111), anns[0].Label)
	// Both items of "seen" share its number and palette index.
	assert.Equal(t, int32(2), anns[1].Number)
	assert.Equal(t, int32(2), anns[2].Number)
	assert.Equal(t, int32(1), anns[1].PaletteIdx)

	ivs := buildDetailIntervals(attrs)
	require.Len(t, ivs, 1)
	assert.Equal(t, int64(400), ivs[0].FromMS)
	assert.Equal(t, int64(900), ivs[0].ToMS)
	assert.Equal(t, "window", ivs[0].LaneHint)
	assert.Equal(t, int32(2), ivs[0].KindID)
}

func TestAttrSummary(t *testing.T) {
	scalar := temporalAttr{kind: kindInstants, points: []int64{111}}
	assert.Equal(t, formatEpochMS(111), scalar.summary())

	arr := temporalAttr{kind: kindInstants, points: []int64{100, 300, 200}}
	assert.Contains(t, arr.summary(), "3 items")
	assert.Contains(t, arr.summary(), formatEpochMS(100))
	assert.Contains(t, arr.summary(), formatEpochMS(300))

	oneSpan := temporalAttr{kind: kindIntervals, spans: []temporalSpan{{400, 900}}}
	assert.Equal(t, formatEpochMS(400)+" … "+formatEpochMS(900), oneSpan.summary())

	manySpan := temporalAttr{kind: kindIntervals, spans: []temporalSpan{{400, 900}, {1000, 1200}}}
	assert.Contains(t, manySpan.summary(), "2 windows")
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

	recB, schemaB := oneRowRec("n", int64Col(mem, 7))
	dt.sync(recB, schemaB, 0, nil)
	assert.Empty(t, dt.attrs)
}
