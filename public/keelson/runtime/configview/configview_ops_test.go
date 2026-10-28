package configview

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

// The registry is process-wide, so the fixture variables are declared once
// for the test binary; the values are set per test with t.Setenv.
var (
	testPlain = env.NewString(env.Spec{Name: "CONFIGVIEW_OPS_TEST_PLAIN", Description: "a plain fixture of the catalog tests",
		Category: env.CategoryTestIntegration, Default: "fallback"})
	testSecret = env.NewString(env.Spec{Name: "CONFIGVIEW_OPS_TEST_SECRET", Description: "a sensitive fixture of the catalog tests",
		Category: env.CategoryTestIntegration, Sensitive: true, Default: "topsecret-default"})
)

const fixtureCategory = string(env.CategoryTestIntegration)

func opsFixture(t *testing.T) {
	t.Helper()
	t.Setenv(testPlain.Spec().Name, "plain-value")
	t.Setenv(testSecret.Spec().Name, "hunter2")
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

func rowNamed(rows []VarSummary, name string) (r VarSummary, ok bool) {
	for _, c := range rows {
		if c.Name == name {
			r, ok = c, true
			return
		}
	}
	return
}

// builtInstance is an instance whose hierarchy was built once, as a frame
// would, so the outline's sections exist for expand_category and get_view.
func builtInstance() (inst *App) {
	inst = newInstance(manifest)
	inst.buildTree(groupByCategory(applyFilter(env.All(), inst.filter)))
	inst.syncTree()
	return
}

func TestOperationsCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	for _, name := range []string{opListVars, opGetVar, opGetView, opSetFilter, opSelectVar, opExpandCategory} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
		if spec.Class == app.OperationClassCommand {
			assert.Equal(t, app.OperationEffectView, spec.Effect, name)
		}
	}
	for _, r := range m.Operations.Resources {
		assert.NotContains(t, r.Name, "reveal", "the reveal toggle is not a resource (ADR-0345 §SD4)")
	}
}

func TestListVarsReadsItsOwnArgumentsNotTheView(t *testing.T) {
	opsFixture(t)
	inst := newInstance(manifest)
	inst.filter = Filter{Query: "no-such-variable-anywhere", OnlySet: true}

	out, err := query[ListVarsArgs, ListVarsResult](t, inst, opListVars, ListVarsArgs{Category: fixtureCategory, Query: "configview_ops_test"})
	require.NoError(t, err)
	assert.Equal(t, 2, out.Matched, "the window's filter is not the query's")
	assert.GreaterOrEqual(t, out.Registered, 2)
	require.Len(t, out.Rows, 2)
	assert.Equal(t, testPlain.Spec().Name, out.Rows[0].Name, "by name within the category")
	plain := out.Rows[0]
	assert.Equal(t, VarSummary{Name: testPlain.Spec().Name, Category: fixtureCategory, Type: "string", Set: true,
		Value: "plain-value", Default: "fallback", Description: "a plain fixture of the catalog tests"}, plain)

	out, err = query[ListVarsArgs, ListVarsResult](t, inst, opListVars, ListVarsArgs{Query: "plain fixture", OnlySet: true})
	require.NoError(t, err)
	assert.Equal(t, 1, out.Matched, "the description matches too")

	_, err = query[ListVarsArgs, ListVarsResult](t, inst, opListVars, ListVarsArgs{Category: "nope"})
	require.NotNil(t, refusal(err))
	assert.Contains(t, refusal(err).Reason, fixtureCategory)
	_, err = query[ListVarsArgs, ListVarsResult](t, inst, opListVars, ListVarsArgs{Limit: -1})
	assert.NotNil(t, refusal(err))
}

func TestListVarsIsBounded(t *testing.T) {
	opsFixture(t)
	inst := newInstance(manifest)
	out, err := query[ListVarsArgs, ListVarsResult](t, inst, opListVars, ListVarsArgs{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, out.Rows, 1)
	assert.Equal(t, len(env.All()), out.Matched)
	out, err = query[ListVarsArgs, ListVarsResult](t, inst, opListVars, ListVarsArgs{Limit: opsMaxRows + 1})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(out.Rows), opsMaxRows)
}

func TestASensitiveValueIsWithheldWhateverTheRevealToggleSays(t *testing.T) {
	opsFixture(t)
	for _, reveal := range []bool{false, true} {
		inst := newInstance(manifest)
		inst.filter.RevealSensitive = reveal

		out, err := query[ListVarsArgs, ListVarsResult](t, inst, opListVars, ListVarsArgs{Category: fixtureCategory})
		require.NoError(t, err)
		secret, ok := rowNamed(out.Rows, testSecret.Spec().Name)
		require.True(t, ok)
		assert.True(t, secret.Sensitive)
		assert.True(t, secret.Set)
		assert.True(t, secret.Withheld, "reveal=%v", reveal)
		assert.Empty(t, secret.Value, "reveal=%v", reveal)
		assert.Empty(t, secret.Default, "reveal=%v", reveal)

		d, err := query[GetVarArgs, VarDetail](t, inst, opGetVar, GetVarArgs{Name: testSecret.Spec().Name})
		require.NoError(t, err)
		assert.True(t, d.Withheld)
		assert.Empty(t, d.Value)
		assert.Empty(t, d.Default)
		assert.Equal(t, "a sensitive fixture of the catalog tests", d.Description)
		assert.NotEmpty(t, d.Package)
	}
	// Unset says unset, which carries no secret; the default stays withheld.
	t.Setenv(testSecret.Spec().Name, "")
	d, err := query[GetVarArgs, VarDetail](t, newInstance(manifest), opGetVar, GetVarArgs{Name: testSecret.Spec().Name})
	require.NoError(t, err)
	assert.False(t, d.Set)
	assert.True(t, d.Withheld)
	assert.Empty(t, d.Default)
}

func TestGetVarRefusesAnUnknownName(t *testing.T) {
	_, err := query[GetVarArgs, VarDetail](t, newInstance(manifest), opGetVar, GetVarArgs{Name: "NOT_REGISTERED_ANYWHERE"})
	require.NotNil(t, refusal(err))
}

func TestViewCommandsSetAndNeverToggle(t *testing.T) {
	opsFixture(t)
	inst := builtInstance()

	for range 2 {
		require.NoError(t, command(t, inst, opSetFilter, SetFilterArgs{Query: "ops_test", OnlySet: true}))
		assert.Equal(t, filterState{Query: "ops_test", OnlySet: true}, inst.filterValue())
	}
	assert.False(t, inst.filter.RevealSensitive, "set_filter never touches the reveal toggle")

	for range 2 {
		require.NoError(t, command(t, inst, opSelectVar, SelectVarArgs{Name: testPlain.Spec().Name}))
		assert.Equal(t, testPlain.Spec().Name, inst.selected)
	}
	assert.NotNil(t, refusal(command(t, inst, opSelectVar, SelectVarArgs{Name: "NOT_REGISTERED_ANYWHERE"})))
	assert.Equal(t, testPlain.Spec().Name, inst.selected, "a refused command changes nothing")

	for range 2 {
		require.NoError(t, command(t, inst, opExpandCategory, ExpandCategoryArgs{Category: fixtureCategory, Expanded: true}))
		assert.Equal(t, []string{fixtureCategory}, inst.expandedCategories())
	}
	assert.NotNil(t, refusal(command(t, inst, opExpandCategory, ExpandCategoryArgs{Category: "nope", Expanded: true})))

	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "ops_test", v.Query)
	assert.True(t, v.OnlySet)
	assert.Equal(t, testPlain.Spec().Name, v.Selected)
	assert.Equal(t, 2, v.Matched)
	var shown *CategoryShown
	for i := range v.Categories {
		if v.Categories[i].Name == fixtureCategory {
			shown = &v.Categories[i]
		}
	}
	require.NotNil(t, shown)
	assert.True(t, shown.Expanded)
	assert.GreaterOrEqual(t, shown.Total, 2)
	assert.GreaterOrEqual(t, shown.Set, 2)

	require.NoError(t, command(t, inst, opExpandCategory, ExpandCategoryArgs{Category: fixtureCategory, Expanded: false}))
	assert.Empty(t, inst.expandedCategories())
	require.NoError(t, command(t, inst, opSelectVar, SelectVarArgs{}))
	assert.Equal(t, "", inst.selected)
}

func TestUndoRestoresTheView(t *testing.T) {
	opsFixture(t)
	inst := builtInstance()
	h := inst.Operations()
	assert.True(t, h.Restore(resFilter, filterState{Query: "x", OnlySet: true}))
	assert.Equal(t, "x", inst.filter.Query)
	assert.False(t, h.Restore(resFilter, "x"))
	assert.True(t, h.Restore(resSelection, testPlain.Spec().Name))
	assert.Equal(t, testPlain.Spec().Name, inst.selected)
	assert.True(t, h.Restore(resExpanded, fixtureCategory))
	assert.Equal(t, fixtureCategory, h.ResourceValue(resExpanded))
	assert.True(t, h.Restore(resExpanded, ""))
	assert.Equal(t, "", h.ResourceValue(resExpanded))
}

// engineWindow is a window served by a host engine, with the gesture path
// installed; frame runs one host frame around body (windowhost's order:
// begin, command stage, snapshot, the app's frame, end).
type engineWindow struct {
	inst *App
	eng  *opengine.Engine
}

func newEngineWindow() (w *engineWindow) {
	inst := builtInstance()
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
	// The task read every resource: a revision never bumped is zero.
	revs := inst.eng.Revisions()
	expects := map[string]uint64{}
	for _, r := range []string{resFilter, resSelection, resExpanded} {
		expects[r] = revs[r]
	}
	inst.eng.Submit(op, opwire.CallRequest{CallId: id, Args: args, Writer: "task:t", Expects: expects})
	inst.frame(nil)
	out, ok := inst.eng.Status(id)
	require.True(t, ok)
	return
}

func TestTheEngineAttributesEachWriter(t *testing.T) {
	opsFixture(t)
	w := newEngineWindow()

	// An agent's command is the task's, and writes only what it declares.
	out := w.submit(t, "c1", opSetFilter, SetFilterArgs{Query: "ops_test"})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Equal(t, "task:t", w.eng.Writer(resFilter))
	// Setting the value already set applies again (ADR-0269 §SD1).
	out = w.submit(t, "c2", opSetFilter, SetFilterArgs{Query: "ops_test"})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Empty(t, w.eng.UndeclaredWrites(0))

	// Typing in the search box commits inside the frame without a command,
	// so its writes are the app's (ADR-0345 §SD3); the last command declared
	// the filter, so the footprint check sees nothing undeclared.
	w.frame(func() { w.inst.filter.Query = "ops" })
	assert.Equal(t, opwire.WriterApp, w.eng.Writer(resFilter))
	assert.Empty(t, w.eng.UndeclaredWrites(0))

	// The Only set checkbox, a row click and a section's toggle are the
	// person's, through the same commands.
	// The checkbox's binding lands at the frame's sync; the next frame
	// notices the draft and routes it.
	w.frame(func() { w.inst.onlySetDraft = true })
	w.frame(func() { w.inst.noticeDrafts() })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resFilter))
	log := w.eng.Log()
	assert.Equal(t, opSetFilter, log[len(log)-1].Op)
	assert.True(t, w.inst.filter.OnlySet)
	// An agent's set_filter moves the draft too, so no click is invented.
	out = w.submit(t, "c3", opSetFilter, SetFilterArgs{Query: "ops", OnlySet: false})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	seq0 := w.eng.LogSeq()
	w.frame(func() { w.inst.noticeDrafts() })
	assert.Empty(t, w.eng.LogSince(seq0), "a command's write is not re-routed as a gesture")
	assert.Equal(t, "task:t", w.eng.Writer(resFilter))

	w.frame(func() { w.inst.selectVarGesture(testPlain.Spec().Name) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resSelection))
	assert.Equal(t, testPlain.Spec().Name, w.inst.selected)

	w.frame(func() { w.inst.expandCategoryGesture(fixtureCategory, true) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resExpanded))
	assert.Equal(t, fixtureCategory, w.inst.Operations().ResourceValue(resExpanded))

	// The reveal toggle moves no resource at all.
	seq := w.eng.LogSeq()
	w.frame(func() { w.inst.filter.RevealSensitive = true })
	assert.Empty(t, w.eng.LogSince(seq), "revealing to the person is invisible to the catalog")
}
