package windowhost

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var arrangeWork = Rect{MinX: 0, MinY: 30, MaxX: 1200, MaxY: 830}

func arrangeFixture(n int) (items []arrangeItem) {
	items = make([]arrangeItem, n)
	for i := range items {
		items[i] = arrangeItem{key: WindowKeyT(i + 1), rect: rectAt(float32(10*i), 40, 300, 200), z: i}
	}
	return
}

func inside(r, work Rect) bool {
	return r.MinX >= work.MinX && r.MinY >= work.MinY && r.MaxX <= work.MaxX && r.MaxY <= work.MaxY
}

func overlap(a, b Rect) bool {
	return a.MinX < b.MaxX && b.MinX < a.MaxX && a.MinY < b.MaxY && b.MinY < a.MaxY
}

// Tiling commands place every window inside the work area without overlap,
// for every window count up to a crowded desktop.
func TestArrangeTilingCommandsPartitionTheWorkArea(t *testing.T) {
	for _, cmd := range []ArrangeE{ArrangeTile, ArrangeColumns, ArrangeRows} {
		for n := 1; n <= 13; n++ {
			out, ok := arrangeRects(cmd, arrangeWork, arrangeFixture(n), defaultArrangeParams)
			for i := range out {
				require.True(t, ok[i], "%s n=%d item %d placed", cmd, n, i)
				assert.True(t, inside(out[i], arrangeWork), "%s n=%d item %d inside: %+v", cmd, n, i, out[i])
				assert.Positive(t, out[i].W())
				assert.Positive(t, out[i].H())
				for j := i + 1; j < len(out); j++ {
					assert.False(t, overlap(out[i], out[j]), "%s n=%d items %d,%d overlap", cmd, n, i, j)
				}
			}
		}
	}
}

// The front window lands top-left when tiling: it is the one being worked in.
func TestArrangeTilePutsTheFrontWindowFirst(t *testing.T) {
	items := arrangeFixture(4)
	items[1].z = 99
	out, _ := arrangeRects(ArrangeTile, arrangeWork, items, defaultArrangeParams)
	for i := range out {
		if i == 1 {
			continue
		}
		assert.True(t, out[1].MinY <= out[i].MinY && (out[1].MinY < out[i].MinY || out[1].MinX < out[i].MinX))
	}
}

// Cascade keeps the stacking: the further front a window is, the further it
// steps down and right, so every title bar behind it stays visible.
func TestArrangeCascadeFollowsStacking(t *testing.T) {
	items := arrangeFixture(5)
	items[0].z, items[4].z = 10, -1
	out, ok := arrangeRects(ArrangeCascade, arrangeWork, items, defaultArrangeParams)
	byZ := []int{4, 1, 2, 3, 0}
	for k := 1; k < len(byZ); k++ {
		require.True(t, ok[byZ[k]])
		assert.Greater(t, out[byZ[k]].MinY, out[byZ[k-1]].MinY)
	}
	for i := range out {
		assert.True(t, inside(out[i], arrangeWork))
	}
}

// Cascade wraps instead of walking off the work area.
func TestArrangeCascadeWrapsOnACrowdedDesktop(t *testing.T) {
	out, _ := arrangeRects(ArrangeCascade, arrangeWork, arrangeFixture(40), defaultArrangeParams)
	for i := range out {
		assert.True(t, inside(out[i], arrangeWork), "item %d: %+v", i, out[i])
	}
}

// Gather moves only what sticks out, and keeps a window's size unless it is
// larger than the work area.
func TestArrangeGatherMovesOnlyWhatSticksOut(t *testing.T) {
	items := []arrangeItem{
		{key: 1, rect: rectAt(100, 100, 300, 200)},
		{key: 2, rect: rectAt(1100, 700, 300, 200)},
		{key: 3, rect: rectAt(-50, 0, 2000, 100)},
		{key: 4, rect: Rect{}},
	}
	out, ok := arrangeRects(ArrangeGather, arrangeWork, items, defaultArrangeParams)
	assert.Equal(t, []bool{false, true, true, false}, ok)
	assert.Equal(t, rectAt(900, 630, 300, 200), out[1])
	assert.Equal(t, rectAt(0, 30, 1200, 100), out[2])
}

func TestArrangeWithoutAWorkAreaPlacesNothing(t *testing.T) {
	_, ok := arrangeRects(ArrangeTile, Rect{}, arrangeFixture(3), defaultArrangeParams)
	assert.Equal(t, []bool{false, false, false}, ok)
}

// fitSpans gives every span at least its minimum and shares the rest as
// equally as the minimums allow.
func TestFitSpansHonoursMinimumsAndSharesTheRest(t *testing.T) {
	offs, sizes := fitSpans(1000, 0, []float32{0, 600, 0})
	assert.InDelta(t, 600, sizes[1], 1e-3)
	assert.InDelta(t, 200, sizes[0], 1e-3)
	assert.InDelta(t, 200, sizes[2], 1e-3)
	assert.InDelta(t, 1000, offs[2]+sizes[2], 1e-3)
}

// Minimums that do not fit keep their size and overlap evenly, ending
// inside the line; a minimum longer than the line is cut to it.
func TestFitSpansOverlapsEvenlyWhenMinimumsDoNotFit(t *testing.T) {
	offs, sizes := fitSpans(1000, 4, []float32{500, 500, 500})
	for i := range sizes {
		assert.InDelta(t, 500, sizes[i], 1e-3)
		assert.GreaterOrEqual(t, offs[i], float32(4))
		assert.LessOrEqual(t, offs[i]+sizes[i], float32(996)+1e-3)
	}
	assert.InDelta(t, offs[1]-offs[0], offs[2]-offs[1], 1e-3)
	_, sizes = fitSpans(1000, 4, []float32{5000})
	assert.InDelta(t, 992, sizes[0], 1e-3)
}

// A window with a wide minimum gets its width under side by side and tile;
// the arrangement still partitions the work area when the minimums fit.
func TestArrangeRespectsWindowMinimums(t *testing.T) {
	for _, cmd := range []ArrangeE{ArrangeTile, ArrangeColumns, ArrangeRows, ArrangeCascade} {
		items := arrangeFixture(3)
		items[1].minW, items[1].minH = 700, 300
		out, ok := arrangeRects(cmd, arrangeWork, items, defaultArrangeParams)
		for i := range out {
			require.True(t, ok[i])
			assert.True(t, inside(out[i], arrangeWork), "%s item %d: %+v", cmd, i, out[i])
		}
		assert.GreaterOrEqual(t, out[1].W(), float32(700), cmd.String())
		assert.GreaterOrEqual(t, out[1].H(), float32(300), cmd.String())
		if cmd == ArrangeCascade {
			continue
		}
		for i := range out {
			for j := i + 1; j < len(out); j++ {
				assert.False(t, overlap(out[i], out[j]), "%s items %d,%d overlap", cmd, i, j)
			}
		}
	}
}
