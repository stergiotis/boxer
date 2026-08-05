---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** A measurement *plan*, not a result:
> every number below is either quoted from upstream or marked "to measure".
> Do not cite as authoritative.

> **Provenance.** Compiled 2026-08-05, ahead of any measurement. Claims are
> two-tiered: (a) statements about this repository were checked against the
> working tree on the compile date (paths cited); (b) statements about
> JSONBench were taken from jsonbench.com and the `ClickHouse/JSONBench`
> GitHub repository (README and `clickhouse/queries.sql`) as fetched on the
> compile date — the benchmark evolves, so M0 pins an upstream commit and
> re-verifies everything quoted here against it.

# JSONBench on `boxer.facts` — measuring the data-centricity tax

## 1 Question and scope

What does holding the JSONBench Bluesky corpus in the `boxer.facts` data
model cost, against ClickHouse's native JSON type on the same hardware and
engine version — in storage footprint, index effectiveness, and query
latency, with and without secondary indices?

This page is a trial protocol (see the [directory convention](./README.md)):
the first task-level probe of the quality practice — an external, published
workload attempted with native idioms only, with friction filed as findings
rather than worked around. The finding fact family and its ISO 25010
classification are a forthcoming ADR; this page does not depend on it beyond
the ledger convention in §7.

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
([`chstore.go`](../../public/keelson/runtime/factsstore/chstore/chstore.go),
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
| B | facts as-is: `ORDER BY ts`, no secondary indices | unindexed |
| C | B + data-skipping indices on kind / collection / subject columns | non-clustered |
| D | *(optional)* facts clone re-keyed for the workload | clustered |

Arm D exists to separate "wrong key" from "wrong model" and runs only if
B and C leave an unexplained gap.

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

Ingest rides the existing lanes: RowDML native ingestion
([ADR-0089](../adr/0089-rowdml-serialization-clickhouse-native-ingestion.md)),
with raw JSON staged per §9 Q5. Subject identity for DIDs — millions of
distinct users at the 100M tier — exercises the identity path
([ADR-0106](../adr/0106-identity-fibonacci-tags-build-tag-retirement.md))
at a cardinality it has not seen; ingest wall-clock and minting cost are
recorded results, not incidental.

## 6 Method

- **Environment.** The repo-pinned ClickHouse server version; hardware,
  server TZ, and disk recorded at run time. Cold/hot procedure vendored
  from the upstream run scripts at M0.
- **Tier ladder.** 1M as smoke, 10M as the development tier, 100M as the
  real run — each tier gated on the previous one's disk and wall-clock
  actuals (§9 Q6). 1B is descoped outright.
- **Per query, per arm:** cold and hot runtimes, memory,
  `EXPLAIN indexes=1` pruning evidence (parts and marks read vs. total).
- **Per arm:** data size, index size (from `system.parts` /
  `system.data_skipping_indices`), ingest wall-clock and peak memory.
- **Idiom rule.** The facts arms' queries run through the native path —
  play/sqlapplet, grammar1 parse, canonicalize — exactly as any boxer
  workload would. Arm A may use `clickhouse-client` directly: it is the
  reference being measured against, not the idiom under test.
- **Reporting.** Results land as facts and are read back through a results
  book applet ([ADR-0132](../adr/0132-sqlapplet-sql-defined-applets.md)) —
  the benchmark dogfoods the reporting layer it is measuring.

## 7 Findings ledger

Friction encountered while executing this plan is filed as findings —
competence (per the corpus of
[ADR-0168](../adr/0168-capmap-business-capability-corpus.md)) × quality
characteristic — rather than silently worked around. Pre-registered
candidates, so later readers can tell hypotheses from surprises: the Q3
timezone dependency; grammar coverage of `IN [..]` array literals,
`date_diff`, and `::String` casts; identity-minting throughput at DID
cardinality; ingest-lane throughput at the 100M tier.

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
- **M5 — results + write-up.** Results-as-facts, the book applet, findings
  filed; this page gains a Results section and a pointer to whatever ADR
  the numbers end up informing.
- **M6 — arm D** *(optional)*, only on an unexplained B/C gap.

## 9 Open questions

1. **Read discipline for the facts arms.** Workingset/argMax semantics
   ([ADR-0148](../adr/0148-app-workingsets.md)) like every other boxer
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
   ([ADR-0134](../adr/0134-adhoc-datasets.md)) vs. plain files at the edge
   — acquisition sits outside the native-idiom boundary, but where the
   bytes rest during ingest is a design choice.
6. **The 100M gate.** Disk and wall-clock budget on the target machine,
   extrapolated from 10M actuals before committing.

Related: [pprof-profiles-as-data](../adr-background-work/pprof-profiles-as-data.md),
[ADR-0169](../adr/0169-continuous-coverage-keelson.md),
[ADR-0109](../adr/0109-leeway-marshall-multi-membership-ref-tuples.md).
