package portolan

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// Projection rounds to whole pixels, so at low zoom a large outline arrives at
// the fill with most of its vertices on the same pixel, and clipping adds more
// along the clip edge. Measured over the vendored country outlines at world
// view, 108 of the 240 rings reaching the fill carried repeated vertices; the
// ear clipper has no defined triangulation for one, and the slivers it emits
// change with every sub-pixel shift of the view.
func TestDropRepeatsRemovesZeroLengthEdges(t *testing.T) {
	pts := []Point{{X: 1, Y: 1}, {X: 1, Y: 1}, {X: 2, Y: 2}, {X: 2, Y: 2}, {X: 2, Y: 2}, {X: 3, Y: 1}}
	require.Equal(t, []Point{{X: 1, Y: 1}, {X: 2, Y: 2}, {X: 3, Y: 1}}, dropRepeats(pts))

	// The closing vertex of a ring that ends on its own first point goes too,
	// however many times it repeats.
	ring := []Point{{X: 0, Y: 0}, {X: 4, Y: 0}, {X: 4, Y: 4}, {X: 0, Y: 0}, {X: 0, Y: 0}}
	require.Equal(t, []Point{{X: 0, Y: 0}, {X: 4, Y: 0}, {X: 4, Y: 4}}, dropRepeats(ring))

	// A clean ring is untouched, and the degenerate inputs do not panic.
	clean := []Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}}
	require.Equal(t, clean, dropRepeats(clean))
	require.Empty(t, dropRepeats(nil))
	require.Len(t, dropRepeats([]Point{{X: 2, Y: 2}}), 1)
	// Every vertex on one pixel — what a sub-pixel island becomes — collapses
	// to a single point, which the caller then declines to fill.
	require.Len(t, dropRepeats([]Point{{X: 7, Y: 7}, {X: 7, Y: 7}, {X: 7, Y: 7}}), 1)
}

func TestRingArea2IsTheShoelaceSum(t *testing.T) {
	square := []Point{{X: 0, Y: 0}, {X: 4, Y: 0}, {X: 4, Y: 4}, {X: 0, Y: 4}}
	require.Equal(t, 32.0, ringArea2(square), "twice the area, wound one way")
	require.Equal(t, -32.0, ringArea2([]Point{{X: 0, Y: 4}, {X: 4, Y: 4}, {X: 4, Y: 0}, {X: 0, Y: 0}}), "and the other")

	// A sliver: three points on a line have no area, which is the case the
	// fill routes away from the ear clipper.
	require.Equal(t, 0.0, ringArea2([]Point{{X: 0, Y: 0}, {X: 5, Y: 0}, {X: 9, Y: 0}}))
	require.Less(t, absF(ringArea2([]Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}})), 2*minConcaveAreaPx,
		"a one-pixel triangle is below the concave threshold")
	require.Greater(t, absF(ringArea2(square)), 2*minConcaveAreaPx)
}

// Sutherland–Hodgman answers a concave ring with points that were never in
// it: where the ring leaves the window and comes back, the output runs along
// the window's edge. Leaflet clips the same way and never sees them, because
// the canvas nonzero fill rule makes a zero-area excursion invisible; an ear
// clipper turns each into a triangle. This is that, as a test, so the reason
// the concave path declines to clip is written down as behaviour.
func TestSutherlandHodgmanInventsEdgesAlongTheWindowForAConcaveRing(t *testing.T) {
	// A comb straddling the window's bottom edge: three prongs hang below it.
	ring := []Point{
		{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 200}, {X: 20, Y: 200},
		{X: 20, Y: 0}, {X: 300, Y: 0}, {X: 300, Y: 200}, {X: 310, Y: 200},
		{X: 310, Y: 0}, {X: 320, Y: 0}, {X: 320, Y: 300}, {X: 0, Y: 300},
	}
	win := BoundsOf(Point{X: -50, Y: -50}, Point{X: 400, Y: 150})
	out := ClipPolygon(ring, win, true)

	in := make(map[Point]bool, len(ring))
	for _, p := range ring {
		in[p] = true
	}
	invented, widest := 0, 0.0
	for i, p := range out {
		if !in[p] {
			invented++
		}
		q := out[(i+1)%len(out)]
		if p.Y == win.Max.Y && q.Y == win.Max.Y {
			widest = math.Max(widest, math.Abs(q.X-p.X))
		}
	}
	require.Positive(t, invented, "the clip introduces vertices of its own")
	require.Equal(t, win.Max.X-win.Min.X-130, widest,
		"including an edge that runs the width of the window — which is what a fill drew across the map")

	// The concave fill path therefore leaves the ring alone, and the canvas
	// clip does the clipping.
	kept, ok := fillRing(ring, win, false)
	require.True(t, ok)
	require.Equal(t, ring, kept, "no vertex the caller did not give")

	// A convex ring is clipped as before: Sutherland–Hodgman is exact there.
	tri := []Point{{X: 0, Y: 0}, {X: 500, Y: 0}, {X: 0, Y: 500}}
	clipped, ok := fillRing(tri, win, true)
	require.True(t, ok)
	require.NotEqual(t, tri, clipped, "a convex ring is still trimmed to the window")
	for _, p := range clipped {
		require.LessOrEqual(t, p.X, win.Max.X+1e-9)
		require.LessOrEqual(t, p.Y, win.Max.Y+1e-9)
	}
}

// The cheap half of what the clip bought is kept: geometry with no business
// on screen is dropped before anything else is done with it.
func TestFillRingDropsARingWhollyOutsideTheWindow(t *testing.T) {
	win := BoundsOf(Point{X: 0, Y: 0}, Point{X: 100, Y: 100})
	away := []Point{{X: 500, Y: 500}, {X: 600, Y: 500}, {X: 600, Y: 600}}
	_, ok := fillRing(away, win, false)
	require.False(t, ok)

	// One that merely overlaps is kept whole.
	over := []Point{{X: 50, Y: 50}, {X: 5000, Y: 50}, {X: 5000, Y: 5000}, {X: 50, Y: 5000}}
	kept, ok := fillRing(over, win, false)
	require.True(t, ok)
	require.Equal(t, over, kept)
}
