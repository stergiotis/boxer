package colorscale

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
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

// The gradient samples in palette space: the value painted at fraction t
// normalizes back to t on every scale, so a log bar's colour at the '1K'
// tick is the colour of 1000, not of 5e5.
func TestSampleAtNormalized(t *testing.T) {
	palette := []uint32{0x000000ff, 0xffffffff}
	lin := colormap.NewConfig(palette, 0, 10)
	logCm := colormap.NewConfig(palette, 1, 1e6)
	logCm.Scale = colormap.ScaleLogE
	db := colormap.NewConfig(palette, -80, 0)
	db.Scale = colormap.ScaleDbE
	for _, cm := range []*colormap.Config{lin, logCm, db} {
		for _, frac := range []float64{0, 0.25, 0.5, 0.75, 1} {
			if got := cm.Normalize(sampleAtNormalized(cm, frac)); math.Abs(got-frac) > 1e-9 {
				t.Errorf("scale %d: Normalize(sampleAtNormalized(%v)) = %v", cm.Scale, frac, got)
			}
		}
	}
	if got := sampleAtNormalized(logCm, 0.5); math.Abs(got-1000) > 1e-6 {
		t.Errorf("log midpoint: got %v want 1000", got)
	}
}
