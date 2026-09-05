package pushoutstore

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

func nodeOf(b byte, idx uint64) types.NodeID { return types.NodeID{Patch: ph(b), Index: idx} }

// TestRetentionLedgerCompacts pins the ADR-0221 ledger shape: every
// UpdateRetention is one delta row, every compactEvery deltas the key
// is reset and rewritten as one full row, and a reopen folds to the
// same set either way.
func TestRetentionLedgerCompacts(t *testing.T) {
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	stor, err := Open(ctx, local, nil, PushoutStoreConfig{}, PushoutCacheConfig{Capacity: 8})
	require.NoError(t, err)
	// Each update is a clickhouse-local process; compact early so the
	// test crosses two compactions in a dozen updates.
	const compactEvery = 4
	stor.compactEvery = compactEvery

	want := map[types.NodeID]repo.RetentionEntry{}
	const n = compactEvery*2 + 5
	for i := range n {
		id := nodeOf(byte(i%7+1), uint64(i))
		delta := repo.RetentionDelta{Upsert: []repo.RetentionEntry{{Node: id, UnixNano: int64(1000 + i), Purged: i%3 == 0}}}
		if i > 0 && i%5 == 0 {
			gone := nodeOf(byte((i-1)%7+1), uint64(i-1))
			delta.Remove = append(delta.Remove, gone)
			delete(want, gone)
		}
		want[id] = delta.Upsert[0]
		require.NoError(t, stor.UpdateRetention(ctx, delta))
	}
	// The mirror never replays more than compactEvery rows.
	require.LessOrEqual(t, stor.retRows, compactEvery)

	check := func(st *Storage) {
		got, lerr := st.LoadRetention(ctx)
		require.NoError(t, lerr)
		require.Len(t, got, len(want))
		for _, e := range got {
			require.Equal(t, want[e.Node], e)
		}
	}
	check(stor)
	require.NoError(t, stor.Close())

	// A fresh handle folds the rows after the last tombstone.
	reopened, err := Open(ctx, local, nil, PushoutStoreConfig{}, PushoutCacheConfig{Capacity: 8})
	require.NoError(t, err)
	reopened.compactEvery = compactEvery
	require.Equal(t, 0, reopened.retRows) // unknown until first use
	check(reopened)
	require.LessOrEqual(t, reopened.retRows, compactEvery)
	require.Greater(t, reopened.retRows, 0)
}

// TestEnvelopeBatchIsOneInsertAndChunkedRead pins the batch verbs'
// cost shape: PutEnvelopes of n new envelopes is ONE insert, and
// LoadEnvelopes over more than one chunk still yields every envelope in
// request order.
func TestEnvelopeBatchIsOneInsertAndChunkedRead(t *testing.T) {
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	counting := &flakyExecutor{inner: local, failNth: -1}
	stor, err := Open(ctx, counting, nil, PushoutStoreConfig{}, PushoutCacheConfig{Capacity: 8})
	require.NoError(t, err)

	const n = loadEnvelopesChunk + 37
	envs := make([]repo.Envelope, 0, n)
	for i := range n {
		var h types.PatchHash
		h[0], h[1], h[31] = byte(i>>8), byte(i), 0xEE
		envs = append(envs, repo.Envelope{Hash: h, Framed: []byte{'P', 'X', 'E', '1', byte(i >> 8), byte(i)}})
	}
	before := counting.inserts
	require.NoError(t, stor.PutEnvelopes(ctx, envs))
	require.Equal(t, 1, counting.inserts-before, "a batch put is one insert")
	// A re-put of the same batch writes nothing.
	require.NoError(t, stor.PutEnvelopes(ctx, envs))
	require.Equal(t, 1, counting.inserts-before, "a duplicate batch put inserts nothing")

	// Reverse request order crosses both chunks.
	req := make([]types.PatchHash, 0, n)
	for i := n - 1; i >= 0; i-- {
		req = append(req, envs[i].Hash)
	}
	i := 0
	for env, lerr := range stor.LoadEnvelopes(ctx, req) {
		require.NoError(t, lerr)
		require.Equal(t, req[i], env.Hash)
		require.Equal(t, envs[n-1-i].Framed, env.Framed)
		i++
	}
	require.Equal(t, n, i)
}
