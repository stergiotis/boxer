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
		NameField:  {1400, 1000, imaging.ColorModelGray, imaging.SampleKindU16},
		NameColour: {640, 480, imaging.ColorModelRGBA, imaging.SampleKindU8},
		NameSmall:  {48, 32, imaging.ColorModelGray, imaging.SampleKindU8},
		NameFloat:  {640, 480, imaging.ColorModelGray, imaging.SampleKindF32},
		NameScaled: {480, 320, imaging.ColorModelGray, imaging.SampleKindU16},
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
