package trailviewer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// The controls' commands set and never toggle, and a refused value leaves
// the state as it was.
func TestControlCommandsSetAndNeverToggle(t *testing.T) {
	inst, _ := opsFixture(t)
	h := inst.Operations()

	for range 2 {
		require.NoError(t, command(t, inst, opSetStrip, SetStripArgs{By: "status"}))
		assert.Equal(t, StripByStatus, inst.strip)
	}
	assert.NotNil(t, refusal(command(t, inst, opSetStrip, SetStripArgs{By: "colour"})))
	assert.Equal(t, StripByStatus, h.ResourceValue(resStrip))

	for range 2 {
		require.NoError(t, command(t, inst, opSetGrouping, SetGroupingArgs{On: true}))
		assert.True(t, inst.grouped)
	}
	assert.Equal(t, true, h.ResourceValue(resGrouping))
	require.NoError(t, command(t, inst, opSetGrouping, SetGroupingArgs{On: false}))
	assert.False(t, inst.grouped)

	for range 2 {
		require.NoError(t, command(t, inst, opSetCompare, SetCompareArgs{Id: 4}))
		assert.Equal(t, uint64(4), inst.compareId())
	}
	assert.Equal(t, rowGoneReason, refusal(command(t, inst, opSetCompare, SetCompareArgs{Id: 99})).Reason)
	assert.Equal(t, uint64(4), h.ResourceValue(resCompare), "a refused id leaves the comparison")
	require.NoError(t, command(t, inst, opSetCompare, SetCompareArgs{}))
	assert.Equal(t, uint64(0), inst.compareId())

	for range 2 {
		require.NoError(t, command(t, inst, opSetMatrix, SetMatrixArgs{Actor: "principal", Object: "entity"}))
		assert.Equal(t, matrixPick{Actor: ActorPrincipal, Object: ObjectEntity}, inst.an.pair)
	}
	assert.NotNil(t, refusal(command(t, inst, opSetMatrix, SetMatrixArgs{Actor: "principal", Object: "colour"})))
	assert.NotNil(t, refusal(command(t, inst, opSetMatrix, SetMatrixArgs{Actor: "", Object: "entity"})))
	assert.Equal(t, matrixPick{Actor: ActorPrincipal, Object: ObjectEntity}, h.ResourceValue(resMatrix))

	for range 2 {
		require.NoError(t, command(t, inst, opSetDotted, SetDottedArgs{Row: "entity"}))
		assert.Equal(t, DottedEntity, inst.an.row)
	}
	assert.NotNil(t, refusal(command(t, inst, opSetDotted, SetDottedArgs{Row: "task"})))
	assert.Equal(t, DottedEntity, h.ResourceValue(resDotted))

	for range 2 {
		require.NoError(t, command(t, inst, opSetSide, SetSideArgs{Pane: "rules"}))
		assert.Equal(t, sideRules, inst.side)
	}
	assert.NotNil(t, refusal(command(t, inst, opSetSide, SetSideArgs{Pane: "marks"})))
	assert.Equal(t, sideRules, h.ResourceValue(resSide))

	for range 2 {
		require.NoError(t, command(t, inst, opSetTree, SetTreeArgs{Expanded: false}))
		assert.False(t, inst.treeV.expanded)
		assert.False(t, inst.treeV.state.IsExpanded(0))
	}
	require.NoError(t, command(t, inst, opSetTree, SetTreeArgs{Expanded: true}))
	assert.Equal(t, true, h.ResourceValue(resTree))
	assert.True(t, inst.treeV.state.IsExpanded(0))

	assert.False(t, inst.cache.refreshNow)
	require.NoError(t, command(t, inst, opRefresh, struct{}{}))
	assert.True(t, inst.cache.refreshNow)
	assert.Equal(t, readValue{Refreshed: inst.cache.refreshedAt}, h.ResourceValue(resRead))

	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "status", v.StripBy)
	assert.False(t, v.Grouping)
	assert.Equal(t, uint64(0), v.Compare)
	assert.Equal(t, "principal × entity", v.Matrix)
	assert.Equal(t, "entity", v.Dotted)
	assert.Equal(t, "rules", v.Side)
	assert.True(t, v.Tree)

	// Undo restores each resource.
	assert.True(t, h.Restore(resStrip, StripByKind))
	assert.Equal(t, StripByKind, inst.strip)
	assert.True(t, h.Restore(resGrouping, true))
	assert.True(t, inst.grouped)
	assert.True(t, h.Restore(resCompare, uint64(3)))
	assert.Equal(t, uint64(3), inst.compareId())
	assert.False(t, h.Restore(resCompare, uint64(99)))
	assert.True(t, h.Restore(resMatrix, matrixPick{Actor: ActorApp, Object: ObjectOperation}))
	assert.True(t, h.Restore(resDotted, DottedRun))
	assert.True(t, h.Restore(resSide, sideFacets))
	assert.True(t, h.Restore(resTree, false))
	assert.False(t, inst.treeV.expanded)
}

// Every control routes through its command: the engine attributes the
// gesture to the person on the resource the command writes.
func TestTheControlsRouteThroughTheirCommands(t *testing.T) {
	inst, _ := opsFixture(t)
	st := &fakeStorage{}
	inst.views.store, inst.views.loaded, inst.views.asked = st, true, true
	inst.views.list = []SavedView{{Name: "saved", Case: "app", Value: "apps/a", Last: "2h", Tab: "context"}}
	w := newEngineWindow(inst)
	person := func(res string) {
		t.Helper()
		assert.Equal(t, opwire.WriterPerson, w.eng.Writer(res), res)
	}

	w.frame(func() { w.inst.setStripGesture(StripByStatus) })
	person(resStrip)
	assert.Equal(t, StripByStatus, inst.strip)
	w.frame(func() { w.inst.setGroupedGesture(true) })
	person(resGrouping)
	assert.True(t, inst.grouped)
	w.frame(func() { w.inst.setCompareGesture(4) })
	person(resCompare)
	assert.Equal(t, uint64(4), inst.compareId())
	w.frame(func() { w.inst.setMatrixGesture(matrixPick{Actor: ActorWindow, Object: ObjectSubject}) })
	person(resMatrix)
	w.frame(func() { w.inst.setDottedGesture(DottedConversation) })
	person(resDotted)
	w.frame(func() { w.inst.setSideGesture(sideViews) })
	person(resSide)
	w.frame(func() { w.inst.setTreeGesture(true) })
	person(resTree)
	assert.True(t, inst.treeV.expanded)
	w.frame(func() { w.inst.refreshGesture() })
	assert.True(t, inst.cache.refreshNow)
	log := w.eng.Log()
	assert.Equal(t, opRefresh, log[len(log)-1].Op)
	assert.Equal(t, opwire.WriterPerson, log[len(log)-1].Writer)

	w.frame(func() { w.inst.openViewGesture("saved") })
	person(resCase)
	person(resRange)
	person(resTab)
	assert.Equal(t, Case{Kind: CaseApp, Value: "apps/a"}, inst.caseValue)
	assert.Equal(t, Window{Last: 2 * time.Hour}, inst.window)
	assert.Equal(t, TabContext, inst.tab)

	w.frame(func() { w.inst.saveViewGesture("new") })
	person(resViews)
	require.Eventually(t, func() bool { inst.tickViews(); return !inst.views.runner.Running() }, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{"saved", "new"}, inst.views.names())
	w.frame(func() { w.inst.deleteViewGesture("saved") })
	person(resViews)
	require.Eventually(t, func() bool { inst.tickViews(); return !inst.views.runner.Running() }, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{"new"}, inst.views.names())

	// The export refuses without a bus, and the refusal reaches the line
	// beside the button through either path.
	w.frame(func() { w.inst.sendToPlayGesture() })
	assert.Contains(t, inst.send.note, "no bus")
	log = w.eng.Log()
	assert.Equal(t, opSendToPlay, log[len(log)-1].Op)
	assert.Equal(t, opwire.PhaseRefused, log[len(log)-1].Phase)
	assert.Empty(t, w.eng.UndeclaredWrites(0))
}

// get_facets and get_strip answer from the table's caches for the read the
// window made, say when the arguments name another, and are bounded.
func TestGetFacetsAndGetStripAnswerFromTheCache(t *testing.T) {
	inst, _ := opsFixture(t)
	inst.cache.facets = make([][]FacetValue, len(AllFacets))
	inst.cache.facets[FacetKind] = []FacetValue{{Value: kindAuditEvent, N: 2}, {Value: "llmCall", N: 1}, {Value: "", N: 1}}
	inst.cache.step = time.Minute
	inst.cache.buckets = []Bucket{{Bucket: t0.Unix(), Key: kindAuditEvent, N: 2}, {Bucket: t0.Unix(), Key: "llmCall", N: 1},
		{Bucket: t0.Unix() + 60, Key: "httpFetch", N: 3}}
	rng := RangeArgs{From: inst.window.From, To: inst.window.To}

	out, err := query[GetFacetsArgs, FacetsResult](t, inst, opGetFacets, GetFacetsArgs{Case: "run", Value: fixtureRun, Range: rng, Facet: "kind"})
	require.NoError(t, err)
	assert.Empty(t, out.Diagnostic)
	assert.Equal(t, "kind", out.Facet)
	assert.Equal(t, 3, out.Count)
	assert.Equal(t, []FacetValueOut{{Value: kindAuditEvent, Rows: 2}, {Value: "llmCall", Rows: 1}, {Value: "", Rows: 1}}, out.Values)
	assert.False(t, out.Cut)
	out, err = query[GetFacetsArgs, FacetsResult](t, inst, opGetFacets, GetFacetsArgs{Case: "run", Value: fixtureRun, Range: rng, Facet: "kind", Limit: 1})
	require.NoError(t, err)
	assert.Len(t, out.Values, 1)
	assert.True(t, out.Cut)
	assert.Equal(t, 3, out.Count)
	out, err = query[GetFacetsArgs, FacetsResult](t, inst, opGetFacets, GetFacetsArgs{Case: "run", Value: fixtureRun, Range: rng, Facet: "principal"})
	require.NoError(t, err)
	assert.Empty(t, out.Values, "a facet with no values is empty, not a diagnostic")
	assert.Empty(t, out.Diagnostic)
	other, err := query[GetFacetsArgs, FacetsResult](t, inst, opGetFacets, GetFacetsArgs{Case: "app", Value: "apps/a", Range: rng, Facet: "kind"})
	require.NoError(t, err)
	assert.Empty(t, other.Values)
	assert.Contains(t, other.Diagnostic, "set_case")
	narrow, err := query[GetFacetsArgs, FacetsResult](t, inst, opGetFacets,
		GetFacetsArgs{Case: "run", Value: fixtureRun, Range: RangeArgs{From: inst.window.From, To: inst.window.To.Add(-time.Minute)}, Facet: "kind"})
	require.NoError(t, err)
	assert.Contains(t, narrow.Diagnostic, "set_range")
	for _, bad := range []GetFacetsArgs{{Case: "run", Value: fixtureRun, Range: rng, Facet: "size"}, {Case: "run", Value: fixtureRun, Range: rng, Facet: "kind", Limit: -1}} {
		_, err = query[GetFacetsArgs, FacetsResult](t, inst, opGetFacets, bad)
		assert.NotNil(t, refusal(err), "%+v", bad)
	}

	sargs := GetStripArgs{Case: "run", Value: fixtureRun, Range: rng}
	sout, err := query[GetStripArgs, StripResult](t, inst, opGetStrip, sargs)
	require.NoError(t, err)
	assert.Empty(t, sout.Diagnostic)
	assert.Equal(t, "kind", sout.By)
	assert.Equal(t, int64(60000), sout.StepMs)
	assert.Equal(t, uint64(6), sout.Rows)
	assert.Equal(t, 2, sout.Buckets)
	assert.Equal(t, stripLine(inst.cache.buckets, time.Minute), sout.Line)
	assert.Equal(t, []StripBar{{Start: t0, Key: kindAuditEvent, Rows: 2}, {Start: t0, Key: "llmCall", Rows: 1}, {Start: t0.Add(time.Minute), Key: "httpFetch", Rows: 3}}, sout.Bars)
	sargs.Limit = 2
	sout, err = query[GetStripArgs, StripResult](t, inst, opGetStrip, sargs)
	require.NoError(t, err)
	assert.Len(t, sout.Bars, 2)
	assert.True(t, sout.Cut)
	assert.Equal(t, uint64(6), sout.Rows, "the totals are over the whole strip")
	sargs.Limit, sargs.By = 0, "status"
	sout, err = query[GetStripArgs, StripResult](t, inst, opGetStrip, sargs)
	require.NoError(t, err)
	assert.Contains(t, sout.Diagnostic, "set_strip reads it by status")
	assert.Empty(t, sout.Bars)
	sargs.By = "colour"
	_, err = query[GetStripArgs, StripResult](t, inst, opGetStrip, sargs)
	assert.NotNil(t, refusal(err))
	filtered, err := query[GetStripArgs, StripResult](t, inst, opGetStrip,
		GetStripArgs{Case: "run", Value: fixtureRun, Range: rng, Filter: []TermArg{{Facet: "kind", Value: "llmCall"}}})
	require.NoError(t, err)
	assert.Contains(t, filtered.Diagnostic, "set_filter")
}

// get_matrix and get_dotted answer from the view's last read for the read
// the window made and the picks it has, as get_dayhour does.
func TestGetMatrixAndGetDottedAnswerFromTheCache(t *testing.T) {
	inst, _ := opsFixture(t)
	inst.cache.step = time.Minute
	rng := RangeArgs{From: inst.window.From, To: inst.window.To}

	margs := GetMatrixArgs{Case: "run", Value: fixtureRun, Range: rng}
	mout, err := query[GetMatrixArgs, MatrixResult](t, inst, opGetMatrix, margs)
	require.NoError(t, err)
	assert.Contains(t, mout.Diagnostic, "set_tab matrix")
	assert.Equal(t, "app", mout.Actor)
	assert.Equal(t, "operation", mout.Object)
	mkey := inst.matrixKey()
	inst.an.matrix.data = pivotMatrix([]MatrixCell{{Actor: "apps/a", Object: "attention.level", N: 3, Last: "denied"},
		{Actor: "apps/b", Object: "apps/b.set_note", N: 2, Last: "rendered"}, {Actor: "apps/a", Object: "trail.config", N: 1, Last: "ok"}},
		ActorApp, ObjectOperation, matrixShown, false)
	inst.an.matrix.has, inst.an.matrix.key, inst.an.matrix.asked = true, mkey, mkey
	mout, err = query[GetMatrixArgs, MatrixResult](t, inst, opGetMatrix, margs)
	require.NoError(t, err)
	assert.Empty(t, mout.Diagnostic)
	assert.Equal(t, 2, mout.Actors)
	assert.Equal(t, 3, mout.Objects)
	assert.Equal(t, uint64(6), mout.Rows)
	assert.Equal(t, 3, mout.Pairs)
	assert.Equal(t, "apps/a", mout.BusiestActor)
	assert.Equal(t, "attention.level", mout.BusiestObject)
	assert.Equal(t, uint64(3), mout.BusiestRows)
	assert.Equal(t, inst.an.matrix.data.line(), mout.Line)
	assert.Equal(t, []MatrixCellOut{{Actor: "apps/a", Object: "attention.level", Rows: 3, LastOutcome: "denied"},
		{Actor: "apps/a", Object: "trail.config", Rows: 1, LastOutcome: "ok"},
		{Actor: "apps/b", Object: "apps/b.set_note", Rows: 2, LastOutcome: "rendered"}}, mout.Cells)
	margs.Limit = 2
	mout, err = query[GetMatrixArgs, MatrixResult](t, inst, opGetMatrix, margs)
	require.NoError(t, err)
	assert.Len(t, mout.Cells, 2, "bounded")
	assert.True(t, mout.Cut)
	assert.Equal(t, 3, mout.Pairs)
	margs.Limit, margs.Actor = 0, "principal"
	mout, err = query[GetMatrixArgs, MatrixResult](t, inst, opGetMatrix, margs)
	require.NoError(t, err)
	assert.Contains(t, mout.Diagnostic, "set_matrix reads principal × operation")
	assert.Empty(t, mout.Cells)
	margs.Actor = "robot"
	_, err = query[GetMatrixArgs, MatrixResult](t, inst, opGetMatrix, margs)
	assert.NotNil(t, refusal(err))
	other, err := query[GetMatrixArgs, MatrixResult](t, inst, opGetMatrix, GetMatrixArgs{Case: "run", Value: "other", Range: rng})
	require.NoError(t, err)
	assert.Contains(t, other.Diagnostic, "set_case")
	assert.Empty(t, other.Cells)
	inst.cache.gen++
	stale, err := query[GetMatrixArgs, MatrixResult](t, inst, opGetMatrix, GetMatrixArgs{Case: "run", Value: fixtureRun, Range: rng})
	require.NoError(t, err)
	assert.Contains(t, stale.Diagnostic, "ask again shortly")
	assert.Len(t, stale.Cells, 3)
	inst.cache.gen--

	dargs := GetDottedArgs{Case: "run", Value: fixtureRun, Range: rng}
	dout, err := query[GetDottedArgs, DottedResult](t, inst, opGetDotted, dargs)
	require.NoError(t, err)
	assert.Contains(t, dout.Diagnostic, "set_tab dotted")
	assert.Equal(t, "run", dout.Row)
	dkey := inst.dottedKey()
	inst.an.dotted.data = buildDotted([]Bucket{{Key: "run-a", Bucket: t0.Unix(), N: 4}, {Key: "run-a", Bucket: t0.Unix() + 60, N: 1},
		{Key: "run-b", Bucket: t0.Unix(), N: 2}}, DottedRun, time.Minute, dottedShown, false)
	inst.an.dotted.has, inst.an.dotted.key, inst.an.dotted.asked = true, dkey, dkey
	dout, err = query[GetDottedArgs, DottedResult](t, inst, opGetDotted, dargs)
	require.NoError(t, err)
	assert.Empty(t, dout.Diagnostic)
	assert.Equal(t, int64(60000), dout.StepMs)
	assert.Equal(t, 2, dout.Keys)
	assert.Equal(t, 2, dout.Buckets)
	assert.Equal(t, uint64(7), dout.Rows)
	assert.Equal(t, "run-a", dout.BusiestKey)
	assert.Equal(t, uint64(5), dout.BusiestRows)
	assert.Equal(t, uint64(4), dout.Largest)
	assert.Equal(t, inst.an.dotted.data.line(), dout.Line)
	assert.Equal(t, []DottedMark{{Key: "run-a", Start: t0, Rows: 4}, {Key: "run-a", Start: t0.Add(time.Minute), Rows: 1}, {Key: "run-b", Start: t0, Rows: 2}}, dout.Marks)
	dargs.Limit = 1
	dout, err = query[GetDottedArgs, DottedResult](t, inst, opGetDotted, dargs)
	require.NoError(t, err)
	assert.Len(t, dout.Marks, 1, "bounded")
	assert.True(t, dout.Cut)
	dargs.Limit, dargs.Row = 0, "entity"
	dout, err = query[GetDottedArgs, DottedResult](t, inst, opGetDotted, dargs)
	require.NoError(t, err)
	assert.Contains(t, dout.Diagnostic, "set_dotted reads a row per entity")
	assert.Empty(t, dout.Marks)
	dargs.Row = "task"
	_, err = query[GetDottedArgs, DottedResult](t, inst, opGetDotted, dargs)
	assert.NotNil(t, refusal(err))
	filtered, err := query[GetDottedArgs, DottedResult](t, inst, opGetDotted,
		GetDottedArgs{Case: "run", Value: fixtureRun, Range: rng, Filter: []TermArg{{Facet: "kind", Value: "llmCall"}}})
	require.NoError(t, err)
	assert.Contains(t, filtered.Diagnostic, "set_filter")
}

// get_context answers for the selected row, whose read the window made;
// get_compare diffs two held rows and refuses an unheld one.
func TestGetContextAndGetCompare(t *testing.T) {
	inst, _ := opsFixture(t)
	_, err := query[GetContextArgs, ContextResult](t, inst, opGetContext, GetContextArgs{Id: 4})
	assert.Contains(t, refusal(err).Reason, "select_row it first")
	require.NoError(t, command(t, inst, opSelectRow, SelectRowArgs{Id: 4}))
	_, err = query[GetContextArgs, ContextResult](t, inst, opGetContext, GetContextArgs{Id: 4})
	assert.Contains(t, refusal(err).Reason, "not been read yet")
	_, err = query[GetContextArgs, ContextResult](t, inst, opGetContext, GetContextArgs{Id: 3})
	assert.Contains(t, refusal(err).Reason, "select_row it first", "another row's context is not the selected row's")
	before := []Row{{Id: 10, TsNs: t0.Add(28 * time.Minute).UnixNano(), Kind: "httpFetch", Run: fixtureRun, Headline: "older"},
		{Id: 3, TsNs: t0.Add(20 * time.Minute).UnixNano(), Kind: "llmCall", Run: fixtureRun, Headline: "model call"}}
	after := []Row{{Id: 5, TsNs: t0.Add(40 * time.Minute).UnixNano(), Kind: kindAuditEvent, Run: fixtureRun, Headline: "trail config -> ok"}}
	inst.detail.fill, inst.detail.id, inst.detail.has = &detailFill{id: 4, before: before, after: after}, 4, true
	out, err := query[GetContextArgs, ContextResult](t, inst, opGetContext, GetContextArgs{Id: 4})
	require.NoError(t, err)
	assert.Equal(t, uint64(4), out.Id)
	assert.Equal(t, fixtureRun, out.Run)
	require.Len(t, out.Before, 2)
	assert.Equal(t, uint64(10), out.Before[0].Id, "oldest first")
	assert.Equal(t, "holds", out.Before[0].Verdict)
	require.Len(t, out.After, 1)
	assert.Equal(t, uint64(5), out.After[0].Id)
	assert.False(t, out.Cut)
	out, err = query[GetContextArgs, ContextResult](t, inst, opGetContext, GetContextArgs{Id: 4, N: 1})
	require.NoError(t, err)
	require.Len(t, out.Before, 1)
	assert.Equal(t, uint64(3), out.Before[0].Id, "the nearest row is kept")
	assert.True(t, out.Cut)
	_, err = query[GetContextArgs, ContextResult](t, inst, opGetContext, GetContextArgs{Id: 4, N: -1})
	assert.NotNil(t, refusal(err))

	cmp, err := query[GetCompareArgs, CompareResult](t, inst, opGetCompare, GetCompareArgs{A: 5, B: 6})
	require.NoError(t, err)
	assert.Equal(t, uint64(5), cmp.OlderId)
	assert.Equal(t, uint64(6), cmp.NewerId)
	assert.True(t, cmp.BothAudit)
	assert.Contains(t, cmp.Base, "id 5")
	fields := map[string]FieldDiff{}
	for _, f := range cmp.Fields {
		fields[f.Field] = f
	}
	assert.Equal(t, FieldDiff{Field: "domain", Before: "trail", After: "attention", Change: "changed"}, fields["domain"])
	assert.Equal(t, FieldDiff{Field: "entity", After: "attention-level:k", Change: "added"}, fields["entity"])
	same, err := query[GetCompareArgs, CompareResult](t, inst, opGetCompare, GetCompareArgs{A: 6, B: 5})
	require.NoError(t, err)
	assert.Equal(t, cmp, same, "the order of a and b does not matter: the older row is before")
	for _, bad := range []GetCompareArgs{{A: 5, B: 99}, {A: 99, B: 5}} {
		_, err = query[GetCompareArgs, CompareResult](t, inst, opGetCompare, bad)
		assert.Equal(t, rowGoneReason, refusal(err).Reason)
	}
	for _, bad := range []GetCompareArgs{{A: 5, B: 5}, {A: 5}, {}} {
		_, err = query[GetCompareArgs, CompareResult](t, inst, opGetCompare, bad)
		assert.NotNil(t, refusal(err), "%+v", bad)
	}
}

// The saved views' commands: save_view then get_view lists the name,
// open_view sets the case, the range, the filter and the tab, and
// delete_view removes it.
func TestSavedViewCommands(t *testing.T) {
	st := &fakeStorage{}
	inst, _ := opsFixture(t)
	inst.views.store = st
	settle := func() {
		t.Helper()
		require.Eventually(t, func() bool { inst.tickViews(); return inst.views.loaded && !inst.views.runner.Running() }, 5*time.Second, 5*time.Millisecond)
	}
	assert.NotNil(t, refusal(command(t, inst, opSaveView, ViewNameArgs{Name: "early"})), "refused until the list is read")
	settle()
	assert.NotNil(t, refusal(command(t, inst, opSaveView, ViewNameArgs{Name: "  "})))
	assert.Equal(t, noSuchViewReason, refusal(command(t, inst, opOpenView, ViewNameArgs{Name: "none"})).Reason)
	assert.Equal(t, noSuchViewReason, refusal(command(t, inst, opDeleteView, ViewNameArgs{Name: "none"})).Reason)

	require.NoError(t, command(t, inst, opSetCase, SetCaseArgs{Case: "task", Value: "task-1"}))
	require.NoError(t, command(t, inst, opSetRange, RangeArgs{Last: "6h"}))
	require.NoError(t, command(t, inst, opSetFilter, SetFilterArgs{Terms: []TermArg{{Facet: "kind", Value: "agentAction"}}}))
	require.NoError(t, command(t, inst, opSetTab, SetTabArgs{Tab: "tree"}))
	require.NoError(t, command(t, inst, opSaveView, ViewNameArgs{Name: " my task "}))
	assert.Equal(t, "my task", inst.Operations().ResourceValue(resViews), "the write is in flight and the resource names it")
	assert.NotNil(t, refusal(command(t, inst, opSaveView, ViewNameArgs{Name: "second"})), "one write at a time")
	settle()
	require.NoError(t, inst.views.err)
	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, []string{"my task"}, v.SavedViews)
	stored, err := loadViews(st)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "tree", stored[0].Tab)

	require.NoError(t, command(t, inst, opSetCase, SetCaseArgs{Case: "all"}))
	require.NoError(t, command(t, inst, opSetRange, RangeArgs{Last: "5m"}))
	require.NoError(t, command(t, inst, opSetFilter, SetFilterArgs{}))
	assert.Equal(t, TabFields, inst.tab, "the tree tab left with the task case")
	for range 2 {
		require.NoError(t, command(t, inst, opOpenView, ViewNameArgs{Name: "my task"}))
		assert.Equal(t, Case{Kind: CaseTask, Value: "task-1"}, inst.caseValue)
		assert.Equal(t, Window{Last: 6 * time.Hour}, inst.window)
		assert.Equal(t, "kind = agentAction", inst.filter.String())
		assert.Equal(t, TabTree, inst.tab)
	}

	require.NoError(t, command(t, inst, opDeleteView, ViewNameArgs{Name: "my task"}))
	settle()
	require.NoError(t, inst.views.err)
	assert.Empty(t, inst.views.names())
	stored, _ = loadViews(st)
	assert.Empty(t, stored)
	assert.Equal(t, "", inst.Operations().ResourceValue(resViews))
}

// send_to_play writes outside the app: consequential, offered to agents
// under the person's confirmation, and refused when there is nothing to
// send or no bus to send over.
func TestSendToPlayIsConsequentialAndRefusesWithoutRows(t *testing.T) {
	spec, ok := manifest.Operations.Lookup(opSendToPlay)
	require.True(t, ok)
	assert.Equal(t, app.OperationEffectConsequential, spec.Effect)
	assert.True(t, spec.Agents)
	assert.Equal(t, app.OperationConsent{}, spec.Consent, "confirmed every time: no standing consent")
	inst, _ := opsFixture(t)
	assert.Contains(t, refusal(command(t, inst, opSendToPlay, struct{}{})).Reason, "no bus")
}
