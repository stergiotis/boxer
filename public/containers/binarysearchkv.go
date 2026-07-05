package containers

import (
	"cmp"
	"sort"

	"iter"
	"slices"

	"github.com/stergiotis/boxer/public/containers/co"
)

// BinarySearchGrowingKV is a sorted-iteration key-value container backed by
// two parallel slices ([]K, []V). Reads are O(log N) binary search; iteration
// is in cmpKey-ascending order with cache-friendly sequential access. Writes
// come in two flavours with very different cost profiles — see UpsertSingle
// and UpsertBatch.
//
// The container internally tracks a flushed flag (sorted + compacted).
// Every read entry point (Has, Get, GetDefault, Len, IterateKeys,
// IterateValues, IteratePairs, IterateFrom, IterateRange, MergeValue,
// UpsertSingle, Delete) transparently invokes ensureSorted before
// operating — the Iterate* methods at the start of each range — so
// callers never need to flush manually.
// UpsertBatch is the only writer that defers — see its docstring for the
// full cost model, invariants, and antipatterns.
//
// Iteration order is deterministic across runs and across the choice of
// UpsertSingle vs UpsertBatch: cmpKey-ascending. Among equal-cmpKey
// entries the newest value wins while the first-inserted key spelling is
// retained (relevant only for comparators that treat distinguishable
// keys as equal).
//
// Not safe for concurrent use. Even pure reads mutate the flushed flag
// via ensureSorted, so readers and writers must serialise externally.
//
// A nil *BinarySearchGrowingKV is a valid empty container for reads:
// IsEmpty, Len, Has, Get, GetDefault and the Iterate* methods return
// zero values or empty sequences on a nil receiver. Write methods
// (UpsertSingle, UpsertBatch, MergeValue, Delete, Grow, Reset) panic on
// nil. This makes the nil early-out of
// [NewBinarySearchGrowingKVFromAnyMap] safe to hand to read-only
// consumers.
//
// Compared to map[K]V: this container is preferred when iteration must be
// deterministic and sorted, when K is not comparable (so map[K]V is not an
// option), or when a custom cmpKey (case-insensitive, locale, byte-slice)
// is needed. For point lookups it is typically several times slower than
// map; for full iteration it is typically an order of magnitude faster.
// See binarysearchkv_bench_test.go for measured break-evens on string keys.
//
// The point-lookup methods (Has, Get, GetDefault, Delete) dispatch through
// the bsearch field, set at construction time. NewBinarySearchGrowingKVOrdered
// stores a closure that calls slices.BinarySearch — cmp.Compare is then
// inlined into the search loop and the per-comparison indirect-call cost
// disappears. NewBinarySearchGrowingKV (general path) stores a closure that
// calls slices.BinarySearchFunc with the supplied cmpKey, paying the
// indirect call per comparison. Construction-time dispatch keeps the public
// API identical across both flavours.
type BinarySearchGrowingKV[K any, V any] struct {
	cmpKey  func(a K, b K) int
	bsearch func(keys []K, target K) (int, bool)
	keys    []K
	vals    []V
	flushed bool
}

// IterateKeys yields the keys in cmpKey-ascending order. The container's
// deferred UpsertBatch state is flushed when ranging begins, not when
// IterateKeys is called, so a Seq obtained earlier always iterates the
// current (sorted, compacted) view. Mutating the container while a range
// is in progress remains undefined behaviour.
func (inst *BinarySearchGrowingKV[K, V]) IterateKeys() iter.Seq[K] {
	return func(yield func(K) bool) {
		if inst == nil {
			return
		}
		inst.ensureSorted()
		for _, k := range inst.keys {
			if !yield(k) {
				return
			}
		}
	}
}

// IterateValues yields the values in cmpKey-ascending key order. Flush
// semantics as in [BinarySearchGrowingKV.IterateKeys].
func (inst *BinarySearchGrowingKV[K, V]) IterateValues() iter.Seq[V] {
	return func(yield func(V) bool) {
		if inst == nil {
			return
		}
		inst.ensureSorted()
		for _, v := range inst.vals {
			if !yield(v) {
				return
			}
		}
	}
}

// IteratePairs yields (key, value) pairs in cmpKey-ascending order.
// Flush semantics as in [BinarySearchGrowingKV.IterateKeys].
func (inst *BinarySearchGrowingKV[K, V]) IteratePairs() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		if inst == nil {
			return
		}
		inst.ensureSorted()
		vals := inst.vals
		for i, k := range inst.keys {
			if !yield(k, vals[i]) {
				return
			}
		}
	}
}

// IterateFrom yields (key, value) pairs in cmpKey-ascending order,
// starting at the first key not less than lo (under cmpKey). Flush
// semantics as in [BinarySearchGrowingKV.IterateKeys]: the deferred
// state is flushed and the start position located when ranging begins.
func (inst *BinarySearchGrowingKV[K, V]) IterateFrom(lo K) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		if inst == nil {
			return
		}
		inst.ensureSorted()
		idx, _ := inst.bsearch(inst.keys, lo)
		keys := inst.keys
		vals := inst.vals
		for i := idx; i < len(keys); i++ {
			if !yield(keys[i], vals[i]) {
				return
			}
		}
	}
}

// IterateRange yields (key, value) pairs in cmpKey-ascending order over
// the half-open interval [lo, hi): keys k with cmpKey(k, lo) >= 0 and
// cmpKey(k, hi) < 0. When cmpKey(lo, hi) >= 0 the range is empty. Flush
// semantics as in [BinarySearchGrowingKV.IterateKeys].
func (inst *BinarySearchGrowingKV[K, V]) IterateRange(lo K, hi K) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		if inst == nil {
			return
		}
		if inst.cmpKey(lo, hi) >= 0 {
			return
		}
		inst.ensureSorted()
		idx, _ := inst.bsearch(inst.keys, lo)
		keys := inst.keys
		vals := inst.vals
		c := inst.cmpKey
		for i := idx; i < len(keys); i++ {
			k := keys[i]
			if c(k, hi) >= 0 {
				return
			}
			if !yield(k, vals[i]) {
				return
			}
		}
	}
}

// IsEmpty reports whether the container holds zero entries. It does not
// flush deferred UpsertBatch state because compaction can only remove
// duplicates, never reduce a non-empty slice to empty: if len(inst.keys)
// is zero the container is genuinely empty; if non-zero, at least one
// entry survives any pending compaction.
func (inst *BinarySearchGrowingKV[K, V]) IsEmpty() bool {
	return inst == nil || len(inst.keys) == 0
}

// Len returns the number of unique entries. It forces ensureSorted so
// that pending UpsertBatch state is flushed and the count reflects the
// post-compaction unique-entry count, not the raw appended-item count.
// Without this flush, an UpsertBatch sequence with duplicate keys would
// over-report by the number of shadowed duplicates.
func (inst *BinarySearchGrowingKV[K, V]) Len() int {
	if inst == nil {
		return 0
	}
	inst.ensureSorted()
	return len(inst.keys)
}

// bskvSortInterface is the sort.Interface adapter used by ensureSorted.
// It is intentionally a private type so the public BinarySearchGrowingKV
// API does not expose Len / Less / Swap. The Len method here reads the
// raw slice length without flushing — the public Len does flush, and
// implementing it via this adapter would induce infinite recursion
// through sort.Stable → Len → ensureSorted → sort.Stable.
type bskvSortInterface[K, V any] struct {
	inst *BinarySearchGrowingKV[K, V]
}

func (s bskvSortInterface[K, V]) Len() int { return len(s.inst.keys) }
func (s bskvSortInterface[K, V]) Less(i, j int) bool {
	keys := s.inst.keys
	return s.inst.cmpKey(keys[i], keys[j]) < 0
}
func (s bskvSortInterface[K, V]) Swap(i, j int) {
	keys := s.inst.keys
	vals := s.inst.vals
	keys[j], keys[i] = keys[i], keys[j]
	vals[j], vals[i] = vals[i], vals[j]
}

// NewBinarySearchGrowingKV constructs a container with a caller-supplied
// comparator. Lookup methods (Has, Get, GetDefault, Delete) go through a
// closure that calls slices.BinarySearchFunc with the supplied cmpKey —
// the per-comparison cost is one indirect call. Prefer
// NewBinarySearchGrowingKVOrdered when K satisfies cmp.Ordered, which
// inlines the comparator and is measurably faster on the lookup hot path.
func NewBinarySearchGrowingKV[K any, V any](estSize int, cmpKey func(a K, b K) int) (inst *BinarySearchGrowingKV[K, V]) {
	inst = &BinarySearchGrowingKV[K, V]{
		keys:   make([]K, 0, estSize),
		vals:   make([]V, 0, estSize),
		cmpKey: cmpKey,
		bsearch: func(keys []K, target K) (int, bool) {
			return slices.BinarySearchFunc(keys, target, cmpKey)
		},
		flushed: true,
	}
	return
}

// NewBinarySearchGrowingKVOrdered constructs a container for keys
// satisfying cmp.Ordered. Lookup methods (Has, Get, GetDefault, Delete)
// dispatch through a closure that calls slices.BinarySearch — cmp.Compare
// is then inlined into the search loop, saving one indirect call per
// comparison. Typically 1.4×–3.5× faster than NewBinarySearchGrowingKV
// for Get on string keys; see binarysearchkv_bench_test.go for measured
// numbers across N.
func NewBinarySearchGrowingKVOrdered[K cmp.Ordered, V any](estSize int) (inst *BinarySearchGrowingKV[K, V]) {
	inst = &BinarySearchGrowingKV[K, V]{
		keys:   make([]K, 0, estSize),
		vals:   make([]V, 0, estSize),
		cmpKey: cmp.Compare[K],
		bsearch: func(keys []K, target K) (int, bool) {
			return slices.BinarySearch(keys, target)
		},
		flushed: true,
	}
	return
}

func (inst *BinarySearchGrowingKV[K, V]) ensureSorted() {
	if !inst.flushed {
		sort.Stable(bskvSortInterface[K, V]{inst: inst})
		inst.compactNewestWins()
		inst.flushed = true
	}
}

// compactNewestWins collapses runs of equal-cmpKey entries in a sorted
// keys/vals pair. Within each run the newest value survives (sort.Stable
// preserves insertion order among equal keys, so the run's last element
// is the most recent UpsertBatch call), while the run's first key
// spelling is retained — matching UpsertSingle and MergeValue, which
// replace the value in place and never touch the resident key. The
// distinction is observable only under comparators that treat
// distinguishable keys as equal (case-insensitive, locale, …). The
// trailing tail is cleared so pointer-valued K/V slots don't keep their
// referents reachable past the entry's lifetime.
func (inst *BinarySearchGrowingKV[K, V]) compactNewestWins() {
	keys := inst.keys
	vals := inst.vals
	if len(keys) < 2 {
		return
	}
	c := inst.cmpKey
	w := 0
	for r := 1; r < len(keys); r++ {
		if c(keys[r], keys[w]) == 0 {
			vals[w] = vals[r]
			continue
		}
		w++
		if w != r {
			keys[w] = keys[r]
			vals[w] = vals[r]
		}
	}
	w++
	clear(keys[w:])
	clear(vals[w:])
	inst.keys = keys[:w]
	inst.vals = vals[:w]
}

func (inst *BinarySearchGrowingKV[K, V]) Has(key K) (has bool) {
	if inst == nil {
		return
	}
	inst.ensureSorted()
	_, has = inst.bsearch(inst.keys, key)
	return
}

func (inst *BinarySearchGrowingKV[K, V]) Get(key K) (val V, has bool) {
	if inst == nil {
		return
	}
	inst.ensureSorted()
	var idx int
	idx, has = inst.bsearch(inst.keys, key)
	if has {
		val = inst.vals[idx]
	}
	return
}

func (inst *BinarySearchGrowingKV[K, V]) GetDefault(key K, defaultV V) (val V) {
	if inst == nil {
		return defaultV
	}
	inst.ensureSorted()
	idx, has := inst.bsearch(inst.keys, key)
	if has {
		val = inst.vals[idx]
	} else {
		val = defaultV
	}
	return
}

// MergeValue combines an incoming value with the resident one: when key
// is present, the stored value becomes merge(old, val) — old is the
// resident value, val the incoming one — and the resident key spelling
// is kept; when absent, (key, val) is inserted without calling merge.
// Returns whether the key was already present. Any deferred UpsertBatch
// state is flushed first, so merge sees the compacted (newest) resident
// value. O(log N) lookup + O(N) shift on insert.
func (inst *BinarySearchGrowingKV[K, V]) MergeValue(key K, val V, merge func(old V, new V) V) (existed bool) {
	inst.ensureSorted()
	_, existed, inst.keys, inst.vals = co.MergeSliceSorted(inst.keys, inst.vals, key, val, inst.cmpKey, merge)
	return
}

// UpsertSingle inserts or replaces the entry for key, keeping the
// container sorted and compacted on the write path. Returns true if the
// key was already present (in which case the value is replaced in place
// with no shift), false if a fresh slot was opened. Cost: O(log N)
// binary search + O(N) shift on insert; O(log N) on in-place replace.
//
// See UpsertBatch for the cost-model comparison and guidance on which
// write path to use for which workload.
func (inst *BinarySearchGrowingKV[K, V]) UpsertSingle(key K, val V) (existed bool) {
	inst.ensureSorted()
	_, existed, inst.keys, inst.vals = co.InsertSliceSortedFunc(inst.keys, inst.vals, key, val, inst.cmpKey)
	return
}

// Delete removes the entry for key. Returns true when an entry was
// present (and removed), false when key was not in the container.
// O(log N) lookup + O(N) shift; sorted/compacted invariants are
// preserved. slices.Delete zeros the trailing slot before truncating
// so pointer values don't leak past their entry's lifetime.
func (inst *BinarySearchGrowingKV[K, V]) Delete(key K) (existed bool) {
	inst.ensureSorted()
	idx, existed := inst.bsearch(inst.keys, key)
	if !existed {
		return
	}
	inst.keys = slices.Delete(inst.keys, idx, idx+1)
	inst.vals = slices.Delete(inst.vals, idx, idx+1)
	return
}
func (inst *BinarySearchGrowingKV[K, V]) Grow(n int) {
	inst.keys = slices.Grow(inst.keys, n)
	inst.vals = slices.Grow(inst.vals, n)
}

// UpsertBatch stages a (key, value) pair on a deferred append buffer. The
// container is *not* sorted or deduplicated at this point — both are
// postponed until the next read (Has, Get, GetDefault, Len, IterateKeys,
// IterateValues, IteratePairs, MergeValue, UpsertSingle, Delete), which
// triggers ensureSorted transparently.
//
// # Invariants
//
//   - Per call: appends one entry to each backing slice and flips the
//     sorted / compacted flags to false. No comparison, no shift, no
//     binary search.
//   - On flush: sort.Stable orders entries by cmpKey-ascending, then
//     compactNewestWins collapses each equal-key run to a single
//     surviving entry whose value is the most recent UpsertBatch call
//     and whose key is the run's first-inserted spelling. "Newest value
//     wins" relies on sort.Stable preserving insertion order among equal
//     keys; keeping the first key spelling matches UpsertSingle's
//     replace-in-place behaviour.
//   - After flush, the container is in the same observable state as if
//     the equivalent UpsertSingle sequence had been issued: same final
//     entries, same iteration order, same Get results.
//
// # Cost model
//
//   - Per call: O(1) plus the occasional growslice when the append
//     exceeds capacity. No comparison, no allocation in the steady state.
//   - First read after N batched calls: O(N log N) sort.Stable + O(N)
//     compaction pass, where N is the *total* number of UpsertBatch
//     calls since the last flush, not the final unique-entry count.
//   - Subsequent reads are free until the next write.
//
// # When UpsertBatch wins
//
//   - Bulk load into a large container (typically N ≳ 3000 unique keys
//     on string-keyed workloads) with reads happening once at the end.
//     The deferred sort amortises the O(N²) cumulative shift cost that
//     a UpsertSingle loop would pay.
//   - Adversarial insert order — e.g. reverse-sorted or repeatedly at
//     position 0 — where every UpsertSingle would pay an O(N) shift.
//     UpsertBatch's append is O(1) regardless of insertion position.
//   - Per-call latency smoothing in hot writer loops, where the worst-
//     case O(N) shift of UpsertSingle is unacceptable jitter and the
//     reader can tolerate a deferred sort.
//
// # When UpsertBatch loses (counter to the name)
//
//   - Heavy-duplicate workloads (the same key reinserted many times).
//     UpsertSingle replaces in place after the first occurrence and so
//     never grows past the unique count; UpsertBatch sorts and discards
//     duplicates only at flush time. Measured 2–3× slower and up to 50×
//     more memory on duplicate-heavy batches in this package's bench.
//   - Mid-size N (~10 to ~2000 unique keys with random insert order).
//     The per-call shift cost of UpsertSingle is small enough that the
//     deferred sort+compact has worse constants in absolute terms.
//   - Workloads that interleave reads with writes. Every read calls
//     ensureSorted, so a Has-gated UpsertBatch loop pays N sort costs
//     instead of one. See the antipatterns section below.
//
// # Antipatterns
//
//   - Has/Get/IteratePairs/Len inside an UpsertBatch loop. Each read
//     forces a sort+compact, defeating the deferred-sort optimisation
//     and producing the worst-of-both-worlds cost profile. If duplicate
//     suppression is needed during build, use UpsertSingle (which
//     idempotently replaces) or maintain an external seen-set.
//
//   - Free mixing of UpsertSingle and UpsertBatch. Each transition pays
//     the flush cost. Choose one strategy per build phase.
//
//   - Calling UpsertBatch during iteration of the same container. The
//     iterator reads the slice headers when ranging begins; the append
//     may grow-and-relocate the underlying array mid-iteration, leaving
//     the loop walking stale storage. (Obtaining an iterator, mutating,
//     and only then ranging is safe — the deferred state is flushed when
//     ranging begins, not when the Iterate method is called.)
//
// # Sizing
//
// The estSize hint passed to NewBinarySearchGrowingKV / Ordered should be
// the expected *total* number of UpsertBatch calls, not the final unique
// count. Under-sizing causes growslice to reallocate the deferred buffer
// multiple times — measured at this package's bench as roughly 4× memory
// blowup and a 10–15% slowdown at N=4096 when sized for the unique count
// vs the total. After flush, the compacted slices may shrink below the
// high-water mark, but the hint controls the peak working set.
func (inst *BinarySearchGrowingKV[K, V]) UpsertBatch(key K, val V) {
	inst.keys = append(inst.keys, key)
	inst.vals = append(inst.vals, val)
	inst.flushed = false
}

func (inst *BinarySearchGrowingKV[K, V]) Reset() {
	inst.flushed = true
	clear(inst.keys)
	clear(inst.vals)
	inst.keys = inst.keys[:0]
	inst.vals = inst.vals[:0]
}

// IterateMergedBinarySearchGrowingKVKeys yields the union of both
// containers' keys in ascending order, each key once (a's spelling wins
// on ties). Both containers must be non-nil and must sort equivalently
// under a's comparator — the merge walks both key slices with a.cmpKey,
// so containers built with incompatible comparators produce meaningless
// output. Flush semantics as in [BinarySearchGrowingKV.IterateKeys].
func IterateMergedBinarySearchGrowingKVKeys[K any, V any, W any](a *BinarySearchGrowingKV[K, V], b *BinarySearchGrowingKV[K, W]) iter.Seq[K] {
	return func(yield func(K) bool) {
		a.ensureSorted()
		b.ensureSorted()
		IterateSortedUniqueFuncUnique(a.keys, b.keys, a.cmpKey)(yield)
	}
}

var _ sort.Interface = bskvSortInterface[any, any]{}

// IterateSortedUniqueOrderedUnique is [IterateSortedUniqueFuncUnique]
// with the natural cmp.Compare ordering.
func IterateSortedUniqueOrderedUnique[T cmp.Ordered](s1 []T, s2 []T) iter.Seq[T] {
	return IterateSortedUniqueFuncUnique(s1, s2, cmp.Compare)
}

// IterateSortedUniqueFuncUnique merges two slices into one ascending
// sequence with cross-slice duplicates collapsed (s1's element wins on
// ties). Precondition — the "SortedUnique" in the name: each input must
// already be sorted under compare and free of internal duplicates;
// duplicates within one slice pass through undeduplicated.
func IterateSortedUniqueFuncUnique[T any](s1 []T, s2 []T, compare func(a, b T) int) iter.Seq[T] {
	return func(yield func(T) bool) {
		i := 0
		j := 0
		for i < len(s1) && j < len(s2) {
			c := compare(s1[i], s2[j])
			if c < 0 {
				if !yield(s1[i]) {
					return
				}
				i++
			} else if c == 0 {
				j++
			} else {
				if !yield(s2[j]) {
					return
				}
				j++
			}
		}

		for i < len(s1) {
			if !yield(s1[i]) {
				return
			}
			i++
		}

		for j < len(s2) {
			if !yield(s2[j]) {
				return
			}
			j++
		}
	}
}
