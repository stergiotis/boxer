package imztop

import (
	"fmt"
	"math"

	"github.com/stergiotis/boxer/public/math/numerical/finddivisions"
)

// talbotTicks routes a plot's Y axis through boxer's Talbot (Extended
// Wilkinson) tick generator, shaped for implot.SetupAxisTicks.
// SimpleLegibilityScorer + DefaultWeights + FastMode is the no-config
// preset; empty TalbotOptions{} degenerates scoring, so the defaults must
// be set explicitly.
//
// dmin/dmax bound the data range; m is the desired tick count (5 reads as
// "around 5"). OnlyLoose keeps the chosen view around [dmin, dmax] rather
// than letting Talbot trade coverage for nicer numbers, which matters
// because callers pin the axis to that view: a view that clipped the range
// would clip the data with it. Reports ok=false when the range is degenerate
// or nothing scored, so the axis falls back to implot's own locator.
func talbotTicks(dmin, dmax float64, m int) (layout finddivisions.AxisLayout, ok bool) {
	if !(dmax > dmin) || math.IsNaN(dmin) || math.IsNaN(dmax) {
		return
	}
	layout = finddivisions.Talbot(dmin, dmax, m, finddivisions.TalbotOptions{
		Weights:   finddivisions.DefaultWeights,
		FastMode:  true,
		OnlyLoose: true,
	}, finddivisions.SimpleLegibilityScorer{})
	if len(layout.TickValues) == 0 || !(layout.ViewMax > layout.ViewMin) {
		return finddivisions.AxisLayout{}, false
	}
	if len(layout.TickLabels) != len(layout.TickValues) {
		labels := make([]string, len(layout.TickValues))
		for i, v := range layout.TickValues {
			labels[i] = fmt.Sprintf("%g", v)
		}
		layout.TickLabels = labels
	}
	return layout, true
}

// rateUpperBound returns a stable, slightly-padded upper bound for rate
// plots so Talbot ticks don't bounce frame-to-frame as data wiggles.
// Empty / all-zero data falls back to 1 so the axis still produces 0..1
// labels rather than nothing.
func rateUpperBound(series ...[]float64) (out float64) {
	for _, s := range series {
		for _, v := range s {
			if v > out {
				out = v
			}
		}
	}
	if out < 1.0 {
		out = 1.0
		return
	}
	out *= 1.1
	return
}
