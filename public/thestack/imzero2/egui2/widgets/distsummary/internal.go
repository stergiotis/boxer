//go:build llm_generated_opus47

package distsummary

import (
	"math"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
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
