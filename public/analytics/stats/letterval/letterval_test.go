package letterval_test

import (
	"math"
	"math/rand"
	"slices"
	"sort"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/stats/letterval"
	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time proof that TDigest satisfies QuantileOracle.
var _ letterval.QuantileOracle = (*tdigest.TDigest)(nil)

// exactOracle is a sort-backed reference oracle for testing the
// letter-value math in isolation from sketch error.
type exactOracle struct {
	sorted []float64
}

func newExactOracle(data []float64) *exactOracle {
	s := slices.Clone(data)
	sort.Float64s(s)
	return &exactOracle{sorted: s}
}

func (o *exactOracle) Count() int64 { return int64(len(o.sorted)) }

func (o *exactOracle) Quantile(q float64) float64 {
	n := len(o.sorted)
	if n == 0 {
		return math.NaN()
	}
	if q <= 0 {
		return o.sorted[0]
	}
	if q >= 1 {
		return o.sorted[n-1]
	}
	pos := q * float64(n-1)
	lo := int(math.Floor(pos))
	hi := lo + 1
	if hi >= n {
		return o.sorted[n-1]
	}
	t := pos - float64(lo)
	return o.sorted[lo]*(1-t) + o.sorted[hi]*t
}

func (o *exactOracle) CDF(x float64) float64 {
	n := len(o.sorted)
	if n == 0 {
		return math.NaN()
	}
	idx := sort.SearchFloat64s(o.sorted, x)
	return float64(idx) / float64(n)
}

func TestRecommendedDepth(t *testing.T) {
	cases := []struct {
		n    int64
		want uint8
	}{
		{0, 0},
		{1, 1},
		{7, 1},
		{15, 1},
		{16, 1},
		{17, 1},
		{32, 2},
		{64, 3},
		{128, 4},
		{1024, 7},
		{1_000_000, 16}, // clamped from log2(1e6/8) ≈ 16.93
		{1_000_000_000, letterval.MaxDepth},
	}
	for _, c := range cases {
		got := letterval.RecommendedDepth(c.n)
		assert.Equal(t, c.want, got, "n=%d", c.n)
	}
}

func TestLevelsEmpty(t *testing.T) {
	o := newExactOracle(nil)
	require.Nil(t, letterval.Levels(o, 5))
	require.Nil(t, letterval.RecommendedLevels(o))
}

func TestLevelsZeroMaxDepth(t *testing.T) {
	data := []float64{1, 2, 3, 4, 5}
	o := newExactOracle(data)
	require.Nil(t, letterval.Levels(o, 0))
}

func TestLevelsSinglePoint(t *testing.T) {
	o := newExactOracle([]float64{42})
	lvs := letterval.Levels(o, 3)
	require.Len(t, lvs, 3)
	for _, lv := range lvs {
		assert.InDelta(t, 42.0, lv.LowerValue, 1e-12)
		assert.InDelta(t, 42.0, lv.UpperValue, 1e-12)
	}
}

func TestLevelsBasicQuantiles(t *testing.T) {
	// Uniform 0..99 — quantiles are easy to verify.
	data := make([]float64, 100)
	for i := range data {
		data[i] = float64(i)
	}
	o := newExactOracle(data)
	lvs := letterval.Levels(o, 4)
	require.Len(t, lvs, 4)

	// Depth 1: median
	assert.Equal(t, uint8(1), lvs[0].Depth)
	assert.InDelta(t, 0.5, lvs[0].LowerQ, 1e-12)
	assert.InDelta(t, 0.5, lvs[0].UpperQ, 1e-12)
	assert.InDelta(t, 49.5, lvs[0].LowerValue, 1e-9)
	assert.InDelta(t, 49.5, lvs[0].UpperValue, 1e-9)
	assert.Equal(t, int64(50), lvs[0].TailCount)

	// Depth 2: quartiles
	assert.Equal(t, uint8(2), lvs[1].Depth)
	assert.InDelta(t, 0.25, lvs[1].LowerQ, 1e-12)
	assert.InDelta(t, 0.75, lvs[1].UpperQ, 1e-12)
	assert.InDelta(t, 24.75, lvs[1].LowerValue, 1e-9)
	assert.InDelta(t, 74.25, lvs[1].UpperValue, 1e-9)
	assert.Equal(t, int64(25), lvs[1].TailCount)

	// Depth 3: octiles
	assert.Equal(t, uint8(3), lvs[2].Depth)
	assert.InDelta(t, 0.125, lvs[2].LowerQ, 1e-12)
	assert.InDelta(t, 0.875, lvs[2].UpperQ, 1e-12)

	// Depth 4: sixteenths
	assert.Equal(t, uint8(4), lvs[3].Depth)
	assert.InDelta(t, 0.0625, lvs[3].LowerQ, 1e-12)
	assert.InDelta(t, 0.9375, lvs[3].UpperQ, 1e-12)
}

// TestLevelsNesting verifies the defining LV invariant: as depth grows,
// the interval [LowerValue, UpperValue] strictly contains all shallower
// intervals (for symmetric continuous data; equality is allowed for
// degenerate data).
func TestLevelsNesting(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	data := make([]float64, 100_000)
	for i := range data {
		data[i] = rnd.NormFloat64()
	}
	o := newExactOracle(data)
	lvs := letterval.Levels(o, 6)
	require.Len(t, lvs, 6)

	for i := 1; i < len(lvs); i++ {
		assert.LessOrEqual(t, lvs[i].LowerValue, lvs[i-1].LowerValue,
			"depth %d lower (%v) > depth %d lower (%v)",
			lvs[i].Depth, lvs[i].LowerValue, lvs[i-1].Depth, lvs[i-1].LowerValue)
		assert.GreaterOrEqual(t, lvs[i].UpperValue, lvs[i-1].UpperValue,
			"depth %d upper (%v) < depth %d upper (%v)",
			lvs[i].Depth, lvs[i].UpperValue, lvs[i-1].Depth, lvs[i-1].UpperValue)
	}
}

func TestLevelsTailCountHalves(t *testing.T) {
	o := newExactOracle(make([]float64, 1024))
	lvs := letterval.Levels(o, 6)
	expected := int64(512)
	for _, lv := range lvs {
		assert.Equal(t, expected, lv.TailCount, "depth %d", lv.Depth)
		expected /= 2
	}
}

func TestBudgetFor(t *testing.T) {
	require.Equal(t, letterval.OutlierBudget{}, letterval.BudgetFor(nil))

	o := newExactOracle(make([]float64, 1024))
	lvs := letterval.Levels(o, 7) // deepest tail = 1024/128 = 8
	b := letterval.BudgetFor(lvs)
	assert.Equal(t, int64(8), b.Each)
	assert.Equal(t, int64(16), b.Total)
}

func TestRecommendedLevelsRespectsCount(t *testing.T) {
	o := newExactOracle(make([]float64, 1000))
	lvs := letterval.RecommendedLevels(o)
	require.Len(t, lvs, int(letterval.RecommendedDepth(1000)))
}

// End-to-end test: feed TDigest data through letterval and check the
// LV values are close to the sort-exact reference.
func TestLevelsWithTDigest(t *testing.T) {
	rnd := rand.New(rand.NewSource(13))
	const n = 100_000
	data := make([]float64, n)
	for i := range data {
		data[i] = rnd.NormFloat64()
	}

	d := tdigest.NewTDigest()
	for _, v := range data {
		d.Push(v)
	}
	ref := newExactOracle(data)

	// Cap comparison at depth 8 — beyond that, each tail has < ~400
	// observations of a Gaussian and the individual sampled extremes
	// dominate over the distributional shape, so a sketch's tail
	// interpolation legitimately diverges from sort-exact values.
	maxDepth := uint8(8)
	lvsDigest := letterval.Levels(d, maxDepth)
	lvsExact := letterval.Levels(ref, maxDepth)
	require.Len(t, lvsDigest, len(lvsExact))

	for i := range lvsDigest {
		tol := 0.05 // standard-deviation units; Gaussian σ=1 → 5% slack
		assert.InDelta(t, lvsExact[i].LowerValue, lvsDigest[i].LowerValue, tol,
			"depth %d lower", lvsDigest[i].Depth)
		assert.InDelta(t, lvsExact[i].UpperValue, lvsDigest[i].UpperValue, tol,
			"depth %d upper", lvsDigest[i].Depth)
	}
}
