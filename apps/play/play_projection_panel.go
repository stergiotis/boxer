package play

import (
	"github.com/apache/arrow-go/v18/arrow"
)

// play_projection_panel.go is slice 2 of ADR-0097: the Projection (UMAP scatter)
// as a PanelI observer of the `main` node. Like the Table, it is both consumer
// and producer of the `selection` signal (SD8): Accept reads the highlighted row
// from the signal env; Render draws the scatter and emits signalSelection on a
// point click. The projector lifecycle (idle / running / done) stays inside
// renderProjection.

type projectionPanel struct {
	app *PlayApp
}

func (inst projectionPanel) ID() PanelID { return "projection" }

// Channels: one required "main" channel — the points to scatter.
func (inst projectionPanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true, Label: "points"}}
}

func (inst projectionPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "Run a query to see results."
		return
	}
	row, _ := readSelection(sig)
	claim = row
	return
}

func (inst projectionPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main := filled[chMain]
	row, _ := main.Claim.(int64)
	inst.app.renderProjection(main.Rec, row, emit)
}
