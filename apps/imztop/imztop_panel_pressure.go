//go:build llm_generated_opus48

package imztop

import (
	"fmt"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/psi"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// renderPressurePanel shows Linux PSI (Pressure Stall Information): the share
// of wall-time tasks spent stalled on CPU / memory / IO, over 10/60/300 s
// windows. "some" = at least one task stalled; "full" = every non-idle task
// stalled (a strong saturation signal). CPU reports only "some".
func (inst *App) renderPressurePanel(snap *PublishedSnapshot) {
	inst.sectionHeader("Pressure (PSI)")

	ps := snap.LatestPSI
	if ps == nil || !ps.Available {
		c.Label("PSI unavailable — kernel built without CONFIG_PSI or booted with psi=0.").Send()
		return
	}

	for range c.Horizontal().KeepIter() {
		c.Label("share of time stalled · some = any task, full = all non-idle tasks · windows 10s / 60s / 300s").Send()
	}
	c.AddSpace(inst.spaceInner())

	inst.renderPressureRow("CPU", ps.CPU, false)
	inst.renderPressureRow("Memory", ps.Memory, true)
	inst.renderPressureRow("IO", ps.IO, true)
}

// renderPressureRow prints one resource's some (and optionally full) pressure
// triple. showFull is false for CPU, whose full line is always zero.
func (inst *App) renderPressureRow(name string, r psi.Resource, showFull bool) {
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel(name) {
			rt.Strong()
		}
		c.AddSpace(inst.spaceOuter())
		c.Label("some " + pressureTriple(r.Some)).Send()
		if showFull {
			c.AddSpace(inst.spaceOuter())
			c.Label("full " + pressureTriple(r.Full)).Send()
		}
	}
}

// pressureTriple formats the avg10/avg60/avg300 percentages.
func pressureTriple(p psi.Pressure) string {
	return fmt.Sprintf("%.1f / %.1f / %.1f%%", p.Avg10, p.Avg60, p.Avg300)
}
