package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/canonicaltypeedit"
)

// =============================================================================
// canonicaltypeedit widget demo — canonical-type editor (ADR-0067). It opens
// compact on the common single-primitive case: a single row holding just the
// free-text formula bar and a small inline caret; clicking the caret reveals the
// grammar-mirroring form below (no sequence chrome). '+ element' grows it into a
// group/signature, where a chip strip with '-'/'_' separators appears
// (progressive disclosure). The 'live type' row is the embedded
// canonicaltypesummary level-1 chip, whose anchor toggle pops the full tethered
// inspector.
//
// Stateful: the SignatureModel is caller-owned, built in Init and rendered in
// RenderStateful. Seeded to `u32l` so the simple primitive view shows its
// numeric controls populated (width, byte order = LE).
// =============================================================================

type ctEditDemoState struct {
	model *canonicaltypeedit.SignatureModel
}

func init() {
	registry.Register(registry.Demo{
		Name:     "canonicaltypeedit",
		Category: "Leeway",
		Title:    icons.PhBracketsAngle + " canonicaltypeedit",
		Stage:    [2]float32{840, 580},
		Kind:     registry.DemoKindMixed,
		Description: "Editor for a leeway canonical type (ADR-0067). It opens compact " +
			"as a single primitive — one row with just the free-text bar and a small " +
			"inline caret; click the caret to reveal the grammar-mirroring form below " +
			"(collapsed by default), the two kept in sync (§SD2). The common case thus " +
			"carries no sequence chrome. Click '+ element' to grow it " +
			"into a group/signature: a chip " +
			"strip appears with '-' (same group) / '_' (new group) separators, " +
			"add / remove / select / reorder, and one shared bar+form editing the " +
			"selected chip (bidirectional per chip). Reorder moves a chip through " +
			"the fixed '-'/'_' gaps (positional separators). The 'live type' row is " +
			"the embedded canonicaltypesummary level-1 chip, whose anchor toggle " +
			"pops the full tethered inspector.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			m := canonicaltypeedit.NewSignatureModel()
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
	c.Label("Edit a canonical type — it opens compact (one row: the bar + a caret; click the caret for the structured form); click '+ element' to grow it into a group/signature (chips with '-'/'_' separators):").Send()
	c.Separator().Horizontal().Send()
	c.AddSpace(styletokens.GapInline(styletokens.DensityFromEnv()))
	st.model.Render(ids, "ctedit-demo")
}
