// Demo-registry enrollment for splashscreen (ADR-0057). Each scene below
// registers as its own Demo whose body is the splash App rendered into the
// host Ui scope, pinned to one tab. The central TestDriver (widgets)
// captures one PNG per scene, and they appear in the widget gallery via
// registry.Embed. The scenes show boxer's tabs; a program registered by the
// binary running the gallery adds a tab, but the scenes stay on boxer's.

package splashscreen

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// splashScenes is one entry per registered Demo. Each pins a first-row
// selection and a page; the shared Init/RenderStateful pair builds an App
// fixed to them and renders it.
var splashScenes = []struct {
	name   string
	sel    string
	page   pageE
	title  string
	desc   string
	source any
}{
	{"splashscreen-splash", boxerKey, pageSplash, icons.PhSparkle + " Boxer splash",
		"boxer's Splash page — the bundled portrait artwork scaled to fit via the imzero2 Image widget (a placeholder line when the asset is absent).",
		(*App).renderSplash},
	{"splashscreen-about", boxerKey, pageAbout, icons.PhInfo + " About boxer",
		"boxer's About page — its copyright, licence and NOTICE, in a scroll area.",
		(*App).renderAbout},
	{"splashscreen-build", buildKey, pageSplash, icons.PhWrench + " Build",
		"The Build tab — the running binary's revision, module and run identity.",
		(*App).renderBuild},
}

func init() {
	for _, sc := range splashScenes {
		registry.Register(registry.Demo{
			Name:           sc.name,
			Category:       "Demos",
			Title:          sc.title,
			Stage:          [2]float32{620, 820},
			Flags:          registry.DemoFlagNeedsLargeArea,
			Kind:           registry.DemoKindUX,
			Description:    sc.desc,
			Init:           makeTourInit(sc.sel, sc.page),
			RenderStateful: tourRenderStateful,
			SourceFunc:     sc.source,
		})
	}
}

// makeTourInit returns an Init that builds a splash App fixed to sel and
// page. Mount normally loads the bundled artwork and NOTICE, but the
// gallery and tour build instances without Mount, so the (idempotent,
// sync.Once) loadAssets runs here. The App is bound to the tour-provided id
// stack (inst.ids), onto which the host/tour pushes a window-unique salt
// before each frame.
func makeTourInit(sel string, page pageE) func(ids *c.WidgetIdStack) (state any) {
	return func(ids *c.WidgetIdStack) (state any) {
		loadAssets()
		inst := newApp()
		inst.sel = sel
		inst.page = page
		inst.ids = ids
		state = inst
		return
	}
}

// tourRenderStateful renders the splash App on the host/tour-provided id
// stack: it rebinds inst.ids to the freshly-scoped stack each frame, then
// drives Frame, which renders on inst.ids without Reset()ing it — the
// window-unique salt on that stack keeps widget ids disjoint across
// instances.
func tourRenderStateful(ids *c.WidgetIdStack, state any) {
	inst, ok := state.(*App)
	if !ok || inst == nil {
		return
	}
	inst.ids = ids
	_ = inst.Frame(nil)
}
