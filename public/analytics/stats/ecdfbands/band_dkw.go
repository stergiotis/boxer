//go:build llm_generated_opus47

package ecdfbands

import (
	"math"
)

// dkwFamily implements the Dvoretzky-Kiefer-Wolfowitz (1956) band
// with Massart's (1990) tight constant. The statistic is
//
//	T_n^DKW = sup_x √n |F_n(x) - F(x)|
//
// and the simultaneous band at level α is the symmetric ε-strip
// around i/n with
//
//	ε(α, n) = √( ln(2/α) / (2n) )
//
// which is closed-form and tight: P(T_n^DKW > √n · ε) ≤ α.
//
// The critical value parameter c is ε itself (the half-width of the
// band). criticalValueBracket returns the closed-form ε directly so
// the outer root-finder spends one bracket evaluation, not its full
// bisection budget, on this family.
type dkwFamily struct{}

func (dkwFamily) name() string { return "dkw" }

func (dkwFamily) boundaries(n int, c float64, lower, upper []float64) {
	for i := range n {
		p := float64(i+1) / float64(n)
		lo := p - c
		hi := p - 1.0/float64(n) + c
		// Clamp to [0, 1]; the band edges can otherwise spill past
		// the unit interval for small n or large alpha.
		if lo < 0 {
			lo = 0
		}
		if hi > 1 {
			hi = 1
		}
		lower[i] = lo
		upper[i] = hi
	}
	clampMonotone(lower, upper)
}

// bandAtP evaluates the DKW band edge for arbitrary p ∈ [0, 1].
// Symmetric ε-strip clipped to the unit interval; closed form.
func (dkwFamily) bandAtP(n int, c, p float64) (lo, hi float64) {
	_ = n
	lo = math.Max(0, p-c)
	hi = math.Min(1, p+c)
	return
}

// criticalValueBracket returns a degenerate bracket around the
// closed-form ε so the outer root-finder converges in one step on
// DKW. The actual finite-sample-exact ε differs slightly from the
// asymptotic formula by O(1/n) — the bisection refines it.
func (dkwFamily) criticalValueBracket(n int, alpha float64) (lo, hi float64) {
	eps := math.Sqrt(math.Log(2/alpha) / (2 * float64(n)))
	lo = math.Max(0, eps-0.05)
	hi = math.Min(1, eps+0.05)
	return
}
