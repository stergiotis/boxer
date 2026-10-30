package trail_test

import (
	"context"
	"errors"
	"iter"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
)

// Over clickhouse-local: law L34 holds over a run's rows as they read
// back — the seals chain and every row is named once — and a row of the
// trail's kinds the run carries that no recorder sealed is reported as
// unnamed, not as a break. Rows of other kinds under the same origin —
// every writer of boxer.facts carries one — are not the trail's and are
// not reported (ADR-0327's Update of 2026-10-30).
func TestSealsHoldOverTheRowsAsTheyReadBack(t *testing.T) {
	exec, ctx := localFacts(t)
	anchors := anchor.NewMemStore()
	rec := trail.NewRecorder(exec, "run-seal", zerolog.Nop(), trail.WithAnchors(anchors))
	t.Cleanup(rec.Close)
	e := trail.AuditEvent{Domain: "dmdm", Action: "vault-resolve", Outcome: trail.OutcomeOk, PrincipalBy: trail.PrincipalByNone, Subject: 7, Retention: "disclosure"}
	require.NoError(t, rec.Event(ctx, time.Now(), trail.Context{Origin: rec.OriginOf("apps/dmdm", 1)}, e))
	require.NoError(t, rec.LlmCall(time.Now(), trail.Context{}, trail.LlmCall{CallId: "c1"}))
	require.NoError(t, rec.Flush(ctx))
	require.NoError(t, rec.Event(ctx, time.Now(), trail.Context{}, e))
	require.NoError(t, rec.Flush(ctx))

	broken, unnamed, unanchored, err := rec.VerifySeals(ctx, "run-seal")
	require.NoError(t, err)
	assert.Equal(t, evidence.SealBreak{}, broken)
	assert.Empty(t, unnamed)
	assert.Empty(t, unanchored)
	assert.EqualValues(t, 2, rec.Counts().Seals)

	// Rows of other writers under the run's origin are not the trail's:
	// an origin-only row, a metrics row and a query run.
	st := trail.NewTrailStore(exec, nil, trail.TrailStoreConfig{})
	t.Cleanup(st.Close)
	b := st.Begin(97, time.Now().UTC(), trail.TrailEnvelope{NaturalKey: []byte("origin-only")})
	b.AddOrigin(trail.Origin{Id: 97, Run: "run-seal"})
	require.NoError(t, b.Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)
	sysm := sysmfacts.NewSysmetricsStore(exec, nil, sysmfacts.SysmetricsStoreConfig{})
	t.Cleanup(sysm.Close)
	require.NoError(t, sysm.Begin(98, time.Now().UTC(), sysmfacts.SysmetricsEnvelope{NaturalKey: []byte("host-a")}).
		AddOrigin(sysmfacts.Origin{Id: 98, Run: "run-seal"}).
		AddSysMem(sysmfacts.SysMem{Id: 98, NaturalKey: []byte("host-a"), Kind: "sysMem", Host: "host-a"}).Commit())
	_, err = sysm.Flush(ctx)
	require.NoError(t, err)
	ent := dml.NewInEntityFacts(memory.NewGoAllocator(), 1)
	require.NoError(t, queryrunfacts.BuildEntities(ent, []queryrunfacts.Row{{
		Type: "QueryFinish", EventUs: time.Now().UnixMicro(), QueryId: "q-seal", Query: "SELECT 1", QueryKind: "Select",
		LogComment: `{"run_id":"run-seal","app":"apps/play","instance":3}`}}))
	records, err := ent.TransferRecords(nil)
	require.NoError(t, err)
	require.NoError(t, exec.InsertArrow(ctx, trail.TrailTableName, records))
	for _, r := range records {
		r.Release()
	}
	origins, err := rec.Scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanOrigin(ctx, recordstore.ScanOpts{})
	})
	require.NoError(t, err)
	foreign := 0
	for _, e := range origins {
		if e.ID == 97 || e.ID == 98 || (!e.EvidenceSeal.Has && !e.AuditEvent.Has && !e.LlmCall.Has) {
			foreign++
		}
	}
	assert.Equal(t, 3, foreign, "the three rows read as Origin")
	broken, unnamed, unanchored, err = rec.VerifySeals(ctx, "run-seal")
	require.NoError(t, err)
	assert.Equal(t, evidence.SealBreak{}, broken)
	assert.Empty(t, unnamed, "a row of another kind under the run's origin is not the trail's")
	assert.Empty(t, unanchored)

	// A row of the trail's kinds written past the recorder, in the run, is
	// a gap.
	b = st.Begin(99, time.Now().UTC(), trail.TrailEnvelope{NaturalKey: []byte("stray")})
	b.AddOrigin(trail.Origin{Id: 99, Run: "run-seal"}).AddAuditEvent(e)
	require.NoError(t, b.Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)
	broken, unnamed, unanchored, err = rec.VerifySeals(ctx, "run-seal")
	require.NoError(t, err)
	assert.Equal(t, evidence.SealBreak{}, broken)
	assert.Equal(t, []uint64{99}, unnamed)
	assert.Empty(t, unanchored)

	// An anchor the stream does not reproduce breaks its ordinal (L32).
	_, err = anchors.Append(ctx, anchor.Head{Scope: trail.SealScope("run-seal"), Ordinal: 3, Scheme: trail.TrailSealScheme.Name(), Digest: "forged"})
	require.NoError(t, err)
	_, _, unanchored, err = rec.VerifySeals(ctx, "run-seal")
	require.NoError(t, err)
	assert.Equal(t, []uint64{3}, unanchored)

	// Another run's rows are not this stream's.
	broken, unnamed, unanchored, err = rec.VerifySeals(ctx, "run-other")
	require.NoError(t, err)
	assert.Equal(t, evidence.SealBreak{}, broken)
	assert.Empty(t, unnamed)
	assert.Empty(t, unanchored)
}

// The trail's binding passes the shared writer's conformance test on
// clickhouse-local (ADR-0330 Verification plan).
func TestTrailConformance(t *testing.T) {
	exec, _ := localFacts(t)
	trail.ConformanceForTest(t, exec)
}

// lostAckExec stores inserts and, while lostAcks is positive, reports
// them failed, as a lost acknowledgement does.
type lostAckExec struct {
	recordstore.ExecutorI
	lostAcks atomic.Int32
}

func (inst *lostAckExec) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) error {
	if err := inst.ExecutorI.InsertArrow(ctx, table, records); err != nil {
		return err
	}
	if inst.lostAcks.Add(-1) >= 0 {
		return errors.New("acknowledgement lost")
	}
	return nil
}

// Over clickhouse-local: a batch the server stored but whose
// acknowledgement was lost is retried and so stored twice; the reads
// return its event once and the seal law holds, the seal read twice
// counting once rather than as a fork (ADR-0330 §SD7).
func TestRetriedBatchIsNotAFork(t *testing.T) {
	local, ctx := localFacts(t)
	exec := &lostAckExec{ExecutorI: local}
	exec.lostAcks.Store(1)
	anchors := anchor.NewMemStore()
	rec := trail.NewRecorder(exec, "run-retry", zerolog.Nop(), trail.WithAnchors(anchors))
	t.Cleanup(rec.Close)
	e := trail.AuditEvent{Domain: "dmdm", Action: "vault-resolve", Outcome: trail.OutcomeOk, PrincipalBy: trail.PrincipalByNone, Subject: 7, Retention: "disclosure"}
	require.NoError(t, rec.Event(ctx, time.Now(), trail.Context{}, e))
	require.Error(t, rec.Flush(ctx), "the acknowledgement is lost")
	require.NoError(t, rec.Flush(ctx), "the retry lands")
	ents, err := rec.EventsBySubject(ctx, 7, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	assert.Len(t, ents, 1, "an event stored twice reads once")
	broken, unnamed, unanchored, err := rec.VerifySeals(ctx, "run-retry")
	require.NoError(t, err)
	assert.Equal(t, evidence.SealBreak{}, broken, "a seal stored twice is not a fork")
	assert.Empty(t, unnamed)
	assert.Empty(t, unanchored)
	assert.Len(t, anchors.All(), 1)
}
