package regions

import (
	"image"
	"iter"
	"math"
	"slices"
	"sort"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging/stats"
)

// CoverageRuleE names the rule a figure's pixels were chosen by (ADR-0399
// §SD3, §SD4).
type CoverageRuleE uint8

const (
	// CoverageRuleCentre: the pixels whose centre lies inside an area shape,
	// a centre on the outline decided by the tie rule (§SD3).
	CoverageRuleCentre CoverageRuleE = iota + 1
	// CoverageRulePoint: the pixel that contains a point (§SD3).
	CoverageRulePoint
	// CoverageRuleLine: the pixels a line passes through (§SD3, §SD8).
	CoverageRuleLine
	// CoverageRuleArea: every pixel weighted by the fraction of its area an
	// area shape covers (§SD4).
	CoverageRuleArea
)

// AllCoverageRules lists the rules.
var AllCoverageRules = []CoverageRuleE{CoverageRuleCentre, CoverageRulePoint, CoverageRuleLine, CoverageRuleArea}

func (inst CoverageRuleE) String() string {
	switch inst {
	case CoverageRuleCentre:
		return "centre"
	case CoverageRulePoint:
		return "point"
	case CoverageRuleLine:
		return "line"
	case CoverageRuleArea:
		return "area"
	}
	return "none"
}

// Coverage is the pixels a shape covers under ADR-0399 §SD3, as row spans
// in row order and, within a row, in column order without overlap, clipped
// to the image. The zero value covers nothing.
type Coverage struct {
	rule   CoverageRuleE
	spans  []stats.RowSpan
	pixels int64
}

// Rule is the rule the pixels were chosen by: [CoverageRuleCentre] for an
// area shape, [CoverageRulePoint] or [CoverageRuleLine].
func (inst Coverage) Rule() CoverageRuleE { return inst.rule }

// Pixels is the number of pixels covered.
func (inst Coverage) Pixels() int64 { return inst.pixels }

// All is the covered pixels, one run of a row at a time.
func (inst Coverage) All() iter.Seq[stats.RowSpan] {
	return func(yield func(stats.RowSpan) bool) {
		for _, s := range inst.spans {
			if !yield(s) {
				return
			}
		}
	}
}

// Bounds is the smallest rectangle holding every covered pixel; empty when
// nothing is covered.
func (inst Coverage) Bounds() (r image.Rectangle) {
	for i, s := range inst.spans {
		b := image.Rect(s.Min, s.Row, s.Max, s.Row+1)
		if i == 0 {
			r = b
			continue
		}
		r = r.Union(b)
	}
	return r
}

// Contains reports whether pixel (col, row) is covered.
func (inst Coverage) Contains(col, row int) bool {
	i := sort.Search(len(inst.spans), func(i int) bool {
		s := inst.spans[i]
		return s.Row > row || (s.Row == row && s.Max > col)
	})
	return i < len(inst.spans) && inst.spans[i].Row == row && inst.spans[i].Min <= col
}

func (inst *Coverage) add(row, lo, hi int) {
	if lo >= hi {
		return
	}
	if n := len(inst.spans); n > 0 && inst.spans[n-1].Row == row && inst.spans[n-1].Max == lo {
		inst.spans[n-1].Max = hi
	} else {
		inst.spans = append(inst.spans, stats.RowSpan{Row: row, Min: lo, Max: hi})
	}
	inst.pixels += int64(hi - lo)
}

// CoverageOf is the pixels of a width × height image that a shape in pixel
// coordinates covers (ADR-0399 §SD3).
//
//   - An area shape — rectangle, ellipse, polygon — covers the pixels whose
//     centre (col + ½, row + ½) lies inside it. A centre on the outline is
//     decided on its row: the shape meets the row's line y = row + ½ in
//     intervals, and each interval holds its left end and not its right
//     end. For a polygon the line meets an edge only where one end of the
//     edge lies at or above the line and the other strictly below it
//     (min(y₀, y₁) ≤ y < max(y₀, y₁)), and a centre is inside when the edges
//     met at or left of it are odd in number. So a centre on a left or a
//     top edge is inside, one on a right or a bottom edge is not, and a
//     vertex or a tangent point alone on the line covers nothing; two
//     shapes sharing an edge cover each pixel centre on it once, and a
//     rectangle on whole coordinates covers the pixels of the equal
//     [image.Rectangle].
//   - A point covers the pixel containing it: (⌊x⌋, ⌊y⌋).
//   - A line covers the pixels it passes through, as [Profile] traverses
//     them.
//
// Pixels outside the image are not covered.
func CoverageOf(s Shape, width, height int) (c Coverage, err error) {
	if width <= 0 || height <= 0 {
		return c, eb.Build().Int("width", width).Int("height", height).Errorf("image size must be positive")
	}
	switch s.kind {
	case ShapeKindPoint:
		c.rule = CoverageRulePoint
		col, row := math.Floor(s.a.X), math.Floor(s.a.Y)
		if col >= 0 && row >= 0 && col < float64(width) && row < float64(height) {
			c.add(int(row), int(col), int(col)+1)
		}
	case ShapeKindLine:
		c.rule = CoverageRuleLine
		c.spans, c.pixels = lineSpans(s.a, s.b, width, height)
	case ShapeKindRectangle, ShapeKindPolygon:
		c.rule = CoverageRuleCentre
		vs := s.vertices
		if s.kind == ShapeKindRectangle {
			vs = s.corners()
		}
		polygonCentres(&c, vs, width, height)
	case ShapeKindEllipse:
		c.rule = CoverageRuleCentre
		e := newEllipseGeom(s)
		ellipseCentres(&c, &e, width, height)
	default:
		return c, eb.Build().Stringer("kind", s.kind).Errorf("no coverage for a shape of this kind")
	}
	return c, nil
}

// clampIndex turns a float coordinate into an index clamped to [lo, hi],
// so that one far outside the image cannot overflow an int.
func clampIndex(v float64, lo, hi int) int {
	switch {
	case !(v >= float64(lo)):
		return lo
	case v > float64(hi):
		return hi
	}
	return int(v)
}

// rowRange is the rows a shape spanning [y0, y1] can cover, clipped to the
// image: a conservative range, the exact rule decides each row.
func rowRange(y0, y1 float64, height int) (lo, hi int) {
	return clampIndex(math.Floor(y0)-1, 0, height), clampIndex(math.Ceil(y1)+1, 0, height)
}

// polyEdge is an edge of a ring with its ends ordered so that y0 < y1.
type polyEdge struct {
	x0, y0, x1, y1 float64
}

func ringEdges(vs []Point) (edges []polyEdge) {
	edges = make([]polyEdge, 0, len(vs))
	for i, a := range vs {
		b := vs[(i+1)%len(vs)]
		switch {
		case a.Y < b.Y:
			edges = append(edges, polyEdge{a.X, a.Y, b.X, b.Y})
		case a.Y > b.Y:
			edges = append(edges, polyEdge{b.X, b.Y, a.X, a.Y})
		}
	}
	slices.SortFunc(edges, func(p, q polyEdge) int {
		switch {
		case p.y0 < q.y0:
			return -1
		case p.y0 > q.y0:
			return 1
		}
		return 0
	})
	return edges
}

// polygonCentres fills c with the pixels whose centre a ring covers under
// the even-odd rule and the tie rule of [CoverageOf].
func polygonCentres(c *Coverage, vs []Point, width, height int) {
	edges := ringEdges(vs)
	if len(edges) == 0 {
		return
	}
	lo, hi := Point{X: vs[0].X, Y: vs[0].Y}, Point{X: vs[0].X, Y: vs[0].Y}
	for _, v := range vs {
		lo.Y, hi.Y = min(lo.Y, v.Y), max(hi.Y, v.Y)
	}
	r0, r1 := rowRange(lo.Y, hi.Y, height)
	active := make([]polyEdge, 0, 8)
	ks := make([]int, 0, 8)
	next := 0
	for row := r0; row < r1; row++ {
		y := float64(row) + 0.5
		for next < len(edges) && edges[next].y0 <= y {
			active = append(active, edges[next])
			next++
		}
		active = slices.DeleteFunc(active, func(e polyEdge) bool { return e.y1 <= y })
		ks = ks[:0]
		for _, e := range active {
			ks = append(ks, edgeThreshold(e, y, width))
		}
		slices.Sort(ks)
		for i := 0; i+1 < len(ks); i += 2 {
			c.add(row, ks[i], ks[i+1])
		}
	}
}

// edgeThreshold is the first column in [0, width] whose centre on the row
// line y lies at or right of where edge e crosses it, decided exactly.
func edgeThreshold(e polyEdge, y float64, width int) int {
	x := e.x0 + (y-e.y0)*(e.x1-e.x0)/(e.y1-e.y0)
	k := clampIndex(math.Ceil(x-0.5), 0, width)
	at := func(col int) bool { return atOrRightOfEdge(e.x0, e.y0, e.x1, e.y1, float64(col)+0.5, y) }
	for k > 0 && at(k-1) {
		k--
	}
	for k < width && !at(k) {
		k++
	}
	return k
}

// ellipseCentres fills c with the pixels whose centre an ellipse covers
// under the tie rule of [CoverageOf]: on each row, the columns from the
// first whose centre lies at or right of the chord's left end up to, not
// including, the first whose centre lies at or right of its right end.
func ellipseCentres(c *Coverage, e *ellipseGeom, width, height int) {
	r0, r1 := rowRange(e.cy-e.hy, e.cy+e.hy, height)
	for row := r0; row < r1; row++ {
		y := float64(row) + 0.5
		xl, xr, ok := e.chord(y)
		if !ok {
			xl, xr = e.cx-e.b*(y-e.cy)/(2*e.a), e.cx-e.b*(y-e.cy)/(2*e.a)
		}
		left := func(col int) bool { return e.atOrRightOfLeft(float64(col)+0.5, y) }
		kl := clampIndex(math.Ceil(xl-0.5), 0, width)
		for kl > 0 && left(kl-1) {
			kl--
		}
		for kl < width && !left(kl) {
			kl++
		}
		beyond := func(col int) bool { return !e.leftOfRight(float64(col)+0.5, y) }
		kr := clampIndex(math.Ceil(xr-0.5), 0, width)
		for kr > 0 && beyond(kr-1) {
			kr--
		}
		for kr < width && !beyond(kr) {
			kr++
		}
		c.add(row, kl, kr)
	}
}
