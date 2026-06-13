package ecdfbands

import (
	"math"
)

// berkJonesFamily implements the Berk & Jones (1979) test statistic
//
//	T_n^BJ = max_{i=1..n} n · D(p_i ‖ U_{(i)})  with  p_i = i/n
//
// and D the binomial Kullback-Leibler divergence. The critical value
// c parametrising the per-rank acceptance region is the level of T_n^BJ
// itself: U_{(i)} is in the band iff n · D(p_i ‖ U_{(i)}) ≤ c.
//
// Because D(p_i ‖ q) is strictly convex in q with a unique minimum at
// q = p_i and explodes at q ∈ {0, 1} for interior p_i, the band edges
// are well-defined as the two roots of n · D(p_i ‖ q) = c on either
// side of p_i. We find them by bisection in q-space — about 50
// iterations each suffice to reach 1e-15 relative tolerance.
type berkJonesFamily struct{}

func (berkJonesFamily) name() string { return "berkjones" }

func (berkJonesFamily) boundaries(n int, c float64, lower, upper []float64) {
	if n <= 0 {
		return
	}
	target := c / float64(n)
	for i := 1; i <= n; i++ {
		p := float64(i) / float64(n)
		lower[i-1] = bjLowerEdge(p, target)
		upper[i-1] = bjUpperEdge(p, target)
	}
	clampMonotone(lower, upper)
}

// bjLowerEdge returns the unique q ∈ (0, p] solving D(p ‖ q) = target.
//
// D(p ‖ q) is monotonically decreasing in q on (0, p]: at q → 0 it
// diverges to +∞, at q = p it equals 0. For target > 0 the equation
// has a unique root strictly inside (0, p), found by bisection.
//
// target == 0 returns p (the trivial band collapses to a point).
// target +∞ returns 0 (the band reaches the left endpoint of the
// open unit interval — represented as 0 here).
func bjLowerEdge(p, target float64) float64 {
	if target <= 0 {
		return p
	}
	if math.IsInf(target, 1) {
		return 0
	}
	lo := 0.0
	hi := p
	// Make sure D(p ‖ lo) ≥ target (it does at lo=0: D = +∞).
	// Bisect 60 iterations to reach ~1e-18 in q (double precision
	// can't distinguish finer than that anyway).
	for range 60 {
		mid := 0.5 * (lo + hi)
		if mid == lo || mid == hi {
			break
		}
		d := binomKL(p, mid)
		if d > target {
			lo = mid
		} else {
			hi = mid
		}
	}
	return 0.5 * (lo + hi)
}

// bjUpperEdge returns the unique q ∈ [p, 1) solving D(p ‖ q) = target.
//
// D(p ‖ q) is monotonically increasing in q on [p, 1), bisection
// converges; edge cases mirror bjLowerEdge.
func bjUpperEdge(p, target float64) float64 {
	if target <= 0 {
		return p
	}
	if math.IsInf(target, 1) {
		return 1
	}
	lo := p
	hi := 1.0
	for range 60 {
		mid := 0.5 * (lo + hi)
		if mid == lo || mid == hi {
			break
		}
		d := binomKL(p, mid)
		if d > target {
			hi = mid
		} else {
			lo = mid
		}
	}
	return 0.5 * (lo + hi)
}

// clampMonotone enforces non-decreasing lower/upper sequences and
// the lower[i] ≤ upper[i] invariant. The Berk-Jones band by
// construction satisfies both for c that is the same across ranks,
// but tiny ulp-level drift in the bisection endpoints can violate
// non-strict monotonicity; this brushes those off.
func clampMonotone(lower, upper []float64) {
	for i := 1; i < len(lower); i++ {
		if lower[i] < lower[i-1] {
			lower[i] = lower[i-1]
		}
		if upper[i] < upper[i-1] {
			upper[i] = upper[i-1]
		}
	}
	for i, lo := range lower {
		if lo > upper[i] {
			lower[i] = upper[i]
		}
	}
}

// bandAtP evaluates the BJ band edge for arbitrary p ∈ [0, 1]. At
// the boundaries the binomial KL inverts in closed form:
// D(0, q) = -log(1-q) gives q ≤ 1 - exp(-c/n); D(1, q) = -log q
// gives q ≥ exp(-c/n). Interior p uses the same bisection helpers
// as boundaries().
func (berkJonesFamily) bandAtP(n int, c, p float64) (lo, hi float64) {
	target := c / float64(n)
	switch {
	case p <= 0:
		lo = 0
		hi = 1 - math.Exp(-target)
	case p >= 1:
		lo = math.Exp(-target)
		hi = 1
	default:
		lo = bjLowerEdge(p, target)
		hi = bjUpperEdge(p, target)
	}
	return
}

// criticalValueBracket returns a generous bracket for the BJ critical
// value at the given (n, α). The asymptotic distribution under the
// null is heavy-tailed: c_n grows like log log n (Donoho-Jin 2004).
// For n up to 10⁵ and α down to 10⁻⁴, c stays below 10. We bracket
// (0.1, 50) to leave headroom on both ends.
func (berkJonesFamily) criticalValueBracket(n int, alpha float64) (lo, hi float64) {
	_ = n
	_ = alpha
	lo = 0.1
	hi = 50
	return
}
