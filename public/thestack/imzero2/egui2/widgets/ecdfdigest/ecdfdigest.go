//go:build llm_generated_opus47

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
	if gridN < 2 {
		gridN = 2
	}
	xmin := digest.Min()
	xmax := digest.Max()
	xs = make([]float64, gridN)
	fn = make([]float64, gridN)
	step := (xmax - xmin) / float64(gridN-1)
	for i := range xs {
		xs[i] = xmin + step*float64(i)
		fn[i] = digest.CDF(xs[i])
	}
	return
}
