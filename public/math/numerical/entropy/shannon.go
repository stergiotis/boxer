//go:build llm_generated_opus46

// Package entropy provides numerically stable Shannon entropy computation.
package entropy

import (
	"iter"
	"math"
)

// Shannon computes the Shannon entropy H(p) = -Σ pᵢ log₂(pᵢ) in bits,
// using Kahan compensated summation for numerical stability.
//
// Zero-probability entries are skipped (0·log(0) is defined as 0).
func Shannon(p iter.Seq[float64]) float64 {
	var sum, comp float64
	for pi := range p {
		if pi <= 0 {
			continue
		}
		term := pi * math.Log2(pi)

		y := term - comp
		t := sum + y
		comp = (t - sum) - y
		sum = t
	}
	return -sum
}

// ShannonNats computes the Shannon entropy in nats (using natural log).
func ShannonNats(p iter.Seq[float64]) float64 {
	var sum, comp float64
	for pi := range p {
		if pi <= 0 {
			continue
		}
		term := pi * math.Log(pi)

		y := term - comp
		t := sum + y
		comp = (t - sum) - y
		sum = t
	}
	return -sum
}

// ShannonFromLogProbs computes Shannon entropy from log-probabilities (natural log),
// returning the result in nats.
//
// This is useful when probabilities are very small and already in log-space,
// avoiding the precision loss of exp → log round-trips.
func ShannonFromLogProbs(logp iter.Seq[float64]) float64 {
	var sum, comp float64
	for lp := range logp {
		if math.IsInf(lp, -1) {
			continue // p=0, contribution is 0
		}
		term := math.Exp(lp) * lp

		y := term - comp
		t := sum + y
		comp = (t - sum) - y
		sum = t
	}
	return -sum
}
