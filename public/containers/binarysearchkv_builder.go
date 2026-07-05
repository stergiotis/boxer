package containers

import (
	"cmp"
	"iter"
	"slices"
)

// BinarySearchGrowingKVBuilder accumulates (key, value) pairs in a
// write-only staging area, then produces a sorted-and-compacted
// [BinarySearchGrowingKV] via [BinarySearchGrowingKVBuilder.Freeze].
//
// The builder has no read methods (no Get, Has, IteratePairs). That is
// the point — the Has-gated-UpsertBatch antipattern documented on
// [BinarySearchGrowingKV.UpsertBatch] becomes a compile-time error
// rather than a silent N-fold perf regression. A build phase that
// genuinely needs to read the in-progress state is a build phase that
// should use [BinarySearchGrowingKV.UpsertSingle] instead.
//
// Single-use: after Freeze, subsequent Stage / StageSeq / Freeze calls
// panic. Allocate a fresh builder for the next build phase.
//
// Not safe for concurrent use.
type BinarySearchGrowingKVBuilder[K any, V any] struct {
	keys    []K
	vals    []V
	cmpKey  func(a K, b K) int
	bsearch func(keys []K, target K) (int, bool)
	frozen  bool
}

// NewBinarySearchGrowingKVBuilder allocates a builder backed by slices
// of the given estimated capacity. The estimate should be the expected
// *total* number of Stage / StageSeq inserts (not the unique-key count),
// to avoid growslice reallocations during build. See
// [BinarySearchGrowingKV.UpsertBatch]'s docstring for the rationale.
//
// Prefer [NewBinarySearchGrowingKVBuilderOrdered] when K satisfies
// cmp.Ordered — the produced container's point-lookup methods will use
// an inlined comparator on the hot path. See [NewBinarySearchGrowingKV]
// for the cost comparison.
func NewBinarySearchGrowingKVBuilder[K any, V any](estSize int, cmpKey func(a K, b K) int) *BinarySearchGrowingKVBuilder[K, V] {
	return &BinarySearchGrowingKVBuilder[K, V]{
		keys:   make([]K, 0, estSize),
		vals:   make([]V, 0, estSize),
		cmpKey: cmpKey,
		bsearch: func(keys []K, target K) (int, bool) {
			return slices.BinarySearchFunc(keys, target, cmpKey)
		},
	}
}

// NewBinarySearchGrowingKVBuilderOrdered is the cmp.Ordered
// convenience variant. The produced container dispatches point-lookups
// (Has, Get, GetDefault, Delete) through an inlined-comparator binary
// search — see [NewBinarySearchGrowingKVOrdered] for the rationale.
func NewBinarySearchGrowingKVBuilderOrdered[K cmp.Ordered, V any](estSize int) *BinarySearchGrowingKVBuilder[K, V] {
	return &BinarySearchGrowingKVBuilder[K, V]{
		keys:   make([]K, 0, estSize),
		vals:   make([]V, 0, estSize),
		cmpKey: cmp.Compare[K],
		bsearch: func(keys []K, target K) (int, bool) {
			return slices.BinarySearch(keys, target)
		},
	}
}

// Stage appends one (key, value) pair to the staging buffer. O(1) per
// call. Panics if Freeze has already been called.
func (b *BinarySearchGrowingKVBuilder[K, V]) Stage(key K, val V) {
	if b.frozen {
		panic("BinarySearchGrowingKVBuilder: Stage after Freeze")
	}
	b.keys = append(b.keys, key)
	b.vals = append(b.vals, val)
}

// StageSeq appends every (key, value) pair from the iterator. Useful
// when the source is already an iter.Seq2 (e.g. maps.All on a map, a
// zip of two slices, a filter pipeline). Panics if Freeze has already
// been called.
func (b *BinarySearchGrowingKVBuilder[K, V]) StageSeq(pairs iter.Seq2[K, V]) {
	if b.frozen {
		panic("BinarySearchGrowingKVBuilder: StageSeq after Freeze")
	}
	for k, v := range pairs {
		b.keys = append(b.keys, k)
		b.vals = append(b.vals, v)
	}
}

// Len returns the raw count of staged inserts. Duplicate keys are not
// collapsed at this point, so the value can exceed
// Freeze().Len(). Useful for sizing decisions before Freeze.
func (b *BinarySearchGrowingKVBuilder[K, V]) Len() int {
	return len(b.keys)
}

// Freeze produces a sorted-and-compacted [BinarySearchGrowingKV] from
// the staged pairs and marks the builder as consumed. Subsequent
// Stage / StageSeq / Freeze calls panic.
//
// Among duplicate keys, the value of the latest Stage call survives —
// same newest-wins semantics as [BinarySearchGrowingKV.UpsertBatch].
//
// The returned container owns the builder's backing slices (no copy).
// Freezing eagerly runs sort + compact so the returned container is
// already in the ready-for-read state; freeze cost is therefore
// O(N log N) on N = total staged inserts.
func (b *BinarySearchGrowingKVBuilder[K, V]) Freeze() *BinarySearchGrowingKV[K, V] {
	if b.frozen {
		panic("BinarySearchGrowingKVBuilder: Freeze called twice")
	}
	b.frozen = true
	kv := &BinarySearchGrowingKV[K, V]{
		keys:    b.keys,
		vals:    b.vals,
		cmpKey:  b.cmpKey,
		bsearch: b.bsearch,
		flushed: false,
	}
	kv.ensureSorted()
	return kv
}
