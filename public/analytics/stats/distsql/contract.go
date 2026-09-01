package distsql

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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
		return eb.Build().Int("ps", len(ps)).Int("qs", len(qs)).Errorf("ps/qs length mismatch")
	}
	if len(ps) < 2 {
		return eb.Build().Int("levels", len(ps)).Errorf("grid too short; at least 2 levels are needed")
	}
	for i, p := range ps {
		if !(p > 0 && p < 1) {
			return eb.Build().Int("index", i).Float64("p", p).Errorf("ps value is outside (0, 1)")
		}
		if i > 0 && !(p > ps[i-1]) {
			return eb.Build().Int("index", i).Float64("value", p).Float64("previous", ps[i-1]).Errorf("ps not strictly ascending")
		}
	}
	for i := 1; i < len(qs); i++ {
		if qs[i] < qs[i-1] {
			return eb.Build().Int("index", i).Float64("value", qs[i]).Float64("previous", qs[i-1]).Errorf("qs not non-decreasing")
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
		return eb.Build().Int("lo", len(lo)).Int("hi", len(hi)).Int("w", len(w)).Errorf("hist triplet lengths differ")
	}
	for i := range lo {
		if !(hi[i] > lo[i]) {
			return eb.Build().Int("bin", i).Float64("lo", lo[i]).Float64("hi", hi[i]).Errorf("hist bin has non-positive width")
		}
	}
	return nil
}
