package internalized

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/require"
)

// TestCreate_SecondGeneratorForTagErrors is the regression test for the
// stable-id violation: two generators on one tag share the store but not a
// lock, so both could miss the same key and mint different ids. Create must
// refuse the duplicate (ADR-0106 SD6).
func TestCreate_SecondGeneratorForTagErrors(t *testing.T) {
	genFac, err := NewBadgerIdInternalizedGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()

	gen, err := genFac.Create(identifier.TagValue(9), 64)
	require.NoError(t, err)
	defer func() { _ = gen.Release() }()

	_, err = genFac.Create(identifier.TagValue(9), 64)
	require.ErrorIs(t, err, identgen.ErrTagInUse)

	// Release keeps the generator usable, so the slot stays held.
	require.NoError(t, gen.Release())
	_, err = genFac.Create(identifier.TagValue(9), 64)
	require.ErrorIs(t, err, identgen.ErrTagInUse)

	// Other tags are unaffected.
	gen2, err := genFac.Create(identifier.TagValue(10), 64)
	require.NoError(t, err)
	defer func() { _ = gen2.Release() }()
}

func keysColumnN(prefix string, n int) (keys identgen.KeysColumn) {
	keys.Data = make([]byte, 0, n*(len(prefix)+7))
	keys.Ends = make([]uint32, 0, n)
	for i := 0; i < n; i++ {
		keys = keys.AppendKey(fmt.Appendf(nil, "%s%06d", prefix, i))
	}
	return
}

// TestAppendIds_LargeBatchCommitsChunked is the regression test for the
// unhandled transaction-size limit: a batch of half a million fresh keys used
// to fail with "Txn is too big to fit into one request".
func TestAppendIds_LargeBatchCommitsChunked(t *testing.T) {
	if testing.Short() {
		t.Skip("large batch")
	}
	genFac, err := NewBadgerIdInternalizedGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()
	gen, err := genFac.Create(identifier.TagValue(5), 1<<20)
	require.NoError(t, err)
	defer func() { _ = gen.Release() }()
	bgen := gen.(identgen.BatchInternalizerI)

	const n = 500_000
	keys := keysColumnN("k", n)
	ids, _, err := bgen.AppendIds(context.Background(), nil, keys, nil)
	require.NoError(t, err)
	require.Len(t, ids, n)

	// The mappings persisted across the chunk boundary: re-batching resolves
	// to the same ids with nothing fresh.
	ids2, fresh2, err := bgen.AppendIds(context.Background(), nil, keys, make([]bool, 0))
	require.NoError(t, err)
	require.Equal(t, ids, ids2)
	for i, f := range fresh2 {
		require.False(t, f, "key %d must already be interned", i)
	}
}

// TestExhaustion_MaxWidthTag exercises ErrIdSpaceExhausted end to end. The
// widest uint32 tag (width 47) holds exactly 2^17-1 = 131071 bodies, so the
// space fills in one batch — no injection seam needed (ADR-0106 SD6).
func TestExhaustion_MaxWidthTag(t *testing.T) {
	if testing.Short() {
		t.Skip("fills a 131071-id space")
	}
	genFac, err := NewBadgerIdInternalizedGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()

	const capacity = 1<<17 - 1

	// A batch exceeding the whole space errors, but the mappings minted
	// before the overrun persist: consumed sequence values cannot be
	// returned, so dropping them would burn the space with nothing mapped.
	// A retry therefore resolves the minted keys as existing.
	genOver, err := genFac.Create(identifier.TagValue(math.MaxUint32-1), 1<<18)
	require.NoError(t, err)
	defer func() { _ = genOver.Release() }()
	over := genOver.(identgen.BatchInternalizerI)
	_, _, err = over.AppendIds(context.Background(), nil, keysColumnN("o", capacity+1), nil)
	require.ErrorIs(t, err, identgen.ErrIdSpaceExhausted)
	_, fresh, err := over.GetId(context.Background(), []byte("o000000"))
	require.NoError(t, err)
	require.False(t, fresh, "mappings minted before the overrun persist for idempotent retry")

	// Filling the space exactly succeeds; the next distinct key exhausts.
	genFull, err := genFac.Create(identifier.TagValue(math.MaxUint32), 1<<18)
	require.NoError(t, err)
	defer func() { _ = genFull.Release() }()
	full := genFull.(identgen.BatchInternalizerI)
	ids, _, err := full.AppendIds(context.Background(), nil, keysColumnN("f", capacity), nil)
	require.NoError(t, err)
	require.Len(t, ids, capacity)
	_, _, err = full.GetId(context.Background(), []byte("one-too-many"))
	require.ErrorIs(t, err, identgen.ErrIdSpaceExhausted)
	// Existing keys still resolve after exhaustion.
	id0, fresh0, err := full.GetId(context.Background(), []byte("f000000"))
	require.NoError(t, err)
	require.False(t, fresh0)
	require.Equal(t, ids[0], id0)
}
