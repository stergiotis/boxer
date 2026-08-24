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

## 2026-08-07 — M2, the reportable run at 10M — the formulation rule paid for itself on every engine, Parquet round-trips at parity, and one column costs the whole difference

- **Build under test:** boxer `72f56db1`; ClickHouse 26.7.3.19, DuckDB v1.5.5,
  datafusion-cli 54.1.0. 9,999,994 documents / 121,205,987 attributes.
- **Environment:** as M0. **Cold runs unavailable** (no passwordless sudo), so
  every latency below is hot = min(try 2, try 3) of 3 and the cold column is
  *absent*. Timing is **process** wall clock via `/usr/bin/time`, uniform across
  the three; startup baselines are recorded (`baseline.tsv`) and matter —
  ClickHouse's client costs **0.03 s**, DuckDB's and DataFusion's are below the
  timer's resolution. For ClickHouse the server-side figure is carried beside it
  in `clickhouse.mem.tsv` and is the one to quote for that engine alone.
- **Attempted:** nine configurations — three engines × {packed, exploded} plus
  the join formulation of exploded on each — measured on the same 10M corpus,
  and re-verified for correctness against the tiebroken oracle.

**Correctness at 10M is what it was at 1M**, with one new divergence found and
fixed (below): only Q1 in the exploded renderings (absent-path bucket) and U5
(the Int64 overflow). `m2-correctness.tsv`.

### The formulation rule paid for itself, and most on the engine I had not tested

Regroup (`GROUP BY doc` + `CASE WHEN`) against join (semi-joins on the path
prefix), hot seconds:

| | ClickHouse | DuckDB | DataFusion |
| --- | --- | --- | --- |
| Q2 | 1.010 → 0.480 | 2.720 → **0.630** | 1.910 → 1.510 |
| Q3 | 0.750 → 0.290 | 2.140 → **0.460** | 1.310 → 1.140 |
| Q4 | 1.090 → 0.360 | 2.920 → **0.660** | 1.670 → 1.260 |
| Q5 | 1.070 → 0.360 | 2.830 → **0.640** | 1.670 → 1.280 |

The gap is **4.3–4.7× on DuckDB** — larger than on ClickHouse, where the error
was first caught. Had M2 measured only the formulation I happened to write
first, DuckDB's exploded rendering would have been reported roughly four times
worse than it is. §4's rule was added after the arm X retraction; this is the
run that shows it was not a one-off precaution.

DataFusion gains least (1.1–1.3×), which is consistent with it having no sort
key to prune on — only Parquet row-group statistics.

### Cross-engine, at each side's best formulation

Full table in `m2-latency.tsv`. Two readings, and the caveat first: ClickHouse
reads its native MergeTree while the other two read Parquet, so this compares
**stacks, not engines**, and ClickHouse additionally carries 0.03 s of client
startup that hurts it most on the sub-100 ms queries.

- **Packed layout.** ClickHouse leads on 12 of 14. DuckDB is 1.5–2.0× behind on
  Q1–Q5 and 0.75–3.3× across the U-set; DataFusion 1.9–3.4× and 0.8–6.7×.
- **Exploded layout.** DuckDB **beats ClickHouse on the path-oriented half** —
  U3 0.010 s against 0.150, U6 0.000 against 0.030, U4 0.010 against 0.030,
  Q1 0.020 against 0.040 — and loses the reassembly half (Q2 0.630 against
  0.480). The split that arms X/Y found between layouts reappears here between
  engines on the same layout.

### Storage — and a correction I had to make mid-run

The first storage number I computed said Parquet costs the exploded layout
1.427× its native size. That was wrong, and comparing a like with an unlike:
**ClickHouse's Parquet export writes bloom filters by default**
(`output_format_parquet_write_bloom_filter=1`), which the native table does not
have. They are 349.9 MiB of a 1460.0 MiB file — the exact gap between the sum
of the column chunks and the file size, which is what prompted the check.

| layout | ClickHouse native | Parquet, default | Parquet, no bloom | no-bloom ÷ native |
| --- | --- | --- | --- | --- |
| packed | 1,540,236,424 B | 1,696,159,022 B (1.101×) | 1,548,274,422 B | **1.005×** |
| exploded | 1,072,833,164 B | 1,530,885,376 B (1.427×) | 1,164,039,241 B | **1.085×** |
| exploded ÷ packed | 0.697 | 0.903 | **0.752** | |

So the packed layout round-trips to Parquet at **parity**, the exploded layout
costs **8.5 %**, and the exploded form's storage advantage survives the trip
(0.697 → 0.752). Anyone sizing a Parquet export of leeway data should know the
default costs +9.7 % on the packed layout and **+31.5 %** on the exploded one
before any of this is measured.

**And the 8.5 % is one column.** `m2-encoding-gap.tsv`:

| col | ClickHouse codec | native | Parquet encoding | Parquet | Δ |
| --- | --- | --- | --- | --- | --- |
| `str` | ZSTD(3) | 978.2 MiB | PLAIN | 962.0 MiB | −16.2 |
| **`doc`** | **DoubleDelta, ZSTD(3)** | **18.6 MiB** | **PLAIN** | **119.3 MiB** | **+100.7** |
| `i64` | DoubleDelta, ZSTD(3) | 18.5 MiB | PLAIN+RLE_DICTIONARY | 22.4 MiB | +3.9 |
| everything else | ZSTD(3) | 7.6 MiB | PLAIN+RLE_DICTIONARY | 5.7 MiB | −1.9 |

The dense document id — a monotonically ascending integer, the single best case
for delta encoding — is written **PLAIN**, at 6.4× its native size, and that one
column more than accounts for the whole gap. `LowCardinality` survives fine:
`path`, `sym` and `section` all get `RLE_DICTIONARY` and come out at or below
their native size. **This is not a format limitation** — Parquet has
`DELTA_BINARY_PACKED`, and ClickHouse's writer did not select it. It is a
writer gap, which makes it a concrete target for M3, where leeway writes the
file itself.

- **Findings:**
  - **[pain — trial process / S2]** The exploded rendering's regroup
    formulation is 4.3–4.7× slower than its join formulation *on DuckDB* —
    worse than the ClickHouse gap that prompted §4's rule. A cross-engine
    comparison that fixes one formulation measures the author on every engine,
    not just the one where the mistake was noticed.
  - **[pain — trial process / S2]** Q5 diverged on DataFusion at 10M and **not
    at 1M**. `(max − min) / 1000` truncates the difference where
    `date_diff('millisecond', …)` counts millisecond boundaries crossed; the
    two differ by one whenever the sub-millisecond parts do not order the same
    way, which no row in the 1M sample hit and every row in the 10M top-3 did.
    Fixed by dividing each side before subtracting, which reproduces the
    boundary count exactly. **A smoke tier can pass a divergence through.**
  - **[note — engine default, not toolbelt / S3]** ClickHouse's Parquet export
    writes bloom filters by default, +31.5 % on the exploded layout. Worth
    knowing before quoting any Parquet size, and it invalidated this run's
    first storage number.
  - **[missing leeway-ddl-codegen → proposed:leeway-parquet-encoding-aspects / performance-efficiency.resource-utilisation / S3]**
    Round-tripping through ClickHouse's Parquet writer loses the `DoubleDelta`
    encoding aspect and nothing else that matters — 8.5 % of the exploded
    layout, concentrated in one column that Parquet could encode well. README
    §7 Q2 asks whether a DDL backend is worth writing rather than relying on
    schema inference; this is the first number on that question, and it says
    the encoding aspects are worth carrying but the type mapping is not the
    part at risk.
  - **Positive maturity: the ports hold at 10× the tier.** All four reproduce
    the oracle at 10M with the same two explained divergences as at 1M, after
    the Q5 fix. Nothing about the translation degraded with scale.
- **Solution size:** [`m2-bench.sh`](./m2-bench.sh) ~110 lines, and two
  join-formulation query files (`m2-exploded-join.duckdb.sql`,
  `m2-exploded-join.datafusion.sql`) at ~70 lines each.
- **Results:** `m2-latency.tsv` (nine configurations plus a best-formulation
  column per engine), `m2-storage.tsv`, `m2-encoding-gap.tsv`,
  `m2-correctness.tsv`, per-configuration `timings.tsv` and `baseline.tsv`.
- **Run dir:** [`./runs/2026-08-07-m2/`](./runs/2026-08-07-m2/)

## 2026-08-07 — M3, arm W — leeway writes the Parquet itself; the bytes match to 0.45 % and the *types* do not

- **Build under test:** boxer `1ee3266f` plus an uncommitted change to
  `apps/jsonbench/jsonbench_jsonmap_ingest.go`; ClickHouse 26.7.3.19,
  DuckDB v1.5.5. 1,000,000 documents, 12,045,072 attributes.
- **Attempted:** the milestone that turns "the layout ports" into "leeway
  writes it". A `--parquet-out` flag on `jsonbench jsonmap ingest` sends the
  same Arrow record batches to `dml.WriteArrowRecords` and never contacts
  ClickHouse; the schema comes from the generated builder's `GetSchema()`, so
  the file's columns are the leeway DDL pipeline's own output. Both sinks were
  then run over **the same source file with the same symbol routing** — the
  earlier corpora could not serve as the control, because the M1/M2 packed
  export holds a different 1M documents (12,114,997 attributes against
  12,045,072 here) and a size or schema comparison across different documents
  would mean nothing.

**The gate is split.** Same row count, near-identical bytes, different types:

| | leeway writer | ClickHouse export |
| --- | --- | --- |
| documents / attributes | 1,000,000 / 12,045,072 | identical |
| Parquet bytes (ZSTD, no bloom) | 155,186,737 | 154,494,328 — **1.0045×** |
| ingest | 13.28 s, 75,326 docs/s | 12.00 s, 83,355 docs/s |
| row groups | 1 | 3 |
| query results | **12 of 14 match byte-for-byte** | (reference) |
| U4, U9 | **fail to bind** | run |

- **Storage: the claim holds.** leeway's own writer lands within **0.45 %** of
  ClickHouse's export of the same data, and picks the same encodings —
  `RLE_DICTIONARY` on the low-cardinality lanes (`lmv`, `mvhp`, `symbol:value`),
  `PLAIN` on the high-cardinality payload. The neutrality claim survives on
  size: nothing about going through ClickHouse was making the file smaller.
- **Types: it does not.** Every canonical-`y` (bytes) column arrives as
  **`BLOB`** from leeway's writer and **`VARCHAR`** from ClickHouse's — the
  path lanes `lmv`, the array-coordinate params `mvhp`, and the row identity
  `id:blake3hash`. ClickHouse's DDL maps `y` to `String` and its Parquet writer
  emits Utf8; leeway's Arrow schema types it Binary.

**What the type difference costs, exactly.** Equality and `list_position`
survive — DuckDB casts a string literal to BLOB for those — so twelve of the
fourteen queries return byte-identical answers. The two that use a *string
predicate on a path* do not bind at all:

```text
U4  Binder Error: No function matches the given name and argument types
    'starts_with(BLOB, STRING_LITERAL)'
U9  Binder Error: ... 'contains(BLOB, STRING_LITERAL)'
```

Both are path-shape queries — subtree prefix census and array-degree discovery
— which is to say the two most characteristically *leeway* questions in the
set, the ones the USP document builds its thesis on. A consumer reading the
leeway-written file has to cast every path lane before it can ask them.

- **Findings:**
  - **[broken leeway-ddl-codegen → proposed:leeway-arrow-bytes-vs-text / functional-suitability.functional-appropriateness / S2]**
    One leeway schema yields two different Parquet column types depending on
    which writer wrote it: `BLOB` from `ddl/arrow` via `dml.WriteArrowRecords`,
    `VARCHAR` from the ClickHouse DDL backend's export. Membership path lanes
    hold text — JSON paths — and typing them as bytes makes `starts_with` and
    `contains` inapplicable, which is exactly what path-shaped queries need.
    Whether the fix belongs in the canonical type of a membership lane, in the
    arrow backend's mapping of `y`, or in a text-vs-bytes value aspect is a
    design question this trial does not settle; it only shows that the two
    backends disagree and that the disagreement is load-bearing.
  - **[note — writer parity / S4]** Writing Parquet in-process is **slightly
    slower** than inserting into ClickHouse (75.3k against 83.4k docs/s), which
    is the ZSTD work moving from the server into the ingesting process. Worth
    knowing before treating a Parquet sink as the cheap path.
  - **[note — trial process / S4]** The M1/M2 corpora could not act as the
    control here: they hold a different 1M documents. The correct control is
    the same source file through both sinks, which is what this run did.
  - **Positive maturity: `dml.WriteArrowRecords` carried the sink unchanged.**
    Arm W needed no change to the shredder, the generated builder, or the
    record batches — a flag, a writer, and a branch in `flush`. The seam
    README §3a claimed was free turned out to be free.
- **Solution size:** ~95 lines added to
  `apps/jsonbench/jsonbench_jsonmap_ingest.go` (a flag pair, `openParquet`, and
  a branch in `flush`). No change to any package under `public/`.
- **Results:** `m3-summary.tsv`, `m3-gate.tsv`, `m3-encodings.tsv`, and
  per-statement output under `res-leeway-written/` and
  `res-clickhouse-written/`. To reproduce the query comparison, point
  `m1-run.sh`'s `CWD` at a directory holding the file under test named
  `packed.parquet`; the two scaffolding directories that did so are not
  committed, being symlinks to gitignored data.
- **Run dir:** [`./runs/2026-08-07-m3/`](./runs/2026-08-07-m3/)

## 2026-08-07 — M4, the USP counterpart — the thesis survives the change of engine, and the reason for half of it does not

- **Build under test:** boxer `ba9f5b46`; DuckDB v1.5.5. 10,000,000 documents
  from the same ten source files, against the leeway arms' 9,999,994.
- **Environment:** as M0; hot = min(try 2, try 3) of 3, cold absent.
- **Attempted:** the arm H5 turns on. The sibling trial's
  [USP experiments](../jsonbench-on-facts/README.md) (§6) argue *a
  JSON column is fast when the path is in the query, a leeway table is fast
  when the path is in the data*, and rest that on two structural facts about
  ClickHouse's `JSON` type: an enumerated path cannot be used to **read** the
  column, and enumeration stops at an array (both in its §6a). This arm re-poses the
  head-to-head on an engine with a different JSON type, in the two shapes
  DuckDB actually offers.

**H5 is confirmed: both structural facts are ClickHouse's, not JSON's.**
DuckDB has `json_extract(doc, p)` with `p` a runtime expression, and
`json_tree(doc)`, a table function that walks every node — descending into
arrays — and reports each one's `fullkey`. The path *is* data. So arm N-text
expresses **all fourteen queries**, including the two the USP document records
as *no expression exists* (U5, U9) and the one it measures at 676× (U3).

**And the thesis survives anyway, because the cost half is larger, not
smaller:**

| | leeway (DuckDB) | N-text | ×  | N-struct | × |
| --- | --- | --- | --- | --- | --- |
| Q1 | 0.120 s | 0.560 s | 5 | **0.050 s** | **0.42** |
| Q2 | 0.650 s | 1.480 s | 2 | **0.200 s** | **0.31** |
| Q3 | 0.210 s | 1.720 s | 8 | **0.070 s** | **0.33** |
| Q4 / Q5 | 0.510 s | 1.24 / 1.26 s | 2 | **0.08 / 0.09 s** | **0.16** |
| U1 | 1.180 s | 58.6 s | **50** | *none* | — |
| U2 | 0.940 s | 62.7 s | **67** | *none* | — |
| U3 | 0.270 s | 19.0 s | **70** | *none* | — |
| U4 | 0.360 s | 26.7 s | **74** | *none* | — |
| U5 | 0.030 s | 25.3 s | **843** | *none* | — |
| U6 | 0.140 s | 42.9 s | **306** | *none* | — |
| U7 | 0.070 s | 0.540 s | 8 | 0.170 s | 2.43 |
| U8 | 0.030 s | 27.6 s | **920** | *none* | — |
| U9 | 0.560 s | 27.3 s | **49** | *none* | — |

Memory says it as loudly: N-text's path-in-data queries peak at **15–81 GiB**
against leeway's **0.07–0.36 GiB**.

**Three readings.**

- **The first half of the thesis holds harder here than in the sibling trial.**
  There, one query (U7) went to the JSON column, by 1.4×. Here **N-struct wins
  every benchmark query outright** — 0.16–0.42× of the leeway rendering on
  Q1–Q5 — because schema-on-read gives it plain typed columns. When the path is
  in the query, a typed column is simply the right representation and leeway is
  paying for generality it is not using.
- **The second half holds too, and for a changed reason.** In ClickHouse the
  path-in-data queries were slow or impossible because of the addressing model.
  In DuckDB they are all possible and **49–920×** slower — against the sibling
  trial's 11–676×. The verdict "the path is in the data" is not an artifact of
  ClickHouse's JSON type; the *explanation* was.
- **Schema-on-read types the backbone and abandons the tail.** Inference over
  10M documents produced typed `did` / `time_us` / `kind` / `commit.{rev,
  operation, collection, rkey, cid}` — and `commit.record` as
  **`MAP(VARCHAR, JSON)`**. That is precisely the heterogeneous part, and it is
  why N-struct answers **6 of 14** and only 1 of the 9 USP queries. Its speed
  and its blindness have the same cause.

**Storage** (`m4-storage.tsv`) — stack against stack, Parquet files against
DuckDB's native format, so read it as an order of magnitude and not a ratio:
leeway packed 1.44 GiB, leeway exploded 1.08, **N-struct 2.54, N-text 4.56**.
Here the JSON shapes are *larger* than leeway; in the sibling trial's
same-engine comparison jsonv2 was slightly smaller.

- **Findings:**
  - **[note — the USP thesis, refined / S2]** H5 holds: USP §2a and §2b describe
    ClickHouse's JSON type, not JSON columns generally, and the document's
    "no expression exists" rows are engine-specific. The thesis itself survives
    on cost, with a wider margin than it was originally measured at. Worth
    carrying back to the USP document as an Update if that trial is revisited.
  - **[note — engine, not toolbelt / S3]** DuckDB's JSON reader **aborts the
    whole read on the first malformed document** unless `ignore_errors=true` is
    passed; the corpus contains one. This is the same class as the sibling
    trial's ledger row 2, hit by a different reader.
  - **[pain — trial process / S2]** The session scratchpad is **tmpfs**, i.e.
    RAM-backed. A 7.2 GiB DuckDB database placed there competed with the
    queries' own working set, a run was killed with no error message, and the
    file was later wiped mid-milestone. Large working data belongs on
    disk-backed storage; this arm's database was rebuilt on btrfs and every
    number here comes from that.
  - **[pain — trial process / S4]** `TRY_CAST` is required where a type
    predicate and a cast appear in the same `WHERE`: DuckDB evaluated the cast
    before the `type IN (...)` filter and failed on a string atom.
  - **Positive maturity: none.** No boxer code ran in this arm.
- **Solution size:** [`m4-ntext.duckdb.sql`](./m4-ntext.duckdb.sql) ~137 lines
  for all fourteen; [`m4-nstruct.duckdb.sql`](./m4-nstruct.duckdb.sql) ~95, of
  which more than half is the header recording what cannot be written.
- **Results:** `m4-latency.tsv`, `m4-storage.tsv`, `m4-expressibility.tsv`, and
  per-statement output under `res-ntext/` and `res-nstruct/`. Q1 differs from
  the leeway arms by exactly **6 documents** in the absent-path bucket — the
  ones leeway's ingest skipped as undecodable — and by nothing else; N-text and
  N-struct are byte-identical to each other on it.
- **Run dir:** [`./runs/2026-08-07-m4/`](./runs/2026-08-07-m4/). The DuckDB
  database is a dataset and is not committed; `read_json_objects` /
  `read_json` over the ten source files rebuild it in ~30 s.

### Follow-up, same day — qualifying "N-struct beats leeway on every benchmark query"

The claim above is true and two things about it were unexamined: how much of
the margin is the *storage format* rather than the representation, and whether
the win extends past the paths inference typed. Both were measured
(`m4-backbone-vs-tail.tsv`), by loading the leeway packed Parquet into a DuckDB
**native** table so the two sit in the same format, and by asking one query
whose path is in the tail.

| query | class | N-struct | leeway (native) | winner |
| --- | --- | --- | --- | --- |
| Q1 | backbone | 0.05 s | 0.08 s | N-struct, 1.6× |
| Q2 | backbone | 0.19 s | 0.49 s | N-struct, 2.6× |
| T1 `commit.record/$type` | tail | 0.18 s | **0.07 s** | **leeway, 2.6×** |
| T2 `commit.record/createdAt` | tail | 0.17 s | **0.05 s** | **leeway, 3.4×** |

- **About half the headline margin was format, not representation.** Q1 is
  1.6× with both in DuckDB's native format against 2.4× when leeway is read
  from Parquet. The M4 table compares a DuckDB table to a Parquet file, which
  is what the ported arms actually do, but it is not a representation
  comparison and should not be read as one.
- **The win is exactly co-extensive with what inference typed.** All five
  benchmark paths sit in the backbone. One step into `commit.record` and it
  inverts.
- **The structural difference is the flatness.** leeway costs 0.05–0.08 s on
  single-path queries regardless of where the path sits; N-struct costs 0.05 s
  on a typed column and ~0.17 s once it has to go through
  `MAP(VARCHAR, JSON)` — 3.5× its own backbone cost. The benchmark set never
  makes it pay that, because JSONBench names only stable paths.

**A second finding fell out of the format-confound test, and it sharpens M3.**
Loading the ClickHouse-written Parquet into a DuckDB table **fails**:

```text
Invalid Input Error: Invalid string encoding found in Parquet file ...
value "\x8Bo\xFB\x89..." is not valid UTF8!
```

`id:blake3hash` holds raw 32-byte digests, and ClickHouse's export declares the
column Utf8. A scan that never touches the value succeeds — which is why every
query in M1 and M2 ran — but materialising it does not. So M3's finding is not
symmetric after all: **leeway's `BLOB` is the correct typing and ClickHouse's
`VARCHAR` produces a file that violates the Parquet string contract.** What M3
recorded as a disagreement is a disagreement in which one side is right, and
the inconvenient side is the right one. The convenience the ClickHouse typing
buys — `starts_with` on a path lane — is bought with an invalid file.

- **Findings (added):**
  - **[broken leeway-ddl-codegen → proposed:leeway-arrow-bytes-vs-text / functional-suitability.functional-correctness / S2]**
    Supersedes the severity, not the substance, of M3's finding: the ClickHouse
    DDL backend's `y`→`String` mapping exports Parquet columns declared Utf8
    that contain non-UTF-8 bytes, which DuckDB rejects on materialisation. Any
    fix should keep leeway's `BLOB` for genuinely binary lanes and distinguish
    the *path* lanes, which are text, rather than making everything Utf8.
  - **[pain — trial process / S3]** A cross-representation ratio was quoted
    across two storage formats. The §4 rule says a ratio needs the best
    formulation on both sides; it should say the same about the storage format,
    and this run is the second time that has bitten.

### Follow-up — the three DuckDB configurations, measured the same way

The trial had accumulated DuckDB numbers from three harnesses and two storage
formats. This settles them into one table (`m4-duckdb-configs.tsv`): the same
eight queries, the same engine, three ways of holding the same 10M documents.
The row-identity column is excluded throughout, because the native
leeway-shaped table cannot hold it (see the UTF-8 finding above).

| | Parquet (leeway shape) | native (leeway shape) | native (w/ MAP) |
| --- | --- | --- | --- |
| **storage** | **1.14 GiB** | 2.46 GiB | 2.54 GiB |
| Q1–Q5 total | 1.88 s | 1.38 s | **0.46 s** |
| U7 + T1 + T2 total | 0.26 s | **0.17 s** | 0.55 s |

Three readings, none of which the trial had stated:

- **Parquet is 2.2× smaller and 1.3–1.7× slower than DuckDB's own format**, for
  identical data and identical queries. That is the whole cost of holding
  leeway data in the portable format rather than the engine's, and it is a
  clean trade rather than a defect: the arms that read Parquet were paying it
  everywhere, unremarked.
- **The two native shapes cost the same to store** — 2.46 against 2.54 GiB,
  3 % apart. So schema-on-read does not buy a smaller table here; whatever it
  wins it wins on access, not footprint.
- **At equal storage the two shapes trade latency in opposite directions.**
  `MAP` is 3.0× faster over the five benchmark paths and 3.2× slower over the
  three tail paths. The benchmark set never crosses that line, which is why
  M4's first reading looked one-sided.

### Follow-up — the same three configurations on DataFusion

The DuckDB table above has no exact DataFusion counterpart, because
**DataFusion has no persistent native format.** It is a query engine over
files; the nearest analogue to a native table is a `CREATE TABLE … AS SELECT`
MemTable, which lives for one process. So the third column is a different kind
of thing and is reported as such. Nor can DataFusion *produce* the
schema-on-read shape — it ships no JSON functions (M0) — so that column reads a
Parquet file DuckDB's inference wrote. Evidence:
`m4-datafusion-configs.tsv`.

| | Parquet (leeway shape) | in-memory (leeway shape) | Parquet (struct/MAP shape) |
| --- | --- | --- | --- |
| **footprint** | 1.14 GiB on disk | **11.8 GB RAM** | 0.99 GiB on disk |
| Q1–Q5 total | 3.31 s | 1.63 s † | **0.65 s** |
| U7 + T1 + T2 total | 0.30 s | **0.15 s** † | 2.31 s |

† load-exclusive. The MemTable costs **1.23 s to build** and must be rebuilt
every session; load-inclusive the same totals are 7.78 s and 3.84 s.

- **The directional trade reproduces, with wider margins than DuckDB's.** The
  struct shape is **5.1×** faster over the five benchmark paths and **7.7×**
  slower over the three tail paths — against DuckDB's 3.0× and 3.2×. Same line,
  crossed harder. Two engines now agree that where the path sits decides which
  shape wins, which makes it a property of the shapes rather than of DuckDB.
- **In-memory is ~10× the on-disk footprint** — 11.8 GB of Arrow against
  1.14 GiB of Parquet+ZSTD — and buys 2.0× on the backbone and 2.0× on the tail
  against scanning the file. Whether that is worth 10× the RAM and a 1.23 s
  rebuild per session is a deployment question, not a data-model one, but it is
  the closest DataFusion has to the native-format row and it is a much worse
  trade than DuckDB's (2.2× storage for 1.3–1.7× latency).
- **DataFusion cannot reach the fast column by itself.** The struct shape is
  the only configuration here that beats DuckDB on the benchmark set, and
  DataFusion has no way to build it: it needs an engine with JSON inference to
  write the file first. As a consumer of someone else's Parquet it is
  competitive; as a self-sufficient stack over raw JSON it cannot start.

**And the UTF-8 defect reproduces on a second engine.** `CREATE TABLE … AS
SELECT * FROM` the ClickHouse-written Parquet fails in DataFusion too:

```text
Parquet error: encountered non UTF-8 data: invalid utf-8 sequence of 1 bytes
```

Two of the three engines now refuse to materialise that file, and only a lazy
scan that never touches `id:blake3hash` succeeds. The in-memory column above is
measured with that one column excluded, matching the DuckDB native row. This
removes any remaining doubt about which side of M3's `BLOB`-versus-`VARCHAR`
disagreement is correct.

### Follow-up — all six configurations, one harness, and why there is no single ranking

The obvious question the three tables invite is which configuration is best.
`m4-six-way.tsv` answers it, on one harness, over the two query dimensions the
trial has been separating all along. The ClickHouse `JSON` arm
(`jsonbench_a00_10m`, plain `JSON`, no type hints) was re-measured here rather
than quoted from the USP document, because this harness reads that arm's
path-census queries **~0.6× the document's figures** and mixing the two would
repeat the error §4 exists to prevent.

| configuration | storage | Q1–Q5 (path in the query) | U1,U2,U3,U4,U6,U8 (path in the data) |
| --- | --- | --- | --- |
| ClickHouse jsonv2 | 1.07 GiB | **0.72 s** | 234.72 s |
| ClickHouse leeway | 1.13 GiB | 1.24 s | **1.50 s** |
| DuckDB leeway (native) | 2.46 GiB | 1.38 s | — |
| DuckDB leeway (Parquet) | 1.14 GiB | 1.88 s | 2.92 s |
| DuckDB `MAP` (native) | 2.54 GiB | **0.46 s** | **no expression** |
| DataFusion leeway (Parquet) | 1.14 GiB | 3.31 s | 2.66 s |
| DataFusion `MAP` (Parquet) | 0.99 GiB | 0.65 s | **no expression** |

The two orderings are close to inverses, and neither is a ranking of the
configurations — both are rankings of *how well each configuration suits a
question type*:

- **Path in the query:** `MAP` shapes first (0.46 / 0.65 s), ClickHouse's JSON
  third (0.72 s), leeway fourth to seventh (1.24–3.31 s).
- **Path in the data:** leeway first on every engine (1.50–2.92 s), ClickHouse
  JSON **156× behind** at 234.72 s, and the `MAP` shapes not on the board at
  all — they cannot express any of the six.

**Storage separates them far less than latency does.** Five of the seven rows
sit between 0.99 and 1.14 GiB; the two outliers are DuckDB's native format at
2.46–2.54 GiB, which is a format choice rather than a shape one. So a
deployment picking between these is not trading disk — it is trading which
question type it wants to be fast at, and whether it wants the other type to be
possible at all.

**The two U-queries no configuration in this table answers well** are U5 and
U9, excluded above because ClickHouse's JSON type has no expression for either.
leeway answers both in well under a second on all three engines.

### Follow-up — retrieve against aggregate, and a recommendation withdrawn

The trial had been reading arms X and Y as *"exploded wins path-oriented
questions, packed wins document reassembly"*, and I had turned that into
deployment advice: multi-row is the discovery companion, one-row is the serving
shape. **The advice was wrong, and the reason is that every multi-attribute
query the trial had run was an aggregation.**

Q2–Q5 do combine four and five attributes — the objection that they might not
is answered, they do — but all four end in `GROUP BY` and collapse millions of
documents to a handful of rows. Nothing here had asked the shape a tiered
architecture opens with: **filter on several attributes and return the matching
rows**. Measured (`m4-retrieve-vs-aggregate.tsv`), ClickHouse, same corpus,
multi-row sorted `(path, doc)` and queried with semi-joins:

| shape | matches | one-row | multi-row | |
| --- | --- | --- | --- | --- |
| retrieve, 4 filters incl. a `did` | 7 | 0.50 s | **0.10 s** | multi-row **5.0×** |
| retrieve, 3 filters | 139,918 | 0.54 s | **0.25 s** | multi-row 2.2× |
| retrieve, 3 filters | 895,614 | 0.58 s | **0.32 s** | multi-row 1.8× |
| retrieve, 3 filters | 4,631,335 | 0.67 s | **0.49 s** | multi-row 1.4× |
| aggregate (Q2–Q5) | ~8.4M scanned | **0.13–0.44 s** | 0.29–0.48 s | one-row 1.1–2.2× |

**Multi-row wins retrieval at every selectivity measured**, and the margin grows
as the filter tightens — 1.4× when it returns 4.6M rows, **5.0× when it returns
seven**. The mechanism is visible in the shapes: one-row must evaluate
`indexOf` — a scan of the path lane — once per predicate per row, ten million
times over, and again per projected attribute; multi-row seeks each path range
once on its sort key and intersects document ids.

So the axis is not discovery against serving, and not selectivity on its own.
It is **retrieve against aggregate**:

- **Retrieve** — filter, then return rows: multi-row, 1.4–5.0×, better the more
  selective the filter.
- **Aggregate** — filter, then group and collapse: one-row, 1.1–2.2×, because
  the reassembly is paid across the whole matched set and no per-row seek
  advantage is left to recover it.

**What this withdraws.** The reading that multi-row belongs to exploration and
one-row to the serving layer does not survive. A tiered or hierarchical design
whose first stage narrows a candidate set is a *retrieval* query, and that is
multi-row's strongest measured case — the one ADR-0171 §SD5's companion table
would serve, which the §SD5 text describes only as accelerating "some form of
queries". This names them.

**Does it hold on the other engines? Two of three.** The retrieval result above
is ClickHouse-only, so it was re-run on the two targets over the same Parquet
files — packed against exploded, identical query shapes, all forms verified to
return the same 7 rows (`m4-retrieve-cross-engine.tsv`):

| engine | selective (7 rows out) | | mid (896k rows out) | |
| --- | --- | --- | --- | --- |
| | one-row / multi-row | | one-row / multi-row | |
| ClickHouse (native) | 0.50 / **0.10** | multi **5.0×** | 0.58 / **0.32** | multi 1.8× |
| DuckDB (Parquet) | 0.85 / **0.37** | multi **2.3×** | 4.91 / **1.08** | multi **4.5×** |
| DataFusion (Parquet) | **0.82** / 1.16 | **one-row 1.4×** | **0.94** / 1.41 | **one-row 1.5×** |

**DataFusion inverts it.** Multi-row is 1.4–1.5× *slower* there, where it is
2.3–5.0× faster on the other two. The exploded retrieval is five joins, and
ClickHouse's sorted MergeTree and DuckDB both turn each leg into a cheap seek
where DataFusion does not.

This is the sharpest statement of a distinction the trial has been circling:
**portability and performance are different properties of a rendering.** The
exploded form uses no array function, no lambda and no UDF — which is precisely
why DataFusion can run it at all, and why M0's descope was withdrawn. On
DataFusion it is also the *slower* of the two. Being expressible everywhere
does not make it fast everywhere, and the engine that most needs the portable
rendering is the one that benefits least from it.

One more thing the table shows in passing: **DuckDB's packed idiom degrades
badly with output size** — 0.85 s for 7 rows, 4.91 s for 896k — while
DataFusion's barely moves (0.82 → 0.94 s). Projecting through `list_position`
is where DuckDB spends it.

- **Findings (added):**
  - **[pain — trial process / S2]** The trial characterised two layouts from a
    query set that is entirely aggregations, and a recommendation was made from
    that characterisation. JSONBench has no retrieval query, so the shape was
    invisible until it was asked for directly. **A workload's silence about a
    query shape is not evidence about it** — the fourth confound of this family
    here, after formulation, storage format and configuration labelling.
  - **[note — engine, not toolbelt / S3]** The layout that wins retrieval is
    engine-dependent: multi-row on ClickHouse and DuckDB, one-row on
    DataFusion. Any deployment guidance from this trial has to name the engine,
    not just the layout.

### Correction, same day — the ClickHouse row above was the wrong arm

The table as first published invited the reading "DuckDB beats ClickHouse on
JSONBench's own queries", which **contradicts jsonbench.com and is an artifact
of two choices, both tilting the same way**.

- **The ClickHouse row was arm A00** — plain `JSON`, no type hints, no index.
  The sibling trial built that arm deliberately, as *the only shape a store
  holding a mixture of document shapes could actually have* (its §4), which is
  the right control for **this** trial's question and the wrong label for a
  benchmark comparison. **The actual JSONBench entry is arm A**: five typed
  path hints *and* a clustered index on them. Built here from the sibling
  trial's committed `arm-a0/ddl-as-applied.sql` with the sort key restored,
  9,999,994 rows, 1.54 GiB.
- **The fastest DuckDB row is off-benchmark.** `MAP` is `read_json` schema
  inference into typed `STRUCT` columns — schema-on-write, which JSONBench's
  stay-JSON rule excludes. It is not a legal entry, so it was never competing
  with ClickHouse on the benchmark's terms.

Arm A measured on this harness, Q1–Q5: **0.34 s** — Q1 0.04, Q2 0.13, Q3 0.06,
Q4 0.05, Q5 0.06. That is faster than every other configuration in the table,
including both `MAP` shapes, and **the ordering now agrees with
jsonbench.com**. The gap is wider than it looks: ClickHouse's client startup is
0.03 s per query, so 0.15 s of that 0.34 s is process launch and the
server-side total is ≈0.19 s, while DuckDB's and DataFusion's startup are below
the timer's resolution.

The corrected table is `m4-six-way.tsv`, which now carries an
`on_benchmark` column so the distinction cannot be lost again. Also filled:
DuckDB leeway **native** on the USP six, **2.24 s**, which the first table left
blank because only the Parquet variant had been run on that set.

**None of this moves the trial's own finding.** The two orderings are still
near inverses: arm A wins Q1–Q5 at 0.34 s and cannot express U5 or U9 at all,
while its unhinted sibling takes 234.72 s over the six USP queries it can
express against leeway's 1.50. What changes is the headline — *"ClickHouse's
tuned benchmark entry is the fastest thing here on the benchmark's own
queries"* — and it should never have read otherwise.

- **Findings (added):**
  - **[pain — trial process / S2]** A cross-system table was published with one
    engine represented by its deliberately-weakest arm, because that arm was
    the trial's control for a different question. Third confound of the same
    family in this trial after formulation and storage format: **the label on a
    row has to say which configuration it is, not which system.** The
    `on_benchmark` column exists so the next reader cannot repeat it.

## 2026-08-07 — M7, the 100M tier and the section lever — the USP gap widens with scale, the retrieval verdict took two corrections, and a schema lever the protocol never varied beats every index at the selective end

*This entry was reconstructed after the trial closed, during a review that
found the milestone had shipped its evidence into README §4a and the fact
sheet without the logbook entry §4's reporting rule requires. Every number
below was already committed in the run dir; the narrative is re-derived from
that evidence and the §4a text. The run dir is named `m6-100m` because it
predates the closing README edit that numbered this work M7, the write-up
having taken M6; the committed name stands.*

- **Build under test:** boxer `1fca52e0` … `78c4b1c5` (the evidence landed
  across eight commits); ClickHouse 26.7.3.19; workload pin unchanged. No
  boxer code ran — every arm is ClickHouse SQL over the M1-groundwork tables:
  99,999,968 documents / 1,200,650,881 attributes, packed and exploded, plus
  a plain-`JSON` arm holding the same documents for the other side.
- **Environment:** as M0, with the CPU governor pinned to `performance` and
  the form factor said plainly: a laptop (DMI chassis type 10, battery
  present) — `environment.tsv`. Hot = min(try 2, try 3) of 3, cold absent;
  the two JSON-side queries over 18 minutes ran once. Both document-per-row
  arms `ORDER BY tuple()`; the attribute-per-row arm keeps `(path, doc)`
  except where a row says unsorted.
- **Attempted:** four things the 10M tier had left open. (1) Whether the USP
  gap survives 10× the corpus. (2) The retrieval ladder at three predicate
  densities with every arm at its best form — the retrieve-vs-aggregate
  follow-up had measured that shape at 10M only. (3) `read_bytes`
  instrumentation of the selective point, to say *why* the arms differ.
  (4) The section-split arm that instrumentation implied.

### The USP head-to-head at 100M — every ratio is wider

Seven of the nine USP queries re-run at 100M (U2 and U7 were not re-run at
this tier), leeway packed against plain `JSON`, both unindexed
(`usp-100m.tsv`):

| | leeway | JSON type | ratio | ratio at 10M |
| --- | --- | --- | --- | --- |
| U3 find a value anywhere | 1.42 s | 1,116.61 s | **786×** | 695× |
| U8 numeric predicate, any path | 0.11 s | 1,125.88 s | **10,235×** | 2,783× |
| U6 leaf count | 0.10 s | 39.89 s | 399× | 50.7× |
| U4 subtree prefix census | 1.88 s | 39.92 s | 21.2× | 13.1× |
| U1 path census | 3.17 s | 39.79 s | 12.6× | 7.9× |
| U5 / U9 | 0.14 / 3.41 s | *no expression* | — | — |

On 10× the documents leeway scales at or below linear (1.7–8.9× per query)
and the JSON type at or above it (10.0–14.1×), so every ratio is wider at
100M than at 10M. The 10M columns in `usp-100m.tsv` are this harness's own
re-measure, not the USP document's figures — mixing harnesses is the error
§4 exists to prevent.

### Retrieval at three densities — two corrections before the number held

The four-criteria filter-and-retrieve query
([`queries-retrieve-4criteria.sql`](./queries-retrieve-4criteria.sql)), at
three predicate densities. It took two corrections, both of the confound
family the trial had already met four times:

- **The first comparison gave only one arm an index.** Exploded sorted
  `(path, doc)` had been put against packed and JSON arms at
  `ORDER BY tuple()`, and won by 2.2×. A JSON column *can* be sorted on the
  paths a query filters; so sorted, at 10M, it prunes to 5 granules of 1,225
  and answers in 0.03 s against the exploded arm's 0.10 — 3.3× the other
  way. The margin had measured the index. **Index parity** is the fifth
  confound of the family, after formulation, storage format, configuration
  labelling and workload silence: a layout comparison must give every arm
  the index it admits, or none of them any.
- **Packed was measured below its best form, twice.** Moving the selective
  predicate into `PREWHERE` is worth 1.1–1.7× (2.30 → 1.33 s at the
  selective point); splitting the five queried paths into their own sections
  is worth far more (below). §4's rule had been read as covering query text
  only; it has to cover the schema levers a representation offers.

Every arm unindexed at 100M, packed at its best form
(`retrieve-4criteria-100m.tsv`, `split-ladder-100m.tsv`; the sorted-exploded
column is the one arm carrying an index, kept for reference):

| events out | packed, one lane per type | packed, backbone split | JSON type | exploded, unsorted | *exploded, sorted* |
| --- | --- | --- | --- | --- | --- |
| 7 | 1.33 s | **0.09 s** | 0.80 s | 1.39 s | *0.29 s* |
| 10,936 | 1.65 s | **0.55 s** | 1.04 s | 1.40 s | *0.40 s* |
| 8,377,929 | 2.27 s | 1.32 s | **1.08 s** | 4.26 s | *1.95 s* |

### Why — bytes obliged, and the lever that follows

`system.query_log` at the 7-event point (`read-bytes-100m.tsv`): the
full-lane arms all sustain 21.4–25.6 GiB/s, durations within a few percent
of proportional to bytes read — exploded-unsorted 34.03 GiB, packed 28.27,
JSON 12.21, exploded-sorted 6.51. The query is bandwidth-bound, and the
layouts differ in how much of the corpus a predicate obliges them to touch.
The JSON type is not faster — it is narrower, one subcolumn per path.

Narrowness is a modelling choice leeway also has. The split arm moves the
five queried paths into their own sections — the same value-plus-path lane
shape, narrower — for a flat **1.40×** storage (13.93 → 19.49 GiB). At the
selective point it reads **4.49 GiB** and answers in **0.09 s**: 14.7× the
one-lane form, past every other arm including the indexed one, with no index
at all. That is *more* than its 6.3× byte reduction predicts — ≈50 GiB/s,
above the band — so at that width something besides streaming bandwidth
helps; recorded, not explained. The gain tracks selectivity and is gone by
8.4M events out, where the JSON type retakes the lead by 1.2×.

- **Findings:**
  - **[pain — trial process / S2]** Index parity: a layout comparison gave
    one arm an index the others could also have had, and the margin measured
    the index. Fifth confound of the family. Corrected by re-running every
    arm unindexed, with the indexed-JSON point kept as a separately-labelled
    10M measurement.
  - **[pain — trial process / S3]** Packed was quoted below its best form
    twice — `WHERE` for `PREWHERE`, and one-lane-per-type where the mapping
    admits a split. The §4 rule's word "formulation" was being read as query
    text only; a representation with a free schema lever has not been
    measured until the lever has.
  - **[note — data model / S2]** The section lever: 14.7× at the selective
    end for a flat 1.40× storage and no index, from one schema declaration.
    The JSON type's per-path subcolumn is this lever taken to its limit; a
    leeway schema chooses where on the spectrum to sit and pays in lane
    overhead rather than query time. One schema point, derived from the
    packed table rather than re-ingested, one query — the mechanism and its
    slope, not an optimum.
  - **[note — mechanism, recorded not explained / S4]** The split arm beats
    the bandwidth-proportional model that fits the four full-lane arms
    (≈50 GiB/s against their 21.4–25.6 band). The ranking by bytes read is
    unchanged.
  - **[pain — trial process / S3]** Reproducibility is thinner here than in
    the earlier milestones: the split table's DDL and the 100M plain-`JSON`
    ingest statement are not committed — a re-run reconstructs them from
    this entry and the fact sheet's method note. The retrieval query file is
    committed.
  - **[pain — trial process / S3]** This milestone initially shipped with no
    logbook entry — its evidence went to §4a and the fact sheet directly,
    and the entry above was reconstructed after close. Recorded so the gap
    is visible rather than silently patched.
  - **Positive maturity: none.** No boxer code ran in this milestone.
- **Solution size:**
  [`queries-retrieve-4criteria.sql`](./queries-retrieve-4criteria.sql)
  ~77 lines, one statement per arm and density. The split DDL and the
  plain-`JSON` 100M ingest were ad hoc; see the reproducibility finding.
- **Results:** `usp-100m.tsv`, `retrieve-4criteria-100m.tsv`,
  `read-bytes-100m.tsv`, `split-ladder-100m.tsv`, `storage-100m.tsv`,
  `environment.tsv`.
- **Run dir:** [`./runs/2026-08-07-m6-100m/`](./runs/2026-08-07-m6-100m/)
