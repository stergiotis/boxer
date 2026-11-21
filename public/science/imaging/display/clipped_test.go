package display

import (
	"image"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/stats"
)

// countClipped counts the valid values strictly below lo and strictly above
// hi, sample by sample.
func countClipped(p imaging.Plane, lo, hi float64) (below, above, valid int) {
	for y := range p.Height() {
		for x := range p.Width() {
			v, ok := p.ValueAt(x, y)
			if !ok {
				continue
			}
			valid++
			if v < lo {
				below++
			} else if v > hi {
				above++
			}
		}
	}
	return below, above, valid
}

func largestBin(h stats.Histogram) (n uint64) {
	for i := range h.Bins() {
		n = max(n, h.Count(i))
	}
	return n
}

// For every kind, the shares are the count over the samples: exactly for a
// kind with codes, to within the largest bin's share otherwise, also when
// the bins divide a range narrower than the samples'.
func TestClippedSharesAgreeWithACountOverTheSamples(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		kind := rapid.SampledFrom(imaging.AllSampleKinds).Draw(t, "kind")
		w, h := rapid.IntRange(1, 40).Draw(t, "w"), rapid.IntRange(1, 30).Draw(t, "h")
		p := drawPlane(t, "p", kind, w, h, rapid.Bool().Draw(t, "declare"))
		hist := stats.Of(p)
		if _, _, exact := kind.Codes(); !exact && rapid.Bool().Draw(t, "narrow") {
			if lo, hi, ok := p.StoredRange(); ok {
				var err error
				hist, err = stats.OfRegionIn(p, whole(p), lo+(hi-lo)*0.2, hi-(hi-lo)*0.3)
				require.NoError(t, err)
			}
		}
		r := drawRecipe(t, "r", p)
		lo, hi := r.Limits.Lo, r.Limits.Hi
		if rapid.Bool().Draw(t, "onASample") {
			// A limit on a value that occurs is not clipping it.
			if v, ok := p.ValueAt(rapid.IntRange(0, w-1).Draw(t, "x"), rapid.IntRange(0, h-1).Draw(t, "y")); ok {
				if rapid.Bool().Draw(t, "low") {
					lo = min(v, hi)
				} else {
					hi = max(v, lo)
				}
			}
		}
		below, above, valid := countClipped(p, lo, hi)
		share, ok := ClippedShares(hist, Limits{Lo: lo, Hi: hi})
		if valid == 0 {
			require.False(t, ok)
			return
		}
		require.True(t, ok)
		_, _, exact := kind.Codes()
		require.Equal(t, exact, share.Exact)
		if exact {
			require.Equal(t, float64(below)/float64(valid), share.Below)
			require.Equal(t, float64(above)/float64(valid), share.Above)
			return
		}
		tol := 2*float64(largestBin(hist))/float64(valid) + 1e-12
		require.InDelta(t, float64(below)/float64(valid), share.Below, tol)
		require.InDelta(t, float64(above)/float64(valid), share.Above, tol)
	})
}

func TestClippedSharesEdges(t *testing.T) {
	p, err := imaging.NewPlaneU8(5, 1, []uint8{0, 10, 20, 30, 40})
	require.NoError(t, err)
	s, ok := ClippedShares(stats.Of(p), Limits{Lo: 10, Hi: 30})
	require.True(t, ok)
	assert.Equal(t, ClippedShare{Below: 0.2, Above: 0.2, Exact: true}, s, "a value at a limit is not clipped")
	s, ok = ClippedShares(stats.Of(p), Limits{Lo: 20, Hi: 20})
	require.True(t, ok)
	assert.Equal(t, ClippedShare{Below: 0.4, Above: 0.4, Exact: true}, s)
	_, ok = ClippedShares(stats.Of(p), Limits{Lo: 30, Hi: 10})
	assert.False(t, ok)
	_, ok = ClippedShares(stats.Of(p), Limits{Lo: math.NaN(), Hi: 10})
	assert.False(t, ok)
	_, ok = ClippedShares(stats.Histogram{}, Limits{Lo: 0, Hi: 1})
	assert.False(t, ok, "nothing counted")

	nan := []float64{math.NaN(), math.NaN()}
	q, err := imaging.NewPlaneF64(2, 1, nan)
	require.NoError(t, err)
	_, ok = ClippedShares(stats.Of(q), Limits{Lo: 0, Hi: 1})
	assert.False(t, ok, "no valid sample")

	// A binned plane: limits at its data range clip nothing exactly; limits
	// beyond it clip all of one side exactly; a negative value transform
	// turns stored order round.
	f, err := imaging.NewPlaneF32(4, 1, []float32{1, 2, 3, 4})
	require.NoError(t, err)
	f, err = f.WithTransform(imaging.ValueTransform{Scale: -1, Offset: 0})
	require.NoError(t, err)
	l, ok := DataLimits(f)
	require.True(t, ok)
	s, ok = ClippedShares(stats.Of(f), l)
	require.True(t, ok)
	assert.Equal(t, ClippedShare{}, s)
	s, ok = ClippedShares(stats.Of(f), Limits{Lo: 0, Hi: 10})
	require.True(t, ok)
	assert.Equal(t, 1.0, s.Below)
	assert.Equal(t, 0.0, s.Above)
	s, ok = ClippedShares(stats.Of(f), Limits{Lo: -3.5, Hi: -1.5})
	require.True(t, ok)
	assert.InDelta(t, 0.25, s.Below, 1e-3, "-4 is below")
	assert.InDelta(t, 0.25, s.Above, 1e-3, "-1 is above")

	one, err := imaging.NewPlaneF64(3, 1, []float64{5, 5, 5})
	require.NoError(t, err)
	s, ok = ClippedShares(stats.Of(one), Limits{Lo: 4, Hi: 6})
	require.True(t, ok)
	assert.Equal(t, ClippedShare{}, s)
	s, ok = ClippedShares(stats.Of(one), Limits{Lo: 6, Hi: 7})
	require.True(t, ok)
	assert.Equal(t, 1.0, s.Below)

	h, err := stats.OfRegion(f, image.Rect(0, 0, 2, 1))
	require.NoError(t, err)
	s, ok = ClippedShares(h, Limits{Lo: -1.5, Hi: 0})
	require.True(t, ok)
	assert.InDelta(t, 0.5, s.Below, 1e-3, "a region's histogram")
}
