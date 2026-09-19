package bgjob

import (
	"context"
	"errors"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// ErrCancelled is what a [Keyed] holds for a key whose run was cancelled — by
// [Keyed.Cancel] or, for a run published as a task, from the bus. It is an
// answer, not a failure: the key stays answered with it until the key changes
// or the lane is invalidated, so a cancelled run is not started again by the
// next frame that demands it.
var ErrCancelled = errors.New("cancelled")

// Keyed runs one background computation at a time, keyed by what it is for,
// and keeps the result for as long as the key stands: a new key cancels the
// old run, a repeated key is a cache hit, a nil run is a poll. Where [Runner]
// is a job somebody starts and whose result is consumed once, a Keyed is a
// value a frame demands every frame — "the listing for this place", "the rows
// for this query" — and computes when the demand changes.
//
// It follows the package's contract: the run's goroutine never touches the UI,
// the render thread polls, and a superseded run's result is dropped instead of
// clobbering a newer key's. With [Keyed.Configure] each run is also a
// cancellable keelson task, and [Keyed.Snapshot] is what the standard job row
// draws.
//
// A value that owns something — a file, a decoder, a connection — sets a
// disposer, and the Keyed is then its only owner: it releases the value it
// replaces, the value a superseded run produced anyway, and whatever it holds
// when it is invalidated or closed. A caller that kept its own pointer to such
// a value would be holding one the Keyed may close, so it does not.
//
// The zero value is ready to use. Not to be copied after first use.
type Keyed[T any] struct {
	mu      sync.Mutex
	tasks   task.TaskApiI
	kind    string
	title   string
	dispose func(T)

	key     string
	gen     uint64
	running bool
	done    bool
	val     T
	err     error
	cancel  context.CancelFunc

	fraction float32
	etaMs    int64
	rate     float64
	note     string
}

// Configure publishes every later run as a cancellable keelson task of the
// given kind and title through tasks (ADR-0038), so the host's task monitor
// lists it and can cancel it. nil tasks keeps the runs local. Call it before
// the first Demand, typically at mount.
func (k *Keyed[T]) Configure(tasks task.TaskApiI, kind string, title string) {
	k.mu.Lock()
	k.tasks, k.kind, k.title = tasks, kind, title
	k.mu.Unlock()
}

// SetDispose installs the release for a value that owns something. Call it
// before the first Demand.
func (k *Keyed[T]) SetDispose(dispose func(T)) {
	k.mu.Lock()
	k.dispose = dispose
	k.mu.Unlock()
}

// Demand returns the state for key, starting run when key is new. busy is
// true while the run is in flight; done and err are meaningful once it is
// not. A nil run is a question, not an order: it reports the state and
// starts nothing. The run shows as indeterminate; see [Keyed.DemandReporting]
// for one that knows its own progress.
func (k *Keyed[T]) Demand(key string, run func(ctx context.Context) (T, error)) (val T, done bool, err error, busy bool) {
	var reporting func(ctx context.Context, report Reporter) (T, error)
	if run != nil {
		reporting = func(ctx context.Context, _ Reporter) (T, error) { return run(ctx) }
	}
	return k.DemandReporting(key, reporting)
}

// DemandReporting is [Keyed.Demand] for a run that publishes its own progress
// through a [Reporter]: the Snapshot then carries its fraction, ETA and rate,
// and the task, when there is one, the same.
func (k *Keyed[T]) DemandReporting(key string, run func(ctx context.Context, report Reporter) (T, error)) (val T, done bool, err error, busy bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if key != k.key || (!k.done && !k.running) {
		if run == nil {
			var zero T
			return zero, false, nil, false
		}
		k.start(key, run)
	}
	return k.val, k.done, k.err, k.running
}

// Snapshot is the render thread's view of the current key's run, in the shape
// the standard job row draws. A cancelled key reads StateFailed with
// [ErrCancelled].
func (k *Keyed[T]) Snapshot() (snap Snapshot) {
	k.mu.Lock()
	defer k.mu.Unlock()
	snap = Snapshot{Fraction: k.fraction, EtaMs: k.etaMs, Rate: k.rate, Note: k.note}
	switch {
	case k.running:
		snap.State = StateRunning
	case k.done && k.err != nil:
		snap.State, snap.Err = StateFailed, k.err
	case k.done:
		snap.State = StateDone
	}
	return
}

// Cancel ends the run in flight and leaves the key answered with
// [ErrCancelled]. Nothing happens when no run is in flight.
func (k *Keyed[T]) Cancel() {
	k.mu.Lock()
	cancel := k.cancel
	k.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Invalidate forgets the current key, so the next Demand runs it again, and
// releases what was held.
func (k *Keyed[T]) Invalidate() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.stopLocked()
}

// Close cancels whatever is in flight and releases what is held. The Keyed is
// usable afterwards; Close is for the owner's unmount.
func (k *Keyed[T]) Close() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.stopLocked()
}

// stopLocked supersedes any run in flight — the generation moves, so its
// result is dropped and released — and clears the key.
func (k *Keyed[T]) stopLocked() {
	if k.cancel != nil {
		k.cancel()
		k.cancel = nil
	}
	k.gen++
	k.key, k.running = "", false
	k.clearLocked()
}

// clearLocked drops the held value, releasing it first when it owns
// something. The release runs under the lock and on whichever goroutine
// reached here — the frame's, for a key that changed — so a disposer that
// blocks costs a frame; it is meant to close a file or a decoder, not a
// network.
func (k *Keyed[T]) clearLocked() {
	if k.done && k.dispose != nil {
		k.dispose(k.val)
	}
	k.done = false
	var zero T
	k.val, k.err = zero, nil
	k.fraction, k.etaMs, k.rate, k.note = -1, 0, 0, ""
}

func (k *Keyed[T]) start(key string, run func(ctx context.Context, report Reporter) (T, error)) {
	if k.cancel != nil {
		k.cancel()
	}
	k.gen++
	gen := k.gen
	ctx, cancel := context.WithCancel(context.Background())
	k.cancel = cancel
	k.clearLocked()
	k.key, k.running = key, true
	tasks, spec := k.tasks, Spec{Kind: k.kind, Title: k.title}
	go func() {
		ctx, h := spawnTask(ctx, tasks, spec)
		report := newReporter(h, func(fraction float32, etaMs int64, rate float64, note string) {
			k.mu.Lock()
			if k.gen == gen {
				k.fraction, k.etaMs, k.rate, k.note = fraction, etaMs, rate, note
			}
			k.mu.Unlock()
		})
		v, err := run(ctx, report)

		k.mu.Lock()
		if k.gen != gen {
			// Superseded, and nobody will ever be handed this: a value that
			// owns something has to be released here or it leaks. Released
			// whatever the run returned — a value that came back beside an
			// error still holds what it opened.
			if k.dispose != nil {
				k.dispose(v)
			}
			k.mu.Unlock()
			if h != nil {
				_ = h.Done(nil)
			}
			return
		}
		// Still the current run and its context is over: that is a cancel —
		// Cancel here, or the task's from the bus. Supersession and
		// invalidation move the generation, so they never reach this.
		if ctx.Err() != nil {
			err = ErrCancelled
		}
		k.val, k.err, k.done, k.running = v, err, true, false
		k.cancel = nil
		k.mu.Unlock()
		cancel()
		if h != nil {
			if err != nil && !errors.Is(err, ErrCancelled) {
				_ = h.Error(err, "background job failed")
			}
			_ = h.Done(nil) // idempotent if Error ran first
		}
	}()
}
