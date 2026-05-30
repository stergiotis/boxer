//go:build llm_generated_opus48

package imztop

import (
	"fmt"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/psi"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// psiRowHeight is the per-row height of the PSI table.
const psiRowHeight float32 = 22.0

// renderPressurePanel shows Linux PSI (Pressure Stall Information): the share
// of wall-time tasks spent stalled on CPU / memory / IO, over 10/60/300 s
// windows. "some" = at least one task stalled; "full" = every non-idle task
// stalled (a strong saturation signal). CPU has no meaningful "full" line, so
// only its "some" row is shown.
func (inst *App) renderPressurePanel(snap *PublishedSnapshot) {
	inst.sectionHeader("Pressure (PSI)")

	ps := snap.LatestPSI
	if ps == nil || !ps.Available {
		c.Label("PSI unavailable — kernel built without CONFIG_PSI or booted with psi=0.").Send()
		return
	}

	for range c.Horizontal().KeepIter() {
		c.Label("share of time stalled · some = any task, full = all non-idle tasks").Send()
	}
	c.AddSpace(inst.spaceInner())

	rows := []struct {
		label string
		p     psi.Pressure
	}{
		{"CPU · some", ps.CPU.Some},
		{"Memory · some", ps.Memory.Some},
		{"Memory · full", ps.Memory.Full},
		{"IO · some", ps.IO.Some},
		{"IO · full", ps.IO.Full},
	}

	// Register-drain table: columns → headers → row-major cells → Table().
	c.TableColumn().Initial(150.0).Resizable(true).Send()
	c.TableColumn().Initial(80.0).Resizable(true).Send()
	c.TableColumn().Initial(80.0).Resizable(true).Send()
	c.TableColumn().Remainder().Send()

	c.TableHeaderText("pressure").Send()
	c.TableHeaderText("avg 10s").Send()
	c.TableHeaderText("avg 60s").Send()
	c.TableHeaderText("avg 300s").Send()

	for _, r := range rows {
		c.TableCellText(r.label).Send()
		c.TableCellText(fmt.Sprintf("%.1f%%", r.p.Avg10)).Send()
		c.TableCellText(fmt.Sprintf("%.1f%%", r.p.Avg60)).Send()
		c.TableCellText(fmt.Sprintf("%.1f%%", r.p.Avg300)).Send()
	}

	c.Table(inst.ids.PrepareStr("psi-table"), psiRowHeight, uint64(len(rows))).Striped(true).Send()
}
