package implot

import (
	"testing"

	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/legend"
)

// legendFrame renders one frame of a plot with n labelled series under the
// given placement and returns its plot area and the handle.
func legendFrame(t *testing.T, place LegendPlacementE, labels []string, noLegend bool) (x, y, w, h float32, p *Plot) {
	t.Helper()
	t.Cleanup(egui2test.Install())
	ids := c.NewWidgetIdStack()
	p = Begin(ids, "##legend-place-test", 400, 300)
	p.SetupAxisLimits(AxisX1, 0, 1, CondAlways).SetupAxisLimits(AxisY1, 0, 1, CondAlways).SetupLegend(place)
	if noLegend {
		p.NoLegend()
	}
	for _, l := range labels {
		p.Line(l, []float64{0, 1}, []float64{0, 1})
	}
	p.End()
	t.Cleanup(func() { Release(p.ScopeId()) })
	x, y, w, h, ok := p.PlotAreaPrev()
	require.True(t, ok)
	return x, y, w, h, p
}

// An outside legend takes its room from the plot area — the width to the
// right, the height below — and an inside one, or none, takes nothing.
func TestOutsideLegendShrinksThePlotArea(t *testing.T) {
	labels := []string{"requests", "errors", "p99 latency"}
	x0, y0, w0, h0, _ := legendFrame(t, LegendInside, labels, false)

	x, y, w, h, p := legendFrame(t, LegendRight, labels, false)
	require.Equal(t, [3]float32{x0, y0, h0}, [3]float32{x, y, h})
	require.InDelta(t, w0-p.legendW-legendGap, w, 0.01)
	lx, ly := p.legendOrigin(x, y, w)
	require.InDelta(t, x+w+legendGap, lx, 0.01)
	require.LessOrEqual(t, lx+p.legendW, float32(400))
	require.Equal(t, y, ly)

	x, y, w, h, p = legendFrame(t, LegendBelow, labels, false)
	require.Equal(t, [3]float32{x0, y0, w0}, [3]float32{x, y, w})
	require.InDelta(t, h0-p.legendH-legendGap, h, 0.01)
	_, ly = p.legendOrigin(x, y, w)
	require.Greater(t, ly, y+h, "the legend overlaps the plot area")
	require.InDelta(t, 300-legendMargin, ly+p.legendH, 0.01)

	_, _, w, h, _ = legendFrame(t, LegendRight, labels, true)
	require.Equal(t, [2]float32{w0, h0}, [2]float32{w, h}, "NoLegend still reserved room")
	_, _, w, h, _ = legendFrame(t, LegendBelow, nil, false)
	require.Equal(t, [2]float32{w0, h0}, [2]float32{w, h}, "an empty legend reserved room")
}

// A legend below wraps at the canvas width rather than running off it.
func TestLegendBelowWraps(t *testing.T) {
	var labels []string
	for _, l := range []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliett"} {
		labels = append(labels, l+" series")
	}
	_, _, _, _, p := legendFrame(t, LegendBelow, labels, false)
	require.LessOrEqual(t, p.legendW, float32(400-2*legendMargin))
	one, _ := legend.Measure(nil, p.legendStyle())
	require.Zero(t, one)
	_, oneLine := legend.Measure([]legend.Item{{Label: "x"}}, p.legendStyle())
	require.Greater(t, p.legendH, oneLine, "ten long labels fit one line of a 400 px canvas")
}
