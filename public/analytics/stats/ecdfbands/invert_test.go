//go:build llm_generated_opus47

package ecdfbands

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInversionSelfConsistency verifies that for every band family,
// the c returned by the bisection produces a crossing probability
// within 1e-6 of the target 1 - α. This is the operative correctness
// statement of the inversion layer — independent of any external
// reference table.
func TestInversionSelfConsistency(t *testing.T) {
	methods := []BandMethodE{
		BandMethodBerkJones,
		BandMethodDKW,
		BandMethodEqualPrecision,
		BandMethodHigherCriticism,
	}
	for _, method := range methods {
		for _, n := range []int{10, 25, 50} {
			for _, alpha := range []float64{0.01, 0.05, 0.10} {
				c, lower, upper, err := criticalValueAndBands(n, alpha, method, CrossingAlgorithmMoscovich)
				require.NoError(t, err, "method=%d n=%d α=%v", method, n, alpha)
				p, err := CrossingProbability(lower, upper, CrossingAlgorithmMoscovich)
				require.NoError(t, err)
				assert.InDelta(t, 1-alpha, p, 1e-6,
					"method=%d n=%d α=%v: c=%v P=%v target=%v",
					method, n, alpha, c, p, 1-alpha)
			}
		}
	}
}

// TestKSReferenceCriticalValues verifies the DKW/KS family against
// classical published critical values. We compare to the
// Smirnov/Massey/Lilliefors tabulations of d_n at α = 0.05 and
// α = 0.01. Tolerance of 5e-3 absorbs the discretisation of the
// published tables (typically reported to 3 decimal places).
//
// References:
//
//   - Smirnov, N.V. (1948). "Table for estimating the goodness of fit
//     of empirical distributions." Ann. Math. Statist. 19, 279-281.
//   - Massey, F.J. (1951). "The Kolmogorov-Smirnov test for goodness
//     of fit." J. Amer. Statist. Assoc. 46, 68-78.
//   - Lilliefors, H.W. (1967). "On the Kolmogorov-Smirnov test for
//     normality with mean and variance unknown." JASA 62, 399-402.
func TestKSReferenceCriticalValues(t *testing.T) {
	type ref struct {
		n     int
		alpha float64
		d     float64
	}
	cases := []ref{
		{5, 0.05, 0.5633},
		{10, 0.05, 0.40925},
		{20, 0.05, 0.29408},
		{30, 0.05, 0.24170},
		{50, 0.05, 0.18841},
		{10, 0.01, 0.48893},
		{20, 0.01, 0.35241},
		{50, 0.01, 0.22657},
	}
	for _, r := range cases {
		c, _, _, err := criticalValueAndBands(r.n, r.alpha, BandMethodDKW, CrossingAlgorithmMoscovich)
		require.NoError(t, err, "n=%d α=%v", r.n, r.alpha)
		assert.InDelta(t, r.d, c, 5e-3,
			"KS critical value n=%d α=%v: want %v, got %v", r.n, r.alpha, r.d, c)
	}
}

// TestInversionCacheHit confirms repeated requests return identical
// results (no per-call recomputation visible in the c value) and that
// returned slices are independent copies — mutating one does not
// poison the cache.
func TestInversionCacheHit(t *testing.T) {
	c1, lo1, up1, err := criticalValueAndBands(20, 0.05, BandMethodBerkJones, CrossingAlgorithmMoscovich)
	require.NoError(t, err)
	c2, lo2, up2, err := criticalValueAndBands(20, 0.05, BandMethodBerkJones, CrossingAlgorithmMoscovich)
	require.NoError(t, err)
	assert.Equal(t, c1, c2, "cached c differs")
	assert.Equal(t, lo1, lo2)
	assert.Equal(t, up1, up2)

	// Mutate the first copy; the second must be untouched.
	lo1[0] = 999
	up1[5] = -999
	_, lo3, up3, err := criticalValueAndBands(20, 0.05, BandMethodBerkJones, CrossingAlgorithmMoscovich)
	require.NoError(t, err)
	assert.NotEqual(t, 999.0, lo3[0])
	assert.NotEqual(t, -999.0, up3[5])
}

// TestInversionAlphaQuantization shows that ε-differences in α below
// the 1e-9 quantization grid hit the same cache entry. Above the
// grid, distinct entries form.
func TestInversionAlphaQuantization(t *testing.T) {
	const n = 12
	c1, _, _, err := criticalValueAndBands(n, 0.05, BandMethodBerkJones, CrossingAlgorithmMoscovich)
	require.NoError(t, err)
	c2, _, _, err := criticalValueAndBands(n, 0.05+1e-12, BandMethodBerkJones, CrossingAlgorithmMoscovich)
	require.NoError(t, err)
	assert.Equal(t, c1, c2, "α + 1e-12 should hit the same cache entry")

	c3, _, _, err := criticalValueAndBands(n, 0.10, BandMethodBerkJones, CrossingAlgorithmMoscovich)
	require.NoError(t, err)
	assert.NotEqual(t, c1, c3, "α = 0.10 must be a distinct entry")
}

// TestInversionInvalidInputs asserts that out-of-range (n, α) reject.
func TestInversionInvalidInputs(t *testing.T) {
	cases := []struct {
		n     int
		alpha float64
		name  string
	}{
		{0, 0.05, "n=0"},
		{-5, 0.05, "negative n"},
		{20, 0, "alpha=0"},
		{20, 1, "alpha=1"},
		{20, -0.1, "negative alpha"},
		{20, 1.5, "alpha > 1"},
		{20, math.NaN(), "NaN alpha"},
	}
	for _, c := range cases {
		_, _, _, err := criticalValueAndBands(c.n, c.alpha, BandMethodBerkJones, CrossingAlgorithmMoscovich)
		assert.Error(t, err, c.name)
	}
}

// TestBJCriticalValueGrowsWithN walks BJ's critical value across n
// and verifies the asymptotic O(log log n) growth: not strictly
// monotone (tied or near-tied at adjacent n), but monotone in the
// asymptotic sense — c(100) > c(20) > c(5).
func TestBJCriticalValueGrowsWithN(t *testing.T) {
	cPrev := 0.0
	for _, n := range []int{5, 20, 100} {
		c, _, _, err := criticalValueAndBands(n, 0.05, BandMethodBerkJones, CrossingAlgorithmMoscovich)
		require.NoError(t, err)
		assert.Greater(t, c, cPrev, "BJ c non-monotone at n=%d", n)
		cPrev = c
	}
}

// TestMethodRanking probes the well-known relative tightness of the
// band families at a single configuration (n=50, α=0.05). All four
// families satisfy P(inside) ≈ 0.95 by construction; the bands
// themselves differ in shape. BJ and DKW are roughly comparable at
// p≈0.5; near the tails BJ is markedly tighter. We do not lock down
// a strict ordering — that would be brittle — but we verify the
// bands are within a sensible envelope of the DKW symmetric width.
func TestMethodRanking(t *testing.T) {
	const n = 50
	const alpha = 0.05

	widths := map[BandMethodE][]float64{}
	for _, method := range []BandMethodE{
		BandMethodBerkJones,
		BandMethodDKW,
		BandMethodEqualPrecision,
		BandMethodHigherCriticism,
	} {
		_, lower, upper, err := criticalValueAndBands(n, alpha, method, CrossingAlgorithmMoscovich)
		require.NoError(t, err)
		ws := make([]float64, n)
		for i := range n {
			ws[i] = upper[i] - lower[i]
		}
		widths[method] = ws
	}

	// At the centre p≈0.5 (i = n/2), all methods should give bands
	// of similar order of magnitude (within factor 3 of DKW).
	mid := n / 2
	dkwMid := widths[BandMethodDKW][mid]
	for method, ws := range widths {
		ratio := ws[mid] / dkwMid
		assert.Greater(t, ratio, 0.3, "method=%d too narrow at midpoint", method)
		assert.Less(t, ratio, 3.0, "method=%d too wide at midpoint", method)
	}
}
