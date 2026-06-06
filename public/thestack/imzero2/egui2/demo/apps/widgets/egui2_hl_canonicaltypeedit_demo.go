package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/canonicaltypeedit"
)

// =============================================================================
// canonicaltypeedit widget demo — canonical-type signature editor (ADR-0067
// group/signature cut). A chip strip of primitive elements joined by '-'/'_'
// separators; click a chip to edit that primitive in the shared bar+form
// below; the 'live signature' chip is the embedded canonicaltypesummary
// level-1, whose anchor toggle pops the full tethered inspector.
//
// Stateful: the SignatureModel is caller-owned, built in Init and rendered in
// RenderStateful. Seeded to `u32-s_vc` (a two-group signature) so the chip
// strip, separators, and a populated per-chip form all show.
// =============================================================================

type ctEditDemoState struct {
	model *canonicaltypeedit.SignatureModel
}

func init() {
	registry.Register(registry.Demo{
		Name:     "canonicaltypeedit",
		Category: "Leeway",
		Title:    icons.PhBracketsAngle + " canonicaltypeedit",
		Stage:    [2]float32{900, 640},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "Editor for a leeway canonical-type signature (ADR-0067). " +
			"A chip strip of primitive elements joined by '-' (same group) or '_' " +
			"(new group); click a chip to edit that primitive in the shared bar+form " +
			"below, where the free-text bar and the grammar-mirroring controls stay " +
			"in sync (§SD2). Add / remove / select / separator-toggle build the " +
			"signature; per-chip editing stays bidirectional. The 'live signature' " +
			"row is the embedded canonicaltypesummary level-1 chip — validity dot + " +
			"footprint over the whole signature, with an anchor toggle that pops the " +
			"full tethered inspector. Chip reorder is deferred.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			m := canonicaltypeedit.NewSignatureModel()
			m.SetCanonical("u32-s_vc")
			return &ctEditDemoState{model: m}
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoCanonicalTypeEdit(ids, state.(*ctEditDemoState))
		},
		SourceFunc: demoCanonicalTypeEdit,
	})
}

func demoCanonicalTypeEdit(ids *c.WidgetIdStack, st *ctEditDemoState) {
	c.Label("Build a canonical-type signature — chips are elements, toggle '-'/'_' between them, click a chip to edit it; the 'live signature' chip pops the full inspector:").Send()
	c.Separator().Horizontal().Send()
	c.AddSpace(6)
	st.model.Render(ids, "ctedit-demo")
}
