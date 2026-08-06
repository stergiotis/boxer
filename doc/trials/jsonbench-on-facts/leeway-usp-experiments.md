---
type: explanation
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative. The measurements are single-run on one workstation and one
> corpus; see *Threats to validity*.

# Where the leeway shape beats a JSON column, and where it does not

A side-experiment of the [jsonbench-on-facts](./README.md) trial. The trial
proper asks what it costs to hold an external JSON corpus in a leeway table.
This document asks the complementary question — **which queries are easier and
cheaper on the leeway shape than on ClickHouse's `JSON` type**, and which are
not — and answers it with matched pairs measured on the same corpus, same
tier, same machine.

It exists because the trial's own five benchmark queries cannot answer it.
JSONBench is posed for a corpus whose schema you already know: every query
names its paths. That workload is the case a JSON column is built for, and the
trial measured it honestly — the leeway arm is *slower* there than a
schema-hinted JSON column. The interesting questions are the ones the benchmark
does not ask.

## 1 The thesis, in one line

**A JSON column is fast when the path is in the query. A leeway table is fast
when the path is in the data.**

Everything below is a consequence of that. It is not a claim that one
representation dominates; §5 records where the JSON column wins, on the same
data, in the same run.

## 2 The structural facts

Three properties, each verified against the live server (26.7.2.59) rather than
read off documentation.

### 2a A JSON column cannot address a path known only at runtime

ClickHouse can *enumerate* paths — `JSONAllPaths`, `JSONAllPathsWithTypes`,
and the aggregates `distinctJSONPaths` / `distinctJSONPathsAndTypes`. Schema
discovery is therefore expressible on a JSON column, and the naive claim that
it is not would be wrong.

What is not available is using an enumerated path to read the column:

```text
SELECT data[JSONAllPaths(data)[1]] FROM bluesky
  → Code 43: First argument for function 'arrayElement' must be array, got 'JSON'

SELECT getSubcolumn(data, JSONAllPaths(data)[1]) FROM bluesky
  → Code 44: The second argument of function getSubcolumn should be a
             constant string with the name of a subcolumn
```

The only escape is `toString(data)` followed by `JSONExtract*` — that is,
re-serialising each document back to JSON text and re-parsing it, per row,
giving up the columnar storage entirely. Every query whose path set is
data-dependent inherits that cost.

In a leeway table the path is an ordinary column value (`<section>:lmv`), so a
runtime path resolves with `indexOf` like any other array lookup, and a path
*predicate* is just a predicate:

```sql
value[indexOf(lmv, (SELECT … ))]          -- resolve a runtime-chosen path
arrayFilter(p -> startsWith(p, '/x/'), lmv)  -- range over paths by shape
```

### 2b A JSON column does not enumerate inside arrays

`JSONAllPaths` stops at an array; the array is a value, not a set of addresses:

```text
'commit.record.facets'  → Array(...)          one path
'commit.record.langs'   → Array(Nullable(String))
```

The leeway shred addresses each element — `/commit/record/facets/_/features/_/$type`
with the elided indices carried in `mvhp` — so element position is queryable
without naming the containing path. For a *known* path a JSON column indexes
arrays perfectly well (`data.commit.record.langs[1]` works and is cheap); the
gap is only over paths not known in advance.

This also means the two systems do not enumerate the same path set, so
path *counts* are not comparable between them. Runtimes are.

### 2c The leeway lanes are narrow, and the JSON column is not

A path query on a leeway table reads one dictionary-encoded
`Array(LowCardinality(String))` column. The equivalent on a JSON column has to
materialise the object. The memory column in §4 shows this directly: every
JSON-column path query costs ~4.2 GB regardless of what is asked, while the
leeway side varies with the lane it touches.

## 3 Setup

| | leeway | jsonv2 |
| --- | --- | --- |
| table | `jsonbench_j2_10m.json` | `jsonbench_a00_10m.bluesky` |
| schema | `mapping.LoadJsonMapping` | `data JSON CODEC(ZSTD(1))`, engine defaults |
| documents | 9,999,994 | 9,999,994 |
| sort key | `ORDER BY tuple()` | `ORDER BY tuple()` |
| on disk, as loaded | 1,540,236,424 B | 1,150,367,898 B |
| on disk, comparable (§3a) | 1,220,126,096 B | 1,150,367,898 B |

Same corpus (files 1–10), same tier, same machine, neither table indexed for
the workload. The JSON side is the trial's **A00** reference — plain `JSON`,
no typed path hints — because that is the only variant a store holding a
mixture of document shapes could actually have, and because its DDL is taken
verbatim from
[`runs/2026-08-06-m4-10m/arm-a00/ddl-as-applied.sql`](./runs/2026-08-06-m4-10m/arm-a00/ddl-as-applied.sql).
Loading it here reproduced that run's size to within 0.06 %.

### 3a The row-identity column, and why it is excluded from the size comparison

The canonical mapping declares one plain-value column, `id:blake3hash`, holding
a content hash of the document. **The A00 table has no row identity at all** —
it is a single `data JSON` column — so a size comparison that includes the hash
charges the leeway side for something the comparison does not require and no
query here reads.

It is not a rounding error. At 10M the column is **320,110,328 B, 20.3 % of
the leeway table**, and it compresses at 1.2× because a hash is incompressible
by construction, so it is nearly pure overhead in the compressed figure.

Two things compound it, and both are this trial's doing rather than the
mapping's:

- The hash is written at **32 bytes** here (`blake3.Sum256`). The facts arm
  wrote 16 (`nk[:16]`), and the canonical type is unbounded bytes, so the width
  is the writer's choice. Halving it would return ~10 % of the table.
- It carries `CODEC(ZSTD(3))`, which spends compression CPU on random bytes for
  a 1.2× return.

The trial already made this mistake once and caught it: the 1M diagnostics
record `id:naturalKey` as "a near-incompressible hash … 8.6 % of arm B's total"
with no arm A equivalent. It recurred here because the canonical mapping's
*only* plain value is that hash.

Both figures are reported below. The **comparable** row excludes the column;
the **as loaded** row does not.

Queries: [`queries-usp-leeway.sql`](./queries-usp-leeway.sql) and
[`queries-usp-jsonv2.sql`](./queries-usp-jsonv2.sql), matched statement for
statement. Harness: the trial's own `measure.sh`, TRIES=3, cache dropped before
each query's tries, cold = try 1, hot = min(try 2, try 3).

### Fairness controls

Two server defaults had to be turned off, and both were found by getting a
wrong answer first.

- **`use_query_condition_cache=0`.** ClickHouse remembers which granules
  matched a predicate. With it on, the leeway value-anywhere query reported
  **0.003 s / 170 KB** on its second run — the cache replaying "no granule
  matches", not work. The honest number is 100× that. The JSON side could not
  benefit, because its query never completed to populate a cache.
- **`min_execution_speed=0`.** With the default 250,000 rows/s minimum, the
  JSON-column value-anywhere query is **killed by the server** after
  `timeout_before_checking_execution_speed` (10 s):

  ```text
  Code 160: Query is executing too slow: 66158.668 rows/sec., minimum: 250000
  ```

  That is a result in itself — under stock settings the query does not return —
  but an abort has no runtime, so the guard was relaxed to obtain one.

## 4 Results

Hot seconds and peak memory, 10M tier, both sides through the same harness.
Evidence: [`runs/2026-08-06-jsonmap-100m/usp/`](./runs/2026-08-06-jsonmap-100m/usp/).

| | Query | leeway | jsonv2 | jsonv2 ÷ leeway |
| --- | --- | --- | --- | --- |
| U1 | path census + per-path doc counts | **0.400 s** / 397 MB | 4.459 s / 4,198 MB | **11.1×** |
| U2 | path × type census | **0.879 s** / 956 MB | 4.944 s / 4,208 MB | **5.6×** |
| U3 | value anywhere, exact | **0.261 s** / 146 MB | 176.550 s / 4,395 MB | **676×** |
| U4 | subtree prefix census | **0.233 s** / 344 MB | 4.730 s / 4,192 MB | **20.3×** |
| U5 | sum every integer, any path | **0.024 s** / 24 MB | *no expression exists* | — |
| U6 | leaf count per document † | **0.025 s** / 16 MB | 4.709 s / 4,182 MB | **188×** |
| U7 | presence of one **constant** path | 0.041 s / 13 MB | **0.026 s** / 27 MB | **0.63×** |
| U8 | numeric predicate over all int paths ‡ | **0.027 s** / 9 MB | 176.359 s / 4,405 MB | *not comparable* |
| U9 | array degree for every array path | **0.418 s** / 450 MB | *no expression exists* | — |

The leeway side is measured on the **fixed-codec** table (§4a): the mapping
formerly declared no encoding hints on its numeric value columns. The two
queries that read the `int64` lane, U5 and U8, are the only ones the fix made
*slower* — 0.018 s → 0.024 s and 0.018 s → 0.027 s, the cost of decoding
`DoubleDelta` — while using less memory (28 → 24 MB, 13 → 9 MB) and 2.37× less
disk. At sub-30 ms that is a trade worth taking, and it is recorded rather than
smoothed over.

† Semantics differ (§2b): `JSONAllPaths` does not descend into arrays, so an
array counts once there and once per element here. Both answer "how wide is a
document" in their own vocabulary; only the runtimes are comparable.

‡ **Not the same question, and the jsonv2 side is the easier one.** The leeway
form scans every integer-valued path; the jsonv2 form names `time_us`, because
asking it over unknown paths would require running U2 first and emitting a
per-path disjunction. The 176 s is the cost of the text fallback on a *named*
path, so it understates the gap rather than inflating it.

Three readings.

**The gap tracks the mechanism, not the query.** U1/U2/U4/U6 are all "range over
paths"; all four cost jsonv2 ~4.5–5 s and ~4.2 GB regardless of what is asked,
because each one materialises the object to enumerate it. The leeway side varies
between 16 MB and 992 MB with the lane it actually touches. The memory column is
the clearer signal: jsonv2's is flat at ~4.2 GB, leeway's spans 60×.

**U3 is the structural case, not a tuning case.** 651× is what §2a costs when
paid per row: there is no columnar form, so the document is re-serialised and
re-parsed. Under stock server settings it does not merely take 176 s — it is
killed at 10 s by `min_execution_speed` and returns nothing at all.

**U7 is the counter-example and it is real.** Ask for one path known at query
time and jsonv2 wins: a typed subcolumn read, 1.4× faster than an `indexOf`
over a path lane. It is also the only row where jsonv2 uses *less* time but
*more* memory (27 MB against 13 MB).

### 4a Where the leeway bytes actually go

Per-lane breakdown of the 100M table (15,314,038,009 B of column data), because
the recurring objection to a shredded representation is that the addressing
overhead eats the win:

| Lane group | Compressed | Ratio | Share |
| --- | --- | --- | --- |
| `string:value` — the payload | 10.32 GiB | 2.7× | 74.2 % |
| `id:blake3hash` — row identity (§3a) | 2.98 GiB | 1.2× | 21.4 % |
| `int64:value` (after the fix below) | 229 MiB | 7.1× | 1.6 % |
| `symbol:value` | 76.7 MiB | 15× | 0.5 % |
| **addressing: `lmv` + `mvhp` + `lmvcard`, all 7 sections** | **~337 MiB** | 16–283× | **2.4 %** |
| empty sections (`null` / `undefined` / `float64` offsets) | ~5 MiB | ~1470× | 0.03 % |

**The addressing machinery is 2.3 % of the table** — every path, every array
coordinate and every membership cardinality across 1,200,650,881 attributes.
The `mvhp` lanes compress 99–140× and `lmvcard` 216–283×, which is the
fixed-width params encoding and the `T64` support codec doing their job. On this
corpus the overhead objection does not survive measurement; what costs is the
payload, and the row-identity column this trial added to it.

### 4b The codec defect, and the fix

The first pass of this document found `int64:value` and `float64:value`
carrying **no codec at all** — inheriting server-default LZ4 — while the
*support* lane beside them in the same section got `T64, ZSTD(3)`. The cause is
not the generator: the ClickHouse codec builder handles `Delta` / `DoubleDelta`
/ `T64` / `FPC` / `Gorilla` correctly and gates each on the scalar type. It was
`mapping.LoadJsonMapping`, which declared no `AddColumnEncodingHints` for the
`bool`, `int64` and `float64` value columns where `string` and `symbol` do.

**An omitted hint is not a neutral default** — it emits no `CODEC` clause at
all — and nothing in the mapping source makes that visible.

Candidates measured on the real 10M lane (1.59 GiB of `int64`), each a
one-column table loaded from the same source column:

| Codec | Size | Ratio |
| --- | --- | --- |
| none (LZ4 default) — *was* | 56.67 MiB | 2.89× |
| `T64, LZ4` | 38.19 MiB | 4.29× |
| `T64, ZSTD(3)` | 29.49 MiB | 5.56× |
| `ZSTD(3)` | 29.10 MiB | 5.63× |
| `Delta, ZSTD(3)` | 25.00 MiB | 6.55× |
| **`DoubleDelta, ZSTD(3)` — *is*** | **23.98 MiB** | **6.83×** |

**The measurement contradicted the obvious argument, which is why it was worth
making.** A leeway value lane is flattened across attributes, so consecutive
elements are different fields — a microsecond timestamp beside an image
height — which suggests delta encoding should be useless and a bit-plane
transform like `T64` should win. In fact `T64` is *worse* than plain `ZSTD(3)`
here and `DoubleDelta` is best, 2.36× better than the default. On the rebuilt
100M table the lane went 588,211,626 → 240,175,153 B (2.89× → 7.12×).

Cost: the two queries that read the lane, U5 and U8, got slightly slower
(§4), and the whole 10M table went 1,575,148,554 → 1,540,236,424 B.

Two things this does **not** establish:

- **The float hint is unvalidated.** `FPC(12)`, `Gorilla` and plain `ZSTD(3)`
  produced byte-identical sizes on this corpus, which only says the transform
  had nothing to work on — Bluesky is essentially float-free (53.66 KiB of lane
  against a 348 KiB empty-array baseline). `LightGeneralCompression` was chosen
  as the conservative fix; a float-heavy corpus should re-measure.
- **One corpus is not a proof for `int64` either.** These integers are dominated
  by a per-document timestamp. A corpus without that shape could invert the
  ranking again.

`id:blake3hash` was left alone: it carries `ZSTD(3)` for a 1.2× return, but its
width and codec are the trial ingester's choice rather than the mapping's, and
changing the mapping's declared identity is a design question, not a defect
(§3a).

## 5 Where the JSON column wins

Three ways, and they are not marginal.

**On the benchmark's own queries** — the case where every path is named in the
query — the JSON column is faster on all five, even stripped of its hints and
its index. Hot seconds at the same 10M tier
([the 10M run](./runs/2026-08-06-m4-10m/results.md) for A00; this arm's own
measurement for leeway):

| | Q1 | Q2 | Q3 | Q4 | Q5 |
| --- | --- | --- | --- | --- | --- |
| A00 (plain `JSON`) | **0.049** | **0.385** | **0.159** | **0.197** | **0.192** |
| leeway | 0.058 | 0.639 | 0.182 | 0.462 | 0.486 |
| leeway ÷ A00 | 1.18× | 1.66× | 1.14× | 2.35× | 2.53× |

Add the hints and the clustered index back — arm A, the benchmark's actual
entry — and the gap widens to 5.0–15.9×. A typed subcolumn read beats an
`indexOf` over a path lane, and it should: the JSON column has already done at
write time the work leeway defers to read time.

**On storage**, at this tier, the JSON column is smaller — but by much less
than the raw totals suggest, and the correction is §3a's:

| | bytes | leeway ÷ jsonv2 |
| --- | --- | --- |
| leeway, pre-fix, as loaded | 1,575,148,554 | 1.369× |
| leeway, fixed codecs, as loaded | 1,540,236,424 | 1.339× |
| leeway, fixed codecs, minus `id:blake3hash` | 1,220,126,096 | **1.061×** |
| jsonv2 (A00) | 1,150,367,898 | — |

So the honest figure is **6 % larger, not 37 %** — but 6 % larger is still
larger, and it gets worse if the backbone is materialized for the known-path
workload: **1.304× minus identity, 1.582× as loaded** (measured, +18.1 % for
the five columns). **On storage the JSON column wins, on every variant that a
heterogeneous store could actually use.**

Of what remains, the
addressing lanes are 2.3 % of the table (§4a); the rest is the payload itself,
where leeway's `string:value` and the JSON column's string subcolumns are
storing the same bytes, both near the entropy floor — raising the string lane
from `ZSTD(3)` to `ZSTD(12)` buys 4 % (2.68× → 2.79×) for a large CPU cost,
which is why it was left alone.

**On ingest**, at this tier: 77 s against 186 s. Not a like-for-like format
comparison — the JSON side is ClickHouse's own multi-threaded server-side
parser, the leeway side a single Go process doing JSON decode plus Arrow
building, and the leeway ingest parallelises across files (the 100M run reached
297k docs/s on 8 shards). But as measured, per process, the JSON column loads
**2.4× faster**.

So the honest summary is a trade, not a win: the leeway shape pays a constant
factor on known-path analytics and in storage, and buys the ability to ask
questions whose paths are not known when the query is written.

## 6 Threats to validity

- **One corpus.** Bluesky Jetstream is shallow-to-moderately-nested, heavily
  repetitive, and string-dominated. A corpus with deep unique nesting, or one
  where most attributes are numeric, would move these numbers. Nothing here
  generalises to "JSON" as a category.
- **One tier, one machine, one run per configuration.** TRIES=3 controls
  short-term noise, not run-to-run drift on a shared workstation.
- **The leeway table's string/symbol split was sampled from file 1** and pinned.
  A different split changes the leeway side's column widths and therefore its
  timings.
- **The JSON side is A00, not A.** Against the benchmark's own hinted, clustered
  entry the leeway arm loses on the benchmark's own queries — see
  [`runs/2026-08-06-m4-10m/results.md`](./runs/2026-08-06-m4-10m/results.md).
  A00 is the right control for *these* questions and the wrong one for those.
- **Semantics differ where noted.** U6's leaf counts and any path-count
  comparison are not like-for-like (§2b); only the runtimes are.
- **This is not a benchmark entry.** It is a pair of query sets chosen to probe
  a hypothesis, and the hypothesis was formed before the measurements. The
  queries where the JSON column wins were added for the same reason, and are
  reported in §5 rather than dropped.
