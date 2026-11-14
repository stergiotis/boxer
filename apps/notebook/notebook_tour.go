// Demo-registry enrollment for the notebook window (ADR-0057): the window
// on a sample, without window chrome, so the screenshot driver and the
// gallery see it. No host serves the catalog and there is no bus, so the
// buttons take their direct fallbacks and collection says it has no store
// to reach.

package notebook

import (
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// tourSample is the sample the tour shows: the report that leads with its
// figures.
const tourSample = "report"

func init() {
	registry.Register(registry.Demo{
		Name:     "notebook-report",
		Category: "Data",
		Title:    icons.PhNotebook + " Notebook — a sealed report",
		Stage:    [2]float32{1100, 820},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindDX,
		Description: "The notebook window (ADR-0371 §SD10) on the report sample: the Read face draws the notebook from what it " +
			"stores, with no server asked, and the status row says it is sealed. The Records face is a play instance over the " +
			"notebook's records. Without a host there is no store, so Collect says what stands in its way.",
		Init:           tourInit,
		RenderStateful: tourRender,
		SourceFunc:     (*App).Frame,
	})
}

// tourInit mounts a window on a static context with no bus and opens the
// sample in it.
func tourInit(ids *c.WidgetIdStack) (state any) {
	inst := newApp()
	mc := app.NewStaticMountContext(AppId, zerolog.Nop(), nil, nil, nil)
	mc.SetIds(ids)
	if err := inst.Mount(mc); err != nil {
		inst.note = err.Error()
	}
	if inst.doc == nil && inst.opening == nil {
		if err := inst.openSample(tourSample); err != nil {
			inst.note = err.Error()
		}
	}
	return inst
}

func tourRender(ids *c.WidgetIdStack, state any) {
	inst, ok := state.(*App)
	if !ok || inst == nil {
		return
	}
	inst.ids = ids
	_ = inst.Frame(nil)
}
