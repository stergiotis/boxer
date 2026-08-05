package play

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// play_series_labels_test.go covers ADR-0163 M3: what a verdict is attached
// to, how the append-only table reads back as current state, and the readout
// that turns "beats the one-liner" into a number.
//
// The tests that matter most are the ones about what the readout REFUSES to
// claim: only confirmed spans become positives, a thin sample says so, and a
// baseline that wins is reported as winning.

// A verdict is attached to the compiled INPUT, not to the buffer text. Two
// buffers that fuse to the same input are the same series; a changed input is
// a different one, and its old labels correctly stop applying.
func TestInputHashFollowsTheCompiledInput(t *testing.T) {
	a := compiledNode{SQL: "SELECT t, v FROM x", Params: map[string]string{"param_h": "1"}}
	b := compiledNode{SQL: "SELECT t, v FROM x", Params: map[string]string{"param_h": "1"}}
	assert.Equal(t, tsInputHash(a), tsInputHash(b), "the same input is the same subject")

	c := a
	c.Params = map[string]string{"param_h": "2"}
	assert.NotEqual(t, tsInputHash(a), tsInputHash(c), "a different param is a different series")

	d := a
	d.SQL = "SELECT t, v FROM y"
	assert.NotEqual(t, tsInputHash(a), tsInputHash(d), "a different query is a different series")

	// The transform is part of the compiled identity, so re-parameterising the
	// DETECTOR also re-subjects the labels. That is the honest reading: a span
	// flagged at window 64 was not adjudicated at window 128.
	e := a
	e.Client = &tsCall{Text: "tsAnomalyScores(t, v, 64)"}
	f := a
	f.Client = &tsCall{Text: "tsAnomalyScores(t, v, 128)"}
	assert.NotEqual(t, tsInputHash(e), tsInputHash(f))
}

func TestVerdictRoundTrip(t *testing.T) {
	for _, v := range []tsVerdictE{tsVerdictConfirmed, tsVerdictFalseAlarm} {
		assert.Equal(t, v, tsVerdictFromString(v.String()), v.String())
	}
	assert.Equal(t, tsVerdictNone, tsVerdictFromString("maybe"))
	assert.Empty(t, tsVerdictNone.String(), "the absence of a verdict is never a stored value")
}

// The write goes in as JSONEachRow, so the tags ARE the column names the
// INSERT lists. A drift between the two is a runtime error at the server, so
// it is worth pinning here.
func TestLabelRowMarshalsToTheInsertColumns(t *testing.T) {
	raw, err := json.Marshal(tsLabelRow{
		InputHash: "abc", SpanFrom: "2026-01-01 00:00:00.000", SpanTo: "2026-01-01 00:05:00.000",
		Verdict: "confirmed", Detector: "tsAnomalyScores", Window: 64, Note: "#1",
	})
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(raw, &fields))
	for _, col := range []string{"input_hash", "span_from", "span_to", "verdict", "detector", "window", "note"} {
		assert.Contains(t, fields, col)
		assert.Contains(t, tsLabelsQuery+tsLabelsDDL, col, "%s is in the table too", col)
	}
}

// Timestamps are written forced-UTC. A label whose meaning depended on the
// writer's timezone would be worthless to the next reader — so the moment is
// built as UTC here and must come back spelled the same way, whatever zone
// the machine running the test is in.
func TestLabelTimesAreUTC(t *testing.T) {
	moment := time.Date(2026, 7, 5, 12, 34, 56, 789_000_000, time.UTC)
	assert.Equal(t, "2026-07-05 12:34:56.789", formatSeriesLabelTime(moment.UnixMilli()))

	// The same instant read through a non-UTC zone must not shift the
	// spelling: the formatter forces UTC rather than inheriting a location.
	elsewhere := moment.In(time.FixedZone("UTC+9", 9*3600))
	assert.Equal(t, "2026-07-05 12:34:56.789", formatSeriesLabelTime(elsewhere.UnixMilli()))
}

func labelsRec(t *testing.T, rows [][3]any) arrow.RecordBatch {
	t.Helper()
	alloc := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "from_ms", Type: arrow.PrimitiveTypes.Int64},
		{Name: "to_ms", Type: arrow.PrimitiveTypes.Int64},
		{Name: "verdict", Type: arrow.BinaryTypes.String},
	}, nil)
	fb := array.NewInt64Builder(alloc)
	tb := array.NewInt64Builder(alloc)
	vb := array.NewStringBuilder(alloc)
	defer fb.Release()
	defer tb.Release()
	defer vb.Release()
	for _, r := range rows {
		fb.Append(int64(r[0].(int)))
		tb.Append(int64(r[1].(int)))
		vb.Append(r[2].(string))
	}
	fa, ta, va := fb.NewArray(), tb.NewArray(), vb.NewArray()
	defer fa.Release()
	defer ta.Release()
	defer va.Release()
	return array.NewRecordBatch(schema, []arrow.Array{fa, ta, va}, int64(len(rows)))
}

func TestDecodeSeriesLabels(t *testing.T) {
	rec := labelsRec(t, [][3]any{
		{0, 60_000, "confirmed"},
		{120_000, 180_000, "false_alarm"},
		{240_000, 300_000, "nonsense"},
	})
	defer rec.Release()
	out := decodeSeriesLabels(rec)
	require.Len(t, out, 2, "a verdict the vocabulary does not know is dropped, not guessed")
	assert.Equal(t, tsVerdictConfirmed, out[tsLabelKey{fromMS: 0, toMS: 60_000}])
	assert.Equal(t, tsVerdictFalseAlarm, out[tsLabelKey{fromMS: 120_000, toMS: 180_000}])
}

// --- the readout ------------------------------------------------------------

func readoutFixture(n int) (scores []float64, baseline []float64, ts []float64) {
	scores = make([]float64, n)
	baseline = make([]float64, n)
	ts = make([]float64, n)
	for i := range ts {
		ts[i] = float64(i) * 60 // one-minute grid, in seconds
		scores[i] = 0.1
		baseline[i] = 0.5
	}
	return
}

// Only CONFIRMED spans become positives. A false alarm is the ABSENCE of an
// event, which is what the unlabelled majority already says — counting it as
// anything else would put a thumb on the scale.
func TestReadoutCountsOnlyConfirmedSpans(t *testing.T) {
	scores, baseline, ts := readoutFixture(300)
	for i := 100; i < 120; i++ {
		scores[i] = 9 // the detector finds the confirmed span
	}
	labels := map[tsLabelKey]tsVerdictE{
		{fromMS: 100 * 60_000, toMS: 119 * 60_000}: tsVerdictConfirmed,
		{fromMS: 200 * 60_000, toMS: 219 * 60_000}: tsVerdictFalseAlarm,
	}
	out, ok := buildSeriesReadout(scores, baseline, ts, labels)
	require.True(t, ok)
	assert.Equal(t, 1, out.spans, "the false alarm is not a labelled event")
	assert.Equal(t, 20, out.labelled)
	assert.True(t, out.haveBaseline)
	assert.Greater(t, out.detector.VUSPR, out.baseline.VUSPR,
		"a detector that spikes exactly on the confirmed span should beat a flat one")
}

func TestReadoutNeedsLabels(t *testing.T) {
	scores, baseline, ts := readoutFixture(100)
	_, ok := buildSeriesReadout(scores, baseline, ts, nil)
	assert.False(t, ok, "no labels, no claim")

	_, ok = buildSeriesReadout(scores, baseline, ts, map[tsLabelKey]tsVerdictE{
		{fromMS: 0, toMS: 60_000}: tsVerdictFalseAlarm,
	})
	assert.False(t, ok, "false alarms alone leave nothing to score against")
}

// The readout must be equally capable of reporting that the one-liner won —
// that is the whole reason the baseline is mandated rather than optional.
func TestReadoutReportsABaselineWin(t *testing.T) {
	scores, baseline, ts := readoutFixture(300)
	for i := 100; i < 120; i++ {
		baseline[i] = 9 // the BASELINE finds it; the detector does not
	}
	labels := map[tsLabelKey]tsVerdictE{
		{fromMS: 100 * 60_000, toMS: 119 * 60_000}: tsVerdictConfirmed,
		{fromMS: 10 * 60_000, toMS: 20 * 60_000}:   tsVerdictConfirmed,
		{fromMS: 250 * 60_000, toMS: 260 * 60_000}: tsVerdictConfirmed,
	}
	out, ok := buildSeriesReadout(scores, baseline, ts, labels)
	require.True(t, ok)
	require.True(t, out.haveBaseline)
	assert.Less(t, out.detector.VUSPR, out.baseline.VUSPR)
	assert.Contains(t, seriesReadoutLine(out), "THE ONE-LINER IS AHEAD HERE")
}

// VUS-ROC must never be shown bare: a reader who takes 0.5 for chance and 1.0
// for perfect will misread it, since the buffer gives a random scorer positive
// mass and costs a perfectly-located one.
func TestReadoutLineAnnotatesTheUsableBand(t *testing.T) {
	line := seriesReadoutLine(tsScoreReadout{spans: 4, labelled: 40, haveBaseline: true})
	assert.Contains(t, line, "VUS-PR", "the measure that leads")
	assert.Contains(t, line, "VUS-ROC")
	assert.Contains(t, line, "0.55")
	assert.Contains(t, line, "0.92")
	assert.NotContains(t, line, "Too few spans", "four spans is past the caveat")
}

// A thin sample says so. Two confirmed spans is a reading, not evidence, and
// the line has to be the thing that says it.
func TestReadoutLineCaveatsAThinSample(t *testing.T) {
	line := seriesReadoutLine(tsScoreReadout{spans: 2, labelled: 20})
	assert.Contains(t, line, "Too few spans to conclude anything")
}

func TestReadoutLineReportsAScoringFailure(t *testing.T) {
	line := seriesReadoutLine(tsScoreReadout{err: "labels must be non-empty"})
	assert.Contains(t, line, "could not be scored")
	assert.NotContains(t, line, "VUS-PR", "a failed scoring reports no numbers at all")
}

// The DDL and the read must agree about the table, and the read must be
// parameterised — an input hash spliced into SQL would be an injection seam
// on a value that comes from a hash, but the habit is what matters.
func TestLabelsQueryShape(t *testing.T) {
	assert.Contains(t, tsLabelsDDL, tsLabelsTable)
	assert.Contains(t, tsLabelsQuery, tsLabelsTable)
	assert.Contains(t, tsLabelsQuery, "{ts_input_hash:String}", "the hash rides a param slot")
	assert.Contains(t, tsLabelsQuery, "argMax(verdict, created_at)",
		"latest-wins per span is what makes an append-only table read as state")
	assert.True(t, strings.Contains(tsLabelsDDL, "ENGINE MergeTree()"))
}
