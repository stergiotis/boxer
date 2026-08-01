---
type: how-to
audience: engineer writing ad-hoc ClickHouse SQL against leeway tables
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to query leeway co-arrays and CSR values in ClickHouse

A leeway tagged-value section lands in ClickHouse as a bundle of parallel
arrays: per row, every tagged-value column of the section — and of its
co-sections — has the same length, and position `i` in each lane refers to the
same attribute instance. Non-scalar values add a second level: the value
column is one flat stream and a cardinality column says how many consecutive
elements belong to each attribute instance — a CSR layout (roles `card` /
`len`, optionally with materialized cumulative offsets `cusumcard` /
`cusumlen`).

This page collects a small kernel of array functions that is sufficient for
querying both layouts by hand, and the idioms built from it. Caveats up front:

- Every example and edge-case claim on this page was executed verbatim against
  ClickHouse 26.7; older servers may lack a few functions (`arrayFold`,
  `arrayZipUnaligned`). The
  [ClickHouse array-function reference](https://clickhouse.com/docs/en/sql-reference/functions/array-functions)
  is authoritative for signatures.
- These are hand-written-query idioms. The DQL read-back generator
  ([ADR-0066](../adr/0066-leeway-dql-clickhouse-readback-generator.md)) emits
  its own SQL and is not affected by this page.
- Examples quote lanes in the `"section:column"` style of the play surface;
  in the CSR section, `vals` stands for the flat value column and `card` for
  its cardinality column — substitute the physical names of your table.

## The kernel

| operation | function | note |
|---|---|---|
| element access | `a[i]` | 1-based; index `0` and out-of-range yield the element type's default; negative counts from the end |
| keyed position | `indexOf(a, k)` | first position, `0` when absent |
| membership | `has`, `hasAny`, `hasAll`, `hasSubstr` | constant-argument forms; the shapes skip indexes serve |
| first match | `arrayFirst`, `arrayFirstIndex`, `arrayLast…` | lambda may span several co-lanes |
| argwhere | `arrayFilter((i, …) -> p, arrayEnumerate(a), …)` | positions where a predicate holds |
| gather | `arrayMap(i -> lane[i], sel)` | project any lane through an index list |
| predicate | `arrayExists`, `arrayAll`, `arrayCount` | multi-lane lambdas |
| zip / unzip | `arrayZip`, tuple access `t.1` | glue lanes into `Array(Tuple)`; `arrayZipUnaligned` pads with NULL |
| argsort | `arraySort(i -> key[i], arrayEnumerate(key))` | permutation to gather every lane through |
| offsets ↔ lengths | `arrayCumSum`, `arrayDifference` | CSR bookkeeping |
| segment | `arraySlice(vals, start, len)` | with `arrayCumSum(card)`, the CSR cut |
| reduce | `arraySum/Min/Max/Avg`, `arrayReduce`, `arrayReduceInRanges`, `arrayFold` | `arrayReduce` takes any aggregate, including parametrized (`'topK(3)'`) and multi-argument (`'argMax'`) ones |
| explode / implode | `ARRAY JOIN` clause, `groupArray` | the clause zips several arrays positionally; `LEFT ARRAY JOIN` keeps rows with empty sections |
| cross-row co-arrays | `sumMap` / `minMap` / `maxMap`, `-Array` / `-ForEach` combinators | aggregate over lanes across rows without exploding |
| dictionary view | `mapFromArrays(keys, vals)` then `m[k]` | duplicate keys are kept, lookup returns the first; a missing key yields the default value |

Assembly and display round it out: `range`, `arrayEnumerate`,
`arrayWithConstant`, `arrayConcat`, `arrayResize`, `arrayCompact`,
`arrayStringConcat`.

## Co-array idioms

### Look up a sibling lane by key

```sql
"string:short"[indexOf("string:model", '/my/model')]
```

Absent keys index at `0`, which yields the type default (`''`) —
indistinguishable from a stored empty string. When that matters, disambiguate
through the index:

```sql
WITH indexOf("string:model", '/my/model') AS i
SELECT if(i > 0, "string:short"[i], NULL)
```

Beyond equality, move the predicate into `arrayFirst` (same not-found caveat;
pair with `arrayExists` or `arrayFirstIndex` to distinguish):

```sql
arrayFirst((s, m) -> m LIKE '%/my/%', "string:short", "string:model")
```

An equality-keyed pair of lanes can also be read as a dictionary:

```sql
mapFromArrays("string:model", "string:short")['/my/model']
```

### Select positions once, gather any number of lanes

`arrayFilter` returns the lane of its first lambda argument only. Filtering
two lanes consistently by repeating the predicate invites drift; select
positions once instead:

```sql
arrayFilter((i, st, v) -> st = 'myst' AND v LIKE '%/test',
            arrayEnumerate("string:semanticType"),
            "string:semanticType", "string:short") AS sel,
arrayMap(i -> "string:short"[i], sel) AS shorts,
arrayMap(i -> "string:model"[i], sel) AS models
```

Argwhere + gather is the general plan: every further lane — including lanes
of co-sections — projects through the same `sel`. For a one-off over two
lanes, the zip style reads well too:

```sql
arrayMap(t -> t.2,
         arrayFilter(t -> t.1 = 'myst',
                     arrayZip("string:semanticType", "string:short")))
```

### Existence tests, with a sargable guard

```sql
arrayExists((st, v) -> st = 'myst' AND v = 'abc',
            "string:semanticType", "string:short")
AND has("string:short", 'abc')
```

The `has` conjunct looks redundant but usually is not. ClickHouse rewrites
one special case by itself — a single-lane pure-equality lambda,
`arrayExists(v -> v = 'abc', lane)`, becomes `has(lane, 'abc')` in the query
tree (`optimize_rewrite_array_exists_to_has`, on by default) and prunes on
its own. Everything past that special case — multi-lane lambdas like the one
above, `LIKE`, arithmetic — is opaque to index analysis: on a
bloom-filter-indexed table the two-lane form scanned every granule without
the guard and 4/245 granules with it. Keep the lambda as the semantics and
the `has` as the pruner.

### Reductions across lanes

`arrayReduce` applies any aggregate function to arrays, which makes co-array
argmax a one-liner — "the short name at the newest timestamp":

```sql
arrayReduce('argMax', "string:short", "ts:modified")
```

`arraySum` and friends take multi-lane lambdas: `arraySum((v, w) -> v * w, a, b)`.

### Sort every lane by one lane

```sql
arraySort(i -> "ts:modified"[i], arrayEnumerate("ts:modified")) AS perm,
arrayMap(i -> "string:short"[i], perm) AS shorts_by_time
```

The direct form `arraySort((v, k) -> k, vals, keys)` sorts a single output
lane by another lane. Never sort (or dedup) one lane in place — the siblings
silently stop corresponding; go through the permutation.

## CSR idioms

The invariant, worth asserting when something looks off:

```sql
arraySum(card) = length(vals)
```

End offsets are `arrayCumSum(card)`; the start of segment `i` is
`hi[i] - card[i] + 1`. When the table materializes `cusumcard`, use it instead
of recomputing.

Materializing the segments gives an `Array(Array(T))` lane co-aligned with the
membership lanes — empty members come out as `[]`:

```sql
arrayMap((c, hi) -> arraySlice(vals, hi - c + 1, c),
         card, arrayCumSum(card)) AS seg
```

With `seg` in hand the whole co-array kernel lifts one level — "members whose
value list contains a model ending in /test":

```sql
arrayFilter((m, vs) -> arrayExists(v -> v LIKE '%/test', vs),
            "string:member", seg)
```

Per-segment aggregates work without materializing, via 1-based
`(start, length)` ranges:

```sql
WITH arrayCumSum(card) AS hi,
     arrayMap((h, c) -> (h - c + 1, c), hi, card) AS ranges
SELECT arrayReduceInRanges('sum', ranges, vals)
```

The k-th value of member `i` is `vals[hi[i] - card[i] + k]`, valid while
`k <= card[i]`.

One trap: `arraySplit` cuts *before* mask positions and can never produce an
empty segment, so any mask-based reconstruction mis-assigns values as soon as
one member has `card = 0`. The `arraySlice` recipe handles zeros; prefer it.
`arrayFlatten(seg)` is the inverse direction.

## Crossing rows

The `ARRAY JOIN` *clause* zips several arrays positionally (equal lengths
required) — exactly the co-array exploder. Join `arrayEnumerate(...)` as one
more array to keep the position, and use `LEFT ARRAY JOIN` to keep rows whose
section is empty:

```sql
SELECT id, m, s, i
FROM t
LEFT ARRAY JOIN
    "string:model" AS m,
    "string:short" AS s,
    arrayEnumerate("string:model") AS i
```

The `arrayJoin()` *function* expands one array; several calls with different
arguments do not stay aligned. Use the clause for co-arrays.

After exploding you are in plain SQL (`WHERE`, `GROUP BY`); reassemble with
`groupArray`, and when order matters collect `(i, x)` tuples and `arraySort`
by the tag — merge order across streams is not guaranteed.

Three aggregate families work on co-arrays across rows without exploding:

- `sumMap(keys, vals)` (and `minMap`, `maxMap`) merge key/value lanes across
  rows into one sorted key set with combined values.
- `-Array` combinators collapse arrays into a scalar aggregate:
  `sumArray(a)`, `uniqArray(a)`.
- `-ForEach` combinators aggregate position-wise and return an array:
  `sumForEach(a)` over rows `[1,2]` and `[3,4]` yields `[4,6]`.

## Wrapping the idioms in SQL UDFs

`CREATE FUNCTION` (SQL UDFs, not executable UDFs) fits this vocabulary
unusually well because a SQL UDF is a macro: it is inlined into the query
tree during analysis. Verified consequences:

```sql
CREATE OR REPLACE FUNCTION csrSegments AS (vals, card) ->
    arrayMap((c, hi) -> arraySlice(vals, hi - c + 1, c), card, arrayCumSum(card))
```

- **Zero overhead.** `EXPLAIN actions = 1` of a UDF call and of its
  handwritten expansion are byte-identical.
- **Index-transparent.** A UDF-wrapped `has` prunes granules exactly like a
  bare one and moves to the PREWHERE filter; bundling
  `arrayExists(f, lane) AND has(lane, x)` inside one UDF keeps the pruning.
- **Lambdas can be parameters.** A UDF may accept a lambda and forward it to
  a higher-order builtin —
  `CREATE FUNCTION csrExists AS (f, vals, card) -> arrayMap(vs -> arrayExists(f, vs), csrSegments(vals, card))`
  works — so the kernel lifts to the ragged case generically.
- **UDFs compose.** A UDF body may call other UDFs (non-recursively), and
  constant arguments stay constant through inlining: even a constructed
  aggregate name, `arrayReduce(concat('topK(', toString(k), ')'), a)`, folds
  and is accepted.
- **Duplication is free.** Textually repeated subexpressions (e.g. the
  double `indexOf` in a lookup-or-NULL body) become one node in the actions
  DAG.

Two caveats: the namespace is global and flat (prefix a function pack), and
`CREATE FUNCTION` is server state — a read-only or external target
(`url(...)`) will not have it. Since inlining is all a SQL UDF does, a
client-side expansion of the same bodies is an equivalent substitute there.

## Sharp edges

1. Arrays are 1-based; index `0` and out-of-range return the element type's
   default and negative indices count from the end. That is what makes the
   `indexOf` idiom degrade gracefully — and what makes "not found" ambiguous.
2. Every multi-lane lambda and the multi-array `ARRAY JOIN` clause require
   equal lengths and throw otherwise. A violated `card` invariant or a
   misaligned co-section surfaces as an "arrays … must have equal size" error,
   usually far from the cause.
3. Order-destroying functions (`arrayIntersect`, `arrayDistinct`,
   `groupUniqArray`) break positional alignment — apply them to one lane you
   will not index into again, or after zipping.
4. `arrayFold((acc, x) -> …, arr, init)` takes the accumulator *last*, and its
   type must match exactly — write `toUInt64(0)`, not `0`.
5. `mapFromArrays` keeps duplicate keys; `m[k]` returns the first match and a
   missing key returns the default value — not NULL, not an error.
6. Lambdas past the single-lane equality rewrite never use indexes; when a
   query should prune granules, add a constant-argument `has` / `hasAny`
   guard beside the lambda. Stream-level `has(vals, x)` stays a valid guard
   under CSR: it is a necessary condition for any member's list to contain
   `x`.
