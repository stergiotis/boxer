//go:build llm_generated_opus47

package distsummary

import (
	"strconv"
	"strings"

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

// defaultFormat renders a value as strconv.FormatFloat('g', 4, 64) — a
// terse representation suitable for ad-hoc dashboards. Override via
// Renderer.Format when the value domain has units or a preferred
// precision.
func defaultFormat(v float64) string {
	return strconv.FormatFloat(v, 'g', 4, 64)
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
func formatSummary(s fiveNumberSummary, showN, showIcon bool, fn FormatFunc) string {
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
	b.WriteString(fn(s.min))
	b.WriteString(" │ ")
	b.WriteString(fn(s.q1))
	b.WriteString(" ")
	b.WriteString(fn(s.median))
	b.WriteString(" ")
	b.WriteString(fn(s.q3))
	b.WriteString(" │ ")
	b.WriteString(fn(s.max))
	return b.String()
}
