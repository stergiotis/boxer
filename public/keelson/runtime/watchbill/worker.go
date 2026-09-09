package watchbill

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Config parameterises a [Worker].
type Config struct {
	// Store is the job table; required.
	Store StoreI
	// Handlers is the registry the worker drains; nil is [DefaultRegistry].
	Handlers *Registry
	// RunId is this process's run id, the worker's mark on every row it
	// changes; required.
	RunId string
	// Bus is where the doorbell is heard, the announcements made, and the
	// tasks reported; nil runs without any of the three.
	Bus app.BusI
	// Liveness answers the sweep (ADR-0223 §SD4); nil sweeps nothing.
	Liveness LivenessI
	// MaxWorkers is the concurrency per kind; zero is DefaultMaxWorkers.
	MaxWorkers int
	// Poll, AbandonAfter and Keep default to the environment's.
	Poll         time.Duration
	AbandonAfter time.Duration
	Keep         time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Log is the worker's logger; zero writes nowhere.
	Log zerolog.Logger
}

// DefaultMaxWorkers is the per-kind concurrency when Config leaves it zero.
const DefaultMaxWorkers = 2

// Worker drains the queue (ADR-0223 §SD5): it polls, claims, runs each
// job as a keelson task where a bus exists, writes every transition and
// its event, announces it, honours cancels within a poll, sweeps
// abandoned claims and expires finished rows.
type Worker struct {
	cfg      Config
	handlers *Registry
	now      func() time.Time
	log      zerolog.Logger

	wake   chan struct{}
	unsub  func()
	cancel context.CancelFunc
	done   chan struct{}

	// stopping is set when the loop leaves; a run that ends after it
	// records no outcome, since the row belongs to the sweep from then on.
	stopping atomic.Bool

	mu      sync.Mutex
	running map[string]*run
}

// run is one claimed job in flight.
type run struct {
	job    watchbillstore.Job
	cancel context.CancelCauseFunc
}

var (
	errCancelRequested = errors.New("cancel requested")
	errTimedOut        = errors.New("timed out")
)

// New builds a worker; Start runs it.
func New(cfg Config) (inst *Worker, err error) {
	if cfg.Store == nil {
		return nil, eh.Errorf("watchbill: worker without a store")
	}
	if cfg.RunId == "" {
		return nil, eh.Errorf("watchbill: worker without a run id")
	}
	if cfg.Handlers == nil {
		cfg.Handlers = DefaultRegistry
	}
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = DefaultMaxWorkers
	}
	if cfg.Poll <= 0 {
		cfg.Poll = Poll.Get()
	}
	if cfg.AbandonAfter <= 0 {
		cfg.AbandonAfter = AbandonAfter.Get()
	}
	if cfg.Keep <= 0 {
		cfg.Keep = Keep.Get()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	inst = &Worker{
		cfg: cfg, handlers: cfg.Handlers, now: cfg.Now, log: cfg.Log,
		wake:    make(chan struct{}, 1),
		running: make(map[string]*run, cfg.MaxWorkers*4),
	}
	return
}

// Start subscribes the doorbell and runs the loop until ctx ends or Stop
// is called.
func (inst *Worker) Start(ctx context.Context) (err error) {
	if inst.cfg.Bus != nil {
		inst.unsub, err = inst.cfg.Bus.Subscribe(SubjectWake, func(*app.Msg) { inst.Wake() })
		if err != nil {
			return eh.Errorf("subscribe wake: %w", err)
		}
	}
	ctx, inst.cancel = context.WithCancel(ctx)
	inst.done = make(chan struct{})
	go inst.loop(ctx)
	return
}

// Stop ends the loop, cancels every run in flight and waits for them to
// report; their rows are left running for the sweep, since a stopped
// worker is a dead run to every other.
func (inst *Worker) Stop() {
	if inst.cancel == nil {
		return
	}
	if inst.unsub != nil {
		inst.unsub()
	}
	inst.cancel()
	<-inst.done
}

// Wake asks for a poll now.
func (inst *Worker) Wake() {
	select {
	case inst.wake <- struct{}{}:
	default:
	}
}

func (inst *Worker) loop(ctx context.Context) {
	defer close(inst.done)
	ticker := time.NewTicker(inst.cfg.Poll)
	defer ticker.Stop()
	var wg sync.WaitGroup
	for {
		if err := inst.Tick(ctx, &wg); err != nil && ctx.Err() == nil {
			inst.log.Warn().Err(err).Msg("watchbill: poll")
		}
		select {
		case <-ctx.Done():
			inst.stopping.Store(true)
			wg.Wait()
			return
		case <-ticker.C:
		case <-inst.wake:
		}
	}
}

// Tick is one pass of the loop, exposed so a test drives it without a
// clock: honour cancels, sweep, expire, then fill the free slots. wg
// counts the runs started; nil is accepted when the caller does not wait.
func (inst *Worker) Tick(ctx context.Context, wg *sync.WaitGroup) (err error) {
	now := inst.now().UTC()
	if err = inst.honourCancels(ctx); err != nil {
		return
	}
	if err = inst.sweep(ctx, now); err != nil {
		return
	}
	if err = inst.cfg.Store.Expire(ctx, now.Add(-inst.cfg.Keep)); err != nil {
		return
	}
	return inst.fill(ctx, now, wg)
}

// honourCancels cancels the context of every held job whose row says
// cancel; the run's goroutine then writes cancelled.
func (inst *Worker) honourCancels(ctx context.Context) (err error) {
	held, err := inst.cfg.Store.Held(ctx, inst.cfg.RunId)
	if err != nil {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, j := range held {
		if j.State != watchbillstore.StateCancel {
			continue
		}
		if r, ok := inst.running[j.ID]; ok {
			r.cancel(errCancelRequested)
		}
	}
	return
}

// sweep abandons the jobs held by runs that show no life (ADR-0223 §SD4)
// and re-queues those with attempts left. Every held job not this run's
// is a candidate; liveness is asked once per run.
func (inst *Worker) sweep(ctx context.Context, now time.Time) (err error) {
	if inst.cfg.Liveness == nil {
		return
	}
	held, err := inst.cfg.Store.Held(ctx, "")
	if err != nil {
		return
	}
	alive := make(map[string]bool, 4)
	since := now.Add(-inst.cfg.AbandonAfter)
	for _, j := range held {
		if j.WorkerRun == inst.cfg.RunId {
			continue
		}
		live, seen := alive[j.WorkerRun]
		if !seen {
			live, err = inst.cfg.Liveness.Alive(ctx, j.WorkerRun, since)
			if err != nil {
				return
			}
			alive[j.WorkerRun] = live
		}
		if live {
			continue
		}
		if err = inst.abandon(ctx, j, now); err != nil {
			return
		}
	}
	return
}

// abandon writes abandoned on a job held by a dead run, then queued when
// attempts remain; a competing sweeper loses the first guard and writes
// nothing.
func (inst *Worker) abandon(ctx context.Context, j watchbillstore.Job, now time.Time) (err error) {
	job, ok, err := inst.cfg.Store.Transition(ctx, Transition{
		ID: j.ID, From: heldStates, HeldBy: j.WorkerRun, To: watchbillstore.StateAbandoned,
		Actor: inst.cfg.RunId, FinishedAt: &now,
	})
	if err != nil || !ok {
		return
	}
	if err = inst.event(ctx, now, job, watchbillstore.StateAbandoned, nil, "worker run "+j.WorkerRun+" showed no life"); err != nil {
		return
	}
	if job.Attempt >= job.MaxAttempts {
		return
	}
	after := now
	job, ok, err = inst.cfg.Store.Transition(ctx, Transition{
		ID: j.ID, From: []string{watchbillstore.StateAbandoned}, HeldBy: inst.cfg.RunId,
		To: watchbillstore.StateQueued, Actor: inst.cfg.RunId, RunAfter: &after,
	})
	if err != nil || !ok {
		return
	}
	return inst.event(ctx, now, job, watchbillstore.StateQueued, nil, "re-queued after abandonment")
}

// fill claims due jobs for the kinds with free slots and starts them.
func (inst *Worker) fill(ctx context.Context, now time.Time, wg *sync.WaitGroup) (err error) {
	kinds := inst.handlers.Kinds()
	if len(kinds) == 0 {
		return
	}
	free := inst.freeSlots(kinds)
	var want []string
	total := 0
	for _, k := range kinds {
		if free[k] > 0 {
			want = append(want, k)
			total += free[k]
		}
	}
	if total == 0 {
		return
	}
	ids, err := inst.cfg.Store.Queue(ctx, want, now, total)
	if err != nil {
		return
	}
	for _, id := range ids {
		job, won, cerr := inst.cfg.Store.Claim(ctx, id, inst.cfg.RunId, now)
		if cerr != nil {
			return cerr
		}
		if !won {
			continue
		}
		if free[job.Kind] <= 0 {
			// The queue read was over the kinds' total; the row is
			// ours now, so it runs anyway rather than being handed back.
			inst.log.Debug().Str("kind", job.Kind).Msg("watchbill: over the per-kind cap by one")
		}
		free[job.Kind]--
		inst.start(ctx, job, now, wg)
	}
	return
}

func (inst *Worker) freeSlots(kinds []string) (free map[string]int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	free = make(map[string]int, len(kinds))
	for _, k := range kinds {
		free[k] = inst.cfg.MaxWorkers
	}
	for _, r := range inst.running {
		free[r.job.Kind]--
	}
	return
}

// start records the run and launches its goroutine.
func (inst *Worker) start(ctx context.Context, job watchbillstore.Job, now time.Time, wg *sync.WaitGroup) {
	jobCtx, cancel := context.WithCancelCause(ctx)
	if job.TimeoutMs > 0 {
		var stop context.CancelFunc
		jobCtx, stop = context.WithTimeoutCause(jobCtx, time.Duration(job.TimeoutMs)*time.Millisecond, errTimedOut)
		prev := cancel
		cancel = func(cause error) { prev(cause); stop() }
	}
	inst.mu.Lock()
	inst.running[job.ID] = &run{job: job, cancel: cancel}
	inst.mu.Unlock()
	if wg != nil {
		wg.Add(1)
	}
	go func() {
		defer func() {
			cancel(nil)
			inst.mu.Lock()
			delete(inst.running, job.ID)
			inst.mu.Unlock()
			if wg != nil {
				wg.Done()
			}
		}()
		inst.execute(jobCtx, job, now)
	}()
}

// outcomeE is what a finished attempt writes.
type outcomeE uint8

const (
	outcomeSucceeded outcomeE = iota
	outcomeCancelled
	outcomeFailed
	// outcomeAbandon writes nothing: the worker is stopping and the row
	// stays running for the sweep, since a stopped worker is a dead run.
	outcomeAbandon
)

// execute runs one claimed job to its transition (ADR-0223 §SD2, §SD5).
// jobCtx is the run's, ended by a cancel request, a timeout, or the worker
// stopping.
func (inst *Worker) execute(jobCtx context.Context, job watchbillstore.Job, claimedAt time.Time) {
	// The claim is the running transition; its event is written here,
	// after the read-back said the claim was won.
	if err := inst.event(context.Background(), claimedAt, job, watchbillstore.StateRunning, nil, ""); err != nil {
		inst.log.Warn().Err(err).Str("id", job.ID).Msg("watchbill: running event")
	}
	h, ok := inst.handlers.Lookup(job.Kind)
	if !ok {
		inst.settle(job, outcomeFailed, eb.Build().Str("kind", job.Kind).Errorf("no handler registered"), "")
		return
	}
	handle, err := inst.spawnTask(jobCtx, job)
	if err != nil {
		inst.settle(job, outcomeFailed, err, "")
		return
	}
	hctx := handle.Ctx()
	runErr := inst.runHandler(hctx, h, job, handle)
	inst.finishTask(handle, runErr)

	cause := context.Cause(jobCtx)
	var outcome outcomeE
	var note string
	switch {
	case runErr == nil:
		outcome = outcomeSucceeded
	case errors.Is(cause, errCancelRequested):
		outcome = outcomeCancelled
	case jobCtx.Err() == nil && hctx.Err() != nil:
		// The task's own cancel subject ended the handle's context while
		// the job's was still live: a cancel from an observer.
		outcome = outcomeCancelled
		note = "cancelled through the task"
	case errors.Is(cause, errTimedOut):
		outcome = outcomeFailed
		runErr = eb.Build().Str("timeout", (time.Duration(job.TimeoutMs)*time.Millisecond).String()).Errorf("timed out: %w", runErr)
	case inst.stopping.Load():
		outcome = outcomeAbandon
	default:
		outcome = outcomeFailed
	}
	inst.settle(job, outcome, runErr, note)
}

// runHandler guards the handler against a panic, which is an error of
// the attempt like any other.
func (inst *Worker) runHandler(ctx context.Context, h HandlerI, job watchbillstore.Job, handle task.HandleI) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = eb.Build().Str("panic", fmt.Sprint(r)).Errorf("handler panicked")
		}
	}()
	return h.RunE(ctx, job, handle)
}

// spawnTask reports the run as a keelson task with the job's id; without
// a bus it is a handle over the job context that reports to no one.
func (inst *Worker) spawnTask(ctx context.Context, job watchbillstore.Job) (h task.HandleI, err error) {
	if inst.cfg.Bus == nil {
		return noopHandle{ctx: ctx, id: task.TaskIdT(job.ID)}, nil
	}
	logger := inst.log
	h, err = task.Spawn(ctx, inst.cfg.Bus, task.SpawnOpts{
		Id: task.TaskIdT(job.ID), Kind: job.Kind, Title: job.Kind + " " + job.Subject,
		OwnerAppId: app.AppIdT(job.OwnerAppId), OwnerRunId: inst.cfg.RunId,
		Cancellable: true, Logger: &logger,
	})
	if err != nil {
		err = eb.Build().Str("id", job.ID).Errorf("spawn task: %w", err)
	}
	return
}

func (inst *Worker) finishTask(h task.HandleI, runErr error) {
	if runErr == nil {
		_ = h.Done(nil)
		return
	}
	_ = h.Error(runErr, runErr.Error())
}

// settle writes the outcome: succeeded; cancelled; queued again under
// the policy when attempts remain and discarded otherwise. Each is one
// guarded transition by this run on the row it holds, then one event.
func (inst *Worker) settle(job watchbillstore.Job, outcome outcomeE, runErr error, note string) {
	if outcome == outcomeAbandon {
		inst.log.Debug().Str("id", job.ID).Msg("watchbill: stopping; row left running for the sweep")
		return
	}
	ctx := context.Background()
	now := inst.now().UTC()
	base := Transition{ID: job.ID, From: heldStates, HeldBy: inst.cfg.RunId, Actor: inst.cfg.RunId}
	var t Transition
	var eventState string
	switch outcome {
	case outcomeSucceeded:
		t, eventState = base, watchbillstore.StateSucceeded
		t.To, t.FinishedAt = watchbillstore.StateSucceeded, &now
	case outcomeCancelled:
		t, eventState = base, watchbillstore.StateCancelled
		t.To, t.FinishedAt = watchbillstore.StateCancelled, &now
	default:
		msg := runErr.Error()
		t = base
		t.LastError = &msg
		if job.Attempt < job.MaxAttempts {
			after := now.Add(backoffOf(job))
			t.To, t.RunAfter = watchbillstore.StateQueued, &after
			eventState = watchbillstore.StateFailed
			note = "attempt failed; due again at " + after.Format(time.RFC3339)
		} else {
			t.To, t.FinishedAt = watchbillstore.StateDiscarded, &now
			eventState = watchbillstore.StateDiscarded
			note = "attempts exhausted"
		}
	}
	after, ok, err := inst.cfg.Store.Transition(ctx, t)
	if err != nil {
		inst.log.Warn().Err(err).Str("id", job.ID).Msg("watchbill: settle")
		return
	}
	if !ok {
		// The row moved under this run — a sweep abandoned it — so the
		// outcome is not this run's to record.
		inst.log.Debug().Str("id", job.ID).Str("state", after.State).Msg("watchbill: outcome not recorded; row no longer held")
		return
	}
	if err = inst.event(ctx, now, after, eventState, runErr, note); err != nil {
		inst.log.Warn().Err(err).Str("id", job.ID).Msg("watchbill: settle event")
	}
}

// backoffOf is the wait before the next attempt (ADR-0223 §SD6), from the
// attempt that just failed.
func backoffOf(job watchbillstore.Job) (d time.Duration) {
	base := time.Duration(job.BackoffBaseMs) * time.Millisecond
	switch job.Backoff {
	case watchbillstore.BackoffLinear:
		return base * time.Duration(max(job.Attempt, 1))
	case watchbillstore.BackoffExponential:
		return base << min(job.Attempt-1, 30)
	default:
		return 0
	}
}

// event writes the transition row after the update it records, then
// announces the id. The row is flushed before the message, every time.
func (inst *Worker) event(ctx context.Context, at time.Time, job watchbillstore.Job, state string, runErr error, note string) (err error) {
	ev := watchbillstore.Event{ID: job.ID, State: state, Attempt: job.Attempt, WorkerRun: inst.cfg.RunId, Note: note}
	if runErr != nil {
		ev.Error = []byte(eh.FormatErrorWithStackS(runErr))
	}
	if err = inst.cfg.Store.WriteEvent(ctx, at, ev); err != nil {
		return
	}
	if inst.cfg.Bus != nil {
		if perr := inst.cfg.Bus.Publish(SubjectChanged, []byte(job.ID)); perr != nil {
			inst.log.Debug().Err(perr).Str("id", job.ID).Msg("watchbill: changed not announced")
		}
	}
	return
}

// noopHandle is the task handle a worker without a bus hands its
// handlers: the context is real, the reports go nowhere.
type noopHandle struct {
	ctx context.Context
	id  task.TaskIdT
}

var _ task.HandleI = noopHandle{}

func (inst noopHandle) Id() (id task.TaskIdT)            { return inst.id }
func (inst noopHandle) Ctx() (ctx context.Context)       { return inst.ctx }
func (inst noopHandle) Cancelled() (b bool)              { return inst.ctx.Err() != nil }
func (inst noopHandle) Report(task.ProgressReport)       {}
func (inst noopHandle) Note(string)                      {}
func (inst noopHandle) Done([]byte) (err error)          { return }
func (inst noopHandle) Error(error, string) (rerr error) { return }
