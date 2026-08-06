---
type: explanation
audience: package maintainer
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-05
---

> **Provenance.** Compiled 2026-08-05, ahead of any measurement. Claims are
> two-tiered: (a) statements about this repository were checked against the
> working tree on the compile date (paths cited); (b) statements about
> JSONBench were taken from jsonbench.com and the `ClickHouse/JSONBench`
> GitHub repository (README and `clickhouse/queries.sql`) as fetched on the
> compile date — the benchmark evolves, so M0 pins an upstream commit and
> re-verifies everything quoted here against it.

# JSONBench on `boxer.facts` — a toolbelt trial, and the data-centricity tax

## 1 Question and scope

Two questions, **equally weighted in this first trial** (the trials
convention treats domain numbers as a side-product; this protocol elevates
them):

1. **The trial proper.** How well does the native toolbelt carry a
   recreation of JSONBench — which gaps and frictions surface on the way,
   and how short and how legible is the solution that emerges? The solution
   artifacts (mapping declarations, translated queries, the results applet)
   are committed and counted; every escape hatch is a finding, not a fix.
2. **The side-product, co-equal here.** What does holding the JSONBench
   Bluesky corpus in the `boxer.facts` data model cost, against ClickHouse's
   native JSON type on the same hardware and engine version — in storage
   footprint, index effectiveness, and query latency, with and without
   secondary indices?

This page is a trial protocol (see the [directory convention](../README.md));
runs append to the [logbook](./logbook.md). It is the
first task-level probe of the quality practice — an external, published
workload attempted with native idioms only, with friction filed as findings
rather than worked around. The finding fact family and its ISO 25010
classification are a forthcoming ADR; until it lands, the logbook carries
the ledger (§7).

In scope: reuse of the upstream dataset, queries, and measurement
discipline; the mapping design space; four experiment arms; the tier ladder;
results stored as facts with a book applet over them. Out of scope:
leaderboard eligibility (§3 — off-benchmark by design), the 1-billion-row
tier, systems other than ClickHouse, and mutation workloads (the upstream
benchmark is read-only analytics).

## 2 The benchmark, as verified upstream *(tier b)*

- **Dataset.** Bluesky Jetstream events, one JSON document per event. Tiers
  of 1M / 10M / 100M / 1B rows; the full corpus is quoted at 125 GB
  compressed, 425 GB raw.
- **Rules.** Analytics over documents that remain JSON — no relational
  schema-on-write; indexes explicitly allowed, both clustered (primary key /
  sort order) and non-clustered (secondary); otherwise default settings.
- **Metrics.** Data size and index size after load; per-query runtimes with
  cold and hot runs; memory; physical plans. Repetition counts and the
  cold-cache procedure live in the per-system run scripts, not the README —
  pin and vendor them at M0.
- **The five queries** (from `clickhouse/queries.sql`, 2026-08-05 fetch;
  re-verify against the pinned commit at M0):

  | # | Intent | Shape |
  | --- | --- | --- |
  | Q1 | event counts by collection | unfiltered GROUP BY `commit.collection` |
  | Q2 | counts + distinct users per collection | filter `kind='commit' ∧ operation='create'`, `uniqExact(did)` |
  | Q3 | hour-of-day histogram for post/repost/like | `toHour(fromUnixTimestamp64Micro(time_us))`, IN-list on collection |
  | Q4 | three earliest posters | GROUP BY `did`, `min(ts)`, LIMIT 3 |
  | Q5 | three longest activity spans | GROUP BY `did`, `date_diff(max,min)`, LIMIT 3 |

  Two portability notes, pre-registered: `toHour()` is server-timezone-
  dependent, so the server TZ must be pinned and recorded or Q3 results are
  not comparable; and the queries use ClickHouse-idiom forms (`IN [..]`
  array literals, `::String` casts) that the facts arms must translate
  through the native query path (§6) — any form grammar1/canonicalize cannot
  carry is a finding, not a blocker.

## 3 Why facts is off-benchmark, and what is measured instead

Mapping events through leeway is schema-on-write, which violates the
stay-JSON rule — a facts entry can never appear on the public leaderboard,
and this page does not pretend otherwise. What is reused: the dataset, the
query intents, and the measurement discipline. What replaces the
leaderboard: a local, same-hardware comparison whose product is a number
this repository has never had — the **data-centricity tax**, defined as the
deltas in size, latency, and memory between the native-JSON reference (arm
A) and the facts arms (B–D), plus how much of the gap each indexing step
closes.

## 4 System under test

The facts store's ClickHouse backend declares its table as
`MergeTree() ORDER BY ts` — timestamp only, no subject or kind in the key
([`chstore.go`](../../../public/keelson/runtime/factsstore/chstore/chstore.go),
`defaultEngineClause`). The in-code rationale: latest-state readback sorts
by `ts`, so a `ts` primary key turns that read into a sparse-index range
scan. Q1–Q5 group and filter by collection and user, not time.

**Standing hypothesis, to be confirmed or refuted at M2:** on the unmodified
facts table all five queries read every part — the primary index prunes
nothing.

The benchmark database is a **separate ClickHouse database using the facts
DDL**, not the live store instance — "facts holding the data" means the
model, not the production tables (§9 Q3 covers identity-space isolation).

The arms:

| Arm | What | JSONBench vocabulary |
| --- | --- | --- |
| A | upstream ClickHouse JSON entry, vendored DDL + queries, run locally | the reference |
| A0 | arm A with the clustered index removed (`ORDER BY tuple()`) | *(added 2026-08-06)* like-for-like on the key |
| A00 | plain `JSON`, no type hints, no index | *(added 2026-08-06)* the general-store reference |
| B | facts as-is: `ORDER BY ts`, no secondary indices | unindexed |
| C | B + data-skipping indices on kind / collection / subject columns | non-clustered |
| D | *(optional)* facts clone re-keyed for the workload | clustered |

Arm D exists to separate "wrong key" from "wrong model" and runs only if
B and C leave an unexplained gap.

**Arm A0 was added after the 10M run** and is now the primary reference for
the facts comparison. Arm A sorts on exactly the five paths the queries touch;
a facts table holding a mixture of document shapes cannot have that key,
because most rows carry none of those paths. Measuring facts against arm A
charges the data model for the benchmark's homogeneity. Arm A remains
reported, as the upper bound on what a workload-shaped clustered index buys a
single-schema table — 1.02–1.73× latency and 9.8 % storage at 10M.

## 5 Mapping design space

Bluesky events are heterogeneous: posts carry deeply nested records
(embeds, reply refs, langs); likes and follows are nearly flat. Candidate
shapes:

1. **One facts kind per collection**, each leeway-mapped to its own depth.
2. **One generic event kind**: a shallow common backbone — did, time_us,
   kind, operation, collection — with the remainder as an opaque payload.
3. **Hybrid**: the common backbone as one kind, per-collection payload
   kinds beside it.

A load-bearing observation: **Q1–Q5 touch only the common backbone.** No
query reaches inside `commit.record`. Mapping depth beyond the backbone
therefore changes storage size, not benchmark latency — which cleanly
splits the experiment: latency comparisons need only the backbone; how
deeply the long tail is mapped is a size-only sub-experiment (deep leeway
map vs. payload-as-string, measured, not argued).

The backbone's string-vs-symbol split likewise needs no sample-data
inference: the pinned upstream DDL already encodes it in its typed-path
hints ([the pin](./upstream/PIN.md) § The table). Read across to leeway:
`kind`, `commit.operation`, and `commit.collection` are
`LowCardinality(String)` upstream, so they belong in the `symbol`
tagged-value section; `did` is a plain `String` upstream — deliberately
un-dictionaried at millions of distinct users — and stays `string` (it is
the subject-identity axis besides, §9 Q3); `time_us` is numeric-temporal.

Ingest rides the existing lanes: RowDML native ingestion
([ADR-0089](../../adr/0089-rowdml-serialization-clickhouse-native-ingestion.md)),
with raw JSON staged per §9 Q5. Subject identity for DIDs — millions of
distinct users at the 100M tier — exercises the identity path
([ADR-0106](../../adr/0106-identity-fibonacci-tags-build-tag-retirement.md))
at a cardinality it has not seen; ingest wall-clock and minting cost are
recorded results, not incidental.

## 6 Method

- **Environment.** The repo-pinned ClickHouse server version; hardware,
  server TZ, and disk recorded at run time. Cold/hot procedure vendored
  from the upstream run scripts at M0.
- **Tier ladder.** 1M as smoke, 10M as the development tier, 100M as the
  real run — each tier gated on the previous one's disk and wall-clock
  actuals (§9 Q6). 1B is descoped outright. **100M is descoped too, as of
  2026-08-06** — the gate passes and the tier was never run; §8 M4 says what
  that costs the result.
- **Per query, per arm:** cold and hot runtimes, memory,
  `EXPLAIN indexes=1` pruning evidence (parts and marks read vs. total).
- **Per arm:** data size, index size (from `system.parts` /
  `system.data_skipping_indices`), ingest wall-clock and peak memory.
- **Idiom rule.** The facts arms' queries run through the native path —
  play/sqlapplet, grammar1 parse, canonicalize — exactly as any boxer
  workload would. Arm A may use `clickhouse-client` directly: it is the
  reference being measured against, not the idiom under test.
- **Process measurement.** The solution itself is a result: artifacts are
  committed, their size recorded (files, lines, passes touched), and manual
  interventions counted — an escape hatch is by definition a finding.
- **Reporting.** Domain results land as facts and are read back through a
  results book applet
  ([ADR-0132](../../adr/0132-sqlapplet-sql-defined-applets.md)) — the benchmark
  dogfoods the reporting layer it is measuring. Every run, at every
  milestone, appends a logbook entry (date, build, hardware, findings,
  outcome) per the directory convention.

### 6a Teardown

Each arm script drops its own database before rebuilding it, so re-running one
arm needs no cleanup. Nothing sweeps them all; a full run leaves one database
per arm per tier (the 1M and 10M runs together left 13, ~11 GiB). To reclaim
that, list first and drop second:

```sh
clickhouse-client -q "SELECT name FROM system.databases WHERE name LIKE 'jsonbench%' ORDER BY name"

clickhouse-client -q "SELECT name FROM system.databases WHERE name LIKE 'jsonbench%'" \
  | xargs -r -I{} clickhouse-client -q "DROP DATABASE IF EXISTS \`{}\`"
```

Dropping them loses nothing reproducible: the run directories are the numbers
of record, the arm scripts rebuild from the raw corpus, and the `jsonbench`
book carries its own committed summary rather than reading `jsonbench_results`.

**The pattern deliberately does not catch the UDFs.** `chpack` and the
`LEEWAY_*` read-back family install server-wide rather than into a
`jsonbench_*` database, and they are a shipped repo capability
([ADR-0162](../../adr/0162-leeway-co-ragged-function-pack.md)) that other work
may be using — so a benchmark teardown has no business removing them. Removing
them deliberately is awkward for the reason §7b row 7 files: the read-back
family carries no version marker and every statement is `CREATE OR REPLACE`, so
there is no reconcile step and the functions have to be dropped by name.

## 7 Findings ledger

Friction encountered while executing this plan is filed as findings rather
than silently worked around — competence slug × relation
(`missing` / `broken` / `pain`) × ISO 25010 characteristic, with severity
and evidence, per the classification scheme in the
[directory convention](../README.md). Competence slugs come from the corpus
vault ([ADR-0168](../../adr/0168-capmap-business-capability-corpus.md));
a `missing` finding anchors at the nearest existing competence and proposes
a slug. Until the finding fact family lands, findings live in the
[logbook](./logbook.md) in the convention's line format, so later migration
to facts is mechanical. Pre-registered
candidates, so later readers can tell hypotheses from surprises: the Q3
timezone dependency; grammar coverage of `IN [..]` array literals,
`date_diff`, and `::String` casts; identity-minting throughput at DID
cardinality; ingest-lane throughput at the 100M tier.

### 7b The ledger, rolled up

The [logbook](./logbook.md) stays the per-run record — a finding appears there
in the entry that hit it, with the review that later moved it. This table is
the deduplicated roll-up across both runs: one row per proximate obstacle,
with the status each carried when the trial closed on 2026-08-06. **Open /
fixed was re-checked against the tree at `b33bab3a`**, not carried over from
the entry that filed it.

| # | Finding | Class | Status at close |
| --- | --- | --- | --- |
| 1 | Nothing in-tree turns a JSON document into rows under `mapping.LoadJsonMapping`'s schema — every leeway ingestion here is hand-written per domain | `missing leeway → proposed:leeway-json-shredding` / functional-suitability.functional-completeness / S3 | **open** — `mapping/` still exports schema constructors only; needs a dialogue (new package) |
| 2 | The trial's shredder aborted the whole ingest on the first undecodable document, where the reference loader skips and continues | `broken leeway-dml-codegen → proposed:leeway-json-shredding` / reliability.fault-tolerance / S2 | fixed in the trial's own ingester; recurs in whatever (1) becomes |
| 3 | boxer.facts sections accept only Ref-shaped memberships; an open JSON corpus has no closed path vocabulary, so paths were demoted into the high-cardinality parameter channel | `broken leeway-dml-codegen` / functional-suitability.functional-appropriateness / S2 | **open** — facts schema change, Tier 1; needs a dialogue |
| 4 | boxer.facts has no `null` / `undefined` section, so JSON nulls cannot round-trip | `missing leeway → proposed:leeway-json-shredding` / functional-suitability.functional-completeness / S4 | **open** — verified: 21 tagged-value sections, none of them null. 10 nulls dropped at 10M |
| 5 | No tooling emits `MATERIALIZED` column definitions from a leeway schema, so a SQL consumer hand-writes them per path and keeps them in sync with physical column names | `missing leeway-ddl-codegen → proposed:leeway-sql-materialized-projections` / functional-suitability.functional-completeness / S3 | **open** — verified: no `MATERIALIZED` anywhere under `leeway/`. This is the lever worth 3.8–13.8× |
| 6 | Nothing a task-level consumer walks — the leeway skills, `mapping`, the generated DML/RA packages — points at the SQL query vocabulary | `pain leeway → proposed:leeway-query-vocabulary-discoverability` / usability.user-error-protection / S2 | **open** — verified: zero mentions of `chpack` or the `LEEWAY_*` family across all three leeway skills. **Filed three times** (chpack, the read-back family, ADR-0116 column handles) |
| 7 | The read-back UDF family carries no version marker, and every statement is `CREATE OR REPLACE`, so nothing detects or removes a retired function | `broken leeway-ddl-codegen → proposed:leeway-udf-provisioning-drift` / reliability.maturity / S3 | **open** — verified: `chpack` has `Version`/`LEEWAY_PACK_VERSION`, the read-back family has neither |
| 8 | A Ref-membership table cannot be read by anyone who does not already hold the registry — there is no server-side name→id lookup, so ids ride SQL pages as uint64 literals | `pain leeway-ddl-codegen → proposed:leeway-vocab-introspection` / usability.self-descriptiveness / S3 | **open** — verified: no vocabulary table is reachable from SQL |
| 9 | Resolving a value by path in SQL needs two independent cumulative-sum reconstructions, one over `lmrcard` and one over `len` | `pain leeway-read-access-codegen` / performance-efficiency.resource-utilisation / S3 | **halved on review** — only the `len` half is intrinsic to facts; the other half was this trial's own encoding choice |
| 10 | Getting that second reconstruction wrong fails *silently* on this corpus — every attribute has `len = 1`, so a naive co-index returns correct answers here and wrong ones on any multi-element array | `pain leeway-read-access-codegen` / functional-suitability.functional-correctness / S2 | **open as a hazard**; `LEEWAY_LIST_BY_TAG_EQUAL` is the form that does not have it |
| 11 | The facts DDL cannot be applied, or the table cloned, without `allow_suspicious_low_cardinality_types=1` | `pain leeway-ddl-codegen` / usability.operability / S4 | **open** — every client invocation in the harness carries the flag |
| 12 | `FROM {db:Identifier}.facts` fails grammar1 with *no viable alternative*, so an applet carrying it never mounts; ClickHouse itself accepts the form | `missing nanopass-pass-pipeline → proposed:grammar1-identifier-params` / functional-suitability.functional-completeness / S3 | **fixed 2026-08-06** — `paramSlot` was an alternative of `columnExpr` only; it is now also one of `tableIdentifier` / `databaseIdentifier` in grammar1 **and** grammar2 |
| 13 | A column handle bound by a `WITH <handle> AS alias` expression alias is not resolved — the pass visits a SELECT's own scope but not the WITH-expression clause | `missing nanopass-scope-resolution → proposed:resolve-column-names-with-aliases` / functional-suitability.functional-completeness / S3 | **fixed 2026-08-06** — the clause parses as the query-level `ctes` rule, a *sibling* of selectStmt; the pass now visits it against the first SELECT it precedes |
| 14 | Applet pages named `FROM facts` unqualified; hand-testing them with `--database=` hid it completely, and every page failed `UNKNOWN_TABLE` under a real applet | `pain — trial process` / S3 | fixed; a regression test now rejects an unqualified table reference in the book |
| 15 | The JSONBench query set does not port to unhinted JSON — `Dynamic` columns are refused by `GROUP BY` and by `IN`, and Q3 cannot execute without a cast | `note — workload, not toolbelt` / S3 | not actionable here; it bounds what the domain numbers can claim |

Retracted, and kept visible because the retraction is the more useful record:

- **[missing leeway-read-access-codegen → proposed:leeway-sql-read-access /
  performance-efficiency.time-behaviour / S1]** — filed after the 1M run as
  "resolving a value by path in SQL has no accelerated form". The claim that
  nothing equivalent exists is **wrong**: a `MATERIALIZED` column resolving the
  path at merge time is exactly that, and it took arm B's Q1 from 0.073 s /
  194 MB to 0.008 s / 4.7 MB. Refiled narrower as row 5 above. This was the
  trial's largest single error and it inflated the headline for a full run.

Positive maturity — competences the runs leaned on successfully, which the
convention asks for in their own right:

- **`leeway-dml-codegen`** carried an ingestion shape it was not designed for
  — arbitrary shredded JSON, 121.2M attributes across five sections at the 10M
  tier — at 30,252 docs/s in 384 MB of RSS, with no generated-code changes and
  no escape hatch.
- **`leeway-ddl-codegen`** composed the benchmark table straight from
  `chstore.ComposeSetupSQL`, so arm B is provably the live store's own DDL
  rather than a hand-copied approximation.
- **ADR-0162 `chpack`** installed cleanly on a live 26.7 server and, with the
  read-back family, expressed all five queries with no escape hatch.
- The resulting facts table is **smaller on disk** than ClickHouse's own JSON
  type holding the same corpus.

**Where the open rows go.** Rows 5–8 are one cluster — the SQL read surface is
unversioned, undiscoverable and ungenerated — and are carried by
[ADR-0171](../../adr/0171-leeway-sql-read-surface.md). Rows 12 and 13 were
localized defects that the trigger list puts in neither ADR tier, and both were
**fixed on 2026-08-06**, each with a regression test naming this ledger row.
Rows 1, 3 and 4 change the facts schema or add a package, so they need a design
dialogue before an ADR, and none is opened by this trial.

Row 12's fix is worth a line, because the re-check changed what it was: not a
missing grammar rule but a misplaced one. `paramSlot` existed and was reachable
only from `columnExpr`, which is why `{tier:String}` parsed and
`{db:Identifier}` did not. Admitting it in table position also required
grammar2 — a slot has no canonical form to be rewritten into, so
`ValidateGrammar2` would otherwise reject any normalised query parameterised on
its database — and nil-guarding three call sites that had been dereferencing
`tableIdentifier.Identifier()` unconditionally.

## 7a Results so far

**Latest — 2026-08-06, M4 at the 10M tier, arms A–E, cold runs measured**
([logbook](./logbook.md),
[`runs/2026-08-06-m4-10m/`](./runs/2026-08-06-m4-10m/)). All four arms hold
9,999,994 documents and return byte-identical results.

Three references are reported (§4), because the answer depends on which one a
general fact store should be held to:

| Reference | Declares | Facts + materialized backbone vs it |
| --- | --- | --- |
| A — the benchmark's entry | 5 typed paths + a clustered index on them | 1.03–1.4× slower |
| A0 — index removed | 5 typed paths | 0.72–1.09× — parity |
| **A00 — nothing declared** | — | **0.28–0.69× — 1.4–3.5× faster** |

Only A00 is a shape a store holding a mixture of document shapes could
actually have — and **the benchmark's own queries do not run against it**
(`Dynamic` columns are refused by `GROUP BY` and by `IN`; Q3 cannot execute
without a cast). That is the sharpest evidence that this workload is not posed
for high-variability JSON.

Against **arm A0** (§4):

- **Storage: facts is 0.807× unindexed native JSON**; 0.958× with the backbone
  materialized. The 1M tier's 1.44× *inverts* at scale.
- **Latency with the five backbone paths materialized (arm D): 0.72–1.09×** —
  parity, and faster on the two `did`-grouped queries. **Memory 0.99–1.15×.**
  On a like-for-like key the facts model is at parity with ClickHouse's native
  JSON type.
- **Without materialization (arm B): 8.0–12.8×.** This is the whole remaining
  gap, and it is the read path, not the model. §4's hypothesis holds at 10M —
  arm B reads every granule on every query.
- **Arm C prunes 2 granules of 2394.** Two tiers, same verdict: data-skipping
  indices over section value lanes cannot serve this workload.
- **Re-keying is the second lever, and it is free (arm E).** Arm D still reads
  every granule; arm E is arm D sorted on the materialized backbone and prunes
  227 of 2,389 on Q4/Q5 — the same ~10× reduction arm A gets. Over identical
  data the two levers isolate cleanly: **materialization is worth 3.8–13.8×**
  at +18.8 % storage, **re-keying a further 1.07–1.76×** while *saving* 9.3 %.
  Together they put facts at **0.59–1.18× of the benchmark's own entry**,
  faster on Q4 and Q5, at 0.954× its storage.
- **The 100M gate passes** (§9 Q6): ~77 GiB against 262 GiB free, ~2 h wall
  clock, dominated by the single-process facts ingest. **The tier was
  descoped rather than run** — §8 M4 says what that leaves unmeasured.
- **Results are readable as data**: the **`jsonbench` book**
  ([`apps/sqlapplet/bookjsonbench/`](../../../apps/sqlapplet/bookjsonbench/))
  carries the 10M summary in its pages, so it answers without anything having
  been loaded first. `jsonbench results` additionally lands a run directory's
  full per-try set in a facts table for ad-hoc querying.

The facts data model costs very little here. Almost everything the first run
attributed to it belonged to how the queries were written and to the absence
of a workload-shaped key — both fixable without touching the model.

### The 1M run, and why its numbers no longer stand

First run 2026-08-05 (M0–M3, 1M tier) — full entry in the
[logbook](./logbook.md), evidence in
[`runs/2026-08-05-m0-m3-1m/`](./runs/2026-08-05-m0-m3-1m/). **Its facts-arm
figures are superseded**: the queries open-coded the lane arithmetic (~3×
slower than the leeway query vocabulary), the run had no cold column, and 1M
proved too small a tier for the storage comparison to mean anything. Arm A's
numbers and the qualitative findings stand; the ratios below do not.

All three arms return byte-identical results for all five queries. Against
ClickHouse's native JSON type on the same box, holding the corpus in the facts
model cost **1.44× storage, 9.2–15.3× hot latency, and 5.6–76× peak query
memory** at 1M. The §4 hypothesis is **confirmed**: on the unmodified facts
table every query reads every granule. Arm C's data-skipping indices are
built and used by the planner but prune 1 granule of 245, and an on/off A/B on
the same table shows no runtime difference — **arm C closes none of the gap**,
because granule-level pruning cannot work on membership set semantics unless
the rows are clustered by the filtered value.

A same-day decomposition
([`diagnostics.md`](./runs/2026-08-05-m0-m3-1m/diagnostics.md)) then relocated
most of that tax. The leeway membership machinery is **8 %** of arm B's disk
footprint; 81.5 % is the shredded values. And the latency and memory columns
are dominated by resolving a value by its path *in SQL* — on Q1 that
reconstruction alone costs 7× the time and 8× the memory, and removing it puts
arm B within **2.4×** of arm A rather than 13.8×. The gap is mostly in the read
path, not the data model.

Two corrections to this protocol that the first run forced:

- **§8 M0's "vendor" step is not performed and should not be.** JSONBench is
  CC BY-NC-SA 4.0 and this repository is MIT. The step is satisfied by a
  commit pin plus SHA-256 verification instead — see
  [`upstream/PIN.md`](./upstream/PIN.md).
- **§8 M6 (arm D) is no longer optional-on-an-unexplained-gap.** The B/C gap
  is explained, and the explanation is an argument for re-keying, so arm D is
  the natural next experiment rather than a contingency.

## 8 Milestone cut (each descope-able)

- **M0 — vendor + pin.** Pin an upstream commit; vendor the ClickHouse DDL,
  queries, and run discipline; download the 1M tier; run arm A locally.
  Gate: arm A's relative query ordering is consistent with published
  results (sanity check, not absolute-number reproduction).
- **M1 — mapping + ingest.** Settle the §5 shape (short dialogue); leeway
  map + RowDML ingest of the 1M tier; ingest metrics recorded.
- **M2 — arm B.** Translate Q1–Q5 to facts shape through the native query
  path; run at 1M; collect pruning evidence; confirm or refute the §4
  hypothesis.
- **M3 — arm C.** Add skipping indices; re-run; attribute the delta.
- **M4 — scale.** 10M, then 100M behind the gate; repeat arms A–C.
  **10M done; 100M descoped 2026-08-06.** The gate passes on both counts
  (~77 GiB against 262 GiB free, ~2 h wall clock), so this is a judgement
  about value, not feasibility: the trial's primary question — how the
  toolbelt carries the workload — was answered at 10M, and arm E spent the
  last untried lever. What 100M would still have bought is the scaling
  direction of two ratios that moved between 1M and 10M: storage, which
  *inverted* from 1.44× to 0.807×, and arm A's pruning advantage, which
  widens with scale. Both are reported at one tier only, and any claim about
  where they go next is extrapolation. Re-running is a matter of raising
  `TIER`; the ingest is single-process and is the thing to parallelise first.
- **M5 — results + write-up.** Results-as-facts, the book applet, findings
  filed; this page gains a Results section and a pointer to whatever ADR
  the numbers end up informing. **Done** — §7a and §7b, the `jsonbench` book,
  and [ADR-0171](../../adr/0171-leeway-sql-read-surface.md), which carries the
  read-surface cluster.
- **M6 — arm D** *(optional)*, only on an unexplained B/C gap. **Done, and
  split in two.** The gap was explained rather than unexplained, and the
  explanation argued for re-keying, so the arm ran: arm D is the read-path
  half (materialize the backbone), arm E the key half (sort on it). They are
  independent and additive, which is why they are separate arms.

**Closed 2026-08-06, and not retired.** Every milestone is either done or
descoped above, so the trial has no open work; the findings it produced are
rolled up in §7b and the read-surface cluster is carried by ADR-0171. The
protocol stays here rather than moving to
[`doc/adr-background-work/`](../../adr-background-work/) because re-running it
is the point: it is the quality practice's first task-level probe, and the
thing most worth measuring on a later build is whether §7b's open rows have
moved. A re-run should read §7a's comparability notes first — the facts arms'
numbers are not comparable across the two runs already recorded.

## 9 Open questions

1. **Read discipline for the facts arms.** Workingset/argMax semantics
   ([ADR-0148](../../adr/0148-app-workingsets.md)) like every other boxer
   read, or append-only raw reads on the grounds that events are immutable?
   Whichever is chosen defines "the" facts number; running both would
   itself measure the versioning overhead.
2. **Mapping shape** — §5 options 1/2/3, and whether depth-beyond-backbone
   is in scope at all for the first pass.
3. **Identity isolation.** Mint DID subjects through the real identity path
   inside the benchmark-local database, or a benchmark-local id space?
   Either way the shared production store must not absorb benchmark
   subjects.
4. **Cold-run mechanics** on a shared workstation (cache-drop needs
   privileges; upstream's exact procedure), and whether hot = min or
   median of N.
5. **Raw staging.** Ad-hoc dataset store
   ([ADR-0134](../../adr/0134-adhoc-datasets.md)) vs. plain files at the edge
   — acquisition sits outside the native-idiom boundary, but where the
   bytes rest during ingest is a design choice.
6. **The 100M gate.** Disk and wall-clock budget on the target machine,
   extrapolated from 10M actuals before committing.

Related: [pprof-profiles-as-data](../../adr-background-work/pprof-profiles-as-data.md),
[ADR-0169](../../adr/0169-continuous-coverage-keelson.md),
[ADR-0109](../../adr/0109-leeway-marshall-multi-membership-ref-tuples.md).
