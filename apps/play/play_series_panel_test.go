package play

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// play_series_panel_test.go covers the ADR-0163 M0 verification plan: the
// typed claim, the Δt classifier (including its jitter tolerance), the
// envelope's cannot-drop-extremes property, and the segmentation that keeps
// the panel from drawing across a hole.

// schemaWith, tsField, strField and f64Field live in the timeline and kanban
// panel tests; seriesSchema is just their reader-facing name here.
func seriesSchema(fields ...arrow.Field) *arrow.Schema { return schemaWith(fields...) }

// The typed claim (§SD1): x is the FIRST temporal column, every numeric
// column is a lane, everything else is ignored rather than refused.
func TestSeriesClaimResolvesTypes(t *testing.T) {
	k, reason := resolveSeriesColumns(seriesSchema(
		strField("host"),
		tsField("t"),
		f64Field("cpu"),
		strField("note"),
		arrow.Field{Name: "mem", Type: arrow.PrimitiveTypes.Int64},
		tsField("ingested_at"),
	))
	require.Empty(t, reason)
	assert.Equal(t, 1, k.tCol, "the FIRST temporal column is the axis")
	assert.Equal(t, []int{2, 4}, k.vCols, "every numeric column is a lane, in schema order")
}

func TestSeriesClaimRejectsWithAReason(t *testing.T) {
	_, reason := resolveSeriesColumns(seriesSchema(
		strField("host"),
		f64Field("cpu"),
	))
	assert.Contains(t, reason, "time column")
	// The reject names what the result ACTUALLY carries, so it says what is
	// wrong with this result rather than restating the contract.
	assert.Contains(t, reason, "`cpu`")

	_, reason = resolveSeriesColumns(seriesSchema(tsField("t"),
		strField("host")))
	assert.Contains(t, reason, "numeric column")
	assert.Contains(t, reason, "`t`", "the reject names the axis it found")
}

// A ClickHouse DateTime reaches Arrow as a bare uint32 of epoch seconds, with
// no metadata naming the source type (verified against a live server under
// both Arrow writers — ADR-0163 Update 2026-08-05). Taking it as a time axis
// would make `SELECT id, count()` draw an id as time, so it is refused and the
// reject names the cast rather than leaving the user to guess.
func TestSeriesClaimRefusesEpochIntegers(t *testing.T) {
	_, reason := resolveSeriesColumns(seriesSchema(
		arrow.Field{Name: "t", Type: arrow.PrimitiveTypes.Uint32},
		arrow.Field{Name: "n", Type: arrow.PrimitiveTypes.Uint64},
	))
	require.NotEmpty(t, reason, "a uint32 is not a time axis, however it was spelled in SQL")
	assert.Contains(t, reason, "toDateTime64", "the reject names the fix")

	// And the aggregation scaffold must not emit SQL this very claim refuses:
	// toStartOfInterval yields a DateTime, so the cast rides along.
	d := &SeriesDriver{tLabel: "t", grid: seriesGrid{medianSec: 60}, lanes: []seriesLane{{label: "v"}}}
	assert.Contains(t, d.gridScaffold(), "toDateTime64(toStartOfInterval(t, INTERVAL 1 MINUTE), 3)")
}

// A Date column is a time axis too — the claim is about TYPES, and Date32 /
// Date64 are what a daily rollup arrives as.
func TestSeriesClaimAcceptsDateColumns(t *testing.T) {
	for _, dt := range []arrow.DataType{arrow.FixedWidthTypes.Date32, arrow.FixedWidthTypes.Date64} {
		k, reason := resolveSeriesColumns(seriesSchema(
			arrow.Field{Name: "d", Type: dt}, f64Field("v")))
		require.Empty(t, reason, "%s is temporal", dt)
		assert.Equal(t, 0, k.tCol)
	}
}

// The optional channels are the `scores` / `spans` CTEs by NAME (§SD1), but
// their SHAPE is still checked — a CTE called `scores` that carries no score
// column is a mistake worth naming.
func TestSeriesAuxChannelAcceptance(t *testing.T) {
	_, reason := acceptSeriesScores(seriesSchema(tsField("t"), f64Field("score")))
	assert.Empty(t, reason)

	_, reason = acceptSeriesScores(seriesSchema(tsField("t"), f64Field("value")))
	assert.Contains(t, reason, "tsAnomalyScores")

	_, reason = acceptSeriesSpans(seriesSchema(
		tsField(timelineSlotBandFrom), tsField(timelineSlotBandTo),
		strField(timelineSlotBandColor)))
	assert.Empty(t, reason, "the Timeline band contract is what tsAnomalySpans emits")

	_, reason = acceptSeriesSpans(seriesSchema(tsField(timelineSlotBandFrom), tsField(timelineSlotBandTo)))
	assert.Contains(t, reason, timelineSlotBandColor, "the missing column is named")
}

// §SD2's classifier, class by class.
func TestSeriesGridClassification(t *testing.T) {
	grid := func(n int, step float64) (out []float64) {
		out = make([]float64, n)
		for i := range out {
			out[i] = float64(i) * step
		}
		return
	}

	t.Run("regular", func(t *testing.T) {
		g := classifySeriesGrid(grid(20, 5))
		assert.Equal(t, seriesGridRegular, g.class)
		assert.InDelta(t, 5.0, g.medianSec, 1e-9)
		assert.Zero(t, g.gaps)
	})

	t.Run("jitter inside tolerance stays regular", func(t *testing.T) {
		// ±19% of the median: under the ±20% constant, so no gap opens.
		ts := []float64{0, 5, 9.05, 14, 19.05, 24, 28.95, 34}
		g := classifySeriesGrid(ts)
		assert.Equal(t, seriesGridRegular, g.class)
	})

	t.Run("jitter beyond tolerance opens a gap", func(t *testing.T) {
		ts := grid(10, 5)
		for i := 5; i < len(ts); i++ {
			ts[i] += 3 // one 8s interval: 60% over the median
		}
		g := classifySeriesGrid(ts)
		assert.Equal(t, 1, g.gaps)
		assert.Equal(t, []int{5}, g.breaks)
	})

	t.Run("whole missing samples read as gaps, not as irregularity", func(t *testing.T) {
		ts := []float64{0, 5, 10, 15, 30, 35, 40, 55, 60}
		g := classifySeriesGrid(ts)
		assert.Equal(t, seriesGridGapped, g.class, "15s and 15s are 3× the 5s median")
		assert.Equal(t, 2, g.gaps)
	})

	t.Run("spacing that was never a grid is irregular", func(t *testing.T) {
		g := classifySeriesGrid([]float64{0, 5, 11.7, 13, 29.4, 30, 47.2})
		assert.Equal(t, seriesGridIrregular, g.class)
	})

	t.Run("backwards time is reported before anything else", func(t *testing.T) {
		g := classifySeriesGrid([]float64{0, 5, 10, 7, 15, 20})
		assert.Equal(t, seriesGridUnordered, g.class,
			"every Δt downstream of a reversal is meaningless")
	})

	t.Run("too short to classify", func(t *testing.T) {
		assert.Equal(t, seriesGridUnknown, classifySeriesGrid([]float64{0, 5}).class)
	})

	t.Run("repeated timestamps are not a grid", func(t *testing.T) {
		g := classifySeriesGrid([]float64{7, 7, 7, 7, 7})
		assert.Equal(t, seriesGridIrregular, g.class, "a zero step is nothing to analyse in")
	})
}

// The tolerance is a boundary, so it is worth pinning from both sides.
func TestSeriesGridJitterToleranceBoundary(t *testing.T) {
	const step = 10.0
	for _, tc := range []struct {
		name   string
		excess float64
		want   int
	}{
		{"just inside", step * (seriesJitterTol - 0.01), 0},
		{"just outside", step * (seriesJitterTol + 0.01), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := []float64{0, step, 2 * step, 3*step + tc.excess, 4*step + tc.excess, 5*step + tc.excess}
			assert.Equal(t, tc.want, classifySeriesGrid(ts).gaps)
		})
	}
}

// The ADR's named envelope property (§SD1 / Q9): per pixel bucket, what is
// drawn reaches exactly what the source reaches. This is the whole reason the
// automatic decimator is an envelope and not LTTB — a selection-based
// decimator can drop the one narrow extreme a reader is looking for.
func TestEnvelopeKeepsEveryBucketExtreme(t *testing.T) {
	rng := rand.New(rand.NewSource(0x5e21e5))
	for trial := range 200 {
		n := 500 + rng.Intn(4000)
		widthPx := 20 + rng.Intn(300)
		ts := make([]float64, n)
		vs := make([]float64, n)
		for i := range ts {
			ts[i] = float64(i)
			vs[i] = rng.NormFloat64()
		}
		// A lone spike: the sample a selection-based decimator is entitled to
		// drop and this one may not.
		spike := rng.Intn(n)
		vs[spike] = 1e6

		xMin, xMax := ts[0], ts[n-1]+1
		outT, outV := envelopeDecimate(ts, vs, xMin, xMax, widthPx)
		require.LessOrEqual(t, len(outT), len(ts), "decimation never grows the series")

		bucketOf := func(x float64) int {
			b := int((x - xMin) / (xMax - xMin) * float64(widthPx))
			return min(b, widthPx-1)
		}
		type ext struct{ lo, hi float64 }
		srcExt := map[int]*ext{}
		for i := range ts {
			b := bucketOf(ts[i])
			e, seen := srcExt[b]
			if !seen {
				srcExt[b] = &ext{vs[i], vs[i]}
				continue
			}
			e.lo, e.hi = math.Min(e.lo, vs[i]), math.Max(e.hi, vs[i])
		}
		drawnExt := map[int]*ext{}
		for i := range outT {
			b := bucketOf(outT[i])
			e, seen := drawnExt[b]
			if !seen {
				drawnExt[b] = &ext{outV[i], outV[i]}
				continue
			}
			e.lo, e.hi = math.Min(e.lo, outV[i]), math.Max(e.hi, outV[i])
		}
		for b, want := range srcExt {
			got, drew := drawnExt[b]
			require.True(t, drew, "trial %d: bucket %d vanished", trial, b)
			assert.Equal(t, want.lo, got.lo, "trial %d: bucket %d minimum", trial, b)
			assert.Equal(t, want.hi, got.hi, "trial %d: bucket %d maximum", trial, b)
		}
	}
}

// Decimation is worth doing only above a threshold, and must be a no-op below
// it: handing back the input unchanged is what keeps a short series exact.
func TestEnvelopePassesShortSeriesThrough(t *testing.T) {
	ts := []float64{0, 1, 2, 3}
	vs := []float64{9, 8, 7, 6}
	outT, outV := envelopeDecimate(ts, vs, 0, 4, 100)
	assert.Equal(t, ts, outT)
	assert.Equal(t, vs, outV)

	// A degenerate range cannot be bucketed; the caller gets its data back
	// rather than a divide by zero.
	outT, _ = envelopeDecimate(ts, vs, 2, 2, 100)
	assert.Equal(t, ts, outT)
}

// A decimated run keeps its endpoints, so the line still starts and ends
// where the data does rather than at the first bucket's extreme.
func TestEnvelopeKeepsEndpoints(t *testing.T) {
	n := 10000
	ts := make([]float64, n)
	vs := make([]float64, n)
	for i := range ts {
		ts[i] = float64(i)
		vs[i] = math.Sin(float64(i) / 100)
	}
	outT, outV := envelopeDecimate(ts, vs, 0, float64(n), 200)
	assert.Equal(t, ts[0], outT[0])
	assert.Equal(t, vs[0], outV[0])
	assert.Equal(t, ts[n-1], outT[len(outT)-1])
	assert.Equal(t, vs[n-1], outV[len(outV)-1])
	// Time never runs backwards in the output, or the polyline would fold.
	for i := 1; i < len(outT); i++ {
		require.GreaterOrEqual(t, outT[i], outT[i-1], "sample %d", i)
	}
}

// Segmentation is the "never fill" commitment made concrete: a line breaks at
// a null value AND at a grid gap, so nothing is drawn across time that
// carries no samples.
func TestSeriesSegmentsBreakAtNullsAndGaps(t *testing.T) {
	valid := []bool{true, true, true, true, true, true}
	assert.Equal(t, []seriesSegment{{0, 6}}, buildSeriesSegments(valid, nil),
		"a clean lane is one run")

	assert.Equal(t, []seriesSegment{{0, 2}, {3, 6}},
		buildSeriesSegments([]bool{true, true, false, true, true, true}, nil),
		"a null breaks the line rather than being interpolated across")

	assert.Equal(t, []seriesSegment{{0, 3}, {3, 6}}, buildSeriesSegments(valid, []int{3}),
		"a grid gap breaks it too, so the draw never spans the hole")

	assert.Empty(t, buildSeriesSegments([]bool{false, false}, nil))

	// A break landing on a null must not emit an empty run.
	assert.Equal(t, []seriesSegment{{0, 2}, {3, 5}},
		buildSeriesSegments([]bool{true, true, false, true, true}, []int{2}))
}

// The scaffolds carry the MEASURED step, written in the coarsest unit that
// stays exact — a 5-minute grid reads INTERVAL 5 MINUTE, not 300 SECOND.
func TestSeriesIntervalSpelling(t *testing.T) {
	for _, tc := range []struct {
		sec  float64
		n    int64
		unit string
	}{
		{1, 1, "SECOND"}, {30, 30, "SECOND"}, {90, 90, "SECOND"},
		{300, 5, "MINUTE"}, {3600, 1, "HOUR"}, {7200, 2, "HOUR"},
		{86400, 1, "DAY"}, {0.4, 1, "SECOND"},
	} {
		n, unit := seriesInterval(tc.sec)
		assert.Equal(t, tc.n, n, "%v s", tc.sec)
		assert.Equal(t, tc.unit, unit, "%v s", tc.sec)
	}
}

func TestSeriesScaffoldsCarryTheMeasuredStep(t *testing.T) {
	d := &SeriesDriver{tLabel: "ts", grid: seriesGrid{medianSec: 300}, lanes: []seriesLane{{label: "cpu"}}}
	assert.Contains(t, d.fillScaffold(), "ORDER BY ts WITH FILL STEP INTERVAL 5 MINUTE")
	assert.Contains(t, d.gridScaffold(), "toStartOfInterval(ts, INTERVAL 5 MINUTE)")
	assert.Contains(t, d.gridScaffold(), "avg(cpu)")
	assert.Contains(t, d.orderScaffold(), "ORDER BY ts")
}

// The class names reach the status line, so they have to read as findings a
// user can act on rather than as enum spellings.
func TestSeriesGridClassNames(t *testing.T) {
	seen := map[string]bool{}
	for _, cl := range []seriesGridE{seriesGridRegular, seriesGridGapped, seriesGridIrregular,
		seriesGridUnordered, seriesGridUnknown} {
		name := fmt.Sprint(cl)
		assert.NotEmpty(t, name)
		assert.False(t, seen[name], "class names are distinct: %q", name)
		seen[name] = true
	}
}

// The series leaf can hold TWO plots — the series and its x-linked score plot
// — so the pane's height is a budget they SPLIT, not a value either takes.
// Stacked boxes clip as soon as their SUM exceeds the pane, and the bottom of
// each box is where implot draws its x tick labels: here, the UTC time axis. A
// series whose x axis is below the fold is a shape with no when, and the part
// of the y range under the clip reads as missing samples rather than as a
// cropped view. Same bug the Chart tab carried (ADR-0172), one plot harder.
func TestSeriesPlotHeightsSplitThePane(t *testing.T) {
	d := &SeriesDriver{}

	// No score channel: one box, at the preferred height until the pane is
	// shorter than it. The probe misses on the frame a Lazy tab comes back, so
	// the driver holds its last good answer — a miss must not resize anything.
	seriesH, scoreH := d.plotHeights()
	assert.Equal(t, float32(seriesPlotHeight), seriesH, "before the probe lands, the preferred height")
	assert.Zero(t, scoreH, "no score channel, no score box")

	d.paneH = 1000
	seriesH, _ = d.plotHeights()
	assert.Equal(t, float32(seriesPlotHeight), seriesH,
		"a tall pane does not stretch the box past its preferred height")

	d.paneH = 255
	seriesH, _ = d.plotHeights()
	assert.Equal(t, float32(255-seriesPaneSlack), seriesH)

	// With a score channel filled, the same budget is split — the series
	// keeping the larger share, because a score is read for WHERE its peaks
	// fall and that needs less height than reading a shape does.
	d.scores = seriesScores{t: []float64{1, 2, 3}}
	d.paneH = 0
	seriesH, scoreH = d.plotHeights()
	assert.Positive(t, scoreH, "a filled score channel gets a box")
	assert.Greater(t, seriesH, scoreH, "the series keeps the larger share")

	// The property, over every pane the pair can fit in at all: BOTH boxes,
	// not merely either one. The smallest such pane is two floors plus the
	// slack — under it no split fits and the leaf's ScrollArea takes over,
	// which is the honest failure.
	for _, pane := range []float32{2*seriesPlotMinH + seriesPaneSlack, 176, 200, 255, 340, 500, 1000} {
		d.paneH = pane

		d.scores = seriesScores{}
		seriesH, scoreH = d.plotHeights()
		assert.LessOrEqualf(t, seriesH+scoreH, pane, "one box must fit a %vpt pane", pane)

		d.scores = seriesScores{t: []float64{1, 2, 3}}
		seriesH, scoreH = d.plotHeights()
		assert.LessOrEqualf(t, seriesH+scoreH, pane, "BOTH boxes must fit a %vpt pane", pane)
	}

	// The OTHER way these labels are lost, and the one that cost the most to
	// find. implot's gutters come out of the box height rather than from space
	// outside it, so a box under its minimum clips its own time axis while the
	// pane still looks roomy — and splitting a tight budget past that floor
	// buys nothing, because the gutters are laid out at the floor whatever
	// height the box is handed. An earlier version of this split went
	// proportional below the floor to keep the pair inside the pane; it simply
	// traded a clip by the pane for a clip by the canvas.
	assert.GreaterOrEqual(t, seriesPlotMinH, implot.MinBoxHeight(false, false, true, 1),
		"the floor must clear what these axes need")
	for _, pane := range []float32{20, 40, 100, 140, 176, 340, 1000} {
		d.paneH = pane
		d.scores = seriesScores{t: []float64{1, 2, 3}}
		seriesH, scoreH = d.plotHeights()
		assert.GreaterOrEqualf(t, seriesH, seriesPlotMinH, "series box, %vpt pane", pane)
		assert.GreaterOrEqualf(t, scoreH, seriesPlotMinH, "score box, %vpt pane", pane)

		d.scores = seriesScores{}
		seriesH, _ = d.plotHeights()
		assert.GreaterOrEqualf(t, seriesH, seriesPlotMinH, "lone box, %vpt pane", pane)
	}
}
