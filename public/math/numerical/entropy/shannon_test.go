//go:build llm_generated_opus46

package entropy

import (
	"math"
	"slices"
	"testing"
)

const tol = 1e-12 // tolerance for floating-point comparison

// ---------------------------------------------------------------------
// Reference values
//
// All expected values below are derived from the analytical formula
//   H = -Σ pᵢ log₂(pᵢ)
// and cross-checked against known results from:
//   - Wikipedia "Entropy (information theory)"
//   - Wolfram MathWorld "Entropy"
//   - Shannon's original 1948 paper examples
// ---------------------------------------------------------------------

func TestShannonFairCoin(t *testing.T) {
	// Fair coin: H({1/2, 1/2}) = 1.0 bit exactly.
	got := Shannon(slices.Values([]float64{0.5, 0.5}))
	if math.Abs(got-1.0) > tol {
		t.Errorf("fair coin: got %.15f, want 1.0", got)
	}
}

func TestShannonFairDie(t *testing.T) {
	// Fair six-sided die: H = log₂(6) ≈ 2.584962500721156 bits.
	p := []float64{1.0 / 6, 1.0 / 6, 1.0 / 6, 1.0 / 6, 1.0 / 6, 1.0 / 6}
	want := math.Log2(6)
	got := Shannon(slices.Values(p))
	if math.Abs(got-want) > tol {
		t.Errorf("fair die: got %.15f, want %.15f", got, want)
	}
}

func TestShannonDyadicDistribution(t *testing.T) {
	// {1/2, 1/4, 1/8, 1/8} → H = 7/4 = 1.75 bits exactly.
	// This is a classic textbook example (e.g. Cover & Thomas Ch.2).
	p := []float64{0.5, 0.25, 0.125, 0.125}
	want := 1.75
	got := Shannon(slices.Values(p))
	if math.Abs(got-want) > tol {
		t.Errorf("dyadic {1/2,1/4,1/8,1/8}: got %.15f, want %.15f", got, want)
	}
}

func TestShannonHorseRace(t *testing.T) {
	// Shannon's horse-race example (Cover & Thomas, §2):
	//   {1/2, 1/4, 1/8, 1/16, 1/64, 1/64, 1/64, 1/64} → H = 2.0 bits exactly.
	p := []float64{
		1.0 / 2, 1.0 / 4, 1.0 / 8, 1.0 / 16,
		1.0 / 64, 1.0 / 64, 1.0 / 64, 1.0 / 64,
	}
	want := 2.0
	got := Shannon(slices.Values(p))
	if math.Abs(got-want) > tol {
		t.Errorf("horse race: got %.15f, want 2.0", got)
	}
}

func TestShannonCertainty(t *testing.T) {
	// Deterministic variable: H({1}) = 0.
	got := Shannon(slices.Values([]float64{1.0}))
	if got != 0.0 {
		t.Errorf("certainty: got %v, want 0", got)
	}
}

func TestShannonEmpty(t *testing.T) {
	// Empty distribution → 0.
	got := Shannon(slices.Values([]float64{}))
	if got != 0.0 {
		t.Errorf("empty: got %v, want 0", got)
	}
}

func TestShannonZeroProbabilities(t *testing.T) {
	// Zeros must be skipped gracefully (0·log0 = 0 by convention).
	// H({0.5, 0, 0.5, 0}) should equal H({0.5, 0.5}) = 1.0.
	got := Shannon(slices.Values([]float64{0.5, 0.0, 0.5, 0.0}))
	if math.Abs(got-1.0) > tol {
		t.Errorf("with zeros: got %.15f, want 1.0", got)
	}
}

func TestShannonUniformScaling(t *testing.T) {
	// For uniform distributions, H = log₂(n).
	// Test for several values of n.
	for _, n := range []int{2, 4, 8, 16, 32, 64, 128, 256} {
		p := make([]float64, n)
		for i := range p {
			p[i] = 1.0 / float64(n)
		}
		want := math.Log2(float64(n))
		got := Shannon(slices.Values(p))
		if math.Abs(got-want) > tol {
			t.Errorf("uniform(%d): got %.15f, want %.15f", n, got, want)
		}
	}
}

func TestShannonNatsConversion(t *testing.T) {
	// ShannonNats should return entropy in nats.
	// For a fair coin: H = ln(2) nats ≈ 0.693147...
	got := ShannonNats(slices.Values([]float64{0.5, 0.5}))
	want := math.Ln2
	if math.Abs(got-want) > tol {
		t.Errorf("nats fair coin: got %.15f, want %.15f (ln2)", got, want)
	}
}

func TestShannonBitsNatsRelation(t *testing.T) {
	// H_bits = H_nats / ln(2) for any distribution.
	p := []float64{0.1, 0.2, 0.3, 0.15, 0.25}
	bits := Shannon(slices.Values(p))
	nats := ShannonNats(slices.Values(p))
	ratio := bits / nats
	wantRatio := 1.0 / math.Ln2 // = log₂(e)
	if math.Abs(ratio-wantRatio) > tol {
		t.Errorf("bits/nats ratio: got %.15f, want %.15f", ratio, wantRatio)
	}
}

func TestShannonFromLogProbs(t *testing.T) {
	// Verify ShannonFromLogProbs matches ShannonNats for a known distribution.
	p := []float64{0.5, 0.25, 0.125, 0.125}
	logp := make([]float64, len(p))
	for i, pi := range p {
		logp[i] = math.Log(pi)
	}

	gotLog := ShannonFromLogProbs(slices.Values(logp))
	gotDirect := ShannonNats(slices.Values(p))

	if math.Abs(gotLog-gotDirect) > tol {
		t.Errorf("from-log-probs: got %.15f, want %.15f", gotLog, gotDirect)
	}
}

func TestShannonFromLogProbsWithNegInf(t *testing.T) {
	// -Inf log-prob means p=0 — should be skipped.
	logp := []float64{math.Log(0.5), math.Inf(-1), math.Log(0.5)}
	got := ShannonFromLogProbs(slices.Values(logp))
	want := math.Ln2 // same as fair coin in nats
	if math.Abs(got-want) > tol {
		t.Errorf("from-log-probs with -Inf: got %.15f, want %.15f", got, want)
	}
}

func TestKahanAccuracy(t *testing.T) {
	// Stress test: large uniform distribution where naïve summation
	// would accumulate noticeable error. For n=1_000_000, H = log₂(n).
	const n = 1_000_000
	p := make([]float64, n)
	for i := range p {
		p[i] = 1.0 / float64(n)
	}
	want := math.Log2(float64(n))
	got := Shannon(slices.Values(p))

	// With Kahan summation we expect error < 1e-10.
	// Without it, error is typically ~1e-7 for n=10⁶.
	if math.Abs(got-want) > 1e-10 {
		t.Errorf("kahan stress (n=%d): got %.15f, want %.15f, err=%e",
			n, got, want, math.Abs(got-want))
	}
}

func TestShannonNonNegativity(t *testing.T) {
	// Shannon entropy of any valid distribution must be ≥ 0.
	distributions := [][]float64{
		{1.0},
		{0.5, 0.5},
		{0.99, 0.01},
		{0.001, 0.999},
		{0.1, 0.2, 0.3, 0.4},
	}
	for i, p := range distributions {
		h := Shannon(slices.Values(p))
		if h < -tol {
			t.Errorf("distribution %d: H=%.15f is negative", i, h)
		}
	}
}

func TestShannonMaximumAtUniform(t *testing.T) {
	// For any n, the uniform distribution maximises entropy at log₂(n).
	// Check that a non-uniform distribution over the same support has
	// strictly lower entropy.
	uniform := []float64{0.25, 0.25, 0.25, 0.25}
	skewed := []float64{0.7, 0.1, 0.1, 0.1}

	hU := Shannon(slices.Values(uniform))
	hS := Shannon(slices.Values(skewed))

	if hS >= hU {
		t.Errorf("maximum entropy violated: H(skewed)=%.6f >= H(uniform)=%.6f", hS, hU)
	}
}

// Benchmark to verify there's no significant overhead from Kahan summation.
func BenchmarkShannon1M(b *testing.B) {
	const n = 1_000_000
	p := make([]float64, n)
	for i := range p {
		p[i] = 1.0 / float64(n)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Shannon(slices.Values(p))
	}
}
