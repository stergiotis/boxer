package samples

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
)

func TestEveryNamedSampleIsMadeAndIsTheSameEachTime(t *testing.T) {
	want := map[string]struct {
		w, h  int
		model imaging.ColorModelE
		kind  imaging.SampleKindE
	}{
		NameField:     {1400, 1000, imaging.ColorModelGray, imaging.SampleKindU16},
		NameColour:    {640, 480, imaging.ColorModelRGBA, imaging.SampleKindU8},
		NameSmall:     {48, 32, imaging.ColorModelGray, imaging.SampleKindU8},
		NameFloat:     {640, 480, imaging.ColorModelGray, imaging.SampleKindF32},
		NameScaled:    {480, 320, imaging.ColorModelGray, imaging.SampleKindU16},
		NameElevation: {400, 300, imaging.ColorModelGray, imaging.SampleKindI16},
		NameDouble:    {320, 240, imaging.ColorModelGray, imaging.SampleKindF64},
		NameCounts:    {320, 240, imaging.ColorModelGray, imaging.SampleKindI32},
	}
	require.Len(t, Names(), len(want))
	for _, name := range Names() {
		a, err := ByName(name)
		require.NoError(t, err, name)
		b, err := ByName(name)
		require.NoError(t, err)
		assert.Equal(t, a.Hash(), b.Hash(), "%s: the same samples each time", name)
		assert.Equal(t, want[name].w, a.Width(), name)
		assert.Equal(t, want[name].h, a.Height(), name)
		assert.Equal(t, want[name].model, a.Model(), name)
		assert.Equal(t, want[name].kind, a.Kind(), name)
		assert.Contains(t, a.Provenance().SourceName, "synthetic")
	}
	_, err := ByName("nope")
	assert.Error(t, err)
}

func TestTheFloatSampleHasInvalidSamplesAndTheScaledOneATransform(t *testing.T) {
	f, err := ByName(NameFloat)
	require.NoError(t, err)
	_, ok := f.Plane(0).StoredAt(FloatInvalid.Min.X, FloatInvalid.Min.Y)
	assert.False(t, ok, "inside the invalid block")
	_, ok = f.Plane(0).StoredAt(FloatInvalid.Max.X, FloatInvalid.Min.Y)
	assert.True(t, ok, "just outside it")
	lo, hi, ok := f.Plane(0).StoredRange()
	require.True(t, ok)
	assert.Less(t, lo, -40.0)
	assert.Greater(t, hi, 9e4)

	sc, err := ByName(NameScaled)
	require.NoError(t, err)
	assert.Equal(t, ScaledTransform, sc.Plane(0).Transform())
	stored, _ := sc.Plane(0).StoredAt(0, 0)
	value, _ := sc.Plane(0).ValueAt(0, 0)
	assert.Equal(t, 4000.0, stored)
	assert.InDelta(t, 0.0, value, 1e-9, "0.01 × 4000 − 40")
}

func TestTheNewKindsSamplesDeclareWhatTheySay(t *testing.T) {
	e, err := ByName(NameElevation)
	require.NoError(t, err)
	inv, ok := e.Plane(0).Invalid()
	require.True(t, ok)
	assert.Equal(t, float64(ElevationInvalid), inv)
	_, ok = e.Plane(0).StoredAt(ElevationBorder-1, 100)
	assert.False(t, ok, "the border is invalid")
	_, ok = e.Plane(0).StoredAt(ElevationBorder, ElevationBorder)
	assert.True(t, ok, "inside it is not")
	lo, hi, ok := e.Plane(0).StoredRange()
	require.True(t, ok)
	assert.Less(t, lo, -300.0, "the basin is below zero")
	assert.Greater(t, hi, 1500.0)

	d, err := ByName(NameDouble)
	require.NoError(t, err)
	lo, hi, ok = d.Plane(0).StoredRange()
	require.True(t, ok)
	assert.Greater(t, lo, 1e9-2e-4)
	assert.Less(t, hi, 1e9+2e-4)
	assert.Equal(t, float32(lo), float32(hi), "as 32-bit floats the ripples are one value")
	_, ok = d.Plane(0).StoredAt(5, 5)
	assert.False(t, ok, "the diagonal is NaN")

	c, err := ByName(NameCounts)
	require.NoError(t, err)
	inv, ok = c.Plane(0).Invalid()
	require.True(t, ok)
	assert.Equal(t, float64(CountsInvalid), inv)
	lo, hi, ok = c.Plane(0).StoredRange()
	require.True(t, ok)
	assert.Less(t, lo, 0.0)
	assert.Greater(t, hi, 1e9)
}

func TestTheSampleStackHasItsAxesAndSelectsImages(t *testing.T) {
	require.Equal(t, []string{StackBands}, StackNames())
	a, err := StackByName(StackBands)
	require.NoError(t, err)
	b, err := StackByName(StackBands)
	require.NoError(t, err)
	assert.Equal(t, a.Hash(), b.Hash(), "the same samples each time")
	assert.Equal(t, 12, a.PlaneCount())
	assert.Equal(t, imaging.SampleKindU16, a.Kind())
	axes := a.Axes()
	require.Len(t, axes, 2)
	assert.Equal(t, "time", axes[0].Name)
	assert.Equal(t, imaging.AxisRoleSelector, axes[0].Role)
	assert.Equal(t, []float64{490, 560, 665, 842}, axes[1].Coords)
	band, ok := a.BandAxis()
	require.True(t, ok)
	assert.Equal(t, 1, band)
	assert.Equal(t, BandsPlacement, a.Placement())
	assert.Contains(t, a.Provenance().SourceName, "synthetic")

	img, err := a.Image([]int{2, 0}, []int{3, 2, 1})
	require.NoError(t, err)
	assert.Equal(t, imaging.ColorModelRGB, img.Model())
	assert.Equal(t, []string{"B8", "B4", "B3"}, img.PlaneLabels())
	inv, ok := img.Plane(0).Invalid()
	require.True(t, ok)
	assert.Equal(t, float64(BandsInvalid), inv)
	_, ok = img.Plane(0).StoredAt(0, 0)
	assert.False(t, ok, "the swath edge is invalid")
	nir, _ := img.Plane(0).StoredAt(150, 60)
	red, _ := img.Plane(1).StoredAt(150, 60)
	assert.Greater(t, nir, 4*red, "vegetation is bright in B8 against B4")

	_, err = StackByName("nope")
	assert.Error(t, err)
}
