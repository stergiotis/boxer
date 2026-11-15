package imaging

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlaneConstructorsRefuseAMismatchedSize(t *testing.T) {
	_, err := NewPlaneU8(2, 2, make([]uint8, 3))
	assert.Error(t, err)
	_, err = NewPlaneU16(0, 4, nil)
	assert.Error(t, err)
	_, err = NewPlaneF32(-1, 1, make([]float32, 1))
	assert.Error(t, err)
	p, err := NewPlaneU16(3, 2, make([]uint16, 6))
	require.NoError(t, err)
	assert.Equal(t, SampleKindU16, p.Kind())
	assert.Equal(t, 3, p.Width())
	assert.Equal(t, 2, p.Height())
	assert.True(t, p.Transform().IsIdentity())
	assert.True(t, Plane{}.IsZero())
}

func TestStoredAtReadsRowMajorAndKnowsItsEdges(t *testing.T) {
	p, err := NewPlaneU16(3, 2, []uint16{0, 1, 2, 10, 11, 65535})
	require.NoError(t, err)
	v, ok := p.StoredAt(2, 1)
	assert.True(t, ok)
	assert.Equal(t, 65535.0, v)
	v, ok = p.StoredAt(0, 1)
	assert.True(t, ok)
	assert.Equal(t, 10.0, v)
	for _, at := range [][2]int{{-1, 0}, {3, 0}, {0, -1}, {0, 2}} {
		_, ok = p.StoredAt(at[0], at[1])
		assert.False(t, ok, "(%d,%d) is outside", at[0], at[1])
	}
}

func TestValueAtAppliesTheTransformAndNaNIsInvalid(t *testing.T) {
	p, err := NewPlaneF32(2, 1, []float32{2, float32(math.NaN())})
	require.NoError(t, err)
	p, err = p.WithTransform(ValueTransform{Scale: 0.5, Offset: 10})
	require.NoError(t, err)
	v, ok := p.ValueAt(0, 0)
	assert.True(t, ok)
	assert.Equal(t, 11.0, v)
	_, ok = p.ValueAt(1, 0)
	assert.False(t, ok)
	_, err = p.WithTransform(ValueTransform{Scale: 0})
	assert.Error(t, err)
	_, err = p.WithTransform(ValueTransform{Scale: math.Inf(1)})
	assert.Error(t, err)
}

func TestStoredRangeSkipsInvalidSamples(t *testing.T) {
	nan := float32(math.NaN())
	p, _ := NewPlaneF32(4, 1, []float32{nan, -3, 7.5, nan})
	lo, hi, ok := p.StoredRange()
	assert.True(t, ok)
	assert.Equal(t, -3.0, lo)
	assert.Equal(t, 7.5, hi)
	p, _ = NewPlaneF32(2, 1, []float32{nan, nan})
	_, _, ok = p.StoredRange()
	assert.False(t, ok)
	q, _ := NewPlaneU8(3, 1, []uint8{9, 200, 4})
	lo, hi, ok = q.StoredRange()
	assert.True(t, ok)
	assert.Equal(t, 4.0, lo)
	assert.Equal(t, 200.0, hi)
}

func TestNominalMax(t *testing.T) {
	hi, ok := SampleKindU8.NominalMax()
	assert.True(t, ok)
	assert.Equal(t, 255.0, hi)
	hi, ok = SampleKindU16.NominalMax()
	assert.True(t, ok)
	assert.Equal(t, 65535.0, hi)
	_, ok = SampleKindF32.NominalMax()
	assert.False(t, ok)
}

func TestColorModels(t *testing.T) {
	assert.Equal(t, 1, ColorModelGray.PlaneCount())
	assert.Equal(t, []string{"red", "green", "blue", "alpha"}, ColorModelRGBA.PlaneNames())
	assert.True(t, ColorModelGrayAlpha.HasAlpha())
	assert.False(t, ColorModelRGB.HasAlpha())
	assert.True(t, ColorModelRGB.IsColor())
	assert.False(t, ColorModelGrayAlpha.IsColor())
	assert.Zero(t, ColorModelE(0).PlaneCount())
	assert.Zero(t, ColorModelE(200).PlaneCount())
}

func TestNewImageChecksItsPlanes(t *testing.T) {
	a, _ := NewPlaneU8(2, 1, []uint8{1, 2})
	b, _ := NewPlaneU8(2, 1, []uint8{3, 4})
	c, _ := NewPlaneU8(2, 1, []uint8{5, 6})
	wide, _ := NewPlaneU8(3, 1, []uint8{5, 6, 7})
	deep, _ := NewPlaneU16(2, 1, []uint16{5, 6})

	_, err := NewImage(ColorModelRGB, []Plane{a, b}, Provenance{})
	assert.Error(t, err, "too few planes")
	_, err = NewImage(ColorModelRGB, []Plane{a, b, wide}, Provenance{})
	assert.Error(t, err, "one plane of another size")
	_, err = NewImage(ColorModelRGB, []Plane{a, b, deep}, Provenance{})
	assert.Error(t, err, "one plane of another kind")
	_, err = NewImage(ColorModelE(0), nil, Provenance{})
	assert.Error(t, err)

	img, err := NewImage(ColorModelRGB, []Plane{a, b, c}, Provenance{SourceName: "x.png"})
	require.NoError(t, err)
	assert.Equal(t, 2, img.Width())
	assert.Equal(t, 1, img.Height())
	assert.Equal(t, SampleKindU8, img.Kind())
	assert.Equal(t, 3, img.PlaneCount())
	_, ok := img.Alpha()
	assert.False(t, ok)
	assert.True(t, img.Plane(7).IsZero())
	assert.NotEqual(t, [32]byte{}, img.Provenance().SampleHash)
	assert.True(t, Image{}.IsZero())
}

func TestHashFollowsSamplesAndNothingElse(t *testing.T) {
	mk := func(samples []uint16, name string, model ColorModelE) Image {
		p, err := NewPlaneU16(len(samples), 1, samples)
		require.NoError(t, err)
		planes := []Plane{p}
		if model == ColorModelGrayAlpha {
			planes = append(planes, p)
		}
		img, err := NewImage(model, planes, Provenance{SourceName: name})
		require.NoError(t, err)
		return img
	}
	a := mk([]uint16{1, 2, 3}, "a", ColorModelGray)
	assert.Equal(t, a.Hash(), mk([]uint16{1, 2, 3}, "b", ColorModelGray).Hash(), "provenance is not hashed")
	assert.NotEqual(t, a.Hash(), mk([]uint16{1, 2, 4}, "a", ColorModelGray).Hash(), "one sample differs")
	assert.NotEqual(t, a.Hash(), mk([]uint16{1, 2, 3}, "a", ColorModelGrayAlpha).Hash(), "the model differs")

	// The same bytes as two kinds are two images.
	p8, _ := NewPlaneU8(2, 1, []uint8{1, 0})
	p16, _ := NewPlaneU16(1, 1, []uint16{1})
	i8, _ := NewImage(ColorModelGray, []Plane{p8}, Provenance{})
	i16, _ := NewImage(ColorModelGray, []Plane{p16}, Provenance{})
	assert.NotEqual(t, i8.Hash(), i16.Hash())

	// A plane longer than the hasher's block still hashes every sample.
	long := make([]uint16, 100_000)
	h0 := mk(long, "", ColorModelGray).Hash()
	long2 := make([]uint16, 100_000)
	long2[99_999] = 1
	assert.NotEqual(t, h0, mk(long2, "", ColorModelGray).Hash())
}

func TestDerivationsAreASet(t *testing.T) {
	d := DerivationPaletteExpanded | DerivationColorKeyAlpha
	assert.True(t, d.Has(DerivationPaletteExpanded))
	assert.True(t, d.Has(DerivationColorKeyAlpha))
	assert.False(t, d.Has(DerivationCMYKConverted))
	seen := DerivationE(0)
	for _, x := range AllDerivations {
		assert.Zero(t, seen&x, "derivations must not overlap")
		seen |= x
	}
}
