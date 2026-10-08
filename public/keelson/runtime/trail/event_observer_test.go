package trail

import (
	"context"
	"iter"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// keysObserver records what the recorder's stores tell a write observer.
type keysObserver struct {
	mu        sync.Mutex
	committed []recordstore.WrittenKey[uint64, time.Time]
	durable   map[recordstore.BatchIdT][]uint64
	discarded []uint64
}

func newKeysObserver() *keysObserver {
	return &keysObserver{durable: map[recordstore.BatchIdT][]uint64{}}
}

func (inst *keysObserver) Committed(w recordstore.WrittenKey[uint64, time.Time]) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.committed = append(inst.committed, w)
}

func (inst *keysObserver) Durable(_ context.Context, batch recordstore.BatchIdT, keys iter.Seq[recordstore.WrittenKey[uint64, time.Time]]) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for w := range keys {
		inst.durable[batch] = append(inst.durable[batch], w.Key)
	}
}

func (inst *keysObserver) Discarded(keys iter.Seq[recordstore.WrittenKey[uint64, time.Time]]) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for w := range keys {
		inst.discarded = append(inst.discarded, w.Key)
	}
}

func (inst *keysObserver) durableKeys() (n int, batches int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, ks := range inst.durable {
		n += len(ks)
	}
	return n, len(inst.durable)
}

func event() AuditEvent {
	return AuditEvent{Domain: "dmdm", Action: "vault-resolve", Outcome: OutcomeOk, PrincipalBy: PrincipalByEnv, Subject: 7, Retention: "disclosure"}
}

// The identity on the context reaches the row's committer (ADR-0295 §SD7
// through the recorder): the observer's Committed carries it, and a row
// written by a verb without a context carries none.
func TestEventCommitsUnderTheCallIdentity(t *testing.T) {
	exec := &gateExec{open: true}
	obs := newKeysObserver()
	rec := NewRecorder(exec, "run-1", zerolog.Nop(), WithWriteObserver(obs))
	defer rec.Close()
	ci := callident.CallIdentity{Origin: callident.Origin{Run: "run-1", App: "apps/dmdm", Instance: 3}, Claims: callident.Claims{Principal: "p-1", Purpose: "art-15"}}
	ctx := callident.WithCallIdentity(context.Background(), ci)
	require.NoError(t, rec.Event(ctx, time.Now(), Context{}, event()))
	require.NoError(t, rec.LlmCall(time.Now(), Context{}, LlmCall{CallId: "c1"}))
	require.NoError(t, rec.Flush(context.Background()))
	require.Len(t, obs.committed, 2)
	assert.Equal(t, ci, obs.committed[0].Identity, "the event's row carries the identity it was committed under")
	assert.True(t, obs.committed[1].Identity.IsZero(), "a verb without a context commits under no identity")
	n, batches := obs.durableKeys()
	assert.Equal(t, 2, n)
	assert.Equal(t, 1, batches)
}

// Across a failed flush the observer is told each key durable exactly
// once, under the batch that landed it, and nothing is discarded.
func TestObserverSeesABatchOnceAcrossAFailedFlush(t *testing.T) {
	exec := &gateExec{}
	obs := newKeysObserver()
	rec := NewRecorder(exec, "run-1", zerolog.Nop(), WithWriteObserver(obs))
	require.NoError(t, rec.Event(context.Background(), time.Now(), Context{}, event()))
	require.NoError(t, rec.Event(context.Background(), time.Now(), Context{}, event()))
	require.Error(t, rec.Flush(context.Background()))
	n, _ := obs.durableKeys()
	assert.Equal(t, 0, n, "nothing is durable while the server is down")
	require.NoError(t, rec.Event(context.Background(), time.Now(), Context{}, event()))
	exec.mu.Lock()
	exec.open = true
	exec.mu.Unlock()
	require.NoError(t, rec.Flush(context.Background()))
	n, batches := obs.durableKeys()
	assert.Equal(t, 3, n, "every key once")
	assert.Equal(t, 2, batches, "the held batch and the new one land under batch ids of their own")
	assert.Empty(t, obs.discarded)
	rec.Close()
	assert.Empty(t, obs.discarded, "a clean close discards nothing")
}

// A recorder with no backend accepts the verb and records nothing.
func TestEventWithoutABackend(t *testing.T) {
	var none *Recorder
	assert.NoError(t, none.Event(context.Background(), time.Now(), Context{}, event()))
	rec := NewRecorder(nil, "run-1", zerolog.Nop())
	defer rec.Close()
	assert.NoError(t, rec.Event(context.Background(), time.Now(), Context{}, AuditEvent{}))
}
