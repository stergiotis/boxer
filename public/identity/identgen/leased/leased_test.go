package leased_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identgen/leased"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/memalloc"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/require"
)

// countingAlloc wraps an allocator to count block reservations, proving the
// backend is touched once per block rather than once per id.
type countingAlloc struct {
	inner identgen.AllocatorI
	calls int
}

func (inst *countingAlloc) AllocateBlock(ctx context.Context, tagValue identifier.TagValue, minSize uint64, maxSize uint64) (lo identifier.UntaggedId, hi identifier.UntaggedId, err error) {
	inst.calls++
	return inst.inner.AllocateBlock(ctx, tagValue, minSize, maxSize)
}

func (inst *countingAlloc) Close() (err error) { return inst.inner.Close() }

// stubAlloc always returns a fixed block, used to drive the cursor's clamp to
// the tag's body ceiling.
type stubAlloc struct {
	lo, hi identifier.UntaggedId
}

func (inst stubAlloc) AllocateBlock(ctx context.Context, tagValue identifier.TagValue, minSize uint64, maxSize uint64) (lo identifier.UntaggedId, hi identifier.UntaggedId, err error) {
	return inst.lo, inst.hi, nil
}

func (inst stubAlloc) Close() (err error) { return nil }

func TestSequence_DenseMonotonicAcrossBlocks(t *testing.T) {
	ca := &countingAlloc{inner: memalloc.NewAllocator()}
	s, err := leased.NewSequence(ca, identifier.TagValue(3), 4)
	require.NoError(t, err)

	seen := make(map[identifier.UntaggedId]struct{})
	var prev uint64
	for i := range 10 {
		u, fresh, err := s.GetUntaggedId(context.Background(), nil)
		require.NoError(t, err)
		require.True(t, fresh)
		require.EqualValues(t, i+1, u) // dense from body 1; 0 stays reserved
		require.Greater(t, uint64(u), prev)
		_, dup := seen[u]
		require.False(t, dup)
		seen[u] = struct{}{}
		prev = uint64(u)
	}
	// 10 ids at bandwidth 4 => 3 block leases (4 + 4 + 2), not 10.
	require.Equal(t, 3, ca.calls)
}

func TestSequence_IgnoresNaturalKey(t *testing.T) {
	s, err := leased.NewSequence(memalloc.NewAllocator(), identifier.TagValue(1), 8)
	require.NoError(t, err)

	a, _, err := s.GetId(context.Background(), []byte("same"))
	require.NoError(t, err)
	b, _, err := s.GetId(context.Background(), []byte("same"))
	require.NoError(t, err)
	require.NotEqual(t, a, b) // no dedup: the same key mints two ids
}

func TestInternalizer_LocalDedup(t *testing.T) {
	m, err := leased.NewInternalizer(memalloc.NewAllocator(), identifier.TagValue(5), 4)
	require.NoError(t, err)

	id1, fresh1, err := m.GetId(context.Background(), []byte("k"))
	require.NoError(t, err)
	require.True(t, fresh1)

	id2, fresh2, err := m.GetId(context.Background(), []byte("k"))
	require.NoError(t, err)
	require.False(t, fresh2) // second sight of the key is deduped
	require.Equal(t, id1, id2)
	require.Equal(t, 1, m.Len())

	key, found := m.Resolve(id1)
	require.True(t, found)
	require.Equal(t, "k", key)

	_, _, err = m.GetId(context.Background(), nil)
	require.ErrorIs(t, err, identgen.ErrEmptyNaturalKey)
}

// TestInternalizer_GlobalSequenceLocalDedup is the crux: two internalizers over
// one shared allocator dedup locally but draw globally-unique ids. The same key
// resolves to different ids across instances (dedup is local only), yet no two
// ids collide (the shared allocator serialises the id source).
func TestInternalizer_GlobalSequenceLocalDedup(t *testing.T) {
	alloc := memalloc.NewAllocator() // the one shared "global sequence"
	tag := identifier.TagValue(7)

	a, err := leased.NewInternalizer(alloc, tag, 4)
	require.NoError(t, err)
	b, err := leased.NewInternalizer(alloc, tag, 4)
	require.NoError(t, err)

	keys := []string{"acme", "globex", "acme", "initech", "globex"}
	idsA := make(map[string]identifier.TaggedId)
	idsB := make(map[string]identifier.TaggedId)

	insert := func(g *leased.Internalizer, dst map[string]identifier.TaggedId, k string) {
		id, _, err := g.GetId(context.Background(), []byte(k))
		require.NoError(t, err)
		dst[k] = id
	}
	for _, k := range keys {
		insert(a, idsA, k)
		insert(b, idsB, k)
	}

	// Local dedup: each instance holds only its distinct keys.
	require.Equal(t, 3, a.Len())
	require.Equal(t, 3, b.Len())

	// The same key resolves DIFFERENTLY across instances (dedup is local only).
	for _, k := range []string{"acme", "globex", "initech"} {
		require.NotEqualf(t, idsA[k], idsB[k], "key %q should differ across instances", k)
	}

	// Yet every id across both instances is globally unique (shared allocator).
	all := make(map[identifier.TaggedId]struct{})
	for _, id := range idsA {
		_, dup := all[id]
		require.False(t, dup)
		all[id] = struct{}{}
	}
	for _, id := range idsB {
		_, dup := all[id]
		require.False(t, dup, "cross-instance id collision")
		all[id] = struct{}{}
	}
	require.Len(t, all, 6) // 3 distinct keys x 2 instances, all distinct
}

func TestSequence_ExhaustionAtTagCeiling(t *testing.T) {
	maxId := identifier.TagValue(3).GetTag().GetMaxPossibleIdIncl()

	// A block starting exactly at the ceiling: exactly one usable id, then exhausted.
	s, err := leased.NewSequence(stubAlloc{lo: maxId, hi: maxId + 5}, identifier.TagValue(3), 8)
	require.NoError(t, err)
	u, _, err := s.GetUntaggedId(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, maxId, u)
	_, _, err = s.GetUntaggedId(context.Background(), nil)
	require.ErrorIs(t, err, identgen.ErrIdSpaceExhausted)

	// A block entirely above the ceiling: nothing usable.
	s2, err := leased.NewSequence(stubAlloc{lo: maxId + 1, hi: maxId + 5}, identifier.TagValue(3), 8)
	require.NoError(t, err)
	_, _, err = s2.GetUntaggedId(context.Background(), nil)
	require.ErrorIs(t, err, identgen.ErrIdSpaceExhausted)
}

func TestNewSequence_RejectsInvalidTagValue(t *testing.T) {
	_, err := leased.NewSequence(memalloc.NewAllocator(), identifier.TagValue(0), 4)
	require.Error(t, err) // zero is the reserved/invalid TagValue (ADR-0106)

	_, err = leased.NewSequence(memalloc.NewAllocator(), identifier.TagValue(1), 0)
	require.Error(t, err) // zero bandwidth
}

// TestSequence_ConcurrentUniqueness hammers one Sequence from many goroutines
// (validated under -race) and asserts every minted id is unique.
func TestSequence_ConcurrentUniqueness(t *testing.T) {
	s, err := leased.NewSequence(memalloc.NewAllocator(), identifier.TagValue(4), 8)
	require.NoError(t, err)

	const goroutines, perG = 8, 200
	got := make([][]identifier.TaggedId, goroutines)
	var wg sync.WaitGroup
	for gi := range goroutines {
		wg.Go(func() {
			out := make([]identifier.TaggedId, 0, perG)
			for range perG {
				id, _, e := s.GetId(context.Background(), nil)
				if e != nil {
					t.Errorf("GetId: %v", e)
					return
				}
				out = append(out, id)
			}
			got[gi] = out
		})
	}
	wg.Wait()

	seen := make(map[identifier.TaggedId]struct{}, goroutines*perG)
	for _, out := range got {
		for _, id := range out {
			_, dup := seen[id]
			require.False(t, dup)
			seen[id] = struct{}{}
		}
	}
	require.Len(t, seen, goroutines*perG)
}

// TestInternalizer_ConcurrentDedup hammers one Internalizer with overlapping key
// sets from many goroutines (validated under -race) and asserts dedup holds.
func TestInternalizer_ConcurrentDedup(t *testing.T) {
	m, err := leased.NewInternalizer(memalloc.NewAllocator(), identifier.TagValue(6), 8)
	require.NoError(t, err)

	keys := []string{"a", "b", "c", "d", "e"}
	const goroutines = 8
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range 50 {
				for _, k := range keys {
					if _, _, e := m.GetId(context.Background(), []byte(k)); e != nil {
						t.Errorf("GetId: %v", e)
						return
					}
				}
			}
		})
	}
	wg.Wait()

	require.Equal(t, len(keys), m.Len()) // dedup held under concurrency
	ids := make(map[identifier.TaggedId]struct{})
	for _, k := range keys {
		id, fresh, err := m.GetId(context.Background(), []byte(k))
		require.NoError(t, err)
		require.False(t, fresh) // already assigned during the concurrent phase
		ids[id] = struct{}{}
	}
	require.Len(t, ids, len(keys))
}

// TestFactories exercises the IdGeneratorFactoryI surface for both flavours.
func TestFactories(t *testing.T) {
	alloc := memalloc.NewAllocator()

	sf := leased.NewSequenceFactory(alloc)
	var _ identifier.IdGeneratorFactoryI = sf
	g1, err := sf.Create(identifier.TagValue(2), 16)
	require.NoError(t, err)
	id, fresh, err := g1.GetId(context.Background(), nil)
	require.NoError(t, err)
	require.True(t, fresh)
	require.True(t, id.IsValid())
	require.EqualValues(t, 2, id.GetTag().GetValue())

	// Two sequential generators over one allocator draw disjoint ids (no
	// one-generator-per-tag restriction).
	g2, err := sf.Create(identifier.TagValue(2), 16)
	require.NoError(t, err)
	idA, _, err := g1.GetId(context.Background(), nil)
	require.NoError(t, err)
	idB, _, err := g2.GetId(context.Background(), nil)
	require.NoError(t, err)
	require.NotEqual(t, idA, idB)

	nf := leased.NewInternalizerFactory(memalloc.NewAllocator())
	var _ identifier.IdGeneratorFactoryI = nf
	gi, err := nf.Create(identifier.TagValue(9), 8)
	require.NoError(t, err)
	x, freshX, err := gi.GetId(context.Background(), []byte("dup"))
	require.NoError(t, err)
	require.True(t, freshX)
	y, freshY, err := gi.GetId(context.Background(), []byte("dup"))
	require.NoError(t, err)
	require.False(t, freshY)
	require.Equal(t, x, y)
}
