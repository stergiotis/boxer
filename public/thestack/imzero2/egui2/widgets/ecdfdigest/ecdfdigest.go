// Package ecdfdigest bridges a boxer tdigest.TDigest to the ecdf
// widget. The widget itself accepts an explicit (xs, fnAt, n) grid
// via RenderGrid; this helper builds that grid from a streaming
// sketch in one call.
//
// Kept in its own package so the ecdf widget remains import-free of
// the tdigest dependency. Callers that already pull in tdigest for
// other purposes can opt into the bridge by importing this package;
// callers that build the grid from a different sketch (Greenwald-
// Khanna, HDR histogram, anything with a CDF accessor) can compose
// the equivalent themselves in a few lines.
package ecdfdigest

import (
	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/ecdf"
)

// RenderDigest renders the ECDF and simultaneous confidence band of
// the data summarised by digest, using the supplied widget renderer.
// The visualisation grid is uniform across [digest.Min(), digest.Max()]
// with gridN samples (gridN ≥ 2). The band's calibration depends on
// the total observation count digest.Count(), not on gridN.
//
// The caller must already be inside a c.Plot block — RenderDigest
// invokes renderer.RenderGrid which enqueues PlotPolygon /
// PlotLine primitives in the usual way.
//
// Returns an error if gridN < 2 or if the digest's support has
// collapsed (Min ≥ Max — typically because Count == 0 or all
// observations were identical).
func RenderDigest(renderer ecdf.Renderer, digest *tdigest.TDigest, gridN int) (err error) {
	if digest == nil {
		err = eh.Errorf("nil digest")
		return
	}
	if gridN < 2 {
		err = eh.Errorf("gridN must be ≥ 2, got %d", gridN)
		return
	}
	n := digest.Count()
	if n <= 0 {
		err = eh.Errorf("digest is empty (Count == 0)")
		return
	}
	xmin := digest.Min()
	xmax := digest.Max()
	if !(xmax > xmin) {
		err = eh.Errorf("digest support collapsed (Min=%v Max=%v); the band is degenerate", xmin, xmax)
		return
	}
	xs, fn := BuildDigestGrid(digest, gridN)
	err = renderer.RenderGrid(xs, fn, int(n))
	return
}

// BuildDigestGrid samples a uniform x-grid over [digest.Min(),
// digest.Max()] of length gridN and evaluates the digest's CDF at
// each point. Returned slices are freshly allocated and aligned by
// index. Useful when the caller wants the grid in hand (e.g. for a
// custom render path) rather than going through RenderDigest.
//
// gridN < 2 is clamped to 2 to keep the API total without an error
// path; callers worried about edge cases should validate gridN
// upstream.
func BuildDigestGrid(digest *tdigest.TDigest, gridN int) (xs, fn []float64) {
	return BuildDigestGridRange(digest, gridN, digest.Min(), digest.Max())
}

// BuildDigestGridRange is BuildDigestGrid restricted to an explicit
// x-window [lo, hi]: it samples a uniform grid of length gridN across
// [lo, hi] (not the digest's full support) and evaluates the digest's
// CDF at each point. This is how a caller concentrates grid resolution
// in a visible body after clipping a long tail (ADR-0093) — the uniform
// full-range grid wastes most of its points in a flat tail, leaving the
// informative body coarse. The CDF values still come from the whole digest, so
// F_n(hi) reflects the true fraction at or below the cutoff (≈ the cutoff
// quantile) rather than 1; the band's calibration likewise depends on the
// total observation count, not on this window.
//
// lo / hi are used as given (caller computes them, e.g. from quantiles);
// when hi ≤ lo the window collapses and every grid point lands at lo
// (step 0) — callers should pass a non-degenerate window. gridN < 2 is
// clamped to 2.
func BuildDigestGridRange(digest *tdigest.TDigest, gridN int, lo, hi float64) (xs, fn []float64) {
	if gridN < 2 {
		gridN = 2
	}
	xs = make([]float64, gridN)
	fn = make([]float64, gridN)
	step := (hi - lo) / float64(gridN-1)
	for i := range xs {
		xs[i] = lo + step*float64(i)
		fn[i] = digest.CDF(xs[i])
	}
	return
}
