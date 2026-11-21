package regions

import (
	"image"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func fractions(t require.TestingT, s Shape, w, h int) map[image.Point]float64 {
	seq, err := AreaFractions(s, w, h)
	require.NoError(t, err)
	out := make(map[image.Point]float64)
	last := image.Pt(-1, -1)
	for p, f := range seq {
		require.True(t, f > 0 && f <= 1, "fraction %v at %v", f, p)
		require.True(t, p.Y > last.Y || (p.Y == last.Y && p.X > last.X), "row-major order")
		require.True(t, p.In(image.Rect(0, 0, w, h)))
		last = p
		out[p] = f
	}
	return out
}

func total(m map[image.Point]float64) (sum float64) {
	for _, f := range m {
		sum += f
	}
	return sum
}

// supersampled is the share of a 64 × 64 grid of points in a pixel that the
// even-odd reference puts inside the shape.
func supersampled(s Shape, col, row int) float64 {
	const n = 64
	in := 0
	for i := range n {
		for j := range n {
			x, y := float64(col)+(float64(i)+0.5)/n, float64(row)+(float64(j)+0.5)/n
			if insideFloat(s, x, y) {
				in++
			}
		}
	}
	return float64(in) / (n * n)
}

// insideFloat is a plain point-in-shape test, off the tie rule's points.
func insideFloat(s Shape, x, y float64) bool {
	switch s.kind {
	case ShapeKindEllipse:
		sn, cs := sincosDeg(s.angle)
		dx, dy := x-s.a.X, y-s.a.Y
		u, v := (cs*dx+sn*dy)/s.b.X, (-sn*dx+cs*dy)/s.b.Y
		return u*u+v*v <= 1
	default:
		vs := s.vertices
		if s.kind == ShapeKindRectangle {
			vs = s.corners()
		}
		in := false
		for i, a := range vs {
			b := vs[(i+1)%len(vs)]
			if (a.Y <= y) != (b.Y <= y) && x < a.X+(y-a.Y)*(b.X-a.X)/(b.Y-a.Y) {
				in = !in
			}
		}
		return in
	}
}

// A circle's fractions add up to πr², a rotated ellipse's to π·rx·ry.
func TestEllipseAreaAddsUpToTheClosedForm(t *testing.T) {
	for _, c := range []struct {
		cx, cy, rx, ry, angle float64
	}{
		{20, 20, 7.3, 7.3, 0}, {20.5, 19.5, 3, 3, 0}, {30, 25, 12.25, 4.5, 33}, {25, 25, 0.3, 0.2, 10},
		{24.9, 25.1, 20, 1, -71}, {25, 25, 0.5, 0.5, 0}, {100, 80, 70, 60, 5},
	} {
		s, err := NewEllipse(Point{X: c.cx, Y: c.cy}, c.rx, c.ry, c.angle)
		require.NoError(t, err)
		got := total(fractions(t, s, 200, 160))
		assert.InDelta(t, math.Pi*c.rx*c.ry, got, 1e-9*math.Pi*c.rx*c.ry, "%+v", c)
	}
}

// A polygon's fractions add up to its shoelace area, and a pixel-aligned
// rectangle's are 0 or 1.
func TestPolygonAreaAddsUpToTheShoelaceArea(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(3, 8).Draw(t, "n")
		// A star-shaped ring about a centre, which is simple.
		cx, cy := rapid.Float64Range(15, 25).Draw(t, "cx"), rapid.Float64Range(15, 25).Draw(t, "cy")
		vs := make([]Point, n)
		for i := range vs {
			a := 2 * math.Pi * (float64(i) + rapid.Float64Range(0, 0.9).Draw(t, "jitter")) / float64(n)
			r := rapid.Float64Range(0.3, 12).Draw(t, "r")
			vs[i] = Point{X: cx + r*math.Cos(a), Y: cy + r*math.Sin(a)}
		}
		s, err := NewPolygon(vs)
		if err != nil {
			t.Skip("degenerate")
		}
		got := total(fractions(t, s, 40, 40))
		require.InDelta(t, math.Abs(signedArea(vs)), got, 1e-9*(1+math.Abs(signedArea(vs))))
	})
	r, err := NewRectangle(Point{X: 5, Y: 4}, 6, 4, 0)
	require.NoError(t, err)
	m := fractions(t, r, 12, 12)
	assert.Len(t, m, 24)
	for p, f := range m {
		assert.Equal(t, 1.0, f, "%v", p)
		assert.True(t, p.In(image.Rect(2, 2, 8, 6)))
	}
	// Off the grid, each pixel's share is the product of its overlaps.
	r, err = NewRectangle(Point{X: 3.375, Y: 2.5}, 2.25, 1.5, 0)
	require.NoError(t, err)
	m = fractions(t, r, 12, 12)
	overlap := func(a, b, c float64) float64 { return max(0, min(b, c+1)-max(a, c)) }
	for row := range 12 {
		for col := range 12 {
			want := overlap(2.25, 4.5, float64(col)) * overlap(1.75, 3.25, float64(row))
			assert.InDelta(t, want, m[image.Pt(col, row)], 1e-15, "(%d, %d)", col, row)
		}
	}
}

// Every pixel's fraction is within the error bound of a 64 × 64
// supersampling of it, for rotated rectangles, ellipses and polygons —
// self-crossing ones included, under the even-odd rule — and the
// supersampling's mean error is far below its bound. The bound: the
// outline crosses at most 8·64 of a pixel's 64² sample cells (four
// monotone pieces, two crossings per cell row or column each), and each
// misjudged cell is off by 1/64².
func TestAreaFractionsAgreeWithSupersampling(t *testing.T) {
	const bound = 8.0 / 64
	rapid.Check(t, func(t *rapid.T) {
		s := drawAreaShape(t, 1, 9)
		got := fractions(t, s, 10, 10)
		var sumErr float64
		for row := range 10 {
			for col := range 10 {
				ss := supersampled(s, col, row)
				e := math.Abs(got[image.Pt(col, row)] - ss)
				require.LessOrEqual(t, e, bound, "pixel (%d, %d) of %+v", col, row, s)
				sumErr += e
			}
		}
		require.Less(t, sumErr/100, 1.0/64)
	})
}

// A shape reaching outside the image yields the pixels inside it only, with
// their fractions unchanged.
func TestAreaFractionsAreClippedToTheImage(t *testing.T) {
	e, err := NewEllipse(Point{X: 1, Y: 1}, 3, 2, 20)
	require.NoError(t, err)
	clipped := fractions(t, e, 10, 10)
	moved, err := NewEllipse(Point{X: 6, Y: 6}, 3, 2, 20)
	require.NoError(t, err)
	whole := fractions(t, moved, 20, 20)
	for p, f := range clipped {
		assert.InDelta(t, whole[p.Add(image.Pt(5, 5))], f, 1e-12)
	}
	assert.Less(t, total(clipped), total(whole))
	assert.InDelta(t, 6*math.Pi, total(whole), 1e-9)
	p, _ := NewPoint(Point{X: 1, Y: 1})
	_, err = AreaFractions(p, 5, 5)
	assert.Error(t, err)
}

func TestCirclePolygonAreaClosedForms(t *testing.T) {
	sq := func(x0, y0, x1, y1 float64) [][2]float64 {
		return [][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
	}
	assert.InDelta(t, math.Pi, circlePolygonArea(sq(-2, -2, 2, 2)), 1e-15, "the whole circle")
	assert.InDelta(t, math.Pi/4, circlePolygonArea(sq(0, 0, 2, 2)), 1e-15, "a quadrant")
	assert.InDelta(t, 0.25, circlePolygonArea(sq(0, 0, 0.5, 0.5)), 1e-15, "a square inside")
	assert.Zero(t, circlePolygonArea(sq(1, 1, 2, 2)), "outside, touching nothing")
	assert.Zero(t, circlePolygonArea(sq(1, -1, 2, 1)), "touching at one point")
	// Half the circle, cut by x = 0.
	assert.InDelta(t, math.Pi/2, circlePolygonArea(sq(0, -3, 3, 3)), 1e-15)
	// A circular segment: the part with x ≥ 0.5 is acos(0.5) − 0.5·√(0.75).
	assert.InDelta(t, math.Acos(0.5)-0.5*math.Sqrt(0.75), circlePolygonArea(sq(0.5, -3, 3, 3)), 1e-15)
}
