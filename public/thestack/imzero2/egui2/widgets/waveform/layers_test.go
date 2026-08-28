package waveform

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestVisibleRegionsMatchesBruteForce(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 200).Draw(t, "n")
		regions := make([]Region, n)
		for i := range regions {
			from := rapid.Int64Range(0, 1_000_000).Draw(t, "from")
			regions[i] = Region{FromFrame: from, ToFrame: from + rapid.Int64Range(1, 50_000).Draw(t, "len")}
		}
		sort.Slice(regions, func(i, j int) bool { return regions[i].FromFrame < regions[j].FromFrame })
		maxLen := maxRegionLen(regions)
		a := rapid.Int64Range(0, 1_000_000).Draw(t, "a")
		b := a + rapid.Int64Range(1, 200_000).Draw(t, "span")
		lo, hi := visibleRegions(regions, maxLen, a, b)
		require.LessOrEqual(t, lo, hi)
		for i, r := range regions {
			intersects := r.ToFrame > a && r.FromFrame < b
			inRange := i >= lo && i < hi
			if intersects {
				require.True(t, inRange, "region %d [%d,%d) intersects [%d,%d) but is outside [%d,%d)", i, r.FromFrame, r.ToFrame, a, b, lo, hi)
			}
		}
	})
}

func TestVisibleMarkersAndPoints(t *testing.T) {
	markers := []Marker{{Frame: 10}, {Frame: 20}, {Frame: 30}, {Frame: 40}}
	lo, hi := visibleMarkers(markers, 15, 35)
	require.Equal(t, 1, lo)
	require.Equal(t, 3, hi)

	frames := []int64{0, 10, 20, 30, 40, 50}
	lo, hi = visiblePoints(frames, 15, 35)
	// 10 and 40 are the neighbours outside the span that keep the line continuous.
	require.Equal(t, 1, lo)
	require.Equal(t, 5, hi)
	lo, hi = visiblePoints(frames, -5, 1000)
	require.Equal(t, 0, lo)
	require.Equal(t, 6, hi)
	lo, hi = visiblePoints(nil, 0, 10)
	require.Equal(t, 0, lo)
	require.Equal(t, 0, hi)
}

func TestCurvesHeight(t *testing.T) {
	require.Equal(t, float32(0), curvesHeight(nil))
	require.Equal(t, defaultCurveHeight+curveGap, curvesHeight([]Curve{{}}))
	require.Equal(t, 2*(30+curveGap), curvesHeight([]Curve{{Height: 30}, {Height: 30}}))
}

func TestRegionHitTest(t *testing.T) {
	v := View{FromFrame: 0, FramesPerPx: 10}
	regions := []Region{{FromFrame: 1000, ToFrame: 2000, Editable: true}, {FromFrame: 5000, ToFrame: 6000}}
	// Left edge of region 0 is at x=100, right edge at x=200.
	idx, edge, ok := hitRegion(regions, maxRegionLen(regions), v, 1200, 103)
	require.True(t, ok)
	require.Equal(t, 0, idx)
	require.Equal(t, int8(-1), edge)
	idx, edge, ok = hitRegion(regions, maxRegionLen(regions), v, 1200, 196)
	require.True(t, ok)
	require.Equal(t, 0, idx)
	require.Equal(t, int8(1), edge)
	idx, edge, ok = hitRegion(regions, maxRegionLen(regions), v, 1200, 150)
	require.True(t, ok)
	require.Equal(t, 0, idx)
	require.Equal(t, int8(0), edge)
	// A non-editable region still hits (for clicks) as a body.
	idx, edge, ok = hitRegion(regions, maxRegionLen(regions), v, 1200, 550)
	require.True(t, ok)
	require.Equal(t, 1, idx)
	require.Equal(t, int8(0), edge)
	_, _, ok = hitRegion(regions, maxRegionLen(regions), v, 1200, 300)
	require.False(t, ok)
}
