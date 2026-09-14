package watchbillreply_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/watchbillreply"
)

func TestBuscodecAutoRegistersWatchbillReply(t *testing.T) {
	got := buscodec.Lookup[watchbillreply.WatchbillReply]()
	require.Equal(t, "watchbillReply-sparse-cbor", got.Name())
}

// Two jobs zip by index across every column; a refusal is Ok false with a
// Reason and no jobs.
func TestBuscodecRoundTrip(t *testing.T) {
	orig := watchbillreply.WatchbillReply{
		FactId: 1, At: time.Unix(0, 1_700_000_000_000_000_000).UTC(), Ok: true,
		Id: []string{"j1", "j2"}, Kind: []string{"k.a", "k.b"}, Subject: []string{"s1", ""}, Queue: []string{"default", "bulk"},
		Priority: []uint64{0, 3}, State: []string{"queued", "discarded"}, Attempt: []uint64{0, 3}, MaxAttempts: []uint64{1, 3},
		Backoff: []string{"none", "exponential"}, BackoffBaseMs: []uint64{0, 2000}, TimeoutMs: []uint64{0, 60_000},
		RunAfterMs: []int64{1_700_000_000_000, 1_700_000_001_000}, WorkerRun: []string{"", "run-x"},
		FinishedAtMs: []int64{0, 1_700_000_002_000}, LastError: []string{"", "boom"},
		OwnerAppId: []string{"app.a", "app.b"}, RequesterRun: []string{"run-1", "run-2"}, ArgsKind: []string{"", "tenderArgs"},
	}
	wire, err := buscodec.Encode(orig)
	require.NoError(t, err)
	got, err := buscodec.Decode[watchbillreply.WatchbillReply](wire)
	require.NoError(t, err)
	assert.True(t, got.Ok)
	assert.Empty(t, got.Reason)
	assert.Equal(t, 2, got.Len())
	assert.Equal(t, orig.Id, got.Id)
	assert.Equal(t, orig.Kind, got.Kind)
	assert.Equal(t, orig.Subject, got.Subject)
	assert.Equal(t, orig.Queue, got.Queue)
	assert.Equal(t, orig.Priority, got.Priority)
	assert.Equal(t, orig.State, got.State)
	assert.Equal(t, orig.Attempt, got.Attempt)
	assert.Equal(t, orig.MaxAttempts, got.MaxAttempts)
	assert.Equal(t, orig.Backoff, got.Backoff)
	assert.Equal(t, orig.BackoffBaseMs, got.BackoffBaseMs)
	assert.Equal(t, orig.TimeoutMs, got.TimeoutMs)
	assert.Equal(t, orig.RunAfterMs, got.RunAfterMs)
	assert.Equal(t, orig.WorkerRun, got.WorkerRun)
	assert.Equal(t, orig.FinishedAtMs, got.FinishedAtMs)
	assert.Equal(t, orig.LastError, got.LastError)
	assert.Equal(t, orig.OwnerAppId, got.OwnerAppId)
	assert.Equal(t, orig.RequesterRun, got.RequesterRun)
	assert.Equal(t, orig.ArgsKind, got.ArgsKind)

	refusal := watchbillreply.WatchbillReply{Reason: "watchbill: no such job"}
	wire, err = buscodec.Encode(refusal)
	require.NoError(t, err)
	got, err = buscodec.Decode[watchbillreply.WatchbillReply](wire)
	require.NoError(t, err)
	assert.False(t, got.Ok)
	assert.Equal(t, refusal.Reason, got.Reason)
	assert.Equal(t, 0, got.Len())
}
