package watchbillrequest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/watchbillrequest"
)

func TestBuscodecAutoRegistersWatchbillRequest(t *testing.T) {
	got := buscodec.Lookup[watchbillrequest.WatchbillRequest]()
	require.Equal(t, "watchbillRequest-sparse-cbor", got.Name())
}

// Every field survives the wire, including the two list-filter arrays
// and the opaque args bytes; an enqueue and a list are the two extremes.
func TestBuscodecRoundTrip(t *testing.T) {
	orig := watchbillrequest.WatchbillRequest{
		FactId: 3, At: time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Op: watchbillrequest.OpEnqueue, Id: "job-1", Kind: "tender.download", Subject: "dl-77", Queue: "bulk",
		Priority: 2, MaxAttempts: 5, Backoff: "exponential", BackoffBaseMs: 1500, TimeoutMs: 60_000,
		ArgsKind: "tenderArgs", Args: []byte{0xa1, 0x01}, RunAfterMs: 1_700_000_100_000, Note: "asked by hand",
	}
	wire, err := buscodec.Encode(orig)
	require.NoError(t, err)
	got, err := buscodec.Decode[watchbillrequest.WatchbillRequest](wire)
	require.NoError(t, err)
	assert.Equal(t, orig.Op, got.Op)
	assert.Equal(t, orig.Id, got.Id)
	assert.Equal(t, orig.Kind, got.Kind)
	assert.Equal(t, orig.Subject, got.Subject)
	assert.Equal(t, orig.Queue, got.Queue)
	assert.Equal(t, orig.Priority, got.Priority)
	assert.Equal(t, orig.MaxAttempts, got.MaxAttempts)
	assert.Equal(t, orig.Backoff, got.Backoff)
	assert.Equal(t, orig.BackoffBaseMs, got.BackoffBaseMs)
	assert.Equal(t, orig.TimeoutMs, got.TimeoutMs)
	assert.Equal(t, orig.ArgsKind, got.ArgsKind)
	assert.Equal(t, orig.Args, got.Args)
	assert.Equal(t, orig.RunAfterMs, got.RunAfterMs)
	assert.Equal(t, orig.Note, got.Note)
	assert.Empty(t, got.States)
	assert.Empty(t, got.Kinds)

	list := watchbillrequest.WatchbillRequest{Op: watchbillrequest.OpList, States: []string{"queued", "running"}, Kinds: []string{"a", "b", "c"}, Limit: 50}
	wire, err = buscodec.Encode(list)
	require.NoError(t, err)
	got, err = buscodec.Decode[watchbillrequest.WatchbillRequest](wire)
	require.NoError(t, err)
	assert.Equal(t, list.States, got.States)
	assert.Equal(t, list.Kinds, got.Kinds)
	assert.Equal(t, list.Limit, got.Limit)
	assert.Empty(t, got.Kind)
}
