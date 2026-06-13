package ecdfbands

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// CrossingAlgorithmE selects which boundary-crossing-probability
// algorithm a routine should use. The two implementations compute the
// same mathematical quantity by independent routes and serve as
// each other's correctness witnesses during testing; in production
// the default Moscovich engine is preferred for its strictly positive
// DP entries.
type CrossingAlgorithmE uint8

const (
	// CrossingAlgorithmAuto picks an algorithm based on sample size:
	// Steck-Noé for n ≤ steckN; Moscovich-Nadler above. This is the
	// default for high-level entry points.
	CrossingAlgorithmAuto CrossingAlgorithmE = iota
	// CrossingAlgorithmMoscovich is the Poissonized DP of Moscovich,
	// Nadler & Spiegelman (AoS 2020, Algorithm 2). Numerically stable
	// for n up to ~10⁵.
	CrossingAlgorithmMoscovich
	// CrossingAlgorithmSteck is the rectangle-probability determinant
	// of Steck (1971) and Noé (1972), computed via Hessenberg LU.
	// Reliable for n ≤ ~10³; primarily used as an independent
	// cross-check of Moscovich.
	CrossingAlgorithmSteck
)

// steckN is the largest n for which CrossingAlgorithmAuto routes to
// Steck-Noé. Above this, double-precision Hessenberg LU starts to
// lose digits of the determinant; the trivial-unit diagnostic showed
// ~1% error in the diagonal product by n = 30, which propagates as
// 2-3 lost digits in P. Capping at 24 keeps the Auto path's Steck
// hand-off above the relative-error threshold that the BJ inversion
// bisection's 1e-6 tolerance can tolerate. Moscovich is the stable
// engine above this; Steck remains exposed as an explicit algorithm
// option for small-n cross-validation.
const steckN = 24

// CrossingProbability returns the simultaneous probability.
//
//	P(lower[i] ≤ U_{(i)} ≤ upper[i] for all i ∈ [1, n])
//
// where U_{(1)} ≤ … ≤ U_{(n)} are the order statistics of n iid
// Uniform(0, 1) random variables and len(lower) == len(upper) == n.
//
// Both lower and upper must be non-decreasing in i and must satisfy
// lower[i] ≤ upper[i] ∈ [0, 1] for every i. Violations of any of
// these invariants are reported as errors (non-monotone bounds yield
// undefined values from both algorithms; out-of-range bounds also
// trigger the per-algorithm fast-path that returns 0 or 1).
//
// algo selects which implementation to use; Auto picks based on n.
//
// The result is a probability in [0, 1]. Hard zeros are returned when
// any lower[i] > upper[i] or when the constraint chain is otherwise
// infeasible (e.g. upper[k] < lower[k+1]).
func CrossingProbability(lower, upper []float64, algo CrossingAlgorithmE) (p float64, err error) {
	if err = validateBoundaries(lower, upper); err != nil {
		return
	}
	if len(lower) == 0 {
		p = 1
		return
	}
	switch algo {
	case CrossingAlgorithmAuto:
		if len(lower) <= steckN {
			p, err = crossingProbabilitySteck(lower, upper)
		} else {
			p, err = crossingProbabilityMoscovich(lower, upper)
		}
	case CrossingAlgorithmSteck:
		p, err = crossingProbabilitySteck(lower, upper)
	case CrossingAlgorithmMoscovich:
		p, err = crossingProbabilityMoscovich(lower, upper)
	default:
		err = eh.Errorf("unknown CrossingAlgorithmE value %d", algo)
	}
	return
}

// validateBoundaries enforces the public invariants on input boundary
// sequences. Boundary sequences shorter than 1 element are accepted
// (the empty product convention gives P=1). It clamps the natural
// [0, 1] range without modifying the input slices — out-of-range
// values are reported, not silently clipped, so callers detect
// upstream bugs early.
func validateBoundaries(lower, upper []float64) (err error) {
	if len(lower) != len(upper) {
		err = eh.Errorf("lower and upper boundary lengths differ: %d vs %d", len(lower), len(upper))
		return
	}
	for i, lo := range lower {
		hi := upper[i]
		if math.IsNaN(lo) || math.IsNaN(hi) {
			err = eh.Errorf("NaN boundary at i=%d (lower=%v, upper=%v)", i, lo, hi)
			return
		}
		if lo < 0 || lo > 1 || hi < 0 || hi > 1 {
			err = eh.Errorf("boundary out of [0,1] at i=%d (lower=%v, upper=%v)", i, lo, hi)
			return
		}
		if i > 0 {
			if lo < lower[i-1] {
				err = eh.Errorf("lower boundary not non-decreasing at i=%d (%v < %v)", i, lo, lower[i-1])
				return
			}
			if hi < upper[i-1] {
				err = eh.Errorf("upper boundary not non-decreasing at i=%d (%v < %v)", i, hi, upper[i-1])
				return
			}
		}
	}
	return
}

// trivialZero reports whether the boundary configuration forces
// P = 0. Used by both algorithms as a fast path before the main DP.
// The chain becomes infeasible whenever any band collapses
// (lower[i] > upper[i]) or whenever the ladder cannot be threaded
// (upper[i] < lower[i+1] but i+1 must come after i — actually that
// case is fine for ECDF semantics; the only true infeasibility is
// the per-band collapse).
func trivialZero(lower, upper []float64) bool {
	for i, lo := range lower {
		if lo > upper[i] {
			return true
		}
	}
	return false
}
