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
// "around 5"). Returns empty slices when the range is degenerate so the
// axis falls back to implot's own locator.
func talbotTicks(dmin, dmax float64, m int) (values []float64, labels []string) {
	if !(dmax > dmin) || math.IsNaN(dmin) || math.IsNaN(dmax) {
		return
	}
	layout := finddivisions.Talbot(dmin, dmax, m, finddivisions.TalbotOptions{
		Weights:  finddivisions.DefaultWeights,
		FastMode: true,
	}, finddivisions.SimpleLegibilityScorer{})
	if len(layout.TickValues) == 0 {
		return
	}
	values = layout.TickValues
	labels = layout.TickLabels
	if len(labels) != len(values) {
		labels = make([]string, len(values))
		for i, v := range values {
			labels[i] = fmt.Sprintf("%g", v)
		}
	}
	return
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
