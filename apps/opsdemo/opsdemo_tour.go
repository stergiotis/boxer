// Demo-registry enrollment for opsdemo (ADR-0057): the window in a seeded
// state, without window chrome, so the screenshot driver and the gallery see
// it. No host serves the catalog here, so the buttons take their direct
// fallbacks (the Clear button clears; Copy reports that the host gives the
// app no bus).

package opsdemo

import (
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// tourNote reads as what the two writers leave behind: the person's line
// and an agent's appended one.
const tourNote = "A note both of you can edit.\nThe agent appended this line through set_note."

// tourLevel is a level an agent set, away from the default so the slider
// reads as moved.
const tourLevel = 72

func init() {
	registry.Register(registry.Demo{
		Name:     "opsdemo-two-writers",
		Category: "Runtime",
		Title:    icons.PhHandshake + " Operations demo — two writers",
		Stage:    [2]float32{520, 360},
		Kind:     registry.DemoKindDX,
		Description: "The smallest participant in the operations contract (ADR-0269): a note and a level the person edits " +
			"in widgets and an agent edits through the catalog's set_note and set_level. Seeded as if an agent had " +
			"appended a line and moved the slider; Copy goes to the clipboard broker as a labelled payload.",
		Init:           tourInit,
		RenderStateful: tourRender,
		SourceFunc:     (*App).Frame,
	})
}

// tourInit mounts a window on a static context with no bus, so the window
// has a lifetime scope of its own for the copy's round trip, and seeds it.
func tourInit(ids *c.WidgetIdStack) (state any) {
	inst := newApp()
	mc := app.NewStaticMountContext(AppId, zerolog.Nop(), nil, nil, nil)
	mc.SetIds(ids)
	_ = inst.Mount(mc)
	inst.note, inst.level = tourNote, tourLevel
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
