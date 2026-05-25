//go:build llm_generated_opus47

// Package tdigest provides a streaming quantile sketch with tail-biased
// accuracy, based on Dunning 2019 (arXiv:1902.04023).
//
// Unlike KLL, which gives uniform rank error, t-digest's centroid sizes
// scale as 1/(q(1-q)) so quantiles near 0 and 1 are estimated more
// accurately than those near 0.5. This matches the requirements of
// letter-value plots and tail-focused dashboards.
//
// Construct with NewTDigest (δ=100) or NewTDigestWithDelta. Zero value
// is not usable.
package tdigest

import (
	"iter"
	"math"
	"slices"
)

const (
	defaultDelta = 100.0
	minDelta     = 10.0
	maxDelta     = 10_000.0
	bufMultiple  = 5
)

// TDigest is a streaming quantile sketch. Not thread-safe.
type TDigest struct {
	delta  float64
	bufCap int32

	means   []float64
	weights []float64

	bufMeans   []float64
	bufWeights []float64

	bufIdx         []int32
	scratchMeans   []float64
	scratchWeights []float64

	totalWeight float64

	min float64
	max float64

	n int64
}

func NewTDigest() (inst *TDigest) {
	inst = NewTDigestWithDelta(defaultDelta)
	return
}

// NewTDigestWithDelta constructs a digest with custom compression.
// delta is clamped to [10, 10000]. Larger delta = more centroids =
// better accuracy and more memory (≈delta/2 centroids retained).
func NewTDigestWithDelta(delta float64) (inst *TDigest) {
	if math.IsNaN(delta) || delta < minDelta {
		delta = minDelta
	} else if delta > maxDelta {
		delta = maxDelta
	}
	bufCap := int32(bufMultiple * delta)
	inst = &TDigest{
		delta:      delta,
		bufCap:     bufCap,
		bufMeans:   make([]float64, 0, bufCap),
		bufWeights: make([]float64, 0, bufCap),
		min:        math.Inf(1),
		max:        math.Inf(-1),
	}
	return
}

func (inst *TDigest) Reset() {
	inst.means = inst.means[:0]
	inst.weights = inst.weights[:0]
	inst.bufMeans = inst.bufMeans[:0]
	inst.bufWeights = inst.bufWeights[:0]
	inst.bufIdx = inst.bufIdx[:0]
	inst.scratchMeans = inst.scratchMeans[:0]
	inst.scratchWeights = inst.scratchWeights[:0]
	inst.totalWeight = 0
	inst.min = math.Inf(1)
	inst.max = math.Inf(-1)
	inst.n = 0
}

func (inst *TDigest) Delta() float64 { return inst.delta }
func (inst *TDigest) Count() int64   { return inst.n }
func (inst *TDigest) Weight() float64 {
	return inst.totalWeight
}

// Min returns the smallest pushed value, or NaN when empty. Callers
// detecting the empty state should compare via math.IsNaN.
func (inst *TDigest) Min() float64 {
	if inst.n == 0 {
		return math.NaN()
	}
	return inst.min
}

// Max returns the largest pushed value, or NaN when empty.
func (inst *TDigest) Max() float64 {
	if inst.n == 0 {
		return math.NaN()
	}
	return inst.max
}

// CentroidCount returns the number of compressed centroids (after flush).
func (inst *TDigest) CentroidCount() int {
	inst.compress()
	return len(inst.means)
}

// Push adds a single observation with weight 1.
// NaN and ±Inf are silently dropped.
func (inst *TDigest) Push(x float64) {
	inst.PushWeighted(x, 1.0)
}

// PushWeighted adds an observation with positive weight w.
// Non-positive weights and non-finite values are dropped.
func (inst *TDigest) PushWeighted(x, w float64) {
	if w <= 0 || math.IsNaN(w) || math.IsInf(w, 0) {
		return
	}
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return
	}
	inst.bufMeans = append(inst.bufMeans, x)
	inst.bufWeights = append(inst.bufWeights, w)
	inst.totalWeight += w
	inst.n++
	if x < inst.min {
		inst.min = x
	}
	if x > inst.max {
		inst.max = x
	}
	if int32(len(inst.bufMeans)) >= inst.bufCap {
		inst.compress()
	}
}

func (inst *TDigest) PushFloat32(x float32) {
	inst.Push(float64(x))
}

func (inst *TDigest) PushSeq(seq iter.Seq[float64]) {
	for v := range seq {
		inst.Push(v)
	}
}

// Centroid is the (mean, weight) pair emitted by Centroids().
type Centroid struct {
	Mean   float64
	Weight float64
}

// Centroids iterates over the compressed centroids in ascending mean order.
// Triggers a flush, so the digest's centroid array is canonicalized
// before the first yield.
func (inst *TDigest) Centroids() iter.Seq2[int, Centroid] {
	return func(yield func(int, Centroid) bool) {
		inst.compress()
		for i, m := range inst.means {
			if !yield(i, Centroid{Mean: m, Weight: inst.weights[i]}) {
				return
			}
		}
	}
}

// compress flushes the unmerged buffer into the centroid array,
// applying the k1 scale constraint.
func (inst *TDigest) compress() {
	if len(inst.bufMeans) == 0 {
		return
	}
	inst.sortBuffer()

	nA := len(inst.means)
	nB := len(inst.bufMeans)
	total := inst.totalWeight
	delta := inst.delta

	inst.scratchMeans = slices.Grow(inst.scratchMeans[:0], nA+nB)
	inst.scratchWeights = slices.Grow(inst.scratchWeights[:0], nA+nB)

	i, j := 0, 0
	pickFromA := func() bool {
		if i >= nA {
			return false
		}
		if j >= nB {
			return true
		}
		return inst.means[i] <= inst.bufMeans[j]
	}

	var curMean, curWeight float64
	if pickFromA() {
		curMean = inst.means[i]
		curWeight = inst.weights[i]
		i++
	} else {
		curMean = inst.bufMeans[j]
		curWeight = inst.bufWeights[j]
		j++
	}
	weightBefore := 0.0

	for i < nA || j < nB {
		var nextMean, nextWeight float64
		if pickFromA() {
			nextMean = inst.means[i]
			nextWeight = inst.weights[i]
			i++
		} else {
			nextMean = inst.bufMeans[j]
			nextWeight = inst.bufWeights[j]
			j++
		}

		qLeft := weightBefore / total
		qRight := (weightBefore + curWeight + nextWeight) / total
		if k1(qRight, delta)-k1(qLeft, delta) <= 1.0 {
			newW := curWeight + nextWeight
			curMean = (curMean*curWeight + nextMean*nextWeight) / newW
			curWeight = newW
		} else {
			inst.scratchMeans = append(inst.scratchMeans, curMean)
			inst.scratchWeights = append(inst.scratchWeights, curWeight)
			weightBefore += curWeight
			curMean = nextMean
			curWeight = nextWeight
		}
	}
	inst.scratchMeans = append(inst.scratchMeans, curMean)
	inst.scratchWeights = append(inst.scratchWeights, curWeight)

	inst.means, inst.scratchMeans = inst.scratchMeans, inst.means[:0]
	inst.weights, inst.scratchWeights = inst.scratchWeights, inst.weights[:0]
	inst.bufMeans = inst.bufMeans[:0]
	inst.bufWeights = inst.bufWeights[:0]
}

// sortBuffer sorts (bufMeans, bufWeights) jointly by bufMeans ascending.
// Uses bufIdx as a permutation, then materializes the result back via
// scratch buffers (swapped into bufMeans/bufWeights).
func (inst *TDigest) sortBuffer() {
	n := len(inst.bufMeans)
	if n <= 1 {
		return
	}
	inst.bufIdx = slices.Grow(inst.bufIdx[:0], n)
	for i := range n {
		inst.bufIdx = append(inst.bufIdx, int32(i))
	}
	bufMeans := inst.bufMeans
	slices.SortFunc(inst.bufIdx, func(a, b int32) int {
		if bufMeans[a] < bufMeans[b] {
			return -1
		}
		if bufMeans[a] > bufMeans[b] {
			return 1
		}
		return 0
	})

	inst.scratchMeans = slices.Grow(inst.scratchMeans[:0], n)
	inst.scratchWeights = slices.Grow(inst.scratchWeights[:0], n)
	for _, idx := range inst.bufIdx {
		inst.scratchMeans = append(inst.scratchMeans, inst.bufMeans[idx])
		inst.scratchWeights = append(inst.scratchWeights, inst.bufWeights[idx])
	}
	inst.bufMeans, inst.scratchMeans = inst.scratchMeans, inst.bufMeans[:0]
	inst.bufWeights, inst.scratchWeights = inst.scratchWeights, inst.bufWeights[:0]
}
