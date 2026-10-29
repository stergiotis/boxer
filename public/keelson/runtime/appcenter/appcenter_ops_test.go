package appcenter

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

// opsFixture is an app center whose reader answers keelson('apps') with two
// rows, one of them this app, read once.
func opsFixture(t *testing.T) (inst *App) {
	t.Helper()
	fx := &fixtureReader{answers: map[string]func(dst any){
		tableApps: func(dst any) {
			*dst.(*appCols) = appCols{Id: []string{string(AppId), "github.com/example/other"}, Display: []string{"App center", "Other"},
				Summary: []string{"", "another app"}, Icon: []string{"", ""}, Kind: []string{"app", "app"},
				Surface: []string{"windowed", "windowed"}, Caps: [][]string{nil, nil}, LaunchKind: []string{"", ""}}
		},
	}}
	inst = newApp()
	inst.reader = fx
	inst.appCtx, inst.cancelApp = context.WithCancel(context.Background())
	t.Cleanup(inst.cancelApp)
	inst.wantGlobal = true
	inst.refresh()
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
	for _, name := range []string{opGetView, opShowApp, opSetFilter, opListRunning} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
		if spec.Class == app.OperationClassCommand {
			assert.Equal(t, app.OperationEffectView, spec.Effect, name)
		}
	}
}

func TestShowAppOpensARegisteredAppsPage(t *testing.T) {
	inst := opsFixture(t)
	for range 2 {
		require.NoError(t, command(t, inst, opShowApp, ShowAppArgs{AppId: string(AppId)}))
		assert.Equal(t, string(AppId), inst.selected)
	}
	assert.Equal(t, string(AppId), inst.want, "the poller reads the page")
	assert.Equal(t, notRegisteredReason, refusal(command(t, inst, opShowApp, ShowAppArgs{AppId: "github.com/example/other"})).Reason,
		"a row the table lists is not a page an agent can open unless the app is registered here")
	assert.Equal(t, string(AppId), inst.selected, "a refused command changes nothing")
	require.NoError(t, command(t, inst, opShowApp, ShowAppArgs{}))
	assert.Equal(t, "", inst.selected)
}

func TestSetFilterAndTheView(t *testing.T) {
	inst := opsFixture(t)
	for range 2 {
		require.NoError(t, command(t, inst, opSetFilter, SetFilterArgs{Text: "other"}))
		assert.Equal(t, "other", inst.filter)
	}
	require.NoError(t, command(t, inst, opShowApp, ShowAppArgs{AppId: string(AppId)}))
	inst.refresh()
	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, string(AppId), v.Selected)
	assert.Equal(t, "other", v.Filter)
	assert.Equal(t, 1, v.Listed)
	assert.Equal(t, 2, v.Registered)
	assert.True(t, v.Reads)
	assert.False(t, v.PageReadAt.IsZero(), "the page was read for the selected app")

	require.NoError(t, command(t, inst, opShowApp, ShowAppArgs{}))
	v, err = query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.True(t, v.PageReadAt.IsZero(), "no page is open")

	h := inst.Operations()
	assert.True(t, h.Restore(resFilter, ""))
	assert.Equal(t, "", inst.filter)
	assert.True(t, h.Restore(resSelection, string(AppId)))
	assert.Equal(t, string(AppId), inst.selected)
	assert.False(t, h.Restore(resSelection, 3))
}

// engineWindow is a window served by a host engine, with the gesture path
// installed; frame runs one host frame around body.
type engineWindow struct {
	inst *App
	eng  *opengine.Engine
}

func newEngineWindow(t *testing.T) (w *engineWindow) {
	inst := opsFixture(t)
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
	expects := map[string]uint64{}
	for _, r := range []string{resFilter, resSelection} {
		expects[r] = revs[r]
	}
	inst.eng.Submit(op, opwire.CallRequest{CallId: id, Args: args, Writer: "task:t", Expects: expects})
	inst.frame(nil)
	out, ok := inst.eng.Status(id)
	require.True(t, ok)
	return
}

func TestTheEngineAttributesEachWriter(t *testing.T) {
	w := newEngineWindow(t)

	out := w.submit(t, "c1", opShowApp, ShowAppArgs{AppId: string(AppId)})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Equal(t, "task:t", w.eng.Writer(resSelection))
	out = w.submit(t, "c2", opShowApp, ShowAppArgs{AppId: string(AppId)})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Empty(t, w.eng.UndeclaredWrites(0))

	// Typing in the filter box commits inside the frame without a command,
	// so it is the app's (ADR-0345 §SD3).
	w.submit(t, "c3", opSetFilter, SetFilterArgs{Text: "o"})
	w.frame(func() { w.inst.filter = "ot" })
	assert.Equal(t, opwire.WriterApp, w.eng.Writer(resFilter))
	assert.Empty(t, w.eng.UndeclaredWrites(0))

	// A row click is the person's, through the same command.
	w.frame(func() { w.inst.selectGesture("") })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resSelection))
	assert.Equal(t, "", w.inst.selected)
	log := w.eng.Log()
	assert.Equal(t, opShowApp, log[len(log)-1].Op)
}
