package regex_explorer

// Query lanes.
//
// Each ClickHouse call the explorer makes (match, extractAll,
// replaceRegexpAll, multiMatchAllIndices) gets one [queryLane]. A lane owns
// everything about that call: whether a run is in flight, the last good
// result, the inputs that result describes, and the last error.
//
// Lanes are *level-triggered*. Render code does not fire a query when an
// input changes; it states, every frame, which inputs it wants results for
// (a [queryKey] fingerprint), and the lane converges. The distinction is
// the whole point:
//
//   - Edge-triggered dispatch — the shape this app used to have — drops the
//     edit that arrives while a query is in flight, and nothing re-fires
//     when that query lands. The displayed result then describes an input
//     the user has already moved on from, with no indication that it does.
//     The status bar would happily report "CH: match=true" for a pattern
//     that no longer matches.
//   - Level-triggered dispatch cannot strand a result that way. If the
//     wanted key still differs from the served key on the next frame, the
//     lane simply starts the query again — for the *latest* input, not the
//     one that was dropped.
//
// It also gives coalescing for free, which is why there is no debounce
// timer here. At most one run per lane is in flight, so the query rate is
// bounded by query latency rather than by keystroke rate: type twenty
// characters during one ~60 ms round-trip and the lane issues exactly one
// follow-up query, for the twentieth. ADR-0054's proposed 300 ms debounce
// was aimed at this problem; convergence solves it without a timer, and
// without the fixed latency floor a debounce would add to every edit.
//
// A lane is render-thread state. The worker goroutine touches only the
// embedded [bgjob.Runner], which has its own lock — so lane fields need no
// synchronisation, and the imzero2 contract (workers never call c.*, see
// the imzero2 skill §"Framework Data Race") is preserved.

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
)

// queryTimeout bounds a single ClickHouse round-trip. clickhouse-local
// answers these in tens of milliseconds against a warm pooled worker; a
// query still running after this long is wedged, and failing it frees the
// pool slot. A timed-out input is not retried — the same query would
// wedge the same way (see [laneError]).
const queryTimeout = 20 * time.Second

// transientRetryDelay is how long a lane holds a transient failure for an
// input before asking again. Long enough that a refused capability or an
// absent bus costs one cheap request every few seconds rather than one
// per frame; short enough that a pool that was still warming up when the
// window opened is picked up without the user having to edit anything.
const transientRetryDelay = 3 * time.Second

// laneError carries a failed run's error together with whether the
// failure is final for its input. Final means re-running the same query
// would fail the same way: ClickHouse answered with an exception (the
// pattern or the query was rejected), or the run hit queryTimeout.
// Everything else — the bus timed out, the capability was refused, no bus
// is attached yet, the pool could not hand out a worker — says nothing
// about the input, and the lane tries again after transientRetryDelay.
type laneError struct {
	err   error
	final bool
}

func (inst *laneError) Error() string { return inst.err.Error() }
func (inst *laneError) Unwrap() error { return inst.err }

// queryKey fingerprints the inputs one query depends on. Two runs with
// equal keys would produce equal results, so a lane whose served key
// equals the wanted key has nothing to do.
type queryKey string

// makeQueryKey builds a key from the inputs a query reads. Each part is
// quoted, so no combination of user input can make two different input
// tuples collide (a raw join on a separator could: pattern "a\x1fb" with
// an empty haystack would otherwise look like pattern "a" over "b").
func makeQueryKey(parts ...string) (key queryKey) {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(strconv.Quote(p))
	}
	key = queryKey(b.String())
	return
}

// laneResult pairs a query's value with the wall-clock the run took, so
// the lane can surface elapsed time without every compute callback having
// to measure itself.
type laneResult[T any] struct {
	Value   T
	Elapsed time.Duration
}

// queryLane is one ClickHouse call's async state. The zero value is ready
// to use.
type queryLane[T any] struct {
	job bgjob.Runner[laneResult[T]]

	// served is the last successful result and servedKey the inputs it
	// was computed for. Retained across a supersede so the UI keeps
	// showing the previous answer while a newer query is in flight
	// rather than flickering to empty.
	served    T
	servedKey queryKey
	hasServed bool
	elapsed   time.Duration

	// err is the last failure and errKey the inputs that produced it.
	// Pairing them is what stops a failing input from being retried in a
	// hot loop: for the same key, a final failure is never re-run and a
	// transient one only once errAt is transientRetryDelay old.
	err      error
	errKey   queryKey
	errAt    time.Time
	errFinal bool

	// pendingKey is the key of the in-flight run. bgjob returns a run's
	// Tag only on success, so the lane remembers it to attribute failures.
	// Cleared once the run's outcome is taken, so a failure that bgjob
	// keeps reporting until the next Start is recorded once, not per frame.
	pendingKey queryKey
}

// demand states which inputs the lane should hold results for and starts
// a run if it does not. Call once per frame from the render thread,
// before reading the lane.
//
// compute runs on a worker goroutine and must not touch render-thread
// state: capture what it needs by value at the call site.
func (lane *queryLane[T]) demand(want queryKey, kind string, compute func(ctx context.Context) (out T, err error)) {
	lane.drain()
	if lane.servedFor(want) || lane.blockedFor(want) || lane.job.Running() {
		return
	}
	lane.pendingKey = want
	lane.job.Start(nil, bgjob.Spec{Kind: kind, Title: kind, Tag: string(want)}, func(ctx context.Context) (out *laneResult[T], err error) {
		ctx, cancel := context.WithTimeout(ctx, queryTimeout)
		defer cancel()
		start := time.Now()
		value, computeErr := compute(ctx)
		if computeErr != nil {
			final := errors.Is(ctx.Err(), context.DeadlineExceeded) || isEngineRejection(computeErr)
			err = &laneError{err: computeErr, final: final}
			return
		}
		out = &laneResult[T]{Value: value, Elapsed: time.Since(start)}
		return
	})
}

// serve records a result the lane derived without running a query — the
// case where the inputs determine the answer outright (an all-invalid
// pattern list has no ClickHouse call to make). Keeps such answers on the
// same staleness footing as fetched ones.
func (lane *queryLane[T]) serve(key queryKey, value T) {
	lane.served = value
	lane.servedKey = key
	lane.hasServed = true
	lane.elapsed = 0
	lane.clearErr()
}

// reset drops everything the lane holds and abandons any in-flight run.
// Used when the inputs stop being dispatchable at all (the pattern was
// cleared, say): the previous answer describes inputs that no longer
// exist and must not keep being displayed.
func (lane *queryLane[T]) reset() {
	if lane.job.Running() {
		lane.job.Invalidate()
	}
	var zero T
	lane.served = zero
	lane.servedKey = ""
	lane.hasServed = false
	lane.elapsed = 0
	lane.clearErr()
	lane.pendingKey = ""
}

func (lane *queryLane[T]) clearErr() {
	lane.err = nil
	lane.errKey = ""
	lane.errAt = time.Time{}
	lane.errFinal = false
}

// drain moves a finished run's outcome onto the lane. Render-thread only.
func (lane *queryLane[T]) drain() {
	if res, tag, ok := lane.job.TakeResult(); ok && res != nil {
		lane.served = res.Value
		lane.elapsed = res.Elapsed
		lane.servedKey = queryKey(tag)
		lane.hasServed = true
		lane.clearErr()
		lane.pendingKey = ""
		return
	}
	snap := lane.job.Snapshot()
	if snap.State == bgjob.StateFailed && snap.Err != nil && lane.pendingKey != "" {
		lane.err = snap.Err
		lane.errKey = lane.pendingKey
		lane.errAt = time.Now()
		var le *laneError
		lane.errFinal = errors.As(snap.Err, &le) && le.final
		lane.pendingKey = ""
	}
}

// servedFor reports whether the lane already holds a result for key.
func (lane *queryLane[T]) servedFor(key queryKey) bool {
	return lane.hasServed && lane.servedKey == key
}

// failedFor reports whether the lane holds a failure for key — what the
// UI shows, whether or not a retry is due.
func (lane *queryLane[T]) failedFor(key queryKey) bool {
	return lane.err != nil && lane.errKey == key
}

// blockedFor reports whether a failure for key still stands in the way of
// running it again: always for a final failure (retrying an input
// ClickHouse just rejected would spin), and for a transient one until
// transientRetryDelay has passed.
func (lane *queryLane[T]) blockedFor(key queryKey) bool {
	if !lane.failedFor(key) {
		return false
	}
	return lane.errFinal || time.Since(lane.errAt) < transientRetryDelay
}

// running reports whether a run is in flight, for the spinner.
func (lane *queryLane[T]) running() bool {
	return lane.job.Running()
}

// view is the render-side snapshot of a lane: the value to draw, whether
// it describes the inputs currently on screen, and the run state around
// it. Fresh is false when the value is a retained last-good from older
// inputs, which is what the UI needs to say "…" rather than present a
// stale answer as current.
type laneView[T any] struct {
	Value   T
	Has     bool
	Fresh   bool
	Running bool
	Err     error
	Elapsed time.Duration
}

// view snapshots the lane against the inputs currently on screen.
func (lane *queryLane[T]) view(want queryKey) (v laneView[T]) {
	v.Value = lane.served
	v.Has = lane.hasServed
	v.Fresh = lane.servedFor(want)
	v.Running = lane.job.Running()
	v.Elapsed = lane.elapsed
	if lane.failedFor(want) {
		v.Err = lane.err
	}
	return
}
