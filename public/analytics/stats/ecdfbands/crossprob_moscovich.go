//go:build llm_generated_opus47

package ecdfbands

import (
	"math"
	"slices"
)

// crossingProbabilityMoscovich implements the Poissonized DP of
// Moscovich, Nadler & Spiegelman (Annals of Statistics, 2020),
// Algorithm 2. For n iid Uniform(0, 1) order statistics
// U_{(1)} ≤ … ≤ U_{(n)}, we want
//
//	P_OS(a, b) = P(a_i ≤ U_{(i)} ≤ b_i ∀ i).
//
// Poissonization: let N be a unit-rate Poisson process on [0, 1] and
// define the band-crossing event for the Poisson counting function as
//
//	L(t) ≤ N(t) ≤ H(t)  ∀ t ∈ [0, 1]
//
// where L(t) = #{i : b_i ≤ t} and H(t) = #{i : a_i ≤ t} are
// right-continuous step functions. Conditioning on N(1) = n recovers
// the order-statistic probability via
//
//	P_OS = P_Poisson(N(1) = n, band held) / P(N(1) = n)
//	     = π_m(n) · e · n!
//
// where π_k(j) = P(N(τ_k) = j AND band held on [0, τ_k]) and τ_0 = 0,
// τ_m = 1 enumerate the sorted boundary jump times (plus the
// endpoints).
//
// The DP propagates π through each constant-bound interval via a
// Poisson convolution. Because N is monotone and the bounds inside
// every interval are constant integers, the in-interval survival
// requirement collapses to "start and end inside [L_k, H_k]"; no
// intermediate state is required. All π entries stay strictly
// non-negative throughout — the algorithm is immune to the
// catastrophic cancellation that makes the Steck-Noé determinant
// unusable past n ≈ 50 in double precision.
//
// Complexity: at most 2n+2 breakpoints, each step propagates between
// state ranges of size ≤ n+1, with each new entry summing ≤ n+1
// terms. Worst-case O(n³); for typical Berk-Jones / DKW bands the
// active range is O(√n log n) and the runtime tracks closer to
// O(n²).
//
// We carry every π entry as its natural logarithm with a logSumExp
// reduction over the convolution sum; this keeps the dynamic range
// manageable up to n on the order of 10⁵.
func crossingProbabilityMoscovich(a, b []float64) (p float64, err error) {
	n := len(a)
	if n == 0 {
		p = 1
		return
	}
	if trivialZero(a, b) {
		p = 0
		return
	}

	// Memoise log(k!) up to k = n so the O(n²) logPoissonPMF calls and
	// the final log(n!) below are table reads, not math.Lgamma calls.
	ensureLogFactorials(n)

	taus := buildBreakpoints(a, b)
	// π carries log P(N(τ_k) = j AND band held). Index j ∈ [0, n].
	// Use -Inf to mark structurally impossible states. (slices.Fill
	// would be more idiomatic but isn't in the std slices package as
	// of Go 1.24 — only added to x/exp/slices.)
	logPi := make([]float64, n+1)
	for i := range logPi {
		logPi[i] = negInf
	}
	logPi[0] = 0 // P(N(0) = 0) = 1

	// Previous bounds on the state range (for trimming at each step).
	prevLo, prevHi := 0, 0

	// Pre-allocated scratch buffers. `scratchPi` holds the new logPi
	// values for one propagation step; `terms` is the running
	// log-sum-exp accumulator for one (k, j) cell. Both are capped at
	// n+1 — the maximum possible state range. Lifted out of the per-
	// step loops to keep allocation count O(1) total instead of O(n²).
	scratchPi := make([]float64, n+1)
	terms := make([]float64, 0, n+1)

	// idxA / idxB amortise the boundary counts across the τ-sweep:
	// since a, b, and taus are all non-decreasing, the count of
	// entries ≤ τ_{k-1} grows monotonically with k, so two pointers
	// give O(n) total across the loop instead of O(n) per step.
	idxA, idxB := 0, 0
	for k := 1; k < len(taus); k++ {
		delta := taus[k] - taus[k-1]
		// Bounds during (τ_{k-1}, τ_k]: based on which b's and a's
		// satisfy ≤ τ_{k-1} (the right limit of the previous jump).
		t := taus[k-1]
		for idxA < n && a[idxA] <= t {
			idxA++
		}
		for idxB < n && b[idxB] <= t {
			idxB++
		}
		loK := idxB
		hiK := idxA
		if hiK < loK {
			// Infeasible bounds inside the interval — band is broken.
			p = 0
			return
		}

		// Trim previous state to the intersection of its valid range
		// [prevLo, prevHi] and the new range [loK, hiK].
		trimLo := max(prevLo, loK)
		trimHi := min(prevHi, hiK)
		if trimLo > trimHi {
			p = 0
			return
		}

		// New state range is [loK, hiK]. Compute scratchPi[j] for
		// each j in that range by summing previous states i ∈
		// [trimLo, min(j, trimHi)]:
		//
		//   scratchPi[j] = logSumExp_i ( logPi[i] + logPoisson(j-i; Δ) )
		for j := loK; j <= hiK; j++ {
			iMax := min(j, trimHi)
			if iMax < trimLo {
				scratchPi[j] = negInf
				continue
			}
			terms = terms[:0]
			for i := trimLo; i <= iMax; i++ {
				if logPi[i] == negInf {
					continue
				}
				terms = append(terms, logPi[i]+logPoissonPMF(j-i, delta))
			}
			scratchPi[j] = logSumExpSlice(terms)
		}
		// Copy scratchPi back into logPi, zeroing entries outside [loK, hiK].
		for j := 0; j <= n; j++ {
			if j < loK || j > hiK {
				logPi[j] = negInf
			} else {
				logPi[j] = scratchPi[j]
			}
		}
		prevLo, prevHi = loK, hiK
	}

	// Final step: condition on N(1) = n.
	if n < prevLo || n > prevHi {
		p = 0
		return
	}
	logPiN := logPi[n]
	if logPiN == negInf {
		p = 0
		return
	}
	// P_OS = π_m(n) / P(N(1) = n) = π_m(n) · e^1 · n!.
	logP := logPiN + 1 + logFactorial(n)
	if logP > 0 {
		// Tiny over-by-one-ulp slipping past the rounding contract.
		p = 1
		return
	}
	p = math.Exp(logP)
	return
}

// buildBreakpoints returns the sorted, deduplicated list of breakpoints
// at which the boundary step functions L(t) or H(t) jump, plus the
// endpoints 0 and 1. The result is monotonically non-decreasing with
// length ≤ 2n + 2.
func buildBreakpoints(a, b []float64) []float64 {
	taus := make([]float64, 0, 2*len(a)+2)
	taus = append(taus, 0)
	taus = append(taus, a...)
	taus = append(taus, b...)
	taus = append(taus, 1)
	slices.Sort(taus)
	// Deduplicate adjacent equal entries; small numerical ties at
	// boundary clips collapse cleanly. We do not skip the start/end
	// endpoints — if a_1 = 0 we want a single entry at 0; if b_n = 1
	// likewise.
	out := taus[:0]
	for _, t := range taus {
		if len(out) > 0 && t == out[len(out)-1] {
			continue
		}
		out = append(out, t)
	}
	return out
}

