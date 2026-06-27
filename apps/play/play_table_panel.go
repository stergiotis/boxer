package play

import (
	"github.com/apache/arrow-go/v18/arrow"
)

// play_table_panel.go is slice 2 of ADR-0097: the Table tab as a PanelI observer
// of the `main` node. It is both consumer and producer of the `selection` signal
// (the viewof duality, SD8): Accept reads the highlighted row from the signal
// env; Render draws the grid and emits signalSelection on a row click.

type tablePanel struct {
	app *PlayApp
}

func (inst tablePanel) ID() PanelID       { return "table" }
func (inst tablePanel) BoundNode() NodeID { return mainNodeID }

// Accept claims any non-nil result; the claim is the row to highlight, read from
// the selection signal (-1 ⇒ nothing highlighted). The loading / error / empty /
// zero-row states stay in renderTableTab — they depend on the query FSM and the
// row count, not the schema shape.
func (inst tablePanel) Accept(schema *arrow.Schema, sig SignalEnvI) (claim PanelClaim, reason string) {
	if schema == nil {
		reason = "Run a query to see results."
		return
	}
	row, _ := readSelection(sig)
	claim = row
	return
}

// Render draws the master table for the claimed selection, publishing row clicks
// through emit (the producer side of the viewof duality).
func (inst tablePanel) Render(rec arrow.RecordBatch, claim PanelClaim, emit SignalEmitterI) {
	row, _ := claim.(int64)
	inst.app.renderMasterTable(rec, rec.Schema(), rec.NumRows(), row, emit)
}
