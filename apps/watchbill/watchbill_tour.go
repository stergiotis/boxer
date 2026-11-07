// Demo-registry enrollment for watchbill (ADR-0057, keelson-app A8): one
// Demo draws the window in a seeded state, without window chrome, so the
// screenshot driver and the design-system review tier see it. The seed is
// a snapshot the poller would have landed — a few jobs across the states,
// the selected job's trail and one worker row — set on the instance
// directly: no bus, no store and no poller run under the tour, so the
// picture is the same on every machine. The tests stand a memory store
// and a worker beside the window instead; that fixture produces ids and
// times of its own and belongs to the tests.

package watchbill

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "watchbill-jobs",
		Category: "Runtime",
		Title:    icons.PhClipboard + " Watchbill — jobs",
		Stage:    [2]float32{1200, 800},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindUX,
		Description: "The watchbill window over a seeded queue: the state pills and the kind battery, the job grid across " +
			"queued, running, failed and final states, the selected job's fields and trail with its state chip, and the worker line.",
		Init:           tourInit,
		RenderStateful: tourRenderStateful,
		SourceFunc:     (*App).render,
	})
}

// tourAt is the fixed instant the seeded rows are dated from.
var tourAt = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)

// tourJobs is the seeded queue: one job per state the window draws a verb
// for, plus the states a sweep and a cancel leave behind.
var tourJobs = []watchbillstore.Job{
	{ID: "01J7X2K9Q4R8T6V1W3Y5Z7A9B0", Kind: "mail.send", Subject: "digest-2026-09-15", Queue: "default", State: watchbillstore.StateRunning,
		Attempt: 1, MaxAttempts: 3, Backoff: watchbillstore.BackoffExponential, BackoffBaseMs: 10_000, TimeoutMs: 600_000,
		RunAfter: tourAt.Add(-40 * time.Second), WorkerRun: "run-8f3a1c", OwnerAppId: "github.com/example/mail", ArgsKind: "mailSend"},
	{ID: "01J7X2J1M2N3P4Q5R6S7T8U9V0", Kind: "index.walk", Subject: "vault/notes", Queue: "bulk", State: watchbillstore.StateFailed,
		Attempt: 2, MaxAttempts: 3, Backoff: watchbillstore.BackoffLinear, BackoffBaseMs: 5_000, TimeoutMs: 300_000,
		RunAfter: tourAt.Add(2 * time.Minute), WorkerRun: "run-8f3a1c", OwnerAppId: "github.com/example/index",
		LastError: "walk: open vault/notes/2026-09.md: permission denied\n\tat index.walk.visit"},
	{ID: "01J7X2H7C8D9E0F1G2H3J4K5L6", Kind: "tender.download", Subject: "dl-77", Queue: "default", State: watchbillstore.StateQueued,
		Attempt: 0, MaxAttempts: 3, Backoff: watchbillstore.BackoffExponential, BackoffBaseMs: 10_000, TimeoutMs: 600_000,
		RunAfter: tourAt.Add(5 * time.Minute), OwnerAppId: "github.com/example/tender"},
	{ID: "01J7X2G3Z4Y5X6W7V8U9T0S1R2", Kind: "mail.send", Subject: "digest-2026-09-14", Queue: "default", State: watchbillstore.StateSucceeded,
		Attempt: 1, MaxAttempts: 3, RunAfter: tourAt.Add(-24 * time.Hour), FinishedAt: tourAt.Add(-24*time.Hour + 3*time.Second),
		WorkerRun: "run-2b9d0e", OwnerAppId: "github.com/example/mail"},
	{ID: "01J7X2F9A0B1C2D3E4F5G6H7J8", Kind: "report.render", Subject: "weekly-37", Queue: "bulk", State: watchbillstore.StateDiscarded,
		Attempt: 3, MaxAttempts: 3, Backoff: watchbillstore.BackoffLinear, BackoffBaseMs: 5_000,
		RunAfter: tourAt.Add(-3 * time.Hour), FinishedAt: tourAt.Add(-2 * time.Hour), WorkerRun: "run-2b9d0e",
		OwnerAppId: "github.com/example/report", LastError: "render: template weekly: no such template"},
	{ID: "01J7X2E5Q6R7S8T9U0V1W2X3Y4", Kind: "index.walk", Subject: "vault/archive", Queue: "bulk", State: watchbillstore.StateCancelled,
		Attempt: 1, MaxAttempts: 1, RunAfter: tourAt.Add(-50 * time.Minute), FinishedAt: tourAt.Add(-49 * time.Minute),
		WorkerRun: "run-8f3a1c", OwnerAppId: "github.com/example/index"},
	{ID: "01J7X2D1K2L3M4N5P6Q7R8S9T0", Kind: "tender.download", Subject: "dl-61", Queue: "default", State: watchbillstore.StateAbandoned,
		Attempt: 1, MaxAttempts: 3, RunAfter: tourAt.Add(-6 * time.Hour), WorkerRun: "run-dead77", OwnerAppId: "github.com/example/tender"},
}

// tourTrail is the selected job's trail: two attempts of index.walk, the
// second failed under the policy.
var tourTrail = eventCols{
	JobId:     []string{tourJobs[1].ID, tourJobs[1].ID, tourJobs[1].ID, tourJobs[1].ID},
	At:        []string{"2026-09-15T09:57:00Z", "2026-09-15T09:57:12Z", "2026-09-15T09:58:00Z", "2026-09-15T09:58:09Z"},
	State:     []string{"running", "failed", "running", "failed"},
	Attempt:   []int64{1, 1, 2, 2},
	WorkerRun: []string{"run-8f3a1c", "run-8f3a1c", "run-8f3a1c", "run-8f3a1c"},
	Note:      []string{"", "retry in 5s under linear", "", "retry in 10s under linear"},
	Error:     []string{"", "walk: open vault/notes/2026-09.md: permission denied", "", tourJobs[1].LastError},
}

// tourWorkers is one live worker: this process's run.
var tourWorkers = workerCols{
	RunId: []string{"run-8f3a1c"}, Host: []string{"cell-a"}, Kinds: [][]string{{"index.walk", "mail.send", "report.render", "tender.download"}},
	Queues: [][]string{{}}, MaxWorkers: []int64{2}, StartedAt: []string{"2026-09-15T09:00:00Z"}, Alive: []bool{true}, Local: []bool{true},
	Running: [][]string{{tourJobs[0].ID}}, LastTick: []string{"2026-09-15T10:00:00Z"}, PollMs: []int64{5000},
	Serving: []bool{true}, Sweeping: []bool{true},
}

// tourInit builds a window bound to the tour's id stack, with the chip the
// Mount would have built and the seeded snapshot in place. The failed job
// is selected and mirrored, so the state chip and the trail draw on the
// first frame.
func tourInit(ids *c.WidgetIdStack) (state any) {
	inst := newApp()
	inst.ids = ids
	inst.chip = fsmview.New(ids, "job-state", inst.machine, fsmview.Options[string]{
		Title: "job state", Tethered: true, AutoAnchor: true, BadgeTone: toneOf,
		Provenance: inspector.Provenance{Subject: stateProvenance, SourceApp: string(manifest.Id)},
	})
	selected := tourJobs[1]
	inst.selectedID = selected.ID
	inst.mirror(selected)
	inst.snap = snapshot{jobs: tourJobs, events: tourTrail, eventsFor: selected.ID, workers: tourWorkers,
		lastNote: "retried " + short(selected.ID), reads: true}
	state = inst
	return
}

// tourRenderStateful draws the seeded window on the tour's id stack. The
// snapshot's read time is set to now on every frame, so the status row
// reads "listed 0s ago" whenever the frame is captured.
func tourRenderStateful(ids *c.WidgetIdStack, state any) {
	inst, ok := state.(*App)
	if !ok || inst == nil {
		return
	}
	inst.ids = ids
	inst.mu.Lock()
	inst.snap.refreshed = time.Now()
	inst.mu.Unlock()
	inst.render()
}
