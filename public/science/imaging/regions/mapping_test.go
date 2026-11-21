package regions

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stergiotis/boxer/public/science/imaging"
)

func refsys(affine [6]float64) imaging.Placement {
	return imaging.Placement{Affine: affine, Frame: imaging.Frame{Kind: imaging.FrameKindReferenceSystem, Code: "EPSG:2056",
		AxisNames: [2]string{"E", "N"}, Units: [2]string{"m", "m"}}}
}

var affines = map[string][6]float64{
	"north-up":    {2_600_000, 0.5, 0, 1_200_000, 0, -0.5},
	"rotated":     {100, 2 * math.Cos(0.4), -2 * math.Sin(0.4), 50, 2 * math.Sin(0.4), 2 * math.Cos(0.4)},
	"anisotropic": {-30, 3, 0, 20, 0, 0.25},
	"sheared":     {0, 1, 0.3, 0, 0.1, 1},
	"everything":  {7, -1.5, 0.7, 3, 0.2, -4},
}

// An ellipse brought through an affine placement is the image ellipse:
// every point of its outline, mapped, lies on the result's outline, and its
// area is the original's over the determinant.
func TestAnEllipseMapsExactlyUnderAnAffine(t *testing.T) {
	for name, a := range affines {
		p := refsys(a)
		world := Point{X: a[0] + 40*a[1] + 30*a[2], Y: a[3] + 40*a[4] + 30*a[5]}
		s, err := NewEllipse(world, 9, 4, 25)
		require.NoError(t, err)
		px, m, err := ToPixels(s, Frame{Kind: FrameKindReferenceSystem, Code: "EPSG:2056"}, p, 0)
		require.NoError(t, err, name)
		assert.Equal(t, MappingExact, m.How, name)
		assert.Equal(t, ShapeKindEllipse, px.Kind(), name)
		c := px.Centre()
		assert.InDelta(t, 40, c.X, 1e-9, name)
		assert.InDelta(t, 30, c.Y, 1e-9, name)
		rx, ry := px.Radii()
		det := math.Abs(a[1]*a[5] - a[2]*a[4])
		assert.InDelta(t, 9*4/det, rx*ry, 1e-9*rx*ry, name)
		e := newEllipseGeom(px)
		sn, cs := sincosDeg(25)
		for i := range 64 {
			u := 2 * math.Pi * float64(i) / 64
			x, y := 9*math.Cos(u), 4*math.Sin(u)
			col, row, ok := p.Pixel(world.X+cs*x-sn*y, world.Y+sn*x+cs*y)
			require.True(t, ok)
			dx, dy := col-e.cx, row-e.cy
			q := e.a*dx*dx + e.b*dx*dy + e.cc*dy*dy
			assert.InDelta(t, 1, q, 1e-9, "%s, outline point %d", name, i)
		}
	}
}

// A rectangle stays a rectangle where the map keeps its right angles — a
// similarity, or an axis-aligned rectangle under an axis-aligned scale —
// and becomes the polygon of its corners where it does not; either way its
// corners are the mapped corners.
func TestARectangleStaysOneWhereTheMapKeepsItsRightAngles(t *testing.T) {
	for _, c := range []struct {
		affine string
		angle  float64
		want   MappingE
	}{
		{"north-up", 30, MappingExact}, {"rotated", 30, MappingExact}, {"anisotropic", 0, MappingExact},
		{"anisotropic", 90, MappingExact}, {"anisotropic", 30, MappingPolygon}, {"sheared", 0, MappingPolygon},
		{"everything", 10, MappingPolygon},
	} {
		a := affines[c.affine]
		p := refsys(a)
		world := Point{X: a[0] + 10*a[1] + 20*a[2], Y: a[3] + 10*a[4] + 20*a[5]}
		s, err := NewRectangle(world, 6, 2, c.angle)
		require.NoError(t, err)
		px, m, err := ToPixels(s, Frame{Kind: FrameKindReferenceSystem, Code: "EPSG:2056"}, p, 0)
		require.NoError(t, err)
		assert.Equal(t, c.want, m.How, "%+v", c)
		got := px.Outline()
		require.Len(t, got, 4)
		if c.want == MappingPolygon {
			assert.Equal(t, 4, m.Vertices)
		}
		for _, wc := range s.Outline() {
			col, row, _ := p.Pixel(wc.X, wc.Y)
			best := math.Inf(1)
			for _, g := range got {
				best = min(best, math.Hypot(g.X-col, g.Y-row))
			}
			assert.Less(t, best, 1e-9, "%+v: corner (%v, %v)", c, col, row)
		}
	}
}

func TestPointsLinesAndPolygonsMapVertexByVertex(t *testing.T) {
	p := refsys(affines["everything"])
	f := Frame{Kind: FrameKindReferenceSystem, Code: "EPSG:2056"}
	vs := []Point{{1, 2}, {30, -4}, {12, 40}}
	poly, _ := NewPolygon(vs)
	px, m, err := ToPixels(poly, f, p, 0)
	require.NoError(t, err)
	assert.Equal(t, MappingExact, m.How)
	for i, v := range px.Vertices() {
		col, row, _ := p.Pixel(vs[i].X, vs[i].Y)
		assert.Equal(t, Point{X: col, Y: row}, v)
	}
	l, _ := NewLine(vs[0], vs[1])
	px, _, err = ToPixels(l, f, p, 0)
	require.NoError(t, err)
	a, b := px.Ends()
	col, row, _ := p.Pixel(vs[1].X, vs[1].Y)
	assert.Equal(t, Point{X: col, Y: row}, b)
	col, row, _ = p.Pixel(vs[0].X, vs[0].Y)
	assert.Equal(t, Point{X: col, Y: row}, a)

	// The pixel frame is the identity, whatever the placement.
	e, _ := NewEllipse(Point{X: 3, Y: 4}, 2, 1, 10)
	px, m, err = ToPixels(e, PixelFrame(), p, 0)
	require.NoError(t, err)
	assert.Equal(t, MappingIdentity, m.How)
	assert.Equal(t, e, px)
	px, _, err = ToPixels(e, PixelFrame(), imaging.Placement{}, 0)
	require.NoError(t, err)
	assert.Equal(t, e, px)
}

// A region is brought only to an image whose placement names its frame,
// and a refusal names both frames.
func TestAnotherFrameIsRefusedByName(t *testing.T) {
	e, _ := NewEllipse(Point{X: 3, Y: 4}, 2, 1, 10)
	p := refsys(affines["north-up"])
	for name, f := range map[string]Frame{
		"another code":  {Kind: FrameKindReferenceSystem, Code: "EPSG:32632"},
		"another kind":  {Kind: FrameKindPhysical, Units: [2]string{"m", "m"}},
		"the sky":       {Kind: FrameKindCelestial, Code: "EPSG:2056"},
		"no frame":      {},
		"an empty code": {Kind: FrameKindReferenceSystem},
	} {
		_, _, err := ToPixels(e, f, p, 0)
		assert.Error(t, err, name)
	}
	_, _, err := ToPixels(e, Frame{Kind: FrameKindReferenceSystem, Code: "EPSG:32632"}, p, 0)
	require.Error(t, err)
	fields := ebtest.Fields(t, err)
	assert.Equal(t, "reference-system EPSG:32632", fields["region"])
	assert.Equal(t, "reference-system EPSG:2056", fields["image"])
	_, _, err = ToPixels(e, Frame{Kind: FrameKindReferenceSystem, Code: "EPSG:2056"}, imaging.Placement{}, 0)
	assert.Error(t, err, "an image without a placement")

	// Two placements whose code is unknown are not the same frame.
	q := refsys(affines["north-up"])
	q.Frame.Code = ""
	assert.False(t, Frame{Kind: FrameKindReferenceSystem}.Matches(q))
	phys := imaging.Placement{Affine: [6]float64{0, 0.5, 0, 0, 0, 0.5}, Frame: imaging.Frame{Kind: imaging.FrameKindPhysical, Units: [2]string{"um", "um"}}}
	assert.True(t, Frame{Kind: FrameKindPhysical, Units: [2]string{"um", "um"}}.Matches(phys))
	assert.False(t, Frame{Kind: FrameKindPhysical, Units: [2]string{"mm", "mm"}}.Matches(phys))
	phys.Frame.Units = [2]string{}
	assert.False(t, Frame{Kind: FrameKindPhysical, Units: [2]string{"", ""}}.Matches(phys))
}

// skyPlacement is a gnomonic placement 1″ a pixel about (150°, 2°), north up
// and east left, the reference point at pixel (512, 512).
func skyPlacement() imaging.Placement {
	const d = 1.0 / 3600
	return imaging.Placement{
		Affine: [6]float64{512 * d, -d, 0, 512 * d, 0, -d},
		Frame: imaging.Frame{Kind: imaging.FrameKindCelestialGnomonic, Code: "ICRS", AxisNames: [2]string{"RA", "Dec"},
			Units: [2]string{"deg", "deg"}, RefLon: 150, RefLat: 2, LonPole: 180},
	}
}

var sky = Frame{Kind: FrameKindCelestial, Code: "ICRS"}

// skyOfTangent is the sky position of tangent-plane coordinates (ξ, η), in
// degrees, about a centre, by the textbook gnomonic inverse.
func skyOfTangent(c Point, xi, eta float64) (lon, lat float64) {
	x, y := xi*math.Pi/180, eta*math.Pi/180
	a0, d0 := c.X*math.Pi/180, c.Y*math.Pi/180
	den := math.Cos(d0) - y*math.Sin(d0)
	lat = math.Atan2(math.Sin(d0)+y*math.Cos(d0), math.Hypot(x, den))
	lon = a0 + math.Atan2(x, den)
	return lon * 180 / math.Pi, lat * 180 / math.Pi
}

func distanceToRing(q Point, vs []Point) float64 {
	best := math.Inf(1)
	for i, a := range vs {
		best = min(best, segmentDistance(q, a, vs[(i+1)%len(vs)]))
	}
	return best
}

// Under a gnomonic placement an ellipse on the sky becomes a polygon whose
// vertices lie on the mapped outline and whose edges stay within the
// tolerance of it.
func TestAnEllipseOnTheSkyIsApproximatedToTheTolerance(t *testing.T) {
	p := skyPlacement()
	for _, tol := range []float64{0, 0.1, 0.001} {
		centre := Point{X: 150.03, Y: 2.05}
		s, err := NewEllipse(centre, 0.02, 0.008, 30)
		require.NoError(t, err)
		px, m, err := ToPixels(s, sky, p, tol)
		require.NoError(t, err)
		want := tol
		if tol == 0 {
			want = DefaultTolerance
		}
		assert.Equal(t, MappingApproximated, m.How)
		assert.Equal(t, want, m.Tolerance)
		vs := px.Vertices()
		assert.Equal(t, len(vs), m.Vertices)
		sn, cs := sincosDeg(30)
		const dense = 20000
		outline := make([]Point, dense)
		for i := range outline {
			u := 2 * math.Pi * float64(i) / dense
			x, y := 0.02*math.Cos(u), 0.008*math.Sin(u)
			lon, lat := skyOfTangent(centre, cs*x-sn*y, sn*x+cs*y)
			col, row, ok := p.Pixel(lon, lat)
			require.True(t, ok)
			outline[i] = Point{X: col, Y: row}
		}
		for _, v := range vs {
			assert.Less(t, distanceToRing(v, outline), 1e-5, "a vertex lies on the outline")
		}
		var worst float64
		for _, q := range outline {
			worst = max(worst, distanceToRing(q, vs))
		}
		t.Logf("tolerance %v: %d vertices, the outline strays %.3g px at most", want, len(vs), worst)
		assert.LessOrEqual(t, worst, want, "tolerance %v with %d vertices", want, len(vs))
	}
}

// A rectangle on the sky has great-circle sides, which the projection keeps
// straight: it becomes the polygon of its corners, exactly; a polygon and a
// line map vertex by vertex.
func TestStraightShapesOnTheSkyMapExactly(t *testing.T) {
	p := skyPlacement()
	centre := Point{X: 149.97, Y: 1.96}
	s, err := NewRectangle(centre, 0.04, 0.02, 20)
	require.NoError(t, err)
	px, m, err := ToPixels(s, sky, p, 0)
	require.NoError(t, err)
	assert.Equal(t, MappingPolygon, m.How)
	vs := px.Vertices()
	require.Len(t, vs, 4)
	for i := range 4 {
		// The middle of each side, in the plane about the centre.
		a, b := s.cornerOffsets()[i], s.cornerOffsets()[(i+1)%4]
		lon, lat := skyOfTangent(centre, (a.X+b.X)/2, (a.Y+b.Y)/2)
		col, row, ok := p.Pixel(lon, lat)
		require.True(t, ok)
		assert.Less(t, segmentDistance(Point{X: col, Y: row}, vs[i], vs[(i+1)%4]), 1e-6, "side %d is straight", i)
	}
	poly, _ := NewPolygon([]Point{{150, 2}, {150.02, 2}, {150.01, 2.03}})
	px, m, err = ToPixels(poly, sky, p, 0)
	require.NoError(t, err)
	assert.Equal(t, MappingExact, m.How)
	col, row, _ := p.Pixel(150.02, 2)
	assert.Equal(t, Point{X: col, Y: row}, px.Vertices()[1])
}

func TestAShapeOffTheVisibleHemisphereIsRefused(t *testing.T) {
	p := skyPlacement()
	far, _ := NewPoint(Point{X: 330, Y: -2})
	_, _, err := ToPixels(far, sky, p, 0)
	assert.Error(t, err)
	e, _ := NewEllipse(Point{X: 330, Y: -2}, 1, 1, 0)
	_, _, err = ToPixels(e, sky, p, 0)
	assert.Error(t, err)
	// Centred where the projection shows it, reaching past 90° from the
	// reference point.
	e, _ = NewEllipse(Point{X: 150 + 80, Y: 2}, 30, 1, 0)
	_, _, err = ToPixels(e, sky, p, 0)
	assert.Error(t, err)
	r, _ := NewRectangle(Point{X: 150 + 80, Y: 2}, 60, 1, 0)
	_, _, err = ToPixels(r, sky, p, 0)
	assert.Error(t, err)
	e, _ = NewEllipse(Point{X: 150, Y: 2}, 0.01, 0.01, 0)
	_, _, err = ToPixels(e, sky, p, -1)
	assert.Error(t, err, "a negative tolerance")
	_, _, err = ToPixels(e, sky, p, 1e-15)
	assert.Error(t, err, "more vertices than the limit")
}

func TestEllipseOfTheSingularValues(t *testing.T) {
	for _, m := range []linear{{2, 0, 0, 1}, {0, -3, 1, 0}, {1, 0, 0, -1}, {1, 0.5, 0, 1}, {-2, 0.3, -0.7, -0.1}} {
		rx, ry, angle := ellipseOf(m)
		// The image of the unit circle is the ellipse found.
		s, c := math.Sincos(angle * math.Pi / 180)
		for i := range 16 {
			u := 2 * math.Pi * float64(i) / 16
			x, y := m.apply(math.Cos(u), math.Sin(u))
			a, b := (c*x+s*y)/rx, (-s*x+c*y)/ry
			assert.InDelta(t, 1, a*a+b*b, 1e-12, "%v", m)
		}
		assert.GreaterOrEqual(t, rx, ry)
	}
}
