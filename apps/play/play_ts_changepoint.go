package play

import (
	"math"
	"strconv"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/analytics/timeseries/changepoint"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// play_ts_changepoint.go holds the change point transforms (ADR-0163 Update
// 2026-11-11). Like the four in play_ts_transforms.go each is an adapter:
// read the arguments, call public/analytics/timeseries/changepoint, build the
// declared output. What the adapters add is the unit a person types in — a
// penalty in multiples of BIC — and the reading of a result as rows.

const (
	// tsPathPenaltyLo and tsPathPenaltyHi bound the penalty path, in
	// multiples of BIC. Below a half the optimum is mostly the noise being
	// segmented, and each run there is a row nobody reads; 64 is far enough
	// out that what survives it is not in doubt.
	tsPathPenaltyLo = 0.5
	tsPathPenaltyHi = 64.0
	// tsBocpdMaxRun truncates the run-length posterior. Every sample walks
	// that many hypotheses, so with the function's row ceiling it bounds the
	// wait; a regime that outlasts it is tracked as one hypothesis that stops
	// gaining confidence.
	tsBocpdMaxRun = 2000
	// tsBocpdPriorSamples is the prefix the run-length model's prior is taken
	// from. Those samples are then not scored: a prior taken from the samples
	// it goes on to judge would let each reading depend on more than its past.
	tsBocpdPriorSamples = 50
	// tsScoreCapFactor bounds an unbounded statistic for drawing, as a
	// multiple of its threshold. The moving-sum statistic is infinite where
	// two noiseless windows differ; that is a change by any standard, and one
	// infinite sample would flatten every other reading on the plot.
	tsScoreCapFactor = 100.0
)

// tsApplyPenalised runs one of the penalised segmentations: PELT, or binary
// segmentation when greedy is set, under the given cost.
func tsApplyPenalised(call *tsCall, ts []int64, vals []float64, params map[string]string, alloc memory.Allocator,
	cost changepoint.CostE, greedy bool) (rec arrow.RecordBatch, err error) {
	series, err := tsChangeSeries(call, vals, params, cost)
	if err != nil {
		return
	}
	factor, err := tsNumberArg(call, 3, params)
	if err != nil {
		return
	}
	if factor <= 0 {
		err = eb.Build().Str("function", call.Spec.Name).Errorf("the penalty must be above zero; 1 is the BIC penalty")
		return
	}
	var seg changepoint.Segmentation
	if greedy {
		seg, err = series.BinSeg(factor*series.PenaltyBIC(), 0)
	} else {
		seg, err = series.Pelt(factor * series.PenaltyBIC())
	}
	if err != nil {
		err = eh.Errorf("%w", err)
		return
	}
	return tsBuildSegments(call, ts, vals, seg.Starts, alloc)
}

// tsChangeSeries reads the minimum segment length and prepares the series.
func tsChangeSeries(call *tsCall, vals []float64, params map[string]string, cost changepoint.CostE) (series *changepoint.Series, err error) {
	minSegment, err := tsIntArg(call, 2, params)
	if err != nil {
		return
	}
	series, err = changepoint.NewSeries(vals, changepoint.Config{Cost: cost, MinSegment: minSegment})
	if err != nil {
		err = eh.Errorf("%w", err)
	}
	return
}

// tsBuildSegments lays a segmentation out on the band contract.
//
// A band runs to the first sample of the next segment, so the bands tile the
// series and each boundary is a change; the last runs to the last sample. The
// colour carries the direction of the shift INTO the segment — the one thing
// about a boundary a reader wants before reading a number.
func tsBuildSegments(call *tsCall, ts []int64, vals []float64, starts []int32, alloc memory.Allocator) (rec arrow.RecordBatch, err error) {
	segs, err := changepoint.Summarize(vals, starts)
	if err != nil {
		err = eh.Errorf("%w", err)
		return
	}
	n := segs.Len()
	from := make([]int64, n)
	to := make([]int64, n)
	labels := make([]string, n)
	colors := make([]string, n)
	samples := make([]int64, n)
	shifts := make([]float64, n)
	for i, s := range segs.All() {
		from[i] = ts[s.Start]
		to[i] = ts[min(int(s.End), len(ts)-1)]
		samples[i] = int64(s.End - s.Start)
		labels[i] = "level " + formatTsScore(s.Mean)
		colors[i] = "neutral.default"
		if i > 0 {
			shifts[i] = s.Mean - segs.Mean[i-1]
			labels[i] += " (" + formatTsSigned(shifts[i]) + ")"
			if shifts[i] >= 0 {
				colors[i] = "warning.default"
			} else {
				colors[i] = "info.default"
			}
		}
	}
	b := newTsBuilder(call.Spec, alloc, n)
	defer b.Release()
	b.time(0, from)
	b.time(1, to)
	b.strings(2, labels)
	b.strings(3, colors)
	b.floats(4, segs.Mean)
	b.floats(5, segs.StdDev)
	b.ints(6, samples)
	b.floats(7, shifts)
	return b.build(n)
}

// tsApplyPenaltyPath is the CROPS sensitivity table of tsPeltMean. Penalties
// are reported in the unit tsPeltMean takes them in, so a row's range can be
// typed back as an argument.
func tsApplyPenaltyPath(call *tsCall, _ []int64, vals []float64, params map[string]string, alloc memory.Allocator) (rec arrow.RecordBatch, err error) {
	series, err := tsChangeSeries(call, vals, params, changepoint.CostMean)
	if err != nil {
		return
	}
	bic := series.PenaltyBIC()
	path, err := series.PenaltyPath(tsPathPenaltyLo*bic, tsPathPenaltyHi*bic, 0)
	if err != nil {
		err = eh.Errorf("%w", err)
		return
	}
	n := path.Len()
	changes := make([]int64, n)
	at := make([]float64, n)
	lo := make([]float64, n)
	hi := make([]float64, n)
	octaves := make([]float64, n)
	complete := make([]bool, n)
	for i := range n {
		changes[i] = int64(path.NumChanges[i])
		at[i] = path.Found[i] / bic
		complete[i] = path.Complete
		// A range is known only when no segmentation is missing beside the
		// row. On a path the budget cut short, the row keeps the one penalty
		// it was found at and claims no width.
		lo[i], hi[i] = at[i], at[i]
		if !path.Complete {
			continue
		}
		lo[i], hi[i] = path.Penalty[i]/bic, tsPathPenaltyHi
		if i+1 < n {
			hi[i] = path.Penalty[i+1] / bic
		}
		if hi[i] > lo[i] {
			octaves[i] = math.Log2(hi[i] / lo[i])
		}
	}
	b := newTsBuilder(call.Spec, alloc, n)
	defer b.Release()
	b.ints(0, changes)
	b.floats(1, at)
	b.floats(2, lo)
	b.floats(3, hi)
	b.floats(4, octaves)
	b.floats(5, path.Cost)
	b.bools(6, complete)
	return b.build(n)
}

// tsMosum runs the moving-sum test at the call's bandwidth, with the level
// and the neighbourhood the package defaults to.
func tsMosum(call *tsCall, vals []float64, params map[string]string) (res changepoint.MosumResult, err error) {
	bandwidth, err := tsIntArg(call, 2, params)
	if err != nil {
		return
	}
	res, err = changepoint.Mosum(vals, changepoint.MosumConfig{Bandwidth: bandwidth})
	if err != nil {
		err = eh.Errorf("%w", err)
	}
	return
}

// tsApplyMosumScores is the statistic per sample, with its critical value.
func tsApplyMosumScores(call *tsCall, ts []int64, vals []float64, params map[string]string, alloc memory.Allocator) (rec arrow.RecordBatch, err error) {
	res, err := tsMosum(call, vals, params)
	if err != nil {
		return
	}
	n := len(ts)
	warm := make([]bool, n)
	limit := tsScoreCapFactor * res.Critical
	for i := range res.Stat {
		warm[i] = int32(i) < res.First || int32(i) > res.Last
		res.Stat[i] = min(res.Stat[i], limit)
	}
	b := newTsBuilder(call.Spec, alloc, n)
	defer b.Release()
	b.time(0, ts)
	b.floats(1, res.Stat)
	b.bools(2, warm)
	b.floats(3, tsConstant(res.Critical, n))
	return b.build(n)
}

// tsApplyMosumSegments is the segmentation the test's reported changes make.
func tsApplyMosumSegments(call *tsCall, ts []int64, vals []float64, params map[string]string, alloc memory.Allocator) (rec arrow.RecordBatch, err error) {
	res, err := tsMosum(call, vals, params)
	if err != nil {
		return
	}
	return tsBuildSegments(call, ts, vals, res.Starts, alloc)
}

// tsCusumRun pushes the series through the chart at its default allowance and
// threshold, estimating the in-control parameters from `warmup` samples.
func tsCusumRun(call *tsCall, vals []float64, params map[string]string) (readings []changepoint.CusumReading, threshold float64, err error) {
	warmup, err := tsIntArg(call, 2, params)
	if err != nil {
		return
	}
	if warmup < 2 {
		err = eb.Build().Str("function", call.Spec.Name).Errorf("warmup must be at least 2 samples; tens is where the estimate starts to be worth using")
		return
	}
	chart, err := changepoint.NewCusum(changepoint.CusumConfig{Warmup: warmup})
	if err != nil {
		err = eh.Errorf("%w", err)
		return
	}
	threshold = chart.Threshold()
	readings = make([]changepoint.CusumReading, len(vals))
	for i, v := range vals {
		readings[i], err = chart.Push(v)
		if err != nil {
			err = eh.Errorf("%w", err)
			return
		}
	}
	return
}

// tsApplyCusumScores is the chart per sample: the larger sum as the score,
// both sums beside it, and the decision interval.
func tsApplyCusumScores(call *tsCall, ts []int64, vals []float64, params map[string]string, alloc memory.Allocator) (rec arrow.RecordBatch, err error) {
	readings, threshold, err := tsCusumRun(call, vals, params)
	if err != nil {
		return
	}
	n := len(ts)
	score := make([]float64, n)
	upper := make([]float64, n)
	lower := make([]float64, n)
	warm := make([]bool, n)
	for i, r := range readings {
		score[i], upper[i], lower[i], warm[i] = max(r.Upper, r.Lower), r.Upper, r.Lower, r.WarmUp
	}
	b := newTsBuilder(call.Spec, alloc, n)
	defer b.Release()
	b.time(0, ts)
	b.floats(1, score)
	b.bools(2, warm)
	b.floats(3, tsConstant(threshold, n))
	b.floats(4, upper)
	b.floats(5, lower)
	return b.build(n)
}

// tsApplyCusumAlarms is one band per alarm, from the estimated onset to the
// sample that raised it.
func tsApplyCusumAlarms(call *tsCall, ts []int64, vals []float64, params map[string]string, alloc memory.Allocator) (rec arrow.RecordBatch, err error) {
	readings, _, err := tsCusumRun(call, vals, params)
	if err != nil {
		return
	}
	var from, to, delay []int64
	var labels, colors, directions []string
	for i, r := range readings {
		if !r.Alarm {
			continue
		}
		direction, color := "down", "info.default"
		if r.Up {
			direction, color = "up", "warning.default"
		}
		d := int64(i) - r.Onset
		from = append(from, ts[r.Onset])
		to = append(to, ts[i])
		delay = append(delay, d)
		directions = append(directions, direction)
		colors = append(colors, color)
		labels = append(labels, "shift "+direction+", alarm after "+strconv.FormatInt(d, 10)+" sample(s)")
	}
	n := len(from)
	b := newTsBuilder(call.Spec, alloc, n)
	defer b.Release()
	b.time(0, from)
	b.time(1, to)
	b.strings(2, labels)
	b.strings(3, colors)
	b.strings(4, directions)
	b.ints(5, delay)
	return b.build(n)
}

// tsApplyBocpdScores is the run-length posterior per sample: the probability
// that the regime is at most `lag` samples old, and its most probable length.
func tsApplyBocpdScores(call *tsCall, ts []int64, vals []float64, params map[string]string, alloc memory.Allocator) (rec arrow.RecordBatch, err error) {
	expectedRun, err := tsIntArg(call, 2, params)
	if err != nil {
		return
	}
	lag, err := tsIntArg(call, 3, params)
	if err != nil {
		return
	}
	if lag < 1 {
		err = eb.Build().Str("function", call.Spec.Name).Errorf("lag must be at least 1 sample")
		return
	}
	n := len(ts)
	skip := tsBocpdPriorSamples + int(lag)
	if n <= skip {
		err = eb.Build().Str("function", call.Spec.Name).Int("rows", n).Int("needed", skip+1).
			Errorf("the input is shorter than the prior's prefix plus the lag, so no sample would be scored")
		return
	}
	prior, err := changepoint.PriorFromSample(vals[:tsBocpdPriorSamples])
	if err != nil {
		err = eb.Build().Str("function", call.Spec.Name).Int("prefix", tsBocpdPriorSamples).
			Errorf("the prior is taken from the first samples of the input: %w", err)
		return
	}
	det, err := changepoint.NewBocpd(changepoint.BocpdConfig{
		ExpectedRun: float64(expectedRun), Prior: prior, Lag: lag,
		MaxRun: int32(min(changepoint.DefaultBocpdRunFactor*int64(expectedRun), tsBocpdMaxRun)),
	})
	if err != nil {
		err = eh.Errorf("%w", err)
		return
	}
	score := make([]float64, n)
	run := make([]int64, n)
	warm := make([]bool, n)
	for i := range n {
		// The first `lag` scored samples are in a regime at most `lag` old by
		// construction, so their probability says nothing and is not shown.
		warm[i] = i < skip
		if i < tsBocpdPriorSamples {
			continue
		}
		r, pErr := det.Push(vals[i])
		if pErr != nil {
			err = eh.Errorf("%w", pErr)
			return
		}
		run[i] = int64(r.MapRun)
		if !warm[i] {
			score[i] = r.RecentChange
		}
	}
	b := newTsBuilder(call.Spec, alloc, n)
	defer b.Release()
	b.time(0, ts)
	b.floats(1, score)
	b.bools(2, warm)
	b.ints(3, run)
	return b.build(n)
}

// tsConstant is a column holding one value on every row.
func tsConstant(v float64, n int) (out []float64) {
	out = make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return
}

// formatTsSigned renders a difference with its sign, at the precision band
// labels use.
func formatTsSigned(v float64) string {
	if v >= 0 {
		return "+" + formatTsScore(v)
	}
	return formatTsScore(v)
}
