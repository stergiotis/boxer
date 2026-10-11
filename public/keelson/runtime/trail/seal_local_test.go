package trail_test

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// Over clickhouse-local: law L34 holds over a run's rows as they read
// back — the seals chain and every row is named once — and a row the run
// carries that no recorder sealed is reported as unnamed, not as a break.
func TestSealsHoldOverTheRowsAsTheyReadBack(t *testing.T) {
	exec, ctx := localFacts(t)
	rec := trail.NewRecorder(exec, "run-seal", zerolog.Nop())
	t.Cleanup(rec.Close)
	e := trail.AuditEvent{Domain: "dmdm", Action: "vault-resolve", Outcome: trail.OutcomeOk, PrincipalBy: trail.PrincipalByNone, Subject: 7, Retention: "disclosure"}
	require.NoError(t, rec.Event(ctx, time.Now(), trail.Context{Origin: rec.OriginOf("apps/dmdm", 1)}, e))
	require.NoError(t, rec.LlmCall(time.Now(), trail.Context{}, trail.LlmCall{CallId: "c1"}))
	require.NoError(t, rec.Flush(ctx))
	require.NoError(t, rec.Event(ctx, time.Now(), trail.Context{}, e))
	require.NoError(t, rec.Flush(ctx))

	broken, unnamed, err := rec.VerifySeals(ctx, "run-seal")
	require.NoError(t, err)
	assert.Equal(t, evidence.SealBreak{}, broken)
	assert.Empty(t, unnamed)
	assert.EqualValues(t, 2, rec.Counts().Seals)

	// A row written past the recorder, in the run, is a gap.
	st := trail.NewTrailStore(exec, nil, trail.TrailStoreConfig{})
	t.Cleanup(st.Close)
	b := st.Begin(99, time.Now().UTC(), trail.TrailEnvelope{NaturalKey: []byte("stray")})
	b.AddOrigin(trail.Origin{Id: 99, Run: "run-seal"})
	require.NoError(t, b.Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)
	broken, unnamed, err = rec.VerifySeals(ctx, "run-seal")
	require.NoError(t, err)
	assert.Equal(t, evidence.SealBreak{}, broken)
	assert.Equal(t, []uint64{99}, unnamed)

	// Another run's rows are not this stream's.
	broken, unnamed, err = rec.VerifySeals(ctx, "run-other")
	require.NoError(t, err)
	assert.Equal(t, evidence.SealBreak{}, broken)
	assert.Empty(t, unnamed)
}
