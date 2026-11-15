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
