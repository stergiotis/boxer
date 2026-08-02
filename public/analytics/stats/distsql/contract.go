package distsql

import (
	"github.com/stergiotis/boxer/public/observability/eh"
)

// The ADR-0161 §SD1 contract column names. Required columns gate the play
// Distribution panel's claim; optional ones extend it. Names are bare by
// decision — validation, not prefixing, is what makes accidental claims
// implausible.
const (
	ColSeries    = "series"
	ColN         = "n"
	ColPs        = "ps"
	ColQs        = "qs"
	ColNNull     = "n_null"
	ColXMin      = "x_min"
	ColXMax      = "x_max"
	ColMean      = "mean"
	ColSd        = "sd"
	ColSkew      = "skew"
	ColKurt      = "kurt"
	ColHistLo    = "hist_lo"
	ColHistHi    = "hist_hi"
	ColHistW     = "hist_w"
	ColEstimator = "estimator"
	// ColSeriesBaseline is reserved by ADR-0161 §SD1 for SQL-declared
	// baselines; no consumer reads it yet.
	ColSeriesBaseline = "series_baseline"
)

// ValidateSeries applies the §SD1 grid rules to one claimed row. The
// messages are part of the panel's loud-reject surface and are pinned by
// tests — a claim must never fail into a silently empty plot.
func ValidateSeries(ps, qs []float64) (err error) {
	if len(ps) != len(qs) {
		return eh.Errorf("ps/qs length mismatch: %d vs %d", len(ps), len(qs))
	}
	if len(ps) < 2 {
		return eh.Errorf("grid too short: %d levels (need ≥ 2)", len(ps))
	}
	for i, p := range ps {
		if !(p > 0 && p < 1) {
			return eh.Errorf("ps[%d] = %v outside (0, 1)", i, p)
		}
		if i > 0 && !(p > ps[i-1]) {
			return eh.Errorf("ps not strictly ascending at [%d]: %v after %v", i, p, ps[i-1])
		}
	}
	for i := 1; i < len(qs); i++ {
		if qs[i] < qs[i-1] {
			return eh.Errorf("qs not non-decreasing at [%d]: %v after %v", i, qs[i], qs[i-1])
		}
	}
	return nil
}

// ValidateHist applies the §SD1 histogram-triplet rules: all three arrays
// or none (the caller enforces presence; this checks shape), equal lengths,
// and strictly positive bin widths — density normalisation divides by the
// width, so a zero-width bin is invalid rather than merely useless.
func ValidateHist(lo, hi, w []float64) (err error) {
	if len(lo) != len(hi) || len(lo) != len(w) {
		return eh.Errorf("hist triplet lengths differ: lo=%d hi=%d w=%d", len(lo), len(hi), len(w))
	}
	for i := range lo {
		if !(hi[i] > lo[i]) {
			return eh.Errorf("hist bin [%d] has non-positive width: [%v, %v)", i, lo[i], hi[i])
		}
	}
	return nil
}
