package distsql

import (
	"math"
	"sort"

	"github.com/stergiotis/boxer/public/analytics/stats/letterval"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// GridOracle adapts a validated (ps, qs, n) quantile grid to
// letterval.QuantileOracle — the "ClickHouse pushdown wrapper" that
// package's doc anticipated. Quantile interpolates linearly between grid
// levels and clamps outside the grid's span; CDF is the inverse, with the
// upper end of a plateau on ties (matching CDF right-continuity). The
// oracle answers only as precisely as the grid it wraps — depth-clamping
// against n is the caller's job (letterval.RecommendedDepth).
type GridOracle struct {
	ps []float64
	qs []float64
	n  int64
}

var _ letterval.QuantileOracle = (*GridOracle)(nil)

// NewGridOracle validates the grid (ValidateSeries) and wraps it. The
// slices are retained, not copied.
func NewGridOracle(ps, qs []float64, n int64) (inst *GridOracle, err error) {
	err = ValidateSeries(ps, qs)
	if err != nil {
		return nil, eh.Errorf("grid oracle: %w", err)
	}
	if n < 0 {
		return nil, eb.Build().Int64("n", n).Errorf("grid oracle: n must not be negative")
	}
	inst = &GridOracle{ps: ps, qs: qs, n: n}
	return
}

func (inst *GridOracle) Count() int64 { return inst.n }

// Quantile returns Q(q) by linear interpolation over the grid; q at or
// beyond the grid's ends clamps to the end values.
func (inst *GridOracle) Quantile(q float64) float64 {
	ps, qs := inst.ps, inst.qs
	if q <= ps[0] {
		return qs[0]
	}
	last := len(ps) - 1
	if q >= ps[last] {
		return qs[last]
	}
	i := sort.SearchFloat64s(ps, q)
	if ps[i] == q {
		return qs[i]
	}
	frac := (q - ps[i-1]) / (ps[i] - ps[i-1])
	return qs[i-1] + frac*(qs[i]-qs[i-1])
}

// CDF returns the grid's estimate of F(x): values outside the grid's value
// span clamp to the end probabilities, exact plateau hits resolve to the
// plateau's upper probability, and strictly increasing segments
// interpolate linearly.
func (inst *GridOracle) CDF(x float64) float64 {
	ps, qs := inst.ps, inst.qs
	last := len(qs) - 1
	if x <= qs[0] {
		if x == qs[0] {
			return ps[upperOfPlateau(qs, 0)]
		}
		return ps[0]
	}
	if x >= qs[last] {
		return ps[last]
	}
	i := sort.SearchFloat64s(qs, x)
	if qs[i] == x {
		return ps[upperOfPlateau(qs, i)]
	}
	// qs[i-1] < x < qs[i] with qs[i-1] < qs[i]: a strict segment.
	frac := (x - qs[i-1]) / (qs[i] - qs[i-1])
	return ps[i-1] + frac*(ps[i]-ps[i-1])
}

// upperOfPlateau returns the last index sharing qs[i]'s value.
func upperOfPlateau(qs []float64, i int) int {
	for i+1 < len(qs) && qs[i+1] == qs[i] {
		i++
	}
	return i
}

// GridMaxDepth is letterval.RecommendedDepth additionally clamped by what
// the grid's tails can support: a letter-value rung at depth k needs the
// quantiles 2^-k and 1-2^-k, and a grid whose deepest tail level is ps[0]
// cannot answer past k = ⌊-log2 ps[0]⌋ — the oracle would clamp, stacking
// visually duplicate boxes at the tail (the GUI-drive F1 finding). The
// §SD2 default grid reaches 2^-16 and is never the binding constraint;
// sparse hand-written grids are.
func GridMaxDepth(ps []float64, n int64) uint8 {
	depth := letterval.RecommendedDepth(n)
	if depth <= 1 || len(ps) == 0 {
		return depth
	}
	lo := math.Floor(-math.Log2(ps[0]))
	hi := math.Floor(-math.Log2(1 - ps[len(ps)-1]))
	cap64 := math.Min(lo, hi)
	if cap64 < 1 {
		cap64 = 1 // the median is always drawable
	}
	if float64(depth) > cap64 {
		depth = uint8(cap64)
	}
	return depth
}
