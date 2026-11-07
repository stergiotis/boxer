package taskdemo

import (
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

func query[In any, Out any](t *testing.T, inst *App, name string, in In) (out Out, err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	raw, err := inst.Operations().Snapshot().Query(name, args)
	if err != nil {
		return
	}
	out, decErr := buscodec.Decode[Out](raw)
	require.NoError(t, decErr)
	return
}

func command[In any](t *testing.T, inst *App, name string, in In) (err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	_, err = inst.Operations().ApplyCommand(app.OperationCall{}, name, args)
	return
}

func refusal(err error) (r *app.OperationRefusal) {
	errors.As(err, &r)
	return
}

func defaultControls() (c Controls) {
	return Controls{Unit: "items", DurationSec: 5.0}
}

func TestOperationsCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	for _, name := range []string{opGetView, opListTasks, opSetControl, opSpawn, opCancelTask} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
	}
	spawn, _ := m.Operations.Lookup(opSpawn)
	assert.True(t, spawn.Async, "spawn settles when the task has started")
	assert.Equal(t, app.OperationEffectRun, spawn.Effect)
	cancel, _ := m.Operations.Lookup(opCancelTask)
	assert.Equal(t, app.OperationEffectConsequential, cancel.Effect, "a cancel is published on the bus")
}

func TestGetViewReadsTheControlsAndTheHost(t *testing.T) {
	a, _, cleanup := setupApp(t)
	defer cleanup()
	v, err := query[struct{}, View](t, a, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, View{Controls: defaultControls(), Tasks: true}, v)
	assert.Equal(t, defaultControls(), a.Operations().ResourceValue(resControls))

	none := newApp()
	require.NoError(t, none.Mount(app.NewStaticMountContext(app.AppIdT(manifest.Id), zerolog.Nop(), nil, nil, nil)))
	none.tasks = nil
	v, err = query[struct{}, View](t, none, opGetView, struct{}{})
	require.NoError(t, err)
	assert.False(t, v.Tasks)
	ok, reason := none.Operations().Snapshot().Available(opSpawn)
	assert.False(t, ok)
	assert.Equal(t, noTaskApiReason, reason)
	assert.Equal(t, noTaskApiReason, refusal(command(t, none, opSpawn, SpawnArgs{})).Reason)
}

func TestSetControlSetsAndNeverToggles(t *testing.T) {
	a, _, cleanup := setupApp(t)
	defer cleanup()
	for range 2 {
		require.NoError(t, command(t, a, opSetControl, SetControlArgs{Control: controlSimulateError, Value: "true"}))
		assert.True(t, a.simulateError)
	}
	require.NoError(t, command(t, a, opSetControl, SetControlArgs{Control: controlSlowFrame, Value: "true"}))
	assert.True(t, a.slowFrame)
	require.NoError(t, command(t, a, opSetControl, SetControlArgs{Control: controlSlowFrame, Value: "false"}))
	assert.False(t, a.slowFrame)
	assert.Equal(t, booleanReason, refusal(command(t, a, opSetControl, SetControlArgs{Control: controlSlowFrame, Value: "yes"})).Reason)

	require.NoError(t, command(t, a, opSetControl, SetControlArgs{Control: controlDuration, Value: "2.5"}))
	assert.Equal(t, 2.5, a.durationSec)
	assert.Equal(t, durationRangeReason, refusal(command(t, a, opSetControl, SetControlArgs{Control: controlDuration, Value: "0"})).Reason)
	assert.Equal(t, durationRangeReason, refusal(command(t, a, opSetControl, SetControlArgs{Control: controlDuration, Value: "61"})).Reason)
	assert.Equal(t, durationRangeReason, refusal(command(t, a, opSetControl, SetControlArgs{Control: controlDuration, Value: "soon"})).Reason)
	assert.Equal(t, 2.5, a.durationSec, "a refused command changes nothing")

	require.NoError(t, command(t, a, opSetControl, SetControlArgs{Control: controlUnit, Value: "bytes"}))
	assert.Equal(t, task.UnitBytes, a.unit)
	assert.Equal(t, unknownUnitReason, refusal(command(t, a, opSetControl, SetControlArgs{Control: controlUnit, Value: "rows"})).Reason)
	assert.Equal(t, unknownControlReason, refusal(command(t, a, opSetControl, SetControlArgs{Control: "speed", Value: "1"})).Reason)

	h := a.Operations()
	assert.True(t, h.Restore(resControls, Controls{Unit: "steps", DurationSec: 7, SimulateError: true}))
	assert.Equal(t, Controls{Unit: "steps", DurationSec: 7, SimulateError: true}, a.controls())
	assert.False(t, h.Restore(resControls, Controls{Unit: "steps", DurationSec: 70}), "a restore is validated like a command")
	assert.False(t, h.Restore(resControls, "steps"))
	assert.Equal(t, Controls{Unit: "steps", DurationSec: 7, SimulateError: true}, a.controls())
}

// engineWindow is a window served by a host engine, with the gesture path
// installed; frame runs one host frame around body (windowhost's order:
// begin, command stage, snapshot, the app's frame, end).
type engineWindow struct {
	inst *App
	eng  *opengine.Engine
}

func newEngineWindow(t *testing.T) (w *engineWindow, cleanup func()) {
	inst, _, cleanup := setupApp(t)
	eng := opengine.New(ops.Catalog(), inst.Operations())
	ctx := app.NewStaticFrameContext(nil, nil)
	ctx.SetOperationsGesture(eng.Gesture)
	inst.frameCtx = ctx
	w = &engineWindow{inst: inst, eng: eng}
	w.frame(nil)
	return
}

func (inst *engineWindow) frame(body func()) {
	inst.eng.BeginFrame()
	inst.eng.ApplyQueued()
	inst.eng.TakeSnapshot()
	if body != nil {
		body()
	}
	inst.eng.EndFrame()
}

func (inst *engineWindow) submit(t *testing.T, id string, op string, in any) (out opwire.Outcome) {
	t.Helper()
	args, err := buscodec.Encode(in)
	require.NoError(t, err)
	// A frame first, so a control the test set directly is observed as the
	// app's change before the call expects the revision it reads.
	inst.frame(nil)
	revs := inst.eng.Revisions()
	inst.eng.Submit(op, opwire.CallRequest{CallId: id, Key: id, Args: args, Writer: "task:t",
		Expects: map[string]uint64{resControls: revs[resControls]}})
	inst.frame(nil)
	out, ok := inst.eng.Status(id)
	require.True(t, ok)
	return
}

// settle runs frames until the call leaves running, as the host does.
func (inst *engineWindow) settle(t *testing.T, id string) (out opwire.Outcome) {
	t.Helper()
	require.True(t, waitFor(func() bool {
		inst.frame(nil)
		out, _ = inst.eng.Status(id)
		return out.Phase != opwire.PhaseRunning
	}), "the spawn did not settle")
	return
}

func TestSpawnSettlesWhenTheTaskHasStarted(t *testing.T) {
	w, cleanup := newEngineWindow(t)
	defer cleanup()
	w.inst.durationSec = 0.1

	out := w.submit(t, "s1", opSpawn, SpawnArgs{Unit: "steps"})
	assert.Equal(t, opwire.PhaseRunning, out.Phase, out.Reason)
	out = w.settle(t, "s1")
	assert.Equal(t, opwire.PhaseCompleted, out.Phase, out.Reason)
	res, err := buscodec.Decode[SpawnResult](out.Result)
	require.NoError(t, err)
	assert.NotEmpty(t, res.Id)
	assert.Equal(t, "demo.steps", res.Kind)
	assert.Equal(t, task.UnitSteps, w.inst.unit, "the spawn's shape becomes the controls' unit")
	assert.Equal(t, "task:t", w.eng.Writer(resControls))

	list, err := query[ListTasksArgs, ListTasksResult](t, w.inst, opListTasks, ListTasksArgs{})
	require.NoError(t, err)
	require.Len(t, list.Tasks, 1)
	assert.Equal(t, res.Id, list.Tasks[0].Id)
	require.True(t, waitFor(func() bool {
		list, _ = query[ListTasksArgs, ListTasksResult](t, w.inst, opListTasks, ListTasksArgs{State: filterFinished})
		return list.Matched == 1
	}), "the task should finish within 2s")
	assert.Equal(t, stateDone, list.Tasks[0].State)
	assert.Equal(t, 1.0, list.Tasks[0].Fraction)

	out = w.submit(t, "s2", opSpawn, SpawnArgs{Unit: "rows"})
	assert.Equal(t, opwire.PhaseRefused, out.Phase)
	assert.Equal(t, unknownUnitReason, out.Reason)
}

func TestListTasksIsBoundedAndFiltered(t *testing.T) {
	a, _, cleanup := setupApp(t)
	defer cleanup()
	a.durationSec = 0.05
	for range 3 {
		require.NotNil(t, a.startTask(task.UnitItems))
	}
	require.True(t, waitFor(func() bool { return a.monitor.HistoryCount() == 3 }))
	require.True(t, waitFor(func() bool {
		out, _ := query[ListTasksArgs, ListTasksResult](t, a, opListTasks, ListTasksArgs{State: stateDone})
		return out.Matched == 3
	}), "the records follow the terminals")

	out, err := query[ListTasksArgs, ListTasksResult](t, a, opListTasks, ListTasksArgs{Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, 3, out.Matched)
	assert.Len(t, out.Tasks, 2)
	assert.True(t, out.Truncated)
	out, err = query[ListTasksArgs, ListTasksResult](t, a, opListTasks, ListTasksArgs{State: filterRunning})
	require.NoError(t, err)
	assert.Zero(t, out.Matched)
	assert.False(t, out.Truncated)
	_, err = query[ListTasksArgs, ListTasksResult](t, a, opListTasks, ListTasksArgs{Limit: -1})
	assert.Equal(t, limitReason, refusal(err).Reason)
	_, err = query[ListTasksArgs, ListTasksResult](t, a, opListTasks, ListTasksArgs{State: "paused"})
	assert.Equal(t, unknownStateReason, refusal(err).Reason)

	v, err := query[struct{}, View](t, a, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, 3, v.Spawned)
	assert.Equal(t, 3, v.History)
}

func TestRecordsAreCapped(t *testing.T) {
	a := newApp()
	for i := range opsMaxRecords + 5 {
		state := stateDone
		if i == 0 {
			state = stateRunning
		}
		a.record(&taskRecord{state: state})
	}
	assert.Len(t, a.records, opsMaxRecords)
	assert.Equal(t, stateRunning, a.records[0].state, "a running record is kept over a finished one")
}

func TestCancelTaskCancelsARunningTaskAndRefusesTheRest(t *testing.T) {
	a, _, cleanup := setupApp(t)
	defer cleanup()
	assert.Equal(t, unknownTaskReason, refusal(command(t, a, opCancelTask, CancelTaskArgs{Id: "none"})).Reason)

	a.durationSec = 10
	rec := a.startTask(task.UnitBytes)
	require.NotNil(t, rec)
	var id string
	require.True(t, waitFor(func() bool {
		a.recMu.Lock()
		id = string(rec.id)
		a.recMu.Unlock()
		row, found := a.findRecord(id)
		return found && row.State == stateRunning
	}), "the task should start")

	require.NoError(t, command(t, a, opCancelTask, CancelTaskArgs{Id: id}))
	require.True(t, waitFor(func() bool {
		row, _ := a.findRecord(id)
		return row.State == stateCancelled && a.monitor.HistoryCount() == 1
	}), "the worker should see the cancel on its next tick")
	assert.Equal(t, "task "+id+" is cancelled; cancel applies to a running task",
		refusal(command(t, a, opCancelTask, CancelTaskArgs{Id: id})).Reason)
}

func TestTheStartButtonIsTheSpawnGesture(t *testing.T) {
	w, cleanup := newEngineWindow(t)
	defer cleanup()
	w.inst.durationSec = 0.1
	w.frame(func() { w.inst.spawnGesture(task.UnitBytes) })
	log := w.eng.Log()
	require.NotEmpty(t, log)
	assert.Equal(t, opSpawn, log[len(log)-1].Op)
	assert.Equal(t, opwire.WriterPerson, log[len(log)-1].Writer)
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resControls))
	assert.Equal(t, task.UnitBytes, w.inst.unit)
	assert.Empty(t, w.eng.UndeclaredWrites(0))
	assert.Empty(t, w.inst.spawns, "a gesture is not an asynchronous call")
	require.True(t, waitFor(func() bool { return w.inst.monitor.HistoryCount() == 1 }))

	// Without a host serving the catalog, the button spawns directly.
	plain, _, cleanupPlain := setupApp(t)
	defer cleanupPlain()
	plain.durationSec = 0.1
	plain.spawnGesture(task.UnitSteps)
	assert.Equal(t, task.UnitSteps, plain.unit)
	require.True(t, waitFor(func() bool { return plain.monitor.HistoryCount() == 1 }))
}
