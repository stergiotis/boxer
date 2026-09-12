---
type: explanation
audience: package maintainer
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-12
---

# containers — search layout and the Eytzinger question

[`BinarySearchGrowingKV`](./binarysearchkv.go) answers point lookups by
binary search over a sorted `[]K`. A recurring question about structures
of this shape is whether the *Eytzinger* layout — the implicit-BST
permutation described in [Algorithmica's *Binary
Search*](https://algorithmica.org/en/eytzinger) — would make those
lookups cheaper. This file records what the layout would require, which
of those requirements the Go toolchain and this container's contract can
supply, and where the crossover sits.

## Two regimes, two different costs

Binary search over a sorted array is the same algorithm at every size,
but the cost is dominated by different hardware effects depending on
whether the key array fits in cache.

**Cache-resident.** Every probe hits L1/L2, so the loads are cheap and
the *branches* are what hurt: the comparison outcome at each level is
unpredictable for random keys, so the search retires roughly log₂N
branch mispredictions, each costing on the order of the pipeline depth.
The fix is to make the search branchless — compute the next index
arithmetically instead of jumping on the comparison. This does not
require any change of layout.

**Cache-resident no longer.** Once the array exceeds last-level cache,
each probe is a dependent load against memory and the search is a chain
of full-latency misses. Branchlessness now *hurts* slightly, because the
speculation it removes was partially hiding that latency. The fix is a
layout where the nodes visited early share cache lines, and where the
addresses of the next few candidates are computable before the current
comparison resolves — which is what Eytzinger provides.

The two fixes are independent, and only the second one costs a layout.

## What the Eytzinger layout is

Given the sorted keys, store them in the breadth-first order of the
balanced BST over them: slot 1 holds the median, slots 2 and 3 its
children, and the children of slot *k* live at *2k* and *2k+1*. The
search is then

```
k := 1
for k <= n {
    k = 2*k + b2i(e[k] < x)
}
k >>= trailingZeros(^k) + 1   // cancel the path's trailing 1-bits
```

which has no data-dependent branch: the comparison contributes an
integer 0 or 1 to an address computation. Two properties follow from the
numbering. The top levels of the tree occupy the first few slots of the
array, so they stay resident regardless of *n*. And because the
descendants of *k* are contiguous at depth *d* (slots `k·2^d` through
`k·2^d + 2^d - 1`), a whole cache line of future candidates can be
prefetched several levels ahead of the comparison that selects among
them.

The recovered *k* is an index into the Eytzinger array, not into the
sorted one. Anything that needs the sorted rank — a co-indexed value
slice, a range scan — needs a second array mapping one to the other.

## Which requirements Go supplies

The branchless step survives compilation. Go 1.27/amd64 emits the C
form for the loop body — a `SETCS` producing the 0/1 and a `LEAQ`
folding it into `2k`, with no jump on the comparison.

Bounds-check elimination fails on `e[k]`, and a hint of the form
`_ = e[len(e)-1]` does not rescue it, so a compare-and-branch per level
survives. That branch is perfectly predicted (it is never taken until
the loop exits), so it costs a fused uop rather than a misprediction —
much less than the folklore about Go bounds checks suggests.

Prefetching does not survive. As of Go 1.27 the toolchain exposes no
prefetch intrinsic and the runtime carries no Go-callable prefetch
function; `PREFETCH*` instructions appear only inside hand-written
runtime assembly. A user-supplied assembly stub cannot be inlined, so
the loop would pay a `CALL` per level to save a cache miss. The
measurements below are therefore Eytzinger *without* prefetch, and the
remaining headroom in the published figures is not reachable from pure
Go.

The comparator shape decides eligibility. A branchless step needs the
comparison to be an inline instruction. `BinarySearchGrowingKV` holds
its comparator as a `func(a, b K) int` field, so
[`NewBinarySearchGrowingKV`](./binarysearchkv.go) pays an indirect call
per comparison — which both prevents the arithmetic form and acts as a
speculation barrier. Only [`NewBinarySearchGrowingKVOrdered`](./binarysearchkv.go)
can inline, and there only for the fixed-width numeric shapes: a `string`
key comparison is a call into the runtime regardless of constructor.

## Measurements

2026-09-12, Go 1.27.0, Intel i7-10510U, single core pinned, performance
governor. Arms are standalone functions over `[]uint64` with identical
lower-bound semantics (cross-checked against `slices.BinarySearch` for
every probe at each size), queried with a fixed pseudorandom permutation
of present keys. `eytzinger+pos` adds the second array that recovers a
sorted rank, and is the variant this container would need. ns/op,
median; N ≤ 16384 over 15 repetitions, N ≥ 65536 over 7.

| N | keys | `slices.BinarySearch` | branchless, sorted layout | eytzinger | eytzinger+pos |
| --- | --- | --- | --- | --- | --- |
| 16 | 0.1 KiB | 17.1 | 5.9 | 6.0 | 6.8 |
| 128 | 1 KiB | 31.2 | 11.6 | 9.1 | 10.7 |
| 1 024 | 8 KiB | 45.4 | 18.9 | 12.5 | 14.9 |
| 16 384 | 128 KiB | 69.2 | 39.6 | 20.9 | 27.8 |
| 65 536 | 512 KiB | 99.8 | 73.5 | 48.2 | 74.5 |
| 262 144 | 2 MiB | 154.9 | 126.4 | 51.2 | 65.9 |
| 1 048 576 | 8 MiB | 212.9 | 195.7 | 71.8 | 116.9 |
| 4 194 304 | 32 MiB | 437.8 | 471.6 | 100.5 | 153.9 |
| 16 777 216 | 128 MiB | 592.2 | 691.7 | 137.9 | 217.8 |

Three readings, each needing both of the arms it compares:

- Against `slices.BinarySearch`, both alternatives are large wins at
  small N, and that win is branch elimination, not layout: at N=16
  branchless and eytzinger are within noise of each other (5.9 vs 6.0).
  `slices.BinarySearch` compiles to a data-dependent conditional branch
  per level with no conditional move, which is what both arms remove.
- Against *branchless on the existing layout* — the comparison that
  matters, since that alternative costs no layout change —
  `eytzinger+pos` is 0.87× at N=16, 1.08× at N=128 and 1.27× at N=1024.
  It stays below 2× through N≈1 048 576 and first clearly exceeds it at
  N≈4 194 304.
- The `pos` indirection is not free: recovering the sorted rank is an
  extra dependent load into a second array, and it adds 13–63% to the
  Eytzinger lookup time across the table.

Above last-level cache the two columns diverge as predicted: branchless
falls behind `slices.BinarySearch` (0.86× at N=16.8M) while Eytzinger
holds 4.3×.

## How the layout meets this container's contract

**Sorted iteration forbids storing keys in Eytzinger order.** The
container's iteration methods, and the merge helper over two
containers, all walk `keys` in physical order; the package doc offers
sorted iteration as the reason to prefer this type over a map. Eytzinger
order is breadth-first, so in-order iteration becomes a data-dependent
tree walk with a non-sequential stride. Eytzinger can therefore only
exist here as an *auxiliary* index beside the sorted arrays — which is
what forces the `pos` column measured above, and what
`IterateFrom`/`IterateRange` would have to pay to convert a lower bound
back into a scan start.

**Write paths get slower.** `UpsertSingle`, `Delete` and `MergeValue`
are a search plus a `memmove` of the tail. An auxiliary Eytzinger index
must be rebuilt after each of them, and a rebuild is a scattered
permutation rather than a linear copy. Measured the same day with a
tuned non-allocating iterative permutation: 695 ns at N=128, 5.3 µs at
N=1024, 86 µs at N=16384 — well above the cost of moving the same bytes
contiguously.

**Amortising the rebuild needs many lookups.** Comparing against
branchless-on-the-sorted-layout, the per-lookup saving pays back one
rebuild after roughly 800 lookups at N=128, 1 300 at N=1024 and 7 300 at
N=16384 — on the order of N itself, or several times it. At N=16 the
saving is negative and it never pays back. A workload that rebuilds and
then reads a comparable number of times does not clear the bar.

**Memory grows.** For `K = uint64` the index adds 8 bytes per entry for
the duplicated key plus 4 for the rank, and the fixed-trip-count form
needs padding to a perfect tree, which can nearly double both.

**Most keys are ineligible.** Per the comparator discussion above, only
fixed-width numeric `K` under the `Ordered` constructor can use the
branchless step at all. Keys that are `string` underneath, and every
instantiation that supplies its own comparator, are excluded.

## Where the time actually goes

Lookup cost is not the dominant term in the container's batched mode.
`ensureSorted` flushes deferred `UpsertBatch` state with `sort.Stable`,
whose O(n log²n) merge does substantially more work than an unstable
sort. Same machine and date, sorting pseudorandom `uint64`:

| N | `sort.Stable` | `slices.Sort` |
| --- | --- | --- |
| 128 | 10.7 µs | 2.2 µs |
| 1 024 | 208.8 µs | 64.3 µs |
| 16 384 | 4 342.8 µs | 831.7 µs |

At N=1024 one flush costs about as much as 4 600 lookups on the current
`slices.BinarySearch` path, or 14 000 on an `eytzinger+pos` path. Any
workload that alternates a batch of writes with a comparable number of
reads spends its time in the flush, and a lookup-side layout change
cannot reach it. Stability is load-bearing — it is what lets
`compactNewestWins` keep the newest value in each equal-key run — so
this is a description of where the cost sits, not of a free
substitution.

## Where this leaves the layout

Eytzinger pays for itself in this container when all of the following
hold at once: `K` is a fixed-width numeric type under the `Ordered`
constructor; the entry count is large enough that the key array misses
last-level cache; lookups outnumber mutations by roughly the entry count
or more; and sorted iteration and range scans are rare enough that the
rank-recovery indirection is not on the hot path. As of 2026-09-12 the
instantiations in this tree are sized between 1 and 1024 entries, which
is the far left of the table above, where the measured advantage over a
branchless search on the existing layout is between 0.86× and 1.26×.

The first regime — branch elimination on the sorted layout — is
available independently, applies to every `Ordered` instantiation
regardless of size, and requires no change to layout, memory, write
paths, or the iteration contract.

## References

- [Algorithmica — *Binary Search*](https://algorithmica.org/en/eytzinger)
  (Eytzinger layout, branchless search, prefetching).
- [`binarysearchkv.go`](./binarysearchkv.go) — the container, its
  comparator dispatch, and the `ensureSorted` flush.
- [`binarysearchkv_bench_test.go`](./binarysearchkv_bench_test.go) —
  the in-tree benchmarks for build, lookup and iteration paths.
