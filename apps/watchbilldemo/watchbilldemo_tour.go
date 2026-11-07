// Demo-registry enrollment for watchbilldemo (ADR-0057, keelson-app A8):
// one Demo draws the window over a seeded list — the controls, a queue
// across the states a demo.sleep job passes through, and the status note
// — set on the instance directly. No bus, worker or poller runs under the
// tour, so the picture is the same on every machine.

package watchbilldemo

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "watchbilldemo-jobs",
		Category: "Runtime",
		Title:    icons.PhClipboardText + " Watchbill demo — a seeded queue",
		Stage:    [2]float32{900, 600},
		Kind:     registry.DemoKindUX,
		Description: "The watchbill demo window: the enqueue controls (duration, attempts, backoff, fail at the end), " +
			"and the queue as the worker lists it, with demo.sleep jobs running, failed under the policy, discarded and succeeded.",
		Init:           tourInit,
		RenderStateful: tourRenderStateful,
		SourceFunc:     (*App).render,
	})
}

// tourJobs is the seeded queue: the states one demo.sleep subject or the
// other passes through.
var tourJobs = []watchbillstore.Job{
	{ID: "01J7X3A1B2C3D4E5F6G7H8J9K0", Kind: KindSleep, Subject: Subject(30*time.Second, false), State: watchbillstore.StateRunning,
		Attempt: 1, MaxAttempts: 1, WorkerRun: "run-demo-1"},
	{ID: "01J7X39Z8Y7X6W5V4U3T2S1R0Q", Kind: KindSleep, Subject: Subject(3*time.Second, true), State: watchbillstore.StateFailed,
		Attempt: 1, MaxAttempts: 3, Backoff: watchbillstore.BackoffLinear, WorkerRun: "run-demo-1", LastError: errAskedToFail.Error()},
	{ID: "01J7X38M7N6P5Q4R3S2T1U0V9W8", Kind: KindSleep, Subject: Subject(500*time.Millisecond, true), State: watchbillstore.StateDiscarded,
		Attempt: 2, MaxAttempts: 2, Backoff: watchbillstore.BackoffNone, WorkerRun: "run-demo-1", LastError: errAskedToFail.Error()},
	{ID: "01J7X37K6L5M4N3P2Q1R0S9T8U7", Kind: KindSleep, Subject: Subject(3*time.Second, false), State: watchbillstore.StateSucceeded,
		Attempt: 1, MaxAttempts: 1, WorkerRun: "run-demo-1"},
	{ID: "01J7X36J5K4L3M2N1P0Q9R8S7T6", Kind: KindSleep, Subject: "soon", State: watchbillstore.StateDiscarded,
		Attempt: 1, MaxAttempts: 1, WorkerRun: "run-demo-1", LastError: "watchbilldemo: subject duration: time: invalid duration \"soon\""},
}

// tourInit builds a window bound to the tour's id stack with the seeded
// list in place and the last verb's note shown.
func tourInit(ids *c.WidgetIdStack) (state any) {
	inst := newApp()
	inst.ids = ids
	inst.simulateFail = true
	inst.maxAttempts = 3
	inst.backoffIdx = 1
	inst.mu.Lock()
	inst.jobs = tourJobs
	inst.lastNote = "enqueued " + short(tourJobs[0].ID) + " as " + watchbillstore.StateQueued
	inst.mu.Unlock()
	state = inst
	return
}

// tourRenderStateful draws the seeded window. The list's read time is set
// to now on every frame, so the line reads "listed 0s ago" whenever the
// frame is captured.
func tourRenderStateful(ids *c.WidgetIdStack, state any) {
	inst, ok := state.(*App)
	if !ok || inst == nil {
		return
	}
	inst.ids = ids
	inst.mu.Lock()
	inst.refreshed = time.Now()
	inst.mu.Unlock()
	inst.render()
}
