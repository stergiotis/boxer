//go:build llm_generated_opus47

package tdigest

import "math"

// k1 is the asymmetric scaling function from Dunning 2019.
//
// Maps a quantile q ∈ [0, 1] to a k-scale position k ∈ [-δ/4, +δ/4].
// The slope dk/dq = δ / (2π · sqrt(q(1-q))) grows without bound at the
// tails, which forces compress() to keep centroids narrow there and
// gives tail accuracy proportional to q(1-q).
//
//	k1(q) = δ / (2π) · arcsin(2q - 1)
func k1(q, delta float64) float64 {
	x := 2.0*q - 1.0
	if x < -1.0 {
		x = -1.0
	} else if x > 1.0 {
		x = 1.0
	}
	return delta / (2.0 * math.Pi) * math.Asin(x)
}
