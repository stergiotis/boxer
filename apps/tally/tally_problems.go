package tally

import (
	"context"
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// problemsSQL lists the entries the walker could not read (ADR-0198 §7).
func problemsSQL(loc location) string {
	return fmt.Sprintf(`SELECT path, node_kind, content, err FROM fs(%d, %d) WHERE err != '' ORDER BY path LIMIT 1000`,
		loc.mount.Value(), loc.snap.UnixNano())
}

// auditSQL recomputes every stored block's BLAKE3 against the digest the
// walker wrote — the whole snapshot, on demand, because it costs what the
// snapshot weighs.
func auditSQL(loc location) string {
	return fmt.Sprintf(`SELECT count() AS blocks, countIf(BLAKE3(data) != hash) AS bad FROM fsdata(%d, %d)`,
		loc.mount.Value(), loc.snap.UnixNano())
}

// renderProblems is the Problems tab: the unreadable entries of the target
// pane's snapshot, and a block audit the reader runs on purpose.
func (inst *App) renderProblems(sc *storeConn) {
	p := inst.focusPane()
	loc, ok := inst.locationOf(p)
	if !ok {
		c.Label("Pick a mount on the left.").Send()
		return
	}
	res, done, perr, busy := inst.problemsLane.demand(loc.key(), func(ctx context.Context) (tableResult, error) {
		return runTable(ctx, sc.exec, problemsSQL(loc))
	})
	if busy {
		c.RequestRepaint()
		for range c.HorizontalTop().KeepIter() {
			c.Spinner().Send()
			c.Label("Reading…").Send()
		}
		return
	}
	if !done {
		return
	}
	if perr != nil {
		c.Label("Cannot read: " + perr.Error()).Send()
		return
	}
	for range c.HorizontalTop().KeepIter() {
		c.LabelAtoms(c.Atoms().BeginRichText(fmt.Sprintf("%d unreadable entries in %s", len(res.rows), inst.mountLabel(loc.mount))).Strong().End().Keep()).Selectable(false).Send()
		c.AddSpace(styletokens.GapInline(inst.density) * 3)
		if c.Button(inst.ids.PrepareStr("audit-go"), c.Atoms().Text("Audit every block").Keep()).SendResp().HasPrimaryClicked() {
			inst.auditArmed = loc.key()
		}
		if inst.auditArmed == loc.key() {
			audit, adone, aerr, abusy := inst.auditLane.demand(loc.key(), func(ctx context.Context) (tableResult, error) {
				return runTable(ctx, sc.exec, auditSQL(loc))
			})
			switch {
			case abusy:
				c.RequestRepaint()
				c.Spinner().Send()
				c.Label("Recomputing BLAKE3 over every block…").Send()
			case adone && aerr != nil:
				c.Label("Audit failed: " + aerr.Error()).Send()
			case adone && len(audit.rows) == 1 && len(audit.rows[0]) == 2:
				c.Label(fmt.Sprintf("Audit: %s blocks, %s bad", audit.rows[0][0], audit.rows[0][1])).Send()
			}
		}
	}
	if len(res.rows) == 0 {
		c.Label("Every entry of this snapshot was read cleanly.").Send()
		return
	}
	inst.problemsTable.scopeKey = "problems-table"
	inst.problemsTable.headers = res.headers
	inst.problemsTable.rows = res.rows
	inst.problemsTable.widths = []float32{420, 90, 90, 500}
	if clicked := inst.problemsTable.render(inst.ids, inst.density); clicked >= 0 {
		inst.travelToRow(p, res, clicked)
	}
}
