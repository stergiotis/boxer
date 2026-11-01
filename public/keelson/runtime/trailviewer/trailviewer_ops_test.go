package trailviewer

import (
	"context"
	"errors"
	"iter"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/keelson/runtime/trailviewer/verdict"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

const fixtureRun = "run-now"

// fakeExec records the statements and the call identity they carried, and
// answers every query with no rows.
type fakeExec struct {
	mu    sync.Mutex
	sqls  []string
	ident []callident.CallIdentity
}

func (inst *fakeExec) Exec(context.Context, string) error { return nil }
func (inst *fakeExec) InsertArrow(context.Context, string, []arrow.RecordBatch) error {
	return errors.New("the viewer never writes")
}
func (inst *fakeExec) QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
	inst.mu.Lock()
	inst.sqls = append(inst.sqls, sql)
	ci, _ := callident.CallIdentityFrom(ctx)
	inst.ident = append(inst.ident, ci)
	inst.mu.Unlock()
	return func(func(arrow.RecordBatch, error) bool) {}
}

func (inst *fakeExec) statements() (out []string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append(out, inst.sqls...)
}

var _ recordstore.ExecutorI = (*fakeExec)(nil)

// fakeTrail verifies every run as holding and drops what it is asked to
// write.
type fakeTrail struct{}

func (fakeTrail) VerifySeals(context.Context, string) (evidence.SealBreak, []uint64, []uint64, error) {
	return evidence.SealBreak{}, nil, nil, nil
}
func (fakeTrail) NotDurable() string { return "" }
func (fakeTrail) Event(context.Context, time.Time, trail.Context, trail.AuditEvent) error {
	return nil
}
func (fakeTrail) OriginOf(appId app.AppIdT, instance uint64) trail.Origin {
	return trail.Origin{Run: fixtureRun, App: string(appId), Instance: instance}
}
func (fakeTrail) FlushSoon() {}

// fixtureRows are six rows of the current run, newest first: two audit
// events, a model call and its action, an egress fetch and a grant.
func fixtureRows() []Row {
	ts := func(m int) int64 { return t0.Add(time.Duration(m) * time.Minute).UnixNano() }
	return []Row{
		{Id: 6, TsNs: ts(50), Kind: kindAuditEvent, Run: fixtureRun, App: "apps/a", Status: "denied", Subject: "attention.level", Headline: "attention level -> denied",
			Audit: &AuditDetail{Id: 6, Domain: "attention", Action: "level", Outcome: "denied", Principal: "p-1", Entity: "attention-level:k",
				AttrKeys: []string{"level", "from"}, AttrValues: []string{"deny", "ask"}, From: "ask"}},
		{Id: 5, TsNs: ts(40), Kind: kindAuditEvent, Run: fixtureRun, App: "apps/a", Status: "ok", Subject: "trail.config", Headline: "trail config -> ok",
			Audit: &AuditDetail{Id: 5, Domain: "trail", Action: "config", Outcome: "ok", Principal: "p-2"}},
		{Id: 4, TsNs: ts(30), Kind: "agentAction", Run: fixtureRun, App: "apps/b", Status: "rendered", Subject: "apps/b.set_note", Task: "task-1", ParentRef: "call-1",
			Headline: "set_note on apps/b"},
		{Id: 3, TsNs: ts(20), Kind: "llmCall", Run: fixtureRun, App: "apps/b", Status: "tool_calls", Subject: "chat/turn", Ref: "call-1", Headline: "model call"},
		{Id: 2, TsNs: ts(10), Kind: "httpFetch", Run: fixtureRun, App: "apps/c", Status: "200", Subject: "tiles.example.org", Headline: "GET tiles"},
		{Id: 1, TsNs: ts(0), Kind: kindAgentGrant, Run: fixtureRun, App: "apps/b", Status: "approved", Subject: "approved", Task: "task-1", Headline: "grant approved"},
	}
}

// opsFixture registers a source over a fake executor and a window holding
// the fixture rows as a fill of the last two hours of the run would.
func opsFixture(t *testing.T) (inst *App, exec *fakeExec) {
	t.Helper()
	exec = &fakeExec{}
	prev := sourceRef.Load()
	Register(&Source{Exec: exec, Trail: fakeTrail{}, Run: func() string { return fixtureRun }})
	t.Cleanup(func() { Register(prev) })
	inst = newInstance(manifest)
	inst.ensureInit()
	inst.applyWindow(Window{From: t0.Add(-time.Hour), To: t0.Add(time.Hour)})
	hold(inst, fixtureRows())
	return
}

// hold puts rows in the cache as the fill for the window's scope, in the
// order a read has them: the window resolved when the read starts, the
// read finishing a moment later.
func hold(inst *App, rows []Row) {
	ch := &inst.cache
	ch.resolvedAt = time.Now()
	ch.from, ch.to = inst.window.Resolve(ch.resolvedAt)
	ch.rows, ch.scope, ch.filled, ch.refreshedAt = rows, inst.scopeValue(), true, ch.resolvedAt.Add(250*time.Millisecond)
	ch.gen++
	inst.verdicts.lines = []verdict.Line{{Run: fixtureRun, State: verdict.StateHolds}}
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

// allOps is every operation agents are offered; mark_row is the one they
// are not (review_test.go).
var allOps = []string{opListRows, opGetRow, opGetPrevious, opGetVerdict, opGetView, opGetExc, opGetTree, opGetDayHour, opGetSeries,
	opGetFacets, opGetStrip, opGetContext, opGetCompare, opGetMatrix, opGetDotted,
	opSetCase, opSetRange, opSetFilter, opSelectRow, opSetTab, opSetSeries, opSetStrip, opSetGrouping, opSetCompare, opSetMatrix, opSetDotted,
	opSetSide, opSetTree, opRefresh, opOpenView, opSaveView, opDeleteView, opSendToPlay}

func TestOperationsCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	names := make([]string, 0, len(m.Operations.Operations))
	for _, o := range m.Operations.Operations {
		names = append(names, o.Name)
	}
	assert.ElementsMatch(t, append(slices.Clone(allOps), opMarkRow), names, "allOps lists every operation")
	for _, name := range allOps {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
		if spec.Class == app.OperationClassCommand {
			switch name {
			case opSaveView, opDeleteView:
				assert.Equal(t, app.OperationEffectDocument, spec.Effect, name)
			case opSendToPlay:
				assert.Equal(t, app.OperationEffectConsequential, spec.Effect, name)
			default:
				assert.Equal(t, app.OperationEffectView, spec.Effect, name)
			}
			assert.NotEmpty(t, spec.Gesture, "%s: every command is a control's", name)
		}
	}
	for _, name := range []string{opListRows, opGetRow, opGetPrevious, opGetView, opGetExc, opGetTree, opGetFacets, opGetStrip, opGetContext, opGetCompare, opGetMatrix, opGetDotted} {
		spec, _ := m.Operations.Lookup(name)
		assert.True(t, spec.Untrusted, "%s returns row text", name)
	}
	assert.ElementsMatch(t, []string{resCase, resRange, resFilter, resSelection, resTab, resSeries, resStrip, resGrouping, resCompare, resMatrix, resDotted,
		resSide, resTree, resRead, resViews}, ops.ResourceNames())
}

func listArgs(inst *App) ListRowsArgs {
	return ListRowsArgs{Case: "run", Value: fixtureRun, Range: RangeArgs{From: inst.window.From, To: inst.window.To}}
}

// list_rows filters by its own arguments, not by the window's filter, and
// is bounded.
func TestListRowsReadsItsOwnArgumentsNotTheView(t *testing.T) {
	inst, _ := opsFixture(t)
	inst.filter = Filter{}.with(FacetKind, "httpFetch", false)

	out, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, listArgs(inst))
	require.NoError(t, err)
	assert.Empty(t, out.Diagnostic)
	assert.Equal(t, 6, out.Matched, "the window's filter is not the query's")
	require.Len(t, out.Rows, 6)
	assert.Equal(t, uint64(6), out.Rows[0].Id, "newest first")
	assert.Equal(t, "holds", out.Rows[0].Verdict)
	assert.True(t, out.Persisted)

	in := listArgs(inst)
	in.Filter = []TermArg{{Facet: "kind", Value: kindAuditEvent}, {Facet: "principal", Value: "p-2", Exclude: true}}
	out, err = query[ListRowsArgs, ListRowsResult](t, inst, opListRows, in)
	require.NoError(t, err)
	require.Len(t, out.Rows, 1)
	assert.Equal(t, uint64(6), out.Rows[0].Id)

	in = listArgs(inst)
	in.Filter = []TermArg{{Facet: "domain.action", Value: "trail.config"}}
	out, err = query[ListRowsArgs, ListRowsResult](t, inst, opListRows, in)
	require.NoError(t, err)
	require.Len(t, out.Rows, 1)
	assert.Equal(t, uint64(5), out.Rows[0].Id)

	// Another case is not what the window read: an empty answer that says so.
	out, err = query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{Case: "app", Value: "apps/a", Range: listArgs(inst).Range})
	require.NoError(t, err)
	assert.Empty(t, out.Rows)
	assert.Contains(t, out.Diagnostic, "set_case")

	for _, bad := range []ListRowsArgs{{Case: "nonsense"}, {Case: "run"}, {Case: "run", Value: "x", Limit: -1},
		{Case: "run", Value: "x", Range: RangeArgs{Last: "1h", From: t0}}, {Case: "run", Value: "x", Filter: []TermArg{{Facet: "size"}}},
		{Case: "run", Value: "x", Cursor: "garbage"}} {
		_, err = query[ListRowsArgs, ListRowsResult](t, inst, opListRows, bad)
		assert.NotNil(t, refusal(err), "%+v", bad)
	}
}

func TestListRowsIsBounded(t *testing.T) {
	inst, _ := opsFixture(t)
	var rows []Row
	for i := range 300 {
		rows = append(rows, Row{Id: uint64(1000 - i), TsNs: t0.Add(-time.Duration(i) * time.Second).UnixNano(), Kind: "httpFetch", Run: fixtureRun,
			Headline: strings.Repeat("x", 2*opsMaxHeadline)})
	}
	hold(inst, rows)
	in := listArgs(inst)
	in.Limit = opsMaxRows + 1
	out, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, in)
	require.NoError(t, err)
	assert.Equal(t, 300, out.Matched)
	assert.Len(t, out.Rows, opsMaxRows)
	assert.Len(t, []rune(out.Rows[0].Headline), opsMaxHeadline+1, "cut, with a mark")
	assert.NotEmpty(t, out.NextCursor)
	in.Limit = 0
	out, err = query[ListRowsArgs, ListRowsResult](t, inst, opListRows, in)
	require.NoError(t, err)
	assert.Len(t, out.Rows, opsDefaultRows)
}

// Following next_cursor walks every matched row once, in order, and ends.
func TestACursorContinuesWithoutRepeatingARow(t *testing.T) {
	inst, _ := opsFixture(t)
	// Two rows share a timestamp: the id orders them.
	rows := fixtureRows()
	rows[3].TsNs = rows[2].TsNs
	hold(inst, rows)
	in := listArgs(inst)
	in.Limit = 2
	var seen []uint64
	for range 10 {
		out, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, in)
		require.NoError(t, err)
		for _, r := range out.Rows {
			seen = append(seen, r.Id)
		}
		if out.NextCursor == "" {
			break
		}
		in.Cursor = out.NextCursor
	}
	assert.Equal(t, []uint64{6, 5, 4, 3, 2, 1}, seen)
}

// A question's last window resolves at the instant the window's own did,
// not at refreshedAt, which lies past the window's end: a shorter last
// inside a last window is covered.
func TestALastQueryInsideALastWindowIsCovered(t *testing.T) {
	inst, _ := opsFixture(t)
	inst.applyWindow(Window{Last: 2 * time.Hour})
	now := time.Now()
	hold(inst, []Row{
		{Id: 2, TsNs: now.Add(-10 * time.Minute).UnixNano(), Kind: "httpFetch", Run: fixtureRun},
		{Id: 1, TsNs: now.Add(-90 * time.Minute).UnixNano(), Kind: "httpFetch", Run: fixtureRun},
	})
	require.True(t, inst.cache.refreshedAt.After(inst.cache.to), "the fixture finishes its read after the window's end, as a read does")

	out, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{Case: "run", Value: fixtureRun, Range: RangeArgs{Last: "1h"}})
	require.NoError(t, err)
	assert.Empty(t, out.Diagnostic)
	require.Len(t, out.Rows, 1)
	assert.Equal(t, uint64(2), out.Rows[0].Id)

	ex, err := query[GetExceptionsArgs, ExceptionsResult](t, inst, opGetExc, GetExceptionsArgs{Case: "run", Value: fixtureRun, Range: RangeArgs{Last: "30m"}})
	require.NoError(t, err)
	assert.Empty(t, ex.Diagnostic)

	out, err = query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{Case: "run", Value: fixtureRun, Range: RangeArgs{Last: "3h"}})
	require.NoError(t, err)
	assert.Contains(t, out.Diagnostic, "set_range", "a longer last reaches outside the window")
}

// A per-row read that failed is read again one refresh interval later
// while its row stays selected, not never.
func TestAFailedRowReadIsRetried(t *testing.T) {
	var dc detailCache
	defer dc.close()
	src := &Source{Exec: &fakeExec{}}
	var calls atomic.Int32
	failing := func() (*probe, error) {
		calls.Add(1)
		return nil, errors.New("the store is away")
	}
	sel := &fixtureRows()[0]
	now := t0
	settle := func() {
		require.Eventually(t, func() bool { return !dc.runner.Running() }, 5*time.Second, time.Millisecond)
	}
	dc.tick(now, src, sel, failing)
	settle()
	dc.tick(now, src, sel, failing)
	require.NotNil(t, dc.current(sel.Id))
	require.Error(t, dc.current(sel.Id).err)
	assert.Equal(t, int32(1), calls.Load())

	dc.tick(now.Add(refreshInterval()/2), src, sel, failing)
	assert.False(t, dc.runner.Running(), "not before the interval")
	assert.Equal(t, int32(1), calls.Load())

	dc.tick(now.Add(refreshInterval()), src, sel, failing)
	settle()
	assert.Equal(t, int32(2), calls.Load(), "read again")
	require.Error(t, dc.current(sel.Id).err, "the error stays shown until a read succeeds")
}

func TestGetRowAndGetPrevious(t *testing.T) {
	inst, _ := opsFixture(t)
	d, err := query[GetRowArgs, RowDetail](t, inst, opGetRow, GetRowArgs{Id: 6})
	require.NoError(t, err)
	assert.Equal(t, "attention", d.Domain)
	assert.Equal(t, "ask", d.From)
	assert.Equal(t, []Attr{{Key: "level", Value: "deny"}, {Key: "from", Value: "ask"}}, d.Attrs)
	assert.Equal(t, "person p-1 › window a", d.Breadcrumb)
	_, err = query[GetRowArgs, RowDetail](t, inst, opGetRow, GetRowArgs{Id: 99})
	assert.Equal(t, rowGoneReason, refusal(err).Reason)

	_, err = query[GetPreviousArgs, PreviousResult](t, inst, opGetPrevious, GetPreviousArgs{Id: 6})
	assert.Contains(t, refusal(err).Reason, "select_row it first")
	require.NoError(t, command(t, inst, opSelectRow, SelectRowArgs{Id: 6}))
	_, err = query[GetPreviousArgs, PreviousResult](t, inst, opGetPrevious, GetPreviousArgs{Id: 6})
	assert.Contains(t, refusal(err).Reason, "not been read yet")
	prev := judgeAudit(inst.selected.Audit, nil, nil, time.Hour)
	inst.detail.fill, inst.detail.id, inst.detail.has = &detailFill{id: 6, previous: &prev}, 6, true
	p, err := query[GetPreviousArgs, PreviousResult](t, inst, opGetPrevious, GetPreviousArgs{Id: 6})
	require.NoError(t, err)
	assert.Equal(t, "no-previous", p.Claim)
	assert.False(t, p.HasPrev)

	v, err := query[GetVerdictArgs, VerdictResult](t, inst, opGetVerdict, GetVerdictArgs{})
	require.NoError(t, err)
	require.Len(t, v.Lines, 1)
	assert.Equal(t, VerdictLine{Run: fixtureRun, State: "holds", Label: "holds", Law: verdict.LawSeals}, v.Lines[0])
	_, err = query[GetVerdictArgs, VerdictResult](t, inst, opGetVerdict, GetVerdictArgs{Run: "other"})
	assert.NotNil(t, refusal(err))
}

func TestViewCommandsSetAndNeverToggle(t *testing.T) {
	inst, _ := opsFixture(t)
	for range 2 {
		require.NoError(t, command(t, inst, opSetCase, SetCaseArgs{Case: "task", Value: "task-1"}))
		assert.Equal(t, Case{Kind: CaseTask, Value: "task-1"}, inst.caseValue)
	}
	assert.NotNil(t, refusal(command(t, inst, opSetCase, SetCaseArgs{Case: "window", Value: "nocolon"})))
	assert.Equal(t, Case{Kind: CaseTask, Value: "task-1"}, inst.caseValue, "a refused command changes nothing")

	for range 2 {
		require.NoError(t, command(t, inst, opSetRange, RangeArgs{Last: "15m"}))
		assert.Equal(t, Window{Last: 15 * time.Minute}, inst.window)
	}
	require.NoError(t, command(t, inst, opSetRange, RangeArgs{From: t0, To: t0.Add(time.Minute)}))
	assert.Equal(t, Window{From: t0, To: t0.Add(time.Minute)}, inst.window)
	assert.NotNil(t, refusal(command(t, inst, opSetRange, RangeArgs{})))
	assert.NotNil(t, refusal(command(t, inst, opSetRange, RangeArgs{From: t0, To: t0})))

	terms := []TermArg{{Facet: "status", Value: "ok", Exclude: true}, {Facet: "kind", Value: "llmCall"}}
	for range 2 {
		require.NoError(t, command(t, inst, opSetFilter, SetFilterArgs{Terms: terms}))
		assert.Equal(t, "kind = llmCall, status ≠ ok", inst.filter.String())
	}
	require.NoError(t, command(t, inst, opSetFilter, SetFilterArgs{}))
	assert.Empty(t, inst.filter.Terms)

	for range 2 {
		require.NoError(t, command(t, inst, opSelectRow, SelectRowArgs{Id: 4}))
		assert.True(t, inst.hasSelected)
		assert.Equal(t, uint64(4), inst.selected.Id)
	}
	assert.Equal(t, uint64(4), inst.Operations().ResourceValue(resSelection))
	assert.Equal(t, rowGoneReason, refusal(command(t, inst, opSelectRow, SelectRowArgs{Id: 99})).Reason)
	assert.Equal(t, uint64(4), inst.selected.Id)
	require.NoError(t, command(t, inst, opSelectRow, SelectRowArgs{}))
	assert.False(t, inst.hasSelected)

	for range 2 {
		require.NoError(t, command(t, inst, opSetTab, SetTabArgs{Tab: "previous"}))
		assert.Equal(t, TabPrevious, inst.tab)
	}
	assert.NotNil(t, refusal(command(t, inst, opSetTab, SetTabArgs{Tab: "sideways"})))
	// The tree tab exists for a chained case, the task here.
	require.NoError(t, command(t, inst, opSetTab, SetTabArgs{Tab: "tree"}))
	assert.Equal(t, TabTree, inst.tab)
	require.NoError(t, command(t, inst, opSetTab, SetTabArgs{Tab: "previous"}))

	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "task", v.Case)
	assert.Equal(t, "task-1", v.Value)
	assert.Equal(t, "previous", v.Tab)
	assert.Equal(t, 6, v.Held)
}

// The selection is a facts id and survives a refill that still holds the
// row; undo restores the view's resources.
func TestSelectionSurvivesARefillAndRestores(t *testing.T) {
	inst, _ := opsFixture(t)
	require.NoError(t, command(t, inst, opSelectRow, SelectRowArgs{Id: 3}))
	rows := fixtureRows()
	rows[3].Headline = "model call, read again"
	hold(inst, append([]Row{{Id: 7, TsNs: t0.Add(55 * time.Minute).UnixNano(), Kind: "httpFetch", Run: fixtureRun}}, rows...))
	inst.refreshSelected()
	assert.Equal(t, uint64(3), inst.selected.Id)
	assert.Equal(t, "model call, read again", inst.selected.Headline)
	hold(inst, rows[:2])
	inst.refreshSelected()
	assert.True(t, inst.hasSelected, "a row the refill no longer holds stays selected")

	h := inst.Operations()
	assert.True(t, h.Restore(resCase, Case{Kind: CaseApp, Value: "apps/a"}))
	assert.Equal(t, CaseApp, inst.caseValue.Kind)
	f := Filter{}.with(FacetApp, "apps/a", false)
	assert.True(t, h.Restore(resFilter, f.key()))
	assert.Equal(t, f.key(), inst.filter.key())
	assert.True(t, h.Restore(resRange, Window{Last: time.Hour}))
	assert.True(t, h.Restore(resTab, TabContext))
	assert.True(t, h.Restore(resSelection, uint64(0)))
	assert.False(t, inst.hasSelected)
	assert.False(t, h.Restore(resSelection, uint64(999)))
}

// The cache reads off the frame through the probe, under the viewer's call
// identity, and the reads are the composed statements.
func TestTheCacheReadsTheMartsOffTheFrame(t *testing.T) {
	exec := &fakeExec{}
	prev := sourceRef.Load()
	Register(&Source{Exec: exec, Trail: fakeTrail{}, Run: func() string { return fixtureRun }})
	t.Cleanup(func() { Register(prev) })
	inst := newInstance(manifest)
	inst.instance = 7
	assert.Empty(t, exec.statements(), "constructing a window reads nothing")
	now := time.Now()
	inst.tick(now)
	require.Eventually(t, func() bool { return !inst.cache.runner.Running() }, 5*time.Second, time.Millisecond)
	inst.tick(now)
	require.True(t, inst.cache.filled, "%v", inst.cache.lastErr)
	sqls := exec.statements()
	require.Len(t, sqls, 2+len(AllFacets), "rows, strip, one statement per facet")
	assert.Contains(t, sqls[0], "dm_trail_timeline")
	assert.Contains(t, sqls[0], "= '"+fixtureRun+"'", "the window opens on the current run")
	assert.Contains(t, sqls[0], "LIMIT 2000")
	assert.Contains(t, sqls[1], "toStartOfInterval")
	exec.mu.Lock()
	ci := exec.ident[0]
	exec.mu.Unlock()
	assert.Equal(t, Purpose, ci.Claims.Purpose)
	assert.Equal(t, string(manifest.Id), ci.Origin.App)
	assert.Equal(t, uint64(7), ci.Origin.Instance)
	assert.Equal(t, fixtureRun, ci.Origin.Run)
}

// Without ClickHouse there is nothing to read: list_rows answers empty and
// says why, a row cannot be read or selected, and the view still answers.
func TestWithoutAPersistedTrailListRowsSaysWhy(t *testing.T) {
	prev := sourceRef.Load()
	src := &Source{NotPersisted: "no ClickHouse endpoint"}
	Register(src)
	t.Cleanup(func() { Register(prev) })
	inst := newInstance(manifest)
	inst.tick(time.Now())
	assert.False(t, inst.cache.runner.Running(), "nothing is read")
	snap := inst.Operations().Snapshot()
	for _, name := range []string{opGetRow, opGetPrevious, opSelectRow, opGetContext, opGetCompare, opSetCompare} {
		ok, reason := snap.Available(name)
		assert.False(t, ok, name)
		assert.Equal(t, notPersistedReason(src), reason)
	}
	out, err := query[ListRowsArgs, ListRowsResult](t, inst, opListRows, ListRowsArgs{Case: "all"})
	require.NoError(t, err)
	assert.Empty(t, out.Rows)
	assert.False(t, out.Persisted)
	assert.Contains(t, out.Diagnostic, "no ClickHouse endpoint")
	_, err = query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "all", inst.caseValue.Kind.String(), "with no run known the window opens on every row")
}

// engineWindow is a window served by a host engine with the gesture path
// installed.
type engineWindow struct {
	inst *App
	eng  *opengine.Engine
}

func newEngineWindow(inst *App) (w *engineWindow) {
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
	for _, r := range []string{resCase, resRange, resFilter, resSelection, resTab} {
		expects[r] = revs[r]
	}
	inst.eng.Submit(op, opwire.CallRequest{CallId: id, Args: args, Writer: "task:t", Expects: expects})
	inst.frame(nil)
	out, ok := inst.eng.Status(id)
	require.True(t, ok)
	return
}

// The person's controls go through the same commands an agent calls, and
// the engine attributes each writer.
func TestTheEngineAttributesEachWriter(t *testing.T) {
	inst, _ := opsFixture(t)
	w := newEngineWindow(inst)

	out := w.submit(t, "c1", opSetFilter, SetFilterArgs{Terms: []TermArg{{Facet: "kind", Value: "llmCall"}}})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Equal(t, "task:t", w.eng.Writer(resFilter))
	assert.Empty(t, w.eng.UndeclaredWrites(0))

	w.frame(func() { w.inst.setFilterGesture(w.inst.filter.with(FacetStatus, "ok", true)) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resFilter))
	assert.Equal(t, "kind = llmCall, status ≠ ok", w.inst.filter.String())
	w.frame(func() { w.inst.setCaseGesture(Case{Kind: CaseApp, Value: "apps/b"}) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resCase))
	w.frame(func() { w.inst.setWindowGesture(Window{Last: 5 * time.Minute}) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resRange))
	assert.Equal(t, Window{Last: 5 * time.Minute}, w.inst.window)
	w.frame(func() { w.inst.selectGesture(4) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resSelection))
	assert.Equal(t, uint64(4), w.inst.selected.Id)
	w.frame(func() { w.inst.setTabGesture(TabContext) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resTab))
	log := w.eng.Log()
	assert.Equal(t, opSetTab, log[len(log)-1].Op)
	assert.Empty(t, w.eng.UndeclaredWrites(0))
}
