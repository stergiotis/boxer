---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The canonical leeway JSON mapping at 100M

A new arm, not a re-run of A–E: the same Bluesky corpus under
`mapping.LoadJsonMapping` instead of `boxer.facts`, at the tier the trial
[descoped on 2026-08-06](../../README.md#8-milestone-cut-each-descope-able).
No comparison table was loaded — ratios against the native-JSON arms are taken
from the [10M run](../2026-08-06-m4-10m/results.md) and are labelled where they
are cross-tier.

The [environment](./environment.md) is identical to the 10M run's, and its
table there says precisely how this arm differs from the facts arms.

## The load

| | 10M | 100M |
| --- | --- | --- |
| documents | 9,999,994 | **99,999,968** |
| attributes | 121,205,987 | **1,200,650,881** |
| nulls dropped | 10 | 14 |
| undecodable documents skipped | 6 | 32 |
| wall clock | 185.968 s | **336.0 s** |
| processes | 1 | 8 shards |
| throughput | 53,773 docs/s | 38,0–39,0k docs/s per shard, **297,619 docs/s aggregate** |

Two things worth reading off this table.

**The shredder is shared with the facts arm, and the attribute count proves
it.** At 10M this arm produced 121,205,987 attributes and the facts arm
produced the same 121.2M from the same files — the decomposition into
(path, indices, value) triples is identical, and only the schema receiving it
differs. That is what makes the two arms comparable at all.

**Sharding worked and was the whole 100M gate.** The 10M logbook entry named
the single-process ingest as the thing to parallelise first; splitting the
corpus across 8 processes writing to one table took the load from a projected
~31 minutes to 5m36s. Per-shard throughput drops from 53.8k to ~38.7k docs/s
under contention, so the speed-up is 5.5× on 8 shards, not 8×.

Single-process throughput is **1.78× the facts arm's** 30,252 docs/s at 10M.
The write path is doing less work: one membership per attribute instead of two,
and no vocabulary id to resolve, because the path is written verbatim.

## Size

Two corrections landed after the first pass of this document, and both moved
the numbers in the same direction. They are described in
[leeway-usp-experiments.md](../../leeway-usp-experiments.md) §3a and §4a; in
short, the mapping declared **no encoding hints** on its `bool` / `int64` /
`float64` value columns (so they inherited server-default LZ4 while the support
lane beside them got `T64, ZSTD(3)`), and the row-identity column was written at
32 bytes where 16 would do. The mapping is fixed; the identity width is not, and
is excluded where the comparison requires it.

| | 10M | 100M |
| --- | --- | --- |
| on disk, pre-fix | 1,575,148,554 | 15,315,765,959 |
| **on disk, fixed codecs** | **1,540,236,424** | **14,960,053,637** |
| uncompressed | 7,707,485,159 | 76,576,345,191 |
| minus row identity (§3a) | 1,220,126,096 | 11,758,951,141 |

`int64:value` went from 2.89× to **7.12×** compressed (588,211,626 →
240,175,153 B at 100M); `bool` and `float64` improved 6× on much smaller lanes.
100M is **9.71×** the 10M table for 10× the documents — mildly sublinear, as
the dictionaries amortise.

Cross-tier, at 10M, against the arms the previous run measured on the same
corpus. **Arm B was rebuilt for this comparison** rather than extrapolated, and
reproduced its recorded total to 0.26 % (1,459,782,330 against 1,463,557,309),
which also confirms the recorded figure:

| Reference | on disk | row identity | this arm ÷ it, as loaded | ÷ it, minus identity |
| --- | --- | --- | --- | --- |
| A — benchmark entry, hints + clustered index | 1,652,215,400 | none | 0.932× | **0.739×** |
| A0 — same hints, no index | 1,814,273,851 | none | 0.849× | **0.673×** |
| A00 — no hints, no index | 1,150,367,898 | none | 1.339× | **1.061×** |
| B — boxer.facts (rebuilt) | 1,459,782,330 | 126,158,268 | 1.055× | **0.915×** |

The A-family have **no row identity at all** — a single `data JSON` column — so
the "minus identity" column is the like-for-like one there. Arm B does have one,
16 bytes to this arm's 32, so neither column is quite right for it; matching the
widths instead (this arm with a 16-byte hash, measured at 160,107,352 B) gives
1,380,233,448 against 1,459,782,330 — **0.945×**.

Two readings, both corrected from the first pass:

- **Against the facts schema the canonical mapping is smaller, not larger.**
  The first pass reported 1.076× — larger — because it charged this arm for a
  32-byte hash against facts' 16-byte one and for the missing int64 codec.
  Like-for-like it is **0.915×**, and it carries **less than a third of the
  uncompressed bytes** — 7,707,485,159 against 24,952,430,378 at 10M, both
  measured on the rebuilt tables — because it has no `len` lanes and one
  membership per attribute instead of two.
- **Against unhinted native JSON it is within 6 %**, not 37 %.

## The five benchmark queries

Seconds, cold = try 1, hot = min(try 2, try 3), per the pinned discipline.
Memory is the hot run's peak.

Both tiers re-measured on the fixed-codec tables, so the scaling column
compares like with like.

| Q | 100M cold | 100M hot | 100M memory | 10M hot | scaling |
| --- | --- | --- | --- | --- | --- |
| Q1 | 0.456 | **0.427** | 153 MB | 0.053 | 8.1× |
| Q2 | 6.121 | **5.343** | 3,132 MB | 0.600 | 8.9× |
| Q3 | 1.528 | **1.338** | 91 MB | 0.158 | 8.5× |
| Q4 | 4.274 | **3.698** | 1,104 MB | 0.428 | 8.6× |
| Q5 | 4.388 | **3.732** | 1,371 MB | 0.433 | 8.6× |

All five scale **sublinearly** (8.1–8.9× for 10× the data) on a table with no
clustered index, so every query reads every granule at both tiers. Cold/hot
spreads stay under 18 %.

The codec fix is a storage change, not a latency one: at 100M the five moved
between −6 % and +1 % against the pre-fix table, which is run-to-run noise on a
shared workstation. Two queries in the USP set do read the `int64` lane and got
measurably slower — see [leeway-usp-experiments.md](../../leeway-usp-experiments.md) §4.

### With the backbone materialized

The arm-D lever applies here too, and costs almost exactly what it cost the
facts arm — **+279,413,991 B, +18.1 %** at 10M against arm D's +18.8 %, with
`did` alone at 210.04 MiB against arm D's 210.1 MiB. The same five values cost
the same to store however they were reached.

What differs is the DDL. On facts each materialized expression rebuilds a
per-attribute path vector from `lmrcard`, and for the array-valued sections a
second vector from `len` (`arm-d.sh` carries five `arrayMap`/`arrayCumSum`
lines per section). Here each column is one `indexOf`:

```sql
ADD COLUMN kind LowCardinality(String) MATERIALIZED `symbol:value`[indexOf(`symbol:lmv`, '/kind')]
```

That does not close ledger row 5 — nothing still emits these definitions from a
leeway schema — but it makes hand-writing them legible rather than forbidding.

It also settles a question the storage table above invites: **no, the mapping
does not beat native JSON on storage once the backbone is materialized.**
Against A00, leeway with materialized columns is **1.582× as loaded** and
**1.304× minus row identity**, against 1.339× / 1.061× without them.

**Correctness.** At 10M this arm's five results are **byte-identical to the
recorded arm A output** (`diff` against
`runs/2026-08-06-m4-10m/arm-a/query-results.txt`), which is the trial's own
criterion. No reference arm was loaded at 100M, so the 100M outputs in
`bench/query-results.txt` are recorded but unattested.

**Against the 10M arms** (cross-tier ratios, this arm's 10M numbers):

| Q | vs B (facts) | vs A00 | vs A0 |
| --- | --- | --- | --- |
| Q1 | **0.50×** | 1.18× | 4.5× |
| Q2 | **0.67×** | 1.66× | 2.5× |
| Q3 | **0.54×** | 1.14× | 4.1× |
| Q4 | **0.66×** | 2.35× | 6.1× |
| Q5 | **0.68×** | 2.53× | 6.9× |

The canonical mapping is **1.5–2.0× faster than the facts arm** with no
materialized columns, no data-skipping indices, no re-keying and **no UDFs
installed at all**. Arm B needed `chpack` plus the `LEEWAY_*` read-back family
to express the same five queries; this arm needs `indexOf`.

That is the structural difference, not a tuning difference. In facts a path
resolution is `LEEWAY_VALUE_BY_TAG_EQUAL(vals, tags, tag, RAGGED_PARENT_IDS(card))`
— a membership-index → attribute-index indirection — and for the array-valued
sections `LEEWAY_LIST_BY_TAG_EQUAL` over a second cumulative sum on `len`. Here
the DDL has no `len` column in any section and one membership per attribute, so
`lmv[i]` names `value[i]` and the whole vocabulary is:

```sql
value[indexOf(lmv, '/commit/collection')]
```

Arm D (facts + five materialized backbone columns) remains faster than this arm
at 10M — 0.014/0.265/0.048/0.055/0.060 — which is expected and not a
counterpoint: materialization answers five known paths and this arm answers all
of them. The same lever is available here and was not pulled.

## The scenarios

[`queries-jsonmap-scenarios.sql`](../../queries-jsonmap-scenarios.sql) — the
questions the benchmark's five cannot ask. Hot seconds at 100M, same discipline:

| | Scenario | hot |
| --- | --- | --- |
| A1 | path census + per-path document counts | 103.6 |
| A2 | polymorphic paths (a path arriving as two types) | 8.7 |
| A3 | distinct document shapes | 5.0 |
| A4 | breadth and depth | 3.6 |
| B1 | value anywhere, and which paths held it | 2.0 |
| B2 | values repeated across paths within a document | 17.2 |
| B3 | subtree prefix census | 2.1 |
| C1 | array degree per path | 0.24 |
| C2 | primary (first) array element via `mvhp` | 2.4 |
| C3 | nested array coordinates | 0.30 |
| D1 | per-collection record schema inference | 4.9 |
| D2 | the rare-path long tail | 3.4 |
| D3 | absence within a collection | 0.83 |

**A1 is the outlier and the cost is the formulation, not the shape.** It uses a
per-row `countEqual` over each distinct path to return document *and* occurrence
counts in one pass, which is quadratic in the ~12 attributes a document carries.
Dropping the occurrence column gives the same census far cheaper — B3 and D2 are
that form, at 2.1 s and 3.4 s. The cost is recorded as measured rather than
tuned away, since the file is meant to be read.

What the scenarios found, reproduced at 100M:

- **3,197 distinct document shapes** (1,216 at 10M — 10× the documents, 2.6×
  the shapes), max 709 attributes, max depth 14.
- **Two genuinely polymorphic paths**, both under `skyfeedBuilder`, arriving as
  `string` in some documents and `int64` in others (5,856 and 97 documents).
  One at 10M; two at 100M.
- **Per-collection record schemas ranging from 3 paths to 355**
  (`app.bsky.graph.block` against `app.bsky.feed.post`), with two fields at
  0.0 % coverage — `/commit/record/type`, a typo for `$type`, and
  `/commit/record/subject/quoteCount`.
- **A path vocabulary that is not finite.** The long tail contains paths like
  `…/skeetsAppHistory/data/_/2024-11-21T16:31:48.241Z` — a client writing
  timestamps as object keys — so the path space grows with the corpus. That is
  the property a closed, registered path vocabulary cannot represent, and the
  reason this mapping's memberships are verbatim rather than Ref-shaped
  (ledger row 3).

## Solution size

| Artifact | Lines |
| --- | --- |
| `apps/jsonbench/jsonmap/dml_json.out.go` (generated) | 1,922 |
| `apps/jsonbench/jsonbench_jsonmap.go` — codegen + DDL commands | 254 |
| `apps/jsonbench/jsonbench_jsonmap_ingest.go` — sharded ingest | 345 |
| `queries-jsonmap.sql` | 82 |
| `queries-jsonmap-scenarios.sql` | 270 |
| edits to `jsonbench.go` / `jsonbench_shred.go` | +67 / +30 |

**Hand-written Go for the whole arm: 599 lines**, plus a generated builder and
a shredder reused unchanged from the facts arm. Nothing was promoted into the
leeway packages — a shipped JSON shredder is trial ledger row 1 and needs a
design dialogue first.

**Manual interventions: one.** The scenario file repeats two sub-selects
instead of binding them with `WITH … AS needle`, because the ADR-0116 handle
resolver does not descend into a WITH scalar subquery (see the logbook).
