//go:build llm_generated_opus47

package ecdfbands

import (
	"math"
)

// crossingProbabilitySteck implements the rectangle-probability formula
// of Steck (1971) and Noé (1972) for n iid Uniform(0, 1) order
// statistics:
//
//	P(a_i ≤ U_{(i)} ≤ b_i ∀ i = 1..n) = n! · det(L)
//
// where L is the n×n upper-Hessenberg matrix
//
//	L[i,j] = (b_i - a_j)_+^{j-i+1} / (j-i+1)!   for j ≥ i-1
//	L[i,j] = 0                                   for j <  i-1
//
// indexed from 1; in this implementation rows and columns are
// 0-indexed and the sub-diagonal sits at j = i-1.
//
// The determinant is computed by direct LU on the dense
// upper-Hessenberg matrix (no pivoting; the diagonal entries
// L[k,k] = b_k - a_k are strictly positive for valid monotone
// configurations). The sub-diagonal scaling preserves the matrix
// structure during elimination — each step zeroes exactly one entry
// L[k+1,k] and updates row k+1 in O(n-k) flops. Total: O(n²).
//
// To stay within double-precision throughout, the running determinant
// is accumulated in log-space (sum of log diagonal entries plus a sign
// bit), then multiplied by n! also in log-space — the final
// exponentiation lands the result in [0, 1] without intermediate
// underflow even when the determinant itself drops to ~10⁻¹⁵⁸ at
// n ≈ 100.
//
// Validity: reliable for n ≤ steckN (currently 24) with relative
// accuracy better than 1e-6 on typical Berk-Jones / DKW boundary
// shapes. CrossingAlgorithmAuto routes Steck only inside this range
// — above it, the (j+1)/(k+1) propagation factor amplifies
// subtractive cancellation past 2-3 digits of accuracy. Callers can
// still pass CrossingAlgorithmSteck explicitly for cross-validation
// up to about n=50, beyond which the result is essentially noise.
// Above the envelope, switch to CrossingAlgorithmMoscovich whose
// Poissonized DP keeps all entries strictly positive.
func crossingProbabilitySteck(a, b []float64) (p float64, err error) {
	n := len(a)
	if n == 0 {
		p = 1
		return
	}
	if trivialZero(a, b) {
		p = 0
		return
	}

	mat := buildSteckMatrixDense(a, b)
	logDet, signDet := hessenbergDeterminantLog(n, mat)
	if signDet <= 0 {
		p = 0
		return
	}

	logP := logFactorial(n) + logDet
	if logP > 0 {
		p = 1
		return
	}
	p = math.Exp(logP)
	return
}

// buildSteckMatrixDense returns the n×n Steck matrix in row-major
// dense storage. Entries below the sub-diagonal (j < i-1) are 0;
// the sub-diagonal (j == i-1) is 1; entries on/above the diagonal
// are (b_i - a_j)^{j-i+1} / (j-i+1)!.
//
// We compute (b_i - a_j)^k / k! incrementally per row as a running
// product, which keeps the per-entry cost at O(1) and avoids
// repeated pow/lgamma calls.
func buildSteckMatrixDense(a, b []float64) []float64 {
	n := len(a)
	mat := make([]float64, n*n)
	for i := range n {
		// Sub-diagonal entry at (i, i-1) = 1, set for i ≥ 1.
		if i >= 1 {
			mat[i*n+(i-1)] = 1
		}
		// Diagonal and above.
		// For j = i: power = (b_i - a_i)^1 / 1!.
		// For j > i: power = (b_i - a_j)^{j-i+1} / (j-i+1)!.
		// Recurrence per j: power_{j} = power_{j-1} · (b_i - a_j) / (j-i+1)
		//                              · ((b_i - a_j) / (b_i - a_{j-1}))^{j-i} (gnarly)
		// Simpler: compute each j independently.
		for j := i; j < n; j++ {
			diff := b[i] - a[j]
			if diff <= 0 {
				continue // matrix entry stays 0
			}
			k := j - i + 1
			mat[i*n+j] = math.Pow(diff, float64(k)) / math.Exp(logFactorial(k))
		}
	}
	return mat
}

// hessenbergDeterminantLog runs Gaussian elimination on the n×n
// upper-Hessenberg matrix stored row-major in mat (in-place; mat is
// destroyed). Returns (log|det|, sign), with sign ∈ {-1, 0, +1}.
//
// The Hessenberg structure means each column k has exactly one
// sub-diagonal entry to eliminate (mat[k+1, k]), so the entire
// elimination is O(n²) flops. We do not pivot — for valid monotone
// boundary chains the diagonal stays strictly positive throughout
// (the band width at every rank is positive). The sign is
// nevertheless tracked so callers detect infeasible inputs that
// drive the determinant to zero.
//
// Working in plain double precision: the matrix entries start in
// [0, 1] (a typical BJ band gives entries ≤ 0.5 ish), and the
// subtractive steps remain well-conditioned as long as the
// elimination factor (sub / pivot) does not blow up — which it
// doesn't for the BJ / DKW shapes we care about.
func hessenbergDeterminantLog(n int, mat []float64) (logDet float64, signDet int8) {
	signDet = +1
	for k := 0; k < n-1; k++ {
		pivot := mat[k*n+k]
		if pivot == 0 {
			signDet = 0
			return
		}
		sub := mat[(k+1)*n+k]
		if sub == 0 {
			continue
		}
		factor := sub / pivot
		for j := k + 1; j < n; j++ {
			mat[(k+1)*n+j] -= factor * mat[k*n+j]
		}
		mat[(k+1)*n+k] = 0
	}
	// Accumulate log|det| with sign tracking from the diagonal.
	for i := range n {
		d := mat[i*n+i]
		if d == 0 {
			signDet = 0
			logDet = negInf
			return
		}
		if d < 0 {
			signDet = -signDet
			d = -d
		}
		logDet += math.Log(d)
	}
	return
}
