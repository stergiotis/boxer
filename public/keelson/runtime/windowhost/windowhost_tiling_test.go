package windowhost

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tileFixture(n int) (items []tileItem) {
	items = make([]tileItem, n)
	for i := range items {
		items[i] = tileItem{key: WindowKeyT(i + 1)}
	}
	return
}

var tiledLayouts = []LayoutE{LayoutMainStack, LayoutTile, LayoutColumns, LayoutRows}

// Every tiled layout but monocle places every window inside the work area
// without overlap when nothing asks for a minimum, for every window count
// up to a crowded desktop and for more than one main window.
func TestTileLayoutsPartitionTheWorkArea(t *testing.T) {
	for _, l := range tiledLayouts {
		for _, mains := range []int{1, 2, 3} {
			for n := 1; n <= 13; n++ {
				p := LayoutParams{MainRatio: 0.6, MainCount: mains}
				out := tileRects(l, arrangeWork, tileFixture(n), p, defaultArrangeParams)
				require.Len(t, out, n)
				for i := range out {
					assert.True(t, inside(out[i], arrangeWork), "%s n=%d mains=%d item %d inside: %+v", l, n, mains, i, out[i])
					assert.Positive(t, out[i].W(), "%s n=%d item %d", l, n, i)
					assert.Positive(t, out[i].H(), "%s n=%d item %d", l, n, i)
					for j := i + 1; j < len(out); j++ {
						assert.False(t, overlap(out[i], out[j]), "%s n=%d mains=%d items %d,%d overlap", l, n, mains, i, j)
					}
				}
			}
		}
	}
}

// Main and stack: the main column takes the ratio of the width, the others
// share the rest in one column, full height between them.
func TestTileMainStackHonoursRatioAndCount(t *testing.T) {
	p := LayoutParams{MainRatio: 0.6, MainCount: 1}
	out := tileRects(LayoutMainStack, arrangeWork, tileFixture(4), p, defaultArrangeParams)
	gap := defaultArrangeParams.gap
	inner := arrangeWork.W() - 3*gap
	assert.InDelta(t, arrangeWork.MinX+gap, out[0].MinX, 0.01)
	assert.InDelta(t, inner*0.6, out[0].W(), 0.01)
	assert.InDelta(t, arrangeWork.H()-2*gap, out[0].H(), 0.01)
	for i := 1; i < 4; i++ {
		assert.InDelta(t, out[0].MaxX+gap, out[i].MinX, 0.01, "stack item %d starts after the main column", i)
		assert.InDelta(t, inner*0.4, out[i].W(), 0.01)
		if i > 1 {
			assert.InDelta(t, out[i-1].H(), out[i].H(), 0.01, "stack rows are even")
			assert.InDelta(t, out[i-1].MaxY+gap, out[i].MinY, 0.01)
		}
	}

	// Two mains share the main column top to bottom.
	p.MainCount = 2
	out = tileRects(LayoutMainStack, arrangeWork, tileFixture(4), p, defaultArrangeParams)
	assert.InDelta(t, out[0].MinX, out[1].MinX, 0.01)
	assert.InDelta(t, out[0].MaxY+gap, out[1].MinY, 0.01)
	assert.Greater(t, out[2].MinX, out[1].MaxX)

	// More mains than windows: one column of rows.
	p.MainCount = 9
	out = tileRects(LayoutMainStack, arrangeWork, tileFixture(3), p, defaultArrangeParams)
	rows := tileRects(LayoutRows, arrangeWork, tileFixture(3), p, defaultArrangeParams)
	assert.Equal(t, rows, out)
}

// The ratio yields before anything overlaps: a stack whose minimum does not
// fit beside the main column at the ratio shrinks the main column towards
// its own minimum first.
func TestTileMainStackRatioYieldsToTheStacksMinimum(t *testing.T) {
	items := tileFixture(3)
	items[1].minW = 700
	p := LayoutParams{MainRatio: 0.6, MainCount: 1}
	out := tileRects(LayoutMainStack, arrangeWork, items, p, defaultArrangeParams)
	gap := defaultArrangeParams.gap
	inner := arrangeWork.W() - 3*gap
	assert.InDelta(t, 700, out[1].W(), 0.01, "the stack keeps its minimum")
	assert.InDelta(t, inner-700, out[0].W(), 0.01, "the main gives up the difference")
	assert.False(t, overlap(out[0], out[1]))
	assert.False(t, overlap(out[0], out[2]))
}

// When the columns' minimums do not fit side by side, each keeps its
// minimum and the stack column overlaps the main one, still inside the
// work area (ADR-0346 §SD2).
func TestTileMainStackOverlapsWhenMinimumsDoNotFit(t *testing.T) {
	items := tileFixture(3)
	items[0].minW = 900
	items[1].minW = 500
	p := LayoutParams{MainRatio: 0.6, MainCount: 1}
	out := tileRects(LayoutMainStack, arrangeWork, items, p, defaultArrangeParams)
	assert.InDelta(t, 900, out[0].W(), 0.01, "the main keeps its minimum")
	assert.InDelta(t, 500, out[1].W(), 0.01, "the stack keeps its minimum")
	assert.True(t, overlap(out[0], out[1]), "the columns overlap")
	for i, r := range out {
		assert.True(t, inside(r, arrangeWork), "item %d inside: %+v", i, r)
	}
}

// Monocle gives every window the whole work area.
func TestTileMonocleGivesEveryWindowTheWorkArea(t *testing.T) {
	out := tileRects(LayoutMonocle, arrangeWork, tileFixture(3), DefaultLayoutParams, defaultArrangeParams)
	for _, r := range out {
		assert.Equal(t, out[0], r)
		assert.True(t, inside(r, arrangeWork))
		assert.Greater(t, r.W(), arrangeWork.W()*0.98)
	}
}

// The front of the order is the main: under Tile it lands top-left.
func TestTileOrderPutsTheMainFirst(t *testing.T) {
	out := tileRects(LayoutTile, arrangeWork, tileFixture(4), DefaultLayoutParams, defaultArrangeParams)
	for i := 1; i < len(out); i++ {
		assert.True(t, out[0].MinY <= out[i].MinY && (out[0].MinY < out[i].MinY || out[0].MinX < out[i].MinX),
			"item %d before the main: %+v vs %+v", i, out[i], out[0])
	}
}

// Nothing is placed on a floating desktop or into an empty work area.
func TestTileRectsPlaceNothingWithoutALayoutOrAWorkArea(t *testing.T) {
	out := tileRects(LayoutFloating, arrangeWork, tileFixture(2), DefaultLayoutParams, defaultArrangeParams)
	for _, r := range out {
		assert.False(t, r.valid())
	}
	out = tileRects(LayoutTile, Rect{}, tileFixture(2), DefaultLayoutParams, defaultArrangeParams)
	for _, r := range out {
		assert.False(t, r.valid())
	}
}

func TestParseLayoutRoundTrips(t *testing.T) {
	for _, l := range Layouts {
		got, ok := ParseLayout(l.Ident())
		assert.True(t, ok, l.String())
		assert.Equal(t, l, got)
		assert.NotEmpty(t, l.String())
	}
	_, ok := ParseLayout("dwindle")
	assert.False(t, ok)
	assert.Equal(t, LayoutFloating, Layouts[0])
	assert.False(t, LayoutFloating.Tiled())
	assert.False(t, LayoutE(200).Tiled())
}

// The host's layout API: unknown layouts are refused, parameters are
// clamped, and a change marks the plan dirty for the next Frame.
func TestHostLayoutAPI(t *testing.T) {
	inst := NewInst(nil, zerolog.Nop())
	assert.Equal(t, LayoutFloating, inst.Layout())
	assert.Equal(t, DefaultLayoutParams, inst.LayoutParams())
	assert.Error(t, inst.SetLayout(LayoutE(99)))
	require.NoError(t, inst.SetLayout(LayoutMainStack))
	assert.Equal(t, LayoutMainStack, inst.Layout())
	assert.True(t, inst.tileDirty)
	inst.tileDirty = false
	require.NoError(t, inst.SetLayout(LayoutMainStack))
	assert.False(t, inst.tileDirty, "setting the same layout is not a change")

	inst.SetLayoutParams(LayoutParams{MainRatio: 0.95, MainCount: 0})
	assert.Equal(t, LayoutParams{MainRatio: MainRatioMax, MainCount: 1}, inst.LayoutParams())
	assert.True(t, inst.tileDirty)

	assert.Error(t, inst.FloatWindow(7, true), "no such window")
	assert.Error(t, inst.ArrangeWindows(ArrangeTile, nil), "arrangements are refused on a tiled desktop")
}

// The order follows the open windows: a new window joins in front, a closed
// one leaves, unfloating puts a window back in front.
func TestTileOrderFollowsTheWindows(t *testing.T) {
	inst := NewInst(nil, zerolog.Nop())
	require.NoError(t, inst.SetLayout(LayoutTile))
	w2, w5 := &window{key: 2}, &window{key: 5}
	inst.windows = []*window{w2, w5}
	in := inst.takeTileInputs(inst.windows)
	assert.Equal(t, []WindowKeyT{5, 2}, in.order)
	assert.True(t, in.dirty)
	in = inst.takeTileInputs(inst.windows)
	assert.False(t, in.dirty, "nothing changed")

	w9 := &window{key: 9}
	inst.windows = append(inst.windows, w9)
	in = inst.takeTileInputs(inst.windows)
	assert.Equal(t, []WindowKeyT{9, 5, 2}, in.order)
	assert.True(t, in.dirty)

	require.NoError(t, inst.FloatWindow(5, true))
	in = inst.takeTileInputs(inst.windows)
	assert.True(t, in.floating[5])
	assert.True(t, inst.WindowFloating(5))
	require.NoError(t, inst.FloatWindow(5, false))
	in = inst.takeTileInputs(inst.windows)
	assert.Equal(t, []WindowKeyT{5, 9, 2}, in.order, "unfloating puts the window in front")
	assert.False(t, in.floating[5])

	// Place on a tiled desktop floats the window (ADR-0346 §SD6).
	require.NoError(t, inst.Place(2, rectAt(0, 0, 100, 100)))
	assert.True(t, inst.WindowFloating(2))

	w9.closeReq = true
	in = inst.takeTileInputs(inst.windows)
	assert.Equal(t, []WindowKeyT{5, 2}, in.order)
	assert.True(t, in.dirty)
}

// IMZERO2_DESKTOP_LAYOUT's allowed set is the layouts' idents (ADR-0346
// §SD7); the env registry cannot import the host, so the two are kept equal
// here.
func TestDesktopLayoutEnvVarNamesTheLayouts(t *testing.T) {
	assert.Equal(t, LayoutIdents(), imzero2env.DesktopLayout.Allowed())
}
