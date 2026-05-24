//go:build llm_generated_opus47

package ecdfbands

import (
	"math"
)

// negInf is the IEEE-754 negative infinity, stored once so callers can
// compare against it without recomputing math.Inf(-1) at every site.
var negInf = math.Inf(-1)

// logSumExpPair returns log(exp(a) + exp(b)) without overflowing
// intermediate exp() calls. Handles -Inf inputs (which represent
// "probability 0" in log-space): two -Inf inputs return -Inf; a single
// -Inf returns the other operand.
func logSumExpPair(a, b float64) float64 {
	if a == negInf {
		return b
	}
	if b == negInf {
		return a
	}
	if a > b {
		return a + math.Log1p(math.Exp(b-a))
	}
	return b + math.Log1p(math.Exp(a-b))
}

// logSumExpSlice returns log(Σ exp(xs[i])) using the max-shift trick.
// Empty input returns -Inf (additive identity for log-space sums).
// Entries equal to -Inf are skipped; if all entries are -Inf the
// result is -Inf.
func logSumExpSlice(xs []float64) float64 {
	if len(xs) == 0 {
		return negInf
	}
	m := negInf
	for _, x := range xs {
		if x > m {
			m = x
		}
	}
	if m == negInf {
		return negInf
	}
	var s float64
	for _, x := range xs {
		if x == negInf {
			continue
		}
		s += math.Exp(x - m)
	}
	return m + math.Log(s)
}

// logFactorial returns log(n!) for n ≥ 0. Uses math.Lgamma (Γ(n+1) = n!)
// which is accurate to ~14 digits for the range needed here. n < 0
// returns NaN.
func logFactorial(n int) float64 {
	if n < 0 {
		return math.NaN()
	}
	if n < 2 {
		return 0
	}
	lg, _ := math.Lgamma(float64(n + 1))
	return lg
}

// logPoissonPMF returns log P(N(t) = k) for a unit-rate Poisson process
// observed for duration t (equivalently, Poisson distribution with
// mean t). t must be ≥ 0.
//
// Edge cases:
//
//   - t == 0, k == 0: returns 0 (probability 1).
//   - t == 0, k > 0:  returns -Inf.
//   - t > 0, k == 0:  returns -t.
//   - t > 0, k > 0:   returns k·log t - t - logΓ(k+1).
//
// Returns NaN for negative t or k.
func logPoissonPMF(k int, t float64) float64 {
	if k < 0 || t < 0 || math.IsNaN(t) {
		return math.NaN()
	}
	if t == 0 {
		if k == 0 {
			return 0
		}
		return negInf
	}
	if k == 0 {
		return -t
	}
	return float64(k)*math.Log(t) - t - logFactorial(k)
}

// binomKL returns the binomial KL divergence D(p ‖ q) directly.
// Handles boundary cases via the standard 0·log 0 = 0 convention.
//
// Returns:
//
//   - 0   when p == q (any q ∈ [0, 1]).
//   - +Inf when q ∈ {0, 1} and p is in the open interval not containing q
//     (e.g. q = 0, p > 0 ⇒ D = +Inf).
//   - NaN if p or q lies outside [0, 1] or is NaN.
//
// Numerical layout: when p and q are both strictly interior, we
// compute the divergence in its direct form. Loss of precision near
// p ≈ q is acceptable here because Berk-Jones boundary computation
// uses this function only with q on the *opposite* side of i/n from
// the median, never with q ≈ p.
func binomKL(p, q float64) float64 {
	if math.IsNaN(p) || math.IsNaN(q) || p < 0 || p > 1 || q < 0 || q > 1 {
		return math.NaN()
	}
	if p == q {
		return 0
	}
	// q == 0: D = (1-p) log((1-p)/1) + p log(p/0). For p > 0, term2 = +∞.
	// For p == 0, p log(p/q) = 0 by convention and (1-p) log(1) = 0, so D = 0
	// — handled by the p==q branch above. So q == 0 here implies p > 0.
	if q == 0 || q == 1 {
		return math.Inf(1)
	}
	// p == 0: D = log(1/(1-q)) = -log(1-q).
	if p == 0 {
		return -math.Log1p(-q)
	}
	// p == 1: D = log(1/q) = -log q.
	if p == 1 {
		return -math.Log(q)
	}
	return p*math.Log(p/q) + (1-p)*math.Log((1-p)/(1-q))
}
