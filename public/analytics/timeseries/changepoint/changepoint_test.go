package changepoint_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/analytics/timeseries/changepoint"
)

// The oracles below share no arithmetic with the implementation. Segment
// costs are computed from the samples of the segment directly, never from
// prefix sums, and the optimal segmentation is found by the unpruned dynamic
// programme over every admissible last change. A pruning rule that discarded
// a candidate it should have kept would agree with itself and disagree here.

func oracleRanks(vals []float64) (ranks []float64) {
	ranks = make([]float64, len(vals))
	for i, v := range vals {
		less, equal := 0, 0
		for _, w := range vals {
			switch {
			case w < v:
				less++
			case w == v:
				equal++
			}
		}
		ranks[i] = float64(less) + 0.5*float64(equal+1)
	}
	return
}

func oracleMeanSq(seg []float64) (mean float64, sumSq float64) {
	for _, v := range seg {
		mean += v
	}
	mean /= float64(len(seg))
	for _, v := range seg {
		sumSq += (v - mean) * (v - mean)
	}
	return
}

// oracleWorking returns the values a cost is evaluated on and the divisor of
// the squared deviations.
func oracleWorking(vals []float64, cost changepoint.CostE, sigma float64) (work []float64, div float64) {
	switch cost {
	case changepoint.CostMean:
		return vals, sigma * sigma
	case changepoint.CostRank:
		ranks := oracleRanks(vals)
		_, ss := oracleMeanSq(ranks)
		return ranks, ss / float64(len(ranks))
	}
	return vals, 1
}

func oracleCost(work []float64, div float64, cost changepoint.CostE, start int, end int) (c float64) {
	_, ss := oracleMeanSq(work[start:end])
	if cost == changepoint.CostMeanVar {
		return float64(end-start) * math.Log(ss/float64(end-start))
	}
	return ss / div
}

// oracleOptimal is optimal partitioning without pruning. It returns the
// minimal penalised cost, counting penalty once per change.
func oracleOptimal(work []float64, div float64, cost changepoint.CostE, minSeg int, penalty float64) (total float64) {
	n := len(work)
	best := make([]float64, n+1)
	for i := range best {
		best[i] = math.Inf(1)
	}
	best[0] = -penalty
	for t := minSeg; t <= n; t++ {
		for s := 0; s <= t-minSeg; s++ {
			if math.IsInf(best[s], 1) {
				continue
			}
			c := best[s] + oracleCost(work, div, cost, s, t) + penalty
			if c < best[t] {
				best[t] = c
			}
		}
	}
	return best[n]
}

func oraclePenalised(work []float64, div float64, cost changepoint.CostE, starts []int32, penalty float64) (total float64) {
	prev := 0
	for _, s := range starts {
		total += oracleCost(work, div, cost, prev, int(s))
		prev = int(s)
	}
	total += oracleCost(work, div, cost, prev, len(work))
	total += penalty * float64(len(starts))
	return
}

// steps builds a piecewise-constant series with unit Gaussian noise scaled by
// sigmas[i] in piece i.
func steps(seed uint64, lengths []int, levels []float64, sigmas []float64) (vals []float64) {
	rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
	for i, length := range lengths {
		for range length {
			vals = append(vals, levels[i]+sigmas[i]*rng.NormFloat64())
		}
	}
	return
}

func unit(count int) (sigmas []float64) {
	sigmas = make([]float64, count)
	for i := range sigmas {
		sigmas[i] = 1
	}
	return
}

func assertNear(t *testing.T, want []int32, got []int32, tol int32) {
	t.Helper()
	require.Len(t, got, len(want), "changes found: %v", got)
	for i := range want {
		assert.LessOrEqual(t, max(want[i]-got[i], got[i]-want[i]), tol, "change %d: want %d got %d", i, want[i], got[i])
	}
}

func drawSeries(t *rapid.T, minLen int, maxLen int) (vals []float64) {
	n := rapid.IntRange(minLen, maxLen).Draw(t, "len")
	pieces := rapid.IntRange(1, 4).Draw(t, "pieces")
	vals = make([]float64, n)
	level := 0.0
	for i := range vals {
		if pieces > 1 && i > 0 && i%(n/pieces+1) == 0 {
			level += float64(rapid.IntRange(-6, 6).Draw(t, "shift"))
		}
		// Quantised to a binary fraction, so the affine and monotone transforms
		// the invariance properties apply are exact and neither create nor
		// break a tie.
		vals[i] = level + float64(rapid.IntRange(-1024, 1024).Draw(t, fmt.Sprintf("e%d", i)))/1024
	}
	return
}

func TestPropertyPeltMatchesUnprunedOptimum(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cost := rapid.SampledFrom(changepoint.AllCosts).Draw(t, "cost")
		minSeg := rapid.IntRange(1, 5).Draw(t, "minSeg")
		if cost == changepoint.CostMeanVar {
			minSeg = max(minSeg, 2)
		}
		vals := drawSeries(t, 2*minSeg, 60)
		penalty := rapid.Float64Range(0.01, 30).Draw(t, "penalty")
		const sigma = 0.5
		series, err := changepoint.NewSeries(vals, changepoint.Config{Cost: cost, MinSegment: int32(minSeg), NoiseScale: sigma})
		require.NoError(t, err)
		seg, err := series.Pelt(penalty)
		require.NoError(t, err)

		work, div := oracleWorking(vals, cost, sigma)
		if div == 0 {
			t.Skip("constant series")
		}
		if cost == changepoint.CostMeanVar {
			// The implementation floors a segment variance; the oracle does
			// not, so a draw with an exactly constant stretch is outside what
			// the two can be compared on.
			for s := 0; s+minSeg <= len(vals); s++ {
				if _, ss := oracleMeanSq(vals[s : s+minSeg]); ss < 1e-6 {
					t.Skip("near-constant stretch")
				}
			}
		}
		want := oracleOptimal(work, div, cost, minSeg, penalty)
		got := oraclePenalised(work, div, cost, seg.Starts, penalty)
		assert.InDelta(t, want, got, 1e-6*(1+math.Abs(want)), "starts %v", seg.Starts)
		assert.InDelta(t, got-penalty*float64(len(seg.Starts)), seg.Cost, 1e-6*(1+math.Abs(got)))

		prev := int32(0)
		for _, s := range seg.Starts {
			assert.GreaterOrEqual(t, s-prev, int32(minSeg))
			prev = s
		}
		assert.GreaterOrEqual(t, int32(len(vals))-prev, int32(minSeg))
	})
}

func TestPeltFindsPlantedShifts(t *testing.T) {
	vals := steps(1, []int{200, 150, 250, 100}, []float64{0, 3, -1, 2}, unit(4))
	want := []int32{200, 350, 600}
	for _, cost := range changepoint.AllCosts {
		t.Run(cost.String(), func(t *testing.T) {
			series, err := changepoint.NewSeries(vals, changepoint.Config{Cost: cost, MinSegment: 5})
			require.NoError(t, err)
			seg, err := series.Pelt(series.PenaltyBIC())
			require.NoError(t, err)
			assertNear(t, want, seg.Starts, 5)
		})
	}
}

func TestPeltReportsNothingOnNoise(t *testing.T) {
	vals := steps(2, []int{2000}, []float64{5}, unit(1))
	for _, cost := range changepoint.AllCosts {
		series, err := changepoint.NewSeries(vals, changepoint.Config{Cost: cost, MinSegment: 5})
		require.NoError(t, err)
		seg, err := series.Pelt(series.PenaltyBIC())
		require.NoError(t, err)
		assert.Empty(t, seg.Starts, cost.String())
	}
}

func TestMeanVarSeesAChangeInSpreadThatMeanDoesNot(t *testing.T) {
	vals := steps(3, []int{300, 300}, []float64{0, 0}, []float64{1, 4})
	mv, err := changepoint.NewSeries(vals, changepoint.Config{Cost: changepoint.CostMeanVar, MinSegment: 10})
	require.NoError(t, err)
	seg, err := mv.Pelt(mv.PenaltyBIC())
	require.NoError(t, err)
	assertNear(t, []int32{300}, seg.Starts, 15)

	// The rank cost is blind to it by construction: the median did not move.
	rk, err := changepoint.NewSeries(vals, changepoint.Config{Cost: changepoint.CostRank, MinSegment: 10})
	require.NoError(t, err)
	seg, err = rk.Pelt(rk.PenaltyBIC())
	require.NoError(t, err)
	assert.Empty(t, seg.Starts)
}

func TestRankIgnoresOutliersThatMoveTheMeanCost(t *testing.T) {
	vals := steps(4, []int{300, 300}, []float64{0, 2}, unit(2))
	for _, at := range []int{50, 120, 410, 500} {
		vals[at] += 60
	}
	rk, err := changepoint.NewSeries(vals, changepoint.Config{Cost: changepoint.CostRank})
	require.NoError(t, err)
	seg, err := rk.Pelt(rk.PenaltyBIC())
	require.NoError(t, err)
	assertNear(t, []int32{300}, seg.Starts, 5)

	mean, err := changepoint.NewSeries(vals, changepoint.Config{Cost: changepoint.CostMean})
	require.NoError(t, err)
	seg, err = mean.Pelt(mean.PenaltyBIC())
	require.NoError(t, err)
	assert.Greater(t, len(seg.Starts), 1, "the mean cost isolates each outlier as a segment of its own")
}

func TestPropertyRankInvariantUnderIncreasingTransform(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vals := drawSeries(t, 10, 80)
		warped := make([]float64, len(vals))
		for i, v := range vals {
			warped[i] = v * v * v
			if v < 0 {
				warped[i] = v / 4
			}
		}
		penalty := rapid.Float64Range(0.5, 20).Draw(t, "penalty")
		a, err := changepoint.NewSeries(vals, changepoint.Config{Cost: changepoint.CostRank})
		require.NoError(t, err)
		b, err := changepoint.NewSeries(warped, changepoint.Config{Cost: changepoint.CostRank})
		require.NoError(t, err)
		sa, _ := a.Pelt(penalty)
		sb, _ := b.Pelt(penalty)
		assert.Equal(t, sa.Starts, sb.Starts)
	})
}

func TestPropertyMeanInvariantUnderAffineTransform(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vals := drawSeries(t, 10, 80)
		scale := rapid.SampledFrom([]float64{0.125, 0.5, 2, 64}).Draw(t, "scale")
		offset := float64(rapid.IntRange(-1000, 1000).Draw(t, "offset"))
		moved := make([]float64, len(vals))
		for i, v := range vals {
			moved[i] = scale*v + offset
		}
		a, err := changepoint.NewSeries(vals, changepoint.Config{})
		require.NoError(t, err)
		b, err := changepoint.NewSeries(moved, changepoint.Config{})
		require.NoError(t, err)
		if a.NoiseScale() == 0 {
			t.Skip("constant series")
		}
		assert.InEpsilon(t, scale*a.NoiseScale(), b.NoiseScale(), 1e-9)
		sa, _ := a.Pelt(a.PenaltyBIC())
		sb, _ := b.Pelt(b.PenaltyBIC())
		// Compared by cost, not by position: two segmentations can tie, and
		// which of them a tie resolves to is allowed to depend on rounding.
		work, div := oracleWorking(vals, changepoint.CostMean, a.NoiseScale())
		assert.InDelta(t,
			oraclePenalised(work, div, changepoint.CostMean, sa.Starts, a.PenaltyBIC()),
			oraclePenalised(work, div, changepoint.CostMean, sb.Starts, a.PenaltyBIC()),
			1e-6*(1+sa.Cost))
	})
}

func TestNoiseScaleMAD(t *testing.T) {
	// The estimate survives the shifts it is meant to look past.
	vals := steps(5, []int{500, 500, 500}, []float64{0, 20, -20}, []float64{2, 2, 2})
	assert.InDelta(t, 2.0, changepoint.NoiseScaleMAD(vals), 0.25)
	assert.Zero(t, changepoint.NoiseScaleMAD([]float64{3, 3, 3, 3}))
	assert.Zero(t, changepoint.NoiseScaleMAD([]float64{3}))
	// More than half the differences identical: the MAD is zero and the
	// root-mean-square difference stands in.
	assert.Positive(t, changepoint.NoiseScaleMAD([]float64{0, 0, 0, 0, 0, 0, 1, 1, 1}))
}

func TestConstantSeriesHasNoChanges(t *testing.T) {
	vals := make([]float64, 100)
	for i := range vals {
		vals[i] = 7
	}
	for _, cost := range changepoint.AllCosts {
		series, err := changepoint.NewSeries(vals, changepoint.Config{Cost: cost})
		require.NoError(t, err)
		seg, err := series.Pelt(1)
		require.NoError(t, err)
		assert.Empty(t, seg.Starts)
		bs, err := series.BinSeg(1, 0)
		require.NoError(t, err)
		assert.Empty(t, bs.Starts)
	}
}

func TestBinSegFindsPlantedShiftsAndHonoursTheLimit(t *testing.T) {
	vals := steps(6, []int{200, 150, 250, 100}, []float64{0, 3, 7, 2}, unit(4))
	series, err := changepoint.NewSeries(vals, changepoint.Config{MinSegment: 5})
	require.NoError(t, err)
	seg, err := series.BinSeg(series.PenaltyBIC(), 0)
	require.NoError(t, err)
	assertNear(t, []int32{200, 350, 600}, seg.Starts, 5)

	one, err := series.BinSeg(series.PenaltyBIC(), 1)
	require.NoError(t, err)
	require.Len(t, one.Starts, 1)
	assert.Contains(t, seg.Starts, one.Starts[0])
}

func TestPropertyBinSegNeverBeatsPelt(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cost := rapid.SampledFrom(changepoint.AllCosts).Draw(t, "cost")
		vals := drawSeries(t, 8, 80)
		penalty := rapid.Float64Range(0.1, 20).Draw(t, "penalty")
		series, err := changepoint.NewSeries(vals, changepoint.Config{Cost: cost, MinSegment: 2})
		require.NoError(t, err)
		exact, _ := series.Pelt(penalty)
		greedy, _ := series.BinSeg(penalty, 0)
		pe := exact.Cost + penalty*float64(exact.NumChanges())
		pg := greedy.Cost + penalty*float64(greedy.NumChanges())
		assert.LessOrEqual(t, pe, pg+1e-9*(1+math.Abs(pg)))
	})
}

func TestBinSegMissesWhatPeltFinds(t *testing.T) {
	// A short excursion in the middle of a long level: no single split lowers
	// the cost by much, because either half still contains most of the bump's
	// contrast averaged away.
	vals := steps(7, []int{400, 12, 400}, []float64{0, 2.2, 0}, unit(3))
	series, err := changepoint.NewSeries(vals, changepoint.Config{NoiseScale: 1})
	require.NoError(t, err)
	exact, _ := series.Pelt(series.PenaltyBIC())
	greedy, _ := series.BinSeg(series.PenaltyBIC(), 0)
	assert.Len(t, exact.Starts, 2)
	assert.Empty(t, greedy.Starts)
}

func TestPenaltyPath(t *testing.T) {
	vals := steps(8, []int{150, 150, 150, 150}, []float64{0, 3, -1, 2}, unit(4))
	series, err := changepoint.NewSeries(vals, changepoint.Config{MinSegment: 3})
	require.NoError(t, err)
	bic := series.PenaltyBIC()
	lo, hi := bic/4, bic*64
	path, err := series.PenaltyPath(lo, hi, 0)
	require.NoError(t, err)
	require.True(t, path.Complete)
	require.GreaterOrEqual(t, path.Len(), 3)

	for i := range path.Len() {
		if i > 0 {
			assert.Less(t, path.NumChanges[i], path.NumChanges[i-1])
			assert.GreaterOrEqual(t, path.Penalty[i], path.Penalty[i-1])
			assert.GreaterOrEqual(t, path.Cost[i], path.Cost[i-1])
		}
		assert.Len(t, path.Starts(i), int(path.NumChanges[i]))
		// Inside its own range, each row is what Pelt returns.
		upper := hi
		if i+1 < path.Len() {
			upper = path.Penalty[i+1]
		}
		if upper-path.Penalty[i] < 1e-9 {
			continue
		}
		seg, err := series.Pelt(0.5 * (path.Penalty[i] + upper))
		require.NoError(t, err)
		assert.Equal(t, path.NumChanges[i], seg.NumChanges(), "row %d", i)
		assert.InDelta(t, path.Cost[i], seg.Cost, 1e-6*(1+seg.Cost))
	}
	assert.Equal(t, lo, path.Penalty[0])
	// The three planted changes hold over the widest stretch of penalties.
	widest, at := 0.0, -1
	for i := range path.Len() {
		upper := hi
		if i+1 < path.Len() {
			upper = path.Penalty[i+1]
		}
		if w := math.Log(upper / path.Penalty[i]); w > widest && path.NumChanges[i] > 0 {
			widest, at = w, i
		}
	}
	require.GreaterOrEqual(t, at, 0)
	assert.Equal(t, int32(3), path.NumChanges[at])
}

func TestPropertyPenaltyPathCoversEveryPenalty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		vals := drawSeries(t, 12, 70)
		series, err := changepoint.NewSeries(vals, changepoint.Config{MinSegment: 2})
		require.NoError(t, err)
		if series.NoiseScale() == 0 {
			t.Skip("constant series")
		}
		path, err := series.PenaltyPath(0.5, 200, 1000)
		require.NoError(t, err)
		require.True(t, path.Complete)
		probe := rapid.Float64Range(0.5, 200).Draw(t, "probe")
		seg, _ := series.Pelt(probe)
		// The optimum at the probe costs what the best row of the path costs
		// there: no optimal segmentation is missing from the path.
		best := math.Inf(1)
		for i := range path.Len() {
			best = min(best, path.Cost[i]+probe*float64(path.NumChanges[i]))
		}
		assert.InDelta(t, seg.Cost+probe*float64(seg.NumChanges()), best, 1e-6*(1+best))
	})
}

func TestPenaltyPathBudget(t *testing.T) {
	vals := steps(9, []int{300}, []float64{0}, unit(1))
	series, err := changepoint.NewSeries(vals, changepoint.Config{})
	require.NoError(t, err)
	path, err := series.PenaltyPath(0.01, 100, 4)
	require.NoError(t, err)
	assert.False(t, path.Complete)
	assert.GreaterOrEqual(t, path.Len(), 2)
}

func TestSummarize(t *testing.T) {
	vals := []float64{1, 1, 1, 5, 7, 2}
	segs, err := changepoint.Summarize(vals, []int32{3, 5})
	require.NoError(t, err)
	assert.Equal(t, []int32{0, 3, 5}, segs.Start)
	assert.Equal(t, []int32{3, 5, 6}, segs.End)
	assert.Equal(t, []float64{1, 6, 2}, segs.Mean)
	assert.InDeltaSlice(t, []float64{0, math.Sqrt2, 0}, segs.StdDev, 1e-12)
	count := 0
	for i, s := range segs.All() {
		assert.Equal(t, segs.Start[i], s.Start)
		count++
	}
	assert.Equal(t, 3, count)

	_, err = changepoint.Summarize(vals, []int32{3, 3})
	assert.Error(t, err)
	_, err = changepoint.Summarize(vals, []int32{6})
	assert.Error(t, err)
	_, err = changepoint.Summarize(nil, nil)
	assert.Error(t, err)
}

func TestSeriesErrors(t *testing.T) {
	ok := []float64{1, 2, 3, 4, 5, 6}
	_, err := changepoint.NewSeries(ok[:1], changepoint.Config{})
	assert.Error(t, err)
	_, err = changepoint.NewSeries(ok[:3], changepoint.Config{Cost: changepoint.CostMeanVar})
	assert.Error(t, err)
	_, err = changepoint.NewSeries(ok, changepoint.Config{MinSegment: 4})
	assert.Error(t, err)
	_, err = changepoint.NewSeries(ok, changepoint.Config{MinSegment: -1})
	assert.Error(t, err)
	_, err = changepoint.NewSeries(ok, changepoint.Config{NoiseScale: -1})
	assert.Error(t, err)
	_, err = changepoint.NewSeries(ok, changepoint.Config{Cost: 99})
	assert.Error(t, err)
	_, err = changepoint.NewSeries([]float64{1, math.NaN(), 3}, changepoint.Config{})
	assert.Error(t, err)

	series, err := changepoint.NewSeries(ok, changepoint.Config{})
	require.NoError(t, err)
	_, err = series.Pelt(-1)
	assert.Error(t, err)
	_, err = series.Pelt(math.Inf(1))
	assert.Error(t, err)
	_, err = series.BinSeg(1, -1)
	assert.Error(t, err)
	_, err = series.PenaltyPath(2, 1, 0)
	assert.Error(t, err)
	assert.Equal(t, "mean", changepoint.CostMean.String())
	assert.Equal(t, "unknown", changepoint.CostE(9).String())
}

func BenchmarkPelt(b *testing.B) {
	for _, n := range []int{10_000, 100_000} {
		lengths := make([]int, n/500)
		levels := make([]float64, len(lengths))
		for i := range lengths {
			lengths[i] = 500
			levels[i] = float64(i%3) * 2
		}
		vals := steps(11, lengths, levels, unit(len(lengths)))
		series, err := changepoint.NewSeries(vals, changepoint.Config{})
		require.NoError(b, err)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for b.Loop() {
				_, _ = series.Pelt(series.PenaltyBIC())
			}
		})
	}
}
