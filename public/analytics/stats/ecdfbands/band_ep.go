//go:build llm_generated_opus47

package ecdfbands

import (
	"math"
)

// equalPrecisionFamily implements the Stepanova & Wang (2008)
// weighted-KS / equal-precision band. The statistic is
//
//	T_n^EP = sup_{t ∈ [η_n, 1-η_n]} √n |F_n(t) - F(t)| / √(F(t)(1-F(t)))
//
// where η_n > 0 trims a vanishing fraction of the tails to keep the
// weight √(F(1-F)) bounded away from zero. Under U(0,1) the band
// acceptance region at level c is
//
//	|U_{(i)} - p_i| ≤ c · √(p_i (1-p_i) / n)  for p_i ∈ [η_n, 1-η_n]
//
// and the trivial U_{(i)} ∈ [0, 1] outside the trimmed central
// interval. The asymmetric square-root width gives uniform precision:
// same fractional miss tolerance at p_i = 0.5 and p_i = 0.1.
//
// We use η_n = log(n)/n. This is the standard Stepanova-Wang trim
// rate — vanishing as n → ∞ (so the asymptotic envelope coincides
// with the un-trimmed weighted KS) but visibly non-zero at finite n,
// trimming ⌈log(n)⌉ ranks from each tail. For n = 80, log(80) ≈ 4.4
// → trim 5 ranks from each end; for n = 10⁴, log(10⁴) ≈ 9.2 → trim
// 10 from each. This is what distinguishes EP from HC at the
// boundary-geometry level (HC keeps every interior rank active).
type equalPrecisionFamily struct{}

func (equalPrecisionFamily) name() string { return "equalprecision" }

func (equalPrecisionFamily) boundaries(n int, c float64, lower, upper []float64) {
	eta := epEta(n)
	sqrtN := math.Sqrt(float64(n))
	for i := range n {
		p := float64(i+1) / float64(n)
		var lo, hi float64
		if p < eta || p > 1-eta {
			lo = 0
			hi = 1
		} else {
			w := c * math.Sqrt(p*(1-p)) / sqrtN
			lo = math.Max(0, p-w)
			hi = math.Min(1, p+w)
		}
		lower[i] = lo
		upper[i] = hi
	}
	clampMonotone(lower, upper)
}

// bandAtP evaluates the EP band edge for arbitrary p ∈ [0, 1].
// Inside the central trimmed interval [η, 1-η] the band is the
// variance-weighted ε-strip; outside it reverts to the trivial
// [0, 1]. The η used here matches the discrete `boundaries`
// implementation so streaming evaluation and per-rank evaluation
// agree at the rank lattice.
func (equalPrecisionFamily) bandAtP(n int, c, p float64) (lo, hi float64) {
	eta := epEta(n)
	if p <= eta || p >= 1-eta {
		lo = 0
		hi = 1
		return
	}
	w := c * math.Sqrt(p*(1-p)) / math.Sqrt(float64(n))
	lo = math.Max(0, p-w)
	hi = math.Min(1, p+w)
	return
}

// epEta returns the per-n EP tail trim level. Caps at 0.5 so the
// active central interval [η, 1-η] is never empty.
func epEta(n int) float64 {
	if n <= 1 {
		return 0.5
	}
	eta := math.Log(float64(n)) / float64(n)
	if eta > 0.5 {
		eta = 0.5
	}
	return eta
}

// criticalValueBracket: the asymptotic null of T_n^EP is (Darling 1955)
// related to the supremum of the standardised Brownian bridge; for
// α = 0.05, c_∞ ≈ 2.8 — but for finite n the value is larger due to
// the discrete log-log envelope. (0.5, 30) is a defensive bracket.
func (equalPrecisionFamily) criticalValueBracket(n int, alpha float64) (lo, hi float64) {
	_ = n
	_ = alpha
	lo = 0.5
	hi = 30
	return
}
