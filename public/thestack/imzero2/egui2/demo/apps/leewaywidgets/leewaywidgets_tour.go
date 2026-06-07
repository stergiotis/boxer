// Demo-registry enrollment for the leewaywidgets showcase (ADR-0057). This
// replaces the former per-app screenshot tour: instead of a
// settle/capture/advance state machine driven by a screenshot-mode
// SeededFuncApp, each view (table2 / json / schema / fixture) registers as its
// own Demo whose body is the showcase App rendered into the host Ui scope. The
// central TestDriver (widgets) captures one PNG per view, and they appear in
// the widget gallery via registry.Embed.

package leewaywidgets_demo

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/leewaywidgets"
)

// leewayScenes is one entry per registered Demo. Each pins a selectedView; the
// shared Init/RenderStateful pair builds an App at that view and renders it.
var leewayScenes = []struct {
	name  string
	view  viewKeyE
	title string
	kind  registry.DemoKindE
	desc  string
}{
	{"leewaywidgets-table2", viewKeyTable2, "🧪 Leeway — table2 card", registry.DemoKindUX,
		"A leeway fixture rendered as a Table2 card: viridis-encoded columns built from the declarative TableDesc."},
	{"leewaywidgets-json", viewKeyJSON, "🧪 Leeway — JSON card", registry.DemoKindMixed,
		"The same fixture in its canonical JSON card form (JsonCardEmitter), syntax-highlighted."},
	{"leewaywidgets-schema", viewKeySchemaGo, "🧪 Leeway — schema.go", registry.DemoKindDX,
		"fixture_schema.go — the declarative TableDesc that drives the table2 and JSON views."},
	{"leewaywidgets-fixture", viewKeyFixtureGo, "🧪 Leeway — fixture.go", registry.DemoKindDX,
		"fixture.go — the data populator and driver wiring behind the fixture."},
}

func init() {
	for _, sc := range leewayScenes {
		registry.Register(registry.Demo{
			Name:           sc.name,
			Category:       "Leeway",
			Title:          sc.title,
			Stage:          [2]float32{1100, 700},
			Flags:          registry.DemoFlagNeedsLargeArea,
			Kind:           sc.kind,
			Description:    sc.desc,
			Init:           makeTourInit(sc.view),
			RenderStateful: tourRenderStateful,
			SourceFunc:     (*App).Frame,
		})
	}
}

// makeTourInit returns an Init that builds a showcase App bound to the
// host-supplied id stack and pinned to view. The Table2 emitter is rebuilt
// against the host stack (mirroring Mount) so table2 widget ids derive from
// the host salt rather than newApp's throwaway ctor stack; the other views
// read inst.ids directly and need no rebind.
func makeTourInit(view viewKeyE) func(ids *c.WidgetIdStack) (state any) {
	return func(ids *c.WidgetIdStack) (state any) {
		inst := newApp()
		inst.ids = ids
		inst.table2Emitter = leewaywidgets.NewTable2CardEmitter(ids, leewaywidgets.ColorPaletteViridis, nil)
		inst.selectedView = view
		state = inst
		return
	}
}

func tourRenderStateful(ids *c.WidgetIdStack, state any) {
	inst, ok := state.(*App)
	if !ok || inst == nil {
		return
	}
	inst.ids = ids
	// Gallery layout, not Frame: the tour (and interactive gallery) host the
	// demo body in a bare, unbounded-height scroll area with no CentralPanel
	// region, where Frame's *Inside side panels collapse and the central
	// content clips vertically (SKILLS.md §"Gallery Scroll-Host Layout").
	inst.renderGallery()
}
