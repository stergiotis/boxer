//go:build llm_generated_opus47

// Package letterval computes letter-value summaries (Hofmann, Wickham &
// Kafadar 2017) from any source that can answer Quantile/CDF queries.
//
// A letter-value plot generalises the boxplot: rather than a single box
// (IQR) plus whiskers, it nests boxes at quantiles 1/2^k and 1-1/2^k
// for increasing k. Each successive level is wider in value-space and
// halves the per-tail observation count, producing an "onion" view of
// the distribution that scales gracefully to large n (where boxplot's
// 1.5×IQR fence flags too many points as outliers).
//
// This package is oracle-agnostic: anything implementing QuantileOracle
// works (a t-digest, an exact sort-backed table, a ClickHouse pushdown
// helper). Computation is pure and side-effect free.
package letterval

import "math"

// QuantileOracle is the dependency surface needed to materialise a
// letter-value summary. Implementations include
// stats/tdigest.TDigest, exact sort-backed tables, and ClickHouse
// pushdown wrappers.
type QuantileOracle interface {
	// Quantile returns the value at quantile q ∈ [0, 1].
	Quantile(q float64) float64
	// CDF returns the estimated cumulative density at value x.
	CDF(x float64) float64
	// Count returns the number of observations behind the oracle.
	Count() int64
}

// LVLevel is one letter-value level. Depth 1 is the median (a single
// value, so LowerValue == UpperValue); deeper levels have distinct
// lower and upper quantile values.
type LVLevel struct {
	// Depth is the letter-value index: 1=M (median), 2=F (fourths/quartiles),
	// 3=E (eighths), 4=D (sixteenths), 5=C, 6=B, 7=A, 8=Z, ...
	Depth uint8
	// LowerQ is the lower quantile = 2^-Depth (0.5 when Depth=1).
	LowerQ float64
	// UpperQ is the upper quantile = 1 - 2^-Depth (0.5 when Depth=1).
	UpperQ float64
	// LowerValue is oracle.Quantile(LowerQ).
	LowerValue float64
	// UpperValue is oracle.Quantile(UpperQ).
	UpperValue float64
	// TailCount is the expected number of observations strictly below
	// LowerValue (equivalently strictly above UpperValue): n · 2^-Depth.
	// For Depth=1 this is approximately n/2.
	TailCount int64
}

// MinTailCount is the per-tail observation count Hofmann recommends as
// the floor for "trustworthy" deepest letter values. Used by
// RecommendedDepth.
const MinTailCount = 8

// MaxDepth is the upper bound enforced by RecommendedDepth; depth 16
// corresponds to a 1/65536 quantile, well past the regime where any
// quantile sketch maintains useful tail precision.
const MaxDepth = 16

// RecommendedDepth returns the deepest letter-value index k such that
// each tail at depth k still has ≈ MinTailCount observations,
// following Hofmann/Wickham/Kafadar (2017) eqn. ~ floor(log2(n/k)).
//
//	n ≤ 0   → 0    (nothing to render)
//	0 < n   → ≥ 1  (median always included)
func RecommendedDepth(n int64) uint8 {
	if n <= 0 {
		return 0
	}
	if n < 2*MinTailCount {
		return 1
	}
	d := math.Floor(math.Log2(float64(n) / float64(MinTailCount)))
	if d < 1 {
		return 1
	}
	if d > MaxDepth {
		return MaxDepth
	}
	return uint8(d)
}

// Levels materialises the LV summary for depths 1..maxDepth.
// Returns nil when the oracle is empty or maxDepth is 0.
func Levels(oracle QuantileOracle, maxDepth uint8) (out []LVLevel) {
	if maxDepth == 0 {
		return nil
	}
	// Clamp to MaxDepth to mirror RecommendedDepth's ceiling. Without
	// this, maxDepth == 255 (max uint8) wraps after `d++` in the loop
	// below and re-enters indefinitely, growing `out` without bound.
	if maxDepth > MaxDepth {
		maxDepth = MaxDepth
	}
	n := oracle.Count()
	if n == 0 {
		return nil
	}
	nF := float64(n)
	out = make([]LVLevel, 0, maxDepth)
	for d := uint8(1); d <= maxDepth; d++ {
		var qLow, qHigh float64
		if d == 1 {
			qLow = 0.5
			qHigh = 0.5
		} else {
			qLow = math.Ldexp(1.0, -int(d))
			qHigh = 1.0 - qLow
		}
		lv := LVLevel{
			Depth:      d,
			LowerQ:     qLow,
			UpperQ:     qHigh,
			LowerValue: oracle.Quantile(qLow),
			UpperValue: oracle.Quantile(qHigh),
			TailCount:  int64(nF * qLow),
		}
		out = append(out, lv)
	}
	return
}

// RecommendedLevels is a convenience: Levels(oracle, RecommendedDepth(n)).
func RecommendedLevels(oracle QuantileOracle) (out []LVLevel) {
	out = Levels(oracle, RecommendedDepth(oracle.Count()))
	return
}

// OutlierBudget is the per-tail expected observation count beyond the
// deepest rendered LV level. Drives whether a boxenplot renderer
// should draw outliers as discrete points (a-mode, when budget is
// small) or replace them with a count annotation (b-mode).
type OutlierBudget struct {
	Each  int64 // observations expected in each tail
	Total int64 // 2·Each (symmetric)
}

// BudgetFor returns the OutlierBudget implied by the deepest LV level
// in levels. Returns the zero value when levels is empty.
func BudgetFor(levels []LVLevel) (b OutlierBudget) {
	if len(levels) == 0 {
		return
	}
	deepest := levels[len(levels)-1]
	b.Each = deepest.TailCount
	b.Total = 2 * b.Each
	return
}
