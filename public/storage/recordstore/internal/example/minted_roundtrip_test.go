package example

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identgen/leased/rowalloc"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// mintedTestStream is a test-only claim: a stream value no committed golden
// holds, since it identifies nothing outside this test.
var mintedTestStream = tagmint.MustClaim("exampleMintedTest", 121394, 1<<30)

// Two stores over one table, each minting from its own sequence over the
// one lease table, as two processes would: every key is distinct, carries
// the stream's tag, and has a body above zero. A store without a stream
// refuses to mint.
func TestMintedStoreKeysAreUniqueAcrossStores(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	layout := rowalloc.Layout{Database: "leasetest"}
	require.NoError(t, rowalloc.ProvisionIn(ctx, exec, layout))
	mk := func() *MintedStore {
		alloc, aerr := rowalloc.NewAllocator(exec, layout)
		require.NoError(t, aerr)
		seq, serr := rowalloc.NewSequence(alloc, mintedTestStream, 2)
		require.NoError(t, serr)
		return NewMintedStore(exec, nil, MintedStoreConfig{Keys: seq})
	}
	a, b := mk(), mk()
	defer a.Close()
	defer b.Close()
	require.NoError(t, a.EnsureTable(ctx))

	seen := map[uint64]bool{}
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		for _, st := range []*MintedStore{a, b} {
			key, merr := st.MintKey(ctx)
			require.NoError(t, merr)
			require.False(t, seen[key], "key %d minted twice", key)
			seen[key] = true
			id := identifier.TaggedId(key)
			assert.Equal(t, mintedTestStream.Value(), id.GetTag().GetValue())
			assert.NotZero(t, id.RemoveTag().Value(), "body 0 is invalid in every stream")
			require.NoError(t, st.Begin(key, now).AddMintedLabel(MintedLabel{Text: "x"}).Commit())
		}
	}
	_, err = a.Flush(ctx)
	require.NoError(t, err)
	_, err = b.Flush(ctx)
	require.NoError(t, err)
	for key := range seen {
		_, found, lerr := a.Latest(ctx, key)
		require.NoError(t, lerr)
		assert.True(t, found)
	}

	bare := NewMintedStore(exec, nil, MintedStoreConfig{})
	defer bare.Close()
	_, err = bare.MintKey(ctx)
	assert.ErrorContains(t, err, "no key stream")
}
