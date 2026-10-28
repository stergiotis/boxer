package capinspector

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
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

// freshApp drains the status bar's queue so the window opens on the picker.
func freshApp() (inst *App) {
	for popSelection() != "" {
	}
	return newApp()
}

func TestOperationsCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	for _, name := range []string{opGetView, opGetActivity, opSelectCap} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
		if spec.Class == app.OperationClassCommand {
			assert.Equal(t, app.OperationEffectView, spec.Effect, name)
		}
	}
}

func TestViewReadsTheSelectedCapAndTheEffectiveBackend(t *testing.T) {
	resetActiveBackends()
	t.Cleanup(resetActiveBackends)
	inst := freshApp()

	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "", v.Selected)
	assert.Equal(t, "", v.Display, "the picker alone has no page")
	assert.Len(t, v.Caps, len(allCapIdsOrdered()))
	assert.Equal(t, CapChoice{Cap: CapRun, Display: Registry[CapRun].Display}, v.Caps[0])

	facts := Registry[CapFacts]
	require.NotEmpty(t, facts.Backends)
	SetActiveBackend(CapFacts, facts.Backends[0].Id)
	require.NoError(t, command(t, inst, opSelectCap, SelectCapArgs{Cap: CapFacts}))
	v, err = query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, CapFacts, v.Selected)
	assert.Equal(t, facts.Display, v.Display)
	assert.Equal(t, facts.SubjectFamily, v.SubjectFamily)
	assert.False(t, v.Degraded)
	assert.True(t, v.Backends[0].Effective)
	require.NotNil(t, facts.Schema)
	assert.Equal(t, facts.Schema.Qualified(), v.SchemaTable)

	resetActiveBackends()
	v, err = query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.True(t, v.Degraded, "no effective backend is the degraded signal")
}

func TestActivityReadsTheCountersAndIsBounded(t *testing.T) {
	inst := freshApp()
	before, err := query[GetActivityArgs, GetActivityResult](t, inst, opGetActivity, GetActivityArgs{Cap: CapPersist})
	require.NoError(t, err)
	require.Len(t, before.Caps, 1)
	Tally.Record(audit.AuditRecord{Subject: "runtime.persist.x.get"})
	after, err := query[GetActivityArgs, GetActivityResult](t, inst, opGetActivity, GetActivityArgs{Cap: CapPersist})
	require.NoError(t, err)
	assert.Equal(t, before.Caps[0].Count+1, after.Caps[0].Count)
	assert.Len(t, after.Caps[0].Buckets, sparkBuckets)
	assert.Equal(t, int(sparkBucketWidth.Seconds()), after.Caps[0].BucketSeconds)

	all, err := query[GetActivityArgs, GetActivityResult](t, inst, opGetActivity, GetActivityArgs{})
	require.NoError(t, err)
	assert.Len(t, all.Caps, len(allCapIdsOrdered()))
	assert.Equal(t, CapRun, all.Caps[0].Cap, "picker order")

	_, err = query[GetActivityArgs, GetActivityResult](t, inst, opGetActivity, GetActivityArgs{Cap: "nope"})
	require.NotNil(t, refusal(err))
	assert.Contains(t, refusal(err).Reason, CapBus)
}

func TestSelectCapSetsAndNeverToggles(t *testing.T) {
	inst := freshApp()
	for range 2 {
		require.NoError(t, command(t, inst, opSelectCap, SelectCapArgs{Cap: CapBus}))
		assert.Equal(t, CapBus, inst.selectedCap)
	}
	assert.Equal(t, CapBus, inst.Operations().ResourceValue(resSelection))
	assert.NotNil(t, refusal(command(t, inst, opSelectCap, SelectCapArgs{Cap: "nope"})))
	assert.Equal(t, CapBus, inst.selectedCap, "a refused command changes nothing")
	require.NoError(t, command(t, inst, opSelectCap, SelectCapArgs{}))
	assert.Equal(t, "", inst.selectedCap)

	h := inst.Operations()
	assert.True(t, h.Restore(resSelection, CapFs))
	assert.Equal(t, CapFs, inst.selectedCap)
	assert.False(t, h.Restore(resSelection, "nope"))
	assert.False(t, h.Restore(resSelection, 3))
}

// engineWindow is a window served by a host engine, with the gesture path
// installed; frame runs one host frame around body (windowhost's order:
// begin, command stage, snapshot, the app's frame, end).
type engineWindow struct {
	inst *App
	eng  *opengine.Engine
}

func newEngineWindow() (w *engineWindow) {
	inst := freshApp()
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
	revs := inst.eng.Revisions()
	inst.eng.Submit(op, opwire.CallRequest{CallId: id, Args: args, Writer: "task:t", Expects: map[string]uint64{resSelection: revs[resSelection]}})
	inst.frame(nil)
	out, ok := inst.eng.Status(id)
	require.True(t, ok)
	return
}

func TestTheEngineAttributesEachWriter(t *testing.T) {
	w := newEngineWindow()

	out := w.submit(t, "c1", opSelectCap, SelectCapArgs{Cap: CapFs})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Equal(t, "task:t", w.eng.Writer(resSelection))
	out = w.submit(t, "c2", opSelectCap, SelectCapArgs{Cap: CapFs})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Empty(t, w.eng.UndeclaredWrites(0))

	// The picker is the person's, through the same command.
	w.frame(func() { w.inst.selectCapGesture(CapBus) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resSelection))
	assert.Equal(t, CapBus, w.inst.selectedCap)
	log := w.eng.Log()
	assert.Equal(t, opSelectCap, log[len(log)-1].Op)

	// A status-bar click lands inside the frame without a command, so its
	// write is the app's.
	PushSelection(CapPersist)
	w.frame(func() { w.inst.consumePendingSelection() })
	assert.Equal(t, opwire.WriterApp, w.eng.Writer(resSelection))
	assert.Equal(t, CapPersist, w.inst.selectedCap)
}
