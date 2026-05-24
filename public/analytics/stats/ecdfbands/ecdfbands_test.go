//go:build llm_generated_opus47

package ecdfbands

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBandsForSampleBasicShape verifies the structural contract of
// the high-level entry point: length of returned slices matches the
// sample size, Xs is the echoed sorted input, LowerCDF ≤ UpperCDF
// at every index, and both edges sit in [0, 1].
func TestBandsForSampleBasicShape(t *testing.T) {
	rnd := rand.New(rand.NewSource(42))
	for _, n := range []int{10, 50, 100} {
		sample := make([]float64, n)
		for i := range n {
			sample[i] = rnd.Float64()
		}
		// Sort.
		for i := 1; i < n; i++ {
			for j := i; j > 0 && sample[j] < sample[j-1]; j-- {
				sample[j], sample[j-1] = sample[j-1], sample[j]
			}
		}
		band, err := BandsForSample(sample, 0.05, BandMethodBerkJones)
		require.NoError(t, err)
		require.Len(t, band.Xs, n)
		require.Len(t, band.LowerCDF, n)
		require.Len(t, band.UpperCDF, n)
		for i := range n {
			assert.Equal(t, sample[i], band.Xs[i], "Xs[%d]", i)
			assert.LessOrEqual(t, band.LowerCDF[i], band.UpperCDF[i], "i=%d", i)
			assert.GreaterOrEqual(t, band.LowerCDF[i], 0.0)
			assert.LessOrEqual(t, band.UpperCDF[i], 1.0)
		}
		assert.Greater(t, band.CritC, 0.0)
		assert.Equal(t, 0.05, band.Alpha)
		assert.Equal(t, BandMethodBerkJones, band.Method)
	}
}

// TestBandsForSampleCopiesInput protects callers against the band
// holding aliased state of the input sample.
func TestBandsForSampleCopiesInput(t *testing.T) {
	sample := []float64{0.1, 0.2, 0.3, 0.4, 0.5}
	band, err := BandsForSample(sample, 0.05, BandMethodDKW)
	require.NoError(t, err)
	sample[0] = 999
	assert.Equal(t, 0.1, band.Xs[0], "Xs aliased input mutation")
}

// TestBandsForSampleRejectsUnsorted catches the most common user
// mistake — passing a raw (unsorted) sample.
func TestBandsForSampleRejectsUnsorted(t *testing.T) {
	_, err := BandsForSample([]float64{0.5, 0.2, 0.8}, 0.05, BandMethodBerkJones)
	assert.Error(t, err)
}

// TestBandsForGridRoundTrip exercises the streaming path by feeding
// the sorted sample's actual ECDF positions back through BandsForGrid
// and checking the per-grid-point bands agree with BandsForSample at
// the same n and method.
func TestBandsForGridRoundTrip(t *testing.T) {
	const n = 20
	sample := make([]float64, n)
	for i := range n {
		sample[i] = float64(i+1) / float64(n+1)
	}
	band, err := BandsForSample(sample, 0.05, BandMethodBerkJones)
	require.NoError(t, err)

	// Build the F_n at the same grid (xs = sample, fnAt = (i+1)/n).
	fnAt := make([]float64, n)
	for i := range n {
		fnAt[i] = float64(i+1) / float64(n)
	}
	g, err := BandsForGrid(sample, fnAt, n, 0.05, BandMethodBerkJones)
	require.NoError(t, err)
	require.Len(t, g.LowerCDF, n)
	require.Len(t, g.UpperCDF, n)
	assert.Equal(t, n, g.N)
	assert.Equal(t, BandMethodBerkJones, g.Method)
	assert.Equal(t, 0.05, g.Alpha)
	for i := range n {
		assert.InDelta(t, band.LowerCDF[i], g.LowerCDF[i], 1e-10, "lower at i=%d", i)
		assert.InDelta(t, band.UpperCDF[i], g.UpperCDF[i], 1e-10, "upper at i=%d", i)
	}
}

// TestQuantileBoundariesAndCriticalValueExpose the two convenience
// entry points share the same cache as the high-level routine.
func TestQuantileBoundariesAndCriticalValueExpose(t *testing.T) {
	lower, upper, err := QuantileBoundaries(15, 0.05, BandMethodBerkJones)
	require.NoError(t, err)
	c, err := CriticalValue(15, 0.05, BandMethodBerkJones)
	require.NoError(t, err)
	assert.Len(t, lower, 15)
	assert.Len(t, upper, 15)
	assert.Greater(t, c, 0.0)

	// Both routines must agree with criticalValueAndBands.
	c2, lo2, up2, err := criticalValueAndBands(15, 0.05, BandMethodBerkJones, CrossingAlgorithmAuto)
	require.NoError(t, err)
	assert.Equal(t, c, c2)
	assert.Equal(t, lower, lo2)
	assert.Equal(t, upper, up2)
}

// TestBandsForGridContinuousP makes sure intermediate grid positions
// (F_n at fractional p, not on the rank grid) produce a valid band
// with monotone behaviour in p.
func TestBandsForGridContinuousP(t *testing.T) {
	const n = 30
	xs := []float64{0.1, 0.3, 0.5, 0.7, 0.9}
	fnAt := []float64{0.05, 0.2, 0.5, 0.8, 0.95}
	g, err := BandsForGrid(xs, fnAt, n, 0.05, BandMethodBerkJones)
	require.NoError(t, err)
	for i := range xs {
		assert.LessOrEqual(t, g.LowerCDF[i], fnAt[i], "lower above p at i=%d", i)
		assert.GreaterOrEqual(t, g.UpperCDF[i], fnAt[i], "upper below p at i=%d", i)
	}
}

// TestEmptySampleRejected catches the n=0 edge case early.
func TestEmptySampleRejected(t *testing.T) {
	_, err := BandsForSample(nil, 0.05, BandMethodBerkJones)
	assert.Error(t, err)
	_, err = BandsForSample([]float64{}, 0.05, BandMethodBerkJones)
	assert.Error(t, err)
}

// TestNaNRejected exercises NaN handling on both BandsForSample and
// BandsForGrid.
func TestNaNRejected(t *testing.T) {
	_, err := BandsForSample([]float64{0.1, math.NaN(), 0.3}, 0.05, BandMethodDKW)
	assert.Error(t, err)
	_, err = BandsForGrid([]float64{0, 0.5, 1}, []float64{0, math.NaN(), 1}, 10, 0.05, BandMethodDKW)
	assert.Error(t, err)
}
