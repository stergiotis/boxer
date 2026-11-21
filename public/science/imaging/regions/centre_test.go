package regions

import (
	"image"
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// The references here decide one pixel centre at a time, in rational
// numbers throughout, from the rule as the package documentation words it.

// refPolygonInside: the edges meeting the row line y — one end at or above
// it, the other strictly below — at or left of x are odd in number.
func refPolygonInside(vs []Point, x, y float64) bool {
	rx, ry := rat(x), rat(y)
	n := 0
	for i, a := range vs {
		b := vs[(i+1)%len(vs)]
		if !(min(a.Y, b.Y) <= y && y < max(a.Y, b.Y)) {
			continue
		}
		// xc = ax + (y − ay)(bx − ax)/(by − ay)
		var xc, t big.Rat
		t.Quo(ratDiff(y, a.Y), ratDiff(b.Y, a.Y))
		xc.Mul(&t, ratDiff(b.X, a.X))
		xc.Add(&xc, rat(a.X))
		if xc.Cmp(rx) <= 0 {
			n++
		}
		_ = ry
	}
	return n%2 == 1
}

// refEllipseInside: x lies in [left end, right end) of the chord on the row
// line y — inside, or on the outline where the shape lies to the right.
func refEllipseInside(s Shape, x, y float64) bool {
	sn, cs := sincosDeg(s.angle)
	rs, rc := rat(sn), rat(cs)
	dx, dy := ratDiff(x, s.a.X), ratDiff(y, s.a.Y)
	var u, v, t1, t2 big.Rat
	u.Add(t1.Mul(rc, dx), t2.Mul(rs, dy))
	v.Sub(t1.Mul(rc, dy), t2.Mul(rs, dx))
	rx2, ry2 := new(big.Rat).Mul(rat(s.b.X), rat(s.b.X)), new(big.Rat).Mul(rat(s.b.Y), rat(s.b.Y))
	var l, a, b, r big.Rat
	a.Mul(&u, &u)
	a.Mul(&a, ry2)
	b.Mul(&v, &v)
	b.Mul(&b, rx2)
	l.Add(&a, &b)
	r.Mul(rx2, ry2)
	switch l.Cmp(&r) {
	case -1:
		return true
	case 1:
		return false
	}
	// On the outline: the left end, where Q falls as x grows.
	a.Mul(&u, rc)
	a.Mul(&a, ry2)
	b.Mul(&v, rs)
	b.Mul(&b, rx2)
	return a.Cmp(&b) < 0
}

func refInside(s Shape, x, y float64) bool {
	switch s.kind {
	case ShapeKindPolygon:
		return refPolygonInside(s.vertices, x, y)
	case ShapeKindRectangle:
		return refPolygonInside(s.corners(), x, y)
	case ShapeKindEllipse:
		return refEllipseInside(s, x, y)
	}
	panic("not an area shape")
}

func requireCoverageIsReference(t require.TestingT, s Shape, w, h int) Coverage {
	c, err := CoverageOf(s, w, h)
	require.NoError(t, err)
	var n int64
	for row := range h {
		for col := range w {
			want := refInside(s, float64(col)+0.5, float64(row)+0.5)
			require.Equal(t, want, c.Contains(col, row), "pixel (%d, %d) of %+v", col, row, s)
			if want {
				n++
			}
		}
	}
	require.Equal(t, n, c.Pixels())
	var last struct{ row, max int }
	last.row = -1
	for sp := range c.All() {
		require.Less(t, sp.Min, sp.Max)
		require.True(t, sp.Row > last.row || sp.Min > last.max, "spans in order and apart")
		last.row, last.max = sp.Row, sp.Max
	}
	return c
}

// drawCoord draws a coordinate on a lattice of step 1/2^k for small k, so
// that ties — centres on edges, outlines through centres — are common, or
// any float in range.
func drawCoord(t *rapid.T, lo, hi float64, label string) float64 {
	if rapid.Bool().Draw(t, label+" on lattice") {
		k := rapid.SampledFrom([]float64{1, 2, 4}).Draw(t, label+" lattice")
		return float64(rapid.IntRange(int(math.Ceil(lo*k)), int(math.Floor(hi*k))).Draw(t, label)) / k
	}
	return rapid.Float64Range(lo, hi).Draw(t, label)
}

func drawPoint(t *rapid.T, lo, hi float64, label string) Point {
	return Point{X: drawCoord(t, lo, hi, label+".x"), Y: drawCoord(t, lo, hi, label+".y")}
}

func drawAreaShape(t *rapid.T, lo, hi float64) Shape {
	switch rapid.IntRange(0, 2).Draw(t, "kind") {
	case 0:
		angle := rapid.SampledFrom([]float64{0, 90, 180, -90, 30, 45, 137.5}).Draw(t, "angle")
		s, err := NewRectangle(drawPoint(t, lo, hi, "centre"), drawCoord(t, 0.5, hi-lo, "w"), drawCoord(t, 0.5, hi-lo, "h"), angle)
		require.NoError(t, err)
		return s
	case 1:
		angle := rapid.SampledFrom([]float64{0, 90, 180, 270, 30, 45, -61}).Draw(t, "angle")
		s, err := NewEllipse(drawPoint(t, lo, hi, "centre"), drawCoord(t, 0.5, (hi-lo)/2, "rx"), drawCoord(t, 0.5, (hi-lo)/2, "ry"), angle)
		require.NoError(t, err)
		return s
	}
	for {
		n := rapid.IntRange(3, 7).Draw(t, "vertices")
		vs := make([]Point, n)
		for i := range vs {
			vs[i] = drawPoint(t, lo, hi, "v")
		}
		if s, err := NewPolygon(vs); err == nil {
			return s
		}
	}
}

// Coverage by centre agrees, pixel for pixel, with the reference over
// random shapes of every area kind — on lattices of whole, half and quarter
// coordinates, where ties are common, and anywhere — including shapes that
// reach outside the image and polygons that cross themselves.
func TestCoverageByCentreIsTheRule(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := drawAreaShape(t, -3, 15)
		requireCoverageIsReference(t, s, 12, 10)
	})
}

// Two triangles on either side of a shared edge cover every pixel centre on
// it once: never both, and together exactly the quadrilateral they make.
func TestShapesSharingAnEdgeCoverEachPixelOnce(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p, r := drawPoint(t, -2, 14, "p"), drawPoint(t, -2, 14, "r")
		q, s := drawPoint(t, -2, 14, "q"), drawPoint(t, -2, 14, "s")
		side := func(x Point) int {
			return signDiffProd(r.X, p.X, x.Y, p.Y, r.Y, p.Y, x.X, p.X)
		}
		if side(q)*side(s) >= 0 {
			t.Skip("q and s not strictly on opposite sides of pr")
		}
		a, errA := NewPolygon([]Point{p, q, r})
		b, errB := NewPolygon([]Point{p, r, s})
		quad, errQ := NewPolygon([]Point{p, q, r, s})
		require.NoError(t, errA)
		require.NoError(t, errB)
		require.NoError(t, errQ)
		ca := requireCoverageIsReference(t, a, 12, 12)
		cb := requireCoverageIsReference(t, b, 12, 12)
		cq := requireCoverageIsReference(t, quad, 12, 12)
		for row := range 12 {
			for col := range 12 {
				inA, inB := ca.Contains(col, row), cb.Contains(col, row)
				require.False(t, inA && inB, "pixel (%d, %d) counted twice", col, row)
				require.Equal(t, cq.Contains(col, row), inA || inB, "pixel (%d, %d)", col, row)
			}
		}
	})
}

// Two rectangles that meet along a whole or a half coordinate, horizontal
// or vertical, partition the rectangle they make.
func TestRectanglesSharingAnEdgePartitionTheirUnion(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		x0 := float64(rapid.IntRange(-4, 20).Draw(t, "x0")) / 2
		x1 := x0 + float64(rapid.IntRange(1, 20).Draw(t, "w"))/2
		y0 := float64(rapid.IntRange(-4, 20).Draw(t, "y0")) / 2
		y1 := y0 + float64(rapid.IntRange(1, 20).Draw(t, "h"))/2
		rect := func(a, b, c, d float64) Shape {
			s, err := NewRectangle(Point{X: (a + b) / 2, Y: (c + d) / 2}, b-a, d-c, 0)
			require.NoError(t, err)
			return s
		}
		var parts [2]Shape
		if rapid.Bool().Draw(t, "split x") {
			if x1-x0 < 1 {
				t.Skip("too narrow to split")
			}
			m := x0 + float64(rapid.IntRange(1, int((x1-x0)*2)-1).Draw(t, "at"))/2
			parts = [2]Shape{rect(x0, m, y0, y1), rect(m, x1, y0, y1)}
		} else {
			if y1-y0 < 1 {
				t.Skip("too low to split")
			}
			m := y0 + float64(rapid.IntRange(1, int((y1-y0)*2)-1).Draw(t, "at"))/2
			parts = [2]Shape{rect(x0, x1, y0, m), rect(x0, x1, m, y1)}
		}
		whole, _ := CoverageOf(rect(x0, x1, y0, y1), 12, 12)
		a, _ := CoverageOf(parts[0], 12, 12)
		b, _ := CoverageOf(parts[1], 12, 12)
		require.Equal(t, whole.Pixels(), a.Pixels()+b.Pixels())
		for row := range 12 {
			for col := range 12 {
				require.False(t, a.Contains(col, row) && b.Contains(col, row))
				require.Equal(t, whole.Contains(col, row), a.Contains(col, row) || b.Contains(col, row))
			}
		}
	})
}

// A rectangle on whole coordinates covers what the equal image.Rectangle
// holds, clipped to the image.
func TestARectangleOnWholeCoordinatesIsTheImageRectangle(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		x0, y0 := rapid.IntRange(-5, 15).Draw(t, "x0"), rapid.IntRange(-5, 15).Draw(t, "y0")
		x1, y1 := x0+rapid.IntRange(1, 15).Draw(t, "w"), y0+rapid.IntRange(1, 15).Draw(t, "h")
		r := image.Rect(x0, y0, x1, y1)
		s, err := NewRectangle(Point{X: float64(x0+x1) / 2, Y: float64(y0+y1) / 2}, float64(x1-x0), float64(y1-y0),
			rapid.SampledFrom([]float64{0, 180, 360, -180}).Draw(t, "angle"))
		require.NoError(t, err)
		c, err := CoverageOf(s, 10, 8)
		require.NoError(t, err)
		clipped := r.Intersect(image.Rect(0, 0, 10, 8))
		require.Equal(t, int64(clipped.Dx()*clipped.Dy()), c.Pixels())
		require.Equal(t, clipped, c.Bounds())
	})
}

func TestTiesOnAnOutlineFollowTheRule(t *testing.T) {
	// A square whose edges run through pixel centres: its left and top
	// edges' centres are in, its right and bottom edges' are not.
	sq, err := NewPolygon([]Point{{1.5, 1.5}, {4.5, 1.5}, {4.5, 4.5}, {1.5, 4.5}})
	require.NoError(t, err)
	c, err := CoverageOf(sq, 8, 8)
	require.NoError(t, err)
	assert.Equal(t, image.Rect(1, 1, 4, 4), c.Bounds())
	assert.EqualValues(t, 9, c.Pixels())

	// A diamond whose vertices are pixel centres: the top and bottom
	// vertices, alone on their rows, cover nothing; the left vertex is in,
	// the right one is not.
	d, err := NewPolygon([]Point{{3.5, 0.5}, {5.5, 2.5}, {3.5, 4.5}, {1.5, 2.5}})
	require.NoError(t, err)
	c, err = CoverageOf(d, 8, 8)
	require.NoError(t, err)
	assert.False(t, c.Contains(3, 0))
	assert.False(t, c.Contains(3, 4))
	assert.True(t, c.Contains(1, 2))
	assert.False(t, c.Contains(5, 2))
	assert.True(t, c.Contains(2, 1), "on the upper left edge")
	assert.False(t, c.Contains(4, 1), "on the upper right edge")

	// A circle of radius 2 about a pixel centre passes through four centres:
	// the left and the right on the centre's row — in and out — and the top
	// and bottom, tangent points, out.
	e, err := NewEllipse(Point{X: 4.5, Y: 4.5}, 2, 2, 0)
	require.NoError(t, err)
	c, err = CoverageOf(e, 10, 10)
	require.NoError(t, err)
	assert.True(t, c.Contains(2, 4))
	assert.False(t, c.Contains(6, 4))
	assert.False(t, c.Contains(4, 2))
	assert.False(t, c.Contains(4, 6))
	assert.True(t, c.Contains(5, 4))
	// The same circle turned by 90° is the same set.
	e90, err := NewEllipse(Point{X: 4.5, Y: 4.5}, 2, 2, 90)
	require.NoError(t, err)
	c90, err := CoverageOf(e90, 10, 10)
	require.NoError(t, err)
	assert.Equal(t, c, c90)
}

// A ring that crosses itself is inside where the even-odd rule says: the
// pentagon in the middle of a five-pointed star is outside it.
func TestASelfCrossingRingIsEvenOdd(t *testing.T) {
	var star []Point
	for i := range 5 {
		a := float64(i*144-90) * math.Pi / 180
		star = append(star, Point{X: 50 + 40*math.Cos(a), Y: 50 + 40*math.Sin(a)})
	}
	s, err := NewPolygon(star)
	require.NoError(t, err)
	c := requireCoverageIsReference(t, s, 100, 100)
	assert.False(t, c.Contains(50, 50), "the inner pentagon is crossed twice")
	assert.True(t, c.Contains(50, 15), "a point of the star")

	_, err = NewPolygon([]Point{{0, 0}, {2, 2}, {2, 0}, {0, 2}})
	assert.Error(t, err, "a symmetric figure-eight has no signed area")
}

func TestPointsAndLinesCoverTheirPixels(t *testing.T) {
	p, _ := NewPoint(Point{X: 3, Y: 2.999})
	c, err := CoverageOf(p, 5, 5)
	require.NoError(t, err)
	assert.Equal(t, CoverageRulePoint, c.Rule())
	assert.True(t, c.Contains(3, 2))
	assert.EqualValues(t, 1, c.Pixels())
	for _, at := range []Point{{-0.5, 1}, {5, 1}, {1, 5}, {1, -1e-300}} {
		p, _ = NewPoint(at)
		c, _ = CoverageOf(p, 5, 5)
		assert.Zero(t, c.Pixels(), "%v is outside", at)
	}
	l, _ := NewLine(Point{X: 0.5, Y: 0.5}, Point{X: 3.5, Y: 3.5})
	c, err = CoverageOf(l, 5, 5)
	require.NoError(t, err)
	assert.Equal(t, CoverageRuleLine, c.Rule())
	assert.EqualValues(t, 4, c.Pixels(), "through the corners, the diagonal only")
	for i := range 4 {
		assert.True(t, c.Contains(i, i))
	}
	_, err = CoverageOf(Shape{}, 5, 5)
	assert.Error(t, err)
	_, err = CoverageOf(l, 0, 5)
	assert.Error(t, err)
}

func TestShapesAreValidatedWhenMade(t *testing.T) {
	_, err := NewPoint(Point{X: math.NaN()})
	assert.Error(t, err)
	_, err = NewLine(Point{}, Point{X: math.Inf(1)})
	assert.Error(t, err)
	_, err = NewRectangle(Point{}, 0, 1, 0)
	assert.Error(t, err)
	_, err = NewRectangle(Point{}, 1, 1, math.NaN())
	assert.Error(t, err)
	_, err = NewEllipse(Point{}, 1, -1, 0)
	assert.Error(t, err)
	_, err = NewEllipse(Point{X: 2 * MaxMagnitude}, 1, 1, 0)
	assert.Error(t, err)
	_, err = NewPolygon([]Point{{0, 0}, {1, 1}})
	assert.Error(t, err)
	_, err = NewPolygon([]Point{{0, 0}, {1, 1}, {2, 2}})
	assert.Error(t, err, "collinear vertices have no area")
	l, err := NewLine(Point{X: 1, Y: 1}, Point{X: 1, Y: 1})
	require.NoError(t, err, "a line may have zero length")
	from, to := l.Ends()
	assert.Equal(t, from, to)

	// The angle turns +x toward +y: a rectangle 4 wide and 2 high turned by
	// 90° stands 4 high, its first corner at the right top on screen.
	r, err := NewRectangle(Point{X: 10, Y: 10}, 4, 2, 90)
	require.NoError(t, err)
	lo, hi := r.Bounds()
	assert.Equal(t, Point{X: 9, Y: 8}, lo)
	assert.Equal(t, Point{X: 11, Y: 12}, hi)
	assert.Equal(t, Point{X: 11, Y: 8}, r.Outline()[0])
	e, _ := NewEllipse(Point{}, 3, 1, 90)
	lo, hi = e.Bounds()
	assert.InDelta(t, 1, hi.X, 1e-15)
	assert.InDelta(t, 3, hi.Y, 1e-15)
	assert.InDelta(t, -3, lo.Y, 1e-15)
}
