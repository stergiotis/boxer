package stats

import (
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/science/imaging"
)

func TestAnIntegerHistogramCountsEverySampleExactly(t *testing.T) {
	p, err := imaging.NewPlaneU16(5, 1, []uint16{7, 7, 300, 65535, 7})
	require.NoError(t, err)
	h := Of(p)
	assert.True(t, h.IsExact())
	assert.Len(t, h.Counts, 65536)
	assert.EqualValues(t, 3, h.Counts[7])
	assert.EqualValues(t, 1, h.Counts[300])
	assert.EqualValues(t, 1, h.Counts[65535])
	assert.EqualValues(t, 5, h.Valid)
	assert.Zero(t, h.Invalid)
	assert.Equal(t, 7.0, h.Lo)
	assert.Equal(t, 65535.0, h.Hi)
	assert.Equal(t, 1.0, h.BinWidth())

	q, _ := imaging.NewPlaneU8(3, 1, []uint8{0, 255, 9})
	h8 := Of(q)
	assert.Len(t, h8.Counts, 256)
	lo, hi, ok := h8.Range()
	assert.True(t, ok)
	assert.Equal(t, 0.0, lo)
	assert.Equal(t, 255.0, hi)
}

// A percentile of an integer plane is the nearest-rank percentile of its
// sorted samples: a value that occurs, with at least p percent at or below.
func TestAnIntegerPercentileIsTheNearestRank(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		samples := rapid.SliceOfN(rapid.Uint16(), 1, 400).Draw(t, "samples")
		p, err := imaging.NewPlaneU16(len(samples), 1, slices.Clone(samples))
		require.NoError(t, err)
		h := Of(p)
		sorted := slices.Clone(samples)
		slices.Sort(sorted)
		pct := rapid.Float64Range(0, 100).Draw(t, "p")
		got, ok := h.PercentileStored(pct)
		require.True(t, ok)
		rank := int(math.Ceil(pct / 100 * float64(len(sorted))))
		want := float64(sorted[max(rank-1, 0)])
		require.Equal(t, want, got, "p=%v of %d samples", pct, len(sorted))
	})
}

func TestPercentilesAtTheEndsAndOutsideThem(t *testing.T) {
	p, _ := imaging.NewPlaneU8(4, 1, []uint8{10, 20, 30, 40})
	h := Of(p)
	for pct, want := range map[float64]float64{0: 10, 25: 10, 26: 20, 50: 20, 75: 30, 99: 40, 100: 40} {
		got, ok := h.PercentileStored(pct)
		assert.True(t, ok)
		assert.Equal(t, want, got, "p=%v", pct)
	}
	_, ok := h.PercentileStored(-1)
	assert.False(t, ok)
	_, ok = h.PercentileStored(100.5)
	assert.False(t, ok)
	_, ok = h.PercentileStored(math.NaN())
	assert.False(t, ok)
	_, ok = Histogram{}.PercentileStored(50)
	assert.False(t, ok)
	_, _, ok = Histogram{}.Range()
	assert.False(t, ok)
}

func TestAFloatHistogramLeavesInvalidSamplesOutAndIsRightToABin(t *testing.T) {
	nan := float32(math.NaN())
	n := 10_000
	s := make([]float32, n+2)
	for i := range n {
		s[i] = float32(i) / float32(n-1) * 50 // 0 … 50, evenly
	}
	s[n], s[n+1] = nan, nan
	p, err := imaging.NewPlaneF32(len(s), 1, s)
	require.NoError(t, err)
	h := Of(p)
	assert.False(t, h.IsExact())
	assert.EqualValues(t, n, h.Valid)
	assert.EqualValues(t, 2, h.Invalid)
	assert.Equal(t, 0.0, h.Lo)
	assert.Equal(t, 50.0, h.Hi)
	assert.InDelta(t, 50.0/FloatBins, h.BinWidth(), 1e-12)
	var total uint64
	for _, c := range h.Counts {
		total += c
	}
	assert.EqualValues(t, n, total, "the largest sample is in the last bin, not past it")
	for _, pct := range []float64{1, 25, 50, 90, 99.5} {
		got, ok := h.PercentileStored(pct)
		require.True(t, ok)
		assert.InDelta(t, pct/100*50, got, 2*h.BinWidth(), "p=%v", pct)
	}

	flat, _ := imaging.NewPlaneF32(3, 1, []float32{2.5, 2.5, nan})
	hf := Of(flat)
	assert.EqualValues(t, 2, hf.Valid)
	got, ok := hf.PercentileStored(50)
	assert.True(t, ok)
	assert.Equal(t, 2.5, got)

	none, _ := imaging.NewPlaneF32(2, 1, []float32{nan, nan})
	hn := Of(none)
	assert.Zero(t, hn.Valid)
	assert.EqualValues(t, 2, hn.Invalid)
	_, ok = hn.PercentileStored(50)
	assert.False(t, ok)
}

func TestValuesGoThroughThePlanesTransform(t *testing.T) {
	p, _ := imaging.NewPlaneU8(3, 1, []uint8{10, 20, 30})
	p, err := p.WithTransform(imaging.ValueTransform{Scale: -2, Offset: 100})
	require.NoError(t, err)
	h := Of(p)
	lo, hi, ok := h.Range()
	assert.True(t, ok)
	assert.Equal(t, 40.0, lo, "the range is in values, lower first")
	assert.Equal(t, 80.0, hi)
	assert.Equal(t, 60.0, h.Value(20))
}
