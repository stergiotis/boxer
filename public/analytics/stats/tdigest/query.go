package tdigest

import (
	"math"
	"slices"
)

// Quantile returns the value at quantile q ∈ [0, 1].
//
// q ≤ 0 returns Min; q ≥ 1 returns Max; empty digest returns NaN.
// Accuracy improves toward the tails (Dunning's k1 scale property).
func (inst *TDigest) Quantile(q float64) float64 {
	inst.compress()
	return inst.quantileNoFlush(q)
}

func (inst *TDigest) quantileNoFlush(q float64) float64 {
	if inst.n == 0 {
		return math.NaN()
	}
	if q <= 0 {
		return inst.min
	}
	if q >= 1 {
		return inst.max
	}
	n := len(inst.means)
	if n == 0 {
		return math.NaN()
	}

	target := q * inst.totalWeight

	if n == 1 {
		half := inst.totalWeight / 2.0
		if target < half {
			return inst.min + (target/half)*(inst.means[0]-inst.min)
		}
		return inst.means[0] + ((target-half)/half)*(inst.max-inst.means[0])
	}

	// Left tail: between (0, min) and (weights[0]/2, means[0]).
	left := inst.weights[0] / 2.0
	if target < left {
		t := target / left
		return inst.min + t*(inst.means[0]-inst.min)
	}

	// Middle anchors.
	cumW := inst.weights[0]
	for i := 0; i < n-1; i++ {
		ai := cumW - inst.weights[i]/2.0
		aj := cumW + inst.weights[i+1]/2.0
		if target >= ai && target < aj {
			span := aj - ai
			if span <= 0 {
				return inst.means[i]
			}
			t := (target - ai) / span
			return inst.means[i] + t*(inst.means[i+1]-inst.means[i])
		}
		cumW += inst.weights[i+1]
	}

	// Right tail: between (totalWeight - weights[n-1]/2, means[n-1]) and (totalWeight, max).
	lastAnchor := inst.totalWeight - inst.weights[n-1]/2.0
	tailSpan := inst.weights[n-1] / 2.0
	if tailSpan <= 0 {
		return inst.max
	}
	t := (target - lastAnchor) / tailSpan
	if t < 0 {
		return inst.means[n-1]
	}
	if t > 1 {
		t = 1
	}
	return inst.means[n-1] + t*(inst.max-inst.means[n-1])
}

// Quantiles returns the values for the given quantile points.
// Equivalent to calling Quantile per element but flushes the buffer once.
func (inst *TDigest) Quantiles(qs []float64) (out []float64) {
	inst.compress()
	out = make([]float64, len(qs))
	for i, q := range qs {
		out[i] = inst.quantileNoFlush(q)
	}
	return
}

// CDF returns the estimated cumulative density at x ∈ ℝ.
//
// x ≤ Min returns 0; x ≥ Max returns 1; empty digest returns NaN.
// CDF is the inverse of Quantile under the same linear interpolation:
// Quantile(CDF(x)) ≈ x within centroid resolution.
func (inst *TDigest) CDF(x float64) float64 {
	inst.compress()
	if inst.n == 0 {
		return math.NaN()
	}
	if x <= inst.min {
		return 0
	}
	if x >= inst.max {
		return 1
	}
	n := len(inst.means)
	if n == 0 {
		return math.NaN()
	}

	if n == 1 {
		half := inst.totalWeight / 2.0
		if x < inst.means[0] {
			rng := inst.means[0] - inst.min
			if rng <= 0 {
				return 0.5
			}
			return ((x - inst.min) / rng) * half / inst.totalWeight
		}
		rng := inst.max - inst.means[0]
		if rng <= 0 {
			return 0.5
		}
		return (half + ((x-inst.means[0])/rng)*half) / inst.totalWeight
	}

	if x < inst.means[0] {
		rng := inst.means[0] - inst.min
		left := inst.weights[0] / 2.0
		if rng <= 0 {
			return 0
		}
		rank := ((x - inst.min) / rng) * left
		return rank / inst.totalWeight
	}

	cumW := inst.weights[0]
	for i := 0; i < n-1; i++ {
		ai := cumW - inst.weights[i]/2.0
		aj := cumW + inst.weights[i+1]/2.0
		if x >= inst.means[i] && x < inst.means[i+1] {
			rng := inst.means[i+1] - inst.means[i]
			if rng <= 0 {
				return (ai + aj) / (2 * inst.totalWeight)
			}
			t := (x - inst.means[i]) / rng
			rank := ai + t*(aj-ai)
			return rank / inst.totalWeight
		}
		cumW += inst.weights[i+1]
	}

	lastAnchor := inst.totalWeight - inst.weights[n-1]/2.0
	// If the middle loop fell through with x at or before the last
	// centroid's mean, we are in a dup-mean run; return the last
	// centroid's anchor rank rather than interpolating into the tail
	// (which can yield 1.0 via rng==0 or negative t via x<means[n-1]).
	if x <= inst.means[n-1] {
		return lastAnchor / inst.totalWeight
	}
	rng := inst.max - inst.means[n-1]
	if rng <= 0 {
		return 1
	}
	t := (x - inst.means[n-1]) / rng
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	rank := lastAnchor + t*(inst.weights[n-1]/2.0)
	return rank / inst.totalWeight
}

// Merge folds other into inst. Other is genuinely read-only: the merge
// runs against a non-mutating snapshot of other's centroids so neither
// other's canonical centroid array nor its unmerged buffer are touched.
//
// Centroids are staged through inst's buffer and re-compressed under
// inst's k1 constraint (so a fine-delta other merged into a coarse-delta
// inst respects inst's budget); min/max anchors and the exact observation
// count are propagated explicitly so n and extrema stay accurate.
//
// Self-merge (inst.Merge(inst)) is a no-op — without the guard it would
// double totalWeight and n, silently corrupting subsequent queries.
func (inst *TDigest) Merge(other *TDigest) {
	if other == nil || other == inst || other.n == 0 {
		return
	}
	otherMeans, otherWeights := other.snapshotCentroids()
	inst.compress()

	inst.bufMeans = slices.Grow(inst.bufMeans, len(otherMeans))
	inst.bufWeights = slices.Grow(inst.bufWeights, len(otherWeights))
	inst.bufMeans = append(inst.bufMeans, otherMeans...)
	inst.bufWeights = append(inst.bufWeights, otherWeights...)
	inst.totalWeight += other.totalWeight
	if other.min < inst.min {
		inst.min = other.min
	}
	if other.max > inst.max {
		inst.max = other.max
	}
	mergedN := inst.n + other.n
	inst.compress()
	inst.n = mergedN
}

// snapshotCentroids returns a stable copy of the effective compressed
// centroids without mutating the receiver. When the buffer is empty
// the returned slices alias the compressed centroid arrays (cheap path);
// when the buffer holds unflushed observations a temporary digest is
// built and compressed to produce the snapshot, leaving the receiver
// untouched. Callers must treat the returned slices as read-only.
func (inst *TDigest) snapshotCentroids() (means, weights []float64) {
	if len(inst.bufMeans) == 0 {
		means = inst.means
		weights = inst.weights
		return
	}
	tmp := TDigest{
		delta:       inst.delta,
		bufCap:      inst.bufCap,
		means:       slices.Clone(inst.means),
		weights:     slices.Clone(inst.weights),
		bufMeans:    slices.Clone(inst.bufMeans),
		bufWeights:  slices.Clone(inst.bufWeights),
		totalWeight: inst.totalWeight,
		min:         inst.min,
		max:         inst.max,
		n:           inst.n,
	}
	tmp.compress()
	means = tmp.means
	weights = tmp.weights
	return
}
