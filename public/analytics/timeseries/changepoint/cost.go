package changepoint

import (
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// CostE selects what a segment is fitted with, and therefore what kind of
// change a segmentation can see.
type CostE uint8

const (
	// CostMean fits each segment with its own mean under one shared noise
	// scale: Σ(x−x̄)²/σ². It sees shifts in level and nothing else. The shared
	// scale is an input, see [Config.NoiseScale].
	CostMean CostE = iota
	// CostMeanVar fits each segment with its own mean and its own variance,
	// the Gaussian −2·log-likelihood up to a constant: len·log(σ̂² + ε). It
	// sees shifts in level and in spread, and needs segments of at least two
	// samples. ε is 1e-10 of the whole series' variance: without it a constant
	// stretch costs −∞ and the segmentation collapses onto every run of
	// repeated values. It is added rather than applied as a lower bound
	// because a bound would let merging two segments cost less than keeping
	// them apart, which is the one thing the pruning in [Series.Pelt] may not
	// meet.
	CostMeanVar
	// CostRank is [CostMean] applied to the mid-ranks of the values, scaled to
	// unit variance (Lung-Yut-Fong, Lévy-Leduc and Cappé, 2015). Ranks are
	// unchanged by any increasing transform of the data and bounded, so one
	// outlier moves a segment's cost by at most what one rank can. It sees a
	// shift in location of any distribution; a change in spread around an
	// unchanged median is invisible to it.
	CostRank
)

// AllCosts lists the costs in declaration order.
var AllCosts = []CostE{CostMean, CostMeanVar, CostRank}

// String returns the cost's name.
func (inst CostE) String() (name string) {
	switch inst {
	case CostMean:
		name = "mean"
	case CostMeanVar:
		name = "meanvar"
	case CostRank:
		name = "rank"
	default:
		name = "unknown"
	}
	return
}

// freeParams is how many parameters one more segment adds, which is what the
// BIC penalty counts beside the change's own location.
func (inst CostE) freeParams() (count int) {
	if inst == CostMeanVar {
		return 2
	}
	return 1
}

// minSegment is the shortest segment the cost is defined on.
func (inst CostE) minSegment() (length int32) {
	if inst == CostMeanVar {
		return 2
	}
	return 1
}

// varFloorRel is ε of [CostMeanVar], as a fraction of the whole series'
// variance.
const varFloorRel = 1e-10

// madToSigma converts a median absolute deviation to a Gaussian standard
// deviation: 1/Φ⁻¹(3/4).
const madToSigma = 1.4826022185056018

// NoiseScaleMAD estimates the standard deviation of the noise around a
// piecewise-constant level from the first differences: 1.4826·MAD(Δx)/√2.
//
// Differencing removes the level, so a shift contaminates one difference
// instead of every sample after it, and the median ignores those few. The
// estimate assumes the noise is independent from one sample to the next:
// positive autocorrelation makes neighbours agree, shrinks the differences
// and underestimates σ, and a segmentation run with that σ reports changes
// that are the noise's own wander. When the MAD is zero (more than half the
// differences are identical, as in a quantised or mostly-flat series) the
// root-mean-square difference is used instead, and a constant series yields 0.
func NoiseScaleMAD(vals []float64) (sigma float64) {
	n := len(vals)
	if n < 2 {
		return
	}
	diffs := make([]float64, n-1)
	for i := 1; i < n; i++ {
		diffs[i-1] = vals[i] - vals[i-1]
	}
	med := medianInPlace(diffs)
	var sumSq float64
	for i, d := range diffs {
		sumSq += d * d
		diffs[i] = math.Abs(d - med)
	}
	mad := medianInPlace(diffs)
	if mad > 0 {
		sigma = madToSigma * mad / math.Sqrt2
		return
	}
	sigma = math.Sqrt(sumSq/float64(n-1)) / math.Sqrt2
	return
}

// medianInPlace sorts vals and returns their median.
func medianInPlace(vals []float64) (median float64) {
	n := len(vals)
	if n == 0 {
		return
	}
	slices.Sort(vals)
	if n%2 == 1 {
		median = vals[n/2]
	} else {
		median = 0.5 * (vals[n/2-1] + vals[n/2])
	}
	return
}

// midRanks returns each value's rank among vals, 1-based, ties sharing the
// mean of the ranks they span.
func midRanks(vals []float64) (ranks []float64) {
	n := len(vals)
	order := make([]int32, n)
	for i := range order {
		order[i] = int32(i)
	}
	slices.SortFunc(order, func(a, b int32) int {
		switch {
		case vals[a] < vals[b]:
			return -1
		case vals[a] > vals[b]:
			return 1
		}
		return int(a - b)
	})
	ranks = make([]float64, n)
	i := 0
	for i < n {
		j := i + 1
		for j < n && vals[order[j]] == vals[order[i]] {
			j++
		}
		// positions i..j-1 hold ranks i+1..j
		mid := 0.5 * float64(i+1+j)
		for k := i; k < j; k++ {
			ranks[order[k]] = mid
		}
		i = j
	}
	return
}

// costTable answers a segment's cost in constant time from prefix sums.
type costTable struct {
	kind CostE
	// sum and sumSq are prefix sums of the working values: centred, and for
	// the mean and rank costs divided by the noise scale. Length n+1.
	sum   []float64
	sumSq []float64
	// varFloor is the ε the meanvar cost adds to a segment variance.
	varFloor float64
	// logUnit restores the meanvar cost to the caller's units: the working
	// values were divided by a scale, which shifts every log-variance by the
	// same amount.
	logUnit float64
	// flat marks a series with no variation at all, on which every cost is 0.
	flat bool
}

// normalise centres vals on their mean and divides by the largest deviation,
// so every later square is of a number in [−1, 1]: prefix sums of squares
// then neither overflow for a series near the top of the float range nor
// underflow to nothing for one near the bottom. scale is the divisor, 0 for
// a constant series. A series whose range itself is not representable is
// refused.
func normalise(vals []float64) (work []float64, scale float64, err error) {
	err = checkFinite(vals)
	if err != nil {
		return
	}
	var mean float64
	for i, v := range vals {
		mean += (v - mean) / float64(i+1)
	}
	for _, v := range vals {
		scale = max(scale, math.Abs(v-mean))
	}
	if math.IsInf(scale, 0) || math.IsNaN(scale) {
		err = eb.Build().Errorf("the series spans more than a float can hold the difference of")
		return
	}
	work = make([]float64, len(vals))
	if scale == 0 {
		return
	}
	for i, v := range vals {
		work[i] = (v - mean) / scale
	}
	return
}

// newCostTable builds the table over working values, each divided by scale
// (1 for meanvar).
func newCostTable(kind CostE, work []float64, scale float64) (inst costTable) {
	n := len(work)
	inst.kind = kind
	inst.sum = make([]float64, n+1)
	inst.sumSq = make([]float64, n+1)
	inv := 1.0
	if scale > 0 {
		inv = 1 / scale
	}
	for i, v := range work {
		w := v * inv
		inst.sum[i+1] = inst.sum[i] + w
		inst.sumSq[i+1] = inst.sumSq[i] + w*w
	}
	total := inst.sumSq[n]
	inst.flat = total == 0
	if n > 0 {
		inst.varFloor = varFloorRel * total / float64(n)
	}
	return
}

// cost returns the cost of the segment [start, end). The caller keeps
// end−start at or above the cost's minimum segment.
func (inst *costTable) cost(start int, end int) (cost float64) {
	if inst.flat {
		return
	}
	length := float64(end - start)
	s := inst.sum[end] - inst.sum[start]
	ss := inst.sumSq[end] - inst.sumSq[start] - s*s/length
	if ss < 0 {
		ss = 0
	}
	if inst.kind == CostMeanVar {
		cost = length * (math.Log(ss/length+inst.varFloor) + inst.logUnit)
		return
	}
	cost = ss
	return
}

// checkFinite refuses a series holding a NaN or an infinity, naming the first.
func checkFinite(vals []float64) (err error) {
	for i, v := range vals {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			err = eb.Build().Int("index", i).Errorf("the series holds a value that is not finite")
			return
		}
	}
	return
}
