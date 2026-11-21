package regions

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An ellipse's outline has its vertices on the ellipse and its edges within
// the tolerance of it; a finer tolerance takes more vertices; the other
// shapes are their own vertices.
func TestTheOutlineOfAnEllipseStaysWithinItsTolerance(t *testing.T) {
	for _, angle := range []float64{0, 30, 90, 137} {
		s, err := NewEllipse(Point{X: 40, Y: 25}, 30, 7, angle)
		require.NoError(t, err)
		e := newEllipseGeom(s)
		// The implicit form at a point: 1 on the ellipse.
		level := func(p Point) float64 {
			dx, dy := p.X-e.cx, p.Y-e.cy
			return e.a*dx*dx + e.b*dx*dy + e.cc*dy*dy
		}
		prev := 0
		for _, tol := range []float64{1, 0.25, 0.01} {
			vs, closed, err := OutlinePolyline(s, tol)
			require.NoError(t, err)
			assert.True(t, closed)
			assert.Greater(t, len(vs), prev, "a finer tolerance takes more vertices")
			prev = len(vs)
			for _, v := range vs {
				assert.InDelta(t, 1, level(v), 1e-9, "a vertex lies on the ellipse")
			}
			// The worst distance from the ellipse to an edge, sampled along
			// the ellipse between two vertices, is within the tolerance.
			n := len(vs)
			worst := 0.0
			sn, cs := sincosDeg(angle)
			for i := range n {
				for _, f := range []float64{0.25, 0.5, 0.75} {
					u := 2 * math.Pi * (float64(i) + f) / float64(n)
					x, y := 30*math.Cos(u), 7*math.Sin(u)
					q := Point{X: 40 + cs*x - sn*y, Y: 25 + sn*x + cs*y}
					worst = max(worst, segmentDistance(q, vs[i], vs[(i+1)%n]))
				}
			}
			assert.LessOrEqual(t, worst, tol, "angle %g tolerance %g", angle, tol)
		}
	}
	s, err := NewEllipse(Point{}, 1, 1, 0)
	require.NoError(t, err)
	vs, _, err := OutlinePolyline(s, 5)
	require.NoError(t, err)
	assert.Len(t, vs, minEllipseVertices, "a coarse tolerance still takes the floor")
	huge, err := NewEllipse(Point{}, 1e9, 1e9, 0)
	require.NoError(t, err)
	vs, _, err = OutlinePolyline(huge, 1e-6)
	require.NoError(t, err)
	assert.Len(t, vs, MaxOutlineVertices, "and never more than the cap")
	_, _, err = OutlinePolyline(s, 0)
	require.Error(t, err)

	rect, err := NewRectangle(Point{X: 5, Y: 5}, 4, 2, 0)
	require.NoError(t, err)
	vs, closed, err := OutlinePolyline(rect, 1)
	require.NoError(t, err)
	assert.True(t, closed)
	assert.Equal(t, []Point{{3, 4}, {7, 4}, {7, 6}, {3, 6}}, vs)
	line, err := NewLine(Point{X: 1, Y: 2}, Point{X: 3, Y: 4})
	require.NoError(t, err)
	vs, closed, err = OutlinePolyline(line, 1)
	require.NoError(t, err)
	assert.False(t, closed)
	assert.Equal(t, []Point{{1, 2}, {3, 4}}, vs)
	_, _, err = OutlinePolyline(Shape{}, 1)
	require.Error(t, err)
}

// FromPixels inverts ToPixels exactly under an affine placement, keeping
// the kind; a rectangle that the affine shears becomes a polygon and says
// so.
func TestFromPixelsInvertsToPixelsUnderAnAffine(t *testing.T) {
	frame := Frame{Kind: FrameKindReferenceSystem, Code: "EPSG:2056"}
	for name, a := range affines {
		p := refsys(a)
		shapes := map[string]Shape{}
		var err error
		shapes["point"], err = NewPoint(Point{X: 12.5, Y: 7.25})
		require.NoError(t, err)
		shapes["line"], err = NewLine(Point{X: 1, Y: 2}, Point{X: 30, Y: 17})
		require.NoError(t, err)
		shapes["ellipse"], err = NewEllipse(Point{X: 20, Y: 10}, 6, 3, 25)
		require.NoError(t, err)
		shapes["polygon"], err = NewPolygon([]Point{{0, 0}, {10, 0}, {4, 6}})
		require.NoError(t, err)
		shapes["rectangle"], err = NewRectangle(Point{X: 20, Y: 10}, 8, 4, 0)
		require.NoError(t, err)
		for kind, px := range shapes {
			world, m, err := FromPixels(px, frame, p)
			require.NoError(t, err, "%s %s", name, kind)
			if kind == "rectangle" && (name == "sheared" || name == "everything" || name == "anisotropic") && m.How == MappingPolygon {
				assert.Equal(t, ShapeKindPolygon, world.Kind())
				assert.Equal(t, 4, m.Vertices)
				continue
			}
			require.Equal(t, MappingExact, m.How, "%s %s", name, kind)
			require.Equal(t, px.Kind(), world.Kind(), "%s %s", name, kind)
			back, _, err := ToPixels(world, frame, p, 0)
			require.NoError(t, err)
			lo, hi := px.Bounds()
			blo, bhi := back.Bounds()
			for _, d := range []float64{lo.X - blo.X, lo.Y - blo.Y, hi.X - bhi.X, hi.Y - bhi.Y} {
				assert.InDelta(t, 0, d, 1e-6, "%s %s: the round trip comes back", name, kind)
			}
		}
	}
	sheared := refsys(affines["sheared"])
	rect, err := NewRectangle(Point{X: 20, Y: 10}, 8, 4, 0)
	require.NoError(t, err)
	world, m, err := FromPixels(rect, frame, sheared)
	require.NoError(t, err)
	assert.Equal(t, MappingPolygon, m.How, "a sheared rectangle is no rectangle in the frame")
	assert.Equal(t, ShapeKindPolygon, world.Kind())

	_, _, err = FromPixels(rect, Frame{Kind: FrameKindReferenceSystem, Code: "EPSG:4326"}, sheared)
	require.Error(t, err, "another frame than the image's")
	same, m, err := FromPixels(rect, PixelFrame(), sheared)
	require.NoError(t, err)
	assert.Equal(t, MappingIdentity, m.How)
	assert.Equal(t, rect, same)
}

// On the sky a pixel rectangle is the polygon of its corners and an ellipse
// is refused.
func TestFromPixelsOnTheSky(t *testing.T) {
	p := skyPlacement()
	rect, err := NewRectangle(Point{X: 500, Y: 500}, 20, 10, 0)
	require.NoError(t, err)
	world, m, err := FromPixels(rect, sky, p)
	require.NoError(t, err)
	assert.Equal(t, MappingPolygon, m.How)
	back, _, err := ToPixels(world, sky, p, 0)
	require.NoError(t, err)
	for i, v := range back.Vertices() {
		want := rect.Outline()[i]
		assert.InDelta(t, want.X, v.X, 1e-6)
		assert.InDelta(t, want.Y, v.Y, 1e-6)
	}
	e, err := NewEllipse(Point{X: 500, Y: 500}, 5, 3, 0)
	require.NoError(t, err)
	_, _, err = FromPixels(e, sky, p)
	require.Error(t, err)
}
