package play

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
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
// outer ScrollArea). Unlike the gallery host (a vertically-unbounded scroll
// host), a dock-tab leaf already bounds the widget's height, so FillHost makes
// it fill the leaf instead of flooring to dockMinHeight — the floor overflows
// the (shorter) leaf and its nested dock paints across the neighbouring panes
// once the section list scrolls.
func (inst *PlayApp) renderSchemaView() {
	if inst.schemaModel == nil || inst.schemaModel.Table == nil {
		for rt := range c.RichTextLabel("No schema to display.") {
			rt.Small().Weak()
		}
		return
	}
	schemaview.Render(schemaview.Input{Ids: inst.ids, ScopeKey: "play-schema", Model: inst.schemaModel, FillHost: true})
}

// syncSchemaModel rebinds the inspector's TableDesc when the active result's
// Arrow schema changes, keyed by pointer identity — the same cheap once-per-
// result cache as colWidthsForSchema and the projector's forSchema. The
// pointer gate also keeps SetTable (which resets the widget's selection /
// filter) from firing every frame.
func (inst *PlayApp) syncSchemaModel(schema *arrow.Schema) {
	if inst.schemaForSchema == schema {
		return
	}
	inst.schemaForSchema = schema
	inst.schemaModel.SetTable(inst.resultTableDesc(schema))
}

// resultTableDesc returns the leeway schema for the current result. The faithful
// path is the reconstruction the CardDriver already derives from the physical
// column names — the SAME derivation the Detail card uses, so the schema is
// computed once in the play core, not re-run here. Only a non-leeway result
// (an aggregation, a join, a non-leeway table) whose names don't parse falls
// back to the shallow opaque inference off the Arrow types.
func (inst *PlayApp) resultTableDesc(schema *arrow.Schema) *common.TableDesc {
	if schema == nil {
		return nil
	}
	if inst.cards != nil {
		inst.cards.EnsureFor(schema)
		if td := inst.cards.TableDesc(); td != nil {
			return td
		}
	}
	return inferOpaqueTableDesc(schema.Fields())
}
