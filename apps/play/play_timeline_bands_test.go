package play

import (
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBandColorByName_Known(t *testing.T) {
	tests := []struct {
		name string
		want styletokens.RGBA8
	}{
		{"neutral.bg.faint", styletokens.NeutralBgFaint},
		{"accent.subtle", styletokens.AccentSubtle},
		{"warning.default", styletokens.WarningDefault},
		{"error.strong", styletokens.ErrorStrong},
		{"success.subtle", styletokens.SuccessSubtle},
		{"info.default", styletokens.InfoDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := bandColorByName(tt.name)
			require.True(t, ok, "expected hit for %q", tt.name)
			want := tt.want
			want.A = bandAlpha
			assert.Equal(t, want.AsHex(), got, "alpha-overridden RGBA8 mismatch")
		})
	}
}

func TestBandColorByName_Unknown(t *testing.T) {
	for _, name := range []string{"", "rainbow", "Accent.Subtle.Extra", "tw.green-500"} {
		_, ok := bandColorByName(name)
		assert.False(t, ok, "expected miss for %q", name)
	}
}

func TestBandColorByName_NormalisesWhitespaceAndCase(t *testing.T) {
	a, ok1 := bandColorByName("  Warning.Default  ")
	b, ok2 := bandColorByName("warning.default")
	require.True(t, ok1)
	require.True(t, ok2)
	assert.Equal(t, b, a, "case + trim should be normalised")
}

func TestBandColorByName_AlphaApplied(t *testing.T) {
	packed, ok := bandColorByName("info.default")
	require.True(t, ok)
	gotAlpha := uint8(packed & 0xff)
	assert.Equal(t, bandAlpha, gotAlpha)
}

func TestSubstituteBandsRange_LiteralForm(t *testing.T) {
	// 2026-05-23T17:13:42.000Z → 1779556422000 ms epoch
	// 2026-05-23T17:14:42.000Z → 1779556482000 ms epoch
	const minMS int64 = 1779556422000
	const maxMS int64 = 1779556482000
	const sql = "SELECT * FROM bands WHERE t BETWEEN _time_data_min AND _time_data_max"

	out := substituteBandsRange(sql, minMS, maxMS)
	assert.Contains(t, out, "toDateTime64('2026-05-23 17:13:42.000', 3, 'UTC')")
	assert.Contains(t, out, "toDateTime64('2026-05-23 17:14:42.000', 3, 'UTC')")
	assert.False(t, strings.Contains(out, "_time_data_min"),
		"placeholder _time_data_min must be replaced")
	assert.False(t, strings.Contains(out, "_time_data_max"),
		"placeholder _time_data_max must be replaced")
}

func TestSubstituteBandsRange_NoPlaceholdersIsNoOp(t *testing.T) {
	const sql = "SELECT 1 AS _tl_band_from, 2 AS _tl_band_to"
	out := substituteBandsRange(sql, 0, 0)
	assert.Equal(t, sql, out)
}

func TestExtentOfEvents_Points(t *testing.T) {
	pts := []*layout.PointEvent{
		{TMS: 10}, {TMS: 30}, {TMS: 20},
	}
	mn, mx, ok := extentOfEvents(nil, pts, nil)
	require.True(t, ok)
	assert.Equal(t, int64(10), mn)
	assert.Equal(t, int64(30), mx)
}

func TestExtentOfEvents_IntervalsCoverToMS(t *testing.T) {
	ivs := []*layout.IntervalEvent{
		{FromMS: 100, ToMS: 200},
		{FromMS: 150, ToMS: 350},
	}
	mn, mx, ok := extentOfEvents(ivs, nil, nil)
	require.True(t, ok)
	assert.Equal(t, int64(100), mn)
	assert.Equal(t, int64(350), mx)
}

func TestExtentOfEvents_Annotations(t *testing.T) {
	anns := []*layout.Annotation{
		{TMS: 50}, {TMS: 60}, {TMS: 40},
	}
	mn, mx, ok := extentOfEvents(nil, nil, anns)
	require.True(t, ok)
	assert.Equal(t, int64(40), mn)
	assert.Equal(t, int64(60), mx)
}

func TestExtentOfEvents_EmptyReturnsFalse(t *testing.T) {
	_, _, ok := extentOfEvents(nil, nil, nil)
	assert.False(t, ok)
}

func TestExtentOfEvents_SkipsNils(t *testing.T) {
	pts := []*layout.PointEvent{nil, {TMS: 100}, nil}
	mn, mx, ok := extentOfEvents(nil, pts, nil)
	require.True(t, ok)
	assert.Equal(t, int64(100), mn)
	assert.Equal(t, int64(100), mx)
}

// The LRU cache retired in 4b — the bands node lane memoizes by compiled SQL
// (extent + bands SQL), so an unchanged (extent, SQL) is a memo hit on the lane.

func bandRec(fromMS, toMS int64, color string) arrow.RecordBatch {
	mem := memory.NewGoAllocator()
	ts := &arrow.TimestampType{Unit: arrow.Millisecond}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: timelineSlotBandFrom, Type: ts},
		{Name: timelineSlotBandTo, Type: ts},
		{Name: timelineSlotBandColor, Type: arrow.BinaryTypes.String},
	}, nil)
	fb := array.NewTimestampBuilder(mem, ts)
	fb.Append(arrow.Timestamp(fromMS))
	tb := array.NewTimestampBuilder(mem, ts)
	tb.Append(arrow.Timestamp(toMS))
	cb := array.NewStringBuilder(mem)
	cb.Append(color)
	fa, ta, ca := fb.NewArray(), tb.NewArray(), cb.NewArray()
	rec := array.NewRecord(schema, []arrow.Array{fa, ta, ca}, 1)
	fb.Release()
	tb.Release()
	cb.Release()
	fa.Release()
	ta.Release()
	ca.Release()
	return rec
}

// demandBands demands the bands node lane (4b-2); setBands maps its _tl_band_*
// result into inst.bands (the chBands channel path).
func TestDemandBandsLaneAndSetBandsMaps(t *testing.T) {
	exec := &mockExecutor{build: func(string) arrow.RecordBatch { return bandRec(1000, 2000, "info.subtle") }}
	sql := "SELECT _time_data_min AS _tl_band_from, _time_data_max AS _tl_band_to, 'info.subtle' AS _tl_band_color"
	drv := &TimelineDriver{
		client:          &Client{},
		bandsLane:       newNodeLane(exec, memory.NewGoAllocator(), 0),
		bandsSQLPtr:     &sql,
		dataMinMS:       1000,
		dataMaxMS:       2000,
		dataExtentValid: true,
	}
	defer drv.bandsLane.close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec, _ := drv.demandBands() // non-blocking; the lane lands the result async
		if rec != nil {
			drv.setBands(rec)
			rec.Release()
		}
		if len(drv.bands) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.Len(t, drv.bands, 1, "the bands node's result is mapped into inst.bands")
	assert.Equal(t, int64(1000), drv.bands[0].FromMS)
	assert.Equal(t, int64(2000), drv.bands[0].ToMS)
	require.NoError(t, drv.bandsErr)

	// An unchanged (extent, SQL) is a memo hit — no second wire call.
	rec, _ := drv.demandBands()
	if rec != nil {
		rec.Release()
	}
	assert.Equal(t, 1, exec.calls, "unchanged compiled SQL must be a lane memo hit")
}
