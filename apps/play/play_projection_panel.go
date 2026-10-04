package play

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
)

// play_projection_panel.go is slice 2 of ADR-0097: the Projection (neighbour embedding)
// as a PanelI observer of the `main` node. Like the Table, it is both consumer
// and producer of the `selection` signal (SD8): Accept reads the highlighted row
// from the signal env; Render draws the neighbour graph and emits signalSelection on a
// node click. The projector lifecycle (idle / running / done) stays inside
// renderProjection.

type projectionPanel struct {
	app *PlayApp
}

func (inst projectionPanel) ID() PanelID { return "projection" }

// Channels: one required "main" channel — the rows to embed.
func (inst projectionPanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true, Label: "rows"}}
}

func (inst projectionPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "Run a query to see results."
		return
	}
	if inst.app != nil && inst.app.projector != nil {
		p := inst.app.projector
		if reason = p.shapeReason(schema); reason != "" {
			return
		}
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

// shapeReason says why a result cannot be projected, before a run would
// find out: the features are read off the leeway card, so the result must
// be leeway-shaped — a leeway table's rows with its columns as stored. The
// verdict is cached per schema; the classifier is the one the card driver
// uses, so the two cannot disagree.
func (inst *Projector) shapeReason(schema *arrow.Schema) (reason string) {
	if schema == inst.shapeSchema {
		return inst.shapeWhy
	}
	names := make([]string, 0, schema.NumFields())
	for _, f := range schema.Fields() {
		names = append(names, f.Name)
	}
	inst.shapeSchema, inst.shapeWhy = schema, ""
	if cl := datacatalog.Classify(names); cl.Kind != datacatalog.KindLeeway {
		inst.shapeWhy = "Projection reads leeway-shaped results: the rows of a leeway table such as boxer.facts, with its columns as stored (SELECT * … or a column subset that keeps the table's shape); this result's columns are not one."
	}
	return inst.shapeWhy
}
