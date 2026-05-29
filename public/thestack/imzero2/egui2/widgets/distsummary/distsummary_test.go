//go:build llm_generated_opus47

package distsummary

import (
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
	label := formatSummary(s, true, true, defaultFormat)
	assert.True(t, strings.HasPrefix(label, icons.IconChartLine), "icon prefix missing: %q", label)
	assert.Contains(t, label, "n=1024")
	assert.Contains(t, label, "│")
	// Box separator appears twice — once before Q1, once after Q3.
	assert.Equal(t, 2, strings.Count(label, "│"))
	assert.Contains(t, label, "0.1")
	assert.Contains(t, label, "89.2")
}

func TestFormatSummaryNoData(t *testing.T) {
	s := fiveNumberSummary{}
	label := formatSummary(s, true, true, defaultFormat)
	assert.Contains(t, label, "(no data)")
	// n= and the quartile separator must not leak into the empty path.
	assert.NotContains(t, label, "n=")
	assert.NotContains(t, label, "│")
}

func TestFormatSummaryHonoursFormatter(t *testing.T) {
	s := fiveNumberSummary{n: 3, min: 0.001, q1: 0.5, median: 1.0, q3: 1.5, max: 2.0}
	fixed := func(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }
	label := formatSummary(s, false, false, fixed)
	assert.Contains(t, label, "0.001")
	assert.Contains(t, label, "2.000")
	// showIcon=false, showN=false — no icon glyph, no n= term.
	assert.NotContains(t, label, icons.IconChartLine)
	assert.NotContains(t, label, "n=")
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
	assert.Equal(t, defaultFormat(0.5), r.formatFunc(0.5))
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

// TestInstanceStateDefaultsToEcdfTab pins the zero-value contract:
// a freshly-opened inspector window must show the ECDF tab without
// any explicit initialiser at the call site or factory.
func TestInstanceStateDefaultsToEcdfTab(t *testing.T) {
	var s instanceState
	assert.Equal(t, tabECDF, s.tab)
}

