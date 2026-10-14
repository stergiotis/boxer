package auditrec_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec/auditrectest"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec/testdata/example"
)

// Each flush the server takes carries one seal; a row outside the bounds
// is replaced by the invalid row, counted, and the call does not fail.
func TestFlushesAreSealedAndInvalidRowsReplaced(t *testing.T) {
	exec := &gateExec{open: true}
	fwd := &auditrectest.Collector{}
	rec := newRecorder(t, exec, auditrec.WithForwarder(fwd))
	bad := auditrectest.FullEvent(1)
	bad.Domain = ""
	require.NoError(t, rec.Event(context.Background(), time.Now(), bad))
	require.NoError(t, rec.Flush(context.Background()))
	require.NoError(t, rec.Event(context.Background(), time.Now(), auditrectest.FullEvent(2)))
	require.NoError(t, rec.Flush(context.Background()))
	require.NoError(t, rec.Flush(context.Background()), "an empty flush seals nothing")
	rec.Close()
	c := rec.Counts()
	assert.EqualValues(t, 2, c.Buffered)
	assert.EqualValues(t, 2, c.Written)
	assert.EqualValues(t, 1, c.Invalid)
	assert.EqualValues(t, 2, c.Seals)
	assert.EqualValues(t, 4, exec.landed(), "two rows and two seals")
	require.Len(t, fwd.Events(), 2)
	assert.Equal(t, audit.ActionAuditInvalid, fwd.Events()[0].Row.Action)
}

// The prompt path forwards an event only once its batch landed; past the
// backlog cap the oldest batch is dropped, and the flush that next lands
// everything writes the gap row and lands it too.
func TestGapRowLandsInTheFlushThatFoundIt(t *testing.T) {
	exec := &gateExec{}
	fwd := &auditrectest.Collector{}
	rec := newRecorder(t, exec, auditrec.WithForwarder(fwd), auditrec.WithMaxBacklog(1), auditrec.WithNode("cell-a"),
		auditrec.WithOriginBy(evidence.OriginByHost))
	first := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	require.NoError(t, rec.Event(context.Background(), first, auditrectest.FullEvent(1)))
	require.Error(t, rec.Flush(context.Background()))
	require.NoError(t, rec.Event(context.Background(), first.Add(time.Minute), auditrectest.FullEvent(2)))
	require.Error(t, rec.Flush(context.Background()), "past the cap the oldest batch is dropped")
	assert.Empty(t, fwd.Events(), "nothing is forwarded before it is durable")
	assert.EqualValues(t, 1, rec.Counts().Dropped)
	exec.set(true)
	require.NoError(t, rec.Flush(context.Background()), "the held batch and the gap row land")
	assert.EqualValues(t, 4, exec.landed(), "the held row, the gap row, and a seal each")
	rec.Close()
	events := fwd.Events()
	require.Len(t, events, 2)
	assert.EqualValues(t, 2, events[0].Row.Subject, "the held event")
	gap := events[1].Row
	assert.Equal(t, audit.ActionAuditGap, gap.Action)
	assert.Equal(t, []string{"1", first.Format(time.RFC3339Nano), first.Format(time.RFC3339Nano)}, gap.AttrValues)
	assert.Equal(t, "host", gap.OriginBy)
	assert.Equal(t, "cell-a", gap.Node.Val)
	assert.Equal(t, "run-1", events[1].Origin.Run)
}

// A write-ahead says whether the event landed; WithRequired turns a
// failure into ErrNotDurable, which the caller must not act past.
func TestWriteAhead(t *testing.T) {
	exec := &gateExec{}
	rec := newRecorder(t, exec, auditrec.WithRequired(true))
	durable, err := rec.EventWriteAhead(context.Background(), time.Now(), auditrectest.FullEvent(1))
	assert.False(t, durable)
	require.ErrorIs(t, err, auditrec.ErrNotDurable)
	exec.set(true)
	durable, err = rec.EventWriteAhead(context.Background(), time.Now(), auditrectest.FullEvent(2))
	require.NoError(t, err)
	assert.True(t, durable)
	assert.EqualValues(t, 2, rec.Counts().Written, "the refused event stayed buffered and landed with the next")
	rec.Close()

	lax := newRecorder(t, &gateExec{})
	durable, err = lax.EventWriteAhead(context.Background(), time.Now(), auditrectest.FullEvent(3))
	require.NoError(t, err, "without WithRequired the caller proceeds")
	assert.False(t, durable)
	lax.Close()
}

// Close lands what it can, counts what it cannot as lost, and refuses
// verbs afterwards.
func TestClose(t *testing.T) {
	exec := &gateExec{}
	rec := newRecorder(t, exec)
	require.NoError(t, rec.Event(context.Background(), time.Now(), auditrectest.FullEvent(1)))
	rec.Close()
	rec.Close()
	assert.EqualValues(t, 1, rec.Counts().Dropped, "the row the server never took is lost")
	assert.False(t, rec.Durable())
	require.ErrorIs(t, rec.Event(context.Background(), time.Now(), auditrectest.FullEvent(2)), auditrec.ErrClosed)
	_, err := rec.EventWriteAhead(context.Background(), time.Now(), auditrectest.FullEvent(3))
	require.ErrorIs(t, err, auditrec.ErrClosed)
	_, err = rec.EventsBySubject(context.Background(), 1, auditrec.Window{})
	require.ErrorIs(t, err, auditrec.ErrClosed)

	up := &gateExec{open: true}
	rec = newRecorder(t, up)
	require.NoError(t, rec.Event(context.Background(), time.Now(), auditrectest.FullEvent(1)))
	rec.Close()
	assert.EqualValues(t, 2, up.landed(), "Close's flush lands the row and its seal")
	assert.Zero(t, rec.Counts().Dropped)
}

// A nil recorder and one without a store record nothing and are not
// durable; a write-ahead under WithRequired then refuses.
func TestWithoutAStore(t *testing.T) {
	var none *auditrec.Recorder[*example.Batch]
	require.NoError(t, none.Event(context.Background(), time.Now(), auditrectest.FullEvent(1)))
	assert.False(t, none.Durable())
	none.Close()

	rec, err := auditrec.New[*example.Batch](nil, "run-1", auditrectest.Scheme, zerolog.Nop(), auditrec.WithRequired(true))
	require.NoError(t, err)
	require.NoError(t, rec.Event(context.Background(), time.Now(), auditrectest.FullEvent(1)))
	assert.False(t, rec.Durable())
	_, err = rec.WriteAhead(context.Background())
	require.ErrorIs(t, err, auditrec.ErrNotDurable)
	rec.Close()

	_, err = auditrec.New[*example.Batch](nil, "", auditrectest.Scheme, zerolog.Nop())
	require.Error(t, err, "a run is required")
	_, err = auditrec.New[*example.Batch](nil, "run-1", evidence.Scheme{}, zerolog.Nop())
	require.Error(t, err, "a scheme is required")
}

// Write shares the buffer and the seal with rows the recorder does not
// build itself; a row the batch refuses is counted and the call fails.
func TestWrite(t *testing.T) {
	exec := &gateExec{open: true}
	rec := newRecorder(t, exec)
	id, err := rec.Write(context.Background(), "own-row", time.Now(), func(b *example.Batch, id uint64) ([]byte, error) {
		row := auditrectest.FullEvent(5)
		row.Id, row.Kind, row.OriginBy, row.PrincipalBy, row.PrincipalVia = id, "auditEvent", "self", audit.PrincipalByNone, audit.AuditEvent{}.PrincipalVia
		return b.Event(context.Background(), id, time.Now(), "own-row", audit.Origin{Id: id, Run: "run-1"}, row)
	})
	require.NoError(t, err)
	assert.NotZero(t, id)
	_, err = rec.Write(context.Background(), "refused", time.Now(), func(*example.Batch, uint64) ([]byte, error) {
		return nil, errors.New("refused")
	})
	require.Error(t, err)
	require.NoError(t, rec.Flush(context.Background()))
	rec.Close()
	c := rec.Counts()
	assert.EqualValues(t, 1, c.Written)
	assert.EqualValues(t, 1, c.Dropped)
	assert.EqualValues(t, 1, c.Seals)
}
