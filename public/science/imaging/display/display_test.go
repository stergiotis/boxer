package display

import (
	"image"
	"math"
	"testing"

	"encoding/json/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/science/imaging"
)

func whole(p imaging.Plane) image.Rectangle { return image.Rect(0, 0, p.Width(), p.Height()) }

func grayOf(t require.TestingT, p imaging.Plane) imaging.Image {
	img, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{})
	require.NoError(t, err)
	return img
}

// direct evaluates the package documentation's formula for one value, with
// no table: the reference the kernels are held to.
func direct(r Recipe, pal *Palette, value float64) uint32 {
	x := (value - r.Limits.Lo) / (r.Limits.Hi - r.Limits.Lo)
	if r.FlagClipped && x < 0 {
		return pal.Under
	}
	if r.FlagClipped && x > 1 {
		return pal.Over
	}
	x = min(max(x, 0), 1)
	param := r.Param
	if param == 0 {
		param = r.Transfer.DefaultParam()
	}
	var y float64
	switch r.Transfer {
	case TransferLinear:
		y = x
	case TransferLog:
		y = math.Log(param*x+1) / math.Log(param+1)
	case TransferSqrt:
		y = math.Sqrt(x)
	case TransferAsinh:
		y = math.Asinh(x/param) / math.Asinh(1/param)
	case TransferGamma:
		y = math.Pow(x, param)
	}
	if r.Invert {
		y = 1 - y
	}
	return pal.Colors[int(math.Round(y*255))]
}

func level(c uint32) int { return int(c >> 24) }

func TestAnEightBitPlaneOverItsNominalRangeIsItsStoredPixels(t *testing.T) {
	samples := make([]uint8, 256)
	for i := range samples {
		samples[i] = uint8(i)
	}
	p, err := imaging.NewPlaneU8(16, 16, samples)
	require.NoError(t, err)
	limits, ok := NominalLimits(imaging.SampleKindU8, p.Transform())
	require.True(t, ok)
	m, err := CompilePlane(Recipe{Limits: limits}, p.Kind(), p.Transform(), nil)
	require.NoError(t, err)
	dst := make([]uint32, 256)
	require.NoError(t, m.Map(dst, p, imaging.Plane{}, whole(p)))
	for i, c := range dst {
		v := uint32(i)
		assert.Equal(t, v<<24|v<<16|v<<8|0xff, c)
	}
}

func TestEveryTransferFunctionRunsFromZeroToOneWithoutTurning(t *testing.T) {
	for _, tr := range AllTransfers {
		assert.InDelta(t, 0, tr.Apply(0, 0), 1e-12, tr.String())
		assert.InDelta(t, 1, tr.Apply(1, 0), 1e-12, tr.String())
		prev := -1.0
		for i := 0; i <= 1000; i++ {
			y := tr.Apply(float64(i)/1000, 0)
			assert.GreaterOrEqual(t, y, prev, "%s at %d", tr, i)
			prev = y
		}
	}
	assert.InDelta(t, 0.25, TransferGamma.Apply(0.5, 2), 1e-12)
}

func TestIntegerKernelsMatchTheDirectEvaluation(t *testing.T) {
	pal := GrayPalette()
	pal.Under, pal.Over = 0x0000ffff, 0xff0000ff
	for _, kind := range []imaging.SampleKindE{imaging.SampleKindU8, imaging.SampleKindU16} {
		top, _ := kind.NominalMax()
		n := int(top) + 1
		var p imaging.Plane
		var err error
		if kind == imaging.SampleKindU8 {
			s := make([]uint8, n)
			for i := range s {
				s[i] = uint8(i)
			}
			p, err = imaging.NewPlaneU8(n, 1, s)
		} else {
			s := make([]uint16, n)
			for i := range s {
				s[i] = uint16(i)
			}
			p, err = imaging.NewPlaneU16(n, 1, s)
		}
		require.NoError(t, err)
		p, err = p.WithTransform(imaging.ValueTransform{Scale: 0.5, Offset: -3})
		require.NoError(t, err)
		lo, hi := p.Transform().Apply(top*0.1), p.Transform().Apply(top*0.7)
		for _, tr := range AllTransfers {
			for _, flags := range [][2]bool{{false, false}, {true, false}, {false, true}} {
				r := Recipe{Limits: Limits{Lo: lo, Hi: hi}, Transfer: tr, Invert: flags[0], FlagClipped: flags[1]}
				m, err := CompilePlane(r, kind, p.Transform(), pal)
				require.NoError(t, err)
				dst := make([]uint32, n)
				require.NoError(t, m.Map(dst, p, imaging.Plane{}, whole(p)))
				for s, c := range dst {
					if want := direct(r, pal, p.Transform().Apply(float64(s))); want != c {
						t.Fatalf("%s %s invert=%v flag=%v sample %d: %#x, direct %#x", kind, tr, flags[0], flags[1], s, c, want)
					}
				}
			}
		}
	}
}

func TestFloatKernelMatchesTheDirectEvaluationWithinOneLevel(t *testing.T) {
	pal := GrayPalette()
	pal.Invalid, pal.Under, pal.Over = 0x00ff0080, 0x0000ffff, 0xff0000ff
	n := 20_000
	s := make([]float32, n)
	for i := range s {
		s[i] = float32(i)/float32(n-1)*14 - 2 // −2 … 12, so both ends clip
	}
	s[7], s[n-9] = float32(math.NaN()), float32(math.NaN())
	p, err := imaging.NewPlaneF32(n, 1, s)
	require.NoError(t, err)
	for _, scale := range []float64{1, -2} {
		q, err := p.WithTransform(imaging.ValueTransform{Scale: scale, Offset: 5})
		require.NoError(t, err)
		lo, hi := q.Transform().Apply(1), q.Transform().Apply(9)
		lo, hi = min(lo, hi), max(lo, hi)
		for _, tr := range AllTransfers {
			for _, flags := range [][2]bool{{false, false}, {true, false}, {false, true}} {
				r := Recipe{Limits: Limits{Lo: lo, Hi: hi}, Transfer: tr, Invert: flags[0], FlagClipped: flags[1]}
				m, err := CompilePlane(r, q.Kind(), q.Transform(), pal)
				require.NoError(t, err)
				dst := make([]uint32, n)
				require.NoError(t, m.Map(dst, q, imaging.Plane{}, whole(q)))
				for i, c := range dst {
					if s[i] != s[i] {
						require.Equal(t, pal.Invalid, c, "NaN at %d", i)
						continue
					}
					want := direct(r, pal, q.Transform().Apply(float64(s[i])))
					if want == pal.Under || want == pal.Over || c == pal.Under || c == pal.Over {
						// At the very limit the table and the formula may
						// fall on different sides of it.
						if want != c {
							x := (q.Transform().Apply(float64(s[i])) - lo) / (hi - lo)
							require.True(t, math.Abs(x) < 1e-4 || math.Abs(x-1) < 1e-4, "scale %v %s sample %d: %#x, direct %#x", scale, tr, i, c, want)
						}
						continue
					}
					if d := level(c) - level(want); d < -1 || d > 1 {
						t.Fatalf("scale %v %s invert=%v sample %d (%v): level %d, direct %d", scale, tr, flags[0], i, s[i], level(c), level(want))
					}
				}
			}
		}
	}
}

func TestAColourImageOverItsNominalRangeIsItsStoredPixels(t *testing.T) {
	mk := func(v ...uint8) imaging.Plane {
		p, err := imaging.NewPlaneU8(2, 2, v)
		require.NoError(t, err)
		return p
	}
	r, g, b := mk(0, 10, 200, 255), mk(1, 20, 100, 254), mk(2, 30, 50, 253)
	a := mk(255, 128, 1, 0)
	img, err := imaging.NewImage(imaging.ColorModelRGBA, []imaging.Plane{r, g, b, a}, imaging.Provenance{})
	require.NoError(t, err)
	recipe := DefaultRecipe(img)
	assert.Equal(t, Limits{Lo: 0, Hi: 255, Source: LimitsSourceNominal}, recipe.Limits)
	m, err := CompileColor(recipe, img.Kind(), img.Plane(0).Transform())
	require.NoError(t, err)
	dst := make([]uint32, 4)
	require.NoError(t, m.Map(dst, img, image.Rect(0, 0, 2, 2)))
	assert.Equal(t, []uint32{0x000102ff, 0x0a141e80, 0xc8643201, 0xfffefd00}, dst,
		"stored red, green, blue and straight alpha, nothing multiplied")

	rgb, err := imaging.NewImage(imaging.ColorModelRGB, []imaging.Plane{r, g, b}, imaging.Provenance{})
	require.NoError(t, err)
	require.NoError(t, m.Map(dst, rgb, image.Rect(0, 0, 2, 2)))
	assert.Equal(t, uint32(0x0a141eff), dst[1], "no alpha plane is opaque")
}

func TestSixteenBitColourIsScaledByItsNominalRangeAndStretchedTogether(t *testing.T) {
	mk := func(v ...uint16) imaging.Plane {
		p, err := imaging.NewPlaneU16(3, 1, v)
		require.NoError(t, err)
		return p
	}
	img, err := imaging.NewImage(imaging.ColorModelRGBA,
		[]imaging.Plane{mk(0, 32768, 65535), mk(257, 1000, 4000), mk(65535, 128, 2000), mk(65535, 32768, 127)}, imaging.Provenance{})
	require.NoError(t, err)
	m, err := CompileColor(DefaultRecipe(img), img.Kind(), imaging.IdentityTransform)
	require.NoError(t, err)
	dst := make([]uint32, 3)
	require.NoError(t, m.Map(dst, img, image.Rect(0, 0, 3, 1)))
	to8 := func(v uint16) uint32 { return uint32(math.Round(float64(v) / 65535 * 255)) }
	assert.Equal(t, to8(0)<<24|to8(257)<<16|to8(65535)<<8|0xff, dst[0])
	assert.Equal(t, to8(32768)<<24|to8(1000)<<16|to8(128)<<8|to8(32768), dst[1])
	assert.Equal(t, uint32(0), dst[2]&0xff, "alpha 127 of 65535 rounds to 0")

	// One recipe for all three colour planes: a narrower range brightens
	// each by the same rule, and alpha is not stretched.
	stretched, err := CompileColor(Recipe{Limits: Limits{Lo: 0, Hi: 4000}}, img.Kind(), imaging.IdentityTransform)
	require.NoError(t, err)
	require.NoError(t, stretched.Map(dst, img, image.Rect(0, 0, 3, 1)))
	assert.Equal(t, uint32(255)<<24|uint32(64)<<16|uint32(8)<<8|to8(32768), dst[1])
}

func TestGrayWithAlphaKeepsItsAlphaAndInvalidStaysTransparent(t *testing.T) {
	g, _ := imaging.NewPlaneU8(3, 1, []uint8{0, 100, 255})
	a, _ := imaging.NewPlaneU8(3, 1, []uint8{255, 7, 0})
	limits, _ := NominalLimits(g.Kind(), g.Transform())
	m, err := CompilePlane(Recipe{Limits: limits}, g.Kind(), g.Transform(), nil)
	require.NoError(t, err)
	dst := make([]uint32, 3)
	require.NoError(t, m.Map(dst, g, a, whole(g)))
	assert.Equal(t, []uint32{0x000000ff, 0x64646407, 0xffffff00}, dst)

	wrong, _ := imaging.NewPlaneU8(2, 1, []uint8{1, 2})
	assert.Error(t, m.Map(dst, g, wrong, whole(g)))
}

func TestMappingARegionTakesTheRightPixels(t *testing.T) {
	s := make([]uint8, 6*4)
	for i := range s {
		s[i] = uint8(i * 10)
	}
	p, _ := imaging.NewPlaneU8(6, 4, s)
	limits, _ := NominalLimits(p.Kind(), p.Transform())
	m, err := CompilePlane(Recipe{Limits: limits}, p.Kind(), p.Transform(), nil)
	require.NoError(t, err)
	rect := image.Rect(2, 1, 5, 3)
	dst := make([]uint32, rect.Dx()*rect.Dy())
	require.NoError(t, m.Map(dst, p, imaging.Plane{}, rect))
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			assert.Equal(t, int(s[y*6+x]), level(dst[(y-rect.Min.Y)*rect.Dx()+(x-rect.Min.X)]), "(%d,%d)", x, y)
		}
	}
	assert.Error(t, m.Map(dst, p, imaging.Plane{}, image.Rect(4, 1, 7, 3)), "outside the plane")
	assert.Error(t, m.Map(dst[:5], p, imaging.Plane{}, rect), "destination of the wrong size")
	assert.Error(t, m.Map(dst, p, imaging.Plane{}, image.Rectangle{}), "empty region")
	wide, _ := imaging.NewPlaneU16(6, 4, make([]uint16, 24))
	assert.Error(t, m.Map(dst, wide, imaging.Plane{}, rect), "another sample kind")
}

func TestARecipeThatCannotBeShownIsRefused(t *testing.T) {
	bad := []Recipe{
		{},
		{Limits: Limits{Lo: 5, Hi: 5}},
		{Limits: Limits{Lo: 5, Hi: 4}},
		{Limits: Limits{Lo: math.NaN(), Hi: 4}},
		{Limits: Limits{Lo: 0, Hi: math.Inf(1)}},
		{Limits: Limits{Lo: 0, Hi: 1}, Transfer: TransferE(99)},
		{Limits: Limits{Lo: 0, Hi: 1}, Param: -1},
		{Limits: Limits{Lo: 0, Hi: 1}, Param: math.NaN()},
	}
	for i, r := range bad {
		assert.Error(t, r.Validate(), "recipe %d", i)
		_, err := CompilePlane(r, imaging.SampleKindU8, imaging.IdentityTransform, nil)
		assert.Error(t, err, "recipe %d", i)
		_, err = CompileColor(r, imaging.SampleKindU8, imaging.IdentityTransform)
		assert.Error(t, err, "recipe %d", i)
	}
	ok := Recipe{Limits: Limits{Lo: 0, Hi: 1}}
	_, err := CompileColor(ok, imaging.SampleKindF32, imaging.IdentityTransform)
	assert.Error(t, err, "float colour is not mapped")
	_, err = CompilePlane(ok, imaging.SampleKindE(0), imaging.IdentityTransform, nil)
	assert.Error(t, err)
}

func TestDefaultRecipeSaysWhereItsLimitsCameFrom(t *testing.T) {
	p, _ := imaging.NewPlaneU16(3, 1, []uint16{100, 4000, 250})
	r := DefaultRecipe(grayOf(t, p))
	assert.Equal(t, Limits{Lo: 100, Hi: 4000, Source: LimitsSourceData}, r.Limits)
	assert.Equal(t, TransferLinear, r.Transfer)

	flat, _ := imaging.NewPlaneU16(2, 1, []uint16{7, 7})
	r = DefaultRecipe(grayOf(t, flat))
	assert.Equal(t, Limits{Lo: 0, Hi: 65535, Source: LimitsSourceNominal}, r.Limits, "a constant plane has no data range")

	nan := float32(math.NaN())
	f, _ := imaging.NewPlaneF32(3, 1, []float32{nan, -1.5, 2})
	r = DefaultRecipe(grayOf(t, f))
	assert.Equal(t, Limits{Lo: -1.5, Hi: 2, Source: LimitsSourceData}, r.Limits)

	empty, _ := imaging.NewPlaneF32(1, 1, []float32{nan})
	r = DefaultRecipe(grayOf(t, empty))
	assert.NoError(t, r.Validate(), "even with nothing to show, the default compiles")

	scaled, err := p.WithTransform(imaging.ValueTransform{Scale: -2, Offset: 10})
	require.NoError(t, err)
	r = DefaultRecipe(grayOf(t, scaled))
	assert.Equal(t, Limits{Lo: 10 - 2*4000, Hi: 10 - 2*100, Source: LimitsSourceData}, r.Limits, "limits are in values, lower first")
}

func TestARecipeReadsAsTextAndComesBackEqual(t *testing.T) {
	r := Recipe{
		Limits:   Limits{Lo: -1.5, Hi: 4000, Source: LimitsSourcePercentile, PercentLo: 0.5, PercentHi: 99.5},
		Transfer: TransferAsinh, Param: 0.05, Invert: true, FlagClipped: true, Palette: "batlow",
	}
	data, err := json.Marshal(r)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"asinh"`)
	assert.Contains(t, string(data), `"percentile"`)
	var back Recipe
	require.NoError(t, json.Unmarshal(data, &back))
	assert.Equal(t, r, back)

	var tr TransferE
	assert.Error(t, tr.UnmarshalText([]byte("rainbow")))
	var src LimitsSourceE
	assert.Error(t, src.UnmarshalText([]byte("guess")))
	for _, x := range AllTransfers {
		text, err := x.MarshalText()
		require.NoError(t, err)
		require.NoError(t, tr.UnmarshalText(text))
		assert.Equal(t, x, tr)
	}
	for _, x := range AllLimitsSources {
		text, err := x.MarshalText()
		require.NoError(t, err)
		require.NoError(t, src.UnmarshalText(text))
		assert.Equal(t, x, src)
	}
}

// A brighter sample is never drawn darker, whatever the limits, the transfer
// function and its parameter — and the other way round when inverted.
func TestABrighterSampleIsNeverDrawnDarker(t *testing.T) {
	samples := make([]uint16, 65536)
	for i := range samples {
		samples[i] = uint16(i)
	}
	p, err := imaging.NewPlaneU16(65536, 1, samples)
	require.NoError(t, err)
	dst := make([]uint32, 65536)
	rapid.Check(t, func(t *rapid.T) {
		lo := rapid.Float64Range(-1000, 70000).Draw(t, "lo")
		r := Recipe{
			Limits:   Limits{Lo: lo, Hi: lo + rapid.Float64Range(0.5, 70000).Draw(t, "span")},
			Transfer: rapid.SampledFrom(AllTransfers).Draw(t, "transfer"),
			Param:    rapid.SampledFrom([]float64{0, 0.01, 0.5, 2, 5000}).Draw(t, "param"),
			Invert:   rapid.Bool().Draw(t, "invert"),
		}
		m, err := CompilePlane(r, p.Kind(), p.Transform(), nil)
		require.NoError(t, err)
		require.NoError(t, m.Map(dst, p, imaging.Plane{}, whole(p)))
		for i := 1; i < len(dst); i++ {
			if d := level(dst[i]) - level(dst[i-1]); (d < 0) != (r.Invert && d != 0) {
				t.Fatalf("sample %d is level %d after %d", i, level(dst[i]), level(dst[i-1]))
			}
		}
	})
}
