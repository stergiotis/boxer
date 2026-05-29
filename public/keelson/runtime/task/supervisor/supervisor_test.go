//go:build llm_generated_opus47

package supervisor

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
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// fixture wires the runtime pieces the supervisor needs: an in-proc
// bus, an in-memory facts store, a supervisor client + Supervisor
// instance (Start already called), a producer client for spawning,
// and a requester client for list-inflight queries.
type fixture struct {
	bus       *inprocbus.Inst
	facts     *factsstore.InMemoryFactsStore
	sup       *Supervisor
	supC      *inprocbus.Client
	producer  *inprocbus.Client
	requester *inprocbus.Client
}

func newFixture(t *testing.T, opts Opts) (f *fixture) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)
	facts := factsstore.NewInMemoryFactsStore()

	supC := bus.NewClient(AppId, Caps())
	sup := New(supC, facts, zerolog.Nop(), opts)
	require.NoError(t, sup.Start())

	producer := bus.NewClient("test.producer", task.ProducerCaps())
	// Requester client doubles as a canceler — most real UI consumers
	// want both list-inflight + cancel; ServiceClients can split if
	// they want strict separation.
	consumerCaps := append([]app.SubjectFilter{}, RequesterCaps()...)
	consumerCaps = append(consumerCaps, task.CancelerCaps()...)
	requester := bus.NewClient("test.requester", consumerCaps)

	f = &fixture{
		bus:       bus,
		facts:     facts,
		sup:       sup,
		supC:      supC,
		producer:  producer,
		requester: requester,
	}
	t.Cleanup(func() {
		_ = sup.Stop()
	})
	return
}

// findLog returns the first LogRow with the given Message, or false.
func findLog(rows []factsstore.LogRow, message string) (row factsstore.LogRow, ok bool) {
	for _, r := range rows {
		if r.Message == message {
			row = r
			ok = true
			return
		}
	}
	return
}

// findField returns the first LogField with the given Name on row.
func findField(row factsstore.LogRow, name string) (f factsstore.LogField, ok bool) {
	for _, fld := range row.Fields {
		if fld.Name == name {
			f = fld
			ok = true
			return
		}
	}
	return
}

func TestSupervisor_StartStopIdempotent(t *testing.T) {
	f := newFixture(t, Opts{})
	require.Error(t, f.sup.Start(), "second Start must error")
	require.NoError(t, f.sup.Stop())
	require.NoError(t, f.sup.Stop(), "second Stop is a no-op")
}

func TestSupervisor_PersistsCreatedAndDone(t *testing.T) {
	f := newFixture(t, Opts{})
	h, err := task.Spawn(context.Background(), f.producer, task.SpawnOpts{
		Id:         "t-done",
		Kind:       "test.kind",
		Title:      "Test done",
		OwnerAppId: "test.producer",
	})
	require.NoError(t, err)
	require.NoError(t, h.Done(nil))

	logs := f.facts.Logs()
	require.GreaterOrEqual(t, len(logs), 2)
	created, ok := findLog(logs, "task.created")
	require.True(t, ok)
	done, ok := findLog(logs, "task.done")
	require.True(t, ok)
	assert.Equal(t, "info", created.Level)
	assert.Equal(t, "info", done.Level)
	assert.Equal(t, app.AppIdT("test.producer"), created.AppId)
	assert.Equal(t, app.AppIdT("test.producer"), done.AppId)
	assert.Equal(t, LogService, created.Service)

	idField, ok := findField(created, "task_id")
	require.True(t, ok)
	assert.Equal(t, "t-done", idField.Str)

	// duration_ms is non-negative on Done (clock monotonicity inside
	// time.Now's window).
	dur, ok := findField(done, "duration_ms")
	require.True(t, ok)
	assert.GreaterOrEqual(t, dur.Int, int64(0))

	assert.Empty(t, f.sup.InflightSnapshot())
}

func TestSupervisor_PersistsErrorWithChain(t *testing.T) {
	f := newFixture(t, Opts{})
	h, err := task.Spawn(context.Background(), f.producer, task.SpawnOpts{
		Id: "t-err", Kind: "test.kind", OwnerAppId: "test.producer",
	})
	require.NoError(t, err)
	require.NoError(t, h.Error(errors.New("boom"), "boom reason"))

	logs := f.facts.Logs()
	errRow, ok := findLog(logs, "task.error")
	require.True(t, ok)
	assert.Equal(t, "error", errRow.Level)
	assert.Equal(t, "boom reason", errRow.Error)

	chain, ok := findField(errRow, "error_text")
	require.True(t, ok, "error_text field must carry the FormatErrorWithStackS bytes")
	assert.Equal(t, factsstore.LogFieldKindString, chain.Kind)
	assert.Contains(t, chain.Str, "boom",
		"error_text must include the original error message")
}

func TestSupervisor_PersistsCancelAndSubsequentDone(t *testing.T) {
	f := newFixture(t, Opts{})
	h, err := task.Spawn(context.Background(), f.producer, task.SpawnOpts{
		Id: "t-cancel", Kind: "test.kind", OwnerAppId: "test.producer",
	})
	require.NoError(t, err)

	// A consumer publishes cancel; the handle's cancel-subscription
	// cancels the producer's context. The producer (the test) is
	// responsible for actually publishing the terminal — emulate that.
	require.NoError(t, task.RequestCancel(f.requester, "t-cancel", "user"))
	require.True(t, h.Cancelled())
	require.NoError(t, h.Done(nil))

	logs := f.facts.Logs()
	_, ok := findLog(logs, "task.cancel")
	require.True(t, ok, "cancel verb must be persisted")
	_, ok = findLog(logs, "task.done")
	require.True(t, ok, "subsequent done after cancel must also be persisted")
}

func TestSupervisor_InflightSnapshot(t *testing.T) {
	f := newFixture(t, Opts{})
	h, err := task.Spawn(context.Background(), f.producer, task.SpawnOpts{
		Id: "t-snap", Kind: "test.kind", Title: "Snap me", OwnerAppId: "test.producer",
	})
	require.NoError(t, err)

	h.Report(task.ProgressReport{Current: 50, Total: 100, Unit: task.UnitItems})

	snap := f.sup.InflightSnapshot()
	require.Len(t, snap, 1)
	e := snap[0]
	assert.Equal(t, "t-snap", e.Created.TaskId)
	assert.Equal(t, "Snap me", e.Created.Title)
	assert.Equal(t, InflightStateRunning, e.State)

	require.NoError(t, h.Done(nil))
	assert.Empty(t, f.sup.InflightSnapshot())
}

func TestSupervisor_ListInflightRequestReply(t *testing.T) {
	f := newFixture(t, Opts{})

	h1, err := task.Spawn(context.Background(), f.producer, task.SpawnOpts{
		Id: "t-list-1", Kind: "k1", Title: "First", OwnerAppId: "test.producer",
	})
	require.NoError(t, err)
	h2, err := task.Spawn(context.Background(), f.producer, task.SpawnOpts{
		Id: "t-list-2", Kind: "k2", Title: "Second", OwnerAppId: "test.producer",
	})
	require.NoError(t, err)

	h1.Report(task.ProgressReport{Current: 25, Total: 100, Unit: task.UnitItems})
	h2.Report(task.ProgressReport{Current: 75, Total: 100, Unit: task.UnitItems})

	raw, err := f.requester.Request(task.SubjectListInflight, nil)
	require.NoError(t, err)
	reply, err := task.UnmarshalInflightSnapshotReply(raw)
	require.NoError(t, err)
	require.Len(t, reply.Entries, 2)

	ids := map[task.TaskIdT]task.InflightSnapshotEntry{}
	for _, e := range reply.Entries {
		ids[e.Id] = e
	}
	assert.Contains(t, ids, task.TaskIdT("t-list-1"))
	assert.Contains(t, ids, task.TaskIdT("t-list-2"))
	first := ids["t-list-1"]
	assert.Equal(t, "First", first.Title)
	assert.Equal(t, "running", first.State)
	assert.Greater(t, reply.AtMs, int64(0))

	_ = h1.Done(nil)
	_ = h2.Done(nil)
}

// fakeClock returns a clock seeded at startMs that advances by stepMs
// on every call. Used to trigger the heartbeat watchdog deterministically
// without time.Sleep.
type fakeClock struct {
	mu      sync.Mutex
	current int64
	stepMs  int64
}

func newFakeClock(startMs, stepMs int64) (fc *fakeClock) {
	fc = &fakeClock{current: startMs, stepMs: stepMs}
	return
}

func (inst *fakeClock) Now() (t time.Time) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	t = time.UnixMilli(inst.current)
	inst.current += inst.stepMs
	return
}

func (inst *fakeClock) Jump(deltaMs int64) {
	inst.mu.Lock()
	inst.current += deltaMs
	inst.mu.Unlock()
}

func TestSupervisor_HeartbeatPromotesAbandoned(t *testing.T) {
	// Drive the supervisor's clock via fakeClock — large heartbeat
	// threshold avoids wall-clock racing the test.
	clk := newFakeClock(1_000_000, 0)
	f := newFixture(t, Opts{
		HeartbeatThresholdMs: 1_000,
		HeartbeatTickMs:      60_000, // disable the goroutine watchdog; we drive scanAbandoned directly
		NowFn:                clk.Now,
	})

	_, err := task.SpawnWithClock(context.Background(), f.producer, task.SpawnOpts{
		Id: "t-aban", Kind: "k", OwnerAppId: "test.producer",
	}, clk.Now)
	require.NoError(t, err)

	// Advance the clock past the threshold, then trigger the scan
	// manually.
	clk.Jump(2_000)
	f.sup.scanAbandoned()

	snap := f.sup.InflightSnapshot()
	require.Len(t, snap, 1)
	assert.Equal(t, InflightStateAbandoned, snap[0].State)

	logs := f.facts.Logs()
	_, ok := findLog(logs, "task.abandoned")
	assert.True(t, ok, "abandoned promotion must persist an audit row")
}

func TestSupervisor_HeartbeatRecoversOnProgress(t *testing.T) {
	clk := newFakeClock(2_000_000, 0)
	f := newFixture(t, Opts{
		HeartbeatThresholdMs: 1_000,
		HeartbeatTickMs:      60_000,
		NowFn:                clk.Now,
	})

	h, err := task.SpawnWithClock(context.Background(), f.producer, task.SpawnOpts{
		Id: "t-recover", Kind: "k", OwnerAppId: "test.producer",
	}, clk.Now)
	require.NoError(t, err)

	clk.Jump(2_000)
	f.sup.scanAbandoned()
	require.Equal(t, InflightStateAbandoned, f.sup.InflightSnapshot()[0].State)

	// Producer emits — should recover. The handle uses its captured
	// clk.Now for AtMs.
	h.Report(task.ProgressReport{Current: 1, Total: 10, Unit: task.UnitItems})
	snap := f.sup.InflightSnapshot()
	require.Len(t, snap, 1)
	assert.Equal(t, InflightStateRunning, snap[0].State,
		"a fresh Progress emission must recover an abandoned entry")
}

func TestSupervisor_NilFactsStoreStillObserves(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	supC := bus.NewClient(AppId, Caps())
	sup := New(supC, nil, zerolog.Nop(), Opts{})
	require.NoError(t, sup.Start())
	defer func() { _ = sup.Stop() }()

	producer := bus.NewClient("test.producer", task.ProducerCaps())
	h, err := task.Spawn(context.Background(), producer, task.SpawnOpts{
		Id: "t-nilfacts", Kind: "k",
	})
	require.NoError(t, err)
	require.NoError(t, h.Done(nil))

	// In-flight snapshot is empty even without a facts store — the
	// supervisor still tracks the lifecycle in-memory; just doesn't
	// persist.
	assert.Empty(t, sup.InflightSnapshot())
	assert.EqualValues(t, 0, sup.PersistedCount())
}

func TestSupervisor_AuditCarriesIdentityFields(t *testing.T) {
	f := newFixture(t, Opts{})
	h, err := task.Spawn(context.Background(), f.producer, task.SpawnOpts{
		Id:           "t-id",
		Kind:         "k",
		OwnerAppId:   "test.producer",
		OwnerTileKey: 42,
		OwnerRunId:   "run-abc",
	})
	require.NoError(t, err)
	require.NoError(t, h.Done(nil))

	logs := f.facts.Logs()
	created, ok := findLog(logs, "task.created")
	require.True(t, ok)

	runIdF, ok := findField(created, "run_id")
	require.True(t, ok, "run_id field must be persisted from TaskCreated.OwnerRunId")
	assert.Equal(t, "run-abc", runIdF.Str)

	instanceF, ok := findField(created, "instance_id")
	require.True(t, ok, "instance_id field must be persisted from TaskCreated.OwnerTileKey")
	assert.EqualValues(t, 42, instanceF.Uint)

	// The same identity should also land on the terminal row so a
	// reader filtering by (run_id, instance_id) catches the entire
	// lifecycle.
	done, ok := findLog(logs, "task.done")
	require.True(t, ok)
	runIdF2, ok := findField(done, "run_id")
	require.True(t, ok)
	assert.Equal(t, "run-abc", runIdF2.Str)
}

func TestSupervisor_PersistedCountTracksRows(t *testing.T) {
	f := newFixture(t, Opts{})
	h, err := task.Spawn(context.Background(), f.producer, task.SpawnOpts{
		Id: "t-count", Kind: "k", OwnerAppId: "test.producer",
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, f.sup.PersistedCount(),
		"created should bump persisted count to 1")
	require.NoError(t, h.Done(nil))
	require.EqualValues(t, 2, f.sup.PersistedCount(),
		"done should bump persisted count to 2")
}
