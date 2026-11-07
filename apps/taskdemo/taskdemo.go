// Package taskdemo is the ADR-0038 M2 + M4 showcase app. One window
// with two sections: spawn controls (pick a unit, pick a duration,
// pick whether the task should "fail" near the end), and a
// taskmonitor widget that renders the in-flight + history panes with
// per-row cancel button and error-text details.
//
// The fake worker is a single goroutine per spawned task, in the window's
// lifetime scope (ADR-0322), that loops for the configured duration,
// calling Report at fixed wall-clock intervals. Report's humanized-change
// gate is what produces the pleasantly throttled progress bar; the gate is
// not visible to the producer code — it just calls Report as fast as it
// wants.
package taskdemo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/taskmonitor"
)

const (
	// minDurationSec / maxDurationSec bound the duration slider.
	minDurationSec = 1.0
	maxDurationSec = 60.0

	// fakeBytesTotal is the synthetic byte-count target for UnitBytes
	// tasks. ~3 GiB — large enough that the humanizer's IBytes
	// formatting produces gigabyte-scale strings.
	fakeBytesTotal = uint64(3 * 1024 * 1024 * 1024)

	// fakeItemsTotal is the synthetic item count for UnitItems tasks.
	fakeItemsTotal = uint64(1_000)

	// fakeStepsTotal is the synthetic step count for UnitSteps tasks.
	// 5 distinct phases reads well in the humanized "step N of 5".
	fakeStepsTotal = uint64(5)

	// reportTickMs is the producer-side Report cadence.
	reportTickMs = 20

	// errorTriggerPct is the fraction-of-progress at which the
	// "simulate error" toggle aborts the task.
	errorTriggerPct = 0.80
)

// App is the per-window taskdemo instance.
type App struct {
	// ids is the per-instance WidgetIdStack. The host pre-pushes a
	// window-unique salt onto it before every Frame() call (ADR-0026
	// §SD9, windowhost.renderWindowBody), so every widget id the app
	// derives is unique across all concurrently open instances — even
	// when two apps share a label string ("topbar", …). Captured from
	// ctx.Ids() in Mount; the app must NOT Reset() it.
	ids *c.WidgetIdStack

	logger zerolog.Logger

	// tasks is the host-supplied high-level API.
	tasks task.TaskApiI

	// monitor is the reusable widget that owns in-flight + history
	// rendering. Constructed in Mount, Started immediately, Stopped
	// in Unmount.
	monitor *taskmonitor.Monitor

	// density resolves IDS spacing tokens at the active preset
	// (ADR-0032 §SD2); cached once at newApp.
	density styletokens.DensityE

	// scope is the window's lifetime: the workers run in it, so the
	// host's retire at close cancels their tasks and its drain waits for
	// them. ownScope says the host gave none and the window made its own,
	// which Unmount retires.
	scope    *lifetime.Scope
	ownScope bool

	// durationSec is the slider-bound float (egui binds via float64).
	durationSec float64

	// simulateError gates whether the next spawned task aborts at
	// ~80% with a synthesised error.
	simulateError bool
	// slowFrame makes every Frame take slowFrameDelay, for the window
	// host's slow-window notice (ADR-0351 §SD6) to report.
	slowFrame bool
}

// slowFrameDelay is above the window host's line for a slow Frame.
const slowFrameDelay = 12 * time.Millisecond

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		ids:         c.NewWidgetIdStack(),
		durationSec: 5.0,
		density:     styletokens.ActiveDensity(),
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

// Mount captures the host-supplied TaskApiI, the window's lifetime scope
// the workers run in, and constructs + starts the taskmonitor widget.
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.logger = ctx.Log()
	inst.tasks = task.ForApp(ctx)
	if inst.scope = app.LifetimeOf(ctx); inst.scope == nil {
		// A host without a lifetime scope (a bare test context).
		inst.scope, inst.ownScope = lifetime.New(nil), true
	}

	inst.monitor = taskmonitor.New(inst.ids, "tm", inst.tasks, taskmonitor.Options{
		DefaultOpen: true,
	})
	if startErr := inst.monitor.Start(); startErr != nil {
		inst.logger.Warn().Err(startErr).Msg("taskdemo: monitor start failed")
	}
	return
}

// Unmount stops the monitor (unsubscribes the observer) and returns. The
// workers' tasks were cancelled when the host retired the scope before
// Unmount, and the host drains the scope after it, off the render
// goroutine (ADR-0322); a scope of the window's own retires here. The
// terminals the workers publish after this land in no window.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	if inst.ownScope {
		inst.scope.Retire()
	}
	if inst.monitor != nil {
		_ = inst.monitor.Close()
	}
	return
}

// Frame renders one frame of the taskdemo window body. The host has
// already pre-pushed a window-unique salt onto inst.ids via c.IdScope
// (windowhost.renderWindowBody, ADR-0026 §SD9), so widget ids derived
// from inst.ids are unique across all concurrently open instances — the
// app must not Reset() the stack or wrap the body in its own instance
// salt (doing so discards the host salt and collides with sibling apps
// that share a label string).
func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	inst.renderApp()
	if inst.slowFrame {
		time.Sleep(slowFrameDelay)
	}
	return
}

// --- producer side ---------------------------------------------------

// startTask starts a worker in the window's scope that spawns a task of
// the chosen unit + duration and runs the fake-work loop. Nothing starts
// once the scope retired.
func (inst *App) startTask(unit task.UnitE) {
	if inst.tasks == nil {
		return
	}
	durationMs := int64(inst.durationSec * 1000.0)
	simulateErr := inst.simulateError
	inst.scope.Go(func(ctx context.Context) {
		inst.runFakeTask(ctx, unit, durationMs, simulateErr)
	})
}

// runFakeTask is the worker. Spawns a task under ctx, the scope's work
// context, so the handle is cancelled with the scope; ticks every
// reportTickMs until duration elapses (or until cancelled), and publishes
// Done or Error.
func (inst *App) runFakeTask(ctx context.Context, unit task.UnitE, durationMs int64, simulateErr bool) {
	total := totalForUnit(unit)
	title := titleForUnit(unit, durationMs)

	h, err := inst.tasks.Spawn(ctx, task.SpawnOpts{
		Kind:        "demo." + unit.String(),
		Title:       title,
		Cancellable: true,
		EstimatedMs: durationMs,
	})
	if err != nil {
		inst.logger.Warn().Err(err).Msg("taskdemo: spawn failed")
		return
	}

	startedAt := time.Now()
	ticker := time.NewTicker(reportTickMs * time.Millisecond)
	defer ticker.Stop()

	deadline := startedAt.Add(time.Duration(durationMs) * time.Millisecond)
	errorAt := startedAt.Add(time.Duration(float64(durationMs)*errorTriggerPct) * time.Millisecond)

	for {
		if h.Cancelled() {
			_ = h.Done(nil)
			return
		}
		now := time.Now()
		if simulateErr && now.After(errorAt) {
			_ = h.Error(errors.New("taskdemo: simulated failure at 80%"), "simulated failure")
			return
		}
		if now.After(deadline) {
			h.Report(task.ProgressReport{Current: total, Total: total, Unit: unit})
			_ = h.Done(nil)
			return
		}
		fraction := float64(now.Sub(startedAt)) / float64(deadline.Sub(startedAt))
		if fraction > 1 {
			fraction = 1
		}
		current := uint64(float64(total) * fraction)
		h.Report(task.ProgressReport{
			Current: current,
			Total:   total,
			Unit:    unit,
		})
		<-ticker.C
	}
}

func totalForUnit(u task.UnitE) (total uint64) {
	switch u {
	case task.UnitBytes:
		total = fakeBytesTotal
	case task.UnitSteps:
		total = fakeStepsTotal
	default:
		total = fakeItemsTotal
	}
	return
}

func titleForUnit(u task.UnitE, durationMs int64) (s string) {
	s = fmt.Sprintf("Fake %s task · %.1fs", u.String(), float64(durationMs)/1000.0)
	return
}

// --- render ----------------------------------------------------------

// noTaskApiLine is the empty state when the host gives the window no task
// API: nothing can be spawned here, and the remedy is a host with a bus.
const noTaskApiLine = "This host gives the window no task API, so nothing can be spawned here. " +
	"Open the demo from a host with a bus — the desktop or the headless host — to see tasks run."

func (inst *App) renderApp() {
	for range c.PanelTopInside(inst.ids.PrepareStr("topbar")).Resizable(false).KeepIter() {
		c.Label("Background task demo — spawn fake tasks; watch the humanized-change emission gate at work").Send()
	}
	for range c.PanelCentralInside().KeepIter() {
		for range c.ScrollArea().Vscroll(true).KeepIter() {
			inst.renderControls()
			c.AddSpace(styletokens.PaddingOuter(inst.density))
			// The taskmonitor widget owns the in-flight + history
			// panes including per-row progress bars, cancel buttons,
			// and error-text details. taskdemo is now just spawn
			// controls + the widget.
			if inst.monitor != nil {
				inst.monitor.Render()
			}
		}
	}
}

func (inst *App) renderControls() {
	for range c.CollapsingHeader(inst.ids.PrepareStr("hdr-controls"),
		c.WidgetText().Text("Spawn").Keep()).
		DefaultOpen(true).KeepIter() {

		_ = c.SliderF64(inst.ids.PrepareStr("duration"), inst.durationSec, minDurationSec, maxDurationSec).
			Text("duration (s)").
			SendRespVal(&inst.durationSec)

		_ = c.Checkbox(inst.ids.PrepareStr("simerr"), inst.simulateError, "Simulate error at 80%").
			SendRespVal(&inst.simulateError)
		_ = c.Checkbox(inst.ids.PrepareStr("slowframe"), inst.slowFrame, "Slow Frame (12 ms), which every window waits for").
			SendRespVal(&inst.slowFrame)

		for range c.Horizontal().KeepIter() {
			if c.Button(inst.ids.PrepareStr("start-items"),
				c.Atoms().Text("Start items task").Keep()).
				Kind(c.ButtonKindPrimary).
				SendResp().HasPrimaryClicked() {
				inst.startTask(task.UnitItems)
			}
			if c.Button(inst.ids.PrepareStr("start-bytes"),
				c.Atoms().Text("Start bytes task").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.startTask(task.UnitBytes)
			}
			if c.Button(inst.ids.PrepareStr("start-steps"),
				c.Atoms().Text("Start steps task").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.startTask(task.UnitSteps)
			}
		}

		if inst.tasks == nil {
			c.Label(noTaskApiLine).Wrap().Send()
		}
	}
}
