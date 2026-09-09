package watchbill

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
)

// clock is a settable time source for the worker under test.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (inst *clock) now() time.Time { inst.mu.Lock(); defer inst.mu.Unlock(); return inst.t }
func (inst *clock) advance(d time.Duration) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.t = inst.t.Add(d)
}

var t0 = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

// fixture is a worker over a MemStore with a recording handler.
type fixture struct {
	store    *MemStore
	reg      *Registry
	clk      *clock
	w        *Worker
	ran      []string
	ranMu    sync.Mutex
	outcome  func(job watchbillstore.Job) error
	running  atomic.Int32
	release  chan struct{}
	blocking atomic.Bool
}

func newFixture(t *testing.T, runId string, opts ...func(*Config)) (f *fixture) {
	t.Helper()
	f = &fixture{store: NewMemStore(), reg: NewRegistry(), clk: &clock{t: t0}, release: make(chan struct{})}
	f.outcome = func(watchbillstore.Job) error { return nil }
	require.NoError(t, f.reg.Register(HandlerFunc{KindName: "test.kind", Run: func(ctx context.Context, job watchbillstore.Job, h task.HandleI) error {
		f.ranMu.Lock()
		f.ran = append(f.ran, job.ID)
		f.ranMu.Unlock()
		f.running.Add(1)
		defer f.running.Add(-1)
		if f.blocking.Load() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-f.release:
			}
		}
		return f.outcome(job)
	}}))
	cfg := Config{Store: f.store, Handlers: f.reg, RunId: runId, MaxWorkers: 2, Poll: time.Hour, AbandonAfter: 90 * time.Second, Keep: time.Hour, Now: f.clk.now}
	for _, o := range opts {
		o(&cfg)
	}
	var err error
	f.w, err = New(cfg)
	require.NoError(t, err)
	return
}

// tick drives one pass and waits for every run it started.
func (f *fixture) tick(t *testing.T) {
	t.Helper()
	var wg sync.WaitGroup
	require.NoError(t, f.w.Tick(context.Background(), &wg))
	wg.Wait()
}

func (f *fixture) enqueue(t *testing.T, req Request) (id string) {
	t.Helper()
	req.Kind = "test.kind"
	if req.RunAfter.IsZero() {
		req.RunAfter = f.clk.now()
	}
	id, err := Enqueue(context.Background(), f.store, req)
	require.NoError(t, err)
	return
}

func (f *fixture) job(t *testing.T, id string) (job watchbillstore.Job) {
	t.Helper()
	job, found, err := f.store.Get(context.Background(), id)
	require.NoError(t, err)
	require.True(t, found)
	return
}

func states(evs []MemEvent) (out []string) {
	for _, e := range evs {
		out = append(out, e.Event.State)
	}
	return
}

// The queue read orders by priority, then due time; with one slot the
// worker takes exactly the first each pass and leaves the rest queued.
func TestQueueOrderAndCap(t *testing.T) {
	f := newFixture(t, "run-a", func(c *Config) { c.MaxWorkers = 1 })
	late := f.enqueue(t, Request{Subject: "late", Priority: 1, RunAfter: t0.Add(time.Second)})
	low := f.enqueue(t, Request{Subject: "low", Priority: 9, RunAfter: t0})
	high := f.enqueue(t, Request{Subject: "high", Priority: 1, RunAfter: t0})
	future := f.enqueue(t, Request{Subject: "future", Priority: 0, RunAfter: t0.Add(time.Hour)})

	f.tick(t)
	assert.Equal(t, []string{high}, f.ran, "the lowest priority number first")
	f.tick(t)
	assert.Equal(t, []string{high, low}, f.ran)
	assert.Equal(t, watchbillstore.StateQueued, f.job(t, late).State, "not due")
	f.clk.advance(2 * time.Second)
	f.tick(t)
	assert.Equal(t, []string{high, low, late}, f.ran)
	assert.Equal(t, watchbillstore.StateQueued, f.job(t, future).State, "not due")
	for _, id := range []string{high, low, late} {
		j := f.job(t, id)
		assert.Equal(t, watchbillstore.StateSucceeded, j.State)
		assert.Equal(t, "run-a", j.WorkerRun)
		assert.EqualValues(t, 1, j.Attempt)
		assert.Equal(t, []string{watchbillstore.StateRunning, watchbillstore.StateSucceeded}, states(f.store.Events(id)))
	}
}

// With two slots both due jobs run in one pass, whichever finishes first.
func TestCapTakesSeveral(t *testing.T) {
	f := newFixture(t, "run-a")
	a := f.enqueue(t, Request{})
	b := f.enqueue(t, Request{})
	c := f.enqueue(t, Request{})
	f.tick(t)
	assert.Len(t, f.ran, 2)
	queued := 0
	for _, id := range []string{a, b, c} {
		if f.job(t, id).State == watchbillstore.StateQueued {
			queued++
		}
	}
	assert.Equal(t, 1, queued, "one of three equal jobs waits for the next pass")
}

// A failed attempt is re-queued under the policy, then discarded after the
// last; the event trail carries the failure and its chain.
func TestRetryPolicyAndDiscard(t *testing.T) {
	f := newFixture(t, "run-a")
	f.outcome = func(watchbillstore.Job) error { return errors.New("boom") }
	id := f.enqueue(t, Request{MaxAttempts: 3, Backoff: watchbillstore.BackoffExponential, BackoffBase: 10 * time.Second})

	f.tick(t)
	j := f.job(t, id)
	assert.Equal(t, watchbillstore.StateQueued, j.State)
	assert.EqualValues(t, 1, j.Attempt)
	assert.Equal(t, t0.Add(10*time.Second), j.RunAfter, "base × 2^0")
	assert.Equal(t, "boom", j.LastError)

	f.tick(t)
	assert.Len(t, f.ran, 1, "not due until the backoff passes")
	f.clk.advance(10 * time.Second)
	f.tick(t)
	j = f.job(t, id)
	assert.EqualValues(t, 2, j.Attempt)
	assert.Equal(t, t0.Add(10*time.Second).Add(20*time.Second), j.RunAfter, "base × 2^1")

	f.clk.advance(20 * time.Second)
	f.tick(t)
	j = f.job(t, id)
	assert.Equal(t, watchbillstore.StateDiscarded, j.State)
	assert.EqualValues(t, 3, j.Attempt)
	evs := f.store.Events(id)
	assert.Equal(t, []string{"running", "failed", "running", "failed", "running", "discarded"}, states(evs))
	assert.Contains(t, string(evs[1].Event.Error), "boom")
	assert.Equal(t, uint32(1), evs[1].Event.Attempt)
}

// A cancel requested on a running job is honoured within a poll: the
// handler's context ends, and the row reads cancelled by the holder.
func TestCancelWhileRunning(t *testing.T) {
	f := newFixture(t, "run-a")
	f.blocking.Store(true)
	id := f.enqueue(t, Request{})
	var wg sync.WaitGroup
	require.NoError(t, f.w.Tick(context.Background(), &wg))
	require.Eventually(t, func() bool { return f.running.Load() == 1 }, time.Second, time.Millisecond)
	assert.Equal(t, watchbillstore.StateRunning, f.job(t, id).State)

	ok, err := RequestCancel(context.Background(), f.store, id, "someone", t0)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, watchbillstore.StateCancel, f.job(t, id).State)
	assert.Equal(t, "run-a", f.job(t, id).WorkerRun, "a cancel request leaves the holder in place")

	require.NoError(t, f.w.Tick(context.Background(), nil))
	wg.Wait()
	j := f.job(t, id)
	assert.Equal(t, watchbillstore.StateCancelled, j.State)
	assert.Equal(t, []string{"running", "cancel", "cancelled"}, states(f.store.Events(id)))

	// A cancel on a queued job is immediate; on a finished one, nothing.
	queued := f.enqueue(t, Request{RunAfter: t0.Add(time.Hour)})
	ok, err = RequestCancel(context.Background(), f.store, queued, "someone", t0)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, watchbillstore.StateCancelled, f.job(t, queued).State)
	ok, err = RequestCancel(context.Background(), f.store, queued, "someone", t0)
	require.NoError(t, err)
	assert.False(t, ok)
}

// A job past its timeout fails the attempt with the timeout as the error.
func TestTimeoutFailsTheAttempt(t *testing.T) {
	f := newFixture(t, "run-a")
	f.blocking.Store(true)
	id := f.enqueue(t, Request{Timeout: 20 * time.Millisecond, MaxAttempts: 1})
	f.tick(t)
	j := f.job(t, id)
	assert.Equal(t, watchbillstore.StateDiscarded, j.State)
	assert.Contains(t, j.LastError, "timed out")
}

// Jobs held by a run with no life are abandoned and, with attempts left,
// re-queued; a live run's are left alone; this run's own are never swept.
func TestSweepAbandonsDeadRuns(t *testing.T) {
	live := MemLiveness{Live: map[string]bool{"run-live": true}}
	f := newFixture(t, "run-a", func(c *Config) { c.Liveness = live })
	dead := f.enqueue(t, Request{MaxAttempts: 2})
	deadLast := f.enqueue(t, Request{MaxAttempts: 1})
	held := f.enqueue(t, Request{})
	for _, c := range []struct{ id, run string }{{dead, "run-dead"}, {deadLast, "run-dead"}, {held, "run-live"}} {
		_, won, err := f.store.Claim(context.Background(), c.id, c.run, t0)
		require.NoError(t, err)
		require.True(t, won)
	}
	f.tick(t)
	assert.Equal(t, watchbillstore.StateRunning, f.job(t, held).State)
	assert.Equal(t, watchbillstore.StateAbandoned, f.job(t, deadLast).State)
	assert.Equal(t, []string{"abandoned"}, states(f.store.Events(deadLast)))
	// The re-queued one was taken and run by this worker in the same pass.
	j := f.job(t, dead)
	assert.Equal(t, watchbillstore.StateSucceeded, j.State)
	assert.EqualValues(t, 2, j.Attempt)
	assert.Equal(t, []string{"abandoned", "queued", "running", "succeeded"}, states(f.store.Events(dead)))
}

// Finished rows older than Keep are deleted at the sweep; their events stay.
func TestExpireFinishedRows(t *testing.T) {
	f := newFixture(t, "run-a", func(c *Config) { c.Keep = time.Minute })
	id := f.enqueue(t, Request{})
	f.tick(t)
	assert.Equal(t, watchbillstore.StateSucceeded, f.job(t, id).State)
	f.clk.advance(2 * time.Minute)
	f.tick(t)
	_, found, err := f.store.Get(context.Background(), id)
	require.NoError(t, err)
	assert.False(t, found)
	assert.Len(t, f.store.Events(id), 2)
}

// Retry puts a finished job back with its attempts reset.
func TestRetryResetsAttempts(t *testing.T) {
	f := newFixture(t, "run-a")
	f.outcome = func(watchbillstore.Job) error { return errors.New("no") }
	id := f.enqueue(t, Request{MaxAttempts: 1})
	f.tick(t)
	require.Equal(t, watchbillstore.StateDiscarded, f.job(t, id).State)
	ok, err := Retry(context.Background(), f.store, id, "someone", t0)
	require.NoError(t, err)
	require.True(t, ok)
	f.outcome = func(watchbillstore.Job) error { return nil }
	f.tick(t)
	j := f.job(t, id)
	assert.Equal(t, watchbillstore.StateSucceeded, j.State)
	assert.EqualValues(t, 1, j.Attempt)
}

// Two workers over one store: every job runs exactly once, whichever
// claimed it, and a job's outcome is recorded by the run that holds it.
func TestTwoWorkersOneStore(t *testing.T) {
	a := newFixture(t, "run-a")
	b := &fixture{store: a.store, reg: a.reg, clk: a.clk, release: make(chan struct{})}
	var err error
	b.w, err = New(Config{Store: a.store, Handlers: a.reg, RunId: "run-b", MaxWorkers: 2, Poll: time.Hour, Now: a.clk.now, Keep: time.Hour, AbandonAfter: time.Minute})
	require.NoError(t, err)
	var ids []string
	for i := 0; i < 6; i++ {
		ids = append(ids, a.enqueue(t, Request{}))
	}
	var wg sync.WaitGroup
	require.NoError(t, a.w.Tick(context.Background(), &wg))
	require.NoError(t, b.w.Tick(context.Background(), &wg))
	wg.Wait()
	require.NoError(t, a.w.Tick(context.Background(), &wg))
	wg.Wait()
	seen := map[string]int{}
	for _, id := range a.ran {
		seen[id]++
	}
	for _, id := range ids {
		assert.Equal(t, 1, seen[id], "job %s ran once", id)
		j := a.job(t, id)
		assert.Equal(t, watchbillstore.StateSucceeded, j.State)
		assert.Contains(t, []string{"run-a", "run-b"}, j.WorkerRun)
	}
}

// A worker whose registry names no kind claims nothing.
func TestNoKindsClaimsNothing(t *testing.T) {
	f := newFixture(t, "run-a")
	id := f.enqueue(t, Request{})
	f.w.handlers = NewRegistry()
	f.tick(t)
	assert.Equal(t, watchbillstore.StateQueued, f.job(t, id).State)
}

func TestRegistryRefusesDuplicates(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(HandlerFunc{KindName: "k"}))
	assert.Error(t, r.Register(HandlerFunc{KindName: "k"}))
	assert.Error(t, r.Register(HandlerFunc{}))
	assert.Equal(t, []string{"k"}, r.Kinds())
}
