package imzrt

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// beginTimePlot opens the app's standard monitoring plot (ADR-0149 SD7 —
// the panels moved off the egui_plot bridge onto the implot port): fills
// the pane width (R18 available-size, one frame behind), x follows the
// rolling data window until panned or zoomed (a double-click resumes
// following), y refits every frame with a zero floor, and the x axis
// carries local-time calendar ticks. Callers declare series, then End.
func (inst *App) beginTimePlot(id string, height float32, ylabel string, times []float64) *implot.Plot {
	c.CaptureAvailableSize()
	w := c.CurrentApplicationState.StateManager.GetAvailableSize().W
	if !(w >= 200) { // NaN until the first capture lands
		w = 600
	}
	p := implot.Begin(inst.ids, id, w-8, height)
	p.SetupAxes("", ylabel, implot.AxisFlagsFollow, implot.AxisFlagsAutoFit)
	if len(times) >= 2 {
		vals, labels := implot.TimeTicksLocal(times[0], times[len(times)-1], w)
		p.SetupAxisTicks(implot.AxisX1, vals, labels)
	}
	p.IncludeY(0)
	return p
}
