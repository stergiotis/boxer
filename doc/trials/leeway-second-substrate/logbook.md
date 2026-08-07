---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# leeway on a second substrate — logbook

Chronological record of runs of the
[leeway on a second substrate](./README.md) trial, per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in `./runs/<YYYY-MM-DD-slug>/`. Entry template:

```markdown
## YYYY-MM-DD — <milestone / arm> — <one-line outcome>

- **Build under test:** boxer <commit>, <engine versions, workload pin>
- **Environment:** <CPU model, cores, memory, storage class, OS> — no
  hostnames or personal paths
- **Attempted:** <what this run set out to do>
- **Findings:** one line per proximate obstacle, per the trials README's
  *Finding classification* — plus positive-maturity lines
- **Solution size:** <artifacts touched: files, lines>
- **Results:** <numbers / pointers>
- **Run dir:** <./runs/YYYY-MM-DD-slug/>
```

The entries below were compacted on 2026-08-07: the arm X and arm Y runs
happened the same day against the same corpus and are recorded as one entry,
because arm Y is what corrected arm X's reading and the two are unreadable
apart. The workload pin is the sibling trial's
([`../jsonbench-on-facts/upstream/PIN.md`](../jsonbench-on-facts/upstream/PIN.md)).

## 2026-08-07 — M0, acquire and pin — both engines installed; the gate passes for DuckDB and passes *differently* for DataFusion

- **Build under test:** boxer `237ffca5`; ClickHouse client 26.7.3.19, DuckDB
  v1.5.5 (Variegata), datafusion-cli 54.1.0. No corpus loaded — capability only.
- **Environment:** 32-core AMD Ryzen AI MAX+ PRO 395, 93.3 GiB RAM, Fedora
  Linux 44, kernel 7.1.6. **Server timezone `Europe/Zurich`** on both the OS
  and the ClickHouse server.
- **Attempted:** install and pin both targets, then turn every
  *(c, unverified)* claim in README §3 into evidence via
  [`probe.sh`](./probe.sh) — one labelled statement per capability, each in its
  own process so a failure records itself rather than aborting the file.

**H-status after M0:**

| | Predicted | Found |
| --- | --- | --- |
| H2 | no general `arrayCumSum` counterpart | **holds** on both. No `list_cumsum` in DuckDB; the slice rewrite works but is quadratic; `list_sort` takes no key lambda, so the pack's key-sorted argsort has no direct form |
| H3 | absent path yields NULL, not the type default | **holds, on both engines identically** |
| H4 | Q3 timezone-incomparable | **holds, and is live**: DuckDB timestamps are naive while the ClickHouse server runs `Europe/Zurich` |
| H5 | the USP thesis is partly about ClickHouse | **refuted for DuckDB** — `json_extract(doc, p)` with `p` a *column* returns a value (`probe-duckdb.tsv`, `H5_json_extract_runtime`). The limit USP §2a describes is a ClickHouse limit, not a JSON-column limit |

**The gate.** README §6 M0 said to descope arm J-df to Q1–Q5 if DataFusion
lacked lambdas. It lacks them completely — 433 routines, no higher-order array
function under any spelling — but the descope was **wrong**: the probe showed
the lane algebra survives by explosion (`unnest` plus relational operators),
expressing U4/U5/U8 exactly. J-df keeps all nine USP queries in a second
rendering instead of losing four.

- **Findings:**
  - **[pain leeway-read-access-codegen → proposed:leeway-query-vocabulary-portability / functional-suitability.functional-correctness / S2]**
    Absent-path lookups diverge silently: `indexOf`+`arrayElement` returns the
    type default in ClickHouse where `list_position`+`[i]` returns NULL in both
    targets. The byte-identical-results check both prior runs passed cannot
    hold without an explicit coalesce (evidence: `probe-*.tsv`, rows `H3_*`).
  - **[missing leeway-read-access-codegen → proposed:leeway-query-vocabulary-portability / functional-suitability.functional-appropriateness / S3]**
    leeway's lane algebra is written down only in its higher-order form, so
    nothing in the repo tells a reader how to read a leeway table on an engine
    without lambdas — which the probe shows is possible (`df_no_higher_order`,
    `df_U*_rewrite`).
  - **[pain leeway-read-access-codegen / usability.operability / S4]**
    DataFusion's `array_position` returns unsigned and `array_element` accepts
    signed, so the composed idiom — the single form the canonical mapping reads
    through — is a planning error until cast (`df_element_uncast` vs
    `df_element_cast`).
  - **[pain — trial process / S3]** The box runs `Europe/Zurich`, not UTC, so a
    Q3 comparison across the two trials is invalid until the offset is pinned
    or Q3 is dropped (evidence: `env.tsv`).
  - **[note — engine, not toolbelt / S4]** DuckDB 1.5.5 deprecates the `->`
    lambda arrow the ClickHouse files use, removal announced for the next
    release; ported files must use `lambda x: …`.
  - **[note — engine, not toolbelt / S4]** `datafusion-cli` ships no JSON
    functions and no `CREATE MACRO`: arms N-text and N-struct have no
    DataFusion counterpart, and a function pack cannot be installed there.
  - **Positive maturity: none.** M0 exercised no boxer competence — no leeway
    code ran, nothing was generated, no corpus was touched.
- **Solution size:** [`probe.sh`](./probe.sh), ~150 lines. No repo code changed.
- **Results:** `probe-duckdb.tsv` — 39 probes, 2 err, both H2's.
  `probe-datafusion.tsv` — 54 probes, 25 err: 24 in the shared
  ClickHouse/DuckDB-idiom set, 1 in the 15-probe dialect pass and that one
  deliberate. The gap between 24 and 1 is the finding: almost every DataFusion
  "failure" is a dialect difference, and the residue is the higher-order gap.
- **Run dir:** [`./runs/2026-08-07-m0/`](./runs/2026-08-07-m0/)

## 2026-08-07 — arms X, X2, Y — the exploded renderings: competitive with packed, smaller on disk, and the run that caught a formulation error twice

- **Build under test:** boxer `55d0d9e8` … `796d5564`; ClickHouse 26.7.3.19.
  Source `jsonbench_j2_10m.json` — the fixed-codec canonical mapping the USP
  document measured, 9,999,994 documents, 121,205,987 attributes.
- **Environment:** as M0. **Cold runs unavailable** — cache-dropping needs
  passwordless sudo, which this box does not grant, so every number is
  hot = min(try 2, try 3) of 3 and the cold column is *absent*, not noisy.
  USP fairness controls carried over: `use_query_condition_cache=0`,
  `min_execution_speed=0`.
- **Attempted:** M0 found DataFusion can express the lane algebra only by
  explosion. These arms ask what that rendering costs where both forms are
  measurable side by side, on one engine, over identical data:

  | arm | layout | key | built by |
  | --- | --- | --- | --- |
  | J | packed — attributes in co-indexed arrays, one row per document | `tuple()` | (the sibling trial) |
  | X | exploded — one row per attribute, tagged union over sections | `(path, doc)` | [`arm-x.sh`](./arm-x.sh) |
  | X2 | as X | `(doc, path)` | `arm-x.sh ORDER=doc` |
  | Y | exploded — one table per section, no discriminator, no unused lanes | `(path, doc)` | [`arm-y.sh`](./arm-y.sh), derived from X |

- **Preconditions asserted, not assumed.** `arm-x.sh` refuses to build unless,
  for every section, the value lane co-lengths with the path lane and every
  `lmvcard` is exactly 1. Both hold (violations=0 on all four populated
  sections), which is what makes `ARRAY JOIN` lossless here and would **not**
  hold on the facts arm. `arm-y.sh` asserts row-count conservation across the
  split.

**Storage.**

| arm | rows | on disk | vs J |
| --- | --- | --- | --- |
| J — packed | 9,999,994 | 1,540,236,424 B | — |
| X — exploded, tagged union | 121,205,987 | 1,073,622,852 B | **0.70×** |
| X2 — same, `(doc, path)` | 121,205,987 | 1,382,545,797 B | 0.90× |
| Y — one table per section | 121,205,987 | 1,071,356,651 B | 0.70× |

Repeating a document id across 121.2M attributes costs 18.63 MiB, because a
dense id ascending within each path group is what `DoubleDelta` is for (49.6×);
`path` costs 212.76 KiB at 858×, being runs of the sort-key prefix. Two caveats
keep this from being a clean win: arm J carries `id:blake3hash` (20.3 % of it,
per USP §3a) which the exploded arms replace with the dense id, and **arm J is
unsorted while the exploded arms are sorted**, because a path inside an array
cannot be a sort key. That asymmetry is the thing being compared rather than a
flaw in the comparison, but the number is not sort-neutral.

**Latency, best formulation per arm, X against J** (full matrix in
`summary.tsv`):

| Exploded wins | | Exploded loses | |
| --- | --- | --- | --- |
| U4 subtree prefix census | **0.03×** | Q2 counts + distinct users | 1.06× |
| U6 leaf count | 0.07× | U2 path × type census | 1.16× |
| Q1 counts by collection | 0.18× | U1 path census | 1.32× |
| U9 array degree | 0.18× | Q3 hour histogram | 2.19× |
| U7, U3, Q4, Q5, U5 | 0.42–0.80× | U8 numeric predicate | 4.29× |

The mechanism is in the plans: arm X's Q1 reads **1,215 of 14,796 granules** on
the `path` prefix, where arm J reads every granule of every query — it is
`ORDER BY tuple()` and cannot be otherwise. **Memory is where exploded is
consistently worse**: 2.7–13.2× on the reassembly queries Q2–Q5, 11.3× on U1
and 69.8× on U8, against 0.01–0.68× on the queries it wins.

### The formulation error, retracted twice over

Arm Y's Q2–Q5 came in 2–5× faster than arm X's, which looked like the
per-section split paying off. It is not. Arm Y's queries are **joins** —
semi-joins for the filters, inner joins for the projections — because
per-section tables make that natural, where I had written arm X's as
`GROUP BY doc` with `anyIf`. Running Y's formulation against X's own
tagged-union table ([`queries-exploded-join.sql`](./queries-exploded-join.sql))
lands on Y's numbers:

| | X, `GROUP BY doc` | X, join form | Y, join form |
| --- | --- | --- | --- |
| Q2 | 0.990 s / 13,581 MB | 0.460 s / 2,605 MB | 0.468 s / 2,589 MB |
| Q3 | 0.695 s / 11,758 MB | 0.252 s / 1,848 MB | 0.252 s / 1,837 MB |
| Q4 | 1.039 s / 17,866 MB | 0.202 s / 1,251 MB | 0.202 s / 1,212 MB |
| Q5 | 1.066 s / 17,468 MB | 0.204 s / 1,272 MB | 0.199 s / 1,211 MB |

The join form prunes each leg on the `path` sort key and carries only surviving
documents forward; the regroup form builds all 10M groups and filters
afterwards.

> **Retracted, and kept visible because the retraction is the more useful
> record.** The first arm X reading was *"the packed representation's advantage
> is intra-document co-indexing, worth 1.65–3.29× time and 9–27× memory on
> multi-path queries."* **That is not a property of the representations** — it
> is the cost of the reassembly SQL I wrote for arm X, which the exploded form
> does not need. Corrected, exploded is at **parity or better on four of the
> five benchmark queries**.
>
> **It cost a second claim too.** Arm X2 was reported as showing re-keying to
> `(doc, path)` halves the reassembly penalty. Measured at each key's best
> formulation, `(doc, path)` loses to `(path, doc)` on *every* query
> (Q4 0.576 s against 0.202 s) — its apparent benefit was the regroup
> formulation being helped by a key that suits it, and the regroup formulation
> was the wrong one. **`(path, doc)` wins everywhere and X2 has no result.**
>
> The sibling trial's single largest error (its retracted S1) was the same
> mistake: a read-path formulation charged to the data model. Two trials, same
> trap. README §4 now requires a formulation search on both sides before any
> ratio is quoted.

### What the per-section split actually buys

**0.2 % of storage** — ClickHouse had already compressed the unused lanes away
before the layout removed them: `f64` cost 339 bytes and `b` 612 bytes across
121.2M rows in arm X, and the `section` discriminator 135 KiB. Nor does Y save
addressing, because `doc`, `path` and `mvhp` end up stored four times over four
short tables instead of once over one long one. At equal formulation the query
effects cancel: Y wins where a section predicate disappears (U5 0.67×, U8
0.68×, U3 0.93×) and loses where the union must be spelled out (U6 2.00×, U1
1.25×, U4 1.20×, U2 1.18×); Q1–Q5 are within noise of X.

- **Findings:**
  - **[pain leeway-read-access-codegen → proposed:leeway-query-vocabulary-portability / portability.adaptability / S3]**
    The exploded rendering of all fourteen queries
    ([`queries-exploded.sql`](./queries-exploded.sql)) contains no array
    function, no lambda and no UDF, so it runs unchanged on an engine with none
    of those — which M0 showed DataFusion to be. leeway has a rendering that
    ports everywhere and nothing in the repo mentions it.
  - **[pain — trial process / S1]** A representation-level ratio was quoted
    from one formulation per arm and was wrong by 2–5×, and a second claim
    (X2's re-keying benefit) inverted under the same correction. Nothing in the
    protocol required a formulation search; §4 now does. Second occurrence in
    this trial family.
  - **[note — data model, not toolbelt / S2]** One table per section is **not
    worth doing** on this engine for this corpus: 0.2 % storage, query effects
    that cancel, and it costs the reader the section roster — the union in
    U1/U2/U4/U6/U9 can only be written by someone who knows which tables exist.
    Arm X's schema is fixed by the mapping; arm Y's by which sections the data
    happens to populate, so a section appearing later is a DDL change in Y and
    a no-op in X.
  - **[pain — trial process / S3]** Three of the nine USP queries (U1, U4, U9)
    are `LIMIT n` over tied values with no tiebreak, so two semantically
    identical renderings return *different rows*. They cannot serve as a
    cross-engine oracle as written; M1 needs a deterministic tiebreak first.
  - **[pain — trial process / S3]** Arm X's committed evidence was generated
    before a fix to its own query file, so it disagreed with arm Y for a reason
    that was neither arm's. Evidence must be regenerated when the artifact that
    produced it changes; X and X2 were re-measured from the committed file.
  - **[note — absent paths, third form / S4]** Q1's empty bucket does not
    appear on the exploded arms: a document lacking `/commit/collection` has no
    row rather than a defaulted one. With M0's DuckDB result that is three
    renderings and three behaviours for the same absent path — empty string,
    NULL, absent — none wrong and no two agreeing.
  - **Positive maturity: the canonical mapping's 1:1 shape.** `ARRAY JOIN` is
    lossless here only because every membership is verbatim-1 and every value
    lane scalar. The mapping delivered that without special handling.
- **Solution size:** [`arm-x.sh`](./arm-x.sh) ~130 lines,
  [`arm-y.sh`](./arm-y.sh) ~80, [`queries-exploded.sql`](./queries-exploded.sql)
  ~160, [`queries-persection.sql`](./queries-persection.sql) ~170,
  [`queries-exploded-join.sql`](./queries-exploded-join.sql) ~60. No repo code
  changed.
- **Results:** all fourteen queries verified across arms — byte-identical
  between X and Y after normalising row order; against J, U5–U8 identical,
  U1/U2/U3 identical once ordered, U4 and U9 differing only in which tied row
  `LIMIT` admitted, Q1 by the absent-path bucket alone. `summary.tsv` (six
  measured configurations), `sizes.tsv`.
- **Run dir:** [`./runs/2026-08-07-m0/`](./runs/2026-08-07-m0/) — `arm-j/`,
  `arm-x/`, `arm-x-join/`, `arm-x2/`, `arm-x2-join/`, `arm-y/`

## 2026-08-07 — M1 groundwork, and the packed→exploded conversion rate at 100M — 50.8 s, bounded memory, a footprint ratio stable to 0.2 % across a 10× scale-up

- **Build under test:** boxer `f8032930`; ClickHouse 26.7.3.19.
- **Environment:** as M0. Single local disk (`default`, `/srv/clickhouse`);
  no multi-volume storage policy is configured on this server.
- **Attempted:** two things. First the M1 groundwork — the 1M corpus in both
  renderings, exported to Parquet, and a tiebroken oracle. Then, on the
  question of whether the exploded form is affordable as a *maintained
  redundancy* rather than an experiment: the conversion rate at 100M, and
  whether either form's footprint diverges with scale.

### M1 groundwork

[`m1-setup.sh`](./m1-setup.sh) builds the 1M pair from a dense document id
stamped before the split, so both Parquet files provably hold the same
documents, and pins **ZSTD** on the Parquet side (README §7 Q1, decided before
the numbers rather than after). **Both targets read both files** — arm P holds.
`LowCardinality` arrives as plain `VARCHAR` / `Utf8`, which is the
encoding-aspect loss §3a predicted, now observed.

[`m1-packed.clickhouse.sql`](./m1-packed.clickhouse.sql) is the oracle: the
sibling's two query sets run through `jsonbench resolve` to physical names —
committed rather than resolved per comparison, because handle expansion goes
through a ClickHouse parser and the ports cannot use it — then given
deterministic tiebreaks on Q1/Q2/Q4/Q5/U1/U2/U4/U9. It is now byte-identical
across `max_threads=1` and `max_threads=16`, which it was not before.

### The conversion, measured

[`convert.sh`](./convert.sh) does it in **one pass with no staging**: each
section's three lanes are zipped into a tuple array, the four are concatenated
and `ARRAY JOIN`ed once, so `rowNumberInAllBlocks()` is evaluated once per
source row and every section agrees on which document is which. `arm-x.sh`
needed a staged copy for that agreement, which would have charged the
conversion a full extra pass. Verified equivalent to the four-insert build at
1M on three independent checks — content multiset, per-document shape, and
per-document fingerprint multiset all differ by zero rows, up to document
renumbering.

| tier | documents | attributes | convert | docs/s | attrs/s | peak | packed | exploded | ratio |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1M\* | 1,000,000 | 12,114,997 | 0.42 s | 2.37M | 28.7M | 1.99 GB | 239,893,800 B | 107,968,396 B | 0.450\* |
| 10M | 9,999,994 | 121,205,987 | 3.93 s | 2.55M | 30.9M | 5.28 GB | 1,540,236,424 B | 1,072,833,164 B | **0.6965** |
| 100M | 99,999,968 | 1,200,650,881 | **50.79 s** | 1.97M | 23.6M | **5.41 GB** | 14,960,053,637 B | 10,439,188,043 B | **0.6978** |

\* The 1M packed source is a differently-constructed subset — a `LIMIT` of the
10M table, sorted by `doc`, with the `doc` column's bytes subtracted — so its
ratio is not on the same trend and is shown only for the rate. What moves it is
the *packed* side still gaining compression efficiency at that size: 239.9 B
per document at 1M against 154.0 at 10M and 149.6 at 100M. The exploded side is
already flat there — 8.91, 8.85, 8.69 bytes per attribute.

**Three results, against the assumption the question was posed under.**

- **The rate is not a constraint.** 100M documents and 1.2 billion attributes
  convert in **50.8 seconds**, reading 14.0 GiB at 281 MiB/s. Throughput falls
  ~24 % from 10M to 100M (30.9M → 23.6M attributes/s), which is a slope worth
  knowing but not one that changes the answer at this scale.
- **Memory is bounded, not proportional.** Peak is **5.28 GB at 10M and 5.41 GB
  at 100M** — a 2 % rise for a 10× row increase. The conversion streams; it
  does not accumulate. This is the operationally important number, because it
  is what says the conversion does not need a capacity plan of its own.
- **The footprint ratio is stable and nothing diverges.** 0.6965 → 0.6978
  across a 10× scale-up, a 0.2 % move. Per column, 10M → 100M against a 9.906×
  row increase: `str` 9.72×, `doc` **10.17×**, `i64` 9.51×, `sym` 10.46×,
  `mvhp` 9.37×, `path` 10.22×, `section` 10.27×. `doc` is the lane that had to
  be watched — a dense id repeated 1.2 billion times — and it is linear, at
  198.8 MB, 2.0 % of the exploded table. (`b` reads 19.3× on a 10 KB lane;
  that is a near-empty column, not a divergence.)

- **Findings:**
  - **[note — capacity planning / S4]** Both representations are well-behaved
    from 10M upward and the exploded form is the *smaller* of the two, so a
    deployment carrying both plans for **1.70× the packed footprint**, not
    2×. Below 10M the ratio is not yet settled, because the packed form is
    still gaining compression efficiency; a capacity plan extrapolated from a
    1M sample would be wrong in the safe direction.
  - **[note — mechanism available, unmeasured / S4]** Placing the two tables on
    different drives is expressible — MergeTree takes a `storage_policy`, and
    ClickHouse's volume/disk configuration is where that lives — but **this
    server has a single disk configured**, so the trial cannot measure the
    read-parallelism benefit. Recorded as available, not as demonstrated.
  - **[pain — trial process / S4]** `OPTIMIZE TABLE … FINAL` exceeded the
    client's 300 s receive timeout at 100M while the merge itself completed;
    a size read taken on the timeout would have been pre-merge. Sizes here are
    taken after `system.merges` drains.
  - **Positive maturity: none new.** No boxer code ran.
- **Solution size:** [`m1-setup.sh`](./m1-setup.sh) ~90 lines,
  [`convert.sh`](./convert.sh) ~110, [`m1-packed.clickhouse.sql`](./m1-packed.clickhouse.sql)
  generated + tiebroken.
- **Results:** `conversion-scaling.tsv`, `conv-10m/convert.tsv`,
  `conv-100m/convert.tsv`, `corpus/corpus.tsv`. Q1 at 100M returns identical
  counts from both representations.
- **Run dir:** [`./runs/2026-08-07-m1/`](./runs/2026-08-07-m1/)

## 2026-08-07 — M1, the ports — four renderings across three engines reproduce the oracle; the two divergences are an absent path and an integer overflow, and the overflow is ClickHouse's

- **Build under test:** boxer `29c10591`; ClickHouse 26.7.3.19, DuckDB v1.5.5,
  datafusion-cli 54.1.0, over the 1M Parquet corpus from the previous entry.
- **Attempted:** M1 proper — translate both query sets for both targets in both
  renderings, and compare every statement against the tiebroken oracle.
  [`m1-run.sh`](./m1-run.sh) runs a file on any of the three engines and emits a
  canonical per-statement result: all three are asked for CSV, then normalised
  for the four things they spell differently (quoting, NULL, trailing zeros on
  a rounded float, and DataFusion's ISO-8601 `T`). Nothing else is normalised,
  so a real difference still shows as one.

**The matrix** (`m1-comparison.tsv`), 14 statements each:

| | DuckDB | DataFusion |
| --- | --- | --- |
| packed, higher-order | 13/14 — U5 | *(inexpressible: no lambdas)* |
| packed, `array_element`/`array_position` | — | **14/14 exact** |
| exploded, relational | 12/14 — Q1, U5 | 13/14 — Q1 |

**The gate passes.** Every divergence falls into two explained classes and
neither is a translation error:

- **Q1, in the exploded renderings only, one row.** A document lacking
  `/commit/collection` has no row to contribute, where the packed renderings
  reproduce ClickHouse's `''` bucket exactly — 5,435 documents. This is a
  property of the *layout*, not of the engine or the dialect: both packed ports
  match, both exploded ports do not.
- **U5, `sum` over every integer in the corpus.** This is not a rendering
  difference. **ClickHouse silently overflows Int64** and returns
  `-1783513317384783548`; DuckDB promotes to a 128-bit accumulator and returns
  `1732210429611313068356`. Confirmed inside ClickHouse itself — widening the
  lane to `Int128` reproduces DuckDB's answer exactly, and a `Float64`
  accumulation agrees to the printed precision. **DataFusion wraps the same way
  ClickHouse does**, so two of three engines are silently wrong and the port is
  what exposed it.

**Correction, and it was mine.** The M0 entry's second finding is right that
DataFusion has no higher-order array function; I then over-read it into
"the packed rendering is inexpressible there", and wrote that into a committed
file header. It is wrong. The higher-order *rendering* is inexpressible; the
packed *layout* reads fine, through `array_element` over `array_position` —
neither of which is higher-order — plus `unnest` for the path-census set and
`array_max` standing in for `arrayExists(v -> v > k)`, which works because that
predicate is a comparison against a maximum. **That port is the only one of the
four that matches the oracle on all fourteen statements.** The header is fixed;
the distinction between layout and rendering is the thing to keep.

- **Findings:**
  - **[broken — workload, not toolbelt / functional-suitability.functional-correctness / S2]**
    U5 as written overflows a 64-bit accumulator on this corpus. ClickHouse and
    DataFusion wrap silently and disagree with DuckDB by 2^64. The sibling
    trial's USP document reports a runtime for U5 and this trial compared its
    *value* across arms X and Y — those comparisons remain valid, because both
    sides wrapped identically, but the number itself is not the sum of the
    corpus. Any future use of U5 as an oracle needs a widened accumulator.
  - **[pain leeway-read-access-codegen → proposed:leeway-query-vocabulary-portability / portability.adaptability / S3]**
    The whole of the packed read idiom ports to both targets, but not one
    character of it ports *unchanged*: DuckDB needs `list_position` and a
    coalesce, DataFusion needs `array_position`, a coalesce and a cast that the
    composed form is a planning error without. Three engines, three spellings
    of one idiom, and leeway documents one.
  - **[pain — trial process / S4]** The prelude is passed as a single CLI
    argument, so a leading `--` comment line reads as a flag to
    `datafusion-cli`. Comments are now stripped from it.
  - **Positive maturity: the tiebreaks hold.** Every statement that differed
    between arms X and Y on ordering alone now agrees across three engines, and
    the oracle is byte-identical across `max_threads` 1 and 16.
- **Solution size:** the four ported query files total 566 lines against the
  oracle's 102. The two exploded ports differ from each other by **26 lines of
  a 144-line file**, and only in the temporal functions — which is the measured
  cost of moving the exploded rendering between engines.
- **Results:** `m1-comparison.tsv`, and per-engine per-statement output under
  `oracle/`, `duckdb-packed/`, `duckdb-exploded/`, `datafusion-packed/`,
  `datafusion-exploded/`.
- **Run dir:** [`./runs/2026-08-07-m1/`](./runs/2026-08-07-m1/)
