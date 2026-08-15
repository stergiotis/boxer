package providers

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/task/supervisor"
)

// ADR-0188 §SD4: the effect graph is a query. Nil sources answer with empty
// tables (never absent); a live bus and supervisor answer with rows
// attributed to the instance that acquired the effect.

func columnUint64(t *testing.T, rec arrow.RecordBatch, col string, row int) uint64 {
	t.Helper()
	idx := rec.Schema().FieldIndices(col)
	require.NotEmpty(t, idx, "column %q not found", col)
	return rec.Column(idx[0]).(*array.Uint64).Value(row)
}

func columnString(t *testing.T, rec arrow.RecordBatch, col string, row int) string {
	t.Helper()
	idx := rec.Schema().FieldIndices(col)
	require.NotEmpty(t, idx, "column %q not found", col)
	return rec.Column(idx[0]).(*array.String).Value(row)
}

func columnBool(t *testing.T, rec arrow.RecordBatch, col string, row int) bool {
	t.Helper()
	idx := rec.Schema().FieldIndices(col)
	require.NotEmpty(t, idx, "column %q not found", col)
	return rec.Column(idx[0]).(*array.Boolean).Value(row)
}

func TestRegisterEffects_NilSourcesGiveEmptyTables(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, RegisterEffects(r, nil, nil))
	for _, name := range []string{"subscriptions", "client_caps", "tasks"} {
		p, ok := r.Lookup(name)
		require.True(t, ok, "%s registered", name)
		assert.Equal(t, introspect.FreshnessLive, p.Freshness())
		rec, err := p.Snapshot(introspect.AllColumns())
		require.NoError(t, err)
		assert.EqualValues(t, 0, rec.NumRows(), "%s is empty, not absent", name)
		assert.Equal(t, p.Schema().NumFields(), rec.Schema().NumFields())
		rec.Release()
	}
}

func TestSubscriptionsAndClientCaps_AttributeToInstance(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	r := introspect.NewRegistry()
	require.NoError(t, RegisterEffects(r, bus, nil))

	a := bus.NewClient("test.effects", []app.SubjectFilter{{Pattern: "t.>", Direction: app.CapDirectionBoth, Reason: "manifest-shaped"}})
	a.SetInstanceKey(7)
	b := bus.NewClient("test.effects", []app.SubjectFilter{{Pattern: "t.>", Direction: app.CapDirectionBoth, Reason: "manifest-shaped"}})
	b.SetInstanceKey(8)
	_, err := a.Subscribe("t.one", func(*app.Msg) {})
	require.NoError(t, err)
	_, err = b.Subscribe("t.two", func(*app.Msg) {})
	require.NoError(t, err)
	b.AddCap(app.SubjectFilter{Pattern: "fs.handle.x.>", Direction: app.CapDirectionBoth, Reason: "granted"})

	subs, _ := r.Lookup("subscriptions")
	rec, err := subs.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	require.EqualValues(t, 2, rec.NumRows())
	assert.Equal(t, "test.effects", columnString(t, rec, "app_id", 0))
	assert.Equal(t, uint64(7), columnUint64(t, rec, "instance_key", 0))
	assert.Equal(t, "t.one", columnString(t, rec, "pattern", 0))
	assert.False(t, columnBool(t, rec, "is_inbox", 0))
	assert.Equal(t, uint64(8), columnUint64(t, rec, "instance_key", 1))
	assert.Equal(t, "t.two", columnString(t, rec, "pattern", 1))

	caps, _ := r.Lookup("client_caps")
	rec2, err := caps.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec2.Release()
	require.EqualValues(t, 3, rec2.NumRows(), "two manifest-shaped caps and one grant")
	// Sorted by app id then instance key: a's one cap, then b's two.
	assert.Equal(t, uint64(7), columnUint64(t, rec2, "instance_key", 0))
	assert.Equal(t, "t.>", columnString(t, rec2, "pattern", 0))
	assert.Equal(t, "pub+sub", columnString(t, rec2, "direction", 0))
	assert.Equal(t, uint64(8), columnUint64(t, rec2, "instance_key", 2))
	assert.Equal(t, "fs.handle.x.>", columnString(t, rec2, "pattern", 2))
	assert.Equal(t, "granted", columnString(t, rec2, "reason", 2))
	// test.effects is not a registered app, so nothing counts as declared.
	assert.False(t, columnBool(t, rec2, "declared", 0))

	// Closing a client removes its rows from both tables (ADR-0188 §SD1).
	require.NoError(t, b.Close())
	rec3, err := subs.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec3.Release()
	assert.EqualValues(t, 1, rec3.NumRows())
	rec4, err := caps.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec4.Release()
	assert.EqualValues(t, 1, rec4.NumRows())
}

func TestTasks_ShowInflightAttributedToOwner(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	sup := supervisor.New(bus.NewClient(supervisor.AppId, supervisor.Caps()), nil, zerolog.Nop(), supervisor.Opts{})
	require.NoError(t, sup.Start())
	t.Cleanup(func() { _ = sup.Stop() })
	r := introspect.NewRegistry()
	require.NoError(t, RegisterEffects(r, bus, sup))

	producer := bus.NewClient("test.owner", task.ProducerCaps())
	mc := app.NewStaticMountContext("test.owner", zerolog.Nop(), nil, producer, nil)
	mc.SetInstanceKey(21)
	h, err := task.ForApp(mc).Spawn(context.Background(), task.SpawnOpts{Kind: "effects.test", Title: "hello"})
	require.NoError(t, err)

	tasks, _ := r.Lookup("tasks")
	var rec arrow.RecordBatch
	deadline := time.Now().Add(2 * time.Second)
	for {
		rec, err = tasks.Snapshot(introspect.AllColumns())
		require.NoError(t, err)
		if rec.NumRows() == 1 || time.Now().After(deadline) {
			break
		}
		rec.Release()
		time.Sleep(10 * time.Millisecond)
	}
	defer rec.Release()
	require.EqualValues(t, 1, rec.NumRows(), "the spawned task is in flight")
	assert.Equal(t, string(h.Id()), columnString(t, rec, "task_id", 0))
	assert.Equal(t, "effects.test", columnString(t, rec, "kind", 0))
	assert.Equal(t, "hello", columnString(t, rec, "title", 0))
	assert.Equal(t, "test.owner", columnString(t, rec, "owner_app_id", 0))
	assert.Equal(t, uint64(21), columnUint64(t, rec, "owner_instance_key", 0))
	assert.Equal(t, "running", columnString(t, rec, "state", 0))

	require.NoError(t, h.Done(nil))
	deadline = time.Now().Add(2 * time.Second)
	for {
		rec2, err := tasks.Snapshot(introspect.AllColumns())
		require.NoError(t, err)
		n := rec2.NumRows()
		rec2.Release()
		if n == 0 {
			break
		}
		require.True(t, time.Now().Before(deadline), "the finished task must leave keelson('tasks')")
		time.Sleep(10 * time.Millisecond)
	}
}
