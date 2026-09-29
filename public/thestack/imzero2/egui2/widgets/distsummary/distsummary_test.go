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
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/ecdf"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
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

// TestResolvedDefaults pins the documented defaults a zero Input takes,
// and that a set field survives.
func TestResolvedDefaults(t *testing.T) {
	r := Input{}.resolved()
	assert.Equal(t, "distsummary", r.ScopeKey)
	assert.Equal(t, float32(320), r.PopupWidth)
	assert.Equal(t, float32(200), r.PopupHeight)
	assert.False(t, r.HideN)
	assert.False(t, r.HideIcon)
	require.NotNil(t, r.Format)
	_ = r.Format(0.0)
	assert.Equal(t, humanizeValue(0.5), r.Format(0.5))
	assert.Equal(t, defaultEcdfGridN, r.GridN)
	assert.Equal(t, "", Input{}.Unit)
	mod := Input{PopupWidth: 640, PopupHeight: 400, HideN: true, HideIcon: true, Unit: "ms"}.resolved()
	assert.Equal(t, float32(640), mod.PopupWidth)
	assert.True(t, mod.HideN)
	assert.True(t, mod.HideIcon)
	assert.Equal(t, "ms", mod.Unit)
	custom := ecdf.Style{Method: ecdfbands.BandMethodDKW, Alpha: 0.10, SeriesName: "custom"}
	assert.Equal(t, custom, Input{Ecdf: custom}.resolved().Ecdf, "the ECDF style passes through untouched")
}

// TestResolvedGridNClampsBelowMinimum exercises the documented
// "values < 2 → defaultEcdfGridN" contract so a typo at the call
// site cannot silently produce a degenerate two-point grid.
func TestResolvedGridNClampsBelowMinimum(t *testing.T) {
	assert.Equal(t, defaultEcdfGridN, Input{GridN: 1}.resolved().GridN)
	assert.Equal(t, defaultEcdfGridN, Input{GridN: 0}.resolved().GridN)
	assert.Equal(t, defaultEcdfGridN, Input{GridN: -5}.resolved().GridN)
	assert.Equal(t, 64, Input{GridN: 64}.resolved().GridN)
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

// TestStateDefaultsToEcdfTab pins the zero-value contract: a freshly
// opened inspector window must show the ECDF tab without any explicit
// initialiser at the call site.
func TestStateDefaultsToEcdfTab(t *testing.T) {
	var s State
	assert.Equal(t, tabECDF, s.tab)
	assert.False(t, s.Pinned())
}

// TestResolvedTailClipDefaults pins the documented default-on adaptive
// cutoff so existing callers get it without opting in.
func TestResolvedTailClipDefaults(t *testing.T) {
	r := Input{}.resolved()
	assert.False(t, r.NoTailClip)
	assert.Equal(t, defaultTailLowerP, r.TailLowerP)
	assert.Equal(t, defaultTailUpperP, r.TailUpperP)
	assert.Equal(t, defaultTailTriggerIQR, r.TailTriggerIQR)
	assert.Equal(t, defaultExactBandBucketRatio, r.ExactBandBucketRatio)
}

// TestResolvedTailClipNormalises: mis-ordered args are swapped and clamped
// to [0,1]; in-range values pass through; the other knobs are kept.
func TestResolvedTailClipNormalises(t *testing.T) {
	clip := Input{TailLowerP: 1.5, TailUpperP: -0.2}.resolved()
	assert.Equal(t, 0.0, clip.TailLowerP)
	assert.Equal(t, 1.0, clip.TailUpperP)
	clip2 := Input{TailLowerP: 0.005, TailUpperP: 0.995}.resolved()
	assert.Equal(t, 0.005, clip2.TailLowerP)
	assert.Equal(t, 0.995, clip2.TailUpperP)
	assert.True(t, Input{NoTailClip: true}.resolved().NoTailClip)
	assert.Equal(t, 5.0, Input{TailTriggerIQR: 5}.resolved().TailTriggerIQR)
	assert.Equal(t, 2.0, Input{ExactBandBucketRatio: 2}.resolved().ExactBandBucketRatio)
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

// TestProbeSlotsArePerScope guards the r21 pane probes and the band-job key
// the two popup bodies use. Two summaries whose hosts chose the same
// ScopeKey under different parent scopes must hold distinct slots, and the
// two bodies of one summary must not share one either (ADR-0267 W7).
func TestProbeSlotsArePerScope(t *testing.T) {
	slots := func(parent string) (boxen, ecdfPane, job uint64) {
		ids := c.NewWidgetIdStack()
		for range c.IdScope(ids.PrepareStr(parent)) {
			in := Input{Ids: ids, ScopeKey: "fps"}.resolved()
			for range c.IdScope(ids.PrepareStr(in.ScopeKey)) {
				boxen, ecdfPane, job = ids.ProbeSeq("boxen-pane"), ids.ProbeSeq("ecdf-pane"), uint64(in.bandJobKey())
			}
		}
		return
	}
	b1, e1, j1 := slots("left")
	b2, e2, j2 := slots("right")
	require.NotEqual(t, b1, b2, "two hosts share the boxen probe slot")
	require.NotEqual(t, e1, e2, "two hosts share the ecdf probe slot")
	require.NotEqual(t, j1, j2, "two hosts share the band job key")
	assert.NotEqual(t, b1, e1, "the two bodies of one summary share a slot")
}

// TestRenderWithoutStateReportsAnError: a nil State or Ids is a host bug the
// widget names in Result.Err instead of panicking (ADR-0267 W17).
func TestRenderWithoutStateReportsAnError(t *testing.T) {
	t.Cleanup(scenetest.Install())
	res := Render(Input{Ids: c.NewWidgetIdStack()})
	require.Error(t, res.Err)
	res = Render(Input{State: &State{}})
	require.Error(t, res.Err)
}

// TestRenderOneFrameHeadless renders the level-1 anchor and, pinned, the
// inspector window under the discard channel (ADR-0267 W19).
func TestRenderOneFrameHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	d := tdigest.NewTDigest()
	rnd := rand.New(rand.NewSource(9))
	for range 2_000 {
		d.Push(rnd.NormFloat64())
	}
	ids := c.NewWidgetIdStack()
	st := &State{}
	in := Input{Ids: ids, ScopeKey: "t", Digest: d, State: st, ExactBandMaxN: 64}
	res := Render(in)
	require.NoError(t, res.Err)
	assert.False(t, res.Pinned)
	st.SetPinned(true)
	res = Render(in)
	require.NoError(t, res.Err)
	assert.True(t, res.Pinned)
	assert.True(t, st.Pinned())
	st.tab = tabBoxenplot
	require.NoError(t, Render(in).Err)
	// Closing cancels the warm-up the pinned frames may have started.
	st.SetPinned(false)
	require.NoError(t, Render(in).Err)
	// An empty digest draws the placeholder and reports nothing.
	require.NoError(t, Render(Input{Ids: ids, ScopeKey: "e", Digest: tdigest.NewTDigest(), State: &State{}}).Err)
}
