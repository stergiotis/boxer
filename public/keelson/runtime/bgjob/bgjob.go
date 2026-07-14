// Package bgjob runs one fast, pure computation as a cancellable background
// job behind an imzero2 UI.
//
// The contract:
//
//   - The worker goroutine NEVER calls any imzero2 c.* function (imzero2
//     SKILL §12 "Framework Data Race"); the render thread polls Snapshot
//     each frame and sustains repaint itself while Running.
//   - When the work is microsecond-fast, Start paces it across a few staged
//     delays so the progress bar and Cancel affordance stay humanly visible
//     (SKILL §12 "Artificial Delays for Micro-Tasks"); StartReporting instead
//     lets a job whose work dominates publish its own real progress.
//   - Every run gets a token; superseded or cancelled workers drop their
//     result instead of clobbering a newer run's state.
//   - Results are handed to the render thread consume-once via TakeResult,
//     so render code always reads a stable value.
package bgjob

import (
	"context"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/task/estimator"
)

// StateE is the lifecycle state of a Runner.
type StateE uint8

const (
	// StateIdle — no run in flight and no unconsumed result.
	StateIdle StateE = iota
	// StateRunning — a worker goroutine is in flight.
	StateRunning
	// StateDone — a result is waiting to be consumed via TakeResult.
	StateDone
	// StateFailed — the last run errored; Snapshot().Err carries it.
	StateFailed
)

// Snapshot is the render thread's per-frame view of the job.
type Snapshot struct {
	State    StateE
	Fraction float32 // [0,1] progress; negative = indeterminate (jobprogress renders an animated bar)
	EtaMs    int64   // estimated ms remaining; <=0 = unknown
	Note     string  // current phase note; empty when the job has none
	Err      error   // set when State == StateFailed
}

// Reporter publishes real progress from inside a StartReporting compute
// callback: done/total in the callback's own units plus a short phase
// note. total == 0 marks an indeterminate phase (running, magnitude
// unknown) — the Snapshot then carries Fraction < 0 and no ETA. When
// total changes, the ETA estimator restarts so a new phase's rate is not
// polluted by the previous one's. Call from the compute goroutine (or
// any single goroutine); reports from superseded runs are dropped.
type Reporter func(done uint64, total uint64, note string)

// Spec describes one run: the keelson task metadata and the pacing
// stages. len(StageNotes) is the stage count; the compute callback runs
// between the second-to-last and the last stage (all but one stage before
// compute when there are fewer than three).
type Spec struct {
	// Kind and Title feed task.SpawnOpts for the background-task UI.
	Kind  string
	Title string

	// Tag travels with the result and is returned by TakeResult — use it
	// to record what the run was about (e.g. the scan target) so a result
	// can be matched against the current UI scope.
	Tag string

	// StageNotes are the progress notes, one per pacing stage.
	StageNotes []string

	// StageDelay is the artificial delay per stage.
	StageDelay time.Duration
}

// Runner runs at most one job at a time. The zero value is ready to use.
// T is the caller-specific result type.
type Runner[T any] struct {
	mu       sync.Mutex
	state    StateE
	fraction float32
	etaMs    int64
	note     string
	token    uint64
	result   *T
	tag      string
	err      error
	cancel   context.CancelFunc
}

// Start launches a run unless one is already in flight (returns false in
// that case). tasks may be nil; compute must be pure with respect to UI
// state — it receives the run's context and should honor cancellation.
func (r *Runner[T]) Start(tasks task.TaskApiI, spec Spec, compute func(ctx context.Context) (*T, error)) bool {
	token, ctx, ok := r.begin(0, int64(len(spec.StageNotes))*spec.StageDelay.Milliseconds())
	if !ok {
		return false
	}
	go r.run(ctx, tasks, token, spec, compute)
	return true
}

// StartReporting launches a run whose compute callback publishes its own
// real progress through a Reporter, for jobs whose work dominates any
// pacing stages (Spec.StageNotes/StageDelay are ignored). The Snapshot
// starts indeterminate (Fraction < 0) until the first determinate report;
// the ETA comes from a windowed throughput estimate over the reported
// units, restarted whenever the reported total changes (phase change).
// Reports are also forwarded to the keelson task handle, so the host's
// background-task UI shows the same live progress.
func (r *Runner[T]) StartReporting(tasks task.TaskApiI, spec Spec, compute func(ctx context.Context, report Reporter) (*T, error)) bool {
	token, ctx, ok := r.begin(-1, 0)
	if !ok {
		return false
	}
	go r.runReporting(ctx, tasks, token, spec, compute)
	return true
}

// begin transitions Idle/Done/Failed → Running under the mutex and hands
// out the new run's token + context. ok is false when a run is already in
// flight.
func (r *Runner[T]) begin(fraction float32, etaMs int64) (token uint64, ctx context.Context, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == StateRunning {
		return
	}
	r.token++
	token = r.token
	r.state = StateRunning
	r.fraction = fraction
	r.etaMs = etaMs
	r.note = ""
	r.err = nil
	ctx, r.cancel = context.WithCancel(context.Background())
	ok = true
	return
}

// Cancel aborts the in-flight run (if any); the worker's cancel path
// resets the state cleanly.
func (r *Runner[T]) Cancel() {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Invalidate bumps the run token (so any in-flight worker drops its
// result), cancels it, and resets to idle. Use when the job scope changes
// and a pending result would describe the wrong thing.
func (r *Runner[T]) Invalidate() {
	r.mu.Lock()
	r.token++
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.state = StateIdle
	r.result = nil
	r.err = nil
	r.mu.Unlock()
}

// Running reports whether a worker is currently in flight.
func (r *Runner[T]) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state == StateRunning
}

// Snapshot returns the render thread's per-frame view.
func (r *Runner[T]) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Snapshot{State: r.state, Fraction: r.fraction, EtaMs: r.etaMs, Note: r.note, Err: r.err}
}

// TakeResult hands a completed result to the caller exactly once and
// resets the runner to idle. ok is false while running, failed, idle, or
// after the result was already taken.
func (r *Runner[T]) TakeResult() (result *T, tag string, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == StateDone && r.result != nil {
		result, tag, ok = r.result, r.tag, true
		r.result = nil
		r.state = StateIdle
	}
	return result, tag, ok
}

// run is the worker goroutine. It touches only the mutex-guarded fields
// and the (concurrent-safe) keelson task handle — never imzero2 state.
func (r *Runner[T]) run(ctx context.Context, tasks task.TaskApiI, token uint64, spec Spec, compute func(ctx context.Context) (*T, error)) {
	var h task.HandleI
	if tasks != nil {
		h, _ = tasks.Spawn(ctx, task.SpawnOpts{
			Kind:        spec.Kind,
			Title:       spec.Title,
			Cancellable: true,
			EstimatedMs: int64(len(spec.StageNotes)) * spec.StageDelay.Milliseconds(),
		})
	}
	if h != nil {
		ctx = h.Ctx()
		defer func() { _ = h.Done(nil) }() // idempotent if Error() ran first
	}

	total := len(spec.StageNotes)
	stage := func(n int) bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(spec.StageDelay):
		}
		r.mu.Lock()
		if r.token == token {
			r.fraction = float32(n) / float32(total)
			r.etaMs = int64(total-n) * spec.StageDelay.Milliseconds()
			r.note = spec.StageNotes[n-1]
		}
		r.mu.Unlock()
		if h != nil {
			h.Report(task.ProgressReport{Current: uint64(n), Total: uint64(total), Unit: task.UnitSteps, Note: spec.StageNotes[n-1]})
		}
		return true
	}

	for n := 1; n < total; n++ {
		if !stage(n) {
			r.finishCancelled(token)
			return
		}
	}
	result, err := compute(ctx)
	if total > 0 && !stage(total) {
		r.finishCancelled(token)
		return
	}
	r.finish(token, spec, result, err, h)
}

// runReporting is the worker goroutine for StartReporting: no pacing
// stages — the compute callback owns the progress signal through the
// Reporter it receives.
func (r *Runner[T]) runReporting(ctx context.Context, tasks task.TaskApiI, token uint64, spec Spec, compute func(ctx context.Context, report Reporter) (*T, error)) {
	var h task.HandleI
	if tasks != nil {
		h, _ = tasks.Spawn(ctx, task.SpawnOpts{
			Kind:        spec.Kind,
			Title:       spec.Title,
			Cancellable: true,
		})
	}
	if h != nil {
		ctx = h.Ctx()
		defer func() { _ = h.Done(nil) }() // idempotent if Error() ran first
	}

	// Fresh estimator per phase (total change): a new phase's ETA must
	// not be computed from the previous phase's throughput.
	est := estimator.New()
	lastTotal := ^uint64(0)
	report := func(done uint64, total uint64, note string) {
		nowMs := time.Now().UnixMilli()
		r.mu.Lock()
		if r.token == token {
			if total != lastTotal {
				est = estimator.New()
				lastTotal = total
			}
			if total > 0 {
				est.Add(done, nowMs)
				r.fraction = float32(float64(done) / float64(total))
				r.etaMs = max(est.EtaMs(done, total), 0)
			} else {
				r.fraction = -1
				r.etaMs = 0
			}
			r.note = note
		}
		r.mu.Unlock()
		if h != nil {
			h.Report(task.ProgressReport{Current: done, Total: total, Unit: task.UnitItems, Note: note})
		}
	}

	result, err := compute(ctx, report)
	// Without trailing pacing stages the cancel signal must be checked
	// explicitly: a cancelled run resets to idle instead of surfacing the
	// compute's wrapped context error as a failure.
	if ctx.Err() != nil {
		r.finishCancelled(token)
		return
	}
	r.finish(token, spec, result, err, h)
}

// finish records a completed compute under the run's token: Done with the
// result, or Failed with the error (also reported to the task handle).
func (r *Runner[T]) finish(token uint64, spec Spec, result *T, err error, h task.HandleI) {
	r.mu.Lock()
	if r.token != token {
		r.mu.Unlock() // superseded by a newer run — drop this result
		return
	}
	if err != nil {
		r.state = StateFailed
		r.err = err
	} else {
		r.state = StateDone
		r.result = result
		r.tag = spec.Tag
	}
	r.cancel = nil
	r.mu.Unlock()

	if h != nil && err != nil {
		_ = h.Error(err, "background job failed")
	}
}

// finishCancelled resets to idle when a run is cancelled (token-guarded
// so it never clobbers a newer run).
func (r *Runner[T]) finishCancelled(token uint64) {
	r.mu.Lock()
	if r.token == token {
		r.state = StateIdle
		r.cancel = nil
	}
	r.mu.Unlock()
}
