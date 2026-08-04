package imztop

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// paneProbeSalt namespaces this app's pane probes inside the shared r21 slot
// map; the role string separates the panels, and threading both through the
// instance's id stack makes each slot window-unique.
const paneProbeSalt uint64 = 0x172709a1e9a0b511

// paneProbeSeq is the r21 slot for one panel's pane probe. Panels size their
// heatmap / treemap / plot from [c.CapturePaneSize] over this, never
// CaptureAvailableSize: that register is one process-wide scalar the frame's
// last capture wins, and this app renders three leaves per frame.
func (inst *App) paneProbeSeq(role string) (seq uint64) {
	return c.ProbeSeq("imztop", role) ^ inst.ids.PrepareHighEntropy(paneProbeSalt).Derive()
}

// capturePane arms this panel's probe and returns last frame's free rect.
func (inst *App) capturePane(role string) (w, h float32, ok bool) {
	return c.CapturePaneSize(inst.paneProbeSeq(role))
}

// sectionHeader draws a panel-internal heading row: bold title with a
// modest size bump, a thin horizontal rule, and a small bottom margin.
// Per-panel CollapsingHeader was dropped because egui's panel/scroll
// surfaces already give each section a clear region; the toggle only
// added noise.
func (inst *App) sectionHeader(title string) {
	for rt := range c.RichTextLabel(title) {
		rt.Strong().Size(15)
	}
	c.AddSpace(inst.spaceHair())
	c.Separator().Horizontal().Send()
	c.AddSpace(inst.spaceTight())
}
