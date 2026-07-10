package internalized

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/require"
)

func TestBadgerIdInternalizer_RoundtripAndIdempotent(t *testing.T) {
	genFac, err := NewBadgerIdInternalizedGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()

	gen, err := genFac.Create(identifier.TagValue(3), 16)
	require.NoError(t, err)
	defer func() { _ = gen.Release() }()

	k := []byte("de305d54-75b4-431b-adb2-eb6b9e546013")
	id1, fresh1, err := gen.GetId(context.Background(), k)
	require.NoError(t, err)
	require.True(t, fresh1)
	require.True(t, id1.IsValid())
	require.True(t, id1.RemoveTag().IsValid(), "body 0 is reserved as invalid/NULL")
	require.EqualValues(t, 3, id1.GetTag().GetValue())

	id2, fresh2, err := gen.GetId(context.Background(), k)
	require.NoError(t, err)
	require.False(t, fresh2)
	require.Equal(t, id1, id2)
}

// TestBadgerIdInternalizer_TagIsolation is the regression test for the cross-tag
// key collision: the same natural key under two different tags in one store must
// map to two distinct ids, each carrying its own tag.
func TestBadgerIdInternalizer_TagIsolation(t *testing.T) {
	genFac, err := NewBadgerIdInternalizedGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()

	genA, err := genFac.Create(identifier.TagValue(1), 16)
	require.NoError(t, err)
	defer func() { _ = genA.Release() }()
	genB, err := genFac.Create(identifier.TagValue(2), 16)
	require.NoError(t, err)
	defer func() { _ = genB.Release() }()

	key := []byte("shared-key")
	idA, _, err := genA.GetId(context.Background(), key)
	require.NoError(t, err)
	idB, _, err := genB.GetId(context.Background(), key)
	require.NoError(t, err)

	require.NotEqual(t, idA, idB)
	require.EqualValues(t, 1, idA.GetTag().GetValue())
	require.EqualValues(t, 2, idB.GetTag().GetValue())

	// Each tag resolves its own key stably.
	idA2, freshA2, err := genA.GetId(context.Background(), key)
	require.NoError(t, err)
	require.False(t, freshA2)
	require.Equal(t, idA, idA2)
}

func TestBadgerIdInternalizer_RejectsEmptyKey(t *testing.T) {
	genFac, err := NewBadgerIdInternalizedGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()
	gen, err := genFac.Create(identifier.TagValue(1), 16)
	require.NoError(t, err)
	defer func() { _ = gen.Release() }()

	_, _, err = gen.GetId(context.Background(), nil)
	require.Error(t, err)
	_, _, err = gen.GetId(context.Background(), []byte{})
	require.Error(t, err)
}

func TestBadgerIdInternalizer_PersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	genFac, err := NewBadgerIdInternalizedGenerator(dir)
	require.NoError(t, err)
	gen, err := genFac.Create(identifier.TagValue(4), 16)
	require.NoError(t, err)
	key := []byte("persisted")
	id1, _, err := gen.GetId(context.Background(), key)
	require.NoError(t, err)
	require.NoError(t, gen.Release())
	require.NoError(t, genFac.Close())

	// Reopen the same store; the mapping must survive.
	genFac2, err := NewBadgerIdInternalizedGenerator(dir)
	require.NoError(t, err)
	defer func() { _ = genFac2.Close() }()
	gen2, err := genFac2.Create(identifier.TagValue(4), 16)
	require.NoError(t, err)
	defer func() { _ = gen2.Release() }()
	id2, fresh2, err := gen2.GetId(context.Background(), key)
	require.NoError(t, err)
	require.False(t, fresh2, "mapping should have persisted across reopen")
	require.Equal(t, id1, id2)
}

// TestBadgerIdInternalizedGenerator_RejectsInvalidTag checks Create rejects
// the zero (invalid) tag value (ADR-0106: every non-zero uint32 is encodable).
func TestBadgerIdInternalizedGenerator_RejectsInvalidTag(t *testing.T) {
	genFac, err := NewBadgerIdInternalizedGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()

	_, err = genFac.Create(identifier.TagValue(0), 8)
	require.Error(t, err)
}

// TestBadgerIdInternalizer_ConcurrentGetId hammers one internalizer from many
// goroutines (run with -race) to exercise the mutex: distinct keys must map to
// distinct ids and every mapping must survive the race.
func TestBadgerIdInternalizer_ConcurrentGetId(t *testing.T) {
	genFac, err := NewBadgerIdInternalizedGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()
	gen, err := genFac.Create(identifier.TagValue(7), 256)
	require.NoError(t, err)
	defer func() { _ = gen.Release() }()

	const workers = 8
	const perWorker = 64

	var mu sync.Mutex
	seen := make(map[identifier.TaggedId]string)
	errs := make(chan error, workers)

	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range perWorker {
				key := fmt.Sprintf("w%d-k%d", w, i)
				id, _, e := gen.GetId(context.Background(), []byte(key))
				if e != nil {
					errs <- e
					return
				}
				mu.Lock()
				if prev, ok := seen[id]; ok && prev != key {
					e = fmt.Errorf("id %d assigned to both %q and %q", uint64(id), prev, key)
				}
				seen[id] = key
				mu.Unlock()
				if e != nil {
					errs <- e
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	require.Len(t, seen, workers*perWorker, "every distinct key must get a distinct id")
}

func TestBadgerIdInternalizer_AppendIds(t *testing.T) {
	genFac, err := NewBadgerIdInternalizedGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()
	gen, err := genFac.Create(identifier.TagValue(5), 16)
	require.NoError(t, err)
	defer func() { _ = gen.Release() }()

	bgen := gen.(identgen.BatchInternalizerI)

	var keys identgen.KeysColumn
	for _, k := range []string{"a", "b", "a", "c", "b"} {
		keys = keys.AppendKey([]byte(k))
	}

	ids, fresh, err := bgen.AppendIds(context.Background(), nil, keys, make([]bool, 0))
	require.NoError(t, err)
	require.Len(t, ids, 5)
	require.Equal(t, ids[0], ids[2]) // dedup + alignment
	require.Equal(t, ids[1], ids[4])
	require.NotEqual(t, ids[0], ids[3])
	require.Equal(t, []bool{true, true, false, true, false}, fresh)
	for _, id := range ids {
		require.True(t, id.IsValid())
		require.True(t, id.RemoveTag().IsValid(), "body 0 is reserved")
		require.EqualValues(t, 5, id.GetTag().GetValue())
	}

	// The mapping persisted: single GetId returns the same id, not fresh.
	gotA, freshA, err := bgen.GetId(context.Background(), []byte("a"))
	require.NoError(t, err)
	require.False(t, freshA)
	require.Equal(t, ids[0], gotA)

	// Re-batching the same column resolves everything to the same ids, none fresh.
	ids2, fresh2, err := bgen.AppendIds(context.Background(), nil, keys, make([]bool, 0))
	require.NoError(t, err)
	require.Equal(t, ids, ids2)
	require.Equal(t, []bool{false, false, false, false, false}, fresh2)
}

func TestBadgerIdInternalizer_AppendIds_RejectsEmptyKey(t *testing.T) {
	genFac, err := NewBadgerIdInternalizedGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()
	gen, err := genFac.Create(identifier.TagValue(1), 16)
	require.NoError(t, err)
	defer func() { _ = gen.Release() }()
	bgen := gen.(identgen.BatchInternalizerI)

	keys := identgen.KeysColumn{}.AppendKey([]byte("ok")).AppendKey([]byte(""))
	out, _, err := bgen.AppendIds(context.Background(), nil, keys, nil)
	require.ErrorIs(t, err, identgen.ErrEmptyNaturalKey)
	require.Empty(t, out)

	// The rejected batch persisted nothing: "ok" is still fresh.
	_, fresh, err := bgen.GetId(context.Background(), []byte("ok"))
	require.NoError(t, err)
	require.True(t, fresh)
}

// BenchmarkBadgerIdInternalizer_GetId_Hit measures the read path (a Badger View
// txn) for an already-internalized key.
func BenchmarkBadgerIdInternalizer_GetId_Hit(b *testing.B) {
	genFac, err := NewBadgerIdInternalizedGenerator(b.TempDir())
	require.NoError(b, err)
	defer func() { _ = genFac.Close() }()
	gen, err := genFac.Create(identifier.TagValue(1), 1024)
	require.NoError(b, err)
	defer func() { _ = gen.Release() }()

	key := []byte("de305d54-75b4-431b-adb2-eb6b9e546013")
	_, _, _ = gen.GetId(context.Background(), key) // prime

	b.ReportAllocs()
	for b.Loop() {
		_, _, _ = gen.GetId(context.Background(), key)
	}
}

// BenchmarkBadgerIdInternalizer_AppendIds resolves a whole column of
// already-internalized keys in two transactions, amortising the per-op txn
// overhead that BenchmarkBadgerIdInternalizer_GetId_Hit pays per key.
func BenchmarkBadgerIdInternalizer_AppendIds(b *testing.B) {
	genFac, err := NewBadgerIdInternalizedGenerator(b.TempDir())
	require.NoError(b, err)
	defer func() { _ = genFac.Close() }()
	gen, err := genFac.Create(identifier.TagValue(1), 4096)
	require.NoError(b, err)
	defer func() { _ = gen.Release() }()
	bgen := gen.(identgen.BatchInternalizerI)

	const batch = 256
	var keys identgen.KeysColumn
	for i := range batch {
		keys = keys.AppendKey(fmt.Appendf(nil, "k%d", i))
	}
	_, _, _ = bgen.AppendIds(context.Background(), nil, keys, nil) // prime

	var dst []identifier.TaggedId
	b.ReportAllocs()
	for b.Loop() {
		dst, _, _ = bgen.AppendIds(context.Background(), dst[:0], keys, nil)
	}
	b.ReportMetric(batch, "keys/op")
}
