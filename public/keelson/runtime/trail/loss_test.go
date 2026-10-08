package trail

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A forced outage past the backlog cap drops the oldest batches; the first
// flush that lands everything afterwards writes one audit-gap row, and the
// counters say what was buffered, written and dropped (ADR-0296 §SD4).
func TestGapRowAfterRecovery(t *testing.T) {
	exec := &gateExec{}
	rec := NewRecorder(exec, "run-1", zerolog.Nop())
	defer rec.Close()
	rec.maxBacklog = 25
	const batch, batches = 10, 5
	for i := range batches {
		for range batch {
			require.NoError(t, rec.HttpFetch(time.Now(), Context{}, HttpFetch{}))
		}
		require.Error(t, rec.Flush(context.Background()), "batch %d is held", i)
	}
	c := rec.Counts()
	assert.EqualValues(t, batch*batches, c.Buffered)
	assert.EqualValues(t, 0, c.Written)
	assert.EqualValues(t, 30, c.Dropped, "three batches of ten were dropped to hold at most 25 rows")
	rec.flushMu.Lock()
	assert.Equal(t, 30, rec.gap.rows)
	assert.False(t, rec.gap.first.IsZero())
	assert.False(t, rec.gap.last.Before(rec.gap.first))
	rec.flushMu.Unlock()

	exec.mu.Lock()
	exec.open = true
	exec.mu.Unlock()
	require.NoError(t, rec.Flush(context.Background()), "the held batches land")
	assert.EqualValues(t, 20, exec.landed())
	rec.flushMu.Lock()
	assert.Equal(t, 0, rec.gap.rows, "the gap is written back once")
	rec.flushMu.Unlock()
	require.NoError(t, rec.Flush(context.Background()), "the gap row lands with the next flush")
	assert.EqualValues(t, 21, exec.landed(), "twenty held rows and one gap row")
	c = rec.Counts()
	assert.EqualValues(t, batch*batches+1, c.Buffered, "the gap row is buffered like any row")
	assert.EqualValues(t, 21, c.Written)
	assert.EqualValues(t, 30, c.Dropped)
	assert.EqualValues(t, 0, c.Invalid)
	require.NoError(t, rec.Flush(context.Background()))
	assert.EqualValues(t, 21, exec.landed(), "no second gap row")
}

// Close under a server that stays down counts what it loses: the held
// batches and what was buffered since.
func TestCloseCountsWhatItLoses(t *testing.T) {
	exec := &gateExec{}
	rec := NewRecorder(exec, "run-1", zerolog.Nop())
	require.NoError(t, rec.HttpFetch(time.Now(), Context{}, HttpFetch{}))
	require.NoError(t, rec.HttpFetch(time.Now(), Context{}, HttpFetch{}))
	require.Error(t, rec.Flush(context.Background()))
	require.NoError(t, rec.HttpFetch(time.Now(), Context{}, HttpFetch{}))
	rec.Close()
	c := rec.Counts()
	assert.EqualValues(t, 3, c.Buffered)
	assert.EqualValues(t, 0, c.Written)
	assert.EqualValues(t, 3, c.Dropped, "two held and one buffered are lost at close")
}

// An invalid event counts, and a clean run counts nothing dropped.
func TestCountsInvalidAndClean(t *testing.T) {
	exec := &gateExec{open: true}
	rec := NewRecorder(exec, "run-1", zerolog.Nop())
	require.NoError(t, rec.Event(context.Background(), time.Now(), Context{}, AuditEvent{Domain: "Bad Domain"}))
	require.NoError(t, rec.Event(context.Background(), time.Now(), Context{}, event()))
	rec.Close()
	c := rec.Counts()
	assert.Equal(t, Counts{Buffered: 2, Written: 2, Dropped: 0, Invalid: 1}, c)
	assert.Equal(t, Counts{}, (*Recorder)(nil).Counts())
}
