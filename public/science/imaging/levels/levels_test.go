package levels

import (
	"context"
	"image"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/science/imaging"
)

func whole(w, h, k int) image.Rectangle {
	lw, lh := Size(w, h, k)
	return image.Rect(0, 0, lw, lh)
}

// naive is the definition, written without any of the package's machinery:
// for one cell of level k, the samples of the plane it covers.
func naive(p imaging.Plane, k, cx, cy int) (mean, lo, hi float64, valid bool) {
	f := 1 << k
	var sum float64
	n := 0
	lo, hi = math.Inf(1), math.Inf(-1)
	for y := cy * f; y < min((cy+1)*f, p.Height()); y++ {
		for x := cx * f; x < min((cx+1)*f, p.Width()); x++ {
			v, ok := p.StoredAt(x, y)
			if !ok {
				continue
			}
			sum += v
			n++
			lo, hi = min(lo, v), max(hi, v)
		}
	}
	if n == 0 {
		return 0, 0, 0, false
	}
	mean = sum / float64(n)
	if p.Kind().IsInteger() {
		mean = math.Floor(mean + 0.5)
	}
	return mean, lo, hi, true
}

func checkLevel(t require.TestingT, p imaging.Plane, k int, mean, lo, hi imaging.Plane, rect image.Rectangle) {
	require.Equal(t, rect.Dx(), mean.Width())
	require.Equal(t, rect.Dy(), mean.Height())
	for cy := rect.Min.Y; cy < rect.Max.Y; cy++ {
		for cx := rect.Min.X; cx < rect.Max.X; cx++ {
			wm, wl, wh, valid := naive(p, k, cx, cy)
			gm, okm := mean.StoredAt(cx-rect.Min.X, cy-rect.Min.Y)
			gl, okl := lo.StoredAt(cx-rect.Min.X, cy-rect.Min.Y)
			gh, okh := hi.StoredAt(cx-rect.Min.X, cy-rect.Min.Y)
			require.Equal(t, valid, okm, "validity of cell (%d,%d) at level %d", cx, cy, k)
			require.Equal(t, valid, okl)
			require.Equal(t, valid, okh)
			if !valid {
				continue
			}
			if p.Kind().IsInteger() {
				require.Equal(t, wm, gm, "mean of cell (%d,%d) at level %d", cx, cy, k)
			} else {
				require.InEpsilon(t, wm+1e-30, gm+1e-30, 1e-5, "mean of cell (%d,%d) at level %d", cx, cy, k)
				require.LessOrEqual(t, gl, gm+math.Abs(gm)*1e-6, "a mean is not below its minimum")
				require.GreaterOrEqual(t, gh, gm-math.Abs(gm)*1e-6, "nor above its maximum")
			}
			require.Equal(t, wl, gl, "minimum of cell (%d,%d) at level %d", cx, cy, k)
			require.Equal(t, wh, gh, "maximum of cell (%d,%d) at level %d", cx, cy, k)
		}
	}
}

func planeOf(t require.TestingT, kind imaging.SampleKindE, w, h int, sample func(i int) float64) imaging.Plane {
	var p imaging.Plane
	var err error
	n := w * h
	switch kind {
	case imaging.SampleKindU8:
		s := make([]uint8, n)
		for i := range s {
			s[i] = uint8(sample(i))
		}
		p, err = imaging.NewPlaneU8(w, h, s)
	case imaging.SampleKindU16:
		s := make([]uint16, n)
		for i := range s {
			s[i] = uint16(sample(i))
		}
		p, err = imaging.NewPlaneU16(w, h, s)
	default:
		s := make([]float32, n)
		for i := range s {
			s[i] = float32(sample(i))
		}
		p, err = imaging.NewPlaneF32(w, h, s)
	}
	require.NoError(t, err)
	return p
}

func grayOf(t require.TestingT, p imaging.Plane) imaging.Image {
	img, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{SourceName: "t"})
	require.NoError(t, err)
	return img
}

// Every level a build makes is, cell for cell, what the definition says —
// for every sample kind, for sizes that do not halve evenly, and with
// invalid samples in a float plane.
func TestBuildIsTheDefinition(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		kind := rapid.SampledFrom(imaging.AllSampleKinds).Draw(t, "kind")
		w := rapid.IntRange(1, 37).Draw(t, "w")
		h := rapid.IntRange(1, 29).Draw(t, "h")
		raw := rapid.SliceOfN(rapid.Uint16(), w*h, w*h).Draw(t, "samples")
		p := planeOf(t, kind, w, h, func(i int) float64 {
			v := float64(raw[i])
			switch kind {
			case imaging.SampleKindU8:
				return float64(raw[i] & 0xff)
			case imaging.SampleKindF32:
				if raw[i]%5 == 0 {
					return math.NaN()
				}
				return v/7 - 3000
			}
			return v
		})
		py, err := Build(context.Background(), grayOf(t, p), Options{MinSide: 1})
		require.NoError(t, err)
		require.Equal(t, CountFor(w, h, Options{MinSide: 1}), py.Count())
		for k := 1; k <= py.Count(); k++ {
			lv, ok := py.Level(k)
			require.True(t, ok)
			require.Equal(t, 1<<k, lv.Factor)
			checkLevel(t, p, k, lv.Mean.Plane(0), lv.Min[0], lv.Max[0], whole(w, h, k))
		}
		if py.Count() > 0 {
			last, _ := py.Level(py.Count())
			require.Equal(t, 1, max(last.Mean.Width(), last.Mean.Height()), "halving ran down to one cell")
		}
	})
}

// A region reduced directly is the same cells a build has.
func TestReduceIsTheDefinitionForAnyRegion(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		kind := rapid.SampledFrom(imaging.AllSampleKinds).Draw(t, "kind")
		w := rapid.IntRange(1, 40).Draw(t, "w")
		h := rapid.IntRange(1, 40).Draw(t, "h")
		p := planeOf(t, kind, w, h, func(i int) float64 { return float64((i*7919 + 13) % 251) })
		k := rapid.IntRange(0, 4).Draw(t, "k")
		lw, lh := Size(w, h, k)
		x0 := rapid.IntRange(0, lw-1).Draw(t, "x0")
		y0 := rapid.IntRange(0, lh-1).Draw(t, "y0")
		rect := image.Rect(x0, y0, rapid.IntRange(x0+1, lw).Draw(t, "x1"), rapid.IntRange(y0+1, lh).Draw(t, "y1"))
		mean, lo, hi, err := Reduce(p, k, rect)
		require.NoError(t, err)
		checkLevel(t, p, k, mean, lo, hi, rect)
	})
}

func TestAnIntegerMeanIsRoundedNotTruncatedAndIsNotAMeanOfMeans(t *testing.T) {
	// 3 × 1: level 1 has two cells, (0+1)/2 = 0.5 → 1 and the lone 2;
	// level 2 has one, (0+1+2)/3 = 1 — a mean of the two level-1 means
	// would be (1+2)/2 = 1.5 → 2.
	p := planeOf(t, imaging.SampleKindU8, 3, 1, func(i int) float64 { return float64(i) })
	py, err := Build(context.Background(), grayOf(t, p), Options{MinSide: 1})
	require.NoError(t, err)
	require.Equal(t, 2, py.Count())
	l1, _ := py.Level(1)
	assert.Equal(t, []uint8{1, 2}, l1.Mean.Plane(0).U8())
	l2, _ := py.Level(2)
	assert.Equal(t, []uint8{1}, l2.Mean.Plane(0).U8())
	assert.Equal(t, []uint8{0}, l2.Min[0].U8())
	assert.Equal(t, []uint8{2}, l2.Max[0].U8())
}

func TestABrightPixelSurvivesInTheMaximum(t *testing.T) {
	p := planeOf(t, imaging.SampleKindU16, 64, 64, func(i int) float64 {
		if i == 40*64+17 {
			return 60000
		}
		return 100
	})
	py, err := Build(context.Background(), grayOf(t, p), Options{MinSide: 4})
	require.NoError(t, err)
	last, ok := py.Level(py.Count())
	require.True(t, ok)
	_, top, _ := last.Max[0].StoredRange()
	assert.Equal(t, 60000.0, top, "the one bright pixel is still the level's maximum")
	_, meanTop, _ := last.Mean.Plane(0).StoredRange()
	assert.Less(t, meanTop, 1000.0, "and is averaged away in its mean")
}

func TestBuildKeepsModelTransformAndStopsWhereAsked(t *testing.T) {
	p := planeOf(t, imaging.SampleKindU16, 2000, 1200, func(i int) float64 { return float64(i % 1000) })
	p, err := p.WithTransform(imaging.ValueTransform{Scale: 0.5, Offset: 10})
	require.NoError(t, err)
	a := planeOf(t, imaging.SampleKindU16, 2000, 1200, func(i int) float64 { return 65535 })
	img, err := imaging.NewImage(imaging.ColorModelGrayAlpha, []imaging.Plane{p, a}, imaging.Provenance{SourceName: "x"})
	require.NoError(t, err)

	var last, total int64
	py, err := Build(context.Background(), img, Options{Progress: func(done, tot int64) { last, total = done, tot }})
	require.NoError(t, err)
	assert.Equal(t, 2, py.Count(), "2000 → 1000 → 500: two halvings bring the longer side to 512 or less")
	assert.Equal(t, total, last, "progress ends at its total")
	assert.Positive(t, total)
	lv, _ := py.Level(2)
	assert.Equal(t, imaging.ColorModelGrayAlpha, lv.Mean.Model())
	assert.Equal(t, 500, lv.Mean.Width())
	assert.Equal(t, 300, lv.Mean.Height())
	assert.Equal(t, imaging.ValueTransform{Scale: 0.5, Offset: 10}, lv.Mean.Plane(0).Transform())
	assert.Equal(t, imaging.ValueTransform{Scale: 0.5, Offset: 10}, lv.Min[0].Transform())
	assert.Equal(t, "x", lv.Mean.Provenance().SourceName)
	_, ok := py.Level(3)
	assert.False(t, ok)
	_, ok = py.Level(0)
	assert.False(t, ok, "level 0 is the image, not a reduction")

	assert.Equal(t, 0, CountFor(512, 300, Options{}))
	assert.Equal(t, 1, CountFor(513, 300, Options{}))
	assert.Equal(t, 8, CountFor(1<<20, 1<<20, Options{}), "bounded by MaxLevels")
	var none *Pyramid
	assert.Zero(t, none.Count())
}

func TestBuildCanBeCancelledAndRefusesNothing(t *testing.T) {
	p := planeOf(t, imaging.SampleKindU8, 1200, 1200, func(i int) float64 { return 1 })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Build(ctx, grayOf(t, p), Options{})
	assert.ErrorIs(t, err, context.Canceled)
	_, err = Build(context.Background(), imaging.Image{}, Options{})
	assert.Error(t, err)
	_, _, _, err = Reduce(p, 1, image.Rect(0, 0, 601, 600))
	assert.Error(t, err, "a region past the level's edge")
	_, _, _, err = Reduce(p, 1, image.Rectangle{})
	assert.Error(t, err)
}

func BenchmarkBuild(b *testing.B) {
	for _, side := range []int{2048, 4096} {
		p := planeOf(b, imaging.SampleKindU16, side, side, func(i int) float64 { return float64(i % 60000) })
		img := grayOf(b, p)
		b.Run(map[int]string{2048: "u16-2048", 4096: "u16-4096"}[side], func(b *testing.B) {
			b.SetBytes(int64(2 * side * side))
			for b.Loop() {
				if _, err := Build(context.Background(), img, Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
