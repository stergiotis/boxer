package imzrt

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/goruntime"
)

// WindowedHistogram is a per-interval histogram: the bucket-wise difference of
// two cumulative goruntime.Histogram snapshots. Buckets are the runtime's own
// boundaries (len = len(Counts)+1); Counts[i] is the number of observations that
// fell in [Buckets[i], Buckets[i+1]) during the interval.
type WindowedHistogram struct {
	Buckets []float64
	Counts  []uint64
}

// WindowDelta writes cur-prev into out, reusing out's slice backings. The runtime
// histograms are cumulative since process start and share fixed boundaries, so
// the per-interval distribution is the bucket-wise difference (ADR-0061 Q1/O1). A
// length mismatch — the empty first tick, before any prior snapshot exists —
// yields all-zero counts rather than mistaking the whole cumulative history for a
// single interval. A bucket whose count went backwards (a counter reset, not
// expected in practice) clamps to zero.
func WindowDelta(cur, prev goruntime.Histogram, out *WindowedHistogram) {
	out.Buckets = append(out.Buckets[:0], cur.Buckets...)
	n := len(cur.Counts)
	if cap(out.Counts) >= n {
		out.Counts = out.Counts[:n]
	} else {
		out.Counts = make([]uint64, n)
	}
	if len(prev.Counts) != n {
		for i := range out.Counts {
			out.Counts[i] = 0
		}
		return
	}
	for i := range n {
		if cur.Counts[i] >= prev.Counts[i] {
			out.Counts[i] = cur.Counts[i] - prev.Counts[i]
		} else {
			out.Counts[i] = 0
		}
	}
}

// Total returns the number of observations in the window.
func (inst *WindowedHistogram) Total() (n uint64) {
	for _, c := range inst.Counts {
		n += c
	}
	return
}

// Quantile returns the value at q in [0,1], interpolated within the bucket it
// falls in. Returns 0 for an empty window. The runtime's bucket boundaries are
// exponentially spaced, so within-bucket interpolation is an approximation
// ("to the nearest bucket edge"). The open first/last buckets (±Inf edges) clamp
// to their finite neighbour, so the result is always finite.
func (inst *WindowedHistogram) Quantile(q float64) (v float64) {
	if q < 0 {
		q = 0
	}
	if q > 1 {
		q = 1
	}
	total := inst.Total()
	if total == 0 || len(inst.Buckets) < 2 {
		return 0
	}
	target := q * float64(total)
	var acc float64
	for i, cnt := range inst.Counts {
		if cnt == 0 {
			continue
		}
		next := acc + float64(cnt)
		if next >= target {
			lo := inst.Buckets[i]
			hi := inst.Buckets[i+1]
			switch {
			case math.IsInf(lo, -1) && math.IsInf(hi, 1):
				return 0
			case math.IsInf(lo, -1):
				return hi
			case math.IsInf(hi, 1):
				return lo
			}
			frac := (target - acc) / float64(cnt)
			v = lo + frac*(hi-lo)
			return
		}
		acc = next
	}
	// target == total exactly: the upper edge of the last non-empty bucket.
	v = inst.Max()
	return
}

// Max returns the finite upper edge of the highest non-empty bucket — the ceiling
// of the observed values — or 0 if the window is empty. An open top bucket (+Inf
// edge) falls back to its lower edge.
func (inst *WindowedHistogram) Max() (v float64) {
	for i := len(inst.Counts) - 1; i >= 0; i-- {
		if inst.Counts[i] > 0 {
			hi := inst.Buckets[i+1]
			if math.IsInf(hi, 1) {
				hi = inst.Buckets[i]
			}
			v = hi
			return
		}
	}
	return
}
