// Demo-registry enrollment for configview (ADR-0057). This replaces the
// former per-app screenshot tour: instead of a settle/capture/advance state
// machine driven by a screenshot-mode SeededFuncApp, configview registers a
// single Demo whose body is the live config inspector rendered into the host
// Ui scope. The central TestDriver (widgets) captures it as configview.png,
// and it also appears in the widget gallery via registry.Embed.

package configview

import (
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

func init() {
	registry.Register(registry.Demo{
		Name:           "configview",
		Category:       "Runtime",
		Title:          icons.PhGear + " Config inspector",
		Stage:          [2]float32{720, 600},
		Kind:           registry.DemoKindMixed,
		Description:    "The typed env-var registry grouped by category — set/unset state, type chips, sensitive-value masking, and CLI-flag chips. The Database category is pre-expanded.",
		Init:           tourInit,
		RenderStateful: tourRenderStateful,
		SourceFunc:     (*App).render,
	})
}

// tourInit builds a configview instance bound to the host-supplied id stack
// and pins the capture scene: no filter, Database category pre-expanded. The
// CollapsingHeader open state is seeded from expandedCat rather than persisted
// egui memory, so the rendered scene is stable regardless of prior interaction.
func tourInit(ids *c.WidgetIdStack) (state any) {
	inst := newInstance(manifest)
	inst.ids = ids
	inst.filter = Filter{}
	inst.expandedCat = env.CategoryDatabase
	state = inst
	return
}

func tourRenderStateful(ids *c.WidgetIdStack, state any) {
	inst, ok := state.(*App)
	if !ok || inst == nil {
		return
	}
	inst.ids = ids
	_ = inst.render()
}
