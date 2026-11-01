package helphost

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

func opsHost(t *testing.T) (h *HelpHost, appId app.AppIdT) {
	t.Helper()
	lib, appId := fixtureLibrary(t)
	h = New()
	h.SetLibrary(lib)
	return
}

func query[In any, Out any](t *testing.T, inst *HelpHost, name string, in In) (out Out, err error) {
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

func command[In any](t *testing.T, inst *HelpHost, name string, in In) (err error) {
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
	for _, name := range []string{opGetView, opShowDoc, opSetExpanded, opSetViewMode, opSetSearch} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
		if spec.Class == app.OperationClassCommand {
			assert.Equal(t, app.OperationEffectView, spec.Effect, name)
		}
	}
}

func TestShowDocSetsTheSelectionAndRefusesWhatTheLibraryLacks(t *testing.T) {
	h, appId := opsHost(t)
	in := ShowDocArgs{AppId: string(appId), Doc: "howto/replay"}
	for range 2 {
		require.NoError(t, command(t, h, opShowDoc, in))
		assert.Equal(t, help.RefT{AppId: appId, Doc: "howto/replay"}, h.CurrentRef())
		assert.True(t, h.expandedApps[appId], "opening a document expands its book")
	}
	assert.Equal(t, noBookReason, refusal(command(t, h, opShowDoc, ShowDocArgs{AppId: "github.com/none"})).Reason)
	assert.Equal(t, noDocReason, refusal(command(t, h, opShowDoc, ShowDocArgs{AppId: string(appId), Doc: "missing"})).Reason)
	assert.Equal(t, noSecReason, refusal(command(t, h, opShowDoc, ShowDocArgs{AppId: string(appId), Doc: "overview", Section: "nope"})).Reason)
	assert.NotNil(t, refusal(command(t, h, opShowDoc, ShowDocArgs{AppId: string(appId), Section: "x"})))
	assert.Equal(t, help.RefT{AppId: appId, Doc: "howto/replay"}, h.CurrentRef(), "a refused command changes nothing")

	require.NoError(t, command(t, h, opShowDoc, ShowDocArgs{AppId: string(appId)}))
	assert.Equal(t, help.RefT{AppId: appId}, h.CurrentRef())
	require.NoError(t, command(t, h, opShowDoc, ShowDocArgs{}))
	assert.True(t, h.CurrentRef().IsZero(), "an empty app id shows the overview")
}

func TestViewCommandsSetAndNeverToggle(t *testing.T) {
	h, appId := opsHost(t)
	for range 2 {
		require.NoError(t, command(t, h, opSetExpanded, SetExpandedArgs{AppId: string(appId), Expanded: true}))
		assert.True(t, h.expandedApps[appId])
	}
	require.NoError(t, command(t, h, opSetExpanded, SetExpandedArgs{AppId: string(appId), Expanded: false}))
	assert.False(t, h.expandedApps[appId])
	assert.Equal(t, noBookReason, refusal(command(t, h, opSetExpanded, SetExpandedArgs{AppId: "github.com/none"})).Reason)

	for range 2 {
		require.NoError(t, command(t, h, opSetViewMode, SetViewModeArgs{Mode: "source"}))
		assert.Equal(t, ViewModeSource, h.ViewMode())
	}
	require.NoError(t, command(t, h, opSetViewMode, SetViewModeArgs{}))
	assert.Equal(t, ViewModeRendered, h.ViewMode(), "an empty mode is rendered")
	assert.NotNil(t, refusal(command(t, h, opSetViewMode, SetViewModeArgs{Mode: "pdf"})))

	require.NoError(t, command(t, h, opSetSearch, SetSearchArgs{Query: "replay"}))
	assert.Equal(t, "replay", h.searchText)
	assert.Equal(t, "replay", h.Operations().ResourceValue(resSearch))

	require.NoError(t, command(t, h, opShowDoc, ShowDocArgs{AppId: string(appId), Doc: "overview"}))
	v, err := query[struct{}, View](t, h, opGetView, struct{}{})
	require.NoError(t, err)
	// The library syncs the registry's books on first read, so the count is
	// the fixture's plus whatever this process registered.
	assert.GreaterOrEqual(t, v.Books, 1)
	v.Books = 0
	assert.Equal(t, View{AppId: string(appId), Doc: "overview", ViewMode: "rendered", Expanded: []string{string(appId)},
		Search: "replay"}, v)
}

func TestUndoRestoresTheView(t *testing.T) {
	h, appId := opsHost(t)
	ops := h.Operations()
	assert.True(t, ops.Restore(resSelection, help.RefT{AppId: appId, Doc: "overview", Section: "s"}))
	assert.Equal(t, "s", h.selectedSection)
	assert.True(t, ops.Restore(resExpanded, string(appId)+"\ngithub.com/other"))
	assert.True(t, h.expandedApps[appId])
	assert.True(t, h.expandedApps["github.com/other"])
	assert.Equal(t, "github.com/other\n"+string(appId), ops.ResourceValue(resExpanded), "sorted, so a map compares as one value")
	assert.True(t, ops.Restore(resExpanded, ""))
	assert.Empty(t, h.expandedApps)
	assert.True(t, ops.Restore(resViewMode, ViewModeSource))
	assert.Equal(t, ViewModeSource, h.viewMode)
	assert.False(t, ops.Restore(resViewMode, "source"))
	assert.True(t, ops.Restore(resSearch, "q"))
	assert.Equal(t, "q", h.searchText)
}

// engineWindow is a window served by a host engine, with the gesture path
// installed; frame runs one host frame around body (windowhost's order:
// begin, command stage, snapshot, the app's frame, end).
type engineWindow struct {
	inst *HelpHost
	eng  *opengine.Engine
}

func newEngineWindow(t *testing.T) (w *engineWindow, appId app.AppIdT) {
	inst, appId := opsHost(t)
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
	for _, r := range []string{resSelection, resExpanded, resViewMode, resSearch} {
		expects[r] = revs[r]
	}
	inst.eng.Submit(op, opwire.CallRequest{CallId: id, Args: args, Writer: "task:t", Expects: expects})
	inst.frame(nil)
	out, ok := inst.eng.Status(id)
	require.True(t, ok)
	return
}

func TestTheEngineAttributesEachWriter(t *testing.T) {
	w, appId := newEngineWindow(t)

	out := w.submit(t, "c1", opShowDoc, ShowDocArgs{AppId: string(appId), Doc: "overview"})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Equal(t, "task:t", w.eng.Writer(resSelection))
	assert.Equal(t, "task:t", w.eng.Writer(resExpanded))
	out = w.submit(t, "c2", opShowDoc, ShowDocArgs{AppId: string(appId), Doc: "overview"})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Empty(t, w.eng.UndeclaredWrites(0))

	// Typing in the search box commits inside the frame without a command,
	// so it is the app's (ADR-0345 §SD3); a nav click is the person's.
	w.submit(t, "c3", opSetSearch, SetSearchArgs{Query: "r"})
	w.frame(func() { w.inst.searchText = "re" })
	assert.Equal(t, opwire.WriterApp, w.eng.Writer(resSearch))
	assert.Empty(t, w.eng.UndeclaredWrites(0))

	w.frame(func() { w.inst.showDocGesture(help.RefT{AppId: appId, Doc: "howto/replay"}) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resSelection))
	log := w.eng.Log()
	assert.Equal(t, opShowDoc, log[len(log)-1].Op)
	w.frame(func() { w.inst.setExpandedGesture(appId, false) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resExpanded))
	assert.False(t, w.inst.expandedApps[appId])
	w.frame(func() { w.inst.setViewModeGesture(ViewModeSource) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resViewMode))
	w.frame(func() { w.inst.setSearchGesture("") })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resSearch))
	assert.Equal(t, "", w.inst.searchText)
}
