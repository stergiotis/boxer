package lane

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/entrypoint"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
)

// fakeChild stands for a job child: a goroutine that runs the job through
// a second Worker's RunJob against the same store, the way the real child
// does in its own process, or that exits without running when told to.
type fakeChild struct {
	done chan struct{}
	err  error
	// end stands for the signal: it ends the run's context, which is
	// what SIGTERM does to the real child's `watchbill run --job`.
	end context.CancelFunc
}

func (c *fakeChild) Done() <-chan struct{} { return c.done }
func (c *fakeChild) Err() error            { return c.err }
func (c *fakeChild) Terminate()            { c.end() }
func (c *fakeChild) Kill()                 { c.end() }
func (c *fakeChild) Describe() string      { return "fake" }

type fakeLauncher struct {
	store   *watchbill.MemStore
	reg     *watchbill.Registry
	mu      sync.Mutex
	started []string
	// crash makes the child exit without claiming.
	crash bool
	// entered records the entry frame the handler saw, by job id.
	entered map[string]entrypoint.KindE
	release chan struct{}
	block   bool
}

func newFakeLauncher(t *testing.T, store *watchbill.MemStore) (l *fakeLauncher) {
	l = &fakeLauncher{store: store, reg: watchbill.NewRegistry(), entered: map[string]entrypoint.KindE{}, release: make(chan struct{})}
	require.NoError(t, l.reg.Register(watchbill.HandlerFunc{KindName: "lane.kind", RunFunc: func(ctx context.Context, job watchbillstore.Job, h task.HandleI) error {
		l.mu.Lock()
		l.entered[job.ID] = entrypoint.Current()
		block := l.block
		l.mu.Unlock()
		if block {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-l.release:
			}
		}
		if strings.HasPrefix(job.Subject, "fail") {
			return errors.New("the handler failed on purpose")
		}
		return nil
	}}))
	return
}

func (l *fakeLauncher) Start(_ context.Context, job watchbillstore.Job) (ChildI, error) {
	l.mu.Lock()
	l.started = append(l.started, job.ID)
	crash := l.crash
	l.mu.Unlock()
	rctx, end := context.WithCancel(context.Background())
	c := &fakeChild{done: make(chan struct{}), end: end}
	go func() {
		defer close(c.done)
		defer end()
		if crash {
			c.err = errors.New("exit status 2")
			return
		}
		w, err := watchbill.New(watchbill.Config{Store: l.store, Handlers: l.reg, RunId: "run-child-" + job.ID, Poll: 5 * time.Millisecond, Keep: time.Hour, AbandonAfter: time.Minute})
		if err != nil {
			c.err = err
			return
		}
		if _, err = w.RunJob(rctx, job.ID); err != nil {
			c.err = err
		}
	}()
	return c, nil
}

func (l *fakeLauncher) startedIds() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.started...)
}

type notices struct {
	mu   sync.Mutex
	told []string
}

func (n *notices) JobEnded(_ time.Time, job watchbillstore.Job, state string, _ string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.told = append(n.told, job.ID+":"+state)
}

type fixture struct {
	store    *watchbill.MemStore
	launcher *fakeLauncher
	c        *Coordinator
	bus      *inprocbus.Inst
	notices  *notices
}

func newFixture(t *testing.T, children uint8) (f *fixture) {
	t.Helper()
	f = &fixture{store: watchbill.NewMemStore(), bus: inprocbus.NewInst(zerolog.Nop()), notices: &notices{}}
	f.launcher = newFakeLauncher(t, f.store)
	var err error
	f.c, err = New(Config{
		Store: f.store, Launcher: f.launcher, RunId: "run-host", Bus: f.bus.NewClient(AppId, Caps()),
		Notices: f.notices, MaxChildren: children, Poll: time.Hour, KillGrace: 50 * time.Millisecond,
	})
	require.NoError(t, err)
	return
}

func (f *fixture) enqueue(t *testing.T, subject string, lane bool) (id string) {
	t.Helper()
	req := watchbill.Request{Kind: "lane.kind", Subject: subject, MaxAttempts: 1}
	if lane {
		req.Queue = watchbillstore.QueueLane
		req.OnBehalfOf = &app.OnBehalfOf{Task: "task-1", Epoch: 1, Call: "c-" + subject}
	}
	id, err := watchbill.Enqueue(context.Background(), f.store, req)
	require.NoError(t, err)
	return
}

func (f *fixture) state(t *testing.T, id string) string {
	t.Helper()
	j, found, err := f.store.Get(context.Background(), id)
	require.NoError(t, err)
	require.True(t, found)
	return j.State
}

func (f *fixture) eventually(t *testing.T, id string, state string) {
	t.Helper()
	require.Eventually(t, func() bool { return f.state(t, id) == state }, 3*time.Second, 5*time.Millisecond, "job %s should reach %s", id, state)
}

// A queued lane row gets a child, which claims it by id and runs it under
// the agentic frame; a row on another queue is not the coordinator's.
func TestLaneRowsGetAChildEach(t *testing.T) {
	f := newFixture(t, 2)
	a := f.enqueue(t, "a", true)
	b := f.enqueue(t, "b", true)
	other := f.enqueue(t, "other", false)
	require.NoError(t, f.c.Tick(context.Background()))
	f.eventually(t, a, watchbillstore.StateSucceeded)
	f.eventually(t, b, watchbillstore.StateSucceeded)
	assert.Equal(t, watchbillstore.StateQueued, f.state(t, other), "not on the lane, not ours")
	assert.ElementsMatch(t, []string{a, b}, f.launcher.startedIds())
	require.Eventually(t, func() bool { return len(f.c.Running()) == 0 }, 3*time.Second, 5*time.Millisecond)
	f.launcher.mu.Lock()
	defer f.launcher.mu.Unlock()
	assert.Equal(t, entrypoint.KindAgentic, f.launcher.entered[a])
	assert.Equal(t, entrypoint.KindAgentic, f.launcher.entered[b])
}

// The pool bounds the children: with one slot, the second row waits for
// the first child to exit, and a tick never blocks on a full pool.
func TestChildrenAreBounded(t *testing.T) {
	f := newFixture(t, 1)
	f.launcher.block = true
	a := f.enqueue(t, "a", true)
	b := f.enqueue(t, "b", true)
	require.NoError(t, f.c.Tick(context.Background()))
	require.Eventually(t, func() bool { return len(f.launcher.startedIds()) == 1 }, 3*time.Second, 5*time.Millisecond)
	require.NoError(t, f.c.Tick(context.Background()))
	assert.Len(t, f.launcher.startedIds(), 1, "one slot, one child")
	f.eventually(t, f.launcher.startedIds()[0], watchbillstore.StateRunning)
	f.launcher.mu.Lock()
	f.launcher.block = false
	f.launcher.mu.Unlock()
	close(f.launcher.release)
	first := f.launcher.startedIds()[0]
	f.eventually(t, first, watchbillstore.StateSucceeded)
	require.Eventually(t, func() bool { return len(f.c.Running()) == 0 }, 3*time.Second, 5*time.Millisecond)
	require.NoError(t, f.c.Tick(context.Background()))
	f.eventually(t, a, watchbillstore.StateSucceeded)
	f.eventually(t, b, watchbillstore.StateSucceeded)
	assert.EqualValues(t, 2, f.c.Spawned())
}

// A failed child's row tells the notices and the row stays what the child
// made it; a child that exits without claiming leaves the row queued, and
// the next tick starts another child for it.
func TestFailuresAndCrashesSettleTheHostSide(t *testing.T) {
	f := newFixture(t, 2)
	failing := f.enqueue(t, "fail", true)
	require.NoError(t, f.c.Tick(context.Background()))
	f.eventually(t, failing, watchbillstore.StateDiscarded)
	require.Eventually(t, func() bool {
		f.notices.mu.Lock()
		defer f.notices.mu.Unlock()
		return len(f.notices.told) == 1
	}, 3*time.Second, 5*time.Millisecond)
	assert.Equal(t, failing+":"+watchbillstore.StateDiscarded, f.notices.told[0])

	f.launcher.mu.Lock()
	f.launcher.crash = true
	f.launcher.mu.Unlock()
	crashed := f.enqueue(t, "crash", true)
	require.NoError(t, f.c.Tick(context.Background()))
	require.Eventually(t, func() bool { return len(f.c.Running()) == 0 }, 3*time.Second, 5*time.Millisecond)
	assert.Equal(t, watchbillstore.StateQueued, f.state(t, crashed), "a child that never claimed leaves the row queued")
	f.launcher.mu.Lock()
	f.launcher.crash = false
	f.launcher.mu.Unlock()
	require.NoError(t, f.c.Tick(context.Background()))
	f.eventually(t, crashed, watchbillstore.StateSucceeded)
	assert.Len(t, f.launcher.startedIds(), 3)
}

// Cancelling the host-side task writes a cancel on the row, which the
// child honours at its poll.
func TestTaskCancelReachesTheChild(t *testing.T) {
	f := newFixture(t, 1)
	f.launcher.block = true
	id := f.enqueue(t, "long", true)
	require.NoError(t, f.c.Start(context.Background()))
	t.Cleanup(f.c.Stop)
	f.c.Wake()
	f.eventually(t, id, watchbillstore.StateRunning)
	// The task's cancel subject is the window's button.
	obs := f.bus.NewClient("apps/observer", task.CancelerCaps())
	require.NoError(t, obs.Publish(task.SubjectCancel(task.TaskIdT(id)), nil))
	f.eventually(t, id, watchbillstore.StateCancelled)
	require.Eventually(t, func() bool { return len(f.c.Running()) == 0 }, 3*time.Second, 5*time.Millisecond)
}

// Stop ends the children through the group with the grace, and a stopped
// coordinator starts nothing more.
func TestStopEndsTheChildren(t *testing.T) {
	f := newFixture(t, 2)
	f.launcher.block = true
	id := f.enqueue(t, "long", true)
	require.NoError(t, f.c.Start(context.Background()))
	f.c.Wake()
	f.eventually(t, id, watchbillstore.StateRunning)
	// Stop's SIGTERM ends the child's run; its row is left running for
	// the sweep, as a stopped worker's is.
	f.c.Stop()
	assert.Empty(t, f.c.Running())
	assert.Equal(t, watchbillstore.StateRunning, f.state(t, id))
}

// The exec launcher relays a child's lines into the host's log — a
// zerolog JSON line at its own level with its fields, any other line as
// text — cuts an over-long line, and reports the exit status.
func TestExecLauncherRelaysLinesAndExit(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	var sink strings.Builder
	var mu sync.Mutex
	lw := zerolog.ConsoleWriter{Out: lockedWriter{w: &sink, mu: &mu}, NoColor: true, PartsExclude: []string{"time"}}
	logger := zerolog.New(lw)
	l := ExecLauncher{Path: "/bin/sh", Log: logger, MaxLineBytes: 128, Args: func(job watchbillstore.Job) []string {
		return []string{"-c", `echo '{"level":"warn","message":"from the child","runId":"r-1"}'; echo plain text here; printf 'x%.0s' $(seq 1 300); echo; exit 3`}
	}}
	child, err := l.Start(context.Background(), watchbillstore.Job{ID: "j-1"})
	require.NoError(t, err)
	<-child.Done()
	require.Error(t, child.Err())
	assert.Contains(t, child.Err().Error(), "exit status 3")
	assert.Contains(t, child.Describe(), "job j-1 pid ")
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Count(sink.String(), "\n") >= 3
	}, 3*time.Second, 5*time.Millisecond)
	mu.Lock()
	out := sink.String()
	mu.Unlock()
	assert.Contains(t, out, "WRN")
	assert.Contains(t, out, "from the child")
	assert.Contains(t, out, "runId=r-1")
	assert.Contains(t, out, "job=j-1")
	assert.Contains(t, out, "plain text here")
	assert.Contains(t, out, "[line cut]")
	assert.NotContains(t, out, strings.Repeat("x", 200))
}

type lockedWriter struct {
	w  *strings.Builder
	mu *sync.Mutex
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// Caps carry what the coordinator does on the bus and nothing else.
func TestCaps(t *testing.T) {
	caps := Caps()
	var subChanged, pubChanged bool
	for _, c := range caps {
		if c.Pattern == watchbill.SubjectChanged {
			subChanged = subChanged || c.Direction == app.CapDirectionSub
			pubChanged = pubChanged || c.Direction == app.CapDirectionPub
		}
	}
	assert.True(t, subChanged)
	assert.True(t, pubChanged)
	assert.Equal(t, []string{"watchbill", "run", "--job", "j"}, DefaultChildArgs(watchbillstore.Job{ID: "j"}))
}
