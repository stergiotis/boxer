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
// flush that lands everything afterwards writes one audit-gap row and
// lands it in the same flush (ADR-0330 §SD5), and the counters say what
// was buffered, written and dropped (ADR-0296 §SD4).
func TestGapRowAfterRecovery(t *testing.T) {
	exec := &gateExec{}
	fwd := &collectForwarder{}
	rec := NewRecorder(exec, "run-1", zerolog.Nop(), WithMaxBacklogForTest(25), WithForwarder(fwd))
	defer rec.Close()
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

	exec.mu.Lock()
	exec.open = true
	exec.mu.Unlock()
	require.NoError(t, rec.Flush(context.Background()), "the held batches and the gap row land")
	assert.EqualValues(t, 24, exec.landed(), "twenty held rows, one gap row, three seals")
	c = rec.Counts()
	assert.EqualValues(t, batch*batches+1, c.Buffered, "the gap row is buffered like any row")
	assert.EqualValues(t, 21, c.Written)
	assert.EqualValues(t, 30, c.Dropped)
	assert.EqualValues(t, 0, c.Invalid)
	assert.EqualValues(t, 3, c.Seals)
	require.NoError(t, rec.Flush(context.Background()))
	assert.EqualValues(t, 24, exec.landed(), "no second gap row, and an empty flush seals nothing")
	require.Eventually(t, func() bool { return len(fwd.ids()) == 1 }, 5*time.Second, 10*time.Millisecond, "the gap row is forwarded")
	fwd.mu.Lock()
	gap := fwd.got[0].Row
	fwd.mu.Unlock()
	assert.Equal(t, ActionAuditGap, gap.Action)
	require.Equal(t, []string{"rows", "from", "to"}, gap.AttrKeys)
	assert.Equal(t, "30", gap.AttrValues[0], "the gap is written back once, with what was dropped")
	assert.LessOrEqual(t, gap.AttrValues[1], gap.AttrValues[2])
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
	assert.Equal(t, Counts{Buffered: 2, Written: 2, Dropped: 0, Invalid: 1, Seals: 1}, c)
	assert.Equal(t, Counts{}, (*Recorder)(nil).Counts())
}
