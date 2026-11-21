package regions

import (
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/science/imaging/stats"
)

// traverse calls visit for every pixel of a width × height image that the
// segment from a to b passes through, in order from a, each once, with the
// part of the segment in it as parameters t0 ≤ t1 along a + t·(b − a)
// (ADR-0399 §SD8). The segment is cut where it crosses a whole x or a whole
// y; each part lies in one pixel, and a part running along a pixel edge
// lies in the pixel whose index is the floor of the edge's coordinate — the
// pixel below a horizontal edge and right of a vertical one (ADR-0373
// §SD3). Where it crosses a whole x and a whole y at once — a pixel corner —
// the two cuts are one, decided exactly, and the two pixels beside the
// corner are not passed through. A segment of zero length is in the pixel
// containing its point, with t0 = t1 = 0. Parts outside the image are not
// visited.
func traverse(a, b Point, width, height int, visit func(col, row int, t0, t1 float64) bool) {
	if a == b {
		col, row := math.Floor(a.X), math.Floor(a.Y)
		if col >= 0 && row >= 0 && col < float64(width) && row < float64(height) {
			visit(int(col), int(row), 0, 0)
		}
		return
	}
	xs := newAxisWalk(a.X, b.X, width)
	ys := newAxisWalk(a.Y, b.Y, height)
	col, row := xs.cell, ys.cell
	dx, dy := b.X-a.X, b.Y-a.Y
	tPrev := 0.0
	emit := func(t float64) bool {
		t = max(t, tPrev)
		if col >= 0 && row >= 0 && col < width && row < height {
			if !visit(col, row, tPrev, t) {
				return false
			}
		}
		tPrev = t
		return true
	}
	for xs.more() || ys.more() {
		var takeX, takeY bool
		switch {
		case !ys.more():
			takeX = true
		case !xs.more():
			takeY = true
		default:
			// The sign of t_x − t_y, with t_x = (k − a.x)/dx and
			// t_y = (m − a.y)/dy, both positive.
			cmp := signDiffProd(float64(xs.k), a.X, b.Y, a.Y, float64(ys.k), a.Y, b.X, a.X) * xs.dir * ys.dir
			takeX, takeY = cmp <= 0, cmp >= 0
		}
		var t float64
		if takeX {
			t = (float64(xs.k) - a.X) / dx
		} else {
			t = (float64(ys.k) - a.Y) / dy
		}
		t = min(max(t, 0), 1)
		if !emit(t) {
			return
		}
		if takeX {
			col = xs.step()
		}
		if takeY {
			row = ys.step()
		}
	}
	emit(1)
}

// axisWalk is the whole coordinates a segment's coordinate crosses on its
// way from v0 to v1, those that move it into, across or out of [0, n]; the
// ones further out change nothing that is visited.
type axisWalk struct {
	dir  int
	cell int // the index before the next crossing
	k    int // the next crossing
	last int // the last crossing, inclusive in the walk's direction
}

func newAxisWalk(v0, v1 float64, n int) (w axisWalk) {
	switch {
	case v1 > v0:
		w.dir = 1
		first, last := math.Floor(v0)+1, math.Ceil(v1)-1
		w.cell = clampIndex(math.Floor(v0), -1, n)
		if first < 0 && first <= last {
			w.cell = -1
		}
		w.k, w.last = clampIndex(max(first, 0), 0, n+1), clampIndex(min(last, float64(n)), -1, n)
	case v1 < v0:
		w.dir = -1
		first, last := math.Ceil(v0)-1, math.Floor(v1)+1
		w.cell = clampIndex(math.Ceil(v0)-1, -1, n)
		if first > float64(n) && first >= last {
			w.cell = n
		}
		w.k, w.last = clampIndex(min(first, float64(n)), -1, n), clampIndex(max(last, 0), 0, n+1)
	default:
		w.cell = clampIndex(math.Floor(v0), -1, n)
		w.k, w.last = 0, -1
	}
	return w
}

func (inst *axisWalk) more() bool {
	switch inst.dir {
	case 1:
		return inst.k <= inst.last
	case -1:
		return inst.k >= inst.last
	}
	return false
}

// step crosses the next whole coordinate and returns the index after it.
func (inst *axisWalk) step() int {
	if inst.dir > 0 {
		inst.cell = inst.k
	} else {
		inst.cell = inst.k - 1
	}
	inst.k += inst.dir
	return inst.cell
}

// lineSpans is the pixels a line passes through as row spans in row order.
// Within a row they are consecutive columns, since a segment's part in a
// row is a segment.
func lineSpans(a, b Point, width, height int) (spans []stats.RowSpan, pixels int64) {
	traverse(a, b, width, height, func(col, row int, _, _ float64) bool {
		pixels++
		if n := len(spans); n > 0 && spans[n-1].Row == row {
			spans[n-1].Min, spans[n-1].Max = min(spans[n-1].Min, col), max(spans[n-1].Max, col+1)
			return true
		}
		spans = append(spans, stats.RowSpan{Row: row, Min: col, Max: col + 1})
		return true
	})
	slices.SortFunc(spans, func(p, q stats.RowSpan) int { return p.Row - q.Row })
	return spans, pixels
}
