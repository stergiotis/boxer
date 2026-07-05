package containers

// Regression suite for the 2026-07-05 adversarial review of this package.
// Each D-numbered block is the inverted repro of one confirmed defect:
// the test asserts the fixed behaviour and fails on the pre-fix code.
//
// D1 — compactNewestWins replaced the surviving key with the newest
//      spelling, while UpsertSingle/MergeValue keep the resident key.
//      Under an equivalence comparator (case-insensitive, locale, …)
//      the documented Single/Batch equivalence was violated.
// D2 — Iterate{Keys,Values,Pairs} and IterateMergedBinarySearchGrowingKVKeys
//      ran ensureSorted when the Seq was constructed but read the slices
//      when ranging began: a mutation between the two silently yielded an
//      unsorted, duplicate-bearing view. The flush now happens at the
//      start of each range.
// D3 — NewBinarySearchGrowingKVFromAnyMap documents its nil return as an
//      early-out value, but every method panicked on a nil receiver. A
//      nested empty YAML map produced a typed-nil KV inside an interface
//      and crashed the egui2 markdown frontmatter renderer. Reads are
//      now nil-tolerant; writes still panic.

import (
	"cmp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ciCompare treats keys as equal when they differ only by ASCII case —
// the class of comparator the type doc advertises ("case-insensitive,
// locale, byte-slice").
func ciCompare(a, b string) int {
	return cmp.Compare(strings.ToLower(a), strings.ToLower(b))
}

type kvPair struct {
	K string
	V int
}

func collectPairs(kv *BinarySearchGrowingKV[string, int]) (out []kvPair) {
	for k, v := range kv.IteratePairs() {
		out = append(out, kvPair{K: k, V: v})
	}
	return
}

// TestD1_SingleVsBatch_SameObservableState pins the UpsertBatch doc
// claim: after flush, the container is in the same observable state as
// if the equivalent UpsertSingle sequence had been issued.
func TestD1_SingleVsBatch_SameObservableState(t *testing.T) {
	ops := []kvPair{
		{"Alpha", 1}, {"BETA", 2}, {"alpha", 3}, {"beta", 4}, {"GAMMA", 5}, {"gamma", 6},
	}

	single := NewBinarySearchGrowingKV[string, int](8, ciCompare)
	for _, op := range ops {
		single.UpsertSingle(op.K, op.V)
	}
	batch := NewBinarySearchGrowingKV[string, int](8, ciCompare)
	for _, op := range ops {
		batch.UpsertBatch(op.K, op.V)
	}

	sp := collectPairs(single)
	bp := collectPairs(batch)
	require.Equal(t, sp, bp, "Single and Batch paths must produce identical entries and order")
	require.Equal(t, []kvPair{{"Alpha", 3}, {"BETA", 4}, {"GAMMA", 6}}, sp,
		"first-inserted key spelling with newest value")
}

// TestD1_BatchDuplicates_FirstSpellingNewestValue pins the compaction
// rule in isolation: newest value wins, first key spelling is retained.
func TestD1_BatchDuplicates_FirstSpellingNewestValue(t *testing.T) {
	kv := NewBinarySearchGrowingKV[string, int](8, ciCompare)
	kv.UpsertBatch("Key", 1)
	kv.UpsertBatch("KEY", 2)
	kv.UpsertBatch("kEy", 3)

	require.Equal(t, 1, kv.Len())
	require.Equal(t, []string{"Key"}, slices.Collect(kv.IterateKeys()),
		"resident (first-inserted) spelling survives compaction")
	v, has := kv.Get("key")
	require.True(t, has)
	require.Equal(t, 3, v, "newest value survives compaction")
}

// TestD1_SingleThenBatch_KeepsResidentSpelling covers the mixed path:
// a batch upsert onto a key resident via UpsertSingle must not swap the
// spelling during the deferred flush.
func TestD1_SingleThenBatch_KeepsResidentSpelling(t *testing.T) {
	kv := NewBinarySearchGrowingKV[string, int](8, ciCompare)
	kv.UpsertSingle("Key", 1)
	kv.UpsertBatch("KEY", 2)

	require.Equal(t, []string{"Key"}, slices.Collect(kv.IterateKeys()))
	require.Equal(t, 2, kv.GetDefault("key", -1))
}

// TestD2_IteratorObtainedBeforeMutation_SeesFlushedView pins that a Seq
// obtained before UpsertBatch calls iterates the sorted, compacted
// post-mutation view (pre-fix: raw append order including duplicates).
func TestD2_IteratorObtainedBeforeMutation_SeesFlushedView(t *testing.T) {
	mk := func() *BinarySearchGrowingKV[string, int] {
		kv := NewBinarySearchGrowingKVOrdered[string, int](8)
		kv.UpsertSingle("b", 2)
		kv.UpsertSingle("c", 3)
		return kv
	}
	mutate := func(kv *BinarySearchGrowingKV[string, int]) {
		kv.UpsertBatch("a", 1) // new key, sorts before existing ones
		kv.UpsertBatch("b", 9) // duplicate key, newest value must win
	}

	t.Run("keys", func(t *testing.T) {
		kv := mk()
		seq := kv.IterateKeys()
		mutate(kv)
		require.Equal(t, []string{"a", "b", "c"}, slices.Collect(seq))
	})
	t.Run("values", func(t *testing.T) {
		kv := mk()
		seq := kv.IterateValues()
		mutate(kv)
		require.Equal(t, []int{1, 9, 3}, slices.Collect(seq))
	})
	t.Run("pairs", func(t *testing.T) {
		kv := mk()
		seq := kv.IteratePairs()
		mutate(kv)
		var got []kvPair
		for k, v := range seq {
			got = append(got, kvPair{K: k, V: v})
		}
		require.Equal(t, []kvPair{{"a", 1}, {"b", 9}, {"c", 3}}, got)
	})
	t.Run("merged_keys", func(t *testing.T) {
		a := mk()
		b := NewBinarySearchGrowingKVOrdered[string, uint8](4)
		b.UpsertSingle("d", 4)
		seq := IterateMergedBinarySearchGrowingKVKeys(a, b)
		mutate(a)
		b.UpsertBatch("e", 5)
		require.Equal(t, []string{"a", "b", "c", "d", "e"}, slices.Collect(seq))
	})
}

// TestD2_RangeTwiceWithInterveningMutation pins that the same Seq value
// re-ranged after a mutation reflects the newer flushed state — each
// range start re-runs the flush.
func TestD2_RangeTwiceWithInterveningMutation(t *testing.T) {
	kv := NewBinarySearchGrowingKVOrdered[string, int](8)
	kv.UpsertBatch("b", 2)
	seq := kv.IterateKeys()

	require.Equal(t, []string{"b"}, slices.Collect(seq))
	kv.UpsertBatch("a", 1)
	require.Equal(t, []string{"a", "b"}, slices.Collect(seq))
}

// TestD3_NilKV_ReadsAreSafe pins the nil-receiver read contract: a nil
// *BinarySearchGrowingKV behaves as an empty container for every read
// entry point.
func TestD3_NilKV_ReadsAreSafe(t *testing.T) {
	var kv *BinarySearchGrowingKV[string, int]
	require.True(t, kv.IsEmpty())
	require.Equal(t, 0, kv.Len())
	require.False(t, kv.Has("x"))
	v, has := kv.Get("x")
	require.False(t, has)
	require.Zero(t, v)
	require.Equal(t, 42, kv.GetDefault("x", 42))
	require.Empty(t, slices.Collect(kv.IterateKeys()))
	require.Empty(t, slices.Collect(kv.IterateValues()))
	for range kv.IteratePairs() {
		t.Fatal("nil KV must yield nothing")
	}
}

// TestD3_FromAnyMap_NestedEmptyMap_ReadsSafely pins the concrete crash
// shape: a nested empty map converts to a typed-nil KV stored in an
// interface value, and reads on it must behave as an empty container.
func TestD3_FromAnyMap_NestedEmptyMap_ReadsSafely(t *testing.T) {
	kv := NewBinarySearchGrowingKVFromAnyMap(map[string]interface{}{
		"nested": map[string]interface{}{},
		"flat":   1,
	})
	require.NotNil(t, kv)
	nestedRaw, has := kv.Get("nested")
	require.True(t, has)
	nested, ok := nestedRaw.(*BinarySearchGrowingKV[string, interface{}])
	require.True(t, ok, "nested empty map converts to a (typed-nil) KV")
	require.Nil(t, nested)
	require.True(t, nested.IsEmpty())
	require.Equal(t, 0, nested.Len())
	for range nested.IteratePairs() {
		t.Fatal("typed-nil nested KV must yield nothing")
	}
}
