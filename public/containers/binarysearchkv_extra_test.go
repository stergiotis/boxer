package containers

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- MergeValue ---------------------------------------------------------

func TestMergeValue_InsertWhenAbsent(t *testing.T) {
	dict := NewBinarySearchGrowingKVOrdered[string, string](4)
	merge := func(old, new string) string { return old + "+" + new }

	existed := dict.MergeValue("k1", "v1", merge)
	require.False(t, existed)
	require.Equal(t, 1, dict.Len())
	v, _ := dict.Get("k1")
	require.Equal(t, "v1", v)
}

func TestMergeValue_ReplacesViaCallback(t *testing.T) {
	dict := NewBinarySearchGrowingKVOrdered[string, string](4)
	merge := func(old, new string) string { return old + "+" + new }

	dict.UpsertSingle("k1", "v1")
	existed := dict.MergeValue("k1", "v2", merge)
	require.True(t, existed)
	v, _ := dict.Get("k1")
	require.Equal(t, "v1+v2", v, "merge(old,new) — old is the resident value")
}

func TestMergeValue_AfterDeferredBatch_SeesNewestCompacted(t *testing.T) {
	// Compaction is newest-wins. A subsequent MergeValue must merge against
	// the compacted (newest) value, not against an earlier shadow.
	dict := NewBinarySearchGrowingKVOrdered[string, string](4)
	dict.UpsertBatch("k", "old")
	dict.UpsertBatch("k", "new")
	existed := dict.MergeValue("k", "added", func(old, new string) string {
		require.Equal(t, "new", old, "MergeValue must merge against the compacted (newest) value")
		return old + "+" + new
	})
	require.True(t, existed)
	v, _ := dict.Get("k")
	require.Equal(t, "new+added", v)
}

// --- Reset --------------------------------------------------------------

func TestReset_ClearsContent(t *testing.T) {
	dict := NewBinarySearchGrowingKVOrdered[string, int](4)
	dict.UpsertSingle("k1", 1)
	dict.UpsertSingle("k2", 2)
	dict.Reset()
	require.True(t, dict.IsEmpty())
	require.Equal(t, 0, dict.Len())
	require.False(t, dict.Has("k1"))
	require.False(t, dict.Has("k2"))
}

func TestReset_ZeroesPointerValuedSlots(t *testing.T) {
	// Pointer-valued entries removed via Reset must not keep their referents
	// reachable through the trailing slice capacity, otherwise GC can't
	// reclaim them and pointer values silently leak past their entry's
	// lifetime.
	dict := NewBinarySearchGrowingKVOrdered[string, *int](4)
	a, b := 10, 20
	dict.UpsertSingle("a", &a)
	dict.UpsertSingle("b", &b)
	dict.Reset()

	// Inspect backing array through capacity, post-Reset every slot must be nil.
	tail := dict.vals[:cap(dict.vals)]
	for i, p := range tail {
		require.Nil(t, p, "vals[%d] not cleared", i)
	}
}

func TestReset_AllowsReuse(t *testing.T) {
	dict := NewBinarySearchGrowingKVOrdered[string, int](4)
	dict.UpsertBatch("k1", 1)
	dict.UpsertBatch("k2", 2)
	dict.Reset()
	dict.UpsertBatch("z", 99)
	dict.UpsertBatch("a", 0)
	require.Equal(t, []string{"a", "z"}, slices.Collect(dict.IterateKeys()))
}

// --- Grow ---------------------------------------------------------------

func TestGrow_IncreasesCapacityWithoutAffectingContent(t *testing.T) {
	dict := NewBinarySearchGrowingKVOrdered[string, int](2)
	dict.UpsertSingle("a", 1)
	dict.UpsertSingle("b", 2)
	prevCap := cap(dict.keys)
	dict.Grow(1024)
	require.GreaterOrEqual(t, cap(dict.keys), prevCap+1024)
	require.GreaterOrEqual(t, cap(dict.vals), prevCap+1024)
	require.Equal(t, 2, dict.Len())
	v, _ := dict.Get("a")
	require.Equal(t, 1, v)
}

// --- Iterator early termination ----------------------------------------

func TestIterateKeys_EarlyTermination(t *testing.T) {
	dict := NewBinarySearchGrowingKVOrdered[string, int](4)
	dict.UpsertBatch("a", 1)
	dict.UpsertBatch("b", 2)
	dict.UpsertBatch("c", 3)

	var seen []string
	for k := range dict.IterateKeys() {
		seen = append(seen, k)
		if len(seen) == 2 {
			break
		}
	}
	require.Equal(t, []string{"a", "b"}, seen)
}

func TestIterateValues_EarlyTermination(t *testing.T) {
	dict := NewBinarySearchGrowingKVOrdered[string, int](4)
	dict.UpsertBatch("a", 1)
	dict.UpsertBatch("b", 2)
	dict.UpsertBatch("c", 3)

	var seen []int
	for v := range dict.IterateValues() {
		seen = append(seen, v)
		if len(seen) == 1 {
			break
		}
	}
	require.Equal(t, []int{1}, seen)
}

func TestIteratePairs_EarlyTermination(t *testing.T) {
	dict := NewBinarySearchGrowingKVOrdered[string, int](4)
	dict.UpsertBatch("a", 1)
	dict.UpsertBatch("b", 2)
	dict.UpsertBatch("c", 3)

	var seenK []string
	var seenV []int
	for k, v := range dict.IteratePairs() {
		seenK = append(seenK, k)
		seenV = append(seenV, v)
		if len(seenK) == 2 {
			break
		}
	}
	require.Equal(t, []string{"a", "b"}, seenK)
	require.Equal(t, []int{1, 2}, seenV)
}

// --- IterateMergedBinarySearchGrowingKVKeys -----------------------------

func TestIterateMergedKeys(t *testing.T) {
	mk := func(keys ...string) *BinarySearchGrowingKV[string, int] {
		d := NewBinarySearchGrowingKVOrdered[string, int](len(keys))
		for i, k := range keys {
			d.UpsertBatch(k, i)
		}
		return d
	}
	cases := []struct {
		name string
		a, b *BinarySearchGrowingKV[string, int]
		want []string
	}{
		{"both_empty", mk(), mk(), nil},
		{"a_empty", mk(), mk("x", "y"), []string{"x", "y"}},
		{"b_empty", mk("p", "q"), mk(), []string{"p", "q"}},
		{"disjoint", mk("a", "c"), mk("b", "d"), []string{"a", "b", "c", "d"}},
		{"full_overlap", mk("a", "b"), mk("a", "b"), []string{"a", "b"}},
		{"partial_overlap", mk("a", "c", "e"), mk("b", "c", "d"), []string{"a", "b", "c", "d", "e"}},
		{"subset", mk("a", "b", "c"), mk("b"), []string{"a", "b", "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slices.Collect(IterateMergedBinarySearchGrowingKVKeys(tc.a, tc.b))
			if tc.want == nil {
				require.Empty(t, got)
			} else {
				require.Equal(t, tc.want, got)
			}
		})
	}
}

// --- Empty-container invariants -----------------------------------------

func TestEmpty_AllReadsAreSafe(t *testing.T) {
	dict := NewBinarySearchGrowingKVOrdered[string, int](4)
	require.True(t, dict.IsEmpty())
	require.Equal(t, 0, dict.Len())
	require.False(t, dict.Has("anything"))
	v, has := dict.Get("anything")
	require.False(t, has)
	require.Zero(t, v)
	require.Equal(t, 42, dict.GetDefault("anything", 42))
	require.False(t, dict.Delete("anything"))
	require.Empty(t, slices.Collect(dict.IterateKeys()))
	require.Empty(t, slices.Collect(dict.IterateValues()))
	count := 0
	for range dict.IteratePairs() {
		count++
	}
	require.Equal(t, 0, count)
}

// --- Mixed mode transitions ---------------------------------------------

func TestMixedSingleAndBatch_NewestWins(t *testing.T) {
	// Interleave Single and Batch with intervening reads. Every flag
	// transition (sorted/compacted true→false→true) must preserve a
	// consistent newest-wins view.
	dict := NewBinarySearchGrowingKVOrdered[string, string](4)
	dict.UpsertSingle("k", "s1")
	require.Equal(t, "s1", dict.GetDefault("k", ""))

	dict.UpsertBatch("k", "b1")
	require.Equal(t, "b1", dict.GetDefault("k", ""), "batch after single — read should see b1")

	dict.UpsertSingle("k", "s2") // ensureSorted runs, then in-place replace
	require.Equal(t, "s2", dict.GetDefault("k", ""))

	dict.UpsertBatch("k", "b2")
	dict.UpsertBatch("k", "b3")
	require.Equal(t, "b3", dict.GetDefault("k", ""), "newest of two batches wins")

	dict.UpsertSingle("k", "s3")
	dict.UpsertBatch("other", "o1")
	require.Equal(t, "s3", dict.GetDefault("k", ""))
	require.Equal(t, "o1", dict.GetDefault("other", ""))
}

func TestUpsertBatch_DuplicateInSamePhase_NewestWins(t *testing.T) {
	dict := NewBinarySearchGrowingKVOrdered[string, int](4)
	for i := range 5 {
		dict.UpsertBatch("k", i)
	}
	v, _ := dict.Get("k")
	require.Equal(t, 4, v, "compaction must keep the last (newest) insertion")
	require.Equal(t, 1, dict.Len())
}

// TestLen_FlushesDeferredBatchState pins that Len forces ensureSorted so the
// count reflects post-compaction unique entries. Without this flush, Len
// would over-report by the number of shadowed duplicates in a deferred
// batch — a bug the differential fuzz originally caught.
func TestLen_FlushesDeferredBatchState(t *testing.T) {
	dict := NewBinarySearchGrowingKVOrdered[string, int](4)
	dict.UpsertBatch("k", 1)
	dict.UpsertBatch("k", 2)
	dict.UpsertBatch("k", 3)
	require.Equal(t, 1, dict.Len(), "three writes to the same key collapse to one entry")

	collected := slices.Collect(dict.IterateKeys())
	require.Len(t, collected, 1, "iteration sees the same compacted state")
	require.Equal(t, 1, dict.Len(), "second Len is a no-op on the already-flushed container")
}

// --- Delete after deferred batch with many duplicates -------------------

func TestDelete_AfterBatchWithDuplicates(t *testing.T) {
	dict := NewBinarySearchGrowingKVOrdered[string, int](8)
	dict.UpsertBatch("a", 1)
	dict.UpsertBatch("b", 1)
	dict.UpsertBatch("a", 2)
	dict.UpsertBatch("c", 1)
	dict.UpsertBatch("a", 3)
	dict.UpsertBatch("b", 2)

	require.True(t, dict.Delete("a"))
	require.False(t, dict.Has("a"))
	require.Equal(t, 2, dict.Len())
	v, _ := dict.Get("b")
	require.Equal(t, 2, v)
	v, _ = dict.Get("c")
	require.Equal(t, 1, v)
}

// --- Differential fuzz against map[K]V oracle ---------------------------

// TestDifferentialFuzz_AgainstMap exercises mixed Single/Batch/Delete/Merge
// operations and reconciles BSKV against a stdlib map oracle every K ops.
// Deterministic seed by default; override via FUZZ_SEED env var when
// reproducing a regression.
func TestDifferentialFuzz_AgainstMap(t *testing.T) {
	const (
		steps        = 5000
		checkEvery   = 25
		keyspaceSize = 12 // small to force collisions, exercise compaction
	)
	rnd := rand.New(rand.NewPCG(0xC0FFEEC0FFEE, 0xDEADBEEFCAFEBABE))

	bskv := NewBinarySearchGrowingKVOrdered[string, string](16)
	oracle := map[string]string{}
	merge := func(old, new string) string { return old + "|" + new }

	keyspace := make([]string, keyspaceSize)
	for i := range keyspace {
		keyspace[i] = fmt.Sprintf("k%02d", i)
	}

	type opRec struct {
		kind string
		k, v string
	}
	var log []opRec

	check := func(after int) {
		t.Helper()
		// Use iteration count for size, not Len(): see
		// TestLen_AfterDeferredBatchWithDuplicates_KnownInconsistency.
		// Iteration also forces ensureSorted, so subsequent Get probes
		// see compacted state.
		iterated := maps.Collect(bskv.IteratePairs())
		require.Equal(t, len(oracle), len(iterated), "size mismatch after step %d, log=%v", after, log)
		for k, want := range oracle {
			got, has := bskv.Get(k)
			require.True(t, has, "missing key %q after step %d, log=%v", k, after, log)
			require.Equal(t, want, got, "value mismatch for %q after step %d, log=%v", k, after, log)
		}
		for k, v := range iterated {
			want, ok := oracle[k]
			require.True(t, ok, "BSKV has extra key %q=%q after step %d, log=%v", k, v, after, log)
			require.Equal(t, want, v)
		}
		keys := slices.Collect(bskv.IterateKeys())
		require.True(t, slices.IsSorted(keys), "keys not sorted after step %d: %v", after, keys)
	}

	for step := range steps {
		k := keyspace[rnd.IntN(len(keyspace))]
		v := fmt.Sprintf("v%d", step)
		switch rnd.IntN(20) {
		case 0, 1, 2, 3:
			log = append(log, opRec{"single", k, v})
			bskv.UpsertSingle(k, v)
			oracle[k] = v
		case 4, 5, 6, 7, 8, 9:
			log = append(log, opRec{"batch", k, v})
			bskv.UpsertBatch(k, v)
			oracle[k] = v
		case 10, 11:
			log = append(log, opRec{"delete", k, ""})
			wantExisted := false
			if _, ok := oracle[k]; ok {
				wantExisted = true
				delete(oracle, k)
			}
			require.Equal(t, wantExisted, bskv.Delete(k), "delete existence flag mismatch at step %d", step)
		case 12, 13:
			log = append(log, opRec{"merge", k, v})
			wantExisted := false
			if cur, ok := oracle[k]; ok {
				wantExisted = true
				oracle[k] = merge(cur, v)
			} else {
				oracle[k] = v
			}
			require.Equal(t, wantExisted, bskv.MergeValue(k, v, merge),
				"merge existence flag mismatch at step %d", step)
		case 14, 15, 16, 17, 18:
			// Read probe at random key.
			wantV, wantOK := oracle[k]
			gotV, gotOK := bskv.Get(k)
			require.Equal(t, wantOK, gotOK, "Get(%q) ok mismatch at step %d", k, step)
			if wantOK {
				require.Equal(t, wantV, gotV, "Get(%q) value mismatch at step %d", k, step)
			}
		case 19:
			log = append(log, opRec{"reset", "", ""})
			bskv.Reset()
			oracle = map[string]string{}
		}
		if step%checkEvery == 0 {
			check(step)
		}
	}
	check(steps)
}

// TestDifferentialFuzz_AgainstMap_EquivalenceComparator re-runs the
// mixed-operation fuzz with a case-insensitive comparator, i.e. a cmpKey
// that treats distinguishable keys as equal. The oracle tracks, per
// canonical (lowercased) key, the first-inserted spelling and the newest
// value: UpsertSingle, UpsertBatch and MergeValue must all keep the
// resident spelling while replacing/merging the value; Delete erases the
// entry so a re-insert adopts the new spelling.
func TestDifferentialFuzz_AgainstMap_EquivalenceComparator(t *testing.T) {
	const (
		steps        = 4000
		checkEvery   = 25
		keyspaceSize = 6 // canonical keys; spellings vary per op
	)
	rnd := rand.New(rand.NewPCG(0xBADC0FFEE, 0xFACEFEED))

	type entry struct {
		spelling string
		val      string
	}
	bskv := NewBinarySearchGrowingKV[string, string](16, ciCompare)
	oracle := map[string]entry{}
	merge := func(old, new string) string { return old + "|" + new }

	canon := make([]string, keyspaceSize)
	for i := range canon {
		canon[i] = fmt.Sprintf("key%02d", i)
	}
	// spell returns a random-case spelling of canon[i].
	spell := func(i int) string {
		b := []byte(canon[i])
		for j := range b {
			if rnd.IntN(2) == 0 {
				b[j] = byte(strings.ToUpper(string(b[j]))[0])
			}
		}
		return string(b)
	}

	check := func(after int) {
		t.Helper()
		pairs := maps.Collect(bskv.IteratePairs())
		require.Equal(t, len(oracle), len(pairs), "size mismatch after step %d", after)
		for k, v := range pairs {
			want, ok := oracle[strings.ToLower(k)]
			require.True(t, ok, "extra key %q after step %d", k, after)
			require.Equal(t, want.spelling, k, "resident spelling mismatch after step %d", after)
			require.Equal(t, want.val, v, "value mismatch for %q after step %d", k, after)
		}
		keys := slices.Collect(bskv.IterateKeys())
		require.True(t, slices.IsSortedFunc(keys, ciCompare), "keys not ci-sorted after step %d: %v", after, keys)
	}

	for step := range steps {
		i := rnd.IntN(keyspaceSize)
		k := spell(i)
		ck := canon[i]
		v := fmt.Sprintf("v%d", step)
		switch rnd.IntN(10) {
		case 0, 1:
			bskv.UpsertSingle(k, v)
			if e, ok := oracle[ck]; ok {
				oracle[ck] = entry{spelling: e.spelling, val: v}
			} else {
				oracle[ck] = entry{spelling: k, val: v}
			}
		case 2, 3, 4:
			bskv.UpsertBatch(k, v)
			if e, ok := oracle[ck]; ok {
				oracle[ck] = entry{spelling: e.spelling, val: v}
			} else {
				oracle[ck] = entry{spelling: k, val: v}
			}
		case 5:
			wantExisted := false
			if _, ok := oracle[ck]; ok {
				wantExisted = true
				delete(oracle, ck)
			}
			require.Equal(t, wantExisted, bskv.Delete(k), "delete existence mismatch at step %d", step)
		case 6:
			wantExisted := false
			if e, ok := oracle[ck]; ok {
				wantExisted = true
				oracle[ck] = entry{spelling: e.spelling, val: merge(e.val, v)}
			} else {
				oracle[ck] = entry{spelling: k, val: v}
			}
			require.Equal(t, wantExisted, bskv.MergeValue(k, v, merge), "merge existence mismatch at step %d", step)
		case 7, 8:
			// Get probe with an independently randomized spelling.
			q := spell(i)
			want, wantOK := oracle[ck]
			got, gotOK := bskv.Get(q)
			require.Equal(t, wantOK, gotOK, "Get(%q) ok mismatch at step %d", q, step)
			if wantOK {
				require.Equal(t, want.val, got, "Get(%q) value mismatch at step %d", q, step)
			}
		case 9:
			if rnd.IntN(20) == 0 { // rare full reset
				bskv.Reset()
				oracle = map[string]entry{}
			}
		}
		if step%checkEvery == 0 {
			check(step)
		}
	}
	check(steps)
}

// --- Concurrency-stance guard ------------------------------------------

// TestConcurrentReads_DocumentationGuard pins the current behaviour: even
// pure reads mutate internal state (the sorted/compacted flags) via
// ensureSorted. If concurrency safety is ever added, this test should fail
// and be deleted along with this comment.
func TestConcurrentReads_NotSafe_DocumentationGuard(t *testing.T) {
	dict := NewBinarySearchGrowingKVOrdered[string, int](4)
	dict.UpsertBatch("a", 1)
	require.False(t, dict.sorted, "fresh deferred state is unsorted — sanity")
	_ = dict.Has("a")
	require.True(t, dict.sorted, "Has mutated sorted flag — readers are not concurrent-safe")
}

// --- Smoke test: long descending insert path ----------------------------

func TestUpsertSingle_DescendingInsert(t *testing.T) {
	// Each UpsertSingle binary-searches into a sorted slice; descending
	// inserts hit index 0 every time (worst case for slices.Insert shift).
	dict := NewBinarySearchGrowingKVOrdered[string, int](16)
	for i := 99; i >= 0; i-- {
		dict.UpsertSingle(fmt.Sprintf("k%02d", i), i)
	}
	require.Equal(t, 100, dict.Len())
	keys := slices.Collect(dict.IterateKeys())
	require.True(t, slices.IsSorted(keys))
	for i, k := range keys {
		require.Equal(t, fmt.Sprintf("k%02d", i), k)
	}
}

// --- Helper used by benchmarks: assert visible in test build too -------

func TestGenKeys_Helper(t *testing.T) {
	keys := genStringKeys(64, 8, 1)
	require.Len(t, keys, 64)
	require.True(t, len(strings.Join(keys, "")) == 64*8)
	// Uniqueness invariant the bench relies on.
	seen := map[string]struct{}{}
	for _, k := range keys {
		_, dup := seen[k]
		require.False(t, dup, "genStringKeys produced duplicate: %q", k)
		seen[k] = struct{}{}
	}
}
