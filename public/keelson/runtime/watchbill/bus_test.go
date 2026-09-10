package watchbill

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcancel"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcreated"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskdone"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskerror"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskprogress"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
)

// taskRecorder counts what an observer of task.> sees.
type taskRecorder struct {
	created, finished, errored atomic.Int32
}

var _ task.ObserverI = (*taskRecorder)(nil)

func (r *taskRecorder) OnCreated(taskcreated.TaskCreated)    { r.created.Add(1) }
func (r *taskRecorder) OnProgress(taskprogress.TaskProgress) {}
func (r *taskRecorder) OnDone(taskdone.TaskDone)             { r.finished.Add(1) }
func (r *taskRecorder) OnError(taskerror.TaskError)          { r.errored.Add(1) }
func (r *taskRecorder) OnCancel(taskcancel.TaskCancel)       {}

// The wake makes the worker poll now, every row is announced after it is
// written, the run is a task with the job's id, and the task's cancel is
// the job's cancel (ADR-0223 §SD5).
func TestBusDoorbellAnnouncementAndTaskBridge(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	workerBus := bus.NewClient("watchbill-worker", WorkerCaps())
	clientBus := bus.NewClient("watchbill-client", append(ClientCaps(), task.ObserverCaps()...))
	clientBus2 := bus.NewClient("watchbill-canceller", []app.SubjectFilter{{Pattern: task.PatternAll, Direction: app.CapDirectionBoth, Reason: "test"}})

	var changed []string
	var changedMu sync.Mutex
	unsub, err := clientBus.Subscribe(SubjectChanged, func(m *app.Msg) {
		changedMu.Lock()
		changed = append(changed, string(m.Payload))
		changedMu.Unlock()
	})
	require.NoError(t, err)
	defer unsub()
	rec := &taskRecorder{}
	unsubTasks, err := task.WatchAll(clientBus, rec)
	require.NoError(t, err)
	defer unsubTasks()

	store := NewMemStore()
	reg := NewRegistry()
	release := make(chan struct{})
	require.NoError(t, reg.Register(HandlerFunc{KindName: "bus.kind", Run: func(ctx context.Context, job watchbillstore.Job, h task.HandleI) error {
		h.Note("working")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}}))
	w, err := New(Config{Store: store, Handlers: reg, RunId: "run-bus", Bus: workerBus, Poll: time.Hour, Keep: time.Hour, AbandonAfter: time.Minute})
	require.NoError(t, err)
	require.NoError(t, w.Start(context.Background()))
	defer w.Stop()

	// Enqueue, ring: the worker claims within the wake, not the poll.
	id, err := Enqueue(context.Background(), store, Request{Kind: "bus.kind"})
	require.NoError(t, err)
	require.NoError(t, Wake(clientBus, id))
	require.Eventually(t, func() bool {
		j, _, _ := store.Get(context.Background(), id)
		return j.State == watchbillstore.StateRunning
	}, 2*time.Second, 5*time.Millisecond)
	assert.Eventually(t, func() bool { return rec.created.Load() == 1 }, time.Second, 5*time.Millisecond, "the run is a task")

	// The task's cancel is the job's cancel: the row reads cancelled and
	// the announcement follows the row.
	require.NoError(t, task.RequestCancel(clientBus2, task.TaskIdT(id), "from the monitor"))
	require.Eventually(t, func() bool {
		j, _, _ := store.Get(context.Background(), id)
		return j.State == watchbillstore.StateCancelled
	}, 2*time.Second, 5*time.Millisecond)
	assert.Eventually(t, func() bool {
		changedMu.Lock()
		defer changedMu.Unlock()
		return len(changed) == 2 && changed[0] == id && changed[1] == id
	}, time.Second, 5*time.Millisecond, "running and cancelled, each announced after its row")
	assert.Equal(t, []string{"running", "cancelled"}, states(store.Events(id)))
	assert.EqualValues(t, 1, rec.errored.Load(), "a cancelled task ends in error to its observers")
	assert.EqualValues(t, 0, rec.finished.Load())

	// A second job succeeds and ends the task well.
	id2, err := Enqueue(context.Background(), store, Request{Kind: "bus.kind"})
	require.NoError(t, err)
	close(release)
	w.Wake()
	require.Eventually(t, func() bool {
		j, _, _ := store.Get(context.Background(), id2)
		return j.State == watchbillstore.StateSucceeded
	}, 2*time.Second, 5*time.Millisecond)
	assert.Eventually(t, func() bool { return rec.finished.Load() == 1 }, time.Second, 5*time.Millisecond)
}
