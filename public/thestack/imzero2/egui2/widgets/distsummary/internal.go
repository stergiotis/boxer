package distsummary

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/analytics/stats/ecdfbands"
	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// fiveNumberSummary captures the level-1 view of a distribution: the
// classical Tukey 5-number summary plus the observation count.
//
// All quantile fields are population estimates from the supplied
// QuantileOracle (typically a streaming t-digest). For very small n
// the digest's Quantile may collapse Q1/median/Q3 to identical values;
// the formatter renders these as-is rather than special-casing.
type fiveNumberSummary struct {
	n      int64
	min    float64
	q1     float64
	median float64
	q3     float64
	max    float64
}

// computeFiveNumberSummary derives the 5-number summary from a TDigest.
// Returns the zero value (n=0) for a nil digest or a digest with no
// observations.
func computeFiveNumberSummary(d *tdigest.TDigest) (out fiveNumberSummary) {
	if d == nil {
		return
	}
	n := d.Count()
	out.n = n
	if n == 0 {
		return
	}
	// One Quantiles call performs a single buffer flush — cheaper than
	// three separate Quantile invocations.
	qs := d.Quantiles([]float64{0.25, 0.5, 0.75})
	out.q1 = qs[0]
	out.median = qs[1]
	out.q3 = qs[2]
	out.min = d.Min()
	out.max = d.Max()
	return
}

// exactBandBucketFloor is the sample size at or below which
// [bucketExactN] returns n unchanged. Below it the exact O(n²) solve is
// sub-second and the tighter band is worth keeping; bucketing only buys
// stability for the expensive large-n solves, so small samples stay
// exact.
const exactBandBucketFloor = 256

// bucketExactN rounds n DOWN to a coarse geometric ladder (steps of
// `ratio`) so a digest whose size drifts a little reuses the same cached
// exact-band solve instead of cancelling and restarting it every frame —
// which is what lets a live / repeatedly-recomputed inspector's exact
// band settle rather than spin forever (ADR-0093 §Decision; see
// [Renderer.renderEcdfBody]).
//
// Rounding DOWN is deliberate: the bucketed value is ≤ n, so the band is
// calibrated at no more samples than were observed and is therefore a
// conservative over-cover, never an under-coverage. The gap between the
// bucket and the true n is surfaced in the readout (BandN < SampleN), so
// the conservatism is visible rather than silent. Relative conservatism
// is bounded by √ratio on the band half-width.
//
// ratio ≤ 1 disables bucketing (returns n); n ≤ [exactBandBucketFloor]
// is returned unchanged.
func bucketExactN(n int, ratio float64) int {
	if ratio <= 1 || n <= exactBandBucketFloor {
		return n
	}
	k := math.Floor(math.Log(float64(n)) / math.Log(ratio))
	bn := max(int(math.Floor(math.Pow(ratio, k))), exactBandBucketFloor)
	bn = min(bn, n) // floating-point guard: never exceed the true count
	return bn
}

// tailClipBounds computes the ECDF's x-view window, clipping a long tail
// adaptively and per-side (ADR-0093 §Decision). The cutoff itself is a
// quantile (so the retained fraction reads straight off the F(x) axis),
// but a side is
// clipped only when its tail is long relative to the spread — the
// Tukey-style ratio (max−Q3)/IQR or (Q1−min)/IQR exceeding triggerIQR
// (~3, the "far out" multiple). A well-behaved distribution thus renders
// full-range; only a genuinely heavy tail is trimmed. The band's
// calibration is unaffected — it always uses the true observation count,
// not this window.
//
// Returns the window [lo, hi] and which side(s) were clipped (for the
// hidden-tail annotation). When clipping is disabled, the spread is
// degenerate (IQR ≤ 0 / non-finite), or a clip would collapse the window,
// it falls back to the full [Min, Max] with both clipped flags false.
func tailClipBounds(d *tdigest.TDigest, lowerP, upperP, triggerIQR float64, enabled bool) (lo, hi float64, clippedLo, clippedHi bool) {
	xmin := d.Min()
	xmax := d.Max()
	lo, hi = xmin, xmax
	if !enabled {
		return
	}
	// One buffer flush for the four quantiles the trigger + cutoff need.
	q := d.Quantiles([]float64{0.25, 0.75, lowerP, upperP})
	q1, q3, qLo, qHi := q[0], q[1], q[2], q[3]
	iqr := q3 - q1
	if !(iqr > 0) { // degenerate / non-finite spread: no robust trigger
		return
	}
	// Upper tail long relative to spread → clip to the upper quantile.
	if (xmax-q3)/iqr > triggerIQR && isFinite(qHi) && qHi > lo && qHi < xmax {
		hi = qHi
		clippedHi = true
	}
	// Lower tail, symmetric.
	if (q1-xmin)/iqr > triggerIQR && isFinite(qLo) && qLo < hi && qLo > xmin {
		lo = qLo
		clippedLo = true
	}
	if !(hi > lo) { // never collapse the window
		lo, hi = xmin, xmax
		clippedLo, clippedHi = false, false
	}
	return
}

// isFinite reports whether x is neither NaN nor ±Inf.
func isFinite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

// formatTailClipNote builds the always-visible annotation shown below a
// clipped ECDF, naming the visible window and how much of which tail the
// cutoff hid (so the trim is honest, never silent). Returns "" when
// neither side was clipped. fn formats x values for display (the
// renderer's value formatter). The hidden fraction is read from the
// digest's CDF at the cutoff rather than assumed equal to 1−upperP, since
// the digest's quantile and CDF are not exact inverses.
func formatTailClipNote(d *tdigest.TDigest, clipLo, clipHi float64, clippedLo, clippedHi bool, fn FormatFunc) string {
	var parts []string
	if clippedLo {
		parts = append(parts, fmt.Sprintf("lower tail to min=%s hidden (%.2g%% of n)", fn(d.Min()), d.CDF(clipLo)*100))
	}
	if clippedHi {
		parts = append(parts, fmt.Sprintf("upper tail to max=%s hidden (%.2g%% of n)", fn(d.Max()), (1-d.CDF(clipHi))*100))
	}
	if len(parts) == 0 {
		return ""
	}
	return "showing x ∈ [" + fn(clipLo) + ", " + fn(clipHi) + "] · " + strings.Join(parts, " · ")
}

// formatBandStateLine builds the always-visible band-state line shown
// when the exact band is ready: the family and the sample size it was
// calibrated at, flagged conservative when that size lags the true count
// (a capped or bucketed solve, bandN < sampleN) — the staleness made
// visible without needing a hover. Pure (no egui) so it is unit-testable.
func formatBandStateLine(method ecdfbands.BandMethodE, bandN, sampleN int) string {
	s := "exact band · " + method.String() + " · n=" + strconv.Itoa(bandN)
	if bandN < sampleN {
		s += "  (sample " + strconv.Itoa(sampleN) + ", conservative)"
	}
	return s
}

// humanizeValue renders one summary value for the compact level-1 label
// in a form tuned for at-a-glance reading rather than full precision:
//
//   - values in the "comfortable" band [0.001, 1000) print as plain
//     decimals at ~3 significant figures (0.093, 18, 89.2) — no metric
//     prefix, because a bare fraction reads more naturally than "93m"
//     and this is where most latency / ratio / small-count summaries
//     live;
//   - larger or smaller magnitudes take an SI metric prefix so the label
//     never spills into hard-to-scan scientific notation: 1234 → "1.23k",
//     4.5e6 → "4.5M", 1.2e-5 → "12µ", -2.5e9 → "-2.5G";
//   - exact zero is "0"; non-finite values fall back to strconv's 'g'
//     form ("NaN", "+Inf", "-Inf") so a degenerate digest never panics
//     the label.
//
// Three significant figures keep every token short while preserving the
// scale that matters for reading a distribution at a glance; callers
// needing units, fixed precision, or a different rounding override the
// whole formatter via [Renderer.Format] (the inspector window still
// carries the exact ECDF / boxenplot for full fidelity). The SI prefix
// set and the 1000-rollover guard come from [humanize.ComputeSI] so the
// prefixes match the rest of the app (sccmap's humanizeCount, the task
// estimator).
func humanizeValue(v float64) string {
	if v == 0 {
		return "0"
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return strconv.FormatFloat(v, 'g', 4, 64)
	}
	// Round to the display precision first so a value that rounds up across a
	// power-of-1000 boundary (999 999 → 1 000 000) takes the next prefix
	// ("1M", not "1000k") and lands in the correct plain/SI branch.
	v = roundSig(v, 3)
	if a := math.Abs(v); a >= 1000 || a < 1e-3 {
		// Outside the comfortable band: factor out a metric prefix so the
		// mantissa lands back in [1, 1000). ComputeSI carries the sign onto
		// the mantissa and bumps the exponent for an exact 1000, so sigFig
		// formats the signed mantissa directly.
		mant, prefix := humanize.ComputeSI(v)
		return sigFig(mant, 3) + prefix
	}
	return sigFig(v, 3)
}

// roundSig rounds v to sig significant figures. Used to settle the
// display value before SI-prefix selection so a quantile that rounds up
// past a power-of-1000 boundary is grouped with the next prefix. Falls
// back to v unchanged for magnitudes so extreme that the 10^k scale
// factor overflows (well outside any realistic distribution), leaving
// the downstream formatter to cope.
func roundSig(v float64, sig int) float64 {
	p := math.Pow(10, float64(sig-1)-math.Floor(math.Log10(math.Abs(v))))
	if r := math.Round(v*p) / p; !math.IsNaN(r) && !math.IsInf(r, 0) {
		return r
	}
	return v
}

// sigFig formats x with sig significant figures in positional (never
// scientific) notation, trimming trailing zeros and any bare decimal
// point so round values read cleanly ("18", not "18.0"). The sign is
// preserved. The decimal count is derived from x's order of magnitude
// and capped so a tiny mantissa cannot demand an absurd field width.
func sigFig(x float64, sig int) string {
	if x == 0 {
		return "0"
	}
	neg := x < 0
	ax := math.Abs(x)
	decimals := min(12, max(0, sig-1-int(math.Floor(math.Log10(ax)))))
	s := strconv.FormatFloat(ax, 'f', decimals, 64)
	if strings.IndexByte(s, '.') >= 0 {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if neg {
		s = "-" + s
	}
	return s
}

// formatSummary builds the level-1 label string. Layout:
//
//	[icon NBSP] [n=N two-NBSPs] min " │ " Q1 " " Med " " Q3 " │ " max
//
// where the inner pipes are U+2502 BOX DRAWINGS LIGHT VERTICAL — they
// hint at whisker / box / whisker without painting any geometry, and
// the NBSP (U+00A0) keeps the icon glued to the first numeric token.
//
// On n==0 the label degenerates to "(no data)" (with the icon when
// requested) so the affordance keeps a stable cell width across rows.
func formatSummary(s fiveNumberSummary, showN, showIcon bool, fn FormatFunc, unit string) string {
	var b strings.Builder
	if showIcon {
		b.WriteString(icons.IconChartLine)
		b.WriteString(" ")
	}
	if s.n == 0 {
		b.WriteString("(no data)")
		return b.String()
	}
	if showN {
		b.WriteString("n=")
		b.WriteString(strconv.FormatInt(s.n, 10))
		b.WriteString("  ")
	}
	b.WriteString("p0 ")
	b.WriteString(fn(s.min))
	b.WriteString(" · p25 ")
	b.WriteString(fn(s.q1))
	b.WriteString(" · p50 ")
	b.WriteString(fn(s.median))
	b.WriteString(" · p75 ")
	b.WriteString(fn(s.q3))
	b.WriteString(" · p100 ")
	b.WriteString(fn(s.max))
	if unit != "" {
		b.WriteString(" ")
		b.WriteString(unit)
	}
	return b.String()
}
