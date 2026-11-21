package regions

import (
	"image"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

type visited struct {
	cells   []image.Point
	t0s     []float64
	t1s     []float64
	lengths []float64
}

func walk(a, b Point, w, h int) (v visited) {
	l := math.Hypot(b.X-a.X, b.Y-a.Y)
	traverse(a, b, w, h, func(col, row int, t0, t1 float64) bool {
		v.cells = append(v.cells, image.Pt(col, row))
		v.t0s, v.t1s = append(v.t0s, t0), append(v.t1s, t1)
		v.lengths = append(v.lengths, (t1-t0)*l)
		return true
	})
	return v
}

// clippedLength is the length of the part of a segment in the half-open
// image [0, w) × [0, h), which holds a part along x = 0 and not one along
// x = w, as the floor rule says.
func clippedLength(a, b Point, w, h int) float64 {
	t0, t1 := 0.0, 1.0
	for _, ax := range [2][3]float64{{a.X, b.X, float64(w)}, {a.Y, b.Y, float64(h)}} {
		v0, v1, n := ax[0], ax[1], ax[2]
		d := v1 - v0
		if d == 0 {
			if !(v0 >= 0 && v0 < n) {
				return 0
			}
			continue
		}
		lo, hi := (0-v0)/d, (n-v0)/d
		if lo > hi {
			lo, hi = hi, lo
		}
		t0, t1 = max(t0, lo), min(t1, hi)
	}
	if t1 <= t0 {
		return 0
	}
	return (t1 - t0) * math.Hypot(b.X-a.X, b.Y-a.Y)
}

// A line's pixels are each visited once, in order, each next to the last —
// across an edge or, through a corner, diagonally; the lengths add up to the
// length inside the image; a part of any length lies in the pixel holding
// its middle; and the line walked backwards visits the same pixels in the
// opposite order.
func TestTheTraversalInvariants(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b := drawPoint(t, -4, 16, "a"), drawPoint(t, -4, 16, "b")
		const w, h = 12, 10
		v := walk(a, b, w, h)
		seen := map[image.Point]bool{}
		var sum float64
		for i, c := range v.cells {
			require.False(t, seen[c], "pixel %v visited twice", c)
			seen[c] = true
			require.True(t, c.In(image.Rect(0, 0, w, h)))
			require.GreaterOrEqual(t, v.lengths[i], 0.0)
			sum += v.lengths[i]
			if i > 0 {
				d := c.Sub(v.cells[i-1])
				require.True(t, max(abs(d.X), abs(d.Y)) == 1, "pixel %v follows %v", c, v.cells[i-1])
				require.GreaterOrEqual(t, v.t0s[i], v.t1s[i-1])
			}
			if v.lengths[i] > 1e-6 {
				tm := (v.t0s[i] + v.t1s[i]) / 2
				mx, my := a.X+tm*(b.X-a.X), a.Y+tm*(b.Y-a.Y)
				require.Equal(t, image.Pt(int(math.Floor(mx)), int(math.Floor(my))), c, "middle of the part in %v", c)
			}
		}
		require.InDelta(t, clippedLength(a, b, w, h), sum, 1e-9)
		back := walk(b, a, w, h)
		require.Len(t, back.cells, len(v.cells))
		for i, c := range back.cells {
			require.Equal(t, v.cells[len(v.cells)-1-i], c)
		}
	})
}

func abs(v int) int { return max(v, -v) }

func TestTraversalTies(t *testing.T) {
	cells := func(a, b Point) []image.Point { return walk(a, b, 8, 8).cells }
	// Through pixel corners: the diagonal only.
	assert.Equal(t, []image.Point{{1, 1}, {2, 2}, {3, 3}}, cells(Point{1, 1}, Point{4, 4}))
	assert.Equal(t, []image.Point{{3, 3}, {2, 2}, {1, 1}}, cells(Point{4, 4}, Point{1, 1}))
	assert.Equal(t, []image.Point{{3, 0}, {2, 1}, {1, 2}}, cells(Point{4, 0}, Point{1, 3}))
	// Along a pixel edge: the pixel below, or to the right.
	assert.Equal(t, []image.Point{{1, 3}, {2, 3}, {3, 3}}, cells(Point{1, 3}, Point{4, 3}))
	assert.Equal(t, []image.Point{{3, 3}, {2, 3}, {1, 3}}, cells(Point{4, 3}, Point{1, 3}))
	assert.Equal(t, []image.Point{{2, 5}, {2, 4}}, cells(Point{2, 6}, Point{2, 4}))
	// Ending on a grid line: the pixel beyond it is not entered.
	assert.Equal(t, []image.Point{{0, 0}, {1, 0}}, cells(Point{0.5, 0.5}, Point{2, 0.5}))
	assert.Equal(t, []image.Point{{1, 0}, {0, 0}}, cells(Point{2, 0.5}, Point{0.5, 0.5}))
	// Zero length: the pixel holding the point.
	v := walk(Point{3, 4}, Point{3, 4}, 8, 8)
	assert.Equal(t, []image.Point{{3, 4}}, v.cells)
	assert.Equal(t, []float64{0}, v.lengths)
	assert.Empty(t, cells(Point{8, 4}, Point{8, 4}))
	// From far outside to far outside, through the image.
	v = walk(Point{-1e12, 2.5}, Point{1e12, 2.5}, 8, 8)
	assert.Len(t, v.cells, 8)
	var sum float64
	for _, l := range v.lengths {
		sum += l
	}
	assert.InDelta(t, 8, sum, 1e-3)
	// Outside throughout, and along the image's right edge, which is not in
	// it.
	assert.Empty(t, cells(Point{-3, -1}, Point{9, -0.5}))
	assert.Empty(t, cells(Point{8, 0}, Point{8, 8}))
	assert.Len(t, cells(Point{0, 0}, Point{0, 8}), 8, "the left edge is")
}
