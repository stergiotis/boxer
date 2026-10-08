package trail_test

import (
	"context"
	"errors"
	"iter"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// localFacts provisions boxer.facts on clickhouse-local, or skips.
func localFacts(t *testing.T) (exec *chexec.LocalExecutor, ctx context.Context) {
	t.Helper()
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx = context.Background()
	setup, err := chstore.ComposeSetupSQL(chstore.Config{Database: factsschema.DatabaseName, Table: factsschema.TableName}, "")
	require.NoError(t, err)
	for stmt := range strings.SplitSeq(setup, ";") {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			require.NoError(t, exec.Exec(ctx, stmt))
		}
	}
	return
}

func scanEvents(t *testing.T, rec *trail.Recorder, ctx context.Context, opts recordstore.ScanOpts) (ents []*trail.TrailEntity) {
	t.Helper()
	ents, err := rec.Scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanAuditEvent(ctx, opts)
	})
	require.NoError(t, err)
	return
}

// Over clickhouse-local: what an audit event's row carries comes from where
// ADR-0296 §SD1 says — the origin and the claims from the context's call
// identity, the task from the caller's Context — and a row outside the
// bounds reads back as the recorder's audit-invalid row.
func TestEventRowsReadBackWithTheirIdentity(t *testing.T) {
	exec, ctx := localFacts(t)
	rec := trail.NewRecorder(exec, "run-9", zerolog.Nop())
	defer rec.Close()
	at := time.Unix(1_700_000_000, 0).UTC()

	// 1. A vouched origin and claims on the context.
	ci := callident.CallIdentity{Origin: callident.Origin{Run: "run-9", App: "apps/dmdm", Instance: 4}, Claims: callident.Claims{Principal: "p-1", Purpose: "art-15"}}
	withId := callident.WithCallIdentity(ctx, ci)
	require.NoError(t, rec.Event(withId, at, trail.Context{Delegation: option.Some(trail.Delegation{Task: "task-1", Epoch: 2})}, trail.AuditEvent{
		Domain: "dmdm", Action: "vault-resolve", Outcome: trail.OutcomeOk, PrincipalBy: trail.PrincipalBySystem,
		Principal: option.Some("ignored"), Purpose: option.Some("ignored"),
		Node: option.Some("cell-a"), Subject: 42, Retention: "disclosure",
		RefTypes: []string{"vaultref"}, RefValues: []string{"v-1"},
	}))
	// 2. No identity: the origin is what the caller's Context says, under the run.
	require.NoError(t, rec.Event(ctx, at.Add(time.Second), trail.Context{Origin: rec.OriginOf("apps/dmdm", 5)}, trail.AuditEvent{
		Domain: "dmdm", Action: "erase-issue", Outcome: trail.OutcomeDenied, PrincipalBy: trail.PrincipalByEnv,
		Subject: 43, Retention: "erasure",
	}))
	// 3. Outside the bounds: replaced.
	require.NoError(t, rec.Event(withId, at.Add(2*time.Second), trail.Context{}, trail.AuditEvent{
		Domain: "dmdm", Action: "Not A Name", Outcome: trail.OutcomeOk, PrincipalBy: trail.PrincipalBySystem, Subject: 44, Retention: "disclosure",
	}))
	require.NoError(t, rec.Flush(ctx))

	ents := scanEvents(t, rec, ctx, recordstore.ScanOpts{})
	require.Len(t, ents, 3)
	byAction := map[string]*trail.TrailEntity{}
	for _, e := range ents {
		require.True(t, e.AuditEvent.Has)
		byAction[e.AuditEvent.Val.Action] = e
	}

	e := byAction["vault-resolve"]
	require.NotNil(t, e)
	assert.Equal(t, trail.Origin{Id: e.ID, Run: "run-9", App: "apps/dmdm", Instance: 4}, e.Origin.Val, "the vouched origin wins")
	assert.Equal(t, option.Some("p-1"), e.AuditEvent.Val.Principal, "the principal is the claim, not the row's")
	assert.Equal(t, option.Some("art-15"), e.AuditEvent.Val.Purpose)
	assert.Equal(t, trail.PrincipalBySystem, e.AuditEvent.Val.PrincipalBy)
	assert.Equal(t, option.Some("cell-a"), e.AuditEvent.Val.Node)
	assert.EqualValues(t, 42, e.AuditEvent.Val.Subject)
	assert.Equal(t, "disclosure", e.AuditEvent.Val.Retention)
	assert.Equal(t, []string{"vaultref"}, e.AuditEvent.Val.RefTypes)
	assert.Equal(t, []string{"v-1"}, e.AuditEvent.Val.RefValues)
	require.True(t, e.Delegation.Has, "the task rides from the caller's Context")
	assert.Equal(t, "task-1", e.Delegation.Val.Task)
	assert.Equal(t, "auditEvent", e.AuditEvent.Val.Kind)

	e = byAction["erase-issue"]
	require.NotNil(t, e)
	assert.Equal(t, trail.Origin{Id: e.ID, Run: "run-9", App: "apps/dmdm", Instance: 5}, e.Origin.Val)
	assert.False(t, e.AuditEvent.Val.Principal.Has)
	assert.Equal(t, trail.PrincipalByNone, e.AuditEvent.Val.PrincipalBy, "no principal on the context forces none")
	assert.Equal(t, trail.OutcomeDenied, e.AuditEvent.Val.Outcome)
	assert.False(t, e.Delegation.Has)

	e = byAction[trail.ActionAuditInvalid]
	require.NotNil(t, e, "the invalid row is replaced, not dropped")
	assert.Equal(t, trail.TrailDomain, e.AuditEvent.Val.Domain)
	assert.Equal(t, trail.OutcomeFailed, e.AuditEvent.Val.Outcome)
	assert.Equal(t, trail.RetentionTrail, e.AuditEvent.Val.Retention)
	assert.EqualValues(t, 44, e.AuditEvent.Val.Subject, "the subject is kept")
	assert.Equal(t, option.Some("p-1"), e.AuditEvent.Val.Principal, "the vouched principal is kept")
	assert.Equal(t, []string{"domain", "action", "reason"}, e.AuditEvent.Val.AttrKeys)
	assert.Equal(t, "dmdm", e.AuditEvent.Val.AttrValues[0])
	assert.Equal(t, "Not A Name", e.AuditEvent.Val.AttrValues[1])
	assert.Contains(t, e.AuditEvent.Val.AttrValues[2], "action")

	// Each row has a natural key and an id of its own.
	ids := map[uint64]bool{}
	for _, e := range ents {
		ids[e.ID] = true
		assert.NotEmpty(t, e.NaturalKey)
	}
	assert.Len(t, ids, 3)
}

// outageExec is a server that refuses inserts while down.
type outageExec struct {
	recordstore.ExecutorI
	down atomic.Bool
}

func (inst *outageExec) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) error {
	if inst.down.Load() {
		return errors.New("server down")
	}
	return inst.ExecutorI.InsertArrow(ctx, table, records)
}

// Over clickhouse-local: the audit-gap row the recorder writes after an
// outage reads back with the count and the window of what was lost.
func TestGapRowReadsBack(t *testing.T) {
	local, ctx := localFacts(t)
	exec := &outageExec{ExecutorI: local}
	rec := trail.NewRecorder(exec, "run-9", zerolog.Nop())
	defer rec.Close()
	// The cap is internal; three batches of 30_000 rows would be slow, so
	// the window is asserted on what the counters and the row agree on.
	exec.down.Store(true)
	at := time.Unix(1_700_000_000, 0).UTC()
	for i := range 3 {
		require.NoError(t, rec.Event(ctx, at.Add(time.Duration(i)*time.Minute), trail.Context{}, trail.AuditEvent{
			Domain: "dmdm", Action: "vault-resolve", Outcome: trail.OutcomeOk, PrincipalBy: trail.PrincipalByNone, Subject: uint64(i), Retention: "disclosure"}))
		require.Error(t, rec.Flush(ctx))
	}
	// Force the cap: drop everything held but the newest batch.
	dropped := rec.DropHeldForTest(1)
	require.Equal(t, 2, dropped)
	exec.down.Store(false)
	require.NoError(t, rec.Flush(ctx))
	require.NoError(t, rec.Flush(ctx))

	ents := scanEvents(t, rec, ctx, recordstore.ScanOpts{})
	var gapRow *trail.AuditEvent
	kept := 0
	for _, e := range ents {
		switch e.AuditEvent.Val.Action {
		case trail.ActionAuditGap:
			v := e.AuditEvent.Val
			gapRow = &v
		case "vault-resolve":
			kept++
		}
	}
	assert.Equal(t, 1, kept, "the newest batch landed")
	require.NotNil(t, gapRow, "the gap is a row")
	assert.Equal(t, trail.TrailDomain, gapRow.Domain)
	assert.Equal(t, trail.OutcomeFailed, gapRow.Outcome)
	assert.Equal(t, trail.PrincipalByNone, gapRow.PrincipalBy)
	assert.Equal(t, []string{"rows", "from", "to"}, gapRow.AttrKeys)
	assert.Equal(t, "2", gapRow.AttrValues[0])
	assert.Equal(t, at.Format(time.RFC3339Nano), gapRow.AttrValues[1])
	assert.Equal(t, at.Add(time.Minute).Format(time.RFC3339Nano), gapRow.AttrValues[2])
	assert.EqualValues(t, 2, rec.Counts().Dropped)
}

// Over clickhouse-local: the read helpers return one principal's and one
// subject's events within a window, oldest first, under a limit, and a row
// that carries the looked-for value in another slot is not returned.
func TestReadHelpersByPrincipalAndSubject(t *testing.T) {
	exec, ctx := localFacts(t)
	rec := trail.NewRecorder(exec, "run-9", zerolog.Nop())
	defer rec.Close()
	at := time.Unix(1_700_000_000, 0).UTC()
	withP := func(p string) context.Context {
		return callident.WithClaims(ctx, callident.Claims{Principal: p})
	}
	ev := func(action string, subject uint64, refs ...string) trail.AuditEvent {
		e := trail.AuditEvent{Domain: "dmdm", Action: action, Outcome: trail.OutcomeOk, PrincipalBy: trail.PrincipalByEnv, Subject: subject, Retention: "disclosure"}
		for i := 0; i+1 < len(refs); i += 2 {
			e.RefTypes = append(e.RefTypes, refs[i])
			e.RefValues = append(e.RefValues, refs[i+1])
		}
		return e
	}
	require.NoError(t, rec.Event(withP("p-1"), at, trail.Context{}, ev("vault-resolve", 42)))
	require.NoError(t, rec.Event(withP("p-1"), at.Add(time.Minute), trail.Context{}, ev("vault-resolve", 43)))
	require.NoError(t, rec.Event(withP("p-2"), at.Add(2*time.Minute), trail.Context{}, ev("erase-issue", 42)))
	// p-1 appears as a reference value, not as the principal.
	require.NoError(t, rec.Event(withP("p-2"), at.Add(3*time.Minute), trail.Context{}, ev("share-peer", 44, "peer", "p-1")))
	require.NoError(t, rec.Event(withP("p-1"), at.Add(time.Hour), trail.Context{}, ev("vault-resolve", 42)))
	require.NoError(t, rec.Flush(ctx))

	actions := func(ents []*trail.TrailEntity) (out []string) {
		for _, e := range ents {
			out = append(out, e.AuditEvent.Val.Action+"/"+strconv.FormatUint(e.AuditEvent.Val.Subject, 10)+"@"+e.Ts.Sub(at).String())
		}
		return
	}
	ents, err := rec.EventsByPrincipal(ctx, "p-1", time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"vault-resolve/42@0s", "vault-resolve/43@1m0s", "vault-resolve/42@1h0m0s"}, actions(ents), "oldest first; the reference to p-1 is not p-1's event")

	ents, err = rec.EventsByPrincipal(ctx, "p-1", at.Add(30*time.Second), at.Add(time.Hour), 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"vault-resolve/43@1m0s"}, actions(ents), "the window is half-open")

	ents, err = rec.EventsByPrincipal(ctx, "p-1", time.Time{}, time.Time{}, 2)
	require.NoError(t, err)
	assert.Len(t, ents, 2, "the limit holds")

	ents, err = rec.EventsBySubject(ctx, 42, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"vault-resolve/42@0s", "erase-issue/42@2m0s", "vault-resolve/42@1h0m0s"}, actions(ents))

	ents, err = rec.EventsBySubject(ctx, 42, at.Add(time.Minute), time.Time{}, 1)
	require.NoError(t, err)
	assert.Equal(t, []string{"erase-issue/42@2m0s"}, actions(ents))

	_, err = rec.EventsBySubject(ctx, 0, time.Time{}, time.Time{}, 0)
	assert.Error(t, err, "subject 0 is refused")

	ents, err = rec.EventsByPrincipal(ctx, "nobody", time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	assert.Empty(t, ents)
	var none *trail.Recorder
	ents, err = none.EventsBySubject(ctx, 42, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	assert.Nil(t, ents)
}
