//go:build llm_generated_opus47

package ecdfbands

// BandMethodE selects the test statistic whose acceptance region
// defines the simultaneous confidence band. Each value names a
// peer-reviewed band family; the inversion machinery in invert.go
// turns the family's critical-value parametrisation into finite-sample
// exact bands at the target confidence level.
//
// Adding a new family is a four-step rite:
//
//  1. Define a new constant in this enum.
//  2. Add a bandFamily implementation in band_<key>.go.
//  3. Register the implementation in bandFamilyDispatch.
//  4. Cover it in the reference-value tests.
type BandMethodE uint8

const (
	// BandMethodBerkJones picks the Berk & Jones (1979) statistic.
	//
	//	T_n^BJ = max_i n · D(i/n ‖ U_{(i)})
	//
	// where D is the binomial Kullback-Leibler divergence. Tail-tight
	// — dominates the Kolmogorov-Smirnov statistic in the Bahadur
	// sense. Default for general-purpose use.
	BandMethodBerkJones BandMethodE = iota + 1
	// BandMethodDKW picks the Dvoretzky-Kiefer-Wolfowitz statistic.
	//
	//	T_n^DKW = sup_x √n |F_n(x) - F(x)|
	//
	// with Massart's (1990) tight constant. Bands are symmetric
	// ε-strips around i/n; ε has the closed form
	// √(ln(2/α) / (2n)).
	BandMethodDKW
	// BandMethodEqualPrecision picks the Stepanova & Wang (2008)
	// weighted KS statistic.
	//
	//	T_n^EP = sup_t |√n (F_n(t)-F(t))| / √(F(t)(1-F(t)))
	//
	// restricted to a central interval t ∈ [η, 1-η]. Uniform
	// precision across F; better in tails than DKW but less
	// aggressive than BJ.
	BandMethodEqualPrecision
	// BandMethodHigherCriticism picks the Donoho & Jin (2004)
	// higher-criticism statistic.
	//
	//	T_n^HC = max_i √n (F_n(X_{(i)}) - i/n) / √((i/n)(1 - i/n))
	//
	// Adaptive in the tails — extremely sensitive to sparse
	// heterogeneous departures at small or large i/n.
	BandMethodHigherCriticism
)

// bandFamilyI is the internal contract every band family implements.
// External callers do not see this interface; they select a family by
// BandMethodE value and the dispatcher hands the right
// implementation to the inversion routine.
type bandFamilyI interface {
	// name returns a stable identifier, used in error messages and
	// cache keys.
	name() string

	// boundaries fills lower/upper (each length n; allocated by
	// caller) with the per-rank band edges at critical value c.
	// Both result slices must satisfy 0 ≤ lower[i] ≤ upper[i] ≤ 1
	// and be non-decreasing in i — the postcondition required by
	// the crossing-probability engines.
	boundaries(n int, c float64, lower, upper []float64)

	// bandAtP returns the band edge (lo, hi) at an arbitrary
	// p ∈ [0, 1] — the continuous-p analogue of boundaries. Used
	// by BandsForGrid for grid points where F_n is fractional and
	// does not coincide with the integer rank lattice. Edge
	// handling at p ∈ {0, 1} is per-family.
	bandAtP(n int, c, p float64) (lo, hi float64)

	// criticalValueBracket returns an initial (lo, hi) interval on c
	// known to bracket the target alpha critical value for the given
	// n. The outer root-finder bisects within this range; the bracket
	// is best-effort generous — a band family that cannot guarantee
	// containment must return a wide (lo, hi) and accept some
	// initial bisection waste.
	criticalValueBracket(n int, alpha float64) (lo, hi float64)
}

// bandFamilyDispatch maps a BandMethodE to its implementation. Lookup
// failure (unknown method) returns nil — callers must guard.
func bandFamilyDispatch(method BandMethodE) bandFamilyI {
	switch method {
	case BandMethodBerkJones:
		return berkJonesFamily{}
	case BandMethodDKW:
		return dkwFamily{}
	case BandMethodEqualPrecision:
		return equalPrecisionFamily{}
	case BandMethodHigherCriticism:
		return higherCriticismFamily{}
	default:
		return nil
	}
}
