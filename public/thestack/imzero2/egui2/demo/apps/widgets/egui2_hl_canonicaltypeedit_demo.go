package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/canonicaltypeedit"
)

// =============================================================================
// canonicaltypeedit widget demo — single-primitive canonical-type editor
// (ADR-0067). The formula bar and the structured form edit one type and stay
// in sync; the live chip at the bottom is the embedded canonicaltypesummary
// level-1, whose anchor toggle pops the full tethered inspector.
//
// Stateful: the editor Model is caller-owned, so it is built in Init and
// rendered in RenderStateful. Seeded to `u32l` so the numeric controls (width,
// byte order = LE) show populated.
// =============================================================================

type ctEditDemoState struct {
	model *canonicaltypeedit.Model
}

func init() {
	registry.Register(registry.Demo{
		Name:     "canonicaltypeedit",
		Category: "Leeway",
		Title:    icons.PhBracketsAngle + " canonicaltypeedit",
		Stage:    [2]float32{760, 560},
		Kind:     registry.DemoKindMixed,
		Description: "Editor for a single primitive leeway canonical type " +
			"(ADR-0067). Two synchronised views of one type: a free-text formula " +
			"bar holding the canonical string, and a structured form whose controls " +
			"mirror the grammar (family → base → family-specific modifiers → scalar " +
			"shape), so invalid shapes are unrepresentable from the form. Editing " +
			"either side updates the other (ADR-0067 §SD2 edge-ownership). The 'live " +
			"type' row is the embedded canonicaltypesummary level-1 chip — its " +
			"validity dot and footprint trailer are the editor's status, and its " +
			"anchor toggle pops the full tethered inspector. Single primitive; " +
			"groups and signatures are deferred.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			m := canonicaltypeedit.NewModel()
			m.SetCanonical("u32l")
			return &ctEditDemoState{model: m}
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoCanonicalTypeEdit(ids, state.(*ctEditDemoState))
		},
		SourceFunc: demoCanonicalTypeEdit,
	})
}

func demoCanonicalTypeEdit(ids *c.WidgetIdStack, st *ctEditDemoState) {
	c.Label("Edit a primitive canonical type — the formula bar and the form stay in sync; the 'live type' chip pops the full inspector:").Send()
	c.Separator().Horizontal().Send()
	c.AddSpace(6)
	st.model.Render(ids, "ctedit-demo")
}
