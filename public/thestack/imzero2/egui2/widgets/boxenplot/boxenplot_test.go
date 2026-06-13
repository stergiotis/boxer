package boxenplot

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/stats/letterval"
	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeBoxWidth(t *testing.T) {
	const base = 0.6
	const shrink = 0.85
	require.InDelta(t, base, computeBoxWidth(base, shrink, 2), 1e-12)
	require.InDelta(t, base*shrink, computeBoxWidth(base, shrink, 3), 1e-12)
	require.InDelta(t, base*shrink*shrink, computeBoxWidth(base, shrink, 4), 1e-12)
	// shrink=1 → all boxes same width (Hofmann's constant-width variant).
	require.InDelta(t, base, computeBoxWidth(base, 1.0, 5), 1e-12)
	// Depth 1 (median) is not a box; computeBoxWidth returns base as a
	// sane fallback rather than panicking on uint8 underflow.
	require.InDelta(t, base, computeBoxWidth(base, shrink, 1), 1e-12)
}

func TestPaletteT(t *testing.T) {
	// Single rendered depth (maxDepth==2) collapses to tStart.
	require.InDelta(t, float32(0.2), paletteT(2, 2, 0.2, 0.85), 1e-6)
	// Innermost rendered = tStart, outermost = tEnd, midpoint splits.
	require.InDelta(t, float32(0.2), paletteT(2, 6, 0.2, 0.85), 1e-6)
	require.InDelta(t, float32(0.85), paletteT(6, 6, 0.2, 0.85), 1e-6)
	require.InDelta(t, float32(0.525), paletteT(4, 6, 0.2, 0.85), 1e-6)
}

func TestResolveOutlierMode(t *testing.T) {
	// Pass-through for non-Auto.
	require.Equal(t, OutlierModeNone, resolveOutlierMode(OutlierModeNone, 0, 20))
	require.Equal(t, OutlierModePoints, resolveOutlierMode(OutlierModePoints, 999, 20))
	require.Equal(t, OutlierModeCount, resolveOutlierMode(OutlierModeCount, 0, 20))
	// Auto picks Points when budget is small, Count when large.
	require.Equal(t, OutlierModePoints, resolveOutlierMode(OutlierModeAuto, 5, 20))
	require.Equal(t, OutlierModePoints, resolveOutlierMode(OutlierModeAuto, 19, 20))
	require.Equal(t, OutlierModeCount, resolveOutlierMode(OutlierModeAuto, 20, 20))
	require.Equal(t, OutlierModeCount, resolveOutlierMode(OutlierModeAuto, 1000, 20))
}

func TestFillForDepthIsAlphaApplied(t *testing.T) {
	r := New("x").FillAlpha(0x80)
	packed := r.fillForDepth(2, 6)
	// AAlpha is the low byte of 0xRRGGBBAA.
	assert.Equal(t, uint32(0x80), packed&0xFF)
}

func TestFillForDepthRampOrdering(t *testing.T) {
	// With batlow, t=0 → dark (low luminance), t=1 → light. The
	// luminance of the fill should monotonically *increase* with
	// depth under the default (shallow=dark, deep=light) mapping.
	r := New("x").PaletteRange(0.0, 1.0)
	const maxDepth = uint8(8)
	var prevL float64
	for d := uint8(2); d <= maxDepth; d++ {
		packed := r.fillForDepth(d, maxDepth)
		rByte := float64((packed >> 24) & 0xFF)
		gByte := float64((packed >> 16) & 0xFF)
		bByte := float64((packed >> 8) & 0xFF)
		L := 0.2126*rByte + 0.7152*gByte + 0.0722*bByte
		if d > 2 {
			assert.GreaterOrEqual(t, L, prevL-1.0,
				"luminance non-monotone at depth %d (L=%v, prevL=%v)", d, L, prevL)
		}
		prevL = L
	}
}

// Render-emission tests (.Send) require a live FFFI2 runtime, which
// only exists in the actual app. Visual verification of Render lives
// in the demo app (apps/boxenplotdemo); unit tests here cover pure-
// function logic + the early-return paths that never call Send.

func TestRenderEmptyLevelsIsNoOp(t *testing.T) {
	r := New("x")
	require.NotPanics(t, func() {
		r.Render(0.0, nil, nil, -1)
	})
	require.NotPanics(t, func() {
		r.Render(0.0, []letterval.LVLevel{}, nil, -1)
	})
}

func TestMedianOnlyLevelsCount(t *testing.T) {
	// n < 16 → letterval.RecommendedDepth returns 1 → median only.
	d := tdigest.NewTDigest()
	for i := range 5 {
		d.Push(float64(i))
	}
	levels := letterval.RecommendedLevels(d)
	require.Len(t, levels, 1)
}

func TestBoxWidthClampsShrink(t *testing.T) {
	r := New("x").BoxWidth(1.0, -0.5)
	require.Greater(t, r.widthShrink, 0.0)
	r2 := New("x").BoxWidth(1.0, 2.5)
	require.InDelta(t, 1.0, r2.widthShrink, 1e-12)
}

func TestFluentSettersAreImmutable(t *testing.T) {
	base := New("x")
	other := base.SeriesName("renamed").OutlierMode(OutlierModeCount).FillAlpha(0x40).SnapWindow(0.25)
	// Base unchanged.
	assert.Equal(t, "boxen", base.seriesName)
	assert.Equal(t, OutlierModeAuto, base.outlierMode)
	assert.Equal(t, uint8(0xC0), base.fillAlpha)
	assert.Equal(t, 0.5, base.snapWindow)
	// Other modified.
	assert.Equal(t, "renamed", other.seriesName)
	assert.Equal(t, OutlierModeCount, other.outlierMode)
	assert.Equal(t, uint8(0x40), other.fillAlpha)
	assert.Equal(t, 0.25, other.snapWindow)
}

// TestSnapWindowClampsNonPositive: a zero or negative snap window
// would silently match every hover X (At()'s |HoverX - argument|
// check would always succeed); the setter clamps to a tiny positive
// epsilon to keep "no hover" behaviour the no-config default for
// callers who forgot to set it.
func TestSnapWindowClampsNonPositive(t *testing.T) {
	r0 := New("x").SnapWindow(0)
	rNeg := New("x").SnapWindow(-1.5)
	assert.Greater(t, r0.snapWindow, 0.0)
	assert.Greater(t, rNeg.snapWindow, 0.0)
}

// TestFindContainingLevelInnermost verifies the innermost-wins rule:
// LV depths are nested with monotonically widening spreads (depth 2 =
// IQR, depth 3 = wider, …), so a y inside the IQR is also inside
// every deeper level; findContainingLevel must return the SMALLEST
// containing depth — the visual ring the reader's eye picks out as
// "the cursor's box".
func TestFindContainingLevelInnermost(t *testing.T) {
	// Three nested rings: depth 2 ⊂ depth 3 ⊂ depth 4.
	levels := []letterval.LVLevel{
		{Depth: 1, LowerValue: 0, UpperValue: 0},
		{Depth: 2, LowerQ: 0.25, UpperQ: 0.75, LowerValue: -1, UpperValue: +1, TailCount: 25},
		{Depth: 3, LowerQ: 0.125, UpperQ: 0.875, LowerValue: -2, UpperValue: +2, TailCount: 12},
		{Depth: 4, LowerQ: 0.0625, UpperQ: 0.9375, LowerValue: -3, UpperValue: +3, TailCount: 6},
	}
	// Centre — innermost wins (depth 2), not the deeper rings that
	// also contain 0.
	lv := findContainingLevel(levels, 0)
	require.NotNil(t, lv)
	assert.Equal(t, uint8(2), lv.Depth)
	assert.Equal(t, -1.0, lv.LowerValue)
	assert.Equal(t, +1.0, lv.UpperValue)
	// Ring between depth 2 and depth 3.
	lv = findContainingLevel(levels, 1.5)
	require.NotNil(t, lv)
	assert.Equal(t, uint8(3), lv.Depth)
	assert.Equal(t, -2.0, lv.LowerValue)
	assert.Equal(t, +2.0, lv.UpperValue)
	// Outermost ring.
	lv = findContainingLevel(levels, -2.5)
	require.NotNil(t, lv)
	assert.Equal(t, uint8(4), lv.Depth)
	// Outside every box → nil.
	assert.Nil(t, findContainingLevel(levels, 5))
	// NaN y → nil (defensive; cached hover may carry NaN on first frames).
	assert.Nil(t, findContainingLevel(levels, math.NaN()))
}

// TestFindContainingLevelIgnoresMedianSentinel: depth-1 entries are
// the median sentinel (LowerValue == UpperValue, a single point);
// even when y exactly matches the median, findContainingLevel must
// fall through to depth 2, since a "depth 1" box would be a degenerate
// zero-width region — not the affordance the reader is hovering over.
func TestFindContainingLevelIgnoresMedianSentinel(t *testing.T) {
	levels := []letterval.LVLevel{
		{Depth: 1, LowerValue: 0.5, UpperValue: 0.5},
		{Depth: 2, LowerQ: 0.25, UpperQ: 0.75, LowerValue: 0.25, UpperValue: 0.75, TailCount: 50},
	}
	lv := findContainingLevel(levels, 0.5)
	require.NotNil(t, lv)
	assert.Equal(t, uint8(2), lv.Depth)
	assert.Equal(t, 0.25, lv.LowerValue)
	assert.Equal(t, 0.75, lv.UpperValue)
}

// TestDeepestLevel: end-of-slice extraction for the deepest entry the
// "outside-boxes" status-line branch reports against.
func TestDeepestLevel(t *testing.T) {
	assert.Nil(t, deepestLevel(nil))
	assert.Nil(t, deepestLevel([]letterval.LVLevel{
		{Depth: 1, TailCount: 100},
	}))
	levels := []letterval.LVLevel{
		{Depth: 1, TailCount: 100},
		{Depth: 2, TailCount: 50, LowerValue: -1, UpperValue: 1},
		{Depth: 3, TailCount: 25, LowerValue: -2, UpperValue: 2},
		{Depth: 4, TailCount: 12, LowerValue: -3, UpperValue: 3},
	}
	deepest := deepestLevel(levels)
	require.NotNil(t, deepest)
	assert.Equal(t, uint8(4), deepest.Depth)
	assert.Equal(t, int64(12), deepest.TailCount)
	assert.Equal(t, -3.0, deepest.LowerValue)
}

// TestRecoverN verifies sample-size recovery from the shallowest
// non-median LV's TailCount. The depth-2 inversion is exact when n is
// divisible by 4 and otherwise off by < 4; the fallback path is the
// depth-1 sentinel (TailCount ≈ n/2).
func TestRecoverN(t *testing.T) {
	// n=10_000, exact divisibility by 4 → recovered to the unit.
	levels := []letterval.LVLevel{
		{Depth: 1, TailCount: 5000},
		{Depth: 2, TailCount: 2500},
		{Depth: 3, TailCount: 1250},
	}
	assert.Equal(t, int64(10_000), recoverN(levels))
	// n=10_001 → depth 2 floor truncates to 2500 → recovered to 10_000
	// (off by 1, within < 2^2 = 4 of the true value).
	levels = []letterval.LVLevel{
		{Depth: 1, TailCount: 5000},
		{Depth: 2, TailCount: 2500},
	}
	assert.Equal(t, int64(10_000), recoverN(levels))
	// Median-only fallback (small-n boxenplot case): doubles depth-1's
	// TailCount as the best available approximation.
	levels = []letterval.LVLevel{
		{Depth: 1, TailCount: 3},
	}
	assert.Equal(t, int64(6), recoverN(levels))
	// Empty.
	assert.Equal(t, int64(0), recoverN(nil))
}

// TestWithAlphaReplacesLowByte mirrors the matching ecdf test;
// PaintCrosshair relies on alpha-substitution to dim the annotation
// colour without otherwise touching the RGB channels.
func TestWithAlphaReplacesLowByte(t *testing.T) {
	assert.Equal(t, uint32(0x11223380), withAlpha(0x112233FF, 0x80))
	assert.Equal(t, uint32(0xAABBCC00), withAlpha(0xAABBCC55, 0x00))
}

func TestMonotonicNestingInRenderedSpread(t *testing.T) {
	// The widget reads LV levels; the nesting invariant comes from the
	// letterval package (tested there). This is a lightweight sanity
	// check that letterval + tdigest still produces nested ranges in
	// the pipeline boxenplot consumes.
	rnd := rand.New(rand.NewSource(31))
	d := tdigest.NewTDigest()
	for range 50_000 {
		d.Push(rnd.NormFloat64())
	}
	levels := letterval.Levels(d, 6)
	require.Len(t, levels, 6)
	for i := 1; i < len(levels); i++ {
		assert.LessOrEqual(t, levels[i].LowerValue, levels[i-1].LowerValue)
		assert.GreaterOrEqual(t, levels[i].UpperValue, levels[i-1].UpperValue)
	}
	// Median is consistent across depths if levels are well-formed —
	// depth 1 lower = upper = median.
	assert.False(t, math.IsNaN(levels[0].LowerValue))
	assert.InDelta(t, levels[0].LowerValue, levels[0].UpperValue, 1e-12)
}
