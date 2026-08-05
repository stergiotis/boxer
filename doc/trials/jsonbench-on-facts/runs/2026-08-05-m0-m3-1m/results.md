---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Arms A, B and C at the 1M tier — the data-centricity tax

Environment in [`environment.md`](./environment.md); arm A's own detail in
[`arm-a-summary.md`](./arm-a-summary.md); raw evidence under `arm-a/`,
`arm-b/`, `arm-c/`.

All three arms produce **byte-identical query results** (`diff` of the
normalised `query-results.txt`, all 5 queries, all 3 arms). The facts arms are
measured against a correct translation, not an approximation.

| Arm | Table | Key | Index |
| --- | --- | --- | --- |
| A | ClickHouse `JSON` type, upstream DDL | `kind, operation, collection, did, ts` | clustered |
| B | boxer.facts DDL, leeway JSON shred | `ts` (the live store's own default) | none |
| C | arm B cloned + 3 bloom filters | `ts` | non-clustered |
| D | arm B cloned + 5 `MATERIALIZED` backbone columns | `ts` | none |

Arm D is not in the protocol's §4 table. It was added after
[`prior-art.md`](./prior-art.md) showed the pattern, and it isolates the read
path: identical data, identical key, identical results — only the path
resolution moves from per-row-per-query to once per part at merge time.

## Size

| Metric | A | B | C | D | B/A | D/A |
| --- | --- | --- | --- | --- | --- | --- |
| `total_size` | 102,099,450 | 147,254,702 | 147,223,468 | 176,344,857 | **1.44×** | **1.73×** |
| `data_size` | 102,074,732 | 147,066,669 | 146,894,088 | 176,046,054 | 1.44× | 1.72× |
| `index_size` | 24,579 | 156,607 | 278,876 | 285,749 | 6.4× | 11.6× |
| uncompressed | 674,515,642 | 2,484,560,492 | 2,484,574,227 | 2,535,586,930 | **3.68×** | 3.76× |
| parts / marks | 1 / 124 | 3 / 203 | 5 / 250 | 5 / 248 | — | — |

Arm D's five materialized columns add 27.8 MiB — **+19.8 % over arm B** — of
which `did` alone is 22.3 MiB (a high-cardinality string, duplicated out of
the section). `commit_collection` is 535 KiB, `time_us` 4.8 MiB,
`commit_operation` 166 KiB, `kind` 30 KiB.

Storage is the mild half of the tax: **1.44× on disk**.

> **Superseded attribution.** This section originally blamed the 3.68×
> uncompressed inflation on values carrying their JSON path in the parameter
> column. A follow-up decomposition ([`diagnostics.md`](./diagnostics.md))
> measured it: the path columns are 5.9 % of the compressed table and the
> membership machinery as a whole is 8 %. The uncompressed bulk is the support
> columns (`*card`, `len`), which compress 100–220× and cost almost nothing on
> disk. 81.5 % of arm B is the shredded values themselves.

## Latency

Seconds, `hot = min(try2, try3)` per the upstream reduction. **Cold is absent
on this machine** (no passwordless sudo for the page-cache drop); see
`environment.md` § Deviations.

| Q | A | B | C | D | B/A | D/A |
| --- | --- | --- | --- | --- | --- | --- |
| Q1 | 0.005 | 0.069 | 0.059 | 0.006 | 13.8× | **1.2×** |
| Q2 | 0.027 | 0.412 | 0.393 | 0.043 | **15.3×** | 1.6× |
| Q3 | 0.010 | 0.092 | 0.089 | 0.013 | 9.2× | 1.3× |
| Q4 | 0.014 | 0.171 | 0.131 | 0.025 | 12.2× | 1.8× |
| Q5 | 0.015 | 0.170 | 0.144 | 0.026 | 11.3× | 1.7× |

## Memory

Peak query memory, hot run. This is the harshest column.

| Q | A | B | D | B/A | D/A |
| --- | --- | --- | --- | --- | --- |
| Q1 | 2.5 MB | 187 MB | 5.0 MB | **76×** | 2.0× |
| Q2 | 29 MB | 715 MB | 56 MB | 25× | 1.9× |
| Q3 | 40 MB | 223 MB | 12 MB | 5.6× | **0.3×** |
| Q4 | 13 MB | 267 MB | 31 MB | 20× | 2.3× |
| Q5 | 14 MB | 245 MB | 32 MB | 18× | 2.4× |

Arm D uses *less* memory than arm A on Q3 — a materialized `LowCardinality`
column plus a `DateTime64` is a leaner thing to aggregate than arm A's typed
JSON subcolumns.

Every facts query materialises whole array columns per row and rebuilds a
per-attribute path vector before it can evaluate a single predicate. That
reconstruction — not the storage layout — is what the memory column prices,
and [`diagnostics.md`](./diagnostics.md) measures it: on Q1 the reconstruction
alone accounts for 7× the time and 8× the memory. Removing it puts arm B
within 2.4× of arm A instead of 13.8×.

## Ingest

| | A | B |
| --- | --- | --- |
| Wall clock | 3.74 s | 26.94 s |
| Peak client RSS | 1.04 GB | 376 MB |
| Throughput | ~267k docs/s | 37,420 docs/s |
| Attributes written | — | 12,045,072 |

Not a clean comparison: arm A's timed section excludes the gunzip (the loader
decompresses to a temp file first) while arm B decompresses inline, and arm A
pushes raw bytes to the server while arm B parses every document in-process
and builds Arrow batches. Read it as "same order of magnitude of minutes at
100M", not as a 7× ingest penalty.

## The §4 hypothesis: confirmed

> *on the unmodified facts table all five queries read every part — the primary
> index prunes nothing.*

`EXPLAIN indexes=1` on arm B, all five queries: `Condition: true`,
`Parts: 3/3`, `Granules: 200/200`. Confirmed exactly as stated
(`arm-b/explain.txt`).

## Arm C closes none of the gap

The skipping indices are built, materialised, and **used** — the planner names
`idxSymbolValues` on Q3, Q4 and Q5. They prune essentially nothing: 244 of 245
granules survive on Q4 and Q5, 245 of 245 on Q3.

An A/B on the same table with `use_skip_indexes` 1 vs 0
(`arm-c/skip-index-ab.tsv`) settles it — best of 3, seconds:

| Q | index on | index off |
| --- | --- | --- |
| Q1 | 0.060 | 0.056 |
| Q2 | 0.381 | 0.385 |
| Q3 | 0.082 | 0.079 |
| Q4 | 0.130 | 0.132 |
| Q5 | 0.132 | 0.140 |

Within noise in both directions. **Arm C's 20 % edge over arm B on Q4/Q5 is
not the index** — it is a part-count artefact of rebuilding the table by
`INSERT … SELECT` (5 parts vs 3, hence more inter-part parallelism on a
32-thread box). Running arm C's exact queries against arm B's *unindexed*
table reproduces arm B's timings (`arm-b-skipconjunct/timings.tsv`: Q4 0.165,
Q5 0.171), which isolates the effect to the table, not the query and not the
index.

Why the index cannot help: a bloom filter over a section's value column
answers *"does this granule contain this string anywhere"*. A granule is 8192
rows; `app.bsky.feed.post` appears in ~9 % of rows; so essentially every
granule contains one. Membership-style set semantics over a section defeat
granule-level pruning **unless the rows are clustered by the filtered value** —
which is arm D's territory (re-keying), not arm C's.

## Reading the tax

At the 1M tier, holding this corpus in the facts model instead of
ClickHouse's native JSON type costs:

- **as this trial first wrote the queries (arm B):** 1.44× storage,
  9.2–15.3× latency, 5.6–76× memory;
- **with the backbone materialized (arm D):** **1.73× storage, 1.2–1.8×
  latency, 0.3–2.4× memory** — the same data, the same key, the same results.

Data-skipping indices (arm C) recover nothing. Materializing the five backbone
paths recovers almost everything, and buys it with storage: +19.8 % over arm
B, four fifths of that being `did` alone.

So the headline is that **the model is not the expensive part; asking it for a
value by path in SQL is**, and that cost is payable once at merge time rather
than on every query.

One lever remains untested. Arm D is still `ORDER BY ts` and still prunes
nothing — 243/243 granules on every query. Re-keying to the extracted backbone
expressions, the way the reference entry and the sibling experiment both do
([`prior-art.md`](./prior-art.md)), is the remaining experiment, and it is the
one the §4 hypothesis points at.

Two caveats the numbers cannot carry themselves. First, 1M is a smoke tier —
every arm-A query finishes in tens of milliseconds, so scheduler noise is a
material fraction of arm A's denominator and the ratios are soft at the second
digit. Second, arm A is a *specialist*: its DDL names the five paths the
benchmark touches and sorts by exactly them. Arm B holds the whole document
shredded with no query-specific knowledge at all. The comparison is honest —
it is the one the trial set out to make — but it is a general representation
measured against a bespoke one, and the gap should be read that way.
