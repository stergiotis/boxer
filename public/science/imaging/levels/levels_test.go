package levels

import (
	"context"
	"image"
	"math"
	"math/big"
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
// for one cell of level k, the samples of the plane it covers. An integer
// mean is the exact mean rounded to nearest, halves up, and moved off the
// declared invalid value to the neighbour on the side of the exact mean (up
// when it is the declared value); a float mean that equals the declared
// value is moved to the neighbouring float the same way.
func naive(p imaging.Plane, k, cx, cy int) (mean, lo, hi float64, valid bool) {
	f := 1 << k
	var sum float64
	isum := new(big.Int)
	n := 0
	lo, hi = math.Inf(1), math.Inf(-1)
	for y := cy * f; y < min((cy+1)*f, p.Height()); y++ {
		for x := cx * f; x < min((cx+1)*f, p.Width()); x++ {
			v, ok := p.StoredAt(x, y)
			if !ok {
				continue
			}
			sum += v
			isum.Add(isum, big.NewInt(int64(v)))
			n++
			lo, hi = min(lo, v), max(hi, v)
		}
	}
	if n == 0 {
		return 0, 0, 0, false
	}
	inv, hasInv := p.Invalid()
	if p.Kind().IsInteger() {
		exact := new(big.Rat).SetFrac(isum, big.NewInt(int64(n)))
		half := new(big.Rat).Add(exact, big.NewRat(1, 2))
		q := new(big.Int).Div(half.Num(), half.Denom()) // Euclidean: the floor for a positive divisor
		mean = float64(q.Int64())
		if hasInv && mean == inv {
			if exact.Cmp(new(big.Rat).SetInt64(int64(inv))) < 0 {
				mean--
			} else {
				mean++
			}
		}
		return mean, lo, hi, true
	}
	mean = sum / float64(n)
	return mean, lo, hi, true
}

func checkLevel(t require.TestingT, p imaging.Plane, k int, mean, lo, hi imaging.Plane, rect image.Rectangle) {
	require.Equal(t, rect.Dx(), mean.Width())
	require.Equal(t, rect.Dy(), mean.Height())
	inv, hasInv := p.Invalid()
	for _, q := range []imaging.Plane{mean, lo, hi} {
		require.Equal(t, p.Kind(), q.Kind(), "a level is in the plane's own kind")
		gi, gok := q.Invalid()
		require.Equal(t, hasInv, gok, "a level declares what the plane declares")
		require.Equal(t, inv, gi)
		require.Equal(t, p.Transform(), q.Transform())
	}
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
	case imaging.SampleKindI16:
		s := make([]int16, n)
		for i := range s {
			s[i] = int16(sample(i))
		}
		p, err = imaging.NewPlaneI16(w, h, s)
	case imaging.SampleKindI32:
		s := make([]int32, n)
		for i := range s {
			s[i] = int32(sample(i))
		}
		p, err = imaging.NewPlaneI32(w, h, s)
	case imaging.SampleKindF32:
		s := make([]float32, n)
		for i := range s {
			s[i] = float32(sample(i))
		}
		p, err = imaging.NewPlaneF32(w, h, s)
	case imaging.SampleKindF64:
		s := make([]float64, n)
		for i := range s {
			s[i] = sample(i)
		}
		p, err = imaging.NewPlaneF64(w, h, s)
	default:
		require.Fail(t, "no plane of this kind", "%s", kind)
	}
	require.NoError(t, err)
	return p
}

// spread maps a raw 16-bit draw onto a kind's range: negative values for the
// signed kinds, the whole 32-bit range for i32, NaN for a fifth of the float
// samples.
func spread(kind imaging.SampleKindE, raw uint16) float64 {
	v := float64(raw)
	switch kind {
	case imaging.SampleKindU8:
		return float64(raw & 0xff)
	case imaging.SampleKindI16:
		return v - 32768
	case imaging.SampleKindI32:
		return v*65537 - 1<<31
	case imaging.SampleKindF32, imaging.SampleKindF64:
		if raw%5 == 0 {
			return math.NaN()
		}
		return v/7 - 3000
	}
	return v
}

// maybeInvalid declares, when the draw says so, a stored value that occurs
// in the plane invalid.
func maybeInvalid(t *rapid.T, p imaging.Plane) imaging.Plane {
	if !rapid.Bool().Draw(t, "declare invalid") {
		return p
	}
	at := rapid.IntRange(0, p.Width()*p.Height()-1).Draw(t, "invalid at")
	v, ok := p.StoredAt(at%p.Width(), at/p.Width())
	if !ok {
		return p
	}
	q, err := p.WithInvalid(v)
	require.NoError(t, err)
	return q
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
		p := maybeInvalid(t, planeOf(t, kind, w, h, func(i int) float64 { return spread(kind, raw[i]) }))
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
		p := maybeInvalid(t, planeOf(t, kind, w, h, func(i int) float64 {
			v := float64((i*7919 + 13) % 251)
			if kind.IsSigned() {
				v -= 125
			}
			return v
		}))
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

// A mean that would round onto the declared value moves to the neighbouring
// code on the side of the exact mean, and up when the exact mean is the
// declared value; for a float kind, to the neighbouring float.
func TestAMeanThatWouldLandOnTheDeclaredValueMovesToItsNeighbour(t *testing.T) {
	for _, tc := range []struct {
		kind    imaging.SampleKindE
		samples []float64
		invalid float64
		want    float64
		why     string
	}{
		{imaging.SampleKindI16, []float64{-3, 0, 0, 0}, -1, 0, "−0.75 rounds to −1; the exact mean is above it"},
		{imaging.SampleKindI16, []float64{-2, -1, 0, 1}, 0, -1, "−0.5 rounds up to 0; the exact mean is below it"},
		{imaging.SampleKindU8, []float64{99, 99, 101, 101}, 100, 101, "the exact mean is the declared value: up"},
		{imaging.SampleKindU16, []float64{9, 11, 11, 11}, 10, 11, "10.5 rounds to 11, which is not declared: no move"},
		{imaging.SampleKindI32, []float64{-1 << 31, 1<<31 - 1, 0, 0}, 0, -1, "−0.25 rounds to 0; the exact mean is below it"},
		{imaging.SampleKindF64, []float64{-1, 1, 3, -3}, 0, math.Nextafter(0, 1), "a float mean on the declared value: the next float up"},
		{imaging.SampleKindF32, []float64{-1, 1, -2, 2}, 0, float64(math.Nextafter32(0, 1)), "the next 32-bit float up"},
	} {
		p := planeOf(t, tc.kind, 2, 2, func(i int) float64 { return tc.samples[i] })
		p, err := p.WithInvalid(tc.invalid)
		require.NoError(t, err)
		mean, _, _, err := Reduce(p, 1, image.Rect(0, 0, 1, 1))
		require.NoError(t, err)
		got, ok := mean.StoredAt(0, 0)
		require.True(t, ok, "%s: the cell is valid", tc.why)
		wm, _, _, _ := naive(p, 1, 0, 0)
		assert.Equal(t, tc.want, got, tc.why)
		if tc.kind.IsInteger() {
			assert.Equal(t, wm, got, "%s: the test's own definition agrees", tc.why)
		}
	}
}

// A cell all of whose samples are declared invalid is invalid at every
// level, and the cells around it are means of their valid samples only.
func TestACellOfInvalidSamplesIsInvalid(t *testing.T) {
	p := planeOf(t, imaging.SampleKindI16, 4, 2, func(i int) float64 {
		return []float64{-9999, -9999, 5, 6, -9999, -9999, -9999, 8}[i]
	})
	p, err := p.WithInvalid(-9999)
	require.NoError(t, err)
	mean, lo, hi, err := Reduce(p, 1, image.Rect(0, 0, 2, 1))
	require.NoError(t, err)
	_, ok := mean.StoredAt(0, 0)
	assert.False(t, ok, "four invalid samples make an invalid cell")
	_, ok = lo.StoredAt(0, 0)
	assert.False(t, ok)
	m, _ := mean.StoredAt(1, 0)
	assert.Equal(t, 6.0, m, "(5 + 6 + 8) / 3 = 6.33, rounded")
	l, _ := lo.StoredAt(1, 0)
	h, _ := hi.StoredAt(1, 0)
	assert.Equal(t, [2]float64{5, 8}, [2]float64{l, h})

	py, err := Build(context.Background(), grayOf(t, p), Options{MinSide: 1})
	require.NoError(t, err)
	lv, _ := py.Level(2)
	m, ok = lv.Mean.Plane(0).StoredAt(0, 0)
	assert.True(t, ok)
	assert.Equal(t, 6.0, m, "a mean of the valid samples, not of the level below's cells")
}

// Sums of 32-bit samples neither wrap nor lose a unit: a cell of extreme
// values has the exact rounded mean.
func TestThirtyTwoBitSumsDoNotWrap(t *testing.T) {
	p := planeOf(t, imaging.SampleKindI32, 4, 4, func(i int) float64 {
		if i%2 == 0 {
			return math.MaxInt32
		}
		return math.MaxInt32 - 1
	})
	py, err := Build(context.Background(), grayOf(t, p), Options{MinSide: 1})
	require.NoError(t, err)
	lv, _ := py.Level(2)
	m, _ := lv.Mean.Plane(0).StoredAt(0, 0)
	assert.Equal(t, float64(math.MaxInt32), m, "2147483646.5 rounds up")
	lo, _ := lv.Min[0].StoredAt(0, 0)
	assert.Equal(t, float64(math.MaxInt32-1), lo)

	q := planeOf(t, imaging.SampleKindI32, 2, 1, func(i int) float64 { return math.MinInt32 })
	mean, _, _, err := Reduce(q, 1, image.Rect(0, 0, 1, 1))
	require.NoError(t, err)
	m, _ = mean.StoredAt(0, 0)
	assert.Equal(t, float64(math.MinInt32), m)
}
