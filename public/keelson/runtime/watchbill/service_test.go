package watchbill

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/watchbillreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/watchbillrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
)

// serviceFixture is a worker serving watchbill.job.* on an in-proc bus,
// with a handler that blocks until released, and a client with
// ClientCaps and nothing else.
type serviceFixture struct {
	bus     *inprocbus.Inst
	store   *MemStore
	w       *Worker
	client  *Client
	release chan struct{}
	fail    chan struct{}
}

func newServiceFixture(t *testing.T) (f *serviceFixture) {
	t.Helper()
	f = &serviceFixture{bus: inprocbus.NewInst(zerolog.Nop()), store: NewMemStore(), release: make(chan struct{}), fail: make(chan struct{})}
	reg := NewRegistry()
	require.NoError(t, reg.Register(HandlerFunc{KindName: "svc.kind", Run: func(ctx context.Context, job watchbillstore.Job, h task.HandleI) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.release:
			return nil
		case <-f.fail:
			return errors.New("handler failed on purpose")
		}
	}}))
	var err error
	f.w, err = New(Config{
		Store: f.store, Handlers: reg, RunId: "run-svc", Bus: f.bus.NewClient(WorkerAppId, WorkerCaps()),
		Poll: time.Hour, Keep: time.Hour, AbandonAfter: time.Minute,
	})
	require.NoError(t, err)
	require.NoError(t, f.w.Start(context.Background()))
	t.Cleanup(f.w.Stop)
	f.client = NewClient(f.bus.NewClient("apps/consumer", ClientCaps()))
	return
}

func (f *serviceFixture) state(t *testing.T, id string) (state string) {
	t.Helper()
	j, found, err := f.store.Get(context.Background(), id)
	require.NoError(t, err)
	require.True(t, found)
	return j.State
}

func (f *serviceFixture) eventually(t *testing.T, id string, state string) {
	t.Helper()
	require.Eventually(t, func() bool { return f.state(t, id) == state }, 2*time.Second, 5*time.Millisecond, "job %s should reach %s", id, state)
}

// An app with ClientCaps and no store handle enqueues, and the worker
// claims within the wake rather than the poll; the row it gets back is
// attributed to the sender (ADR-0234 §SD1).
func TestClientEnqueueIsAttributedAndWakesTheWorker(t *testing.T) {
	f := newServiceFixture(t)
	job, err := f.client.Enqueue(Request{Kind: "svc.kind", Subject: "row-1", Queue: "bulk", Priority: 3, MaxAttempts: 2, Backoff: watchbillstore.BackoffLinear, BackoffBase: time.Second})
	require.NoError(t, err)
	assert.NotEmpty(t, job.ID)
	assert.Equal(t, "apps/consumer", job.OwnerAppId)
	assert.Equal(t, "run-svc", job.RequesterRun)
	assert.Equal(t, "bulk", job.Queue)
	assert.EqualValues(t, 3, job.Priority)
	assert.EqualValues(t, 2, job.MaxAttempts)
	assert.Equal(t, watchbillstore.BackoffLinear, job.Backoff)
	f.eventually(t, job.ID, watchbillstore.StateRunning)

	got, found, err := f.client.Get(job.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, watchbillstore.StateRunning, got.State)
	assert.Equal(t, "run-svc", got.WorkerRun)

	close(f.release)
	f.eventually(t, job.ID, watchbillstore.StateSucceeded)
}

// A cancel of a queued job lands at once; one on a running job is marked
// and honoured within the wake the service rings; a retry of a discarded
// job re-queues it with the sender in the event note.
func TestClientCancelAndRetry(t *testing.T) {
	f := newServiceFixture(t)
	later := time.Now().Add(time.Hour)
	queued, err := f.client.Enqueue(Request{Kind: "svc.kind", RunAfter: later})
	require.NoError(t, err)
	ok, err := f.client.Cancel(queued.ID, "not needed")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, watchbillstore.StateCancelled, f.state(t, queued.ID))
	ok, err = f.client.Cancel(queued.ID, "again")
	require.NoError(t, err)
	assert.False(t, ok, "a second cancel does not apply")
	evs := f.store.Events(queued.ID)
	require.Len(t, evs, 1)
	assert.Equal(t, "asked by apps/consumer: not needed", evs[0].Event.Note)

	running, err := f.client.Enqueue(Request{Kind: "svc.kind"})
	require.NoError(t, err)
	f.eventually(t, running.ID, watchbillstore.StateRunning)
	ok, err = f.client.Cancel(running.ID, "")
	require.NoError(t, err)
	assert.True(t, ok)
	f.eventually(t, running.ID, watchbillstore.StateCancelled)

	ok, err = f.client.Retry(running.ID, "once more")
	require.NoError(t, err)
	assert.True(t, ok)
	f.eventually(t, running.ID, watchbillstore.StateRunning)
	close(f.release)
	f.eventually(t, running.ID, watchbillstore.StateSucceeded)
	assert.Equal(t, []string{"running", "cancel", "cancelled", "queued", "running", "succeeded"}, states(f.store.Events(running.ID)))

	ok, err = f.client.Retry("no-such-job", "")
	require.NoError(t, err)
	assert.False(t, ok)
	_, found, err := f.client.Get("no-such-job")
	require.NoError(t, err)
	assert.False(t, found)
}

// The list verb filters by state and kind and honours the limit.
func TestClientList(t *testing.T) {
	f := newServiceFixture(t)
	later := time.Now().Add(time.Hour)
	for i := 0; i < 3; i++ {
		_, err := f.client.Enqueue(Request{Kind: "svc.kind", RunAfter: later})
		require.NoError(t, err)
	}
	require.NoError(t, f.store.Enqueue(context.Background(), watchbillstore.Job{ID: "other", Kind: "other.kind", State: watchbillstore.StateDiscarded, Queue: "default"}))

	all, err := f.client.List(nil, nil, 0)
	require.NoError(t, err)
	assert.Len(t, all, 4)
	queued, err := f.client.List([]string{watchbillstore.StateQueued}, nil, 0)
	require.NoError(t, err)
	assert.Len(t, queued, 3)
	two, err := f.client.List(nil, []string{"svc.kind"}, 2)
	require.NoError(t, err)
	assert.Len(t, two, 2)
	other, err := f.client.List([]string{watchbillstore.StateDiscarded}, []string{"other.kind"}, 0)
	require.NoError(t, err)
	require.Len(t, other, 1)
	assert.Equal(t, "other", other[0].ID)
}

// A client without ClientCaps is refused by the bus before the worker
// hears anything; a payload whose op disagrees with its subject, or an
// unknown backoff class, is refused with a reason rather than dropped.
func TestServiceRefusals(t *testing.T) {
	f := newServiceFixture(t)
	bare := NewClient(f.bus.NewClient("apps/bare", nil))
	_, err := bare.Enqueue(Request{Kind: "svc.kind"})
	require.Error(t, err)
	assert.ErrorIs(t, err, inprocbus.ErrPermissionViolation)

	raw := f.bus.NewClient("apps/raw", ClientCaps())
	payload, err := buscodec.Encode(watchbillrequest.WatchbillRequest{Op: watchbillrequest.OpCancel, Id: "x"})
	require.NoError(t, err)
	b, err := raw.Request(SubjectJob(watchbillrequest.OpGet), payload)
	require.NoError(t, err)
	r, err := buscodec.Decode[watchbillreply.WatchbillReply](b)
	require.NoError(t, err)
	assert.False(t, r.Ok)
	assert.Contains(t, r.Reason, "disagree")

	_, err = f.client.Enqueue(Request{Kind: "svc.kind", Backoff: "fibonacci"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backoff")
	_, err = f.client.Enqueue(Request{})
	require.Error(t, err)
}

// keelson('watchbill_worker') is one row for this process's worker: what
// it drains, what it holds, whether it serves and sweeps.
func TestWorkerIntrospectionTable(t *testing.T) {
	f := newServiceFixture(t)
	job, err := f.client.Enqueue(Request{Kind: "svc.kind"})
	require.NoError(t, err)
	f.eventually(t, job.ID, watchbillstore.StateRunning)

	s := f.w.Status()
	assert.Equal(t, "run-svc", s.RunId)
	assert.Equal(t, []string{"svc.kind"}, s.Kinds)
	assert.Equal(t, []string{job.ID}, s.Running)
	assert.True(t, s.Serving)
	assert.False(t, s.Sweeping)
	assert.NotZero(t, s.Ticks)

	reg := introspect.NewRegistry()
	require.NoError(t, RegisterIntrospect(reg, f.store, f.w))
	p, ok := reg.Lookup("watchbill_worker")
	require.True(t, ok)
	rec, err := p.Snapshot(introspect.Projection{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, rec.NumRows())
	rec.Release()

	empty := introspect.NewRegistry()
	require.NoError(t, RegisterIntrospect(empty, nil, nil))
	p, ok = empty.Lookup("watchbill_worker")
	require.True(t, ok)
	rec, err = p.Snapshot(introspect.Projection{})
	require.NoError(t, err)
	assert.EqualValues(t, 0, rec.NumRows())
	rec.Release()
	close(f.release)
}

// The store's list filter, on the memory double: every empty field
// matches, and each set field narrows.
func TestMemStoreList(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()
	require.NoError(t, store.Enqueue(ctx, watchbillstore.Job{ID: "a", Kind: "k1", Queue: "q1", State: watchbillstore.StateQueued, OwnerAppId: "app.x"}))
	require.NoError(t, store.Enqueue(ctx, watchbillstore.Job{ID: "b", Kind: "k2", Queue: "q1", State: watchbillstore.StateRunning, OwnerAppId: "app.y"}))
	require.NoError(t, store.Enqueue(ctx, watchbillstore.Job{ID: "c", Kind: "k1", Queue: "q2", State: watchbillstore.StateDiscarded, OwnerAppId: "app.x"}))
	ids := func(jobs []watchbillstore.Job) (out []string) {
		for _, j := range jobs {
			out = append(out, j.ID)
		}
		return
	}
	all, err := store.List(ctx, watchbillstore.ListFilter{}, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"c", "b", "a"}, ids(all), "newest first")
	byKind, err := store.List(ctx, watchbillstore.ListFilter{Kinds: []string{"k1"}}, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"c", "a"}, ids(byKind))
	byQueueAndOwner, err := store.List(ctx, watchbillstore.ListFilter{Queues: []string{"q1"}, OwnerAppId: "app.x"}, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, ids(byQueueAndOwner))
	limited, err := store.List(ctx, watchbillstore.ListFilter{States: []string{watchbillstore.StateQueued, watchbillstore.StateRunning}}, 1)
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, ids(limited))
}

var _ app.BusI = (*inprocbus.Client)(nil)
