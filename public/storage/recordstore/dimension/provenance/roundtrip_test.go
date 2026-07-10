package provenance

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/identity/identgen/mem"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// TestS1ProvenanceRoundTrip pins the ADR-0112 slice-S1 contract end to end
// (write → flush → clickhouse-local → resolve), with no stamping: the intern →
// emit-once → resolve loop over the generated descriptor store.
func TestS1ProvenanceRoundTrip(t *testing.T) {
	ctx := context.Background()
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}

	st := NewProvenanceStore(exec, nil, ProvenanceStoreConfig{})
	require.NoError(t, st.EnsureTable(ctx))

	// An in-memory INTERNALIZING generator under the provenance tag: dedupes by
	// natural key, so fresh (and thus one emit) marks the first sight of a stack.
	const provenanceTag identifier.TagValue = 1
	gen, err := mem.NewIdInternalizer(provenanceTag, 1024)
	require.NoError(t, err)

	rec, err := NewRecorder(gen, NewStoreSink(st))
	require.NoError(t, err)

	// Dedup: the SAME call site (this loop line) yields ONE id, emitted once.
	var ids []identifier.TaggedId
	for range 3 {
		id, refErr := rec.Reference(ctx)
		require.NoError(t, refErr)
		ids = append(ids, id)
	}
	require.Equal(t, ids[0], ids[1])
	require.Equal(t, ids[1], ids[2])
	require.True(t, ids[0].IsValid(), "a minted id carries a valid fibonacci tag")

	// A DISTINCT call site (extra frame, different pcs) yields a distinct id.
	other, err := referenceFromHelper(rec, ctx)
	require.NoError(t, err)
	require.NotEqual(t, ids[0], other)

	// Resolve after flush: host + this test's frame come back.
	_, err = rec.Flush(ctx)
	require.NoError(t, err)

	host, _ := os.Hostname()
	got, found, err := rec.Resolve(ctx, ids[0])
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, host, got.Host)
	require.Equal(t, uint64(ids[0]), got.ID)
	require.Contains(t, strings.Join(got.Stack, "\n"), "TestS1ProvenanceRoundTrip")

	// The distinct id resolves to a stack that went through the helper.
	gotOther, found, err := rec.Resolve(ctx, other)
	require.NoError(t, err)
	require.True(t, found)
	require.Contains(t, strings.Join(gotOther.Stack, "\n"), "referenceFromHelper")

	// An id never minted resolves as absent.
	_, found, err = rec.Resolve(ctx, other+0x1000)
	require.NoError(t, err)
	require.False(t, found)
}

//go:noinline
func referenceFromHelper(rec *Recorder, ctx context.Context) (identifier.TaggedId, error) {
	return rec.Reference(ctx)
}
