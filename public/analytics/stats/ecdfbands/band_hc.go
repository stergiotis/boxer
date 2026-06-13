package ecdfbands

// higherCriticismFamily implements the Donoho & Jin (2004)
// higher-criticism statistic, restricted to the upper tail (positive
// deviations only — the symmetric two-sided variant is used here for
// confidence-band construction):
//
//	T_n^HC = max_{i=1..n-1} √n · (F_n(X_{(i)}) - i/n) / √((i/n)(1 - i/n))
//
// The acceptance region for the two-sided HC statistic at level c is
//
//	|U_{(i)} - p_i| ≤ c · √(p_i (1-p_i) / n)  for i = 1..n-1
//
// and U_{(n)} ∈ [0, 1] (the i = n term is excluded because the
// variance p_n(1-p_n) = 0 collapses the band to a point of measure
// zero — the maximum in the statistic itself omits this rank for
// the same reason). Crucially HC keeps *every* interior rank
// active; this is what distinguishes it from the Stepanova-Wang
// equal-precision band, which additionally trims ⌈log(n)⌉ ranks
// from each tail to bound the asymptotic weight.
//
// In finite samples the two methods produce visibly different bands:
// HC's tail bands are tight (narrowing toward p=0 and p=1 with the
// √(p(1-p)) factor), while EP's tails are unbounded. At α = 0.05
// the inversion correspondingly finds different critical values.
type higherCriticismFamily struct{}

func (higherCriticismFamily) name() string { return "highercriticism" }

func (higherCriticismFamily) boundaries(n int, c float64, lower, upper []float64) {
	for i := range n {
		p := float64(i+1) / float64(n)
		var lo, hi float64
		if i == n-1 {
			// p_n = 1 ⇒ variance = 0 ⇒ band collapses to a point of
			// measure zero. Widen to the trivial range so the i=n term
			// does not bind (mirrors the i=n exclusion in the
			// statistic itself).
			lo = 0
			hi = 1
		} else {
			lo, hi = varianceWeightedEdge(n, c, p)
		}
		lower[i] = lo
		upper[i] = hi
	}
	clampMonotone(lower, upper)
}

// bandAtP evaluates the HC band edge for arbitrary p ∈ [0, 1].
// Variance-weighted ε-strip across the full open unit interval;
// collapses to [0, 1] only at the closed endpoints where the
// variance term hits zero.
func (higherCriticismFamily) bandAtP(n int, c, p float64) (lo, hi float64) {
	if p <= 0 || p >= 1 {
		lo = 0
		hi = 1
		return
	}
	lo, hi = varianceWeightedEdge(n, c, p)
	return
}

// criticalValueBracket: the HC asymptotic null grows like √(2 log log n)
// (Jaeschke-Eicker theorem). For n up to 10⁵ and α down to 10⁻⁴ the
// finite-sample critical value stays below ~5. (0.1, 30) brackets the
// observed range across the full parameter envelope of this library.
func (higherCriticismFamily) criticalValueBracket(n int, alpha float64) (lo, hi float64) {
	_ = n
	_ = alpha
	lo = 0.1
	hi = 30
	return
}
