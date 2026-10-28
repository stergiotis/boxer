package appstate

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

// fixedReader serves one set of columns, or an error.
type fixedReader struct {
	cols entryCols
	err  error
}

func (inst *fixedReader) entries(context.Context) (cols entryCols, err error) {
	return inst.cols, inst.err
}

// opsFixture is a window that has read four entries of three apps, without
// a bus or a poller.
func opsFixture(t *testing.T) (inst *App, reader *fixedReader) {
	t.Helper()
	reader = &fixedReader{}
	reader.cols.Add(entryRow{Kind: "persist", AppId: "x/play", Key: "tabs", EntityId: "e1", PayloadBytes: 10, Detail: "3 tabs", WrittenAt: "2026-09-22T10:00:00Z", RunId: "r1", InstanceKey: 3})
	reader.cols.Add(entryRow{Kind: "column_width", AppId: "x/play", Key: "column//k", EntityId: "e2"})
	reader.cols.Add(entryRow{Kind: "workingset", AppId: "x/tally", Key: "default", EntityId: "e3", PayloadBytes: 5})
	reader.cols.Add(entryRow{Kind: "persist", AppId: "y/mdedit", Key: "doc", EntityId: "e4", PayloadBytes: 7})
	inst = newApp()
	inst.reader = reader
	inst.appCtx = context.Background()
	inst.refresh()
	snap := inst.snapshot()
	require.Equal(t, 4, snap.rows.Len())
	return
}

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

func TestOperationsCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	for _, name := range []string{opListEntries, opGetView, opSelectApp, opRefresh} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
		if spec.Class == app.OperationClassCommand {
			assert.Equal(t, app.OperationEffectView, spec.Effect, name)
		}
	}
}

func TestListEntriesReadsItsOwnArgumentsNotTheView(t *testing.T) {
	inst, _ := opsFixture(t)
	inst.selected = "x/tally"

	out, err := query[ListEntriesArgs, ListEntriesResult](t, inst, opListEntries, ListEntriesArgs{})
	require.NoError(t, err)
	assert.Equal(t, 4, out.Matched, "the selection is not the query's filter")
	require.Len(t, out.Entries, 4)
	assert.Equal(t, Entry{Kind: "persist", App: "x/play", Key: "tabs", EntityId: "e1", PayloadBytes: 10, Detail: "3 tabs",
		WrittenAt: "2026-09-22T10:00:00Z", RunId: "r1", InstanceKey: 3}, out.Entries[0])
	assert.False(t, out.ReadAt.IsZero())

	out, err = query[ListEntriesArgs, ListEntriesResult](t, inst, opListEntries, ListEntriesArgs{App: "PLAY", Kind: "persist"})
	require.NoError(t, err)
	assert.Equal(t, 1, out.Matched)
	assert.Equal(t, "tabs", out.Entries[0].Key)

	out, err = query[ListEntriesArgs, ListEntriesResult](t, inst, opListEntries, ListEntriesArgs{Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, 4, out.Matched)
	assert.Len(t, out.Entries, 2, "the limit bounds the rows, not the count")

	_, err = query[ListEntriesArgs, ListEntriesResult](t, inst, opListEntries, ListEntriesArgs{Limit: -1})
	assert.NotNil(t, refusal(err))
}

func TestListEntriesIsBounded(t *testing.T) {
	inst, reader := opsFixture(t)
	for i := range opsMaxEntries + 20 {
		reader.cols.Add(entryRow{Kind: "persist", AppId: "z/many", Key: string(rune('a' + i%26)), EntityId: "m"})
	}
	inst.refresh()
	out, err := query[ListEntriesArgs, ListEntriesResult](t, inst, opListEntries, ListEntriesArgs{App: "many", Limit: opsMaxEntries + 50})
	require.NoError(t, err)
	assert.Equal(t, opsMaxEntries+20, out.Matched)
	assert.Len(t, out.Entries, opsMaxEntries)
	assert.Len(t, cutText(string(make([]byte, opsMaxDetailLen+9)), opsMaxDetailLen), opsMaxDetailLen+len("…"))
}

func TestViewReflectsTheReadAndItsErrors(t *testing.T) {
	inst, reader := opsFixture(t)
	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, View{Entries: 4, Apps: 3, ReadAt: v.ReadAt, Reads: true}, v)
	assert.False(t, v.ReadAt.IsZero())

	reader.err = errors.New("boom")
	inst.refresh()
	v, err = query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "read: boom", v.ReadError)
	assert.Equal(t, 4, v.Entries, "a failed read keeps the rows the window had")

	none := newApp()
	none.appCtx = context.Background()
	none.refresh()
	v, err = query[struct{}, View](t, none, opGetView, struct{}{})
	require.NoError(t, err)
	assert.False(t, v.Reads)
	assert.Contains(t, v.ReadError, "no bus")
}

func TestSelectAppSetsAndNeverToggles(t *testing.T) {
	inst, _ := opsFixture(t)
	for range 2 {
		require.NoError(t, command(t, inst, opSelectApp, SelectAppArgs{App: "x/play"}))
		assert.Equal(t, "x/play", inst.selected)
	}
	assert.Equal(t, "x/play", inst.Operations().ResourceValue(resSelection))
	assert.Equal(t, unknownAppReason, refusal(command(t, inst, opSelectApp, SelectAppArgs{App: "x/none"})).Reason)
	assert.Equal(t, "x/play", inst.selected, "a refused command changes nothing")
	require.NoError(t, command(t, inst, opSelectApp, SelectAppArgs{}))
	assert.Equal(t, "", inst.selected)

	h := inst.Operations()
	assert.True(t, h.Restore(resSelection, "x/tally"))
	assert.Equal(t, "x/tally", inst.selected)
	assert.False(t, h.Restore(resSelection, 3))
}

func TestRefreshAsksThePollerToReadAgain(t *testing.T) {
	inst, _ := opsFixture(t)
	for range 2 {
		require.NoError(t, command(t, inst, opRefresh, struct{}{}))
	}
	select {
	case <-inst.dirty:
	default:
		t.Fatal("refresh did not mark the window dirty")
	}
}

// engineWindow is a window served by a host engine, with the gesture path
// installed; frame runs one host frame around body (windowhost's order:
// begin, command stage, snapshot, the app's frame, end).
type engineWindow struct {
	inst *App
	eng  *opengine.Engine
}

func newEngineWindow(t *testing.T) (w *engineWindow) {
	inst, _ := opsFixture(t)
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
	inst.inst.latchRead()
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
	inst.eng.Submit(op, opwire.CallRequest{CallId: id, Args: args, Writer: "task:t",
		Expects: map[string]uint64{resSelection: revs[resSelection], resRead: revs[resRead]}})
	inst.frame(nil)
	out, ok := inst.eng.Status(id)
	require.True(t, ok)
	return
}

func TestTheEngineAttributesEachWriter(t *testing.T) {
	w := newEngineWindow(t)

	out := w.submit(t, "c1", opSelectApp, SelectAppArgs{App: "x/play"})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Equal(t, "task:t", w.eng.Writer(resSelection))
	out = w.submit(t, "c2", opSelectApp, SelectAppArgs{App: "x/play"})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Empty(t, w.eng.UndeclaredWrites(0))

	// A row of the app list and the Refresh button are the person's,
	// through the same commands.
	w.frame(func() { w.inst.selectAppGesture("x/tally") })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resSelection))
	assert.Equal(t, "x/tally", w.inst.selected)
	w.frame(func() { w.inst.refreshGesture() })
	log := w.eng.Log()
	assert.Equal(t, opRefresh, log[len(log)-1].Op)
	assert.Equal(t, opwire.WriterPerson, log[len(log)-1].Writer)
	assert.Empty(t, w.eng.UndeclaredWrites(0))

	// An agent's refresh applies but moves nothing in its frame: the poller
	// lands the read between frames, and the latch attributes the landing
	// to the app rather than to the person (ADR-0329 §SD3's trade-off).
	out = w.submit(t, "r1", opRefresh, struct{}{})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	before := w.eng.Revisions()[resRead]
	w.inst.refresh()
	w.frame(nil)
	assert.Greater(t, w.eng.Revisions()[resRead], before, "the landed read moves the resource")
	assert.Equal(t, opwire.WriterApp, w.eng.Writer(resRead))
	assert.Empty(t, w.eng.UndeclaredWrites(0))
}
