package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/analytics/timeseries/adscore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// play_series_overlays_test.go covers ADR-0163 M2: the score and span channels
// folded, and — the part that is a DECISION rather than a feature — the
// honesty apparatus that must accompany them. The baseline, the warm-up
// shading and the causality label are mandated chrome (§SD5), so the tests
// that matter most here are the ones asserting they cannot be absent silently.

func seriesScoreRec(t *testing.T, n int, warmUntil int) arrow.RecordBatch {
	t.Helper()
	alloc := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "t", Type: tsTimeType()},
		{Name: "score", Type: arrow.PrimitiveTypes.Float64},
		{Name: "warm_up", Type: arrow.FixedWidthTypes.Boolean},
	}, nil)
	tb := array.NewTimestampBuilder(alloc, tsTimeType().(*arrow.TimestampType))
	sb := array.NewFloat64Builder(alloc)
	wb := array.NewBooleanBuilder(alloc)
	defer tb.Release()
	defer sb.Release()
	defer wb.Release()
	for i := range n {
		tb.Append(arrow.Timestamp(int64(i) * 60_000))
		if i < warmUntil {
			sb.Append(0)
			wb.Append(true)
			continue
		}
		sb.Append(float64(i%7) + 0.5)
		wb.Append(false)
	}
	ta, sa, wa := tb.NewArray(), sb.NewArray(), wb.NewArray()
	defer ta.Release()
	defer sa.Release()
	defer wa.Release()
	return array.NewRecordBatch(schema, []arrow.Array{ta, sa, wa}, int64(n))
}

func TestFoldSeriesScores(t *testing.T) {
	rec := seriesScoreRec(t, 50, 10)
	defer rec.Release()
	sc, ok := foldSeriesScores(rec)
	require.True(t, ok)
	assert.Len(t, sc.t, 50)
	assert.Len(t, sc.score, 50)
	assert.True(t, sc.warm[0], "the training prefix is marked")
	assert.False(t, sc.warm[49])
	assert.InDelta(t, 0.0, sc.t[0], 1e-9, "epoch seconds, from epoch milliseconds")
	assert.InDelta(t, 60.0, sc.t[1], 1e-9)
}

// A `scores` CTE without a score column fills nothing — the channel is
// resolved by NAME, but the shape is still checked before anything is drawn.
func TestFoldSeriesScoresNeedsAScoreColumn(t *testing.T) {
	rec := tsTestInput(t, []float64{1, 2, 3})
	defer rec.Release()
	_, ok := foldSeriesScores(rec)
	assert.False(t, ok)
}

func seriesSpanRec(t *testing.T, colors []string) arrow.RecordBatch {
	t.Helper()
	alloc := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: timelineSlotBandFrom, Type: tsTimeType()},
		{Name: timelineSlotBandTo, Type: tsTimeType()},
		{Name: timelineSlotBandLabel, Type: arrow.BinaryTypes.String},
		{Name: timelineSlotBandColor, Type: arrow.BinaryTypes.String},
	}, nil)
	fb := array.NewTimestampBuilder(alloc, tsTimeType().(*arrow.TimestampType))
	tb := array.NewTimestampBuilder(alloc, tsTimeType().(*arrow.TimestampType))
	lb := array.NewStringBuilder(alloc)
	cb := array.NewStringBuilder(alloc)
	defer fb.Release()
	defer tb.Release()
	defer lb.Release()
	defer cb.Release()
	for i, col := range colors {
		fb.Append(arrow.Timestamp(int64(i) * 600_000))
		tb.Append(arrow.Timestamp(int64(i)*600_000 + 300_000))
		lb.Append("#" + string(rune('1'+i)))
		cb.Append(col)
	}
	fa, ta, la, ca := fb.NewArray(), tb.NewArray(), lb.NewArray(), cb.NewArray()
	defer fa.Release()
	defer ta.Release()
	defer la.Release()
	defer ca.Release()
	return array.NewRecordBatch(schema, []arrow.Array{fa, ta, la, ca}, int64(len(colors)))
}

// The span fold applies the SAME colour rule the Timeline's band reader does:
// a token it cannot resolve is dropped and counted. Both places agreeing is
// what lets one `spans` CTE behave identically wherever it is pointed.
func TestFoldSeriesSpansAppliesTheBandColourRule(t *testing.T) {
	rec := seriesSpanRec(t, []string{"error.default", "#ff0000", "warning.default"})
	defer rec.Release()
	spans, skipped := foldSeriesSpans(rec)
	require.Len(t, spans, 2, "the hex literal is not a token the vocabulary knows")
	assert.Equal(t, 1, skipped)
	assert.InDelta(t, 0.0, spans[0].from, 1e-9)
	assert.InDelta(t, 300.0, spans[0].to, 1e-9, "milliseconds become the plot's seconds")
	assert.NotZero(t, spans[0].packed)
}

// --- the mandated chrome ----------------------------------------------------

// The baseline is not optional (§SD5 S3): when a score lane is drawn, either
// the comparison is there or the panel says why it is not. Both branches are
// asserted, because a silently absent baseline is exactly the failure the
// mandate exists to prevent.
func TestBaselineIsMandatedOrExplained(t *testing.T) {
	call := &tsCall{
		Spec: tsFuncSpec{Name: "tsAnomalyScores", Causal: true,
			Args: []tsArgSpec{{"t", tsArgColumn}, {"v", tsArgColumn}, {"window", tsArgInt}}},
		Args: []string{"t", "v", "16"},
	}

	t.Run("computed when the input and window are known", func(t *testing.T) {
		d := &SeriesDriver{
			lanes:         []seriesLane{{label: "v", vals: tsSine(200, 40)}},
			scoreCall:     call,
			scoreWindow:   16,
			scoreWindowOK: true,
		}
		baseline, why := d.buildSeriesBaseline()
		assert.Empty(t, why)
		// The same one-liner adscore computes, at the same window: the panel
		// must not invent its own comparison.
		want := adscore.BaselineScores(d.lanes[0].vals, adscore.BaselineMovingAverageResidual, 16)
		assert.Equal(t, want, baseline)
	})

	t.Run("explained when the score channel is not a ts node", func(t *testing.T) {
		d := &SeriesDriver{lanes: []seriesLane{{label: "v", vals: tsSine(50, 10)}}}
		baseline, why := d.buildSeriesBaseline()
		assert.Nil(t, baseline)
		assert.Contains(t, why, "no baseline")
	})

	t.Run("explained when the window is unresolved", func(t *testing.T) {
		d := &SeriesDriver{
			lanes:     []seriesLane{{label: "v", vals: tsSine(50, 10)}},
			scoreCall: call,
		}
		baseline, why := d.buildSeriesBaseline()
		assert.Nil(t, baseline)
		assert.Contains(t, why, "window")
	})

	t.Run("explained when the charted result lacks the detector's column", func(t *testing.T) {
		d := &SeriesDriver{
			lanes:         []seriesLane{{label: "other", vals: tsSine(50, 10)}},
			scoreCall:     call,
			scoreWindow:   16,
			scoreWindowOK: true,
		}
		baseline, why := d.buildSeriesBaseline()
		assert.Nil(t, baseline)
		assert.Contains(t, why, "`v`", "the missing column is named")
	})
}

// The caption must state causality either way, and must never let a two-sided
// score pass without saying so — that is the S1 dishonesty in one sentence.
func TestOverlayCaptionStatesCausality(t *testing.T) {
	t.Run("causal", func(t *testing.T) {
		d := &SeriesDriver{scores: seriesScores{
			t: []float64{1}, detector: "tsAnomalyScores", causal: true, window: 64,
			baseline: []float64{0},
		}}
		line := d.seriesOverlayCaption()
		assert.Contains(t, line, "causal")
		assert.Contains(t, line, "backtest")
		assert.Contains(t, line, "tsAnomalyScores")
		assert.Contains(t, line, "64")
	})

	t.Run("two-sided says so in the strongest terms", func(t *testing.T) {
		d := &SeriesDriver{scores: seriesScores{
			t: []float64{1}, detector: "tsProfile", causal: false, window: 32,
		}}
		line := d.seriesOverlayCaption()
		assert.Contains(t, line, "two-sided")
		assert.Contains(t, line, "NOT what an alert would have known")
	})

	t.Run("an absent baseline is stated, never silent", func(t *testing.T) {
		d := &SeriesDriver{scores: seriesScores{
			t: []float64{1}, detector: "tsAnomalyScores", causal: true,
			baselineWhy: "the detector's window is not a resolved number",
		}}
		assert.Contains(t, d.seriesOverlayCaption(), "No baseline")
	})

	t.Run("the warm-up count is reported", func(t *testing.T) {
		d := &SeriesDriver{scores: seriesScores{
			t: []float64{1, 2, 3}, warm: []bool{true, true, false}, causal: true,
		}}
		assert.Contains(t, d.seriesOverlayCaption(), "2 position(s) carry no score")
	})
}

// A run of warm-up positions must be found as ONE region, so the shading is a
// band rather than a picket fence — and the run that ENDS the series must not
// be dropped, which is where the off-by-one lives.
func TestWarmUpRunsAreContiguous(t *testing.T) {
	assert.Equal(t, [][2]int{{0, 1}, {4, 4}, {6, 7}},
		warmUpRuns([]bool{true, true, false, false, true, false, true, true}),
		"three runs, including the one that ends the series")
	assert.Equal(t, [][2]int{{0, 2}}, warmUpRuns([]bool{true, true, true}),
		"an all-warm series is one run, not three")
	assert.Empty(t, warmUpRuns([]bool{false, false}))
	assert.Empty(t, warmUpRuns(nil))
}
