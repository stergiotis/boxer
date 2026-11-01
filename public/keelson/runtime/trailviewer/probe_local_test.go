package trailviewer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	keelsonaudit "github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

// localTrail is clickhouse-local with the read surface, the facts table
// and the trail views, or a skip.
func localTrail(t *testing.T) (exec *chexec.LocalExecutor, ctx context.Context) {
	t.Helper()
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx = context.Background()
	setup, err := chstore.ComposeSetupSQL(chstore.Config{Database: factsschema.DatabaseName, Table: factsschema.TableName}, "")
	require.NoError(t, err)
	stmts := append([]string{}, lwsqlsurface.AllStatements("")...)
	for stmt := range strings.SplitSeq(setup, ";") {
		stmts = append(stmts, stmt)
	}
	views, err := trailviews.Statements("", "")
	require.NoError(t, err)
	stmts = append(stmts, views...)
	for _, stmt := range stmts {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			require.NoError(t, exec.Exec(ctx, stmt))
		}
	}
	return
}

// Over clickhouse-local: rows written through the trail's own recorder
// read back through the probe — the table by case, the strip, the facets,
// the context of a row, and the previous-state read with a gap between.
func TestTheProbeReadsBackWrittenRowsOverLocal(t *testing.T) {
	exec, ctx := localTrail(t)
	rec := trail.NewRecorder(exec, "run-1", zerolog.Nop())
	defer rec.Close()
	at := time.Unix(1_700_000_000, 0).UTC()
	c := trail.Context{Origin: rec.OriginOf("apps/rules", 3)}
	ev := func(action string, keys []string, values []string, refs ...string) trail.AuditEvent {
		e := trail.AuditEvent{Domain: "rules", Action: action, Outcome: trail.OutcomeOk, PrincipalBy: trail.PrincipalByNone, Retention: "ops",
			AttrKeys: keys, AttrValues: values}
		for i := 0; i+1 < len(refs); i += 2 {
			e.RefTypes, e.RefValues = append(e.RefTypes, refs[i]), append(e.RefValues, refs[i+1])
		}
		return e
	}
	write := func(offset time.Duration, e trail.AuditEvent) {
		require.NoError(t, rec.Event(ctx, at.Add(offset), c, e))
	}
	write(1*time.Second, ev("created", []string{"state"}, []string{"draft"}, "rule", "r1"))
	write(2*time.Second, ev("created", []string{"state"}, []string{"draft"}, "rule", "r2", "conversation", "conv-9"))
	write(3*time.Second, ev("updated", []string{"state", "from"}, []string{"active", "draft"}, "rule", "r1"))
	write(4*time.Second, ev("updated", []string{"state", "from"}, []string{"active", "retired"}, "rule", "r2"))
	// The recorder's own row for rows of the run it dropped between r1's
	// second and third event.
	require.NoError(t, rec.Event(ctx, at.Add(6500*time.Millisecond), c,
		audit.Gap(2, at.Add(5*time.Second), at.Add(6*time.Second), evidence.OriginByHost, option.None[string]())))
	write(7*time.Second, ev("updated", []string{"state", "from"}, []string{"retired", "active"}, "rule", "r1"))
	require.NoError(t, rec.Flush(ctx))

	src := &Source{Exec: exec, Run: func() string { return "run-1" }}
	p, err := newProbe(src, string(manifest.Id), 1)
	require.NoError(t, err)
	from, to := at, at.Add(time.Minute)

	rows, err := p.listRows(ctx, listQuery{Case: Case{Kind: CaseRun, Value: "run-1"}, From: from, To: to, Limit: 100})
	require.NoError(t, err)
	// r1@7s, the gap row, r2@4s, r1@3s, r2@2s, r1@1s.
	require.Len(t, rows, 6)
	assert.Equal(t, kindAuditEvent, rows[0].Kind)
	assert.Equal(t, at.Add(7*time.Second), rows[0].Ts(), "newest first")
	require.NotNil(t, rows[0].Audit, "the audit events' own columns are attached")
	assert.Equal(t, "rule:r1", rows[0].Audit.Entity)
	assert.Equal(t, "active", rows[0].Audit.From)
	assert.Equal(t, "apps/rules", rows[0].App)
	assert.Equal(t, uint64(3), rows[0].Instance)
	assert.Equal(t, "trail.audit-gap", rows[1].Subject)

	// The cursor continues after the second row without repeating it.
	more, err := p.listRows(ctx, listQuery{Case: Case{Kind: CaseRun, Value: "run-1"}, From: from, To: to, Limit: 100,
		Cursor: &Cursor{TsNs: rows[1].TsNs, Id: rows[1].Id}})
	require.NoError(t, err)
	require.Len(t, more, 4)
	assert.Equal(t, rows[2].Id, more[0].Id)

	byEntity, err := p.listRows(ctx, listQuery{Case: Case{Kind: CaseEntity, Value: "rule:r1"}, From: from, To: to, Limit: 100})
	require.NoError(t, err)
	assert.Len(t, byEntity, 3)
	byConv, err := p.listRows(ctx, listQuery{Case: Case{Kind: CaseConversation, Value: "conv-9"}, From: from, To: to, Limit: 100})
	require.NoError(t, err)
	require.Len(t, byConv, 1, "an audit event referencing the conversation")
	none, err := p.listRows(ctx, listQuery{Case: Case{Kind: CaseDestination, Value: "x"}, From: from, To: to, Limit: 100})
	require.NoError(t, err)
	assert.Empty(t, none)
	filtered, err := p.listRows(ctx, listQuery{Case: Case{Kind: CaseAll}, From: from, To: to, Limit: 100,
		Filter: Filter{}.with(FacetDomainAction, "rules.created", true)})
	require.NoError(t, err)
	assert.Len(t, filtered, 4)

	buckets, err := p.strip(ctx, listQuery{Case: Case{Kind: CaseAll}, From: from, To: to}, 2*time.Second, StripByKind)
	require.NoError(t, err)
	assert.Equal(t, "6 rows in 4 buckets of 2s; busiest 2023-11-14 22:13:22 UTC with 2", stripLine(buckets, 2*time.Second))
	facets, err := p.facet(ctx, FacetDomainAction, Case{Kind: CaseRun, Value: "run-1"}, from, to)
	require.NoError(t, err)
	assert.Equal(t, []FacetValue{{Value: "rules.updated", N: 3}, {Value: "rules.created", N: 2}, {Value: "trail.audit-gap", N: 1}}, facets)

	before, after, err := p.contextOf(ctx, &rows[3], time.Hour)
	require.NoError(t, err)
	require.Len(t, before, 2)
	require.Len(t, after, 3)
	assert.True(t, before[0].TsNs < before[1].TsNs, "oldest first")
	require.NotNil(t, after[2].Audit)

	gapped, err := p.previousOf(ctx, &rows[0], time.Hour)
	require.NoError(t, err)
	require.NotNil(t, gapped)
	assert.True(t, gapped.Has)
	assert.Equal(t, rows[3].Id, gapped.PrevId, "r1's previous event, not r2's")
	assert.Equal(t, ClaimGapBetween, gapped.Claim, gapped.Verdict)
	matches, err := p.previousOf(ctx, &rows[3], time.Hour)
	require.NoError(t, err)
	assert.Equal(t, ClaimMatches, matches.Claim, matches.Verdict)
	// r2's claim, retired, is not the draft its previous event held: the
	// fixture writes it so, for the differs verdict.
	differs, err := p.previousOf(ctx, &rows[2], time.Hour)
	require.NoError(t, err)
	assert.Equal(t, ClaimDiffers, differs.Claim, differs.Verdict)
	first, err := p.previousOf(ctx, &rows[5], time.Hour)
	require.NoError(t, err)
	assert.Equal(t, ClaimNoPrevious, first.Claim)
}

// Over clickhouse-local: the SQL exception rules each raise exactly their
// fixture rows among rows written through the recorder — a denied bus
// request, a refused model call, a widened grant, a gap row, a novel
// domain.action — the claim rule the from mismatch, and a mark written
// through the same recorder reads back from dm_trail_reviews and clears
// its row's exceptions.
func TestTheRulesAndAMarkRunOverLocal(t *testing.T) {
	exec, ctx := localTrail(t)
	rec := trail.NewRecorder(exec, "run-1", zerolog.Nop())
	defer rec.Close()
	at := time.Unix(1_700_000_000, 0).UTC()
	look := 2 * time.Hour
	c := trail.Context{Origin: rec.OriginOf("apps/rules", 3)}
	event := func(offset time.Duration, domain, action, outcome string, kv []string, refs ...string) {
		e := trail.AuditEvent{Domain: domain, Action: action, Outcome: outcome, PrincipalBy: trail.PrincipalByNone, Retention: "ops"}
		for i := 0; i+1 < len(kv); i += 2 {
			e.AttrKeys, e.AttrValues = append(e.AttrKeys, kv[i]), append(e.AttrValues, kv[i+1])
		}
		for i := 0; i+1 < len(refs); i += 2 {
			e.RefTypes, e.RefValues = append(e.RefTypes, refs[i]), append(e.RefValues, refs[i+1])
		}
		require.NoError(t, rec.Event(ctx, at.Add(offset), c, e))
	}
	gap := func(offset time.Duration, from, to time.Duration) {
		require.NoError(t, rec.Event(ctx, at.Add(offset), c, audit.Gap(2, at.Add(from), at.Add(to), evidence.OriginByHost, option.None[string]())))
	}
	// The look-back before the window: what makes an action not novel.
	event(-30*time.Minute, "rules", "created", trail.OutcomeOk, []string{"state", "draft"}, "rule", "r1")
	event(-29*time.Minute, "rules", "updated", trail.OutcomeOk, []string{"state", "x"}, "rule", "r9")
	event(-28*time.Minute, keelsonaudit.BusDomain, keelsonaudit.BusActionRequest, trail.OutcomeOk, nil, keelsonaudit.BusRefSubject, "a.b")
	gap(-27*time.Minute, -3*time.Hour, -179*time.Minute)
	// The window.
	event(1*time.Second, keelsonaudit.BusDomain, keelsonaudit.BusActionRequest, trail.OutcomeDenied, nil, keelsonaudit.BusRefSubject, "a.b")
	grant := trail.Context{Origin: c.Origin, Delegation: option.Some(trail.Delegation{Task: "task-1", Epoch: 2})}
	require.NoError(t, rec.AgentGrant(at.Add(2*time.Second), grant, option.None[trail.Cause](),
		trail.AgentGrant{Event: trail.GrantEventWidened, Destinations: []string{"a", "b"}, DecidedBy: "person"}))
	event(3*time.Second, "rules", "archived", trail.OutcomeOk, []string{"state", "archived"}, "rule", "r1")
	event(5*time.Second, "rules", "updated", trail.OutcomeOk, []string{"state", "active", "from", "retired"}, "rule", "r1")
	event(6*time.Second, "rules", "created", trail.OutcomeOk, []string{"state", "draft"}, "rule", "r3")
	require.NoError(t, rec.LlmCall(at.Add(7*time.Second), c, trail.LlmCall{CallId: "call-1", Purpose: "book/slug", Sensitivity: "open",
		Model: "m", EndpointHost: "h", FinishReason: "stop", Retention: "ops", Refused: true}))
	gap(12*time.Second, 10*time.Second, 11*time.Second)
	require.NoError(t, rec.Flush(ctx))

	src := &Source{Exec: exec, Trail: rec, Run: func() string { return "run-1" }}
	p, err := newProbe(src, string(manifest.Id), 1)
	require.NoError(t, err)
	q := listQuery{Case: Case{Kind: CaseRun, Value: "run-1"}, From: at, To: at.Add(time.Minute), Limit: 100}
	rows, err := p.listRows(ctx, q)
	require.NoError(t, err)
	require.Len(t, rows, 7)
	idOf := func(subject string, offset time.Duration) uint64 {
		for i := range rows {
			if rows[i].Subject == subject && rows[i].Ts().Equal(at.Add(offset)) {
				return rows[i].Id
			}
		}
		t.Fatalf("no %s row at %s", subject, offset)
		return 0
	}
	denied := idOf("bus.request", time.Second)
	var widened, refused uint64
	for i := range rows {
		switch rows[i].Kind {
		case kindAgentGrant:
			widened = rows[i].Id
		case "llmCall":
			refused = rows[i].Id
		}
	}
	require.NotZero(t, widened)
	require.NotZero(t, refused)
	gapRow, novel, mismatch := idOf("trail.audit-gap", 12*time.Second), idOf("rules.archived", 3*time.Second), idOf("rules.updated", 5*time.Second)

	raised, err := queryRows[Exception](ctx, p, p.marts.exceptionsSQL(q, nil, look))
	require.NoError(t, err)
	claims, err := p.judgeClaims(ctx, rows, nil, look)
	require.NoError(t, err)
	assert.Equal(t, map[uint64]claimJudgment{mismatch: {differs: true, reason: claims[mismatch].reason}}, claims, "one row carries a claim")
	assert.Contains(t, claims[mismatch].reason, `claims from "retired"`)
	exc := evaluateExceptions(rows, raised, claims, nil, nil)
	byRule := map[string][]uint64{}
	for _, e := range exc.open {
		byRule[e.Rule] = append(byRule[e.Rule], e.Id)
	}
	assert.Equal(t, map[string][]uint64{
		RuleOutcome:      {gapRow, denied}, // a gap row's outcome is failed
		RuleModelRefused: {refused},
		RuleGrantWidened: {widened},
		RuleTrailRow:     {gapRow},
		RuleNovelAction:  {novel},
		RuleClaimDiffers: {mismatch},
	}, byRule)
	assert.Equal(t, "auditEvent status denied", exc.byRow[denied][0].Reason)
	assert.Equal(t, "rules.archived not seen in the look-back before the window", exc.byRow[novel][0].Reason)

	// As of a bound: the entity's last event at or before it.
	asOf, err := queryRows[AuditDetail](ctx, p, p.marts.asOfSQL("rule:r1", at.Add(4*time.Second), look))
	require.NoError(t, err)
	require.Len(t, asOf, 1)
	assert.Equal(t, novel, asOf[0].Id)
	asOf, err = queryRows[AuditDetail](ctx, p, p.marts.asOfSQL("rule:r1", at.Add(2*time.Second), look))
	require.NoError(t, err)
	require.Len(t, asOf, 1)
	assert.Equal(t, at.Add(-30*time.Minute), asOf[0].Ts(), "before the window, within the look-back")

	// A mark written through the real recorder reads back and clears.
	prev := sourceRef.Load()
	Register(src)
	t.Cleanup(func() { Register(prev) })
	inst := newInstance(manifest)
	inst.cache.rows, inst.cache.exceptions, inst.cache.claims = rows, raised, claims
	inst.cache.gen++
	out, err := inst.markRow(app.OperationCall{Writer: "person"}, MarkRowArgs{Id: denied, Mark: "dismissed", Reason: "a probe of the test"})
	require.NoError(t, err)
	assert.Equal(t, RuleOutcome, out.Rule)
	require.NoError(t, rec.Flush(ctx))
	reviews, err := queryRows[Review](ctx, p, p.marts.reviewsSQL(q, nil))
	require.NoError(t, err)
	require.Len(t, reviews, 1)
	r := reviews[0]
	assert.Equal(t, denied, r.Id)
	assert.Equal(t, "dismissed", r.Mark)
	assert.Equal(t, "a probe of the test", r.Reason)
	assert.Equal(t, RuleOutcome, r.Rule)
	assert.Equal(t, osPrincipal(), r.Principal)
	assert.Equal(t, uint64(1), r.Marks)
	type by struct {
		PrincipalBy string `ch:"pb"`
	}
	pb, err := queryRows[by](ctx, p, "SELECT "+p.marts.col(trailviews.ViewReviews, "principal-by")+" AS pb FROM "+p.marts.table(trailviews.ViewReviews))
	require.NoError(t, err)
	assert.Equal(t, []by{{PrincipalBy: trail.PrincipalByOs}}, pb)
	exc = evaluateExceptions(rows, raised, claims, nil, map[uint64]Review{r.Id: r})
	for _, e := range exc.open {
		assert.NotEqual(t, denied, e.Id, "the marked row has left the list")
	}
	assert.Equal(t, 1, exc.cleared)
}
