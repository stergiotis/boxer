package imzrt

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// paneProbeSalt namespaces this app's pane probes inside the shared r21 slot
// map; the role string separates the call sites, and threading both through the
// instance's id stack makes each slot window-unique.
const paneProbeSalt uint64 = 0x172700721e9a0b52

// paneProbeSeq is the r21 slot for one call site's pane probe.
func (inst *App) paneProbeSeq(role string) (seq uint64) {
	return c.ProbeSeq("imzrt", role) ^ inst.ids.PrepareHighEntropy(paneProbeSalt).Derive()
}

// beginTimePlot opens the app's standard monitoring plot (ADR-0149 SD7 —
// the panels moved off the egui_plot bridge onto the implot port): fills
// the pane width (seq-keyed pane probe, one frame behind), x follows the
// rolling data window until panned or zoomed (a double-click resumes
// following), y refits every frame with a zero floor, and the x axis
// carries local-time calendar ticks. Callers declare series, then End.
func (inst *App) beginTimePlot(id string, height float32, ylabel string, times []float64) *implot.Plot {
	// One probe slot per plot id, window-unique through the instance's id
	// stack. NOT CaptureAvailableSize: a single process-wide slot the frame's
	// last capture wins. Today this app's capturers all sit in one column of
	// one tab, so they happen to agree on a width — a second column, or any
	// reader of the height, would have made them size each other.
	w, _, _ := c.CapturePaneSize(inst.paneProbeSeq("plot#" + id))
	if !(w >= 200) { // no probe has landed yet
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
