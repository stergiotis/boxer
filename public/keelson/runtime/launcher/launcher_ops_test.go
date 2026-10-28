package launcher

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
)

const (
	fixPlay  = "github.com/example/play"
	fixTop   = "github.com/example/imztop"
	fixDemo  = "github.com/example/demo"
	fixAbsnt = "github.com/example/absent"
)

// opsFixture is a launcher over three apps, with the controller role free.
func opsFixture(t *testing.T) (w *launcherApp) {
	t.Helper()
	reg := app.NewRegistry()
	for _, m := range []app.Manifest{
		mkTopicManifest(fixPlay, "Play", app.TopicSql, app.TopicData),
		mkTopicManifest(fixTop, "imztop", app.TopicObservability),
		mkTopicManifest(fixDemo, "Demo", app.TopicUi),
	} {
		if m.Id == fixDemo {
			m.Kind = app.KindDemo
		}
		require.NoError(t, reg.RegisterFactory(m, func() (app.AppI, error) { return nil, errors.New("fixture") }))
	}
	inst := New(reg, nil, nil, zerolog.Nop())
	w = NewApp(inst).(*launcherApp)
	prev := controller.Load()
	controller.Store(nil)
	t.Cleanup(func() { controller.Store(prev) })
	return
}

func query[In any, Out any](t *testing.T, w *launcherApp, name string, in In) (out Out, err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	raw, err := w.Operations().Snapshot().Query(name, args)
	if err != nil {
		return
	}
	out, decErr := buscodec.Decode[Out](raw)
	require.NoError(t, decErr)
	return
}

func command[In any](t *testing.T, w *launcherApp, name string, in In) (err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	_, err = w.Operations().ApplyCommand(app.OperationCall{}, name, args)
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
	for _, name := range []string{opGetView, opSetFilter, opShowApp} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
		if spec.Class == app.OperationClassCommand {
			assert.Equal(t, app.OperationEffectView, spec.Effect, name)
		}
	}
}

func TestSetFilterSetsWholeAndRefusesUnknownNames(t *testing.T) {
	w := opsFixture(t)
	w.claimController()
	in := SetFilterArgs{Query: "pl", Kinds: []string{"app"}, Topics: []string{"sql"}}
	for range 2 {
		require.NoError(t, command(t, w, opSetFilter, in))
		f := w.inst.filterState()
		assert.Equal(t, "pl", f.query)
		assert.False(t, f.kinds.shows(app.KindDemo))
		assert.True(t, f.kinds.shows(app.KindApp))
		assert.Equal(t, []string{"sql"}, selectedTopicNames(f.topics))
	}
	assert.Equal(t, []bool{true, false, false}, w.inst.kindShown, "the checkboxes follow the command")

	require.NoError(t, command(t, w, opSetFilter, SetFilterArgs{}))
	assert.Equal(t, filterT{}, w.inst.filterState(), "empty kinds show all, empty topics restrict nothing")
	assert.Equal(t, []bool{true, true, true}, w.inst.kindShown)

	assert.Contains(t, refusal(command(t, w, opSetFilter, SetFilterArgs{Kinds: []string{"widget"}})).Reason, "app, applet, demo")
	assert.Contains(t, refusal(command(t, w, opSetFilter, SetFilterArgs{Topics: []string{"nope"}})).Reason, "runtime, code")
	assert.Equal(t, filterT{}, w.inst.filterState(), "a refused command changes nothing")
}

func TestShowAppSelectsARowTheListHolds(t *testing.T) {
	w := opsFixture(t)
	w.claimController()
	for range 2 {
		require.NoError(t, command(t, w, opShowApp, ShowAppArgs{AppId: fixTop}))
		assert.Equal(t, app.AppIdT(fixTop), w.inst.selected)
	}
	rows := w.inst.buildRows(w.inst.visibleManifests())
	assert.Equal(t, app.AppIdT(fixTop), rows[w.inst.cursor].m.Id, "the cursor is the selection")

	assert.Equal(t, notListedReason, refusal(command(t, w, opShowApp, ShowAppArgs{AppId: fixAbsnt})).Reason)
	require.NoError(t, command(t, w, opSetFilter, SetFilterArgs{Kinds: []string{"app"}}))
	assert.Equal(t, notListedReason, refusal(command(t, w, opShowApp, ShowAppArgs{AppId: fixDemo})).Reason, "a hidden kind is not in the list")

	v, err := query[struct{}, View](t, w, opGetView, struct{}{})
	require.NoError(t, err)
	// Browsing lists an app under every topic it declares (ADR-0158 §SD3), so
	// play's two topics are two rows.
	assert.Equal(t, View{Kinds: []string{"app"}, Selected: fixTop, Rows: 3, Total: 3, Controller: true}, v)
}

func TestOnlyTheControllerWrites(t *testing.T) {
	w := opsFixture(t)
	other := NewApp(w.inst).(*launcherApp)
	ok, reason := w.Operations().Snapshot().Available(opSetFilter)
	assert.False(t, ok)
	assert.Equal(t, noControllerReason, reason)
	assert.Equal(t, noControllerReason, refusal(command(t, w, opShowApp, ShowAppArgs{AppId: fixTop})).Reason)

	w.claimController()
	other.claimController()
	ok, reason = other.Operations().Snapshot().Available(opShowApp)
	assert.False(t, ok)
	assert.Equal(t, notControllerReason, reason)
	assert.Equal(t, notControllerReason, refusal(command(t, other, opSetFilter, SetFilterArgs{Query: "x"})).Reason)
	ok, _ = w.Operations().Snapshot().Available(opShowApp)
	assert.True(t, ok)

	require.NoError(t, command(t, w, opSetFilter, SetFilterArgs{Query: "play"}))
	assert.Equal(t, "play", w.Operations().ResourceValue(resFilter).(filterT).query)
	assert.Equal(t, filterT{}, other.Operations().ResourceValue(resFilter), "only the controller's value moves")
	assert.False(t, other.Operations().Restore(resFilter, filterT{}))

	w.releaseController()
	other.claimController()
	assert.True(t, other.isController())
}

func TestFilterArgsRoundTrip(t *testing.T) {
	f := filterT{query: "q", kinds: kindFilterT(0).toggled(app.KindDemo)}
	idx, _ := topicIndex(app.TopicData)
	f.topics = f.topics.toggledAt(idx)
	back, err := parseFilterArgs(filterArgs(f))
	require.NoError(t, err)
	assert.Equal(t, f, back)
	in := filterArgs(filterT{})
	assert.Empty(t, in.Kinds, "no hidden kind spells as no kinds")
}

// engineWindow is a window served by a host engine, with the gesture path
// installed; frame runs one host frame around body (windowhost's order:
// begin, command stage, snapshot, the app's frame, end).
type engineWindow struct {
	w   *launcherApp
	eng *opengine.Engine
}

func newEngineWindow(t *testing.T) (ew *engineWindow) {
	w := opsFixture(t)
	eng := opengine.New(ops.Catalog(), w.Operations())
	ctx := app.NewStaticFrameContext(nil, nil)
	ctx.SetOperationsGesture(eng.Gesture)
	ew = &engineWindow{w: w, eng: eng}
	w.claimController()
	w.inst.frameCtx = ctx
	ew.frame(nil)
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
	ew := newEngineWindow(t)
	c := ew.w.inst

	out := ew.submit(t, "c1", opShowApp, ShowAppArgs{AppId: fixPlay})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Equal(t, "task:t", ew.eng.Writer(resSelection))
	out = ew.submit(t, "c2", opShowApp, ShowAppArgs{AppId: fixPlay})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Empty(t, ew.eng.UndeclaredWrites(0))

	// A chip click is the person's, through set_filter; a filter change
	// moves the selection, which the catalog declares as following.
	idx, _ := topicIndex(app.TopicObservability)
	ew.frame(func() {
		f := c.filterState()
		f.topics = f.topics.toggledAt(idx)
		c.setFilterGesture(f)
	})
	assert.Equal(t, opwire.WriterPerson, ew.eng.Writer(resFilter))
	log := ew.eng.Log()
	assert.Equal(t, opSetFilter, log[len(log)-1].Op)

	// Typing in the search box commits inside the frame without a command,
	// so it is the app's (ADR-0345 §SD3).
	ew.submit(t, "c3", opSetFilter, SetFilterArgs{})
	ew.frame(func() { c.searchText = "pl" })
	assert.Equal(t, opwire.WriterApp, ew.eng.Writer(resFilter))
	assert.Empty(t, ew.eng.UndeclaredWrites(0))

	// A checkbox write that landed at Sync is noticed once and routed.
	c.kindShown[app.KindDemo] = false
	ew.frame(func() { c.noticeKindToggles() })
	assert.Equal(t, opwire.WriterPerson, ew.eng.Writer(resFilter))
	assert.False(t, c.appliedKinds.shows(app.KindDemo))

	// A row click is the person's selection; the row is one the list holds.
	ew.submit(t, "c4", opSetFilter, SetFilterArgs{})
	ew.frame(func() { c.selectGesture(fixTop, 0) })
	assert.Equal(t, opwire.WriterPerson, ew.eng.Writer(resSelection))
	assert.Equal(t, app.AppIdT(fixTop), c.selected)
}
