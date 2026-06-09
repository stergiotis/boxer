package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/schemaview"
)

// =============================================================================
// componentview schema demo — the TableDesc behind the componentview record
//
// schemaview (the leeway TableDesc inspector) over the very table the
// componentview demo's drone record lives in. The typed widgets recognise only
// identity/battery/tasked; this shows the full schema those components are a
// subset of, so every column — recognised or not — is discoverable at the GUI
// level. Reuses the shared cvDroneData TableDesc (discovered from the record's
// physical column names).
// =============================================================================

type cvSchemaDemoState struct {
	model  *schemaview.Model
	errMsg string
}

func newCvSchemaState() (st *cvSchemaDemoState) {
	st = &cvSchemaDemoState{}
	d := ensureCvDroneData()
	if d.err != "" {
		st.errMsg = d.err
		return
	}
	st.model = schemaview.NewModel(&d.tblDesc)
	return
}

func init() {
	registry.Register(registry.Demo{
		Name:     "componentview-schema",
		Category: "Leeway",
		Title:    icons.IconTreeStructure + " component view · schema",
		Stage:    [2]float32{1100, 760},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "The leeway TableDesc behind the componentview demo's drone " +
			"record, inspected with schemaview: a master-detail view of every " +
			"section (canonical types, encoding hints, value semantics, membership " +
			"spec, groups). The typed componentview widgets recognise only " +
			"identity/battery/tasked; this is the full schema those components are a " +
			"subset of, so every column is discoverable at the GUI level.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			return newCvSchemaState()
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			renderCvSchemaDemo(ids, state.(*cvSchemaDemoState))
		},
	})
}

func renderCvSchemaDemo(ids *c.WidgetIdStack, st *cvSchemaDemoState) {
	if st.errMsg != "" {
		c.Label("componentview schema unavailable: " + st.errMsg).Wrap().Send()
		return
	}
	schemaview.Render(schemaview.Input{Ids: ids, ScopeKey: "cvschema", Model: st.model})
}
