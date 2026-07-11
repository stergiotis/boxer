package play

import (
	"context"
	"errors"
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

// formatExtentParam renders the raw value a {tl_*:DateTime64(3, 'UTC')} slot
// receives: UTC-formatted, millisecond precision (slice 5d — the successor of
// the retired toDateTime64-literal substitution).
func TestFormatExtentParam(t *testing.T) {
	// 2026-05-23T17:13:42.000Z → 1779556422000 ms epoch
	assert.Equal(t, "2026-05-23 17:13:42.000", formatExtentParam(1779556422000))
	assert.Equal(t, "1970-01-01 00:00:00.001", formatExtentParam(1))
}

// publishExtent emits tl_min/tl_max into the store once an extent exists;
// without one it emits nothing (an extent-referencing bands node stays
// pending until the first events render).
func TestPublishExtentEmitsSignals(t *testing.T) {
	g := newQueryGraph(nil, nil)
	drv := &TimelineDriver{}

	drv.publishExtent(graphEmitter{graph: g})
	_, found := g.signals().Get(string(signalTimelineMin))
	assert.False(t, found, "no extent ⇒ no emit")

	drv.dataMinMS, drv.dataMaxMS, drv.dataExtentValid = 1779556422000, 1779556482000, true
	drv.publishExtent(graphEmitter{graph: g})
	pMin, okMin := g.signals().Get(string(signalTimelineMin))
	pMax, okMax := g.signals().Get(string(signalTimelineMax))
	require.True(t, okMin)
	require.True(t, okMax)
	assert.Equal(t, "2026-05-23 17:13:42.000", pMin.Raw)
	assert.Equal(t, "2026-05-23 17:14:42.000", pMax.Raw)
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

// demandBands compiles the bands node against the signal snapshot (the
// extent rides the tl_min/tl_max signals, 5d) and demands the lane; setBands
// maps its _tl_band_* result into inst.bands (the chBands channel path).
func TestDemandBandsLaneAndSetBandsMaps(t *testing.T) {
	exec := &mockExecutor{build: func(string) arrow.RecordBatch { return bandRec(1000, 2000, "info.subtle") }}
	sql := "SELECT {tl_min:DateTime64(3, 'UTC')} AS _tl_band_from, {tl_max:DateTime64(3, 'UTC')} AS _tl_band_to, 'info.subtle' AS _tl_band_color"
	drv := &TimelineDriver{
		client:      &Client{},
		bandsLane:   newNodeLane(exec, memory.NewGoAllocator(), 0),
		bandsSQLPtr: &sql,
	}
	defer drv.bandsLane.close()

	// Extent absent from the store ⇒ the extent-referencing node is pending,
	// nothing is demanded.
	rec, _ := drv.demandBands(sigNone())
	require.Nil(t, rec)
	require.Equal(t, 0, exec.calls, "no extent signals ⇒ no demand")

	// The Timeline publishes the extent; the bands compile picks it up.
	g := newQueryGraph(nil, nil)
	edrv := &TimelineDriver{dataMinMS: 1000, dataMaxMS: 2000, dataExtentValid: true}
	edrv.publishExtent(graphEmitter{graph: g})
	sig := g.signals()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec, _ := drv.demandBands(sig) // non-blocking; the lane lands the result async
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
	require.NoError(t, drv.bandsLaneErr)
	require.NoError(t, drv.bandsMapErr)
	require.Equal(t, 1, exec.calls)

	// An unchanged (SQL, signal values) pair is a lane memo hit.
	rec, _ = drv.demandBands(sig)
	if rec != nil {
		rec.Release()
	}
	assert.Equal(t, 1, exec.calls, "unchanged compiled pair must be a lane memo hit")

	// A moved extent recompiles and re-executes (supersession by params).
	edrv.dataMaxMS = 3000
	edrv.publishExtent(graphEmitter{graph: g})
	sig2 := g.signals()
	require.Eventually(t, func() bool {
		rec, _ := drv.demandBands(sig2)
		if rec != nil {
			rec.Release()
		}
		return exec.calls == 2
	}, 2*time.Second, time.Millisecond, "a moved extent signal re-executes the bands node")
}

// A legacy bands SQL (the retired _time_data_* placeholders) is never
// demanded; the status line carries the migration hint instead of the
// server's unknown-identifier error.
func TestDemandBandsLegacyPlaceholderHint(t *testing.T) {
	exec := &mockExecutor{build: func(string) arrow.RecordBatch { return bandRec(1, 2, "info.subtle") }}
	sql := "WITH _time_data_min AS lo SELECT lo AS _tl_band_from"
	drv := &TimelineDriver{
		client:      &Client{},
		bandsLane:   newNodeLane(exec, memory.NewGoAllocator(), 0),
		bandsSQLPtr: &sql,
	}
	defer drv.bandsLane.close()

	rec, _ := drv.demandBands(sigNone())
	require.Nil(t, rec)
	require.Equal(t, 0, exec.calls, "a legacy SQL must not reach the lane")
	require.Contains(t, drv.bandsStatusLine(), "retired")
	require.Contains(t, drv.bandsStatusLine(), "{tl_min:DateTime64(3, 'UTC')}")
}

// A bands SQL that does not reference the extent runs immediately — new since
// 5d: absolute-time bands no longer wait for an events render.
func TestDemandBandsExtentFreeRunsWithoutEvents(t *testing.T) {
	exec := &mockExecutor{build: func(string) arrow.RecordBatch { return bandRec(10, 20, "info.subtle") }}
	sql := "SELECT toDateTime64('2026-01-01 00:00:00', 3, 'UTC') AS _tl_band_from, toDateTime64('2026-01-02 00:00:00', 3, 'UTC') AS _tl_band_to, 'info.subtle' AS _tl_band_color"
	drv := &TimelineDriver{
		client:      &Client{},
		bandsLane:   newNodeLane(exec, memory.NewGoAllocator(), 0),
		bandsSQLPtr: &sql,
	}
	defer drv.bandsLane.close()

	require.Eventually(t, func() bool {
		rec, _ := drv.demandBands(sigNone()) // no signals at all — still runs
		if rec != nil {
			drv.setBands(rec)
			rec.Release()
		}
		return len(drv.bands) == 1
	}, 2*time.Second, time.Millisecond, "extent-free bands run without events")
	require.Equal(t, 1, exec.calls)
}

// schemaOnlyExecutor mimics a query that succeeds with an EMPTY result: the
// post-fix executor keeps the stream schema and returns a nil record.
type schemaOnlyExecutor struct {
	schema *arrow.Schema
}

func (inst *schemaOnlyExecutor) execute(context.Context, compiledNode, memory.Allocator) (rec arrow.RecordBatch, schema *arrow.Schema, summary Summary, err error) {
	return nil, inst.schema, Summary{}, nil
}

// A successful empty bands fetch maps to ZERO bands ("0 bands"), clears a
// stale map error, and is distinct from "pending" (review finding: a nil-rec
// success left the previous error latched and read as no-result).
func TestSetBandsEmptySuccessMapsToZeroBands(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: timelineSlotBandFrom, Type: &arrow.TimestampType{Unit: arrow.Millisecond}},
	}, nil)
	sql := "SELECT toDateTime64('2026-01-01 00:00:00', 3, 'UTC') AS _tl_band_from WHERE 0"
	drv := &TimelineDriver{
		client:      &Client{},
		bandsLane:   newNodeLane(&schemaOnlyExecutor{schema: schema}, memory.NewGoAllocator(), 0),
		bandsSQLPtr: &sql,
		bandsMapErr: errors.New("stale mapping error"),
		bands:       []layout.BackgroundBand{{FromMS: 1, ToMS: 2}},
	}
	defer drv.bandsLane.close()

	var rec arrow.RecordBatch
	var sc *arrow.Schema
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec, sc = drv.demandBands(sigNone())
		if !drv.bandsLoading && drv.bandsServedKey != "" {
			break
		}
		if rec != nil {
			rec.Release()
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.Nil(t, rec, "empty result has no record")
	require.NotNil(t, sc, "empty result keeps its schema")

	drv.setBands(rec) // the chBands Render path with a schema-only fill
	assert.Empty(t, drv.bands, "zero bands mapped")
	assert.NoError(t, drv.bandsMapErr, "stale map error cleared by the empty success")
	assert.Equal(t, drv.bandsServedKey, drv.bandsMappedKey)
	assert.Equal(t, "0 bands", drv.bandsStatusLine())
}

// The lane error is mirrored every demand (nil clears), so the status line
// cannot latch a stale error after the lane recovers (review finding).
func TestBandsStatusLineDoesNotLatchErrors(t *testing.T) {
	sql := "SELECT 1"
	drv := &TimelineDriver{bandsSQLPtr: &sql}

	drv.bandsLaneErr = errors.New("timeout")
	require.Contains(t, drv.bandsStatusLine(), "bands error")

	drv.bandsLaneErr = nil // the next demand mirrored a healthy lane
	drv.bandsMapErr = errors.New("bad column")
	require.Contains(t, drv.bandsStatusLine(), "bad column")

	drv.bandsMapErr = nil
	drv.bandsMappedKey = "SELECT 1"
	require.Equal(t, "0 bands", drv.bandsStatusLine())
}

// The bands seam on the wire (5d): the published extent rides the request URL
// as param_tl_min / param_tl_max — no textual rewriting of the bands SQL.
func TestDemandBandsShipsExtentParamsOnWire(t *testing.T) {
	srv, got := captureServer(t)
	defer srv.Close()
	client := NewClient(ClientConfig{URL: srv.URL}, srv.Client())
	sql := "SELECT {tl_min:DateTime64(3, 'UTC')} AS _tl_band_from, {tl_max:DateTime64(3, 'UTC')} AS _tl_band_to, 'info.subtle' AS _tl_band_color"
	drv := &TimelineDriver{
		client:      client,
		bandsLane:   newNodeLane(clientExecutor{client: client}, memory.NewGoAllocator(), 0),
		bandsSQLPtr: &sql,
	}
	defer drv.bandsLane.close()

	g := newQueryGraph(nil, nil)
	edrv := &TimelineDriver{dataMinMS: 1779556422000, dataMaxMS: 1779556482000, dataExtentValid: true}
	edrv.publishExtent(graphEmitter{graph: g})
	sig := g.signals()

	require.Eventually(t, func() bool {
		rec, _ := drv.demandBands(sig)
		if rec != nil {
			rec.Release()
		}
		return len(got()) == 1
	}, 2*time.Second, time.Millisecond)
	qs := got()
	assert.Equal(t, "2026-05-23 17:13:42.000", qs[0].Get("param_tl_min"))
	assert.Equal(t, "2026-05-23 17:14:42.000", qs[0].Get("param_tl_max"))
	assert.NotContains(t, qs[0], "param__time_data_min", "no legacy channel")
}
