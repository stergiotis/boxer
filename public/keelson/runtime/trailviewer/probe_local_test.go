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
	differs, err := p.previousOf(ctx, &rows[2], time.Hour)
	require.NoError(t, err)
	assert.Equal(t, ClaimDiffers, differs.Claim, differs.Verdict)
	first, err := p.previousOf(ctx, &rows[5], time.Hour)
	require.NoError(t, err)
	assert.Equal(t, ClaimNoPrevious, first.Claim)
}
