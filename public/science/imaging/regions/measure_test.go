package regions

import (
	"context"
	"image"
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

func pixelRegion(t require.TestingT, id string, s Shape) Region {
	r := Region{Id: id, Frame: PixelFrame(), Shape: s}
	require.NoError(t, r.Validate())
	return r
}

// testShapes are shapes over a w × h image, placed by fractions of its size
// so that each image's interesting parts — an invalid border, a block of
// NaN, a peak — are crossed.
func testShapes(t *testing.T, w, h int) []Shape {
	fw, fh := float64(w), float64(h)
	must := func(s Shape, err error) Shape {
		require.NoError(t, err)
		return s
	}
	return []Shape{
		must(NewEllipse(Point{X: 0.1 * fw, Y: 0.6 * fh}, 0.12*fw, 0.08*fh, 25)),
		must(NewEllipse(Point{X: 0.5*fw + 0.5, Y: 0.5*fh + 0.5}, 3, 3, 0)),
		must(NewRectangle(Point{X: 0.3 * fw, Y: 0.3 * fh}, 0.5*fw, 0.2*fh, -15)),
		must(NewRectangle(Point{X: 2, Y: 2}, 4, 4, 0)),
		must(NewPolygon([]Point{{-3, -3}, {0.7 * fw, 0.1 * fh}, {0.4 * fw, 0.9 * fh}, {0.05 * fw, 0.5 * fh}})),
		must(NewLine(Point{X: -5, Y: 0.2 * fh}, Point{X: fw + 5, Y: 0.8 * fh})),
		must(NewPoint(Point{X: 0.67 * fw, Y: 0.36 * fh})),
	}
}

// requireFiguresAreDirect holds a measurement against the covered samples
// read one by one: counts, range, mean, standard deviation and nearest-rank
// percentiles in values, exact where the kind has codes and within a bin
// where it does not.
func requireFiguresAreDirect(t *testing.T, img imaging.Image, s Shape, m Measurement) {
	cov, err := CoverageOf(s, img.Width(), img.Height())
	require.NoError(t, err)
	assert.Equal(t, cov.Pixels(), m.Pixels)
	assert.Equal(t, cov.Rule(), m.Rule)
	b := cov.Bounds()
	require.Len(t, m.Planes, img.PlaneCount())
	for i, pf := range m.Planes {
		p := img.Plane(i)
		var vals []float64
		invalid := 0
		for row := b.Min.Y; row < b.Max.Y; row++ {
			for col := b.Min.X; col < b.Max.X; col++ {
				if !cov.Contains(col, row) {
					continue
				}
				v, ok := p.StoredAt(col, row)
				if !ok {
					invalid++
					continue
				}
				vals = append(vals, v)
			}
		}
		slices.Sort(vals)
		require.EqualValues(t, len(vals), pf.Valid, "plane %d of %v", i, s.Kind())
		require.EqualValues(t, invalid, pf.Invalid)
		assert.Equal(t, cov.Rule(), pf.Rule)
		if len(vals) == 0 {
			assert.True(t, math.IsNaN(pf.Mean))
			continue
		}
		tr := p.Transform()
		lo, hi := tr.Apply(vals[0]), tr.Apply(vals[len(vals)-1])
		assert.Equal(t, min(lo, hi), pf.Min)
		assert.Equal(t, max(lo, hi), pf.Max)
		var sum float64
		for _, v := range vals {
			sum += v
		}
		mean := sum / float64(len(vals))
		var m2 float64
		for _, v := range vals {
			m2 += (v - mean) * (v - mean)
		}
		scale := math.Max(math.Abs(vals[0]), math.Abs(vals[len(vals)-1])) + 1
		assert.InDelta(t, tr.Apply(mean), pf.Mean, 1e-9*scale*math.Abs(tr.Scale)+1e-9)
		assert.InDelta(t, math.Abs(tr.Scale)*math.Sqrt(m2/float64(len(vals))), pf.StdDev, 1e-9*scale*math.Abs(tr.Scale)+1e-9)
		_, _, codes := p.Kind().Codes()
		assert.Equal(t, codes, pf.Exact)
		for _, pc := range pf.Percentiles {
			rank := int(math.Ceil(pc.P / 100 * float64(len(vals))))
			want := tr.Apply(vals[max(rank-1, 0)])
			if codes {
				assert.Equal(t, want, pc.Value, "p%v", pc.P)
				continue
			}
			assert.InDelta(t, want, pc.Value, 1.000001*pf.BinWidth*math.Abs(tr.Scale)+1e-9, "p%v", pc.P)
		}
	}
}

// Every sample kind, with NaN and declared-invalid samples, measured under
// every shape kind, gives what a direct reading of the covered samples
// gives.
func TestMeasureIsTheDirectReadingForEveryKind(t *testing.T) {
	ctx := context.Background()
	seen := map[imaging.SampleKindE]bool{}
	for _, name := range []string{samples.NameSmall, samples.NameColour, samples.NameFloat, samples.NameScaled,
		samples.NameElevation, samples.NameDouble, samples.NameCounts} {
		img, err := samples.ByName(name)
		require.NoError(t, err)
		seen[img.Kind()] = true
		for i, s := range testShapes(t, img.Width(), img.Height()) {
			m, err := Measure(ctx, ImageSource(img), pixelRegion(t, "r", s), MeasureOptions{Percentiles: []float64{0, 1, 13, 50, 99, 100}})
			require.NoError(t, err, "%s shape %d", name, i)
			assert.Equal(t, MethodId, m.Method)
			assert.Zero(t, m.Level)
			assert.Equal(t, MappingIdentity, m.Mapping.How)
			requireFiguresAreDirect(t, img, s, m)
		}
	}
	for _, k := range imaging.AllSampleKinds {
		assert.True(t, seen[k], "kind %s measured", k)
	}
}

// The area figures are Σ fraction·value over the valid samples the shape
// covers in part, in values; a pixel-aligned rectangle's equal its
// figures by centre.
func TestAreaFiguresAreTheWeightedSums(t *testing.T) {
	ctx := context.Background()
	img, err := samples.ByName(samples.NameElevation)
	require.NoError(t, err)
	e, _ := NewEllipse(Point{X: 6.3, Y: 50.2}, 5.5, 3.25, 40)
	m, err := Measure(ctx, ImageSource(img), pixelRegion(t, "e", e), MeasureOptions{Area: true})
	require.NoError(t, err)
	a := m.Planes[0].Area
	require.NotNil(t, a)
	assert.Equal(t, CoverageRuleArea, a.Rule)
	seq, _ := AreaFractions(e, img.Width(), img.Height())
	var sum, area, inv float64
	for p, f := range seq {
		v, ok := img.Plane(0).ValueAt(p.X, p.Y)
		if !ok {
			inv += f
			continue
		}
		sum += f * v
		area += f
	}
	assert.InDelta(t, sum, a.Sum, 1e-9*math.Abs(sum))
	assert.InDelta(t, area, a.Area, 1e-12)
	assert.InDelta(t, inv, a.InvalidArea, 1e-12)
	assert.Greater(t, a.InvalidArea, 0.0, "the ellipse reaches the invalid border")
	assert.InDelta(t, math.Pi*5.5*3.25, a.Area+a.InvalidArea, 1e-9)
	assert.InDelta(t, a.Sum/a.Area, a.Mean, 1e-12)

	r, _ := NewRectangle(Point{X: 30, Y: 40}, 20, 10, 0)
	m, err = Measure(ctx, ImageSource(img), pixelRegion(t, "r", r), MeasureOptions{Area: true})
	require.NoError(t, err)
	pf := m.Planes[0]
	assert.EqualValues(t, 200, pf.Valid)
	assert.Equal(t, 200.0, pf.Area.Area)
	assert.InDelta(t, pf.Mean, pf.Area.Mean, 1e-9)

	l, _ := NewLine(Point{X: 1, Y: 1}, Point{X: 5, Y: 9})
	m, err = Measure(ctx, ImageSource(img), pixelRegion(t, "l", l), MeasureOptions{Area: true})
	require.NoError(t, err)
	assert.Nil(t, m.Planes[0].Area, "a line covers no area")
}

// A measurement from a tiled store is the measurement from the image in
// memory, figure for figure.
func TestAStoreMeasuresAsTheImageInMemory(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{samples.NameField, samples.NameCounts, samples.NameColour} {
		img, err := samples.ByName(name)
		require.NoError(t, err)
		store, err := tiles.FromImage(ctx, img, tiles.Options{})
		require.NoError(t, err)
		w, h := img.Width(), img.Height()
		for i, s := range []Shape{
			mustShape(NewEllipse(Point{X: 0.6 * float64(w), Y: 0.33 * float64(h)}, 0.3*float64(w), 0.2*float64(h), 12)),
			mustShape(NewPolygon([]Point{{500, 400}, {530, 620}, {700, 600}, {250, 700}})),
			mustShape(NewLine(Point{X: 3, Y: 4}, Point{X: float64(w) - 7, Y: float64(h) - 2})),
		} {
			r := pixelRegion(t, "r", s)
			mem, err := Measure(ctx, ImageSource(img), r, MeasureOptions{Area: true})
			require.NoError(t, err)
			got, err := Measure(ctx, StoreSource(store), r, MeasureOptions{Area: true})
			require.NoError(t, err)
			if mem.Pixels == 0 {
				continue
			}
			assert.Equal(t, mem, got, "%s shape %d", name, i)
		}
	}
}

func mustShape(s Shape, err error) Shape {
	if err != nil {
		panic(err)
	}
	return s
}

// A region in the stack's reference system measures one image of the stack
// as the same rectangle in pixels does, and its measurement says how it got
// there.
func TestARegionInAReferenceSystemMeasuresTheStack(t *testing.T) {
	ctx := context.Background()
	stack, err := samples.StackByName(samples.StackBands)
	require.NoError(t, err)
	img, err := stack.Image([]int{1, 0}, []int{3})
	require.NoError(t, err)
	// Columns 20 … 60, rows 30 … 50: E = 500 000 + 10·col, N = 5 200 000 − 10·row.
	world, _ := NewRectangle(Point{X: 500_000 + 400, Y: 5_200_000 - 400}, 400, 200, 0)
	r := Region{Id: "field", Label: "a field", Origin: "test", Frame: Frame{Kind: FrameKindReferenceSystem, Code: "EPSG:32632"}, Shape: world}
	m, err := Measure(ctx, ImageSource(img), r, MeasureOptions{})
	require.NoError(t, err)
	assert.Equal(t, MappingExact, m.Mapping.How)
	assert.Equal(t, r.Frame, m.Mapping.From)
	assert.Equal(t, ShapeKindRectangle, m.Shape.Kind())
	px, _ := NewRectangle(Point{X: 40, Y: 40}, 40, 20, 0)
	want, err := Measure(ctx, ImageSource(img), pixelRegion(t, "px", px), MeasureOptions{})
	require.NoError(t, err)
	assert.Equal(t, want.Planes, m.Planes)
	assert.EqualValues(t, 800, m.Pixels)
	assert.Equal(t, []string{"B8"}, []string{m.Planes[0].Label})

	other := r
	other.Frame.Code = "EPSG:2056"
	_, err = Measure(ctx, ImageSource(img), other, MeasureOptions{})
	assert.Error(t, err)
}

func TestMeasureRefusals(t *testing.T) {
	ctx := context.Background()
	img, err := samples.ByName(samples.NameField)
	require.NoError(t, err)
	store, err := tiles.FromImage(ctx, img, tiles.Options{})
	require.NoError(t, err)
	big, _ := NewRectangle(Point{X: 700, Y: 500}, 1400, 1000, 0)
	_, err = Measure(ctx, StoreSource(store), pixelRegion(t, "big", big), MeasureOptions{MaxPixels: 1000})
	require.Error(t, err)
	assert.EqualValues(t, 1000, ebtest.Fields(t, err)["limit"])
	_, err = Measure(ctx, ImageSource(img), pixelRegion(t, "big", big), MeasureOptions{MaxPixels: 1000})
	assert.NoError(t, err, "an image in memory copies nothing")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = Measure(cancelled, ImageSource(img), pixelRegion(t, "big", big), MeasureOptions{})
	assert.ErrorIs(t, err, context.Canceled)
	_, err = Measure(ctx, ImageSource(img), pixelRegion(t, "big", big), MeasureOptions{Percentiles: []float64{101}})
	assert.Error(t, err)
	_, err = Measure(ctx, ImageSource(img), Region{Id: "x", Shape: big}, MeasureOptions{})
	assert.Error(t, err, "a region without a frame")

	// A shape that covers no pixel centre is measured: nothing counted.
	tiny, _ := NewEllipse(Point{X: 10, Y: 10}, 0.3, 0.3, 0)
	m, err := Measure(ctx, StoreSource(store), pixelRegion(t, "tiny", tiny), MeasureOptions{Area: true})
	require.NoError(t, err)
	assert.Zero(t, m.Pixels)
	assert.Zero(t, m.Planes[0].Valid)
	assert.True(t, math.IsNaN(m.Planes[0].Min))
	assert.InDelta(t, math.Pi*0.09, m.Planes[0].Area.Area, 1e-12, "by area it covers its own")
	outside, _ := NewPoint(Point{X: -1, Y: 3})
	m, err = Measure(ctx, StoreSource(store), pixelRegion(t, "out", outside), MeasureOptions{})
	require.NoError(t, err)
	assert.Zero(t, m.Pixels)
}

// The profile of a line holds, for each pixel it passes through, the stored
// sample of every plane — invalid ones flagged — and lengths that add up to
// the length inside the image.
func TestTheProfileIsTheStoredSamples(t *testing.T) {
	ctx := context.Background()
	img, err := samples.ByName(samples.NameElevation)
	require.NoError(t, err)
	a, b := Point{X: -20.5, Y: 3.25}, Point{X: 120, Y: 77.75}
	l, _ := NewLine(a, b)
	p, err := Profile(ctx, ImageSource(img), pixelRegion(t, "transect", l), ProfileOptions{})
	require.NoError(t, err)
	assert.Equal(t, MethodId, p.Method)
	assert.Equal(t, CoverageRuleLine, p.Rule)
	assert.InDelta(t, clippedLength(a, b, img.Width(), img.Height()), p.Length, 1e-9)
	_, ok := p.FrameUnit()
	assert.False(t, ok, "no placement")
	invalid := 0
	prev := math.Inf(-1)
	for i, s := range p.All() {
		st, v, valid := p.Sample(0, i)
		want, wantValid := img.Plane(0).StoredAt(s.Col, s.Row)
		require.Equal(t, wantValid, valid)
		if !valid {
			invalid++
			continue
		}
		assert.Equal(t, want, st, "a profile value is a stored sample")
		assert.Equal(t, img.Plane(0).Transform().Apply(want), v)
		assert.Greater(t, s.Distance, prev)
		prev = s.Distance
		assert.True(t, math.IsNaN(s.FrameDistance))
	}
	assert.Positive(t, invalid, "the line crosses the invalid border")

	store, err := tiles.FromImage(ctx, img, tiles.Options{})
	require.NoError(t, err)
	q, err := Profile(ctx, StoreSource(store), pixelRegion(t, "transect", l), ProfileOptions{MaxPixels: 64})
	require.NoError(t, err)
	assert.Equal(t, p, q, "read in bands of at most 64 pixels from the store")

	e, _ := NewEllipse(Point{X: 3, Y: 3}, 1, 1, 0)
	_, err = Profile(ctx, ImageSource(img), pixelRegion(t, "e", e), ProfileOptions{})
	assert.Error(t, err)
}

// Where the image's placement is linear with one unit, a profile's
// distances are also in that unit.
func TestAProfileOnAPlacedImageHasFrameDistances(t *testing.T) {
	ctx := context.Background()
	stack, err := samples.StackByName(samples.StackBands)
	require.NoError(t, err)
	img, err := stack.Image([]int{0, 0}, []int{0, 1, 2})
	require.NoError(t, err)
	l, _ := NewLine(Point{X: 500_000 + 15, Y: 5_200_000 - 15}, Point{X: 500_000 + 1015, Y: 5_200_000 - 515})
	r := Region{Id: "t", Frame: Frame{Kind: FrameKindReferenceSystem, Code: "EPSG:32632"}, Shape: l}
	p, err := Profile(ctx, ImageSource(img), r, ProfileOptions{})
	require.NoError(t, err)
	unit, ok := p.FrameUnit()
	require.True(t, ok)
	assert.Equal(t, "m", unit)
	assert.Equal(t, []string{"B2", "B3", "B4"}, p.Labels)
	assert.InDelta(t, math.Hypot(100, 50), p.Length, 1e-9)
	first, last := p.Step(0), p.Step(p.Len()-1)
	assert.Equal(t, image.Pt(1, 1), image.Pt(first.Col, first.Row))
	assert.Equal(t, image.Pt(101, 51), image.Pt(last.Col, last.Row))
	for _, s := range p.All() {
		assert.InDelta(t, 10*s.Distance, s.FrameDistance, 1e-9)
	}
	for i := range p.Len() {
		_, _, valid := p.Sample(2, i)
		_, want := img.Plane(2).StoredAt(p.Step(i).Col, p.Step(i).Row)
		assert.Equal(t, want, valid)
	}
}
