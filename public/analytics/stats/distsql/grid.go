// Package distsql implements the ADR-0161 distribution result contract:
// the fixed probability grid the descriptiveStatistics macro emits, the
// claim validation rules the play Distribution panel applies, and the
// letterval.QuantileOracle adapter that feeds a boxenplot ladder from a
// server-side quantile grid. The macro expansion pass joins this package
// in ADR-0161's M1 milestone; M0 is contract-only.
package distsql

import (
	"math"
	"slices"
)

const (
	// gridDyadicMaxDepth is the deepest letter-value index the grid
	// carries: 2^-16 and its mirror, matching letterval.MaxDepth. The
	// panel clamps the *rendered* depth by letterval.RecommendedDepth(n);
	// the grid just has to reach at least as deep.
	gridDyadicMaxDepth = 16
	// gridUniformDenom spaces the uniform body grid (j/64) that gives the
	// ECDF curve its resolution between the dyadic rungs.
	gridUniformDenom = 64
	// GridLevelCount pins the generator's output length (ADR-0161 §SD2).
	// A change here is a contract change: expansion goldens and any
	// hand-written SQL mirroring the default grid move with it.
	GridLevelCount = 87
)

// gridTailLevels are the extreme tail probes beyond the dyadic ladder;
// mirrored around 1/2 by the generator.
var gridTailLevels = [...]float64{1e-3, 1e-4}

// GridLevels generates the ADR-0161 §SD2 probability grid: the dyadic
// letter-value ladder (2^-k and 1-2^-k for k = 1..16) ∪ the uniform body
// grid (j/64) ∪ mirrored tail probes — deduplicated, strictly ascending,
// every level in (0, 1). Each call returns a fresh slice.
//
// Deduplication relies on the dyadic overlaps (1/64, 1/32, …) being
// bit-identical in float64 no matter which formula produced them, which
// holds because all overlapping values are dyadic rationals and both
// formulas round to the same representable number.
func GridLevels() (out []float64) {
	seen := make(map[float64]struct{}, 128)
	add := func(p float64) {
		if p > 0 && p < 1 {
			seen[p] = struct{}{}
		}
	}
	for k := 1; k <= gridDyadicMaxDepth; k++ {
		p := math.Ldexp(1, -k)
		add(p)
		add(1 - p)
	}
	for j := 1; j < gridUniformDenom; j++ {
		add(float64(j) / gridUniformDenom)
	}
	for _, t := range gridTailLevels {
		add(t)
		add(1 - t)
	}
	out = make([]float64, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	slices.Sort(out)
	return
}

// DkwEpsilon is the closed-form half-width of the (1-alpha) DKW-Massart
// simultaneous band on F at sample size n: sqrt(ln(2/alpha) / (2n)).
// The Shift view uses it at alpha/2 per series to build its conservative
// combined band (ADR-0161 §SD5). Returns +Inf when n <= 0 (no data, no
// coverage) and NaN when alpha is outside (0, 1).
func DkwEpsilon(n int64, alpha float64) float64 {
	if alpha <= 0 || alpha >= 1 {
		return math.NaN()
	}
	if n <= 0 {
		return math.Inf(1)
	}
	return math.Sqrt(math.Log(2/alpha) / (2 * float64(n)))
}

// Wasserstein1 computes the 1-Wasserstein (earth mover's) distance between
// two distributions given on a shared probability grid, via the identity
// W1 = ∫₀¹ |Q_a(p) − Q_b(p)| dp, integrated by the trapezoid rule over the
// grid's span. The tails beyond the grid ([0, ps[0]) and (ps[last], 1])
// contribute nothing here — the result is the grid-truncated distance, a
// deliberate underestimate consistent with everything else the panel reads
// off the grid. Inputs must be same-length with len ≥ 2; anything else
// returns NaN (the caller has already validated the grids).
func Wasserstein1(ps, qsA, qsB []float64) float64 {
	if len(ps) < 2 || len(ps) != len(qsA) || len(ps) != len(qsB) {
		return math.NaN()
	}
	var w float64
	prev := math.Abs(qsA[0] - qsB[0])
	for i := 1; i < len(ps); i++ {
		cur := math.Abs(qsA[i] - qsB[i])
		w += 0.5 * (prev + cur) * (ps[i] - ps[i-1])
		prev = cur
	}
	return w
}
