package taskmonitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskprogress"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// busFixture wires an in-proc bus + producer + monitor-side api so
// tests exercise the widget end-to-end against a real bus.
type busFixture struct {
	bus      *inprocbus.Inst
	producer *inprocbus.Client
	consumer *inprocbus.Client
}

func newBusFixture(t *testing.T) (f *busFixture) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)
	producer := bus.NewClient("test.producer", task.ProducerCaps())
	// Monitor consumer needs Sub on task.> + Pub on task.*.cancel (for
	// the Cancel button) + Pub on task.list.inflight (for the optional
	// seed; not exercised here but documented for parity).
	consumerCaps := append([]app.SubjectFilter{}, task.ObserverCaps()...)
	consumerCaps = append(consumerCaps, task.CancelerCaps()...)
	consumer := bus.NewClient("test.monitor", consumerCaps)
	f = &busFixture{bus: bus, producer: producer, consumer: consumer}
	return
}

func newMonitor(t *testing.T, f *busFixture, opts Opts) (m *Inst) {
	t.Helper()
	api := task.NewBusApi(task.ApiConfig{
		Bus:   f.consumer,
		AppId: "test.monitor",
	})
	ids := c.NewWidgetIdStack()
	m = New(api, ids, "tm-test", opts)
	require.NoError(t, m.Start())
	t.Cleanup(func() {
		_ = m.Stop()
	})
	return
}

// waitFor polls the predicate up to 2s; returns true if it ever
// returns true, false on timeout. Used instead of fixed sleeps so
// tests don't race the bus dispatch goroutine.
func waitFor(predicate func() bool) (ok bool) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			ok = true
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	return
}

func TestInst_CreatedThenDone_HistoryHasDone(t *testing.T) {
	f := newBusFixture(t)
	m := newMonitor(t, f, Opts{})

	h, err := task.Spawn(context.Background(), f.producer, task.SpawnOpts{
		Id: "t1", Kind: "k1", Title: "First", OwnerAppId: "test.producer",
	})
	require.NoError(t, err)

	require.True(t, waitFor(func() bool { return m.InflightCount() == 1 }),
		"created event should land in inflight")

	require.NoError(t, h.Done(nil))
	require.True(t, waitFor(func() bool {
		return m.InflightCount() == 0 && m.HistoryCount() == 1
	}), "done should drain inflight + append history")

	m.mu.Lock()
	defer m.mu.Unlock()
	require.Len(t, m.history, 1)
	assert.Equal(t, "done", m.history[0].final)
	assert.Equal(t, "First", m.history[0].created.Title)
}

func TestInst_CancelThenDone_ShowsCancelled(t *testing.T) {
	f := newBusFixture(t)
	m := newMonitor(t, f, Opts{})

	h, err := task.Spawn(context.Background(), f.producer, task.SpawnOpts{
		Id: "t-cancel", Kind: "k", OwnerAppId: "test.producer",
	})
	require.NoError(t, err)

	require.True(t, waitFor(func() bool { return m.InflightCount() == 1 }))

	// Issue cancel through the monitor's own api — same path the
	// Cancel button uses.
	require.NoError(t, task.RequestCancel(f.consumer, "t-cancel", "test"))
	require.True(t, waitFor(func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		row, ok := m.inflight.Get("t-cancel")
		return ok && row.pending
	}), "cancel should mark inflight row pending")

	require.True(t, h.Cancelled())
	require.NoError(t, h.Done(nil))

	require.True(t, waitFor(func() bool { return m.HistoryCount() == 1 }))
	m.mu.Lock()
	defer m.mu.Unlock()
	assert.Equal(t, "cancelled", m.history[0].final,
		"done after cancel-pending should display as cancelled")
}

func TestInst_ErrorPath_CarriesErrorText(t *testing.T) {
	f := newBusFixture(t)
	m := newMonitor(t, f, Opts{})

	h, err := task.Spawn(context.Background(), f.producer, task.SpawnOpts{
		Id: "t-err", Kind: "k", OwnerAppId: "test.producer",
	})
	require.NoError(t, err)
	require.True(t, waitFor(func() bool { return m.InflightCount() == 1 }))

	require.NoError(t, h.Error(errors.New("boom"), "boom reason"))
	require.True(t, waitFor(func() bool { return m.HistoryCount() == 1 }))

	m.mu.Lock()
	defer m.mu.Unlock()
	row := m.history[0]
	assert.Equal(t, "error", row.final)
	assert.Equal(t, "boom reason", row.reason)
	// Error text is the FormatErrorWithStackS rendering from the
	// handle's terminal publish — at minimum carries the .Error()
	// string; for boxer-built errors it also carries the stack.
	assert.Contains(t, row.errorText, "boom",
		"error text must include the original error message")
}

func TestInst_StartStopIdempotent(t *testing.T) {
	f := newBusFixture(t)
	m := newMonitor(t, f, Opts{})
	require.Error(t, m.Start(), "second Start must error")
	require.NoError(t, m.Stop())
	require.NoError(t, m.Stop(), "second Stop is a no-op")
}

func TestInst_HistoryCappedByMaxHistory(t *testing.T) {
	f := newBusFixture(t)
	m := newMonitor(t, f, Opts{MaxHistory: 3})

	for i := 0; i < 5; i++ {
		opts := task.SpawnOpts{
			Id: task.TaskIdT("t-cap-" + string(rune('a'+i))),
			Kind: "k", OwnerAppId: "test.producer",
		}
		h, err := task.Spawn(context.Background(), f.producer, opts)
		require.NoError(t, err)
		require.NoError(t, h.Done(nil))
	}

	require.True(t, waitFor(func() bool { return m.HistoryCount() == 3 }),
		"history should cap at MaxHistory rows even after 5 dones")
}

func TestInst_SeedFromSupervisor_PopulatesInflight(t *testing.T) {
	// Without a real supervisor wired this returns a request-timeout
	// from the bus; the widget should still subscribe and stay usable.
	f := newBusFixture(t)
	// Tight bus timeout so the test doesn't wait for the default 2s.
	f.bus.SetRequestTimeout(100 * time.Millisecond)

	api := task.NewBusApi(task.ApiConfig{
		Bus:   f.consumer,
		AppId: "test.monitor",
	})
	ids := c.NewWidgetIdStack()
	m := New(api, ids, "tm-seed", Opts{SeedFromSupervisor: true})
	require.NoError(t, m.Start(), "seed failure must not block Start")
	defer func() { _ = m.Stop() }()
	assert.Zero(t, m.InflightCount(), "no supervisor ⇒ empty seed")
}

func TestProgressFraction_BoundsAndIndeterminate(t *testing.T) {
	// Total=0 ⇒ 0; current>=total ⇒ 1; otherwise ratio.
	assert.EqualValues(t, 0, progressFraction(taskprogress.TaskProgress{Current: 50, Total: 0}))
	assert.EqualValues(t, 1, progressFraction(taskprogress.TaskProgress{Current: 100, Total: 100}))
	assert.EqualValues(t, 1, progressFraction(taskprogress.TaskProgress{Current: 200, Total: 100}))
	assert.InDelta(t, 0.5, progressFraction(taskprogress.TaskProgress{Current: 50, Total: 100}), 0.001)
}
