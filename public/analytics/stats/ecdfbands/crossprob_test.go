//go:build llm_generated_opus47

package ecdfbands

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSteckN1 exercises the analytical case
//
//	P(a_1 ≤ U_{(1)} ≤ b_1) = b_1 - a_1.
//
// at n = 1. This is the only configuration where the Steck-Noé
// determinant has a closed form to all digits, so we use a strict
// tolerance.
func TestSteckN1(t *testing.T) {
	cases := []struct {
		a, b float64
	}{
		{0.0, 1.0},
		{0.1, 0.9},
		{0.4, 0.6},
		{0.0, 0.5},
		{0.5, 1.0},
	}
	for _, c := range cases {
		p, err := crossingProbabilitySteck([]float64{c.a}, []float64{c.b})
		require.NoError(t, err)
		require.InDelta(t, c.b-c.a, p, 1e-15, "n=1 a=%v b=%v", c.a, c.b)
	}
}

// TestSteckN2Disjoint covers the n = 2 case with non-overlapping bands
// (a_2 > b_1), where the integral collapses to
//
//	P = 2 (b_1 - a_1)(b_2 - a_2)
//
// and the Steck determinant should reproduce it exactly.
func TestSteckN2Disjoint(t *testing.T) {
	a := []float64{0.1, 0.4}
	b := []float64{0.3, 0.7}
	want := 2.0 * (b[0] - a[0]) * (b[1] - a[1])
	got, err := crossingProbabilitySteck(a, b)
	require.NoError(t, err)
	require.InDelta(t, want, got, 1e-14)
}

// TestSteckN2Overlapping covers the n = 2 case with overlapping bands
// (a_2 ≤ b_1). Hand integration gives
//
//	P = (b_1 - a_1)^2 - (a_2 - a_1)^2 + 2(b_1 - a_1)(b_2 - b_1).
func TestSteckN2Overlapping(t *testing.T) {
	a := []float64{0.1, 0.2}
	b := []float64{0.3, 0.5}
	want := math.Pow(b[0]-a[0], 2) - math.Pow(a[1]-a[0], 2) + 2*(b[0]-a[0])*(b[1]-b[0])
	got, err := crossingProbabilitySteck(a, b)
	require.NoError(t, err)
	require.InDelta(t, want, got, 1e-14, "expected %v, got %v", want, got)
}

// TestSteckTrivialUnit verifies that the band covering [0, 1] at every
// rank — the "always inside" constraint — yields P = 1.
//
// Restricted to n ≤ 30: above that, Hessenberg LU on the trivial-unit
// matrix accumulates catastrophic cancellation in the elimination
// (the (j+1)/(k+1) ratio of subtracted entries drives the result to
// fewer than 4 surviving digits by n ≈ 50). The Moscovich-Nadler
// engine handles the same configuration uniformly across n; see
// TestMoscovichTrivialUnit.
func TestSteckTrivialUnit(t *testing.T) {
	for _, n := range []int{1, 2, 5, 10, 20, 30} {
		a := make([]float64, n)
		b := make([]float64, n)
		for i := range b {
			b[i] = 1
		}
		p, err := crossingProbabilitySteck(a, b)
		require.NoError(t, err)
		assert.InDelta(t, 1.0, p, 1e-6, "trivial unit at n=%d", n)
	}
}

// TestSteckCollapsedBand confirms that a degenerate band (a_i = b_i
// for some i) returns 0 — the constraint set is a Lebesgue-null
// subset of the order-statistic simplex.
func TestSteckCollapsedBand(t *testing.T) {
	a := []float64{0.1, 0.5, 0.5}
	b := []float64{0.9, 0.5, 0.9}
	p, err := crossingProbabilitySteck(a, b)
	require.NoError(t, err)
	require.Equal(t, 0.0, p)
}

// TestSteckUpperBoundsOnly compares the one-sided KS upper-bound
// formula P(U_{(i)} ≤ b_i ∀ i) against the closed form
// P = product / n^n ... actually the one-sided Daniels (1945)
// classical result gives, for b_i = i/n + λ/√n,
//
//	P(U_{(i)} ≤ b_i ∀ i) → 1 - exp(-2λ²) as n → ∞.
//
// We use the simpler exact case b_i = β · i for β ∈ (0, 1] with
// a_i = 0, whose closed form is
//
//	P = (1 - β)·(...) — no closed form in general,
//
// so instead we cross-check Steck against direct simulation. Done in
// the slow-tag MonteCarlo test; here we only sanity-check a small n
// with a hand-computed value.
//
// n = 2, a = (0, 0), b = (b1, b2), b1 ≤ b2:
//
//	P = 2·b_1·b_2 - b_1²    (derived above for n=2 lower-zero case)
func TestSteckN2OneSided(t *testing.T) {
	cases := []struct {
		b1, b2 float64
	}{
		{0.5, 0.7},
		{0.3, 0.6},
		{0.1, 0.9},
	}
	for _, c := range cases {
		a := []float64{0, 0}
		b := []float64{c.b1, c.b2}
		want := 2*c.b1*c.b2 - c.b1*c.b1
		got, err := crossingProbabilitySteck(a, b)
		require.NoError(t, err)
		assert.InDelta(t, want, got, 1e-14,
			"n=2 one-sided b=(%v, %v): want %v, got %v", c.b1, c.b2, want, got)
	}
}

// TestSteckDKWClosedForm compares Steck-Noé against the
// Dvoretzky-Kiefer-Wolfowitz closed form. The DKW critical region for
// the two-sided one-sample KS statistic at confidence 1 - α is
//
//	|F_n(x) - F(x)| ≤ ε   for all x, with ε = √(ln(2/α) / (2n)).
//
// Under the bijection F(x) ↦ x ∈ [0, 1] this maps to the symmetric
// bands a_i = i/n - ε, b_i = i/n + ε (both clipped to [0, 1]).
//
// Massart (1990) showed P(|F_n - F| > ε) ≤ 2 exp(-2nε²) is tight, but
// equality is only asymptotic; for finite n the actual probability
// inside the band is *higher* than 1 - 2 exp(-2nε²). The DKW closed
// form is therefore a *lower bound*, not an equality. We accept any
// Steck answer above the closed-form bound.
func TestSteckDKWLowerBound(t *testing.T) {
	const alpha = 0.05
	for _, n := range []int{10, 25, 50, 100} {
		eps := math.Sqrt(math.Log(2/alpha) / (2 * float64(n)))
		a := make([]float64, n)
		b := make([]float64, n)
		for i := range n {
			p := float64(i+1) / float64(n)
			a[i] = math.Max(0, p-eps)
			b[i] = math.Min(1, p+eps)
		}
		got, err := crossingProbabilitySteck(a, b)
		require.NoError(t, err)
		dkwBound := 1 - 2*math.Exp(-2*float64(n)*eps*eps)
		assert.GreaterOrEqual(t, got, dkwBound-1e-10,
			"Steck (%v) below DKW lower bound (%v) at n=%d", got, dkwBound, n)
		assert.LessOrEqual(t, got, 1.0+1e-10, "P > 1 at n=%d", n)
	}
}

// TestMoscovichTrivialUnit is the Moscovich-Nadler counterpart of the
// Steck trivial-unit test. Because the Poissonized DP keeps every
// entry strictly non-negative throughout, the same band yields P = 1
// for n well past the Steck-Noé numerical envelope.
func TestMoscovichTrivialUnit(t *testing.T) {
	for _, n := range []int{1, 2, 5, 10, 50, 100, 500, 1000} {
		a := make([]float64, n)
		b := make([]float64, n)
		for i := range b {
			b[i] = 1
		}
		p, err := crossingProbabilityMoscovich(a, b)
		require.NoError(t, err)
		assert.InDelta(t, 1.0, p, 1e-6, "Moscovich trivial unit at n=%d", n)
	}
}

// TestMoscovichVsSteckSmallN cross-validates the two engines on
// randomly-generated monotone bands at n where Steck still has its
// digits. Agreement to ~1e-6 in P space is the threshold for trusting
// Moscovich as the production algorithm.
func TestMoscovichVsSteckSmallN(t *testing.T) {
	rnd := rand.New(rand.NewSource(13))
	for _, n := range []int{2, 4, 8, 16, 24} {
		for trial := range 5 {
			// Generate monotone a, b with a_i ≤ b_i.
			a := make([]float64, n)
			b := make([]float64, n)
			for i := range n {
				lo := float64(i)/float64(n) - 0.1 - 0.05*rnd.Float64()
				hi := float64(i+1)/float64(n) + 0.05 + 0.05*rnd.Float64()
				a[i] = math.Max(0, lo)
				b[i] = math.Min(1, hi)
			}
			// Enforce monotonicity.
			for i := 1; i < n; i++ {
				if a[i] < a[i-1] {
					a[i] = a[i-1]
				}
				if b[i] < b[i-1] {
					b[i] = b[i-1]
				}
			}
			pSteck, err := crossingProbabilitySteck(a, b)
			require.NoError(t, err)
			pMosc, err := crossingProbabilityMoscovich(a, b)
			require.NoError(t, err)
			assert.InDelta(t, pSteck, pMosc, 1e-6,
				"n=%d trial=%d: Steck=%v Moscovich=%v", n, trial, pSteck, pMosc)
		}
	}
}

// TestKSN1ClosedForm exercises the closed-form distribution of the
// two-sided KS statistic at n=1:
//
//	D_1 = max(U_{(1)}, 1 - U_{(1)})  ⇒  P(D_1 ≤ d) = 2d - 1 for d ≥ ½.
//
// The KS bands at threshold d are a_1 = 1 - d, b_1 = d (n=1). Both
// engines must reproduce 2d - 1.
func TestKSN1ClosedForm(t *testing.T) {
	for _, d := range []float64{0.55, 0.65, 0.75, 0.85, 0.95} {
		a := []float64{1 - d}
		b := []float64{d}
		want := 2*d - 1
		pS, err := crossingProbabilitySteck(a, b)
		require.NoError(t, err)
		assert.InDelta(t, want, pS, 1e-14, "Steck KS n=1 d=%v", d)
		pM, err := crossingProbabilityMoscovich(a, b)
		require.NoError(t, err)
		assert.InDelta(t, want, pM, 1e-12, "Moscovich KS n=1 d=%v", d)
	}
}

// TestCrossProbValidationRejectsBadInputs probes the dispatcher's
// validation path. Length mismatch, NaN entries, non-monotone bounds,
// and out-of-[0,1] entries must all yield errors instead of silently
// producing a meaningless probability.
func TestCrossProbValidationRejectsBadInputs(t *testing.T) {
	cases := []struct {
		name string
		a, b []float64
	}{
		{"length mismatch", []float64{0.1, 0.2}, []float64{0.5}},
		{"NaN lower", []float64{math.NaN(), 0.2}, []float64{0.5, 0.8}},
		{"NaN upper", []float64{0.1, 0.2}, []float64{0.5, math.NaN()}},
		{"out of range lower", []float64{-0.1, 0.2}, []float64{0.5, 0.8}},
		{"out of range upper", []float64{0.1, 0.2}, []float64{0.5, 1.5}},
		{"non-monotone lower", []float64{0.5, 0.2}, []float64{0.6, 0.8}},
		{"non-monotone upper", []float64{0.1, 0.2}, []float64{0.8, 0.5}},
	}
	for _, c := range cases {
		_, err := CrossingProbability(c.a, c.b, CrossingAlgorithmAuto)
		assert.Error(t, err, c.name)
	}
}

// TestCrossProbAutoDispatch picks the right engine based on n. At
// n ≤ steckN we get Steck's exact answer; above we get Moscovich.
// The two agree on the boundary case n = steckN.
func TestCrossProbAutoDispatch(t *testing.T) {
	const n = steckN
	a := make([]float64, n)
	b := make([]float64, n)
	for i := range n {
		b[i] = 1
	}
	pAuto, err := CrossingProbability(a, b, CrossingAlgorithmAuto)
	require.NoError(t, err)
	pMosc, err := CrossingProbability(a, b, CrossingAlgorithmMoscovich)
	require.NoError(t, err)
	assert.InDelta(t, pMosc, pAuto, 1e-6,
		"Auto vs Moscovich disagree at n=%d (steckN boundary)", n)
}

// TestSteckMonotoneInBandWidth checks an intuitive but non-trivial
// property: widening the band (uniformly relaxing the upper edge)
// must monotonically increase the probability. Numerical bugs in
// Hessenberg-Hyman commonly break this.
func TestSteckMonotoneInBandWidth(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	const n = 12
	a := make([]float64, n)
	b := make([]float64, n)
	for i := range n {
		p := float64(i+1) / float64(n)
		a[i] = math.Max(0, p-0.05)
		b[i] = math.Min(1, p+0.05)
	}
	pBase, err := crossingProbabilitySteck(a, b)
	require.NoError(t, err)
	for k := 0; k < 20; k++ {
		bWider := make([]float64, n)
		copy(bWider, b)
		for i := range bWider {
			bWider[i] = math.Min(1, bWider[i]+rnd.Float64()*0.02)
		}
		// Re-enforce monotonicity.
		for i := 1; i < n; i++ {
			if bWider[i] < bWider[i-1] {
				bWider[i] = bWider[i-1]
			}
		}
		pWider, err := crossingProbabilitySteck(a, bWider)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, pWider, pBase-1e-12,
			"non-monotone after widening: base=%v wider=%v", pBase, pWider)
	}
}
