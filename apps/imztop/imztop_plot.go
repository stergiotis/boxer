package imztop

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// beginTimePlot opens the app's standard history plot (ADR-0149 SD7 —
// the panels moved off the egui_plot bridge onto the implot port): fills
// the pane width (R18 available-size, one frame behind), x follows the
// rolling data window until panned or zoomed (a double-click resumes
// following), local-time calendar ticks on x, and Talbot ticks over
// [yTickLo, yTickHi] on y. Callers declare series, then End.
//
// The y axis is pinned to the tick layout's own view rather than refitted
// to the data. implot drops ticks outside the visible range, so an axis
// that fits the data cannot show the nice-number tick above it — the top
// label simply vanishes, and a request for ~5 ticks lands as two or three.
// Pinning is also what makes the callers' padding mean anything: they pass
// the range they want the axis to have. Only a degenerate layout falls back
// to refitting, and then with a zero floor.
func (inst *App) beginTimePlot(id string, height float32, ylabel string, times []float64, yTickLo, yTickHi float64) *implot.Plot {
	c.CaptureAvailableSize()
	w := c.CurrentApplicationState.StateManager.GetAvailableSize().W
	if !(w >= 200) { // NaN until the first capture lands
		w = 600
	}
	yAxis, yAxisOk := talbotTicks(yTickLo, yTickHi, 5)
	yFlags := implot.AxisFlagsAutoFit
	if yAxisOk {
		yFlags = implot.AxisFlagsNone
	}
	p := implot.Begin(inst.ids, id, w-8, height)
	p.SetupAxes("", ylabel, implot.AxisFlagsFollow, yFlags)
	if len(times) >= 2 {
		vals, labels := implot.TimeTicksLocal(times[0], times[len(times)-1], w)
		p.SetupAxisTicks(implot.AxisX1, vals, labels)
	}
	if yAxisOk {
		p.SetupAxisTicks(implot.AxisY1, yAxis.TickValues, yAxis.TickLabels)
		p.SetupAxisLimits(implot.AxisY1, yAxis.ViewMin, yAxis.ViewMax, implot.CondAlways)
	} else {
		p.IncludeY(0)
	}
	return p
}
