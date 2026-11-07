// Demo-registry enrollment for taskdemo (ADR-0057). Two scenes: the window
// as it opens on a host without a task API, which is deterministic, and the
// window over an in-process bus with tasks in flight and one failed, whose
// progress bars and times move, so it is flagged NonDeterministic and the
// TestDriver skips it under IMZERO2_SCREENSHOT_DETERMINISTIC.
//
// The scenes draw the controls and the monitor directly rather than through
// renderApp's panels: the gallery host is a scroll area, in which egui
// panels size against a CentralPanel they do not have (mdedit's tour found
// this).

package taskdemo

import (
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

const (
	// tourLongSec keeps the running scene's tasks in flight for as long as
	// a capture or a gallery visit lasts.
	tourLongSec = 600.0
	// tourFailSec is a task short enough to have failed, at 80% of it, by
	// the time the TestDriver captures (Init runs before the capture loop).
	tourFailSec = 0.2
)

func init() {
	registry.Register(registry.Demo{
		Name:     "taskdemo-idle",
		Category: "Runtime",
		Title:    icons.PhHourglass + " Task demo — no task API",
		Stage:    [2]float32{720, 520},
		Kind:     registry.DemoKindDX,
		Description: "The task primitive's showcase (ADR-0038) as it opens on a host that gives the window no task API: " +
			"the spawn controls, the deliberate slow-frame toggle (ADR-0351 §SD6), the line that says nothing can be " +
			"spawned here, and the monitor's two empty panes.",
		Init:           func(ids *c.WidgetIdStack) (state any) { return tourMount(ids, nil) },
		RenderStateful: tourRender,
		SourceFunc:     (*App).renderControls,
	})
	registry.Register(registry.Demo{
		Name:     "taskdemo-running",
		Category: "Runtime",
		Title:    icons.PhHourglass + " Task demo — tasks in flight",
		Stage:    [2]float32{720, 620},
		Flags:    registry.DemoFlagNonDeterministic | registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindDX,
		Description: "The same window over an in-process bus: an items task and a steps task in flight with their " +
			"progress bars and cancel buttons, and a bytes task that failed at 80% in the history pane. The bars and " +
			"times move, so the capture is not byte-stable.",
		Init:           tourRunningInit,
		RenderStateful: tourRender,
		SourceFunc:     (*App).runFakeTask,
	})
}

// tourMount builds a window on a static mount context over bus (nil for
// none) bound to the tour's id stack. Without a host lifetime the window
// makes a scope of its own; the tour never retires it, as the gallery has
// no close.
func tourMount(ids *c.WidgetIdStack, bus app.BusI) (inst *App) {
	inst = newApp()
	mc := app.NewStaticMountContext(app.AppIdT(manifest.Id), zerolog.Nop(), nil, bus, nil)
	mc.SetIds(ids)
	_ = inst.Mount(mc)
	return
}

// tourRunningInit wires an in-process bus with a client holding the
// manifest's caps, as the tests do, and starts three tasks: one that fails
// soon, two that run for the scene's lifetime.
func tourRunningInit(ids *c.WidgetIdStack) (state any) {
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)
	inst := tourMount(ids, bus.NewClient(app.AppIdT(manifest.Id), manifest.Caps))
	inst.durationSec, inst.simulateError = tourFailSec, true
	inst.startTask(task.UnitBytes)
	inst.durationSec, inst.simulateError = tourLongSec, false
	inst.startTask(task.UnitItems)
	inst.startTask(task.UnitSteps)
	// Back to the window's default, so the slider reads as it opens.
	inst.durationSec = 5.0
	return inst
}

func tourRender(ids *c.WidgetIdStack, state any) {
	inst, ok := state.(*App)
	if !ok || inst == nil {
		return
	}
	inst.ids = ids
	inst.density = styletokens.ActiveDensity()
	c.Label("Background task demo — spawn fake tasks; watch the humanized-change emission gate at work").Send()
	inst.renderControls()
	c.AddSpace(styletokens.PaddingOuter(inst.density))
	if inst.monitor != nil {
		inst.monitor.Render()
	}
}
