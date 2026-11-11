package play

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/analytics/timeseries/changepoint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// play_ts_changepoint_test.go checks the change point ADAPTERS (ADR-0163
// Update 2026-11-11) the way play_ts_test.go checks the first four: each
// transform through the executor, against the package called directly. The
// methods have their own oracles in their own package.

// tsSteps is a piecewise-constant series with unit Gaussian noise.
func tsSteps(seed uint64, lengths []int, levels []float64) (vals []float64) {
	rng := rand.New(rand.NewPCG(seed, 17))
	for i, length := range lengths {
		for range length {
			vals = append(vals, levels[i]+rng.NormFloat64())
		}
	}
	return
}

func tsChangeSQL(call string) string {
	return "WITH b AS (SELECT 1 AS t, 1 AS v), c AS (SELECT " + call + " FROM b) SELECT 1"
}

func tsMsCol(t *testing.T, rec arrow.RecordBatch, name string) (out []int64) {
	t.Helper()
	idx := rec.Schema().FieldIndices(name)
	require.Len(t, idx, 1, "column %q", name)
	arr, ok := rec.Column(idx[0]).(*array.Timestamp)
	require.True(t, ok)
	for i := range arr.Len() {
		out = append(out, int64(arr.Value(i)))
	}
	return
}

func tsIntCol(t *testing.T, rec arrow.RecordBatch, name string) []int64 {
	t.Helper()
	idx := rec.Schema().FieldIndices(name)
	require.Len(t, idx, 1, "column %q", name)
	arr, ok := rec.Column(idx[0]).(*array.Int64)
	require.True(t, ok)
	return arr.Int64Values()
}

func tsStrCol(t *testing.T, rec arrow.RecordBatch, name string) (out []string) {
	t.Helper()
	idx := rec.Schema().FieldIndices(name)
	require.Len(t, idx, 1, "column %q", name)
	arr, ok := rec.Column(idx[0]).(*array.String)
	require.True(t, ok)
	for i := range arr.Len() {
		out = append(out, arr.Value(i))
	}
	return
}

func tsBoolCol(t *testing.T, rec arrow.RecordBatch, name string) (out []bool) {
	t.Helper()
	idx := rec.Schema().FieldIndices(name)
	require.Len(t, idx, 1, "column %q", name)
	arr, ok := rec.Column(idx[0]).(*array.Boolean)
	require.True(t, ok)
	for i := range arr.Len() {
		out = append(out, arr.Value(i))
	}
	return
}

// tsGridMs is the time tsTestInput gives sample i.
func tsGridMs(i int32) int64 { return int64(i) * 60_000 }

func TestTsSegmentationsMatchThePackage(t *testing.T) {
	vals := tsSteps(1, []int{120, 90, 150}, []float64{0, 4, 1})
	for _, tc := range []struct {
		call   string
		cost   changepoint.CostE
		min    int32
		factor float64
		greedy bool
	}{
		{"tsPeltMean(t, v, 5, 1)", changepoint.CostMean, 5, 1, false},
		{"tsPeltMeanVar(t, v, 20, 1.5)", changepoint.CostMeanVar, 20, 1.5, false},
		{"tsPeltRank(t, v, 5, 2)", changepoint.CostRank, 5, 2, false},
		{"tsBinSegMean(t, v, 5, 1)", changepoint.CostMean, 5, 1, true},
	} {
		t.Run(tc.call, func(t *testing.T) {
			rec, err := tsRunCall(t, tsChangeSQL(tc.call), vals)
			require.NoError(t, err)
			defer rec.Release()

			series, err := changepoint.NewSeries(vals, changepoint.Config{Cost: tc.cost, MinSegment: tc.min})
			require.NoError(t, err)
			var seg changepoint.Segmentation
			if tc.greedy {
				seg, err = series.BinSeg(tc.factor*series.PenaltyBIC(), 0)
			} else {
				seg, err = series.Pelt(tc.factor * series.PenaltyBIC())
			}
			require.NoError(t, err)
			require.Len(t, seg.Starts, 2, "the fixture's two shifts are found")
			want, err := changepoint.Summarize(vals, seg.Starts)
			require.NoError(t, err)

			from, to := tsMsCol(t, rec, timelineSlotBandFrom), tsMsCol(t, rec, timelineSlotBandTo)
			require.Len(t, from, 3)
			for i := range from {
				assert.Equal(t, tsGridMs(want.Start[i]), from[i])
				if i+1 < len(from) {
					assert.Equal(t, from[i+1], to[i], "the bands tile: each ends where the next begins")
				}
			}
			assert.Equal(t, tsGridMs(int32(len(vals)-1)), to[2], "the last band ends on the last sample")
			assert.Equal(t, want.Mean, tsFloatCol(t, rec, "level"))
			assert.Equal(t, want.StdDev, tsFloatCol(t, rec, "spread"))
			assert.Equal(t, []int64{int64(want.End[0]), int64(want.End[1] - want.Start[1]), int64(want.End[2] - want.Start[2])},
				tsIntCol(t, rec, "samples"))
			shift := tsFloatCol(t, rec, "shift")
			assert.Zero(t, shift[0], "the first segment shifted from nothing")
			assert.InDelta(t, want.Mean[1]-want.Mean[0], shift[1], 1e-12)

			// The colour carries the direction of the shift into the segment,
			// and every token resolves — a miss would drop the band silently.
			colors := tsStrCol(t, rec, timelineSlotBandColor)
			assert.Equal(t, []string{"neutral.default", "warning.default", "info.default"}, colors)
			for _, name := range colors {
				_, ok := bandColorByName(name)
				assert.True(t, ok, name)
			}
			labels := tsStrCol(t, rec, timelineSlotBandLabel)
			assert.Contains(t, labels[1], "(+")
			assert.Contains(t, labels[2], "(-")

			spans, skipped := foldSeriesSpans(rec)
			assert.Len(t, spans, 3, "the Series pane reads the output as it is")
			assert.Zero(t, skipped)
		})
	}
}

func TestTsPenaltyIsInMultiplesOfBIC(t *testing.T) {
	vals := tsSteps(2, []int{200, 200}, []float64{0, 0.9})
	loose, err := tsRunCall(t, tsChangeSQL("tsPeltMean(t, v, 5, 0.5)"), vals)
	require.NoError(t, err)
	defer loose.Release()
	strict, err := tsRunCall(t, tsChangeSQL("tsPeltMean(t, v, 5, 40)"), vals)
	require.NoError(t, err)
	defer strict.Release()
	assert.Greater(t, loose.NumRows(), strict.NumRows())
	assert.Equal(t, int64(1), strict.NumRows(), "a penalty far above BIC leaves one segment")

	_, err = tsRunCall(t, tsChangeSQL("tsPeltMean(t, v, 5, 0)"), vals)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "above zero")
}

func TestTsPenaltyPathMatchesThePackage(t *testing.T) {
	vals := tsSteps(3, []int{150, 150, 150}, []float64{0, 3, -1})
	rec, err := tsRunCall(t, tsChangeSQL("tsPeltMeanPath(t, v, 5)"), vals)
	require.NoError(t, err)
	defer rec.Release()

	series, err := changepoint.NewSeries(vals, changepoint.Config{MinSegment: 5})
	require.NoError(t, err)
	bic := series.PenaltyBIC()
	path, err := series.PenaltyPath(tsPathPenaltyLo*bic, tsPathPenaltyHi*bic, 0)
	require.NoError(t, err)

	changes := tsIntCol(t, rec, "changes")
	lo, hi := tsFloatCol(t, rec, "penalty_from"), tsFloatCol(t, rec, "penalty_to")
	octaves := tsFloatCol(t, rec, "octaves")
	require.Len(t, changes, path.Len())
	widest := 0
	for i := range changes {
		assert.Equal(t, int64(path.NumChanges[i]), changes[i])
		assert.InDelta(t, path.Penalty[i]/bic, lo[i], 1e-12)
		if i+1 < len(changes) {
			assert.Equal(t, lo[i+1], hi[i], "the ranges are contiguous")
		}
		if octaves[i] > octaves[widest] {
			widest = i
		}
	}
	assert.Equal(t, tsPathPenaltyLo, lo[0])
	assert.Equal(t, tsPathPenaltyHi, hi[len(hi)-1])
	assert.Equal(t, int64(2), changes[widest], "the planted count holds over the widest range")

	// A row's range, typed back as the penalty, reproduces the row's count.
	mid := math.Sqrt(lo[widest] * hi[widest])
	seg, err := series.Pelt(mid * bic)
	require.NoError(t, err)
	assert.Equal(t, int32(2), seg.NumChanges())
	for _, c := range tsBoolCol(t, rec, "complete") {
		assert.Equal(t, path.Complete, c)
	}
}

func TestTsMosumMatchesThePackage(t *testing.T) {
	vals := tsSteps(4, []int{200, 200}, []float64{0, 3})
	want, err := changepoint.Mosum(vals, changepoint.MosumConfig{Bandwidth: 40})
	require.NoError(t, err)
	require.Len(t, want.Starts, 1)

	rec, err := tsRunCall(t, tsChangeSQL("tsMosumScores(t, v, 40)"), vals)
	require.NoError(t, err)
	defer rec.Release()
	assert.Equal(t, want.Stat, tsFloatCol(t, rec, "score"))
	warm := tsBoolCol(t, rec, "warm_up")
	assert.True(t, warm[39])
	assert.False(t, warm[40])
	assert.False(t, warm[len(vals)-40])
	assert.True(t, warm[len(vals)-39])
	for _, th := range tsFloatCol(t, rec, "threshold") {
		assert.Equal(t, want.Critical, th)
	}
	sc, ok := foldSeriesScores(rec)
	require.True(t, ok, "the Series pane's score channel reads the output as it is")
	assert.Equal(t, want.Critical, sc.threshold)

	segs, err := tsRunCall(t, tsChangeSQL("tsMosumSegments(t, v, 40)"), vals)
	require.NoError(t, err)
	defer segs.Release()
	from := tsMsCol(t, segs, timelineSlotBandFrom)
	require.Len(t, from, 2)
	assert.Equal(t, tsGridMs(want.Starts[0]), from[1])
}

func TestTsMosumScoreIsBoundedForDrawing(t *testing.T) {
	vals := make([]float64, 90)
	for i := 45; i < 90; i++ {
		vals[i] = 1
	}
	rec, err := tsRunCall(t, tsChangeSQL("tsMosumScores(t, v, 10)"), vals)
	require.NoError(t, err)
	defer rec.Release()
	score := tsFloatCol(t, rec, "score")
	threshold := tsFloatCol(t, rec, "threshold")[0]
	assert.Equal(t, tsScoreCapFactor*threshold, score[45], "an infinite statistic is drawn at the cap")
}

func TestTsCusumMatchesTheChart(t *testing.T) {
	vals := tsSteps(5, []int{400, 300}, []float64{0, 3})
	const warmup = 100
	chart, err := changepoint.NewCusum(changepoint.CusumConfig{Warmup: warmup})
	require.NoError(t, err)
	wantScore := make([]float64, len(vals))
	wantWarm := make([]bool, len(vals))
	type alarm struct {
		at    int
		onset int64
		up    bool
	}
	var alarms []alarm
	for i, v := range vals {
		r, pErr := chart.Push(v)
		require.NoError(t, pErr)
		wantScore[i], wantWarm[i] = max(r.Upper, r.Lower), r.WarmUp
		if r.Alarm {
			alarms = append(alarms, alarm{i, r.Onset, r.Up})
		}
	}
	require.NotEmpty(t, alarms)

	rec, err := tsRunCall(t, tsChangeSQL("tsCusumScores(t, v, 100)"), vals)
	require.NoError(t, err)
	defer rec.Release()
	assert.Equal(t, wantScore, tsFloatCol(t, rec, "score"))
	assert.Equal(t, wantWarm, tsBoolCol(t, rec, "warm_up"))
	assert.Equal(t, changepoint.DefaultCusumThreshold, tsFloatCol(t, rec, "threshold")[0])
	upper, lower := tsFloatCol(t, rec, "upper"), tsFloatCol(t, rec, "lower")
	for i := range wantScore {
		assert.Equal(t, wantScore[i], max(upper[i], lower[i]))
	}

	bands, err := tsRunCall(t, tsChangeSQL("tsCusumAlarms(t, v, 100)"), vals)
	require.NoError(t, err)
	defer bands.Release()
	from, to := tsMsCol(t, bands, timelineSlotBandFrom), tsMsCol(t, bands, timelineSlotBandTo)
	delay := tsIntCol(t, bands, "delay")
	direction := tsStrCol(t, bands, "direction")
	require.Len(t, from, len(alarms))
	sawShift := false
	for i, a := range alarms {
		assert.Equal(t, tsGridMs(int32(a.onset)), from[i])
		assert.Equal(t, tsGridMs(int32(a.at)), to[i])
		assert.Equal(t, int64(a.at)-a.onset, delay[i])
		assert.Equal(t, a.up, direction[i] == "up")
		if a.up && a.at > 400 && a.at < 420 {
			sawShift = true
		}
	}
	assert.True(t, sawShift, "the planted shift raises an upward alarm within a few samples")
	for _, name := range tsStrCol(t, bands, timelineSlotBandColor) {
		_, ok := bandColorByName(name)
		assert.True(t, ok, name)
	}

	_, err = tsRunCall(t, tsChangeSQL("tsCusumScores(t, v, 1)"), vals)
	assert.Error(t, err)
}

func TestTsCusumAlarmsMayBeEmpty(t *testing.T) {
	vals := make([]float64, 300)
	for i := range vals {
		vals[i] = float64(i % 2)
	}
	rec, err := tsRunCall(t, tsChangeSQL("tsCusumAlarms(t, v, 50)"), vals)
	require.NoError(t, err)
	defer rec.Release()
	assert.Zero(t, rec.NumRows())
}

func TestTsBocpdMatchesTheDetector(t *testing.T) {
	vals := tsSteps(6, []int{300, 300}, []float64{0, 4})
	prior, err := changepoint.PriorFromSample(vals[:tsBocpdPriorSamples])
	require.NoError(t, err)
	det, err := changepoint.NewBocpd(changepoint.BocpdConfig{ExpectedRun: 200, Prior: prior, Lag: 8})
	require.NoError(t, err)

	rec, err := tsRunCall(t, tsChangeSQL("tsBocpdScores(t, v, 200, 8)"), vals)
	require.NoError(t, err)
	defer rec.Release()
	score, warm, run := tsFloatCol(t, rec, "score"), tsBoolCol(t, rec, "warm_up"), tsIntCol(t, rec, "run")
	peak, peakAt := 0.0, -1
	for i, v := range vals {
		if i < tsBocpdPriorSamples {
			assert.True(t, warm[i], "the prior's prefix is not scored")
			assert.Zero(t, score[i])
			continue
		}
		r, pErr := det.Push(v)
		require.NoError(t, pErr)
		assert.Equal(t, int64(r.MapRun), run[i])
		if i < tsBocpdPriorSamples+8 {
			assert.True(t, warm[i], "a regime at most `lag` old by construction says nothing")
			assert.Zero(t, score[i])
			continue
		}
		assert.False(t, warm[i])
		assert.Equal(t, r.RecentChange, score[i])
		if score[i] > peak {
			peak, peakAt = score[i], i
		}
	}
	assert.Greater(t, peak, 0.9)
	assert.InDelta(t, 304, float64(peakAt), 8, "the probability peaks just after the planted shift")

	_, err = tsRunCall(t, tsChangeSQL("tsBocpdScores(t, v, 200, 8)"), vals[:40])
	assert.Error(t, err, "nothing would be scored")
	_, err = tsRunCall(t, tsChangeSQL("tsBocpdScores(t, v, 200, 0)"), vals)
	assert.Error(t, err)
	flat := make([]float64, 200)
	_, err = tsRunCall(t, tsChangeSQL("tsBocpdScores(t, v, 200, 8)"), flat)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "do not vary")
}

// --- the number argument ------------------------------------------------------

func TestSplitNumberArgument(t *testing.T) {
	for _, arg := range []string{"1", "2.5", "0.25", ".5", "{pen:Float64}"} {
		res, err := splitGraph(tsChangeSQL("tsPeltMean(t, v, 5, " + arg + ")"))
		require.NoError(t, err, arg)
		node, ok := findSplitNode(res, "c")
		require.True(t, ok)
		require.NotNil(t, node.Client, arg)
		assert.Equal(t, arg, node.Client.Args[3])
	}
	for _, arg := range []string{"-1", "1e3", "'x'", "v", "1 + 1", "inf"} {
		_, err := splitGraph(tsChangeSQL("tsPeltMean(t, v, 5, " + arg + ")"))
		require.Error(t, err, arg)
		assert.Contains(t, err.Error(), "penalty", arg)
	}
}

func TestTsNumberArgReadsLiteralsAndSlots(t *testing.T) {
	spec, ok := tsFuncByName("tsPeltMean")
	require.True(t, ok)
	call := &tsCall{Spec: spec, Args: []string{"t", "v", "5", "2.5"}}
	v, err := tsNumberArg(call, 3, nil)
	require.NoError(t, err)
	assert.Equal(t, 2.5, v)

	call.Args[3] = "{pen:Float64}"
	_, err = tsNumberArg(call, 3, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no value")
	v, err = tsNumberArg(call, 3, map[string]string{"param_pen": " 0.75 "})
	require.NoError(t, err)
	assert.Equal(t, 0.75, v)
	_, err = tsNumberArg(call, 3, map[string]string{"param_pen": "lots"})
	assert.Error(t, err)
}

// --- the registry's change point entries ----------------------------------------

func TestTsChangeRegistryIsConsistent(t *testing.T) {
	for _, spec := range tsFuncs {
		if !spec.Shipped {
			continue
		}
		names := make([]string, len(spec.Out))
		for i, o := range spec.Out {
			names[i] = o.Name
		}
		joined := strings.Join(names, ",")
		isBands := strings.HasPrefix(joined, timelineSlotBandFrom+","+timelineSlotBandTo)
		assert.Equal(t, isBands, spec.Bands != tsBandNone, "%s: a band output says what its bands are, and only a band output does", spec.Name)
		hasThreshold := strings.Contains(joined, ",threshold")
		assert.Equal(t, hasThreshold, spec.Threshold != "", "%s: a threshold column comes with the condition it holds under", spec.Name)
		if spec.NoBaseline != "" {
			assert.Contains(t, joined, "score", "%s: only a score lane has a baseline to decline", spec.Name)
		}
	}
}

// Every shipped function dispatches: a registry entry with no case in the
// executor would be recognised, accepted and then fail at run time.
func TestEveryShippedTsFunctionDispatches(t *testing.T) {
	vals := tsSteps(7, []int{150, 150}, []float64{0, 3})
	in := tsTestInput(t, vals)
	defer in.Release()
	for _, spec := range tsFuncs {
		if !spec.Shipped {
			continue
		}
		args := make([]string, len(spec.Args))
		for i, a := range spec.Args {
			switch a.Kind {
			case tsArgColumn:
				args[i] = a.Name
			case tsArgNumber:
				args[i] = "1"
			default:
				args[i] = "20"
			}
		}
		rec, err := (&tsCall{Spec: spec, Args: args}).Apply(in, nil, memory.NewGoAllocator())
		require.NoError(t, err, spec.Name)
		assert.Equal(t, tsOutputSchema(spec).String(), rec.Schema().String(), spec.Name)
		rec.Release()
	}
}

// --- the Series pane's reading of the new outputs --------------------------------

func TestSeriesSpanInventoryByKind(t *testing.T) {
	segments, _ := tsFuncByName("tsPeltMean")
	alarms, _ := tsFuncByName("tsCusumAlarms")
	extents, _ := tsFuncByName("tsAnomalySpans")

	line := seriesSpanInventory(&tsCall{Spec: segments}, 4)
	assert.Contains(t, line, "4 segment(s), so 3 change(s)")
	assert.Contains(t, line, "Two-sided")
	assert.Contains(t, line, "tsPeltMean")

	line = seriesSpanInventory(&tsCall{Spec: alarms}, 2)
	assert.Contains(t, line, "2 alarm(s)")
	assert.Contains(t, line, "detection delay")

	for _, call := range []*tsCall{nil, {Spec: extents}} {
		assert.Contains(t, seriesSpanInventory(call, 3), "3 flagged extent(s)")
		assert.Equal(t, tsBandExtent, seriesBandKind(call))
	}
	assert.Equal(t, "segment", seriesBandKind(&tsCall{Spec: segments}).String())
}

func TestChangeScoresDeclineTheAnomalyBaseline(t *testing.T) {
	spec, ok := tsFuncByName("tsCusumScores")
	require.True(t, ok)
	d := &SeriesDriver{
		lanes:         []seriesLane{{label: "v", vals: tsSine(200, 40)}},
		scoreCall:     &tsCall{Spec: spec, Args: []string{"t", "v", "50"}},
		scoreWindow:   50,
		scoreWindowOK: true,
	}
	baseline, why := d.buildSeriesBaseline()
	assert.Nil(t, baseline)
	assert.Contains(t, why, "not a baseline for a change score")

	d.scores = seriesScores{t: []float64{1}, detector: spec.Name, causal: true, window: 50,
		windowName: "warmup", threshold: 5, thresholdNote: spec.Threshold, baselineWhy: why}
	line := d.seriesOverlayCaption()
	assert.Contains(t, line, "tsCusumScores, warmup 50")
	assert.Contains(t, line, "The horizontal line at 5 is the CUSUM decision interval")
	assert.Contains(t, line, "KNOWN", "the threshold's condition travels with it")
	assert.Contains(t, line, "No baseline")
}

func TestChangeScaffoldsRecogniseAsClientCalls(t *testing.T) {
	d := &SeriesDriver{tLabel: "t", lanes: []seriesLane{{label: "v"}}, t: make([]float64, 1000)}
	scaffolds := d.changeScaffolds()
	require.Len(t, scaffolds, len(seriesChangeMethods))
	for i, s := range scaffolds {
		m := seriesChangeMethods[i]
		assert.Equal(t, m.Label, s.Question)
		assert.Contains(t, s.Scaffold, m.About, "the caveat is written into the buffer with the call")
		// With the placeholder filled in, the scaffold is a buffer the split
		// accepts, and every analysis CTE in it is a client node.
		sql := strings.Replace(s.Scaffold, "-- your query here: t, v, ordered by t",
			"SELECT toDateTime64(now(), 3) AS t, 1.0 AS v", 1)
		res, err := splitGraph(sql)
		require.NoError(t, err, m.ID)
		clients := 0
		for _, name := range []NodeID{"scores", "spans", "penalties"} {
			if node, ok := findSplitNode(res, name); ok {
				require.NotNil(t, node.Client, "%s: %s", m.ID, name)
				assert.Equal(t, NodeID("base"), node.Client.Input)
				clients++
			}
		}
		assert.Positive(t, clients, m.ID)
	}
}
