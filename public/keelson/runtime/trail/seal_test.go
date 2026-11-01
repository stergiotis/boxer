package trail

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
)

// Every durable flush carries one seal, chained to the one before it; a
// batch retried after a failed flush keeps its seal rather than taking a
// second, and a flush that lands nothing seals nothing (ADR-0327 §SD5).
func TestEveryDurableFlushIsSealedOnce(t *testing.T) {
	exec := &gateExec{open: true}
	anchors := anchor.NewMemStore()
	obs := newKeysObserver()
	rec := NewRecorder(exec, "run-1", zerolog.Nop(), WithAnchors(anchors), WithWriteObserver(obs))
	ctx := context.Background()
	require.NoError(t, rec.Event(ctx, time.Now(), Context{}, event()))
	require.NoError(t, rec.LlmCall(time.Now(), Context{}, LlmCall{CallId: "c1"}))
	require.NoError(t, rec.Flush(ctx))
	assert.EqualValues(t, 3, exec.landed(), "two rows and their seal")
	require.Len(t, anchors.All(), 1)
	first := anchors.All()[0].Head
	assert.NotEmpty(t, first.Digest)
	require.NoError(t, rec.Flush(ctx))
	assert.EqualValues(t, 3, exec.landed(), "an empty flush seals nothing")
	assert.Len(t, anchors.All(), 1)

	exec.mu.Lock()
	exec.open = false
	exec.mu.Unlock()
	require.NoError(t, rec.Event(ctx, time.Now(), Context{}, event()))
	require.Error(t, rec.Flush(ctx))
	obs.mu.Lock()
	require.Len(t, obs.committed, 5, "the held batch's row and its seal are in the batch before the flush")
	held := obs.committed[4].Key
	obs.mu.Unlock()
	assert.Len(t, anchors.All(), 1, "a seal the server did not take is not anchored")

	exec.mu.Lock()
	exec.open = true
	exec.mu.Unlock()
	require.NoError(t, rec.Flush(ctx))
	assert.EqualValues(t, 5, exec.landed(), "the retried batch lands with the seal it had, not a second")
	obs.mu.Lock()
	assert.Len(t, obs.committed, 5, "the retry commits no second seal")
	var durable []uint64
	for _, ks := range obs.durable {
		durable = append(durable, ks...)
	}
	obs.mu.Unlock()
	assert.Contains(t, durable, held)
	rec.Close()
	c := rec.Counts()
	assert.EqualValues(t, 2, c.Seals)
	assert.EqualValues(t, 3, c.Written, "seals are not among the written rows")
	// Each durable seal is the stream's head at its ordinal (ADR-0327 §SD7).
	rows := anchors.All()
	require.Len(t, rows, 2)
	assert.Equal(t, anchor.Head{Scope: SealScope("run-1"), Ordinal: 1, Scheme: TrailSealScheme.Name(), Digest: first.Digest}, rows[0].Head)
	assert.Equal(t, SealScope("run-1"), rows[1].Head.Scope)
	assert.EqualValues(t, 2, rows[1].Head.Ordinal)
	assert.Equal(t, TrailSealScheme.Name(), rows[1].Head.Scheme)
	assert.NotEmpty(t, rows[1].Head.Digest)
	assert.NotEqual(t, first.Digest, rows[1].Head.Digest)
}
