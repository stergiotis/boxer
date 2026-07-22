package colorscale

import (
	"math"
	"testing"
)

// axisRangeResolvable is the guard that keeps a degenerate value range out of
// the Talbot tick search (which would otherwise probe sub-ULP steps whose loops
// cannot advance — the World-map hang, where the map shaded by a uint64 id
// column near 2^63 with every row ~equal). computeAxis falls back to
// endpointsAxis for the rejected cases.
func TestAxisRangeResolvable(t *testing.T) {
	ok := [][2]float64{
		{0, 100}, {-5, 5}, {1e6, 1e6 + 1}, {0, 1e-6}, {1.5, 3.5},
	}
	for _, c := range ok {
		if !axisRangeResolvable(c[0], c[1]) {
			t.Errorf("axisRangeResolvable(%g,%g) = false, want true", c[0], c[1])
		}
	}
	bad := [][2]float64{
		{5, 5},                   // zero span
		{5, 4},                   // reversed
		{1.8e19, 1.8e19 + 20000}, // near-zero-width span at 2^63 (the hang)
		{math.Inf(1), 0},         // non-finite
		{0, math.NaN()},          // NaN
	}
	for _, c := range bad {
		if axisRangeResolvable(c[0], c[1]) {
			t.Errorf("axisRangeResolvable(%g,%g) = true, want false", c[0], c[1])
		}
	}
}
