package containers

// Regression suite for the 2026-07-05 adversarial review of this package.
// Each D-numbered block is the inverted repro of one confirmed defect:
// the test asserts the fixed behaviour and fails on the pre-fix code.
//
// D1 — compactNewestWins replaced the surviving key with the newest
//      spelling, while UpsertSingle/MergeValue keep the resident key.
//      Under an equivalence comparator (case-insensitive, locale, …)
//      the documented Single/Batch equivalence was violated.

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
