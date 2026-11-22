package auditrec_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec/auditrectest"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec/testdata/example"
)

// A held recorder keeps its rows and sends nothing, even to a server that
// would take them; a write-ahead fails with the hold as its cause. Release
// against a server that is down leaves it held; once the server answers,
// Release lands everything held, sealed, and the recorder is durable.
func TestHeldRecorderSendsNothingUntilReleased(t *testing.T) {
	exec := &gateExec{open: true}
	fwd := &auditrectest.Collector{}
	rec := newRecorder(t, exec, auditrec.WithHeld(), auditrec.WithRequired(true), auditrec.WithForwarder(fwd))
	t.Cleanup(rec.Close)
	assert.False(t, rec.Durable())
	assert.True(t, rec.Held())

	require.NoError(t, rec.Event(context.Background(), time.Now(), auditrectest.FullEvent(1)))
	assert.ErrorIs(t, rec.Flush(context.Background()), auditrec.ErrHeld)
	durable, err := rec.EventWriteAhead(context.Background(), time.Now(), auditrectest.FullEvent(2))
	assert.False(t, durable)
	assert.ErrorIs(t, err, auditrec.ErrNotDurable)
	assert.ErrorIs(t, err, auditrec.ErrHeld)
	assert.Zero(t, exec.landed(), "nothing leaves while held")

	exec.set(false)
	require.Error(t, rec.Release(context.Background()))
	assert.True(t, rec.Held(), "a failed release keeps the hold")
	assert.False(t, rec.Durable())

	exec.set(true)
	require.NoError(t, rec.Release(context.Background()))
	assert.False(t, rec.Held())
	assert.True(t, rec.Durable())
	assert.EqualValues(t, 2, rec.Counts().Written)
	assert.EqualValues(t, 2, rec.Counts().Seals, "the held flush and the write-ahead each kept a batch")
	assert.EqualValues(t, 4, exec.landed(), "two rows and their seals")
	require.Eventually(t, func() bool { return len(fwd.Events()) == 2 }, 5*time.Second, 10*time.Millisecond,
		"the held events are forwarded once they land")

	durable, err = rec.EventWriteAhead(context.Background(), time.Now(), auditrectest.FullEvent(3))
	require.NoError(t, err)
	assert.True(t, durable)
	require.NoError(t, rec.Release(context.Background()), "a recorder no longer held releases nothing")
}

// Past the backlog cap a held recorder drops its oldest batches as one
// that cannot land does, and the gap row follows the release.
func TestHeldRecorderKeepsTheBacklogCap(t *testing.T) {
	exec := &gateExec{open: true}
	fwd := &auditrectest.Collector{}
	rec := newRecorder(t, exec, auditrec.WithHeld(), auditrec.WithMaxBacklog(1), auditrec.WithForwarder(fwd))
	first := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	require.NoError(t, rec.Event(context.Background(), first, auditrectest.FullEvent(1)))
	assert.ErrorIs(t, rec.Flush(context.Background()), auditrec.ErrHeld)
	require.NoError(t, rec.Event(context.Background(), first.Add(time.Minute), auditrectest.FullEvent(2)))
	assert.ErrorIs(t, rec.Flush(context.Background()), auditrec.ErrHeld)
	assert.EqualValues(t, 1, rec.Counts().Dropped)

	require.NoError(t, rec.Release(context.Background()))
	require.NoError(t, rec.Flush(context.Background()))
	rec.Close()
	events := fwd.Events()
	require.Len(t, events, 2)
	assert.EqualValues(t, 2, events[0].Row.Subject, "the newest held event")
	assert.Equal(t, audit.ActionAuditGap, events[1].Row.Action)
}

// A recorder closed while held loses what it holds, counted; Release
// after Close says so.
func TestHeldRecorderClosedLosesItsRows(t *testing.T) {
	exec := &gateExec{open: true}
	rec := newRecorder(t, exec, auditrec.WithHeld())
	require.NoError(t, rec.Event(context.Background(), time.Now(), auditrectest.FullEvent(1)))
	rec.Close()
	assert.EqualValues(t, 1, rec.Counts().Dropped)
	assert.Zero(t, exec.landed())
	assert.False(t, rec.Held())
	assert.True(t, errors.Is(rec.Release(context.Background()), auditrec.ErrClosed))
}

// Without a store there is nothing to hold.
func TestHeldWithoutAStoreIsNotHeld(t *testing.T) {
	rec, err := auditrec.New[*example.Batch](nil, "run-1", auditrectest.Scheme, zerolog.Nop(), auditrec.WithHeld())
	require.NoError(t, err)
	t.Cleanup(rec.Close)
	assert.False(t, rec.Held())
	assert.False(t, rec.Durable())
	assert.Error(t, rec.Release(context.Background()))
}
