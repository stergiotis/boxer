package ecdfdigest

import (
	"math/rand"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/ecdf"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildDigestGridShape exercises the structural contract: gridN
// vertices, monotone x, fn ∈ [0, 1] non-decreasing.
func TestBuildDigestGridShape(t *testing.T) {
	d := tdigest.NewTDigest()
	rnd := rand.New(rand.NewSource(11))
	for range 10_000 {
		d.Push(rnd.NormFloat64())
	}
	xs, fn := BuildDigestGrid(d, 50)
	require.Len(t, xs, 50)
	require.Len(t, fn, 50)
	assert.Equal(t, d.Min(), xs[0])
	assert.Equal(t, d.Max(), xs[len(xs)-1])
	for i := 1; i < len(xs); i++ {
		assert.LessOrEqual(t, xs[i-1], xs[i], "xs non-monotone at %d", i)
		assert.GreaterOrEqual(t, fn[i], 0.0)
		assert.LessOrEqual(t, fn[i], 1.0)
		assert.LessOrEqual(t, fn[i-1], fn[i]+1e-12, "fn non-monotone at %d", i)
	}
}

// TestBuildDigestGridRangeWindow exercises the explicit-window builder:
// the grid spans exactly [lo, hi] (not the digest support), stays
// monotone with fn ∈ [0,1], and the CDF values come from the whole digest
// (so fn at the window edges reflects the true fraction there, not 0/1).
func TestBuildDigestGridRangeWindow(t *testing.T) {
	d := tdigest.NewTDigest()
	for i := range 1000 {
		d.Push(float64(i)) // 0..999
	}
	lo, hi := 200.0, 800.0
	xs, fn := BuildDigestGridRange(d, 7, lo, hi)
	require.Len(t, xs, 7)
	require.Len(t, fn, 7)
	assert.Equal(t, lo, xs[0])
	assert.Equal(t, hi, xs[len(xs)-1])
	for i := 1; i < len(xs); i++ {
		assert.LessOrEqual(t, xs[i-1], xs[i], "xs non-monotone at %d", i)
		assert.LessOrEqual(t, fn[i-1], fn[i]+1e-12, "fn non-monotone at %d", i)
	}
	// The window starts well inside the support, so F at lo is clearly
	// above 0 (≈0.2 for a uniform 0..999) — the band/curve do not pretend
	// the visible window is the whole distribution.
	assert.Greater(t, fn[0], 0.05)
	assert.Less(t, fn[len(fn)-1], 1.0)
}

// TestBuildDigestGridDelegatesToRange pins that the full-range builder is
// exactly the explicit-window builder over [Min, Max], so the two paths
// can never drift.
func TestBuildDigestGridDelegatesToRange(t *testing.T) {
	d := tdigest.NewTDigest()
	rnd := rand.New(rand.NewSource(7))
	for range 5_000 {
		d.Push(rnd.NormFloat64())
	}
	xsA, fnA := BuildDigestGrid(d, 32)
	xsB, fnB := BuildDigestGridRange(d, 32, d.Min(), d.Max())
	assert.Equal(t, xsB, xsA)
	assert.Equal(t, fnB, fnA)
}

// TestBuildDigestGridClampsMinSize ensures gridN < 2 still produces
// a usable 2-point grid (xmin, xmax) rather than panicking.
func TestBuildDigestGridClampsMinSize(t *testing.T) {
	d := tdigest.NewTDigest()
	for i := range 100 {
		d.Push(float64(i))
	}
	xs, _ := BuildDigestGrid(d, 1)
	assert.Len(t, xs, 2)
	xs, _ = BuildDigestGrid(d, 0)
	assert.Len(t, xs, 2)
}

// TestPaintDigestRejectsEmpty rejects a fresh empty digest.
func TestPaintDigestRejectsEmpty(t *testing.T) {
	d := tdigest.NewTDigest()
	err := PaintDigest(implot.NewDetached(), ecdf.Input{Band: ecdf.BandExact}, d, 50).Err
	assert.Error(t, err)
}

// TestPaintDigestRejectsCollapsed rejects a digest where all pushed
// values are identical — the band is degenerate.
func TestPaintDigestRejectsCollapsed(t *testing.T) {
	d := tdigest.NewTDigest()
	for range 100 {
		d.Push(7.0)
	}
	err := PaintDigest(implot.NewDetached(), ecdf.Input{Band: ecdf.BandExact}, d, 50).Err
	assert.Error(t, err)
}

// TestPaintDigestRejectsNil catches the nil-digest mistake.
func TestPaintDigestRejectsNil(t *testing.T) {
	err := PaintDigest(implot.NewDetached(), ecdf.Input{Band: ecdf.BandExact}, nil, 50).Err
	assert.Error(t, err)
}

// TestPaintDigestRejectsTinyGrid rejects gridN < 2 explicitly (the
// helper's grid path would short-circuit silently; PaintDigest's
// contract is more strict — gridN < 2 is a caller bug).
func TestPaintDigestRejectsTinyGrid(t *testing.T) {
	d := tdigest.NewTDigest()
	for i := range 100 {
		d.Push(float64(i))
	}
	err := PaintDigest(implot.NewDetached(), ecdf.Input{Band: ecdf.BandExact}, d, 1).Err
	assert.Error(t, err)
}

// TestPaintDigestPaintsAGrid paints a populated digest's preview band into a
// detached plot (ADR-0267 W19: one frame without a host).
func TestPaintDigestPaintsAGrid(t *testing.T) {
	d := tdigest.NewTDigest()
	for i := range 200 {
		d.Push(float64(i))
	}
	res := PaintDigest(implot.NewDetached(), ecdf.Input{Band: ecdf.BandPreview}, d, 32)
	assert.NoError(t, res.Err)
}
