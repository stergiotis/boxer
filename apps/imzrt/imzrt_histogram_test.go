package imzrt

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/observability/goruntime"
)

func hist(buckets []float64, counts []uint64) (h goruntime.Histogram) {
	h = goruntime.Histogram{Buckets: buckets, Counts: counts}
	return
}

// TestWindowDelta_EmptyPrev: the first tick has no prior snapshot, so the delta
// must be all-zero (not the whole cumulative history mistaken for one interval).
func TestWindowDelta_EmptyPrev(t *testing.T) {
	cur := hist([]float64{0, 1, 2, 4}, []uint64{10, 20, 30})
	var out WindowedHistogram
	WindowDelta(cur, goruntime.Histogram{}, &out)

	if len(out.Counts) != 3 {
		t.Fatalf("Counts len = %d, want 3", len(out.Counts))
	}
	if got := out.Total(); got != 0 {
		t.Errorf("Total = %d, want 0 on empty-prev first tick", got)
	}
	if got := out.Quantile(0.5); got != 0 {
		t.Errorf("Quantile(0.5) = %v, want 0 on empty window", got)
	}
}

// TestWindowDelta_NoNewObservations: cur == prev means nothing happened this
// interval — an all-zero delta, not a carried-over distribution.
func TestWindowDelta_NoNewObservations(t *testing.T) {
	b := []float64{0, 1, 2, 4}
	cur := hist(b, []uint64{10, 20, 30})
	prev := hist(b, []uint64{10, 20, 30})
	var out WindowedHistogram
	WindowDelta(cur, prev, &out)

	if got := out.Total(); got != 0 {
		t.Errorf("Total = %d, want 0 when no new observations", got)
	}
}

// TestWindowDelta_Quantiles checks the per-interval delta and the bucket-edge
// interpolation against hand-computed values.
func TestWindowDelta_Quantiles(t *testing.T) {
	// Buckets [0,1) [1,2) [2,4) [4,8); cumulative prev then cur.
	b := []float64{0, 1, 2, 4, 8}
	prev := hist(b, []uint64{10, 20, 30, 40})
	cur := hist(b, []uint64{10, 22, 35, 41})
	// delta = [0, 2, 5, 1]; total = 8.
	var out WindowedHistogram
	WindowDelta(cur, prev, &out)

	if got := out.Total(); got != 8 {
		t.Fatalf("Total = %d, want 8", got)
	}
	// p50: target = 4; lands in bucket [2,4) at frac (4-2)/5 = 0.4 -> 2.8.
	if got := out.Quantile(0.5); math.Abs(got-2.8) > 1e-9 {
		t.Errorf("Quantile(0.5) = %v, want 2.8", got)
	}
	// Max: highest non-empty bucket is [4,8) -> upper edge 8.
	if got := out.Max(); got != 8 {
		t.Errorf("Max = %v, want 8", got)
	}
	// p100 equals the max bucket's upper edge.
	if got := out.Quantile(1.0); math.Abs(got-8) > 1e-9 {
		t.Errorf("Quantile(1.0) = %v, want 8", got)
	}
}

// TestWindowedHistogram_InfiniteEdges: the runtime's open first/last buckets carry
// ±Inf edges; quantiles must stay finite by clamping to the finite neighbour.
func TestWindowedHistogram_InfiniteEdges(t *testing.T) {
	inf := math.Inf(1)
	h := WindowedHistogram{
		Buckets: []float64{math.Inf(-1), 1, 2, inf},
		Counts:  []uint64{1, 2, 1}, // total 4
	}
	for _, q := range []float64{0, 0.25, 0.5, 0.99, 1.0} {
		v := h.Quantile(q)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("Quantile(%v) = %v, want finite", q, v)
		}
	}
	// p99 lands in the open top bucket [2,+Inf) -> clamps to lower edge 2.
	if got := h.Quantile(0.99); got != 2 {
		t.Errorf("Quantile(0.99) = %v, want 2 (open-top clamp)", got)
	}
	// Max of an open top bucket falls back to its lower edge.
	if got := h.Max(); got != 2 {
		t.Errorf("Max = %v, want 2", got)
	}
}

// TestWindowDelta_ResetGuard: a backwards-going bucket (counter reset) clamps to
// zero rather than underflowing uint64.
func TestWindowDelta_ResetGuard(t *testing.T) {
	b := []float64{0, 1, 2}
	prev := hist(b, []uint64{5, 9})
	cur := hist(b, []uint64{2, 12}) // bucket 0 went backwards
	var out WindowedHistogram
	WindowDelta(cur, prev, &out)

	if out.Counts[0] != 0 {
		t.Errorf("Counts[0] = %d, want 0 (reset clamped)", out.Counts[0])
	}
	if out.Counts[1] != 3 {
		t.Errorf("Counts[1] = %d, want 3", out.Counts[1])
	}
}

// TestWindowDelta_Reuse: out's backings are reused across calls, including when a
// later call has fewer buckets.
func TestWindowDelta_Reuse(t *testing.T) {
	var out WindowedHistogram
	WindowDelta(hist([]float64{0, 1, 2, 4}, []uint64{1, 2, 3}), goruntime.Histogram{Buckets: []float64{0, 1, 2, 4}, Counts: []uint64{0, 0, 0}}, &out)
	if out.Total() != 6 {
		t.Fatalf("first Total = %d, want 6", out.Total())
	}
	WindowDelta(hist([]float64{0, 1}, []uint64{4}), goruntime.Histogram{Buckets: []float64{0, 1}, Counts: []uint64{1}}, &out)
	if len(out.Counts) != 1 {
		t.Fatalf("after reuse Counts len = %d, want 1", len(out.Counts))
	}
	if out.Counts[0] != 3 {
		t.Errorf("after reuse Counts[0] = %d, want 3", out.Counts[0])
	}
}
