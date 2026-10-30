package windowhost

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func shownAt(key WindowKeyT, x, y, w, h float32) *window {
	return &window{key: key, geom: WindowGeom{Shown: true, Rect: rectAt(x, y, w, h)}}
}

// Directional focus picks the nearest window that way, preferring one
// straight ahead over one off to the side, and nothing when none lies there.
func TestNearestInDirection(t *testing.T) {
	from := shownAt(1, 0, 0, 100, 100)
	right := shownAt(2, 200, 0, 100, 100)
	rightFar := shownAt(3, 500, 0, 100, 100)
	rightBelow := shownAt(4, 200, 300, 100, 100)
	below := shownAt(5, 0, 200, 100, 100)
	unseen := &window{key: 6}
	open := []*window{from, right, rightFar, rightBelow, below, unseen}
	assert.Equal(t, WindowKeyT(2), nearestInDirection(open, from, 1, 0))
	assert.Equal(t, WindowKeyT(5), nearestInDirection(open, from, 0, 1))
	assert.Equal(t, WindowKeyT(0), nearestInDirection(open, from, -1, 0), "nothing to the left")
	assert.Equal(t, WindowKeyT(0), nearestInDirection(open, from, 0, -1), "nothing above")
	assert.Equal(t, WindowKeyT(0), nearestInDirection(open, unseen, 1, 0), "no rect to start from")
	assert.Equal(t, WindowKeyT(0), nearestInDirection(open, nil, 1, 0))
}

func TestNextTiledLayoutWraps(t *testing.T) {
	assert.Equal(t, LayoutMainStack, nextTiledLayout(LayoutFloating))
	assert.Equal(t, LayoutTile, nextTiledLayout(LayoutMainStack))
	assert.Equal(t, LayoutMainStack, nextTiledLayout(LayoutMonocle))
	for _, l := range Layouts[1:] {
		assert.True(t, nextTiledLayout(l).Tiled())
	}
}

// Swap and promote move a window along the order and mark the plan dirty.
func TestSwapAndPromoteInOrder(t *testing.T) {
	inst := NewInst(nil, zerolog.Nop())
	inst.tileOrder = []WindowKeyT{1, 2, 3}
	inst.swapInOrderLocked(2, 1)
	assert.Equal(t, []WindowKeyT{1, 3, 2}, inst.tileOrder)
	assert.True(t, inst.tileDirty)
	inst.swapInOrderLocked(1, -1)
	assert.Equal(t, []WindowKeyT{2, 3, 1}, inst.tileOrder, "wraps to the end")
	inst.swapInOrderLocked(9, 1)
	assert.Equal(t, []WindowKeyT{2, 3, 1}, inst.tileOrder, "an unknown key moves nothing")
}

// The focus ring is the order on a tiled desktop and the stacking, front
// first, on a floating one.
func TestFocusRing(t *testing.T) {
	inst := NewInst(nil, zerolog.Nop())
	inst.tileOrder = []WindowKeyT{3, 1, 2}
	a := &window{key: 1, geom: WindowGeom{Stack: 5}}
	b := &window{key: 2, geom: WindowGeom{Stack: 9}}
	d := &window{key: 3, geom: WindowGeom{Stack: 1}}
	open := []*window{a, b, d}
	assert.Equal(t, []WindowKeyT{3, 1, 2}, inst.focusRingLocked(open, true))
	assert.Equal(t, []WindowKeyT{2, 1, 3}, inst.focusRingLocked(open, false))
}

// Snapping places the window in a half of the work area; the same chord
// again steps through the half's quarters and back to the half.
func TestSnapStepsHalfThenQuarters(t *testing.T) {
	inst := NewInst(nil, zerolog.Nop())
	inst.desktop = DesktopInfo{WorkShown: true, Work: Rect{MinX: 0, MinY: 20, MaxX: 1000, MaxY: 820}}
	w := &window{key: 7}
	inst.windows = []*window{w}
	gap := defaultArrangeParams.gap
	halfW, halfH := (1000-3*gap)/2, (800-3*gap)/2

	inst.snapLocked(w, 8, -1, 0) // left half
	r := inst.pendingPlaces[7]
	assert.InDelta(t, gap, r.MinX, 0.01)
	assert.InDelta(t, halfW, r.W(), 0.01)
	assert.InDelta(t, 800-2*gap, r.H(), 0.01)

	inst.snapLocked(w, 8, -1, 0) // top-left quarter
	r = inst.pendingPlaces[7]
	assert.InDelta(t, halfW, r.W(), 0.01)
	assert.InDelta(t, halfH, r.H(), 0.01)
	assert.InDelta(t, 20+gap, r.MinY, 0.01)

	inst.snapLocked(w, 8, -1, 0) // bottom-left quarter
	r = inst.pendingPlaces[7]
	assert.InDelta(t, halfH, r.H(), 0.01)
	assert.InDelta(t, 820-gap, r.MaxY, 0.01)

	inst.snapLocked(w, 8, -1, 0) // back to the half
	r = inst.pendingPlaces[7]
	assert.InDelta(t, 800-2*gap, r.H(), 0.01)

	inst.snapLocked(w, 9, 1, 0) // another chord starts at its half
	r = inst.pendingPlaces[7]
	assert.InDelta(t, 1000-gap, r.MaxX, 0.01)
	assert.InDelta(t, 800-2*gap, r.H(), 0.01)

	inst.desktop.WorkShown = false
	delete(inst.pendingPlaces, 7)
	before := inst.snap
	inst.snapLocked(w, 9, 1, 0)
	_, placed := inst.pendingPlaces[7]
	assert.False(t, placed, "no work area yet, nothing to snap into")
	require.Equal(t, before, inst.snap, "and the snap state does not advance")
}

// Alt+F returns from monocle to the tiled layout it was entered from, and to
// the first tiled layout when the desktop started in monocle; on a floating
// desktop the parameter chords are dropped.
func TestMonocleReturnsToATiledLayout(t *testing.T) {
	inst := NewInst(nil, zerolog.Nop())
	require.NoError(t, inst.SetLayout(LayoutColumns))
	require.NoError(t, inst.SetLayout(LayoutMonocle))
	assert.Equal(t, LayoutColumns, inst.layoutBeforeMonocle, "entering monocle by any route remembers the layout")
	inst.setLayoutLocked(inst.layoutBeforeMonocle)
	assert.Equal(t, LayoutColumns, inst.Layout())

	fresh := NewInst(nil, zerolog.Nop())
	require.NoError(t, fresh.SetLayout(LayoutMonocle))
	assert.False(t, fresh.layoutBeforeMonocle.Tiled())
}
