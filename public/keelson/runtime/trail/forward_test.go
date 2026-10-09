package trail

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collectForwarder keeps what it is handed, and can refuse.
type collectForwarder struct {
	mu     sync.Mutex
	got    []*TrailEntity
	calls  int
	refuse bool
}

func (inst *collectForwarder) Forward(_ context.Context, events []*TrailEntity) error {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.calls++
	if inst.refuse {
		return errors.New("carrier down")
	}
	inst.got = append(inst.got, events...)
	return nil
}

func (inst *collectForwarder) ids() (ids []uint64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, e := range inst.got {
		ids = append(ids, e.ID)
	}
	return
}

// The prompt path hands the forwarder the audit events of each batch the
// server took, with their components, and nothing else: not the other
// kinds, and not a row until it is durable (ADR-0296 §SD6).
func TestPromptPathForwardsDurableEvents(t *testing.T) {
	exec := &gateExec{}
	fwd := &collectForwarder{}
	rec := NewRecorder(exec, "run-1", zerolog.Nop(), WithForwarder(fwd))
	require.NoError(t, rec.Event(context.Background(), time.Now(), Context{Origin: rec.OriginOf("apps/dmdm", 2)}, event()))
	require.NoError(t, rec.LlmCall(time.Now(), Context{}, LlmCall{CallId: "c1"}))
	require.Error(t, rec.Flush(context.Background()), "the server is down")
	assert.Empty(t, fwd.ids(), "nothing is forwarded before it is durable")
	require.NoError(t, rec.Event(context.Background(), time.Now(), Context{}, event()))
	exec.mu.Lock()
	exec.open = true
	exec.mu.Unlock()
	require.NoError(t, rec.Flush(context.Background()))
	rec.Close()
	ids := fwd.ids()
	assert.Len(t, ids, 2, "the two audit events, once each; the model call is not forwarded")
	assert.NotEqual(t, ids[0], ids[1])
	fwd.mu.Lock()
	first := fwd.got[0]
	fwd.mu.Unlock()
	require.True(t, first.AuditEvent.Has)
	assert.Equal(t, "vault-resolve", first.AuditEvent.Val.Action)
	assert.Equal(t, "auditEvent", first.AuditEvent.Val.Kind)
	require.True(t, first.Origin.Has)
	assert.Equal(t, Origin{Id: first.ID, Run: "run-1", App: "apps/dmdm", Instance: 2}, first.Origin.Val)
	assert.NotEmpty(t, first.NaturalKey)
	c := rec.Counts()
	assert.EqualValues(t, 2, c.Forwarded)
	assert.EqualValues(t, 0, c.ForwardDropped)
}

// A forwarder that refuses loses nothing on the server; the refusal is
// counted and the rows are left to the backstop.
func TestRefusedBatchIsCounted(t *testing.T) {
	exec := &gateExec{open: true}
	fwd := &collectForwarder{refuse: true}
	rec := NewRecorder(exec, "run-1", zerolog.Nop(), WithForwarder(fwd))
	require.NoError(t, rec.Event(context.Background(), time.Now(), Context{}, event()))
	require.NoError(t, rec.Flush(context.Background()))
	rec.Close()
	assert.Empty(t, fwd.ids())
	c := rec.Counts()
	assert.EqualValues(t, 1, c.Written, "the row is on the server")
	assert.EqualValues(t, 0, c.Forwarded)
	assert.EqualValues(t, 1, c.ForwardDropped)
	assert.EqualValues(t, 0, c.Dropped, "a refused forward is not a dropped row")
}

// Rows the recorder discards — a Close under a down server — are forgotten
// by the prompt path, and the recorder's own gap row is forwarded like any
// audit event.
func TestDiscardedRowsAreNotForwardedAndGapRowsAre(t *testing.T) {
	exec := &gateExec{}
	fwd := &collectForwarder{}
	rec := NewRecorder(exec, "run-1", zerolog.Nop(), WithForwarder(fwd))
	rec.maxBacklog = 1
	for range 3 {
		require.NoError(t, rec.Event(context.Background(), time.Now(), Context{}, event()))
		require.Error(t, rec.Flush(context.Background()))
	}
	exec.mu.Lock()
	exec.open = true
	exec.mu.Unlock()
	require.NoError(t, rec.Flush(context.Background()))
	rec.Close()
	fwd.mu.Lock()
	defer fwd.mu.Unlock()
	require.Len(t, fwd.got, 2, "the one held event that landed, and the gap row")
	assert.Equal(t, "vault-resolve", fwd.got[0].AuditEvent.Val.Action)
	assert.Equal(t, ActionAuditGap, fwd.got[1].AuditEvent.Val.Action)
	assert.Equal(t, "2", fwd.got[1].AuditEvent.Val.AttrValues[0])
	rec.mu.Lock()
	assert.Empty(t, rec.fwd.pending, "nothing is kept for a row that will never be durable")
	rec.mu.Unlock()
}

// Without a forwarder the recorder keeps no entities and the backstop is
// a no-op.
func TestNoForwarderKeepsNothing(t *testing.T) {
	exec := &gateExec{open: true}
	rec := NewRecorder(exec, "run-1", zerolog.Nop())
	defer rec.Close()
	require.NoError(t, rec.Event(context.Background(), time.Now(), Context{}, event()))
	assert.Nil(t, rec.fwd)
	n, err := rec.ForwardWindow(context.Background(), time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}
