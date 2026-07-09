package play

import (
	"github.com/apache/arrow-go/v18/arrow"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/schemaview"
)

// play_schema_panel.go is the Schema dock tab: an ADR-0097 PanelI observer of
// the `main` node that renders the schemaview inspector over a leeway TableDesc
// inferred from the result's Arrow schema (see play_schema_infer.go). It is a
// pure consumer — it reads no selection signal and emits none; the schema is a
// property of the result shape, not of any highlighted row.

type schemaPanel struct {
	app *PlayApp
}

func (inst schemaPanel) ID() PanelID { return "schema" }

// Channels: one required "main" channel — the result whose schema is inspected.
func (inst schemaPanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true, Label: "schema"}}
}

// AcceptForChannel gates only on the presence of a result schema; the schema
// shape itself is always renderable (unlike the Timeline's column contract).
// The TableDesc rebuild is not done here — Accept must stay pure — but in
// syncSchemaModel, once per frame.
func (inst schemaPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "Run a query to see its inferred leeway schema."
		return
	}
	return
}

// Render draws the inspector for the model synced this frame. emit is unused.
func (inst schemaPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	inst.app.renderSchemaView()
}

// renderSchemaTab is the Schema dock tab body: the same loading / failed /
// empty guards as the other result tabs, then the panel dispatch. The model is
// kept in sync by syncSchemaModel in the per-frame consistency block, so the
// tab body only renders it.
func (inst *PlayApp) renderSchemaTab(rec arrow.RecordBatch, schema *arrow.Schema, err error) {
	if inst.graph.MainLoading() && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		c.Label("Query failed.").Send()
		return
	}
	if rec == nil {
		inst.renderResultsEmpty()
		return
	}
	reject := dispatchPanel(schemaPanel{app: inst}, map[ChannelID]channelInput{
		chMain: {node: inst.activeNodeID(), rec: rec, schema: schema, sig: playSignals{selectedRow: inst.selectedRow}},
	}, nil)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}

// renderSchemaView draws the schemaview inspector for the current model. The
// widget owns its own two-pane dock + scroll, so it is embedded directly (no
// outer ScrollArea), mirroring the demo host.
func (inst *PlayApp) renderSchemaView() {
	if inst.schemaModel == nil || inst.schemaModel.Table == nil {
		for rt := range c.RichTextLabel("No schema to display.") {
			rt.Small().Weak()
		}
		return
	}
	schemaview.Render(schemaview.Input{Ids: inst.ids, ScopeKey: "play-schema", Model: inst.schemaModel})
}

// syncSchemaModel rebinds the inspector's TableDesc when the active result's
// Arrow schema changes, keyed by pointer identity — the same cheap once-per-
// result cache as colWidthsForSchema and the projector's forSchema. Building the
// TableDesc every frame would be wasteful; the schema pointer is stable for a
// given result, so a pointer compare gates the rebuild.
func (inst *PlayApp) syncSchemaModel(schema *arrow.Schema) {
	if inst.schemaForSchema == schema {
		return
	}
	inst.schemaForSchema = schema
	inst.schemaModel.SetTable(inferTableDesc(schema))
}
