package trail

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every durable flush carries one seal, chained to the one before it; a
// batch retried after a failed flush keeps its seal rather than taking a
// second, and a flush that lands nothing seals nothing (ADR-0327 §SD5).
func TestEveryDurableFlushIsSealedOnce(t *testing.T) {
	exec := &gateExec{open: true}
	rec := NewRecorder(exec, "run-1", zerolog.Nop())
	ctx := context.Background()
	require.NoError(t, rec.Event(ctx, time.Now(), Context{}, event()))
	require.NoError(t, rec.LlmCall(time.Now(), Context{}, LlmCall{CallId: "c1"}))
	require.NoError(t, rec.Flush(ctx))
	assert.EqualValues(t, 3, exec.landed(), "two rows and their seal")
	first := rec.lastSeal
	require.NotZero(t, first.Id)
	assert.NotEmpty(t, first.Digest)
	require.NoError(t, rec.Flush(ctx))
	assert.Equal(t, first, rec.lastSeal, "an empty flush seals nothing")

	exec.mu.Lock()
	exec.open = false
	exec.mu.Unlock()
	require.NoError(t, rec.Event(ctx, time.Now(), Context{}, event()))
	require.Error(t, rec.Flush(ctx))
	rec.flushMu.Lock()
	require.Len(t, rec.backlog, 1)
	held := rec.backlog[0].sealId
	rec.flushMu.Unlock()
	assert.NotZero(t, held, "the seal is in the batch before the flush")
	assert.Equal(t, first, rec.lastSeal, "a seal the server did not take is not the last durable one")

	exec.mu.Lock()
	exec.open = true
	exec.mu.Unlock()
	require.NoError(t, rec.Flush(ctx))
	assert.Equal(t, held, rec.lastSeal.Id, "the retried batch lands with the seal it had")
	assert.EqualValues(t, 5, exec.landed())
	rec.Close()
	c := rec.Counts()
	assert.EqualValues(t, 2, c.Seals)
	assert.EqualValues(t, 3, c.Written, "seals are not among the written rows")
}
