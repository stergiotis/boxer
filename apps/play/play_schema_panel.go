package play

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
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
func (inst *PlayApp) renderSchemaTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error) {
	inst.syncSchemaModel(schema)
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		inst.renderResultsEmpty()
		return
	}
	reject := dispatchPanel(schemaPanel{app: inst}, map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("schema"), rec: rec, schema: schema, sig: inst.frameSig},
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
	if inst.schemaTable == nil {
		for rt := range c.RichTextLabel("No schema to display.") {
			rt.Small().Weak()
		}
		return
	}
	schemaview.Render(schemaview.Input{Ids: inst.ids, ScopeKey: "play-schema", Table: inst.schemaTable, State: &inst.schemaState, FillHost: true})
}

// syncSchemaModel rebinds the inspector's TableDesc when the active result's
// Arrow schema changes, keyed by pointer identity — the same cheap once-per-
// result cache as colWidthsForSchema and the projector's forSchema. The
// widget resets its selection when the Table pointer changes, so the gate
// also keeps that from firing every frame.
func (inst *PlayApp) syncSchemaModel(schema *arrow.Schema) {
	if inst.schemaForSchema == schema {
		return
	}
	inst.schemaForSchema = schema
	inst.schemaTable = inst.resultTableDesc(schema)
}

// resultTableDesc returns the leeway schema for the current result. The faithful
// path is the reconstruction the CardDriver already derives from the physical
// column names — the SAME derivation the Detail card uses, so the schema is
// computed once in the play core, not re-run here. Only a non-leeway result
// (an aggregation, a join, a non-leeway table) whose names don't parse falls
// back to the shallow opaque inference off the Arrow types. The names are
// spelled as the result stores them (spelledAsStored).
func (inst *PlayApp) resultTableDesc(schema *arrow.Schema) *common.TableDesc {
	if schema == nil {
		return nil
	}
	if inst.cards != nil {
		inst.cards.EnsureFor(schema)
		if td := inst.cards.TableDesc(); td != nil {
			return spelledAsStored(td, schema.Fields())
		}
	}
	return inferOpaqueTableDesc(schema.Fields())
}

// resultSchemaDesc is resultTableDesc without the card driver, for a read
// off the render goroutine: the same classifier over the same names, so
// the two cannot disagree. leeway says the names carry leeway's encoding.
func resultSchemaDesc(schema *arrow.Schema) (td *common.TableDesc, leeway bool) {
	names := make([]string, 0, schema.NumFields())
	for _, f := range schema.Fields() {
		names = append(names, f.Name)
	}
	if cl := datacatalog.Classify(names); cl.Kind == datacatalog.KindLeeway && cl.Table != nil {
		return spelledAsStored(cl.Table, schema.Fields()), true
	}
	return inferOpaqueTableDesc(schema.Fields()), false
}

// spelledAsStored returns td with its section and column names spelled as
// the result's physical column names spell them. Discovery folds a name to
// leeway's canonical style — u32-array — while the columns, the Table's
// headers, leeway.columns and describe_table say u32Array; the pane names a
// section the way the rest of the surface does. td is left as it is; the
// copy shares everything but the names. A name the result does not spell
// keeps its canonical form.
func spelledAsStored(td *common.TableDesc, fields []arrow.Field) *common.TableDesc {
	if td == nil {
		return nil
	}
	names := make([]string, 0, len(fields))
	for i := range fields {
		names = append(names, fields[i].Name)
	}
	labels := lwsql.BuildLabels(names)
	if len(labels) == 0 {
		return td
	}
	fold := func(s string) string {
		return string(naming.ConvertNameStyle(naming.StylableName(s), naming.LowerSpinalCase))
	}
	sections := make(map[string]string, len(td.TaggedValuesSections))
	columns := make(map[string]string, len(labels))
	plain := make(map[string]string, len(td.PlainValuesNames))
	for _, l := range labels {
		sec, col, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		fs := fold(sec)
		sections[fs] = sec
		columns[fs+"\x00"+fold(col)] = col
		plain[fold(col)] = col
	}
	spell := func(m map[string]string, key string, n naming.StylableName) naming.StylableName {
		if s, ok := m[key]; ok {
			return naming.StylableName(s)
		}
		return n
	}
	out := *td
	out.PlainValuesNames = make([]naming.StylableName, len(td.PlainValuesNames))
	for i, n := range td.PlainValuesNames {
		out.PlainValuesNames[i] = spell(plain, fold(n.String()), n)
	}
	out.TaggedValuesSections = make([]common.TaggedValuesSection, len(td.TaggedValuesSections))
	for i, sec := range td.TaggedValuesSections {
		fs := fold(sec.Name.String())
		cols := make([]naming.StylableName, len(sec.ValueColumnNames))
		for j, cn := range sec.ValueColumnNames {
			cols[j] = spell(columns, fs+"\x00"+fold(cn.String()), cn)
		}
		sec.Name, sec.ValueColumnNames = spell(sections, fs, sec.Name), cols
		out.TaggedValuesSections[i] = sec
	}
	return &out
}
