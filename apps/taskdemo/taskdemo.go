//go:build llm_generated_opus47

// Package taskdemo is the ADR-0038 M2 + M4 showcase app. One window
// with two sections: spawn controls (pick a unit, pick a duration,
// pick whether the task should "fail" near the end), and a
// taskmonitor widget that renders the in-flight + history panes with
// per-row cancel button and error-text details.
//
// The fake worker is a single goroutine per spawned task that loops
// for the configured duration, calling Report at fixed wall-clock
// intervals. Report's humanized-change gate is what produces the
// pleasantly throttled progress bar; the gate is not visible to the
// producer code — it just calls Report as fast as it wants.
package taskdemo

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/taskmonitor"
)

// ids is the package-level WidgetIdStack. Mirrors capdemo — every
// Frame wraps the body in IdScope(PrepareSeq(seed)) so two open
// windows produce disjoint widget ids.
var ids = c.NewWidgetIdStack()

// instanceCounter feeds per-instance seeds.
var instanceCounter atomic.Uint64

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
	seed   uint64
	logger zerolog.Logger

	// tasks is the host-supplied high-level API.
	tasks task.TaskApiI

	// monitor is the reusable widget that owns in-flight + history
	// rendering. Constructed in Mount, Started immediately, Stopped
	// in Unmount.
	monitor *taskmonitor.Inst

	// density resolves IDS spacing tokens at the active preset
	// (ADR-0032 §SD2); cached once at newApp.
	density styletokens.DensityE

	// appCtx is cancelled on Unmount. Workers derive their per-task
	// context from this so window close cascades into cancellation.
	appCtx    context.Context
	cancelApp context.CancelFunc

	// durationSec is the slider-bound float (egui binds via float64).
	durationSec float64

	// simulateError gates whether the next spawned task aborts at
	// ~80% with a synthesised error.
	simulateError bool
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		seed:        instanceCounter.Add(1),
		durationSec: 5.0,
		density:     styletokens.DensityFromEnv(),
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

// Mount captures the host-supplied TaskApiI, constructs + starts the
// taskmonitor widget, and prepares an app-scoped context so Unmount
// can cancel in-flight tasks immediately rather than waiting on the
// host's reapClosed path.
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.logger = ctx.Log()
	inst.tasks = task.ForApp(ctx)
	inst.appCtx, inst.cancelApp = context.WithCancel(context.Background())

	inst.monitor = taskmonitor.New(inst.tasks, ids, "tm", taskmonitor.Opts{
		DefaultOpen: true,
	})
	if startErr := inst.monitor.Start(); startErr != nil {
		inst.logger.Warn().Err(startErr).Msg("taskdemo: monitor start failed")
	}
	return
}

// unmountDrainCapMs bounds how long Unmount waits for cancelled workers
// to publish their terminals before tearing the monitor down. Workers
// see appCtx.Done() within one reportTickMs (20ms) and then publish
// Done immediately; 500ms is generous slack and never blocks the real
// UI close-window path long enough to feel laggy.
const unmountDrainCapMs = 500

// Unmount cancels in-flight tasks via appCtx, briefly waits for the
// resulting terminal publications so OnDone drains the monitor's
// in-flight map, then stops the monitor (unsubscribes the observer).
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	if inst.cancelApp != nil {
		inst.cancelApp()
	}
	deadline := time.Now().Add(unmountDrainCapMs * time.Millisecond)
	for time.Now().Before(deadline) {
		if inst.monitor == nil || inst.monitor.InflightCount() == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if inst.monitor != nil {
		_ = inst.monitor.Stop()
	}
	return
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	ids.Reset()
	for range c.IdScope(ids.PrepareSeq(inst.seed)) {
		inst.renderApp()
	}
	return
}

// --- producer side ---------------------------------------------------

// startTask kicks off a goroutine that spawns a task of the chosen
// unit + duration and runs the fake-work loop.
func (inst *App) startTask(unit task.UnitE) {
	if inst.tasks == nil {
		return
	}
	durationMs := int64(inst.durationSec * 1000.0)
	simulateErr := inst.simulateError
	go inst.runFakeTask(unit, durationMs, simulateErr)
}

// runFakeTask is the worker goroutine. Spawns a task, ticks every
// reportTickMs until duration elapses (or until cancelled), and
// publishes Done or Error.
func (inst *App) runFakeTask(unit task.UnitE, durationMs int64, simulateErr bool) {
	total := totalForUnit(unit)
	title := titleForUnit(unit, durationMs)

	h, err := inst.tasks.Spawn(inst.appCtx, task.SpawnOpts{
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

func (inst *App) renderApp() {
	for range c.PanelTopInside(ids.PrepareStr("topbar")).Resizable(false).KeepIter() {
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
	for range c.CollapsingHeader(ids.PrepareStr("hdr-controls"),
		c.WidgetText().Text("Spawn").Keep()).
		DefaultOpen(true).KeepIter() {

		_ = c.SliderF64(ids.PrepareStr("duration"), inst.durationSec, minDurationSec, maxDurationSec).
			Text("duration (s)").
			SendRespVal(&inst.durationSec)

		_ = c.Checkbox(ids.PrepareStr("simerr"), inst.simulateError, "Simulate error at 80%").
			SendRespVal(&inst.simulateError)

		for range c.Horizontal().KeepIter() {
			if c.Button(ids.PrepareStr("start-items"),
				c.Atoms().Text("Start items task").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.startTask(task.UnitItems)
			}
			if c.Button(ids.PrepareStr("start-bytes"),
				c.Atoms().Text("Start bytes task").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.startTask(task.UnitBytes)
			}
			if c.Button(ids.PrepareStr("start-steps"),
				c.Atoms().Text("Start steps task").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.startTask(task.UnitSteps)
			}
		}

		if inst.tasks == nil {
			c.Label("Task API unavailable (M1 host?) — start the demo via a Phase-A+ carousel").Send()
		}
	}
}
