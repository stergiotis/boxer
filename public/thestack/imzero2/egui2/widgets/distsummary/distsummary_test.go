package distsummary

import (
	"math"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/stats/ecdfbands"
	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/ecdf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeFiveNumberSummaryNil(t *testing.T) {
	out := computeFiveNumberSummary(nil)
	assert.Equal(t, int64(0), out.n)
}

func TestComputeFiveNumberSummaryEmptyDigest(t *testing.T) {
	d := tdigest.NewTDigest()
	out := computeFiveNumberSummary(d)
	assert.Equal(t, int64(0), out.n)
}

func TestComputeFiveNumberSummarySingleton(t *testing.T) {
	d := tdigest.NewTDigest()
	d.Push(7.5)
	out := computeFiveNumberSummary(d)
	assert.Equal(t, int64(1), out.n)
	assert.InDelta(t, 7.5, out.min, 1e-12)
	assert.InDelta(t, 7.5, out.q1, 1e-12)
	assert.InDelta(t, 7.5, out.median, 1e-12)
	assert.InDelta(t, 7.5, out.q3, 1e-12)
	assert.InDelta(t, 7.5, out.max, 1e-12)
}

func TestComputeFiveNumberSummaryUniform(t *testing.T) {
	d := tdigest.NewTDigest()
	for i := 0; i <= 100; i++ {
		d.Push(float64(i))
	}
	out := computeFiveNumberSummary(d)
	require.Equal(t, int64(101), out.n)
	// Quartiles of uniform 0..100 land near 25/50/75 within centroid tolerance.
	assert.InDelta(t, 25.0, out.q1, 2.0)
	assert.InDelta(t, 50.0, out.median, 2.0)
	assert.InDelta(t, 75.0, out.q3, 2.0)
	assert.InDelta(t, 0.0, out.min, 1e-9)
	assert.InDelta(t, 100.0, out.max, 1e-9)
	// Quartile monotonicity is a hard invariant — never reorder.
	assert.LessOrEqual(t, out.q1, out.median)
	assert.LessOrEqual(t, out.median, out.q3)
}

func TestFormatSummaryFullLayout(t *testing.T) {
	s := fiveNumberSummary{n: 1024, min: 0.1, q1: 12.5, median: 18, q3: 24, max: 89.2}
	label := formatSummary(s, true, true, humanizeValue, "fps")
	assert.True(t, strings.HasPrefix(label, icons.IconChartLine), "icon prefix missing: %q", label)
	assert.Contains(t, label, "n=1024")
	// Every quantile is labelled with its percentile rank.
	for _, p := range []string{"p0", "p25", "p50", "p75", "p100"} {
		assert.Contains(t, label, p)
	}
	// Four middle-dot separators between the five label-value pairs.
	assert.Equal(t, 4, strings.Count(label, "·"))
	assert.Contains(t, label, "0.1")
	assert.Contains(t, label, "89.2")
	// Unit written once after the last value.
	assert.True(t, strings.HasSuffix(label, "fps"), "unit suffix missing: %q", label)
}

func TestFormatSummaryNoData(t *testing.T) {
	s := fiveNumberSummary{}
	label := formatSummary(s, true, true, humanizeValue, "fps")
	assert.Contains(t, label, "(no data)")
	// n=, the percentile labels, and the unit must not leak into the empty path.
	assert.NotContains(t, label, "n=")
	assert.NotContains(t, label, "p50")
	assert.NotContains(t, label, "fps")
}

func TestFormatSummaryHonoursFormatter(t *testing.T) {
	s := fiveNumberSummary{n: 3, min: 0.001, q1: 0.5, median: 1.0, q3: 1.5, max: 2.0}
	fixed := func(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }
	label := formatSummary(s, false, false, fixed, "")
	assert.Contains(t, label, "0.001")
	assert.Contains(t, label, "2.000")
	// showIcon=false, showN=false — no icon glyph, no n= term.
	assert.NotContains(t, label, icons.IconChartLine)
	assert.NotContains(t, label, "n=")
}

func TestFormatSummaryUnitOptional(t *testing.T) {
	s := fiveNumberSummary{n: 10, min: 1, q1: 2, median: 3, q3: 4, max: 5}
	intFmt := func(v float64) string { return strconv.FormatFloat(v, 'f', 0, 64) }
	withUnit := formatSummary(s, false, false, intFmt, "fps")
	assert.True(t, strings.HasSuffix(withUnit, "p100 5 fps"), "unit not appended after last value: %q", withUnit)
	noUnit := formatSummary(s, false, false, intFmt, "")
	assert.True(t, strings.HasSuffix(noUnit, "p100 5"), "empty unit should append nothing: %q", noUnit)
	assert.NotContains(t, noUnit, "fps")
}

func TestRendererUnitSetter(t *testing.T) {
	r := New("u").Unit("ms")
	assert.Equal(t, "ms", r.unit)
	// Value-receiver builder: setting Unit on a copy leaves the original empty.
	assert.Equal(t, "", New("u").unit)
}

func TestRendererDefaultsAreUsable(t *testing.T) {
	r := New("test")
	assert.Equal(t, "test", r.idPrefix)
	assert.Equal(t, float32(320), r.popupWidth)
	assert.Equal(t, float32(200), r.popupHeight)
	assert.True(t, r.showN)
	assert.True(t, r.showIcon)
	require.NotNil(t, r.formatFunc)
	// formatFunc is callable on a zero-valued input without panic.
	_ = r.formatFunc(0.0)
	// Default grid resolution must land on the documented constant
	// so callers reading the docstring can predict band smoothness.
	assert.Equal(t, defaultEcdfGridN, r.gridN)
}

func TestRendererFluentSettersReturnCopies(t *testing.T) {
	base := New("test")
	mod := base.PopupSize(640, 400).ShowN(false).ShowIcon(false)
	// Originals untouched (value receiver pattern).
	assert.Equal(t, float32(320), base.popupWidth)
	assert.True(t, base.showN)
	assert.True(t, base.showIcon)
	assert.Equal(t, float32(640), mod.popupWidth)
	assert.False(t, mod.showN)
	assert.False(t, mod.showIcon)
}

func TestRendererFormatNilIsNoop(t *testing.T) {
	r := New("test").Format(nil)
	require.NotNil(t, r.formatFunc)
	assert.Equal(t, humanizeValue(0.5), r.formatFunc(0.5))
}

// TestRendererEcdfSetterReturnsCopy locks the value-receiver contract
// on the new ECDF setter: the base Renderer's embedded ecdfPlot stays
// untouched after a fluent override. The check is a struct-equality
// comparison — ecdf.Renderer is a value type with all-comparable
// fields, so a single assert.Equal pins both the propagation of the
// caller's configuration into mod and the immutability of base.
func TestRendererEcdfSetterReturnsCopy(t *testing.T) {
	base := New("test")
	custom := ecdf.New().Method(ecdfbands.BandMethodDKW).Alpha(0.10).SeriesName("custom")
	defaults := ecdf.New()
	mod := base.Ecdf(custom)
	assert.Equal(t, defaults, base.ecdfPlot, "base must retain default ecdf renderer")
	assert.Equal(t, custom, mod.ecdfPlot, "mod must carry the caller-supplied ecdf renderer")
	assert.NotEqual(t, base.ecdfPlot, mod.ecdfPlot, "Ecdf setter did not produce a distinct value")
}

// TestRendererGridNClampsBelowMinimum exercises the documented
// "values < 2 → defaultEcdfGridN" contract so a typo at the call
// site cannot silently produce a degenerate two-point grid.
func TestRendererGridNClampsBelowMinimum(t *testing.T) {
	r := New("test").GridN(1)
	assert.Equal(t, defaultEcdfGridN, r.gridN)
	r = New("test").GridN(0)
	assert.Equal(t, defaultEcdfGridN, r.gridN)
	r = New("test").GridN(-5)
	assert.Equal(t, defaultEcdfGridN, r.gridN)
}

// TestRendererGridNAcceptsValid confirms an in-range value flows
// through unchanged.
func TestRendererGridNAcceptsValid(t *testing.T) {
	r := New("test").GridN(64)
	assert.Equal(t, 64, r.gridN)
}

// TestHumanizeValue pins the default formatter's contract: plain
// ~3-significant-figure decimals inside the comfortable [0.001, 1000)
// band, SI metric prefixes outside it, and never scientific notation.
// The boundary rows (999 999 rolling up to "1M", the band edges) guard
// the round-first prefix selection.
func TestHumanizeValue(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		// comfortable band: plain decimals, trailing zeros trimmed
		{0, "0"},
		{1, "1"},
		{3, "3"},
		{18, "18"},
		{89.2, "89.2"},
		{12.5, "12.5"},
		{2.66, "2.66"},
		{999, "999"},
		{0.1, "0.1"}, // not "100m" — fractions stay plain
		{0.093, "0.093"},
		{0.005, "0.005"},
		{0.001, "0.001"}, // lower band edge stays plain
		// negatives keep the sign, no prefix in-band
		{-8, "-8"},
		{-2.5, "-2.5"},
		// large magnitudes: SI up-prefixes instead of 1.2e+06
		{1000, "1k"},
		{1234, "1.23k"},
		{12345, "12.3k"},
		{123456, "123k"},
		{999999, "1M"}, // rounds up across the k→M boundary
		{1_000_000, "1M"},
		{4_500_000, "4.5M"},
		{2_000_000_000, "2G"},
		{-2.5e9, "-2.5G"},
		// small magnitudes: SI down-prefixes instead of 1.2e-05
		{5e-5, "50µ"},
		{1.2e-5, "12µ"},
		{0.0001234, "123µ"},
		{0.0009, "900µ"},
		{1e-9, "1n"},
		{-1.2e-5, "-12µ"},
	}
	for _, tc := range cases {
		got := humanizeValue(tc.in)
		assert.Equal(t, tc.want, got, "humanizeValue(%v)", tc.in)
		// The whole point: a summary token never carries an exponent marker.
		assert.NotContains(t, got, "e", "scientific notation leaked for %v", tc.in)
	}
	// Non-finite inputs degrade to strconv's 'g' form rather than panicking.
	assert.Equal(t, "NaN", humanizeValue(math.NaN()))
	assert.Equal(t, "+Inf", humanizeValue(math.Inf(1)))
	assert.Equal(t, "-Inf", humanizeValue(math.Inf(-1)))
}

// TestInstanceStateDefaultsToEcdfTab pins the zero-value contract:
// a freshly-opened inspector window must show the ECDF tab without
// any explicit initialiser at the call site or factory.
func TestInstanceStateDefaultsToEcdfTab(t *testing.T) {
	var s instanceState
	assert.Equal(t, tabECDF, s.tab)
}

// TestRendererTailClipDefaults pins the documented default-on adaptive
// cutoff so existing callers get it without opting in.
func TestRendererTailClipDefaults(t *testing.T) {
	r := New("t")
	assert.True(t, r.tailClipEnabled)
	assert.Equal(t, defaultTailLowerP, r.tailLowerP)
	assert.Equal(t, defaultTailUpperP, r.tailUpperP)
	assert.Equal(t, defaultTailTriggerIQR, r.tailTriggerIQR)
	assert.Equal(t, defaultExactBandBucketRatio, r.exactBandBucketRatio)
}

// TestRendererTailClipSetters exercises the value-receiver builders:
// TailClip swaps mis-ordered args and clamps to [0,1] and enables;
// NoTailClip disables; TailTrigger / ExactBandBucket set their knobs;
// all return copies that leave the base untouched.
func TestRendererTailClipSetters(t *testing.T) {
	base := New("t")
	// Mis-ordered + out-of-range args are normalised.
	clip := base.TailClip(1.5, -0.2)
	assert.Equal(t, 0.0, clip.tailLowerP)
	assert.Equal(t, 1.0, clip.tailUpperP)
	assert.True(t, clip.tailClipEnabled)
	// In-range pass-through.
	clip2 := base.TailClip(0.005, 0.995)
	assert.Equal(t, 0.005, clip2.tailLowerP)
	assert.Equal(t, 0.995, clip2.tailUpperP)
	// NoTailClip disables; base untouched (value receiver).
	off := base.NoTailClip()
	assert.False(t, off.tailClipEnabled)
	assert.True(t, base.tailClipEnabled)
	// TailTrigger / ExactBandBucket set their knobs.
	assert.Equal(t, 5.0, base.TailTrigger(5).tailTriggerIQR)
	assert.Equal(t, 2.0, base.ExactBandBucket(2).exactBandBucketRatio)
}

// TestBucketExactN pins the round-down ladder: identity below the floor
// and when disabled, ≤ n always, and stable across small drift within a
// bucket (the property that lets a live solve settle).
func TestBucketExactN(t *testing.T) {
	// Disabled (ratio ≤ 1) and small-n (≤ floor) are identity.
	assert.Equal(t, 2000, bucketExactN(2000, 1.0))
	assert.Equal(t, 200, bucketExactN(200, 1.25))
	assert.Equal(t, exactBandBucketFloor, bucketExactN(exactBandBucketFloor, 1.25))
	// Above the floor: rounds DOWN (≤ n) so the band is an over-cover.
	b := bucketExactN(2000, 1.25)
	assert.LessOrEqual(t, b, 2000)
	assert.Greater(t, b, 1600) // within √ratio of n, not collapsed
	// Stable across small upward drift inside the same bucket.
	assert.Equal(t, b, bucketExactN(2050, 1.25))
	assert.Equal(t, b, bucketExactN(2100, 1.25))
	// Monotone non-decreasing as n grows into the next bucket.
	assert.GreaterOrEqual(t, bucketExactN(3000, 1.25), b)
}

// TestTailClipBoundsHeavyTail: a smooth heavy right tail (exponential —
// the realistic shape, unlike a single outlier which a t-digest smears
// across the gap) triggers an upper clip strictly below the max, above
// the median, leaving the short lower side alone.
func TestTailClipBoundsHeavyTail(t *testing.T) {
	d := tdigest.NewTDigest()
	rnd := rand.New(rand.NewSource(3))
	for range 10_000 {
		d.Push(rnd.ExpFloat64()) // mean 1, long right tail
	}
	lo, hi, clippedLo, clippedHi := tailClipBounds(d, 0.001, 0.999, 3.0, true)
	assert.True(t, clippedHi, "long upper tail must trigger an upper clip")
	assert.False(t, clippedLo, "short lower side must not clip")
	assert.Equal(t, d.Min(), lo)
	assert.Less(t, hi, d.Max(), "clip cutoff must sit below the max (tail hidden)")
	assert.Greater(t, hi, d.Quantile(0.5), "cutoff must stay above the body")
}

// TestTailClipBoundsWellBehavedNoClip: a uniform distribution whose
// extremes lie within ~3·IQR of the quartiles is shown full-range.
func TestTailClipBoundsWellBehavedNoClip(t *testing.T) {
	d := tdigest.NewTDigest()
	for i := range 1001 {
		d.Push(float64(i)) // uniform 0..1000
	}
	lo, hi, clippedLo, clippedHi := tailClipBounds(d, 0.001, 0.999, 3.0, true)
	assert.False(t, clippedLo)
	assert.False(t, clippedHi)
	assert.Equal(t, d.Min(), lo)
	assert.Equal(t, d.Max(), hi)
}

// TestTailClipBoundsDisabled returns the full support without touching
// the digest's quantiles.
func TestTailClipBoundsDisabled(t *testing.T) {
	d := tdigest.NewTDigest()
	for i := range 500 {
		d.Push(float64(i % 10))
	}
	d.Push(1e9)
	lo, hi, clippedLo, clippedHi := tailClipBounds(d, 0.001, 0.999, 3.0, false)
	assert.False(t, clippedLo)
	assert.False(t, clippedHi)
	assert.Equal(t, d.Min(), lo)
	assert.Equal(t, d.Max(), hi)
}

// TestFormatBandStateLine names the family + calibration n, flagging
// conservative only when that n lags the true sample size.
func TestFormatBandStateLine(t *testing.T) {
	fresh := formatBandStateLine(ecdfbands.BandMethodBerkJones, 1832, 1832)
	assert.Contains(t, fresh, "Berk-Jones")
	assert.Contains(t, fresh, "n=1832")
	assert.NotContains(t, fresh, "conservative")
	stale := formatBandStateLine(ecdfbands.BandMethodBerkJones, 413, 505)
	assert.Contains(t, stale, "n=413")
	assert.Contains(t, stale, "sample 505")
	assert.Contains(t, stale, "conservative")
}

// TestFormatTailClipNote is empty when nothing was clipped and names the
// visible window + hidden upper tail (with mass) when it was.
func TestFormatTailClipNote(t *testing.T) {
	d := tdigest.NewTDigest()
	for i := range 2000 {
		d.Push(float64(i % 100))
	}
	d.Push(1e9)
	assert.Equal(t, "", formatTailClipNote(d, d.Min(), d.Max(), false, false, humanizeValue))
	note := formatTailClipNote(d, d.Min(), 99, false, true, humanizeValue)
	assert.Contains(t, note, "showing x ∈")
	assert.Contains(t, note, "upper tail")
	assert.Contains(t, note, "hidden")
	assert.Contains(t, note, "% of n")
}
