// Demo-registry enrollment for agentconsole (ADR-0057): the window after a
// grant, with a seeded call log, without window chrome. No host serves
// runtime.agent here — the window is mounted on a static context whose bus
// answers nothing — so the buttons report errors in the note; the picture is
// the seeded state.

package agentconsole

import (
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// tourRows is the seeded log, oldest first: a query whose result the app
// marked untrusted, a command that applied, a refused command with the
// app's reason, and a capture.
var tourRows = []row{
	{key: "k1", op: "get_state", phase: "completed",
		result: "untrusted, from window 2 · github.com/stergiotis/boxer/apps/opsdemo · get_state: {\"Note\":\"A note both of you can edit.\",\"Level\":50}"},
	{key: "k2", op: "set_note", phase: "rendered"},
	{key: "k3", op: "set_level", phase: "refused", reason: "the level is between 0 and 100"},
	{key: "k4", op: "capture svg", phase: "completed", result: "image/svg+xml · 18342 bytes, sealed"},
}

func init() {
	registry.Register(registry.Demo{
		Name:     "agentconsole-call-log",
		Category: "Runtime",
		Title:    icons.PhTerminalWindow + " Agent console — a call log",
		Stage:    [2]float32{640, 560},
		Kind:     registry.DemoKindDX,
		Description: "A coordinator driven by hand (ADR-0269): the console holds a task grant over window 2 of the " +
			"operations demo and shows the outcomes of four calls — a query whose result the app marked untrusted, " +
			"shown attributed and as data; a command that applied; a command the app refused with its reason; a capture.",
		Init:           tourInit,
		RenderStateful: tourRender,
		SourceFunc:     (*App).Frame,
	})
}

// tourInit mounts the console on a static context, which gives it a client
// and a lifetime scope of its own, then seeds the grant and the log.
func tourInit(ids *c.WidgetIdStack) (state any) {
	inst := newApp()
	mc := app.NewStaticMountContext(AppId, zerolog.Nop(), nil, nil, nil)
	mc.SetIds(ids)
	_ = inst.Mount(mc)
	inst.windowKey = "2"
	inst.op, inst.args = "set_level", "{\"level\": 72}"
	inst.grant = agent.Grant{Task: "task-tour", Handle: "tour"}
	inst.note = "act on window 2"
	for i := range tourRows {
		r := tourRows[i]
		inst.rows = append(inst.rows, &r)
	}
	inst.n = len(tourRows)
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
