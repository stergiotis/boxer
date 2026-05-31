// Demo-registry enrollment for splashscreen (ADR-0057). This replaces the
// former per-app screenshot tour: instead of a settle/capture/advance state
// machine driven by a screenshot-mode SeededFuncApp, each tab (splash / about /
// notice) registers as its own Demo whose body is the splash App rendered into
// the host Ui scope. The central TestDriver (widgets) captures one PNG per tab,
// and they appear in the widget gallery via registry.Embed.

package splashscreen

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// splashScenes is one entry per registered Demo. Each pins a tab; the shared
// Init/RenderStateful pair builds an App fixed to that tab and renders it.
var splashScenes = []struct {
	name   string
	tab    tabE
	title  string
	desc   string
	source any
}{
	{"splashscreen-splash", tabSplash, icons.PhSparkle + " Boxer splash",
		"The boxer splash pane — bundled portrait artwork scaled to fit via the imzero2 Image widget (a placeholder line when the git-ignored asset is absent).",
		(*App).renderSplash},
	{"splashscreen-about", tabAbout, icons.PhInfo + " About boxer",
		"The About tab — app identity, version, copyright/VCS provenance, and the license line.",
		(*App).renderAbout},
	{"splashscreen-notice", tabNotice, icons.PhScroll + " NOTICE",
		"The NOTICE tab — the bundled project NOTICE text in a scroll area.",
		(*App).renderNotice},
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
			Init:           makeTourInit(sc.tab),
			RenderStateful: tourRenderStateful,
			SourceFunc:     sc.source,
		})
	}
}

// makeTourInit returns an Init that builds a splash App fixed to tab. Mount
// normally loads the bundled artwork/NOTICE, but the gallery and tour build
// instances without Mount, so the (idempotent, sync.Once) loadAssets runs
// here. newApp assigns a process-unique seed used to scope widget ids.
func makeTourInit(tab tabE) func(ids *c.WidgetIdStack) (state any) {
	return func(ids *c.WidgetIdStack) (state any) {
		loadAssets()
		inst := newApp()
		inst.tab = tab
		state = inst
		return
	}
}

// tourRenderStateful renders the splash App. splashscreen manages its own
// package-global id stack scoped by inst.seed inside Frame, so the
// host-supplied id stack is intentionally unused here.
func tourRenderStateful(_ *c.WidgetIdStack, state any) {
	inst, ok := state.(*App)
	if !ok || inst == nil {
		return
	}
	_ = inst.Frame(nil)
}
