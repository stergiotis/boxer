package sqlappletcreator

// sqlappletcreator_tour.go enrols one synthetic scene into the imzero2 demo
// registry (ADR-0057, keelson-app A8) so the central TestDriver captures the
// authoring form in the widgets tour. The live App is opened by the SQL
// playground on its buffer; the tour seeds the same form with a fixed buffer
// and drafts, with no bus behind it, so the scene is deterministic and
// network-free. Screenshot scaffolding only: Save and Export report the
// missing bus in the status line instead of acting.

import (
	"github.com/stergiotis/boxer/apps/sqlappletcreator/appletcreatecfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sqleditor"
)

// tourSQL is the scene's buffer: two statements over the introspection
// endpoint, so the statement note under the editor has something to say.
const tourSQL = `SET param_topic = 'data';
SELECT id, display, summary
FROM keelson('apps')
WHERE has(topics, {topic:String})
ORDER BY display`

func init() {
	registry.Register(registry.Demo{
		Name:     "sqlappletcreator-form",
		Category: "UX",
		Title:    icons.PhPuzzlePiece + " SQL applet creator — the authoring form",
		Stage:    [2]float32{700, 520},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "The form that composes an applet document from a buffer: the SQL in the shared editing " +
			"surface, the slug, title, summary and icon drafts, the endpoint note, and the Save and Export " +
			"actions. The live window is opened by the SQL playground on its buffer; Save sends the document " +
			"to the applet store, which validates it and mints a manifest, and Export writes it to a file " +
			"through the Powerbox dialog.",
		Init:           creatorTourInit,
		RenderStateful: creatorTourRender,
		SourceFunc:     (*App).renderFormBody,
	})
}

func creatorTourInit(ids *c.WidgetIdStack) (state any) {
	inst := &App{ids: ids}
	inst.editor = sqleditor.New(ids, "sql")
	inst.sql = tourSQL
	inst.endpoint = appletcreatecfg.EndpointIntrospection
	inst.slug = "apps-by-topic"
	inst.title = "Apps by topic"
	inst.summary = "The registered apps under one topic, as a relation"
	inst.icon = icons.PhPuzzlePiece
	return inst
}

func creatorTourRender(ids *c.WidgetIdStack, state any) {
	if inst, ok := state.(*App); ok && inst != nil {
		inst.ids = ids
		inst.renderFormBody()
	}
}
