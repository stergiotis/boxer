// Package digest bridges a tdigest.TDigest to the ecdfbands library.
// BandsForDigest is a one-call wrapper that builds the (xs, fnAt)
// grid from a streaming sketch and forwards to BandsForGrid; the
// underlying BuildGrid is exported for callers that want the grid
// in hand (e.g. for custom analysis or feeding through a different
// visualisation path).
//
// Kept as a sub-package so the main ecdfbands package stays free of
// the tdigest dependency. Callers using a different sketch
// (Greenwald-Khanna, HDR histogram, anything with min/max/CDF
// accessors) can compose the equivalent in a few lines without
// pulling tdigest into their dependency graph.
package digest

import (
	"github.com/stergiotis/boxer/public/analytics/stats/ecdfbands"
	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// BandsForDigest evaluates F_n at a uniform x-grid spanning
// [digest.Min(), digest.Max()] with gridN samples, then computes
// the simultaneous (1-α)·100% confidence band via
// ecdfbands.BandsForGrid. The band's calibration uses
// digest.Count() as the total observation count, not gridN.
//
// Returns an error if gridN < 2, the digest is empty
// (Count == 0), or the digest's support has collapsed
// (Min ≥ Max — typically because all observations were
// identical).
func BandsForDigest(
	digest *tdigest.TDigest, gridN int, alpha float64, method ecdfbands.BandMethodE,
) (b ecdfbands.GridBand, err error) {
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
	xs, fn := BuildGrid(digest, gridN)
	b, err = ecdfbands.BandsForGrid(xs, fn, int(n), alpha, method)
	return
}

// BuildGrid samples a uniform x-grid over [digest.Min(),
// digest.Max()] of length gridN and evaluates the digest's CDF at
// each point. Returned slices are freshly allocated and aligned by
// index. gridN < 2 is clamped to 2 to keep the API total — callers
// that need stricter validation should guard upstream (see
// BandsForDigest for the error-returning variant).
func BuildGrid(digest *tdigest.TDigest, gridN int) (xs, fn []float64) {
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
