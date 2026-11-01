package trailviewer

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/keelson/runtime/trailviewer/verdict"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

// captureTrail is a recorder that keeps what it is asked to write, with
// the call identity each write carried.
type captureTrail struct {
	fakeTrail
	mu      sync.Mutex
	events  []trail.AuditEvent
	ctxs    []callident.CallIdentity
	origins []trail.Origin
	flushes int
	why     string
}

func (inst *captureTrail) NotDurable() string { return inst.why }
func (inst *captureTrail) Event(ctx context.Context, _ time.Time, c trail.Context, row trail.AuditEvent) error {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ci, _ := callident.CallIdentityFrom(ctx)
	inst.events, inst.ctxs, inst.origins = append(inst.events, row), append(inst.ctxs, ci), append(inst.origins, c.Origin)
	return nil
}
func (inst *captureTrail) FlushSoon() { inst.flushes++ }

var _ RecorderI = (*captureTrail)(nil)

// reviewFixture is the ops fixture over a capturing recorder.
func reviewFixture(t *testing.T) (inst *App, rec *captureTrail) {
	t.Helper()
	inst, _ = opsFixture(t)
	rec = &captureTrail{}
	src := *sourceRef.Load()
	src.Trail = rec
	Register(&src)
	return
}

func TestMarkRowIsConsequentialAndPersonOnly(t *testing.T) {
	spec, ok := manifest.Operations.Lookup(opMarkRow)
	require.True(t, ok)
	assert.Equal(t, app.OperationClassCommand, spec.Class)
	assert.Equal(t, app.OperationEffectConsequential, spec.Effect)
	assert.False(t, spec.Agents, "review is the person's act")
	assert.NotEmpty(t, spec.Gesture)
	// Every other command is view, but the saved views (authored state,
	// document) and the export (outside the app, consequential and
	// confirmed by the person; ADR-0350 §SD8).
	effects := map[string]app.OperationEffectE{opMarkRow: app.OperationEffectConsequential, opSendToPlay: app.OperationEffectConsequential,
		opSaveView: app.OperationEffectDocument, opDeleteView: app.OperationEffectDocument}
	for _, o := range manifest.Operations.Operations {
		if o.Class != app.OperationClassCommand {
			continue
		}
		want, named := effects[o.Name]
		if !named {
			want = app.OperationEffectView
		}
		assert.Equal(t, want, o.Effect, o.Name)
		assert.Equal(t, o.Name != opMarkRow, o.Agents, "%s: mark_row is the one command agents are not offered", o.Name)
	}
}

// mark_row refuses what it cannot record, and writes a review-domain
// audit event naming the row, the reason and the rules it clears, under
// the person's OS principal.
func TestMarkRowWritesTheReviewRow(t *testing.T) {
	inst, rec := reviewFixture(t)
	for _, bad := range []MarkRowArgs{{Id: 6, Mark: "approved"}, {Id: 99, Mark: "reviewed"}, {Id: 0, Mark: "reviewed"},
		{Id: 6, Mark: "dismissed"}, {Id: 6, Mark: "dismissed", Reason: "  "}} {
		assert.NotNil(t, refusal(command(t, inst, opMarkRow, bad)), "%+v", bad)
	}
	_, err := inst.markRow(app.OperationCall{Writer: "task:t1"}, MarkRowArgs{Id: 6, Mark: "reviewed"})
	assert.Contains(t, refusal(err).Reason, "person's act")
	_, err = inst.markRow(app.OperationCall{OnBehalfOf: &app.OnBehalfOf{Task: "t1"}}, MarkRowArgs{Id: 6, Mark: "reviewed"})
	assert.NotNil(t, refusal(err))
	rec.why = "no ClickHouse"
	_, err = inst.markRow(app.OperationCall{Writer: opwire.WriterPerson}, MarkRowArgs{Id: 6, Mark: "reviewed"})
	assert.Contains(t, refusal(err).Reason, "not durable")
	rec.why = ""
	assert.Empty(t, rec.events, "a refused mark writes nothing")

	// Row 6 is a denied attention event: the outcome rule raised it.
	inst.cache.exceptions = []Exception{{Id: 6, Rule: RuleOutcome, Reason: "auditEvent status denied"}}
	inst.cache.gen++
	out, err := inst.markRow(app.OperationCall{Writer: opwire.WriterPerson}, MarkRowArgs{Id: 6, Mark: "dismissed", Reason: " expected in the test "})
	require.NoError(t, err)
	require.Len(t, rec.events, 1)
	ev := rec.events[0]
	assert.Equal(t, trail.ReviewDomain, ev.Domain)
	assert.Equal(t, trail.ReviewActionDismissed, ev.Action)
	assert.Equal(t, trail.OutcomeOk, ev.Outcome)
	assert.Equal(t, trail.RetentionTrail, ev.Retention)
	assert.Equal(t, trail.PrincipalByOs, ev.PrincipalBy)
	assert.Equal(t, []string{trail.ReviewRefTrailRow}, ev.RefTypes)
	assert.Equal(t, []string{"6"}, ev.RefValues)
	assert.Equal(t, []string{trail.ReviewAttrReason, trail.ReviewAttrRule}, ev.AttrKeys)
	assert.Equal(t, []string{"expected in the test", RuleOutcome}, ev.AttrValues)
	ci := rec.ctxs[0]
	stamped := audit.Stamp(callident.WithCallIdentity(context.Background(), ci), ev, evidence.OriginByHost)
	require.NoError(t, stamped.Validate(), "the row as the recorder stamps it")
	assert.Equal(t, option.Some(osPrincipal()), stamped.Principal)
	assert.Equal(t, trail.PrincipalByOs, stamped.PrincipalBy, "beside a principal, principal-by stays the emitter's")
	assert.Equal(t, osPrincipal(), ci.Claims.Principal, "the principal rides the call identity, as the recorder stamps it")
	assert.Equal(t, Purpose, ci.Claims.Purpose)
	assert.Equal(t, string(manifest.Id), ci.Origin.App)
	assert.Equal(t, string(manifest.Id), rec.origins[0].App)
	assert.Equal(t, 1, rec.flushes)
	assert.Equal(t, MarkRowResult{Id: 6, Mark: "dismissed", Rule: RuleOutcome, Principal: osPrincipal(), At: out.At}, out)
	assert.True(t, inst.cache.refreshNow, "the refill reads the mark back")

	// A row with no exception: no rule attribute, and a reason is optional.
	_, err = inst.markRow(app.OperationCall{Writer: opwire.WriterPerson}, MarkRowArgs{Id: 4, Mark: "reviewed"})
	require.NoError(t, err)
	assert.Empty(t, rec.events[1].AttrKeys)
}

// The principal is a pseudonymous reference, stable within a process, and
// never the user's name.
func TestTheReviewerIsAPseudonymousReference(t *testing.T) {
	p := osPrincipal()
	assert.Equal(t, p, osPrincipal())
	assert.True(t, strings.HasPrefix(p, "os-"))
	assert.Len(t, p, len("os-")+32)
	ev := trail.AuditEvent{Domain: trail.ReviewDomain, Action: "reviewed", Outcome: trail.OutcomeOk, PrincipalBy: trail.PrincipalByOs,
		OriginBy: evidence.OriginByHost.String(), Retention: trail.RetentionTrail}
	ev.Principal = option.Some(p)
	assert.NoError(t, ev.Validate())
}

// A mark clears the row's exceptions: the row leaves the open list at once
// as a pending mark, and stays out once a refill reads the mark back.
func TestAMarkedExceptionLeavesTheList(t *testing.T) {
	inst, _ := reviewFixture(t)
	inst.cache.exceptions = []Exception{{Id: 6, Rule: RuleOutcome, Reason: "denied"}, {Id: 5, Rule: RuleTrailRow, Reason: "trail.config row"}}
	inst.cache.gen++
	require.Len(t, inst.exceptions().open, 2)

	_, err := inst.markRow(app.OperationCall{Writer: opwire.WriterPerson}, MarkRowArgs{Id: 6, Mark: "reviewed"})
	require.NoError(t, err)
	exc := inst.exceptions()
	require.Len(t, exc.open, 1)
	assert.Equal(t, uint64(5), exc.open[0].Id)
	assert.Equal(t, 1, exc.cleared)
	assert.Len(t, exc.byRow[6], 1, "the row keeps its exception, cleared")
	text, _, hover := exceptionCell(exc.byRow[6], inst.marks(), 6)
	assert.Equal(t, "reviewed", text, "the table shows the mark")
	assert.Contains(t, hover, "clears outcome")

	// The refill reads it back: the pending mark settles into the cache's.
	pending := inst.pending[6]
	inst.cache.reviews = map[uint64]Review{6: {Id: 6, TsNs: pending.TsNs, Mark: "reviewed", Rule: RuleOutcome, Principal: pending.Principal, Marks: 1}}
	inst.cache.gen++
	inst.settlePending()
	assert.Empty(t, inst.pending)
	assert.Len(t, inst.exceptions().open, 1)

	ex, err := query[GetExceptionsArgs, ExceptionsResult](t, inst, opGetExc, GetExceptionsArgs{Case: "run", Value: fixtureRun, Range: listArgs(inst).Range})
	require.NoError(t, err)
	assert.Empty(t, ex.Diagnostic)
	assert.Equal(t, 1, ex.Open)
	assert.Equal(t, 1, ex.Cleared)
	require.Len(t, ex.Exceptions, 1)
	assert.Equal(t, ExceptionRow{Id: 5, Ts: fixtureRows()[1].Ts(), Kind: kindAuditEvent, Headline: "trail config -> ok", Rule: RuleTrailRow,
		Reason: "trail.config row"}, ex.Exceptions[0])
	assert.Len(t, ex.Rules, len(Rules))
	d, err := query[GetRowArgs, RowDetail](t, inst, opGetRow, GetRowArgs{Id: 6})
	require.NoError(t, err)
	assert.Equal(t, "reviewed", d.Mark)
	require.Len(t, d.Exceptions, 1)
	assert.Equal(t, RuleOutcome, d.Exceptions[0].Rule)

	ex, err = query[GetExceptionsArgs, ExceptionsResult](t, inst, opGetExc, GetExceptionsArgs{Case: "run", Value: fixtureRun, Range: listArgs(inst).Range, Rule: RuleOutcome})
	require.NoError(t, err)
	assert.Equal(t, 0, ex.Open)
	_, err = query[GetExceptionsArgs, ExceptionsResult](t, inst, opGetExc, GetExceptionsArgs{Case: "run", Value: fixtureRun, Rule: "nonsense"})
	assert.NotNil(t, refusal(err))
	ex, err = query[GetExceptionsArgs, ExceptionsResult](t, inst, opGetExc, GetExceptionsArgs{Case: "app", Value: "apps/a"})
	require.NoError(t, err)
	assert.Contains(t, ex.Diagnostic, "set_case", "another case is not what the window read")
}

// The two rules computed from what the window holds raise exactly their
// rows: the unsealed rule every row of a run whose line is broken, a gap or
// a fork, the claim rule the rows judged to differ.
func TestTheHeldRulesRaiseExactlyTheirRows(t *testing.T) {
	rows := fixtureRows()
	rows[2].Run, rows[3].Run, rows[4].Run = "run-gap", "run-gap", "run-unanchored"
	lines := []verdict.Line{{Run: fixtureRun, State: verdict.StateHolds}, {Run: "run-gap", State: verdict.StateGap, Count: 2},
		{Run: "run-unanchored", State: verdict.StateUnanchored, Ordinal: 3}}
	claims := map[uint64]claimJudgment{6: {differs: true, reason: "differs from the previous event"}, 5: {}}
	exc := evaluateExceptions(rows, nil, claims, lines, nil)
	byRule := map[string][]uint64{}
	for _, e := range exc.open {
		byRule[e.Rule] = append(byRule[e.Rule], e.Id)
	}
	assert.Equal(t, map[string][]uint64{RuleUnsealed: {4, 3}, RuleClaimDiffers: {6}}, byRule)
	assert.Equal(t, "writer run run-gap reads gap: 2 rows no seal names", exc.byRow[4][0].Reason)

	// SQL rules' rows join in the rules' order, an unknown or repeated rule
	// is dropped, and a row outside the held ones is not listed.
	raised := []Exception{{Id: 3, Rule: RuleNovelAction}, {Id: 3, Rule: RuleOutcome}, {Id: 3, Rule: RuleOutcome}, {Id: 3, Rule: "nonsense"}, {Id: 99, Rule: RuleOutcome}}
	exc = evaluateExceptions(rows, raised, nil, lines, nil)
	var got []string
	for _, e := range exc.byRow[3] {
		got = append(got, e.Rule)
	}
	assert.Equal(t, []string{RuleOutcome, RuleUnsealed, RuleNovelAction}, got)
	_, listed := exc.byRow[99]
	assert.False(t, listed)
}

// Every rule is shown with a predicate a person can read: the SQL rules'
// placeholders as logical names, the held rules' in words.
func TestEveryRuleShowsItsPredicate(t *testing.T) {
	names := map[string]bool{}
	for _, r := range Rules {
		assert.False(t, names[r.Name], "%s twice", r.Name)
		names[r.Name] = true
		assert.Regexp(t, `^[a-z][a-z-]*$`, r.Name)
		assert.NotEmpty(t, r.Description, r.Name)
		text := r.PredicateText()
		assert.NotEmpty(t, text, r.Name)
		assert.NotContains(t, text, "{", r.Name)
		assert.NotContains(t, text, ":s:", "%s: no physical name", r.Name)
		if !r.FromHeld() {
			assert.NotEmpty(t, r.Reason, r.Name)
		}
		got, ok := LookupRule(r.Name)
		assert.True(t, ok)
		assert.Equal(t, r.Name, got.Name)
	}
	assert.Len(t, Rules, 7, "the rules ADR-0350 §SD7 lists")
	novel, _ := LookupRule(RuleNovelAction)
	assert.Contains(t, novel.PredicateText(), "(domain, action) NOT IN (SELECT DISTINCT domain, action FROM dm_trail_audit_events WHERE ts < window_start AND ts >= (window_start - lookback))")
}

// Compare puts the older row left whichever is selected first, and says
// so above the table; audit columns are compared only between two audit
// events.
func TestCompareOrdersOlderLeftAndStatesItsBase(t *testing.T) {
	rows := fixtureRows()
	format := func(t time.Time) string { return t.Format(time.RFC3339) }
	cmp := compareRows(rows[0], rows[1], format)
	assert.Equal(t, uint64(5), cmp.Older.Id)
	assert.Equal(t, uint64(6), cmp.Newer.Id)
	assert.Equal(t, "Left is the older row, id 5 (auditEvent) at 2026-10-31T12:40:00Z; right is the newer row, id 6 (auditEvent) at 2026-10-31T12:50:00Z. "+
		"Change says how the newer row differs from the older.", cmp.Base)
	assert.Equal(t, cmp, compareRows(rows[1], rows[0], format), "the order the person picked them in does not matter")
	changes := map[string]FieldDiff{}
	for _, f := range cmp.Fields {
		changes[f.Field] = f
	}
	assert.Equal(t, FieldDiff{Field: "status", Before: "ok", After: "denied", Change: "changed"}, changes["status"])
	assert.Equal(t, FieldDiff{Field: "attr.from", After: "ask", Change: "added"}, changes["attr.from"], "the claim is compared too")
	assert.Equal(t, FieldDiff{Field: "principal", Before: "p-2", After: "p-1", Change: "changed"}, changes["principal"])
	assert.Equal(t, "same", changes["run"].Change)

	mixed := compareRows(rows[0], rows[4], format)
	assert.Equal(t, uint64(2), mixed.Older.Id)
	assert.Contains(t, mixed.Base, "only when both rows are audit events")
	for _, f := range mixed.Fields {
		assert.NotEqual(t, "principal", f.Field)
	}
}

// marks merges the cache's reviews with the pending marks once per fill
// and pending change, not on every call of a frame.
func TestTheMergedMarksAreBuiltOncePerChange(t *testing.T) {
	inst, _ := reviewFixture(t)
	assert.Nil(t, inst.marks(), "no reviews and nothing pending: the cache's own")
	_, err := inst.markRow(app.OperationCall{Writer: opwire.WriterPerson}, MarkRowArgs{Id: 6, Mark: "reviewed"})
	require.NoError(t, err)
	first := inst.marks()
	require.Contains(t, first, uint64(6))
	assert.Equal(t, reflect.ValueOf(first).Pointer(), reflect.ValueOf(inst.marks()).Pointer(), "the same map while nothing moved")

	inst.cache.reviews = map[uint64]Review{5: {Id: 5, TsNs: t0.UnixNano(), Mark: "flagged"}}
	inst.cache.gen++
	second := inst.marks()
	assert.NotEqual(t, reflect.ValueOf(first).Pointer(), reflect.ValueOf(second).Pointer(), "a refill rebuilds it")
	assert.Equal(t, "flagged", second[5].Mark)
	assert.Equal(t, "reviewed", second[6].Mark)
	assert.NotContains(t, first, uint64(5), "a map handed out is not written to")

	_, err = inst.markRow(app.OperationCall{Writer: opwire.WriterPerson}, MarkRowArgs{Id: 5, Mark: "reviewed"})
	require.NoError(t, err)
	assert.Equal(t, "reviewed", inst.marks()[5].Mark, "a new pending mark rebuilds it")
}

// The as-of line names the domain each state is in: the entity case
// names none, and one entity string in two domains is two states.
func TestTheAsOfLineSaysWhichDomain(t *testing.T) {
	ev := func(id uint64, at time.Time, domain, action string, kv ...string) AuditDetail {
		a := AuditDetail{Id: id, TsNs: at.UnixNano(), Domain: domain, Action: action, Entity: "k:a"}
		for i := 0; i+1 < len(kv); i += 2 {
			a.AttrKeys, a.AttrValues = append(a.AttrKeys, kv[i]), append(a.AttrValues, kv[i+1])
		}
		return a
	}
	bound := t0.Add(time.Hour)
	format := func(t time.Time) string { return t.Format(time.RFC3339) }
	one := &AsOf{Entity: "k:a", Bound: bound, Events: []AuditDetail{ev(3, bound, "attention", "level", "level", "at the bound", "from", "x")}}
	assert.Equal(t, "As of 2026-10-31T13:00:00Z, k:a's state in domain attention comes from event id 3 (attention.level) at 2026-10-31T13:00:00Z: level=at the bound",
		asOfText(one, format), "the from claim is not state")
	two := &AsOf{Entity: "k:a", Bound: bound, Events: []AuditDetail{
		ev(3, bound, "attention", "level", "level", "deny"),
		ev(1, t0, "archive", "stored", "shelf", "b"),
	}}
	assert.Equal(t, "As of 2026-10-31T13:00:00Z, k:a's state in domain attention comes from event id 3 (attention.level) at 2026-10-31T13:00:00Z: level=deny; "+
		"in domain archive comes from event id 1 (archive.stored) at 2026-10-31T12:00:00Z: shelf=b", asOfText(two, format))
	assert.Contains(t, asOfText(&AsOf{Entity: "k:a", Bound: bound}, time.Time.String), "no event of k:a at or before it")
}

// A table whose first frame draws its end row reads no more until a
// second frame of the same fill draws it too.
func TestTheTableExtendsOnlyOnASettledEnd(t *testing.T) {
	g := &gridState{}
	assert.False(t, g.reachedEnd(true, 1), "a fill's first frame")
	assert.False(t, g.reachedEnd(false, 1), "the fitted frame shows the top")
	assert.False(t, g.reachedEnd(true, 1), "scrolled to the end once")
	assert.True(t, g.reachedEnd(true, 1), "and it stays there")
	assert.False(t, g.reachedEnd(true, 2), "an extension's fill starts over")
	assert.True(t, g.reachedEnd(true, 2))
}

// The person's Mark buttons go through mark_row as a gesture; a task's
// call of it is refused.
func TestTheMarkButtonsRouteThroughTheCatalog(t *testing.T) {
	inst, rec := reviewFixture(t)
	w := newEngineWindow(inst)
	w.frame(func() {
		w.inst.markReason = "seen"
		w.inst.markGesture(6, MarkFlagged)
	})
	require.NoError(t, w.inst.markErr)
	require.Len(t, rec.events, 1)
	assert.Equal(t, trail.ReviewActionFlagged, rec.events[0].Action)
	assert.Empty(t, w.inst.markReason, "the reason box clears once the mark is written")
	log := w.eng.Log()
	assert.Equal(t, opMarkRow, log[len(log)-1].Op)
	assert.Equal(t, opwire.WriterPerson, log[len(log)-1].Writer)

	w.frame(func() { w.inst.markGesture(6, MarkDismissed) })
	assert.Contains(t, refusal(w.inst.markErr).Reason, "reason is required", "the refusal is shown, not retried")
	assert.Len(t, rec.events, 1)

	out := w.submit(t, "c1", opMarkRow, MarkRowArgs{Id: 6, Mark: "reviewed"})
	assert.NotEqual(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Len(t, rec.events, 1, "a task's call writes nothing")
}
