package auditrec_test

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec/auditrectest"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec/testdata/example"
)

// The example binding passes the conformance test on clickhouse-local.
func TestExampleConformance(t *testing.T) {
	exec := localExec(t)
	auditrectest.Conformance(t, func(t *testing.T) auditrec.StoreI[*example.Batch] { return exampleStore(t, exec) })
}

// A batch the server stored but whose acknowledgement was lost is retried
// and so stored twice: the reads return each event once and the seal law
// holds, the seal read twice counting once; the anchored seals match.
func TestRetriedBatchVerifies(t *testing.T) {
	inner := localExec(t)
	exec := &gateExec{open: true, lostAcks: 1, inner: inner}
	anchors := anchor.NewMemStore()
	rec := newRecorder(t, exec, auditrec.WithAnchors(anchors, "cell-a"))
	defer rec.Close()
	ctx := context.Background()
	at := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	require.NoError(t, rec.Event(ctx, at, auditrectest.FullEvent(7)))
	require.Error(t, rec.Flush(ctx), "the acknowledgement is lost")
	require.NoError(t, rec.Flush(ctx), "the retry lands")
	assert.EqualValues(t, 4, exec.landed(), "the event and its seal, twice")
	got, err := rec.EventsBySubject(ctx, 7, auditrec.Window{})
	require.NoError(t, err)
	assert.Len(t, got, 1, "an event stored twice reads once")
	broken, unnamed, unanchored, err := rec.VerifySeals(ctx, "run-1")
	require.NoError(t, err)
	assert.Equal(t, evidence.SealBreakNone, broken.Why, "a seal stored twice is not a fork: %+v", broken)
	assert.Empty(t, unnamed)
	assert.Empty(t, unanchored)
	assert.Len(t, anchors.All(), 1, "one durable seal, anchored once")
}

// With a node, the reads and the backstop see only that node's rows of a
// shared table.
func TestNodeScopesReadsAndBackstop(t *testing.T) {
	exec := localExec(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 13, 11, 0, 0, 0, time.UTC)
	fa, fb := &auditrectest.Collector{}, &auditrectest.Collector{}
	a := newRecorder(t, exec, auditrec.WithNode("cell-a"), auditrec.WithForwarder(fa))
	b, err := auditrec.New[*example.Batch](exampleStore(t, exec), "run-2", auditrectest.Scheme, zerolog.Nop(),
		auditrec.WithNode("cell-b"), auditrec.WithForwarder(fb), auditrec.WithFlushInterval(0))
	require.NoError(t, err)
	defer a.Close()
	defer b.Close()
	ea, eb := auditrectest.FullEvent(9), auditrectest.FullEvent(9)
	ea.Node, eb.Node = audit.AuditEvent{}.Node, audit.AuditEvent{}.Node
	require.NoError(t, a.Event(ctx, at, ea))
	require.NoError(t, b.Event(ctx, at, eb))
	require.NoError(t, a.Flush(ctx))
	require.NoError(t, b.Flush(ctx))
	got, err := a.EventsBySubject(ctx, 9, auditrec.Window{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "cell-a", got[0].Row.Node.Val, "a row without a node takes the recorder's")

	// A recorder over the same table, as a backstop after a restart:
	// nothing forwarded lately, so it re-sends its node's event only.
	fc := &auditrectest.Collector{}
	c := newRecorder(t, exec, auditrec.WithNode("cell-a"), auditrec.WithForwarder(fc))
	defer c.Close()
	n, err := c.ForwardWindow(ctx, at.Add(-time.Minute), at.Add(time.Minute), 0)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, got[0].Id, fc.Events()[0].Id)

	// The prompt path forwarded it already; the same recorder's backstop
	// skips it.
	require.Eventually(t, func() bool { return len(fa.Events()) == 1 }, 5*time.Second, 10*time.Millisecond)
	n, err = a.ForwardWindow(ctx, at.Add(-time.Minute), at.Add(time.Minute), 0)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "what the prompt path forwarded lately is skipped")

	// CatchUp walks a long interval window by window and reaches its end;
	// an event that landed late with an old time is inside a window.
	fd := &auditrectest.Collector{}
	d := newRecorder(t, exec, auditrec.WithNode("cell-a"), auditrec.WithForwarder(fd))
	defer d.Close()
	reached, n, err := d.CatchUp(ctx, at.Add(-time.Hour), at.Add(time.Hour), 10*time.Minute)
	require.NoError(t, err)
	assert.True(t, reached.Equal(at.Add(time.Hour)))
	assert.Equal(t, 1, n)
}
