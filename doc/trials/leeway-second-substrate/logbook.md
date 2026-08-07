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

Chronological, append-only record of runs of the
[leeway on a second substrate](./README.md) trial, per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in its own `./runs/<YYYY-MM-DD-slug>/` directory. Entry
template:

```markdown
## YYYY-MM-DD — <milestone / arm> — <one-line outcome>

- **Build under test:** boxer <commit>, <engine versions, workload pin>
- **Environment:** <CPU model, cores, memory, storage class, OS> — no
  hostnames or personal paths
- **Attempted:** <what this run set out to do>
- **Findings:** one line per proximate obstacle, per the trials README's
  *Finding classification*:
  **[<relation> <competence-slug> / <characteristic> / S#]** <statement>
  (evidence: <file in run dir>) — plus positive-maturity lines for
  competences the run leaned on successfully
- **Solution size:** <artifacts touched: files, lines — when applicable>
- **Results:** <the run's numbers, per README §4 — small tables are the
  expected form for this trial>
- **Run dir:** <./runs/YYYY-MM-DD-slug/ — evidence backing this entry>
```

The workload pin is the sibling trial's
([`../jsonbench-on-facts/upstream/PIN.md`](../jsonbench-on-facts/upstream/PIN.md)).

## 2026-08-07 — M0, acquire and pin — both engines installed; the gate passes for DuckDB and passes *differently* for DataFusion

- **Build under test:** boxer `237ffca5`; ClickHouse client 26.7.3.19,
  DuckDB v1.5.5 (Variegata), datafusion-cli 54.1.0. No corpus loaded — M0
  probes capability only.
- **Environment:** 32-core AMD Ryzen AI MAX+ PRO 395, 93.3 GiB RAM, Fedora
  Linux 44, kernel 7.1.6. **Server timezone `Europe/Zurich`** on both the OS
  and the ClickHouse server — see finding 4.
- **Attempted:** install and pin both target engines, then turn every
  *(c, unverified)* claim in README §3 into evidence, via
  [`probe.sh`](./probe.sh) — one labelled statement per capability, each in its
  own process so a failure records itself rather than aborting the file.
- **Outcome on the gate.** README §6 M0 said: *if DataFusion lacks the lambda
  forms U1–U9 need, descope arm J-df to Q1–Q5 and file the coverage finding.*
  DataFusion does lack them — completely — but the descope is **wrong**, and
  the probe found the reason. See finding 2.

**H-status after M0:**

| | Predicted | Found |
| --- | --- | --- |
| H2 | no general `arrayCumSum` counterpart | **holds** on both. DuckDB has no `list_cumsum`; the slice rewrite works but is quadratic. `list_sort` takes no key lambda, so the pack's key-sorted argsort has no direct form either |
| H3 | absent path yields NULL, not the type default | **holds, on both engines identically** |
| H4 | Q3 timezone-incomparable | **holds, and is live**: DuckDB timestamps are naive while the ClickHouse server runs `Europe/Zurich`, so Q3 differs by the offset unless pinned |
| H5 | the USP thesis is partly about ClickHouse | **refuted for DuckDB** — `json_extract(doc, p)` with `p` a *column* returns a value (evidence: `probe-duckdb.tsv`, `H5_json_extract_runtime`). The limit USP §2a describes is a ClickHouse limit, not a JSON-column limit |

- **Findings:**
  - **[pain leeway-read-access-codegen → proposed:leeway-query-vocabulary-portability / functional-suitability.functional-correctness / S2]**
    Absent-path lookups diverge silently: `indexOf`+`arrayElement` returns the
    type default in ClickHouse where `list_position`+`[i]` returns NULL in both
    targets, so a ported query returns a NULL bucket where the sibling trial
    reports an empty-string one, with no error on either side. The
    byte-identical-results check both prior runs passed cannot hold without an
    explicit coalesce (evidence: `probe-duckdb.tsv` and `probe-datafusion.tsv`,
    rows `H3_*`).
  - **[missing leeway-read-access-codegen → proposed:leeway-query-vocabulary-portability / functional-suitability.functional-appropriateness / S3]**
    leeway's lane algebra is written down only in its higher-order form
    (`arrayMap` / `arrayFilter` / `arrayReduce` and their DuckDB counterparts).
    DataFusion 54.1.0 has **433 routines and none of them higher-order** — no
    `transform`, `filter`, `reduce` or lambda under any spelling — so nothing
    in the repo tells a reader how to read a leeway table there. The probe
    shows the algebra nonetheless survives, by explosion: `unnest` plus
    ordinary relational operators express U4/U5/U8 exactly (evidence:
    `probe-datafusion.tsv`, rows `df_no_higher_order`, `df_U*_rewrite`).
    **This is why the pre-registered descope was wrong** — the right move is a
    second rendering of the same queries, not fewer queries.
  - **[pain leeway-read-access-codegen / usability.operability / S4]**
    DataFusion's `array_position` returns an unsigned type and `array_element`
    accepts a signed one, so the composed form — the single idiom the canonical
    mapping reads through — is a planning error until cast: `coercion from
    List(Utf8), UInt64 … failed` (evidence: `probe-datafusion.tsv`,
    `df_element_uncast` vs `df_element_cast`).
  - **[pain — trial process / S3]** The measurement box runs
    `Europe/Zurich`, not UTC, on both the OS and the ClickHouse server. Q3 is
    the query the sibling trial pre-registered as timezone-dependent, and the
    ported engines have no server timezone at all, so a Q3 comparison across
    the two trials is invalid until the offset is pinned or Q3 is dropped
    (evidence: `env.tsv`).
  - **[note — engine, not toolbelt / S4]** DuckDB 1.5.5 deprecates the `->`
    lambda arrow that the ClickHouse query files use, announcing removal in
    the next release. The ported files must use `lambda x: …`, so the two
    engines' query files cannot share lambda syntax even where the function
    names match (evidence: `probe-duckdb.tsv`, `lambda_*`).
  - **[note — engine, not toolbelt / S4]** `datafusion-cli` ships no JSON
    functions and no `CREATE MACRO`. Arms N-text and N-struct therefore have
    no DataFusion counterpart, and the USP head-to-head is DuckDB-only; a
    function pack cannot be installed there in any form.
  - **Positive maturity: none to record.** M0 exercised no boxer competence —
    no leeway code ran, nothing was generated, no corpus was touched. Stating
    that is more useful than crediting the trial for a successful download.
- **Solution size:** [`probe.sh`](./probe.sh), 1 file, ~120 lines, plus two
  evidence TSVs and two environment captures. No repo code changed.
- **Results:** capability only, no domain numbers. `probe-duckdb.tsv` — 39
  probes, 2 err, both of them H2's (`list_cumsum` absent, `list_sort` refuses a
  key lambda). `probe-datafusion.tsv` — 54 probes, 25 err: 24 in the shared
  ClickHouse/DuckDB-idiom set, and 1 in the 15-probe dialect pass, that one
  deliberate (`df_element_uncast`, which exists to show the cast is required).
  The gap between 24 and 1 is the finding: almost every DataFusion "failure" is
  a dialect difference, and the residue that is not — the higher-order
  functions — is what finding 2 is about.
- **Run dir:** [`./runs/2026-08-07-m0/`](./runs/2026-08-07-m0/)

## 2026-08-07 — arm X, the exploded rendering in ClickHouse — the two representations win opposite halves of the query set, and explosion is *smaller*

- **Build under test:** boxer `6a6eb4f7`; ClickHouse 26.7.3.19. Source table
  `jsonbench_j2_10m.json` — the fixed-codec canonical mapping the USP document
  measured, 9,999,994 documents.
- **Environment:** as the M0 entry. **Cold runs unavailable** — cache-dropping
  needs passwordless sudo, which this box does not grant, so every number below
  is hot = min(try 2, try 3) of 3, and the cold column is *absent*, not noisy.
  Fairness controls from the USP document carried over:
  `use_query_condition_cache=0`, `min_execution_speed=0`.
- **Attempted:** materialise the exploded rendering — one row per attribute,
  obtained with `ARRAY JOIN` — and measure it against the packed arm on the
  same corpus and the same fourteen queries. M0 had found that DataFusion can
  only express the lane algebra by explosion; this asks what that rendering
  costs where both forms are measurable side by side.
- **Preconditions asserted, not assumed.** `arm-x.sh` refuses to build unless,
  for every section, the value lane co-lengths with the path lane and every
  `lmvcard` is exactly 1. Both hold on the canonical mapping (violations=0 on
  all four populated sections), which is what makes `ARRAY JOIN` lossless here
  and would *not* hold on the facts arm.

**Storage — the explosion is smaller, not larger:**

| arm | rows | on disk | vs J |
| --- | --- | --- | --- |
| J — packed | 9,999,994 | 1,540,236,424 B | — |
| X — exploded, `ORDER BY (path, doc)` | 121,205,987 | 1,073,622,852 B | **0.70×** |
| X2 — exploded, `ORDER BY (doc, path)` | 121,205,987 | 1,382,545,797 B | 0.90× |

Repeating a document id across 121.2M attributes costs 18.63 MiB, because a
dense id ascending within each path group is what `DoubleDelta` is for (49.6×).
`path` costs 212.76 KiB at 858× — it is the sort-key prefix, so it is runs. Two
caveats keep this from being a clean win for explosion: arm J carries
`id:blake3hash` (20.3 % of it, per USP §3a) which arm X replaces with the dense
id, and **arm J is unsorted while arm X is sorted**, because a path inside an
array cannot be a sort key. That asymmetry is not a flaw in the comparison —
it is the thing being compared — but the storage number is not sort-neutral and
must not be quoted as if it were.

**Latency — the split is clean and it is the arm's whole result.** Full table
in `exploded-summary.tsv`; hot seconds, X against J:

| Explosion wins — path is the sort key | | Explosion loses — reassembly |  |
| --- | --- | --- | --- |
| U4 subtree prefix census | **0.02×** | Q3 hour histogram | 6.50× |
| U6 leaf count | 0.07× | U8 numeric predicate, all int paths | 4.43× |
| Q1 counts by collection | 0.16× | Q5 activity spans | 3.93× |
| U9 array degree | 0.19× | Q4 earliest posters | 3.84× |
| U7 constant-path presence | 0.46× | Q2 counts + distinct users | 2.45× |
| U3 value anywhere | 0.67× | U1 path census | 1.44× |

The mechanism is visible in the plans: arm X's Q1 reads **1,215 of 14,796
granules** on the `path` prefix, where arm J reads every granule of every query
(it is `ORDER BY tuple()`, and cannot be otherwise). The losing half is one
shape — `GROUP BY doc` with `anyIf` to rebuild a document from its attributes,
which is what the packed form gets for free by co-indexing within a row. The
memory column says it louder than the time column: **11.7–18.1 GB against
0.14–0.6 GB packed**, 24–84×.

**Re-keying halves the penalty and does not remove it.** X2 sorts by
`(doc, path)` so the reassembly runs in key order: Q2–Q5 drop from 2.45–6.50×
to 1.65–3.29× and their memory from 24–84× to 9–27×. It never reaches parity,
and it gives back the path-oriented half (U4 0.02× → 0.15×, Q1 0.16× → 0.52×).
No exploded sort key wins both halves; the two orders are opposites.

- **Findings:**
  - **[note — data model, not toolbelt / S3]** On this corpus the packed
    representation's advantage over one-row-per-attribute is **not storage and
    not path queries** — it loses both — but *intra-document co-indexing*,
    worth 1.65–3.29× time and 9–27× memory on multi-path queries even after
    the exploded arm is re-keyed to favour them. Nothing in the repo states
    that trade, and it is the load-bearing reason the arrays exist.
  - **[pain leeway-read-access-codegen → proposed:leeway-query-vocabulary-portability / portability.adaptability / S3]**
    The exploded rendering of all fourteen queries
    ([`queries-exploded.sql`](./queries-exploded.sql)) contains no array
    function, no lambda and no UDF — so it runs unchanged on an engine with
    none of those, which M0 showed DataFusion to be. leeway has a rendering
    that ports everywhere and nothing in the repo mentions it.
  - **[pain — trial process / S3]** Three of the nine USP queries (U1, U4, U9)
    are `LIMIT n` over tied values with no tiebreak, so two semantically
    identical renderings return *different rows*. They cannot serve as a
    cross-engine oracle as written; M1 needs a deterministic tiebreak added
    before DuckDB and DataFusion are compared against ClickHouse.
  - **[note — absent paths, third form / S4]** Q1's empty bucket does not
    appear on the exploded arm: a document lacking `/commit/collection` has no
    row rather than a defaulted one. With M0's DuckDB result this makes three
    renderings and three behaviours for the same absent path — empty string,
    NULL, and absent — none of which is wrong and no two of which agree.
  - **Positive maturity: the canonical mapping's 1:1 shape.** `ARRAY JOIN` is
    lossless here only because every membership is verbatim-1 and every value
    lane is scalar. The mapping delivered that without special handling, and
    the assertion in `arm-x.sh` passed on all four populated sections.
- **Solution size:** [`arm-x.sh`](./arm-x.sh) (~130 lines) and
  [`queries-exploded.sql`](./queries-exploded.sql) (~160 lines). No repo code
  changed. One defect of my own, caught by result comparison and fixed: U2 had
  projected `section`, which the upstream query groups by but does not select.
- **Results:** all fourteen queries verified against arm J. U5/U6/U7/U8 are
  byte-identical; U1/U2/U3 match once row order is normalised; U4 and U9 differ
  only in which tied row `LIMIT` admitted (see the process finding above); Q1
  differs by the absent-path bucket alone.
  `exploded-summary.tsv`, `exploded-sizes.tsv`.
- **Run dir:** [`./runs/2026-08-07-m0/`](./runs/2026-08-07-m0/) — `arm-x/`,
  `arm-x2/`, `arm-j/`
