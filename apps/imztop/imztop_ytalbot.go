package imztop

import (
	"fmt"
	"math"

	"github.com/stergiotis/boxer/public/math/numerical/finddivisions"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// applyYTalbotTicks routes the plot's Y axis through boxer's Talbot
// (Extended Wilkinson) tick generator. SimpleLegibilityScorer + DefaultWeights
// + FastMode is the no-config preset called out in MEMORY.md
// (project_finddivisions_talbot_weights); empty TalbotOptions{} degenerates
// scoring, so the defaults must be set explicitly.
//
// dmin/dmax bound the data range; m is the desired tick count (5 reads as
// "around 5"). Returns the original fluid unmodified when the range is
// degenerate so egui_plot's auto-axis takes over.
func applyYTalbotTicks(p c.PlotFluid, dmin, dmax float64, m int) (out c.PlotFluid) {
	out = p
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
	labels := layout.TickLabels
	if len(labels) != len(layout.TickValues) {
		labels = make([]string, len(layout.TickValues))
		for i, v := range layout.TickValues {
			labels[i] = fmt.Sprintf("%g", v)
		}
	}
	out = p.YGridMarks(layout.TickValues, labels)
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
