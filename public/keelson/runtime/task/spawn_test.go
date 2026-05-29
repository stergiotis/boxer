//go:build llm_generated_opus47

package task

import (
	"context"
	"errors"
	"sync"
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
)

// busFixture is one inprocbus.Inst + a producer client + an observer
// client, each fully-capped so the tests focus on protocol behaviour
// rather than permission enforcement.
type busFixture struct {
	inst     *inprocbus.Inst
	producer *inprocbus.Client
	observer *inprocbus.Client
}

func newBusFixture(t *testing.T) (f *busFixture) {
	t.Helper()
	f = &busFixture{
		inst: inprocbus.NewInst(zerolog.Nop()),
	}
	f.producer = f.inst.NewClient("test.producer", ProducerCaps())
	f.observer = f.inst.NewClient("test.observer", append(ObserverCaps(), CancelerCaps()...))
	return
}

// recordingObserver captures every event into in-memory slices so tests
// can assert on the order and content of bus traffic without polling.
type recordingObserver struct {
	mu       sync.Mutex
	created  []taskcreated.TaskCreated
	progress []taskprogress.TaskProgress
	done     []taskdone.TaskDone
	errs     []taskerror.TaskError
	cancel   []taskcancel.TaskCancel
}

func (inst *recordingObserver) OnCreated(c taskcreated.TaskCreated) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.created = append(inst.created, c)
}

func (inst *recordingObserver) OnProgress(p taskprogress.TaskProgress) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.progress = append(inst.progress, p)
}

func (inst *recordingObserver) OnDone(d taskdone.TaskDone) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.done = append(inst.done, d)
}

func (inst *recordingObserver) OnError(e taskerror.TaskError) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.errs = append(inst.errs, e)
}

func (inst *recordingObserver) OnCancel(c taskcancel.TaskCancel) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.cancel = append(inst.cancel, c)
}

func (inst *recordingObserver) snapshot() (created []taskcreated.TaskCreated, progress []taskprogress.TaskProgress, done []taskdone.TaskDone, errs []taskerror.TaskError, cancel []taskcancel.TaskCancel) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	created = append(created, inst.created...)
	progress = append(progress, inst.progress...)
	done = append(done, inst.done...)
	errs = append(errs, inst.errs...)
	cancel = append(cancel, inst.cancel...)
	return
}

// fixedClock returns a clock that ticks deterministically — initial
// timestamp now, +stepMs on each call. Tests use this to drive the
// estimator's window without depending on wall time.
func fixedClock(startMs, stepMs int64) (nowFn func() time.Time) {
	cur := startMs
	nowFn = func() time.Time {
		t := time.UnixMilli(cur)
		cur += stepMs
		return t
	}
	return
}

func TestSpawn_PublishesCreated(t *testing.T) {
	f := newBusFixture(t)
	obs := &recordingObserver{}
	unsubscribe, err := WatchAll(f.observer, obs)
	require.NoError(t, err)
	defer unsubscribe()

	h, err := Spawn(context.Background(), f.producer, SpawnOpts{
		Kind:        "test.kind",
		Title:       "Test Task",
		OwnerAppId:  "test.producer",
		Cancellable: true,
		EstimatedMs: 5_000,
	})
	require.NoError(t, err)
	require.NotEmpty(t, h.Id())

	created, _, _, _, _ := obs.snapshot()
	require.Len(t, created, 1)
	assert.Equal(t, string(h.Id()), created[0].TaskId)
	assert.Equal(t, "test.kind", created[0].Kind)
	assert.Equal(t, "Test Task", created[0].Title)
	assert.True(t, created[0].CancellableB)
	assert.EqualValues(t, 5_000, created[0].EstimatedMs)
}

func TestSpawn_AutoGeneratesIdWhenEmpty(t *testing.T) {
	f := newBusFixture(t)
	h, err := Spawn(context.Background(), f.producer, SpawnOpts{Kind: "k"})
	require.NoError(t, err)
	assert.Len(t, string(h.Id()), nanoidLen)
}

func TestSpawn_HonoursExplicitId(t *testing.T) {
	f := newBusFixture(t)
	h, err := Spawn(context.Background(), f.producer, SpawnOpts{Id: "fixed", Kind: "k"})
	require.NoError(t, err)
	assert.Equal(t, TaskIdT("fixed"), h.Id())
}

func TestSpawn_RejectsEmptyKind(t *testing.T) {
	f := newBusFixture(t)
	_, err := Spawn(context.Background(), f.producer, SpawnOpts{})
	assert.Error(t, err)
}

func TestHandle_ReportEmitsOnHumanizedChange(t *testing.T) {
	f := newBusFixture(t)
	obs := &recordingObserver{}
	unsubscribe, err := WatchAll(f.observer, obs)
	require.NoError(t, err)
	defer unsubscribe()

	clock := fixedClock(0, 100)
	h, err := SpawnWithClock(context.Background(), f.producer, SpawnOpts{
		Id:   "t1",
		Kind: "k",
	}, clock)
	require.NoError(t, err)

	// Same humanized form (47%) reported repeatedly → only one emission
	// (plus the implicit "starting" → 47% transition).
	for i := 0; i < 5; i++ {
		h.Report(ProgressReport{Current: 470 + uint64(i), Total: 1_000, Unit: UnitItems})
	}

	_, progress, _, _, _ := obs.snapshot()
	assert.LessOrEqual(t, len(progress), 2, "near-identical reports should coalesce")

	// New humanized form (48%) should add a new emission.
	h.Report(ProgressReport{Current: 480, Total: 1_000, Unit: UnitItems})
	_, progress, _, _, _ = obs.snapshot()
	assert.GreaterOrEqual(t, len(progress), 2)
}

func TestHandle_DoneFlushesPendingReport(t *testing.T) {
	f := newBusFixture(t)
	obs := &recordingObserver{}
	unsubscribe, err := WatchAll(f.observer, obs)
	require.NoError(t, err)
	defer unsubscribe()

	clock := fixedClock(0, 100)
	h, err := SpawnWithClock(context.Background(), f.producer, SpawnOpts{Id: "t2", Kind: "k"}, clock)
	require.NoError(t, err)

	// Two reports at the same humanized form. The second is held back
	// by the emission gate; Done() must flush it before publishing
	// terminal.
	h.Report(ProgressReport{Current: 100, Total: 1_000, Unit: UnitItems})
	h.Report(ProgressReport{Current: 105, Total: 1_000, Unit: UnitItems})
	require.NoError(t, h.Done(nil))

	_, progress, done, _, _ := obs.snapshot()
	require.Len(t, done, 1)
	require.GreaterOrEqual(t, len(progress), 1)
	last := progress[len(progress)-1]
	assert.EqualValues(t, 105, last.Current, "last progress emission should reflect the held sample")
}

func TestHandle_BusCancelCancelsCtx(t *testing.T) {
	f := newBusFixture(t)
	h, err := Spawn(context.Background(), f.producer, SpawnOpts{Id: "t3", Kind: "k"})
	require.NoError(t, err)
	require.False(t, h.Cancelled())

	require.NoError(t, RequestCancel(f.observer, "t3", "test"))
	assert.True(t, h.Cancelled(), "bus cancel should cancel handle ctx")
	assert.Error(t, h.Ctx().Err())
}

func TestHandle_ParentCancelCancelsCtx(t *testing.T) {
	f := newBusFixture(t)
	parent, cancel := context.WithCancel(context.Background())
	h, err := Spawn(parent, f.producer, SpawnOpts{Id: "t4", Kind: "k"})
	require.NoError(t, err)
	require.False(t, h.Cancelled())

	cancel()
	// The parent-cancel goroutine inside Spawn is async; the handle's
	// ctx is derived from parent so Err() resolves immediately.
	assert.Error(t, h.Ctx().Err())
}

func TestHandle_DoneTerminatesAndIsIdempotent(t *testing.T) {
	f := newBusFixture(t)
	obs := &recordingObserver{}
	unsubscribe, err := WatchAll(f.observer, obs)
	require.NoError(t, err)
	defer unsubscribe()

	h, err := Spawn(context.Background(), f.producer, SpawnOpts{Id: "t5", Kind: "k"})
	require.NoError(t, err)

	require.NoError(t, h.Done(nil))
	require.NoError(t, h.Done(nil)) // idempotent
	require.NoError(t, h.Error(errors.New("late"), "late"))

	_, _, done, errs, _ := obs.snapshot()
	assert.Len(t, done, 1, "Done should publish exactly once even after re-call")
	assert.Empty(t, errs, "Error after Done is a no-op")
}

func TestHandle_ErrorEncodesChain(t *testing.T) {
	f := newBusFixture(t)
	obs := &recordingObserver{}
	unsubscribe, err := WatchAll(f.observer, obs)
	require.NoError(t, err)
	defer unsubscribe()

	h, err := Spawn(context.Background(), f.producer, SpawnOpts{Id: "t6", Kind: "k"})
	require.NoError(t, err)

	require.NoError(t, h.Error(errors.New("boom"), "boom"))

	_, _, _, errs, _ := obs.snapshot()
	require.Len(t, errs, 1)
	assert.Equal(t, "boom", errs[0].Reason)
	assert.NotEmpty(t, errs[0].ErrorText, "ErrorText payload should carry the FormatErrorWithStackS rendering")
}

func TestRequestCancel_RejectsEmptyId(t *testing.T) {
	f := newBusFixture(t)
	err := RequestCancel(f.observer, "", "no id")
	assert.Error(t, err)
}

func TestSpawn_NilBus(t *testing.T) {
	_, err := Spawn(context.Background(), nil, SpawnOpts{Kind: "k"})
	assert.Error(t, err)
}

// Verify that the producer client's BusI assertion is sane — defends
// against an accidental rename of inprocbus.Client breaking the cap
// helpers' contract.
var _ app.BusI = (*inprocbus.Client)(nil)
