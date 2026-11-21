package display

import (
	"image"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/stats"
)

// codePlane is a plane holding every code of a kind with codes once, in
// order, one row.
func codePlane(t *testing.T, kind imaging.SampleKindE) imaging.Plane {
	lo, n, ok := kind.Codes()
	require.True(t, ok)
	var p imaging.Plane
	var err error
	switch kind {
	case imaging.SampleKindU8:
		s := make([]uint8, n)
		for i := range s {
			s[i] = uint8(i)
		}
		p, err = imaging.NewPlaneU8(n, 1, s)
	case imaging.SampleKindU16:
		s := make([]uint16, n)
		for i := range s {
			s[i] = uint16(i)
		}
		p, err = imaging.NewPlaneU16(n, 1, s)
	case imaging.SampleKindI16:
		s := make([]int16, n)
		for i := range s {
			s[i] = int16(lo + int64(i))
		}
		p, err = imaging.NewPlaneI16(n, 1, s)
	}
	require.NoError(t, err)
	return p
}

// Every code of every kind with codes maps to what the formula gives, and
// the declared invalid code to the palette's invalid colour.
func TestEveryCodeOfATableKindMatchesTheDirectEvaluation(t *testing.T) {
	pal := GrayPalette()
	pal.Invalid, pal.Under, pal.Over = 0x00ff0080, 0x0000ffff, 0xff0000ff
	for _, kind := range []imaging.SampleKindE{imaging.SampleKindU8, imaging.SampleKindU16, imaging.SampleKindI16} {
		lo, n, _ := kind.Codes()
		nlo, nhi, _ := kind.NominalRange()
		for _, declare := range []bool{false, true} {
			p := codePlane(t, kind)
			p, err := p.WithTransform(imaging.ValueTransform{Scale: 0.5, Offset: -3})
			require.NoError(t, err)
			inv := math.Round(nlo + (nhi-nlo)*0.37)
			if declare {
				p, err = p.WithInvalid(inv)
				require.NoError(t, err)
			}
			for _, tr := range []TransferE{TransferLinear, TransferAsinh} {
				r := Recipe{Limits: Limits{Lo: p.Transform().Apply(nlo + (nhi-nlo)*0.1), Hi: p.Transform().Apply(nlo + (nhi-nlo)*0.7)},
					Transfer: tr, FlagClipped: true}
				m, err := CompilePlaneFor(r, PlaneSpecOf(p), pal)
				require.NoError(t, err)
				dst := make([]uint32, n)
				require.NoError(t, m.Map(dst, p, imaging.Plane{}, whole(p)))
				for i, c := range dst {
					stored := float64(lo + int64(i))
					want := direct(r, pal, p.Transform().Apply(stored))
					if declare && stored == inv {
						want = pal.Invalid
					}
					if want != c {
						t.Fatalf("%s declared=%v %s code %v: %#x, direct %#x", kind, declare, tr, stored, c, want)
					}
				}
			}
		}
	}
}

// The binned kinds are placed in the window in float64 before they are
// quantised: a window one unit wide around 10^9 is resolved for 64-bit
// floats and 32-bit integers, which a float32 position could not.
func TestBinnedKindsMatchTheDirectEvaluationWithinOneLevel(t *testing.T) {
	pal := GrayPalette()
	pal.Invalid = 0x00ff0080
	const n = 4001
	for _, kind := range []imaging.SampleKindE{imaging.SampleKindF32, imaging.SampleKindF64, imaging.SampleKindI32} {
		for _, declare := range []bool{false, true} {
			var p imaging.Plane
			var err error
			var lo, hi, inv float64
			switch kind {
			case imaging.SampleKindF32:
				s := make([]float32, n)
				for i := range s {
					s[i] = float32(i)/400 - 2
				}
				s[17] = float32(math.NaN())
				p, err = imaging.NewPlaneF32(n, 1, s)
				lo, hi, inv = 0, 6, float64(s[1000])
			case imaging.SampleKindF64:
				s := make([]float64, n)
				for i := range s {
					s[i] = 1e9 + float64(i)/2000 - 0.5
				}
				s[17] = math.NaN()
				p, err = imaging.NewPlaneF64(n, 1, s)
				lo, hi, inv = 1e9-0.25, 1e9+0.75, s[1000]
			case imaging.SampleKindI32:
				s := make([]int32, n)
				for i := range s {
					s[i] = int32(1_000_000_000 + i - 1000)
				}
				p, err = imaging.NewPlaneI32(n, 1, s)
				lo, hi, inv = 1e9-500, 1e9+2500, float64(s[1000])
			}
			require.NoError(t, err)
			if declare {
				p, err = p.WithInvalid(inv)
				require.NoError(t, err)
			}
			r := Recipe{Limits: Limits{Lo: lo, Hi: hi}}
			m, err := CompilePlaneFor(r, PlaneSpecOf(p), pal)
			require.NoError(t, err)
			dst := make([]uint32, n)
			require.NoError(t, m.Map(dst, p, imaging.Plane{}, whole(p)))
			for i, c := range dst {
				v, ok := p.StoredAt(i, 0)
				if !ok {
					require.Equal(t, pal.Invalid, c, "%s declared=%v: invalid at %d", kind, declare, i)
					continue
				}
				want := direct(r, pal, v)
				if d := level(c) - level(want); d < -1 || d > 1 {
					t.Fatalf("%s sample %d (%v): level %d, direct %d", kind, i, v, level(c), level(want))
				}
			}
			if declare {
				assert.Equal(t, pal.Invalid, dst[1000], "%s: the declared value is drawn invalid", kind)
			}
		}
	}
}

// A signed 16-bit colour image maps through one table indexed by
// stored − lo, and a pixel is invalid when any colour plane's sample is.
func TestASignedColourImageAndItsInvalidPixels(t *testing.T) {
	mk := func(v ...int16) imaging.Plane {
		p, err := imaging.NewPlaneI16(4, 1, v)
		require.NoError(t, err)
		p, err = p.WithInvalid(-9999)
		require.NoError(t, err)
		return p
	}
	img, err := imaging.NewImage(imaging.ColorModelRGB, []imaging.Plane{
		mk(-1000, 0, -9999, 1000), mk(-1000, 500, 0, 1000), mk(-1000, 1000, 0, -9999)}, imaging.Provenance{})
	require.NoError(t, err)
	r := Recipe{Limits: Limits{Lo: -1000, Hi: 1000}}
	m, err := CompileColorFor(r, PlaneSpecOf(img.Plane(0)))
	require.NoError(t, err)
	dst := make([]uint32, 4)
	require.NoError(t, m.Map(dst, img, image.Rect(0, 0, 4, 1)))
	assert.Equal(t, uint32(0x000000ff), dst[0])
	assert.Equal(t, uint32(0x80bfffff), dst[1], "0 → 128, 500 → 191, 1000 → 255")
	assert.Equal(t, uint32(0), dst[2], "red is invalid: the pixel is")
	assert.Equal(t, uint32(0), dst[3], "blue is invalid: the pixel is")

	undeclared, err := CompileColor(r, imaging.SampleKindI16, imaging.IdentityTransform)
	require.NoError(t, err)
	assert.Error(t, undeclared.Map(dst, img, image.Rect(0, 0, 4, 1)), "a mapper compiled for no invalid value refuses planes that declare one")

	for _, kind := range []imaging.SampleKindE{imaging.SampleKindF32, imaging.SampleKindF64, imaging.SampleKindI32} {
		_, err = CompileColor(r, kind, imaging.IdentityTransform)
		assert.Error(t, err, "%s colour is not mapped", kind)
	}
}

func TestAMapperRefusesAPlaneDeclaringAnotherInvalidValue(t *testing.T) {
	p, err := imaging.NewPlaneU8(3, 1, []uint8{1, 2, 3})
	require.NoError(t, err)
	declared, err := p.WithInvalid(2)
	require.NoError(t, err)
	r := Recipe{Limits: Limits{Lo: 0, Hi: 255}}

	plain, err := CompilePlane(r, imaging.SampleKindU8, imaging.IdentityTransform, nil)
	require.NoError(t, err)
	dst := make([]uint32, 3)
	assert.Error(t, plain.Map(dst, declared, imaging.Plane{}, whole(p)), "its invalid sample would be drawn as data")

	m, err := CompilePlaneFor(r, PlaneSpecOf(declared), nil)
	require.NoError(t, err)
	require.NoError(t, m.Map(dst, declared, imaging.Plane{}, whole(p)))
	assert.Equal(t, uint32(0), dst[1], "the grey palette draws an invalid sample transparent")
	require.NoError(t, m.Map(dst, p, imaging.Plane{}, whole(p)), "a plane that declares none is drawn under the mapper's declaration")
	assert.Equal(t, uint32(0), dst[1])

	other, err := declared.WithInvalid(3)
	require.NoError(t, err)
	assert.Error(t, m.Map(dst, other, imaging.Plane{}, whole(p)))

	for _, bad := range []PlaneSpec{
		{Kind: imaging.SampleKindU8, Transform: imaging.IdentityTransform, Invalid: 256, HasInvalid: true},
		{Kind: imaging.SampleKindI16, Transform: imaging.IdentityTransform, Invalid: 0.5, HasInvalid: true},
		{Kind: imaging.SampleKindF64, Transform: imaging.IdentityTransform, Invalid: math.NaN(), HasInvalid: true},
	} {
		_, err = CompilePlaneFor(r, bad, nil)
		assert.Error(t, err, "%+v", bad)
	}
}

func TestLimitsOfTheSignedAndWideKinds(t *testing.T) {
	l, ok := NominalLimits(imaging.SampleKindI16, imaging.IdentityTransform)
	require.True(t, ok)
	assert.Equal(t, Limits{Lo: -32768, Hi: 32767, Source: LimitsSourceNominal}, l)
	l, ok = NominalLimits(imaging.SampleKindI32, imaging.ValueTransform{Scale: -1})
	require.True(t, ok)
	assert.Equal(t, Limits{Lo: -math.MaxInt32, Hi: -math.MinInt32, Source: LimitsSourceNominal}, l)
	_, ok = NominalLimits(imaging.SampleKindF64, imaging.IdentityTransform)
	assert.False(t, ok)

	p, err := imaging.NewPlaneI16(4, 1, []int16{-32768, -400, 300, -32768})
	require.NoError(t, err)
	p, err = p.WithInvalid(-32768)
	require.NoError(t, err)
	img, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{})
	require.NoError(t, err)
	assert.Equal(t, Limits{Lo: -400, Hi: 300, Source: LimitsSourceData}, DefaultRecipe(img).Limits, "the declared value is not data")

	pl, ok := PercentileLimits(stats.Of(p), 0, 100)
	require.True(t, ok)
	assert.Equal(t, -400.0, pl.Lo)
	assert.Equal(t, 300.0, pl.Hi)

	f, err := imaging.NewPlaneF64(3, 1, []float64{-1e300, 2, 5})
	require.NoError(t, err)
	f, err = f.WithInvalid(-1e300)
	require.NoError(t, err)
	assert.Equal(t, Limits{Lo: 2, Hi: 5, Source: LimitsSourceData}, DefaultPlaneRecipe(f).Limits)
}
