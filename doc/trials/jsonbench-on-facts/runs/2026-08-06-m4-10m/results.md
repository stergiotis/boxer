---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Arms A–D at the 10M tier — with cold runs, and with the query vocabulary

The development tier, run after two corrections that the 1M run's review
forced: the queries now use the leeway query vocabulary
([ADR-0162](../../../../adr/0162-leeway-co-ragged-function-pack.md) `chpack` plus
the `LEEWAY_VALUE_BY_TAG_EQUAL` UDFs) instead of open-coded lane arithmetic,
and the page-cache drop is available, so **cold is measured rather than
absent**.

All four arms hold **9,999,994 documents** — identical corpora — and return
**byte-identical results** for all five queries.

| Arm | Table | Key | Read path |
| --- | --- | --- | --- |
| A | ClickHouse `JSON`, pinned upstream DDL | `kind, operation, collection, did, ts` | typed subcolumns |
| A0 | the same DDL, **`ORDER BY tuple()`** | none | typed subcolumns |
| A00 | plain **`JSON`**, no hints, `ORDER BY tuple()` | none | engine-discovered subcolumns |
| B | boxer.facts DDL, leeway JSON shred | `ts` | value-by-tag over lanes |
| C | arm B + 3 bloom filters | `ts` | value-by-tag + `has()` guards |
| D | arm B + 5 `MATERIALIZED` backbone columns | `ts` | plain columns |

## Arm A0 — the reference without its clustered index

Arm A sorts on exactly the five paths the five queries touch. **A facts table
cannot have that key**: it holds a mixture of document shapes, and most rows
would carry none of those paths, so ordering by them is available to a
single-schema entry and not to a general store. Comparing facts against arm A
therefore charges the data model for an advantage that is really the
benchmark's homogeneity.

Arm A0 is the control: the pinned DDL with the `ORDER BY` clause replaced by
`tuple()` and nothing else changed (`arm-a0/ddl-as-applied.sql`; the
derivation is a single substitution in `arm-a.sh`). Same 9,999,994 documents,
byte-identical results, and `EXPLAIN indexes=1` reports no `PrimaryKey` block
at all — there is nothing to prune with.

**What the clustered index was worth to arm A**, hot seconds:

| Q | A (indexed) | A0 (unindexed) | A0/A | A granules |
| --- | --- | --- | --- | --- |
| Q1 | 0.011 | 0.013 | 1.18× | 1225/1225 |
| Q2 | 0.192 | 0.253 | 1.32× | 1176/1225 |
| Q3 | 0.043 | 0.044 | 1.02× | 751/1225 |
| Q4 | 0.044 | 0.076 | **1.73×** | 115/1225 |
| Q5 | 0.058 | 0.070 | 1.21× | 115/1225 |

It also costs storage to give up: A0 is 1,814,273,851 bytes against A's
1,652,215,400 — **9.8 % larger**, because sorting by low-cardinality columns
clusters like values and compresses better.

### A00 — no hints either, and the queries stop working

A0 still declares five typed paths and bounds the JSON type with
`max_dynamic_paths = 0`. That is the same class of workload knowledge as the
sort key: for high-variability JSON there is no such five. A00 removes it —
plain `JSON`, engine defaults, engine-discovered subcolumns.

**The benchmark's queries do not run against it.** With no hints every
`data.<path>` is `Dynamic`, and ClickHouse refuses `GROUP BY` on a Dynamic
column (relaxable with `allow_suspicious_types_in_group_by = 1`) and refuses
`IN [...]` on one outright, with no setting to relax. Q3 cannot execute at
all. Getting numbers required 19 explicit casts, derived from the pinned file
by [`queries-native-dynamic.sh`](../../queries-native-dynamic.sh) so the delta
stays auditable. **That the workload's own queries need modification to run
against unhinted JSON is the clearest evidence that this benchmark is not
posed for high-variability documents** — and it is a property of the workload,
not of any system measured here.

A00 is also the *smallest* table of any arm: 1,150,367,898 bytes against A0's
1,814,273,851. Letting the engine discover paths and give each a typed
subcolumn compresses **37 % better** than the pinned DDL's
`max_dynamic_paths = 0`, which forces everything into shared data. The pinned
reference DDL is, on this corpus, a storage pessimisation.

### Against A0 and A00

Hot seconds, all five reference variants beside the facts arms:

| Q | A | A0 | A00 | B (facts) | D (facts + mat.) | D/A0 | **D/A00** |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Q1 | 0.011 | 0.013 | 0.049 | 0.117 | 0.014 | 1.08× | **0.29×** |
| Q2 | 0.192 | 0.253 | 0.385 | 0.957 | 0.265 | 1.05× | **0.69×** |
| Q3 | 0.043 | 0.044 | 0.159 | 0.338 | 0.048 | 1.09× | **0.30×** |
| Q4 | 0.044 | 0.076 | 0.197 | 0.700 | 0.055 | 0.72× | **0.28×** |
| Q5 | 0.058 | 0.070 | 0.192 | 0.714 | 0.060 | 0.86× | **0.31×** |

Peak memory, hot:

| Q | A0 | A00 | B | D | D/A0 | **D/A00** |
| --- | --- | --- | --- | --- | --- | --- |
| Q1 | 7.5 MB | 78 MB | 37 MB | 8.6 MB | 1.15× | **0.11×** |
| Q2 | 322 MB | 527 MB | 1165 MB | 343 MB | 1.07× | **0.65×** |
| Q3 | 159 MB | 214 MB | 74 MB | 170 MB | 1.07× | **0.79×** |
| Q4 | 293 MB | 419 MB | 534 MB | 298 MB | 1.02× | **0.71×** |
| Q5 | 321 MB | 428 MB | 550 MB | 317 MB | 0.99× | **0.74×** |

On disk: against A0, **B is 0.807× and D is 0.958×**; against A00, **B is
1.27× and D is 1.51×**.

Two readings, and both are true:

- **Against A0** (hints kept, index removed) the facts model with its backbone
  materialized is at **parity** — 0.72–1.09× latency, 0.99–1.15× memory, and
  4 % smaller on disk.
- **Against A00** (no hints, no index — the only variant a store holding
  heterogeneous documents could actually be) it is **1.4–3.5× faster on every
  query and uses 0.11–0.79× the memory**, at 1.51× the storage.

What remains in both readings is arm B — 2.1–3.7× against A00, 5.0–15.9× against
A — and that is the read path, not the model.

## Size — facts is *smaller* than native JSON at this tier

| Metric | A | A0 | A00 | B | C | D |
| --- | --- | --- | --- | --- | --- | --- |
| `total_size` | 1,652,215,400 | 1,814,273,851 | **1,150,367,898** | 1,463,557,309 | 1,461,300,356 | 1,738,291,789 |
| vs A0 | 0.911× | — | 0.634× | 0.807× | 0.805× | 0.958× |
| vs A00 | 1.436× | 1.577× | — | 1.272× | 1.270× | 1.511× |
| uncompressed | 6,309,069,537 | 6,785,254,205 | 4,627,929,616 | 24,940,202,647 | 24,940,383,073 | 25,450,534,022 |
| parts / marks | 5 / 1230 | 5 / 1230 | 10 / 1510 | 8 / 2015 | 2 / 2396 | 2 / 2395 |

This **inverts the 1M result** (where B/A was 1.44×). The facts shred carries
~4× the uncompressed bytes but compresses far harder — its support lanes
(`*card`, `len`) and its dictionary-encoded membership lanes amortise across
ten times the rows, while arm A's JSON shared-data overhead does not shrink
the same way. At 1M the tier was too small for that to show. **Storage is not
a cost of the facts model at this scale; it is a saving.**

Arm D's materialized columns add 274.7 MiB (+18.8 % over arm B), of which
`did` alone is 210.1 MiB and `time_us` 47.8 MiB; `kind`,
`commit_operation` and `commit_collection` together cost 6.6 MiB.

## The two levers, isolated

Arms B, D and E differ by one thing each, over identical data, so the ladder
attributes cleanly. Hot seconds at 10M:

| Q | B (as declared) | D (+materialized) | E (+re-keyed) | materialization | re-keying | both |
| --- | --- | --- | --- | --- | --- | --- |
| Q1 | 0.120 | 0.014 | 0.013 | 8.6× | 1.08× | **9.2×** |
| Q2 | 1.012 | 0.265 | 0.224 | 3.8× | 1.18× | **4.5×** |
| Q3 | 0.342 | 0.048 | 0.045 | 7.1× | 1.07× | **7.6×** |
| Q4 | 0.753 | 0.055 | 0.033 | 13.7× | **1.67×** | **22.8×** |
| Q5 | 0.826 | 0.060 | 0.034 | 13.8× | **1.76×** | **24.3×** |

**Materialising the backbone is worth 3.8–13.8×** and costs 274.7 MiB
(+18.8 % over arm B), four fifths of it `did`. It buys nothing structural —
arm D still reads every granule — it just replaces a per-row path
reconstruction with a column read.

**Re-keying is worth a further 1.07–1.76×**, and *reduces* storage: arm E is
1,575,922,975 bytes against arm D's 1,738,291,789, a 9.3 % saving, because
sorting on low-cardinality columns compresses better. It is the only lever
here that is free in both directions. The gain lands exactly where pruning
lands — Q4 and Q5, which read 227 granules of 2,389 against arm D's 2,393 of
2,393 — and is absent on Q1, which has no filter to prune on. Cold benefits
about twice as much as hot (Q4 0.114 → 0.060 s).

**Together they take arm B's 4.5–24.3× off the table**, and put facts at
**0.59–1.18× of the benchmark's own entry** — faster on Q4 and Q5, within
18 % on the rest — at 0.954× its storage.

## Latency — seconds, cold = try 1, hot = min(try 2, try 3)

| Q | A cold | A hot | B cold | B hot | C hot | D cold | D hot | B/A | **D/A** |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Q1 | 0.013 | 0.011 | 0.155 | 0.117 | 0.114 | 0.029 | 0.014 | 10.6× | **1.27×** |
| Q2 | 0.212 | 0.192 | 1.091 | 0.957 | 0.925 | 0.341 | 0.265 | 5.0× | **1.38×** |
| Q3 | 0.057 | 0.043 | 0.354 | 0.338 | 0.349 | 0.068 | 0.048 | 7.9× | **1.12×** |
| Q4 | 0.070 | 0.044 | 0.804 | 0.700 | 0.713 | 0.114 | 0.055 | 15.9× | **1.25×** |
| Q5 | 0.079 | 0.058 | 0.819 | 0.714 | 0.704 | 0.114 | 0.060 | 12.3× | **1.03×** |

Cold/hot spreads are modest throughout — on NVMe, re-reading the few columns
each query touches is cheap even from a dropped page cache.

## Memory — peak, hot

| Q | A | B | D | B/A | D/A |
| --- | --- | --- | --- | --- | --- |
| Q1 | 5.3 MB | 37 MB | 8.6 MB | 7.0× | 1.6× |
| Q2 | 272 MB | 650 MB | 343 MB | 2.4× | 1.3× |
| Q3 | 209 MB | 74 MB | 170 MB | **0.35×** | **0.8×** |
| Q4 | 95 MB | 527 MB | 298 MB | 5.6× | 3.1× |
| Q5 | 106 MB | 534 MB | 317 MB | 5.1× | 3.0× |

Arm B's memory is far better than the 1M run's 5.6–76× — that column was
measuring the open-coded query form, not the model.

## Pruning — the §4 hypothesis, and what actually separates the arms

`EXPLAIN indexes=1`, granules read of granules total:

| Q | A | B | C | D |
| --- | --- | --- | --- | --- |
| Q1 | 1225/1225 | 2007/2007 | 2394/2394 | 2393/2393 |
| Q2 | **1176/1225** | 2007/2007 | 2394/2394 | 2393/2393 |
| Q3 | **751/1225** | 2007/2007 | 2394/2394 | 2393/2393 |
| Q4 | **115/1225** | 2007/2007 | 2392/2394 | 2393/2393 |
| Q5 | **115/1225** | 2007/2007 | 2392/2394 | 2393/2393 |

- **§4's hypothesis holds at 10M**: arm B reads every granule on every query.
- **Arm C still prunes nothing** — 2 granules of 2394 on Q4/Q5, none
  elsewhere. Two tiers, same verdict: bloom filters over section value lanes
  cannot serve this workload.
- **Arm A prunes hard** — down to 115/1225 on Q4/Q5, a 10.7× reduction. This
  is what widened the B/A latency gap from the 1M tier: arm A's clustered
  index gets *more* valuable with scale, and arm B has no equivalent.

Arm D closes the gap to 1.03–1.4× **without pruning at all** — it reads every
granule, just of far cheaper columns. That means the two levers are
independent and additive, and the re-keying lever is still entirely unspent.

## Ingest

| | A | B |
| --- | --- | --- |
| Wall clock | 41.2 s (13 inserts: 10 files + 3 retries) | 330.6 s |
| Peak client RSS | 1.06 GB | 384 MB |
| Throughput | ~243k docs/s | 30,252 docs/s |
| Attributes written | — | 121,205,987 |
| Documents skipped | 6 | 6 |
| JSON nulls dropped | n/a | 10 |

Not a like-for-like comparison — see the 1M run's note; arm A's timed section
excludes decompression and it ships raw bytes rather than parsing in-process.

**Both arms skipped the same 6 documents.** The corpus contains malformed
JSON-lines records — file 5 line 91840 is truncated mid-string at exactly
65,536 bytes with its remainder on the next physical line. Arm A skips them
through upstream's documented retry with `input_format_allow_errors_*`
relaxed; the facts ingester needed the same tolerance added before the arms
held the same corpus at all. The 10 dropped JSON nulls are the facts model
having no `null` section — first exercised at this tier, and still tiny.

## The 100M gate (§9 Q6)

- **Disk.** Four arms at 10M occupy 5.9 GiB, so 100M projects to ~59 GiB,
  plus ~13.5 GB of raw `.gz` and a ~4.8 GB peak for arm A's decompressed
  staging: **~77 GiB against 262 GiB free.** Fits with margin.
- **Wall clock.** Arm A load ~7 min, arm B ingest ~55 min, arm C/D rebuilds
  and materialisation ~30 min, measurements ~20 min: **roughly two hours**,
  dominated by the facts ingest.
- **Verdict: the gate passes on both counts.** The one caveat is arm B's
  ingest rate, which is the trial's own single-process shredder and the
  obvious thing to parallelise before committing two hours.

## Reading the tax at 10M

The answer depends entirely on which reference you think a general fact store
should be measured against, so all three are reported.

| Reference | What it declares | Facts+materialized vs it |
| --- | --- | --- |
| **A** — the benchmark's entry | 5 typed paths + a clustered index on them | 1.03–1.4× slower, 1.05× storage |
| **A0** — index removed | 5 typed paths | 0.72–1.09× — parity, 0.958× storage |
| **A00** — nothing declared | — | **0.28–0.69× — 1.4–3.5× faster**, 1.51× storage |

A00 is the only one a store holding a mixture of document shapes could
actually be, and it is also the one whose queries had to be rewritten to run
at all. Against it, the facts model with a materialized backbone is faster on
every query and lighter on memory, and pays for that in storage.

Against A it is slower — but the whole of that difference is workload-specific
schema knowledge: five declared paths and a sort key over them. Those are
available to a benchmark entry built for five queries and not to a general
store, which is the sense in which this benchmark is not posed for
high-variability JSON.

The facts data model does not cost anything measurable here. What cost
something was asking it for values by path in SQL without materializing them —
and the trial's earlier figures, which charged the model for that *and* for
schema knowledge the reference could have and facts could not.
