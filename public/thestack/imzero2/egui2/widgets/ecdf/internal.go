package ecdf

import "sort"

// buildEcdfPolyline returns the (xs, ys) vertices of the ECDF step
// polyline given a sorted sample. The polyline starts at
// (sorted[0], 0) — the bottom of the first jump — and ascends in
// 1/n increments, ending at (sorted[n-1], 1). 2n vertices total.
func buildEcdfPolyline(sorted []float64) (xs, ys []float64) {
	n := len(sorted)
	xs = make([]float64, 0, 2*n)
	ys = make([]float64, 0, 2*n)
	inv := 1.0 / float64(n)
	for i := 0; i < n; i++ {
		// Pre-jump: (X_(i+1), i/n).
		xs = append(xs, sorted[i])
		ys = append(ys, float64(i)*inv)
		// Post-jump: (X_(i+1), (i+1)/n).
		xs = append(xs, sorted[i])
		ys = append(ys, float64(i+1)*inv)
	}
	return
}

// fnAtXSorted returns F_n(x) for a sorted iid sample, using the
// right-continuous definition F_n(x) = #{ i : X_(i) ≤ x } / n. Two
// binary searches give the same answer in O(log n): SearchFloat64s
// places idx at the first sorted[idx] ≥ x; advancing past the
// repeated-x plateau yields the count of values ≤ x.
func fnAtXSorted(sorted []float64, x float64) float64 {
	n := len(sorted)
	if n < 1 {
		return 0
	}
	idx := sort.SearchFloat64s(sorted, x)
	for idx < n && sorted[idx] == x {
		idx++
	}
	return float64(idx) / float64(n)
}

// fnAtXGrid evaluates the grid-form ECDF at x via piecewise-linear
// interpolation, mirroring what egui_plot's PlotLine draws between
// adjacent (xs[i], fnAt[i]) vertices. Returns clamped endpoints
// outside the grid support.
func fnAtXGrid(xs, fnAt []float64, x float64) float64 {
	n := len(xs)
	if n < 1 {
		return 0
	}
	if x <= xs[0] {
		return fnAt[0]
	}
	if x >= xs[n-1] {
		return fnAt[n-1]
	}
	idx := sort.SearchFloat64s(xs, x)
	x0, x1 := xs[idx-1], xs[idx]
	y0, y1 := fnAt[idx-1], fnAt[idx]
	if x1 == x0 {
		return y0
	}
	return y0 + (y1-y0)*(x-x0)/(x1-x0)
}

// bandAtX returns the (lower, upper) plateau active at x for a band
// whose i-th rectangle covers [xs[i], xs[i+1]] × [lower[i], upper[i]]
// (matching emitBandRectangles). Out-of-support x clamps to the
// nearest plateau; the n-th index (last point) reuses the n-1 rect.
func bandAtX(xs, lower, upper []float64, x float64) (lo, hi float64) {
	n := len(xs)
	if n < 2 {
		return 0, 1
	}
	if x <= xs[0] {
		return lower[0], upper[0]
	}
	if x >= xs[n-1] {
		return lower[n-2], upper[n-2]
	}
	idx := sort.SearchFloat64s(xs, x)
	if idx > 0 {
		idx--
	}
	if idx >= n-1 {
		idx = n - 2
	}
	return lower[idx], upper[idx]
}

// nearestIdx returns the 0-based index of the order statistic
// closest to x in absolute value. Ties prefer the larger index
// (the right-hand neighbour). Caller must guarantee n ≥ 1.
func nearestIdx(sorted []float64, x float64) int {
	n := len(sorted)
	if n == 1 {
		return 0
	}
	idx := sort.SearchFloat64s(sorted, x)
	if idx == 0 {
		return 0
	}
	if idx >= n {
		return n - 1
	}
	if x-sorted[idx-1] < sorted[idx]-x {
		return idx - 1
	}
	return idx
}

// withAlpha replaces the alpha byte (low 8 bits) of an RGBA-packed
// uint32. Used to dim the ECDF stroke for the crosshair line so it
// reads as a secondary affordance rather than competing with the
// curve.
func withAlpha(packed uint32, alpha uint8) uint32 {
	return (packed &^ 0xFF) | uint32(alpha)
}
