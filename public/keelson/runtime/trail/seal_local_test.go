package trail_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
)

// Over clickhouse-local: law L34 holds over a run's rows as they read
// back — the seals chain and every row is named once — and a row the run
// carries that no recorder sealed is reported as unnamed, not as a break.
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

	// A row written past the recorder, in the run, is a gap.
	st := trail.NewTrailStore(exec, nil, trail.TrailStoreConfig{})
	t.Cleanup(st.Close)
	b := st.Begin(99, time.Now().UTC(), trail.TrailEnvelope{NaturalKey: []byte("stray")})
	b.AddOrigin(trail.Origin{Id: 99, Run: "run-seal"})
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
