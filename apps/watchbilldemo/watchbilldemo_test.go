package watchbilldemo

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
)

// fixture is the app mounted on an in-proc bus beside a worker over the
// memory store, the way the host stands one — the worker drains the
// handler this package registered at init.
type fixture struct {
	a     *App
	store *watchbill.MemStore
	w     *watchbill.Worker
	mc    *app.StaticMountContext
}

func newFixture(t *testing.T) (f *fixture) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	f = &fixture{store: watchbill.NewMemStore()}
	var err error
	f.w, err = watchbill.New(watchbill.Config{
		Store: f.store, RunId: "run-demo", Bus: bus.NewClient(watchbill.WorkerAppId, watchbill.WorkerCaps()),
		Poll: time.Hour, Keep: time.Hour, AbandonAfter: time.Minute,
	})
	require.NoError(t, err)
	require.NoError(t, f.w.Start(context.Background()))
	t.Cleanup(f.w.Stop)

	id := app.AppIdT(manifest.Id)
	f.mc = app.NewStaticMountContext(id, zerolog.Nop(), nil, bus.NewClient(id, manifest.Caps), nil)
	f.a = newApp()
	require.NoError(t, f.a.Mount(f.mc))
	t.Cleanup(func() { _ = f.a.Unmount(f.mc) })
	return
}

func (f *fixture) eventuallyState(t *testing.T, state string) (job watchbillstore.Job) {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, j := range f.store.Jobs() {
			if j.State == state {
				job = j
				return true
			}
		}
		return false
	}, 3*time.Second, 10*time.Millisecond, "a job should reach %s", state)
	return
}

func TestManifestHoldsClientAndTaskCaps(t *testing.T) {
	patterns := make([]string, 0, len(manifest.Caps))
	for _, cp := range manifest.Caps {
		patterns = append(patterns, cp.Pattern)
	}
	assert.Contains(t, patterns, watchbill.SubjectJobAll)
	assert.Contains(t, patterns, "task.>")
	assert.Equal(t, app.KindDemo, manifest.Kind)
	h, ok := watchbill.DefaultRegistry.Lookup(KindSleep)
	require.True(t, ok, "the handler registers at init")
	assert.Equal(t, KindSleep, h.Kind())
}

// The window enqueues through the bus, the worker runs the demo handler,
// and the list the poller refreshes shows the row succeeded.
func TestEnqueueRunsToSucceeded(t *testing.T) {
	f := newFixture(t)
	f.a.durationSec = 0.2
	f.a.enqueue()
	job := f.eventuallyState(t, watchbillstore.StateSucceeded)
	assert.Equal(t, KindSleep, job.Kind)
	assert.Equal(t, "200ms", job.Subject)
	assert.Equal(t, string(manifest.Id), job.OwnerAppId)
	require.Eventually(t, func() bool {
		jobs, lastError, _, _, _ := f.a.snapshot()
		return lastError == "" && len(jobs) == 1 && jobs[0].State == watchbillstore.StateSucceeded
	}, 3*time.Second, 10*time.Millisecond, "the poller shows the finished row")
}

// A failing subject exhausts its attempts under the policy; a retry from
// the window re-queues it, and a cancel from the window stops a run.
func TestFailRetryAndCancel(t *testing.T) {
	f := newFixture(t)
	f.a.durationSec = 0.05
	f.a.simulateFail = true
	f.a.maxAttempts = 2
	f.a.enqueue()
	job := f.eventuallyState(t, watchbillstore.StateDiscarded)
	assert.EqualValues(t, 2, job.Attempt)
	assert.Contains(t, job.LastError, "asked for a failure")

	// The retry re-queues and the worker claims within its wake, so the
	// queued state is transient; the event trail is what is asserted.
	f.a.retry(job.ID)
	require.Eventually(t, func() bool {
		_, _, note, _, _ := f.a.snapshot()
		return note == "retried "+short(job.ID)
	}, 2*time.Second, 10*time.Millisecond)
	var trail []string
	require.Eventually(t, func() bool {
		trail = trail[:0]
		for _, e := range f.store.Events(job.ID) {
			trail = append(trail, e.Event.State)
		}
		return len(trail) == 9 && trail[8] == watchbillstore.StateDiscarded
	}, 3*time.Second, 10*time.Millisecond, "two attempts, the retry, two attempts again: %v", trail)
	assert.Equal(t, []string{"running", "failed", "running", "discarded", "queued", "running", "failed", "running", "discarded"}, trail)
	assert.Equal(t, "asked by "+string(manifest.Id)+": from the demo window", f.store.Events(job.ID)[4].Event.Note)

	f.a.durationSec = 30
	f.a.simulateFail = false
	f.a.maxAttempts = 1
	f.a.enqueue()
	running := f.eventuallyState(t, watchbillstore.StateRunning)
	f.a.cancel(running.ID)
	f.eventuallyState(t, watchbillstore.StateCancelled)
	require.Eventually(t, func() bool {
		jobs, _, _, _, _ := f.a.snapshot()
		return len(jobs) == 2
	}, 2*time.Second, 10*time.Millisecond)
}

// Without a bus the verbs fail into the status line and nothing panics.
func TestNoBusIsGraceful(t *testing.T) {
	a := newApp()
	mc := app.NewStaticMountContext(app.AppIdT(manifest.Id), zerolog.Nop(), nil, nil, nil)
	require.NoError(t, a.Mount(mc))
	a.enqueue()
	require.Eventually(t, func() bool {
		_, lastError, _, _, inflight := a.snapshot()
		return inflight == 0 && lastError != ""
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, a.Unmount(mc))
}

func TestParseSubject(t *testing.T) {
	d, fail, err := parseSubject("1.5s")
	require.NoError(t, err)
	assert.Equal(t, 1500*time.Millisecond, d)
	assert.False(t, fail)
	d, fail, err = parseSubject(" 2s fail ")
	require.NoError(t, err)
	assert.Equal(t, 2*time.Second, d)
	assert.True(t, fail)
	for _, bad := range []string{"", "soon", "2s later", "2s fail now", "-1s"} {
		_, _, err = parseSubject(bad)
		assert.Error(t, err, bad)
	}
	assert.Equal(t, "3s fail", Subject(3*time.Second, true))
}
