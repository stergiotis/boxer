//go:build llm_generated_opus47

package ecdfbands

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogSumExpPairBasic(t *testing.T) {
	// log(exp(0) + exp(0)) = log 2.
	require.InDelta(t, math.Log(2), logSumExpPair(0, 0), 1e-15)
	// log(exp(1) + exp(2)) = 2 + log(1 + 1/e).
	got := logSumExpPair(1, 2)
	want := 2.0 + math.Log1p(math.Exp(-1.0))
	require.InDelta(t, want, got, 1e-15)
	// Symmetry.
	require.InDelta(t, logSumExpPair(1, 2), logSumExpPair(2, 1), 1e-15)
}

func TestLogSumExpPairOverflowSafe(t *testing.T) {
	// Naive exp(1000)+exp(1000) overflows; logSumExp must not.
	got := logSumExpPair(1000, 1000)
	require.InDelta(t, 1000.0+math.Log(2), got, 1e-12)
	require.False(t, math.IsInf(got, 0))
}

func TestLogSumExpPairNegInf(t *testing.T) {
	// -Inf in either slot returns the other operand.
	require.Equal(t, 3.0, logSumExpPair(negInf, 3.0))
	require.Equal(t, 3.0, logSumExpPair(3.0, negInf))
	// Both -Inf returns -Inf.
	require.True(t, math.IsInf(logSumExpPair(negInf, negInf), -1))
}

func TestLogSumExpSliceEmptyAllNeg(t *testing.T) {
	require.True(t, math.IsInf(logSumExpSlice(nil), -1))
	require.True(t, math.IsInf(logSumExpSlice([]float64{}), -1))
	require.True(t, math.IsInf(logSumExpSlice([]float64{negInf, negInf, negInf}), -1))
}

func TestLogSumExpSliceConsistencyWithPair(t *testing.T) {
	xs := []float64{0.1, 0.2, 0.3, 0.4, 0.5}
	want := xs[0]
	for _, x := range xs[1:] {
		want = logSumExpPair(want, x)
	}
	got := logSumExpSlice(xs)
	require.InDelta(t, want, got, 1e-14)
}

func TestLogSumExpSliceOverflowSafe(t *testing.T) {
	// Single entry near double-max — must not overflow.
	xs := []float64{1000, 999, 998}
	got := logSumExpSlice(xs)
	// log(exp(1000) + exp(999) + exp(998))
	//   = 1000 + log(1 + 1/e + 1/e^2)
	want := 1000.0 + math.Log(1+math.Exp(-1)+math.Exp(-2))
	require.InDelta(t, want, got, 1e-12)
}

func TestLogFactorialSmallInts(t *testing.T) {
	cases := map[int]float64{
		0:  0,
		1:  0,
		2:  math.Log(2),
		5:  math.Log(120),
		10: math.Log(3628800),
		20: math.Log(2432902008176640000),
	}
	for n, want := range cases {
		got := logFactorial(n)
		assert.InDelta(t, want, got, 1e-10, "logFactorial(%d)", n)
	}
}

func TestLogFactorialMonotone(t *testing.T) {
	// 0! == 1! == 1 ⇒ log == 0; strict monotonicity starts at n=2.
	prev := logFactorial(1)
	for n := 2; n < 1000; n++ {
		cur := logFactorial(n)
		require.Greater(t, cur, prev, "logFactorial non-monotone at n=%d", n)
		prev = cur
	}
}

func TestLogFactorialNegativeIsNaN(t *testing.T) {
	require.True(t, math.IsNaN(logFactorial(-1)))
	require.True(t, math.IsNaN(logFactorial(-100)))
}

func TestLogPoissonPMFEdgeT0(t *testing.T) {
	// P(N(0) = 0) = 1 ⇒ logP = 0; P(N(0) = k>0) = 0 ⇒ logP = -Inf.
	require.Equal(t, 0.0, logPoissonPMF(0, 0))
	require.True(t, math.IsInf(logPoissonPMF(1, 0), -1))
	require.True(t, math.IsInf(logPoissonPMF(99, 0), -1))
}

func TestLogPoissonPMFK0(t *testing.T) {
	// P(N(t) = 0) = exp(-t) ⇒ logP = -t.
	require.InDelta(t, -1.0, logPoissonPMF(0, 1.0), 1e-15)
	require.InDelta(t, -5.0, logPoissonPMF(0, 5.0), 1e-15)
}

func TestLogPoissonPMFNormalizes(t *testing.T) {
	// Σ_{k≥0} P(N(τ) = k) = 1 for every τ > 0.
	// Truncate sum where mass becomes negligible.
	for _, tau := range []float64{0.5, 1.0, 2.5, 10.0, 50.0} {
		kmax := int(tau + 30.0*math.Sqrt(tau) + 50)
		logs := make([]float64, 0, kmax+1)
		for k := 0; k <= kmax; k++ {
			logs = append(logs, logPoissonPMF(k, tau))
		}
		logTotal := logSumExpSlice(logs)
		sum := math.Exp(logTotal)
		assert.InDelta(t, 1.0, sum, 1e-10, "Poisson PMF normalization at τ=%g", tau)
	}
}

func TestBinomKLEqual(t *testing.T) {
	for _, p := range []float64{0, 0.001, 0.25, 0.5, 0.999, 1.0} {
		assert.Equal(t, 0.0, binomKL(p, p), "D(%v ‖ %v)", p, p)
	}
}

func TestBinomKLBoundaryP(t *testing.T) {
	// D(0 ‖ q) = -log(1-q) for q ∈ (0, 1). Use a relative
	// tolerance of a few ulp; the implementation goes through
	// math.Log1p which carries its own rounding.
	require.InDelta(t, -math.Log(1-0.3), binomKL(0, 0.3), 1e-14)
	require.InDelta(t, -math.Log(1-0.99), binomKL(0, 0.99), 1e-13)
	// D(1 ‖ q) = -log(q).
	require.InDelta(t, -math.Log(0.3), binomKL(1, 0.3), 1e-14)
	require.InDelta(t, -math.Log(0.01), binomKL(1, 0.01), 1e-13)
}

func TestBinomKLBoundaryQInfinite(t *testing.T) {
	// Interior p, boundary q ⇒ +Inf.
	require.True(t, math.IsInf(binomKL(0.5, 0.0), 1))
	require.True(t, math.IsInf(binomKL(0.5, 1.0), 1))
	require.True(t, math.IsInf(binomKL(0.001, 0.0), 1))
	require.True(t, math.IsInf(binomKL(0.999, 1.0), 1))
	// p == q == 0 or 1 was already zeroed by the equal-p branch.
	require.Equal(t, 0.0, binomKL(0.0, 0.0))
	require.Equal(t, 0.0, binomKL(1.0, 1.0))
}

func TestBinomKLNonNegative(t *testing.T) {
	// KL ≥ 0 always (Gibbs).
	for _, p := range []float64{0.01, 0.1, 0.25, 0.5, 0.75, 0.9, 0.99} {
		for _, q := range []float64{0.01, 0.1, 0.25, 0.5, 0.75, 0.9, 0.99} {
			d := binomKL(p, q)
			require.GreaterOrEqual(t, d, 0.0, "D(%v ‖ %v) = %v", p, q, d)
		}
	}
}

func TestBinomKLOutsideRangeNaN(t *testing.T) {
	require.True(t, math.IsNaN(binomKL(-0.1, 0.5)))
	require.True(t, math.IsNaN(binomKL(0.5, 1.1)))
	require.True(t, math.IsNaN(binomKL(math.NaN(), 0.5)))
	require.True(t, math.IsNaN(binomKL(0.5, math.NaN())))
}

// TestBinomKLSecondOrderApproximation verifies that for q close to p,
// D(p ‖ q) ≈ (p-q)^2 / (2 p (1-p)) — the local Fisher-information
// expansion. This guards against sign errors and overall scale.
func TestBinomKLSecondOrderApproximation(t *testing.T) {
	const eps = 1e-4
	for _, p := range []float64{0.1, 0.3, 0.5, 0.7, 0.9} {
		q := p + eps
		approx := (p - q) * (p - q) / (2 * p * (1 - p))
		exact := binomKL(p, q)
		// Relative error from O(ε^3) terms should be < ε.
		assert.InDelta(t, approx, exact, approx*1e-2,
			"local-expansion mismatch at p=%v, q=%v: approx=%v exact=%v",
			p, q, approx, exact)
	}
}

