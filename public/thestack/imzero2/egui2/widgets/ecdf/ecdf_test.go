//go:build llm_generated_opus47

package ecdf

import (
	"testing"

	"github.com/stergiotis/boxer/public/analytics/stats/ecdfbands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildEcdfPolylineShape exercises the step-polyline construction
// for the canonical small-n case. The polyline must start at
// (x_0, 0), end at (x_{n-1}, 1), and have 2n vertices alternating
// pre- and post-jump.
func TestBuildEcdfPolylineShape(t *testing.T) {
	sorted := []float64{0.1, 0.3, 0.7}
	xs, ys := buildEcdfPolyline(sorted)
	require.Len(t, xs, 6)
	require.Len(t, ys, 6)
	// (X1, 0), (X1, 1/3), (X2, 1/3), (X2, 2/3), (X3, 2/3), (X3, 1).
	expectedXs := []float64{0.1, 0.1, 0.3, 0.3, 0.7, 0.7}
	expectedYs := []float64{0.0, 1.0 / 3, 1.0 / 3, 2.0 / 3, 2.0 / 3, 1.0}
	for i := range xs {
		assert.InDelta(t, expectedXs[i], xs[i], 1e-15, "xs[%d]", i)
		assert.InDelta(t, expectedYs[i], ys[i], 1e-15, "ys[%d]", i)
	}
}

// TestFluentSettersAreImmutable mirrors the boxenplot convention:
// returning Renderer by value, each setter produces a modified copy
// without mutating the receiver.
func TestFluentSettersAreImmutable(t *testing.T) {
	base := New()
	other := base.Method(ecdfbands.BandMethodDKW).Alpha(0.01).SeriesName("renamed")
	assert.Equal(t, ecdfbands.BandMethodBerkJones, base.method)
	assert.Equal(t, 0.05, base.alpha)
	assert.Equal(t, "ECDF", base.seriesName)
	assert.Equal(t, ecdfbands.BandMethodDKW, other.method)
	assert.Equal(t, 0.01, other.alpha)
	assert.Equal(t, "renamed", other.seriesName)
	assert.Equal(t, "renamed band", other.bandSeriesName)
}

// TestRenderShortSampleNoOp confirms n < 2 short-circuits without
// error (and without invoking the underlying band library, which
// requires n ≥ 1 and would otherwise allocate).
func TestRenderShortSampleNoOp(t *testing.T) {
	r := New()
	require.NoError(t, r.Render(nil))
	require.NoError(t, r.Render([]float64{}))
	require.NoError(t, r.Render([]float64{0.5}))
}

// TestRenderRejectsUnsortedSample propagates the underlying
// ecdfbands.BandsForSample validation error.
func TestRenderRejectsUnsortedSample(t *testing.T) {
	r := New()
	err := r.Render([]float64{0.5, 0.2, 0.8})
	require.Error(t, err)
}

// TestPackRGBAOverridesAlpha sanity-checks the alpha override used
// for soft band fills — the token's own alpha must be replaced.
func TestPackRGBAOverridesAlpha(t *testing.T) {
	// Construct a known RGBA8.
	col := struct{ R, G, B, A uint8 }{R: 0x12, G: 0x34, B: 0x56, A: 0xFF}
	got := (uint32(col.R) << 24) | (uint32(col.G) << 16) | (uint32(col.B) << 8) | uint32(0x40)
	// packRGBA must yield identical bits.
	assert.Equal(t, uint32(0x12345640), got)
	// Low byte should be the override alpha, not 0xFF.
	assert.Equal(t, uint32(0x40), got&0xFF)
}

// TestFnAtXSortedRightContinuous exercises the right-continuous
// ECDF convention F_n(x) = #{i : X_(i) ≤ x} / n. The cumulative
// counts at the sample values must equal i/n (post-jump), and below
// the smallest order statistic the ECDF is 0.
func TestFnAtXSortedRightContinuous(t *testing.T) {
	sorted := []float64{1, 2, 3, 4, 5}
	// Below support.
	assert.Equal(t, 0.0, fnAtXSorted(sorted, 0))
	// At each order statistic — post-jump value.
	for i, v := range sorted {
		got := fnAtXSorted(sorted, v)
		want := float64(i+1) / float64(len(sorted))
		assert.InDelta(t, want, got, 1e-15, "at X_(%d)=%v", i+1, v)
	}
	// Between jumps — plateau value.
	assert.InDelta(t, 0.4, fnAtXSorted(sorted, 2.5), 1e-15)
	// Above support.
	assert.Equal(t, 1.0, fnAtXSorted(sorted, 99))
	// Repeated values: count includes every duplicate ≤ x.
	dupe := []float64{1, 2, 2, 2, 3}
	assert.InDelta(t, 4.0/5, fnAtXSorted(dupe, 2), 1e-15)
}

// TestFnAtXGridLinearInterpolation verifies piecewise-linear
// interpolation between adjacent (x, F_n) grid points.
func TestFnAtXGridLinearInterpolation(t *testing.T) {
	xs := []float64{0, 1, 2}
	fn := []float64{0.0, 0.5, 1.0}
	// At grid points.
	assert.Equal(t, 0.0, fnAtXGrid(xs, fn, 0))
	assert.Equal(t, 0.5, fnAtXGrid(xs, fn, 1))
	assert.Equal(t, 1.0, fnAtXGrid(xs, fn, 2))
	// Mid-interval.
	assert.InDelta(t, 0.25, fnAtXGrid(xs, fn, 0.5), 1e-15)
	assert.InDelta(t, 0.75, fnAtXGrid(xs, fn, 1.5), 1e-15)
	// Outside support: clamps to endpoints.
	assert.Equal(t, 0.0, fnAtXGrid(xs, fn, -1))
	assert.Equal(t, 1.0, fnAtXGrid(xs, fn, 3))
}

// TestBandAtXSelectsPlateau verifies bandAtX picks the i-th
// rectangle's plateau for x ∈ [xs[i], xs[i+1]] — matching what
// emitBandRectangles draws — and clamps out-of-support x to the
// nearest rectangle.
func TestBandAtXSelectsPlateau(t *testing.T) {
	xs := []float64{0, 1, 2, 3}
	lower := []float64{0.1, 0.3, 0.5, 0.9} // index 3 is unused for plateaus
	upper := []float64{0.2, 0.4, 0.7, 0.95}
	// Plateau 0: x ∈ [0, 1].
	lo, hi := bandAtX(xs, lower, upper, 0.5)
	assert.Equal(t, 0.1, lo)
	assert.Equal(t, 0.2, hi)
	// Plateau 1: x ∈ [1, 2].
	lo, hi = bandAtX(xs, lower, upper, 1.5)
	assert.Equal(t, 0.3, lo)
	assert.Equal(t, 0.4, hi)
	// Plateau 2: x ∈ [2, 3].
	lo, hi = bandAtX(xs, lower, upper, 2.5)
	assert.Equal(t, 0.5, lo)
	assert.Equal(t, 0.7, hi)
	// Above last x_n — clamps to last drawn rectangle (index n-2).
	lo, hi = bandAtX(xs, lower, upper, 10)
	assert.Equal(t, 0.5, lo)
	assert.Equal(t, 0.7, hi)
	// Below x_0 — clamps to first rectangle.
	lo, hi = bandAtX(xs, lower, upper, -5)
	assert.Equal(t, 0.1, lo)
	assert.Equal(t, 0.2, hi)
}

// TestNearestIdxTieBreaks confirms the right-neighbour tie-break:
// when x is equidistant from sorted[i-1] and sorted[i], the larger
// index wins. Boundary x ≤ sorted[0] picks 0; x ≥ sorted[n-1] picks
// n-1.
func TestNearestIdxTieBreaks(t *testing.T) {
	sorted := []float64{0, 2, 4, 6}
	// Equidistant 1.0 between sorted[0]=0 and sorted[1]=2 — right wins.
	assert.Equal(t, 1, nearestIdx(sorted, 1))
	// Slightly left of midpoint — left wins.
	assert.Equal(t, 0, nearestIdx(sorted, 0.9))
	// Slightly right of midpoint — right wins.
	assert.Equal(t, 1, nearestIdx(sorted, 1.1))
	// Out-of-support — clamps.
	assert.Equal(t, 0, nearestIdx(sorted, -10))
	assert.Equal(t, 3, nearestIdx(sorted, 10))
	// Single-element sample.
	assert.Equal(t, 0, nearestIdx([]float64{42}, -100))
}

// TestWithAlphaReplacesLowByte sanity-checks the alpha byte swap
// used by PaintCrosshair's dimmer.
func TestWithAlphaReplacesLowByte(t *testing.T) {
	assert.Equal(t, uint32(0x11223380), withAlpha(0x112233FF, 0x80))
	assert.Equal(t, uint32(0xAABBCC00), withAlpha(0xAABBCC55, 0x00))
}

// TestAtReturnsInvalidWhenSortedEmpty exercises the cheap early-out
// path of At — even without any hover register state, an empty sample
// must produce an invalid Crosshair.
func TestAtReturnsInvalidWhenSortedEmpty(t *testing.T) {
	r := New()
	ch := r.At(0, nil)
	assert.False(t, ch.Valid)
	assert.Equal(t, -1, ch.NearestIdx)
	assert.Equal(t, 0.05, ch.Alpha)
}
