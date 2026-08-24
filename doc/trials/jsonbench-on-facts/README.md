---
type: explanation
audience: package maintainer
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-24
---

> **Compacted 2026-08-24.** The measurement work closed on 2026-08-06 (§8).
> This page was merged from a 2,600-line dossier — protocol, logbook
> narrative, four per-run results documents, a verdict and a side-experiment —
> because the numbers in it were being quoted without the conditions that make
> them true. The full pre-compaction record is at
> `45b9c1b8:doc/trials/jsonbench-on-facts/`. §0 is the part meant to be
> quoted.

> **Provenance.** Compiled 2026-08-05, ahead of any measurement; results
> merged in on 2026-08-24. Claims about this repository were checked against
> the working tree on their date, paths cited. Claims about JSONBench are
> against the pinned upstream commit ([`upstream/PIN.md`](./upstream/PIN.md)),
> not against the live site, which evolves.

# JSONBench on `boxer.facts` — a toolbelt trial, and the data-centricity tax

## 0 The claim, and how to cite it

**Held to a reference that declares the same schema knowledge, the
`boxer.facts` data model is at parity with ClickHouse's native `JSON` type.**
At the 10M tier, with the five backbone paths materialized (arm D), facts runs
the benchmark's five queries at **0.72–1.09× the latency** of native JSON
declaring the same five typed paths and no index (arm A0), at 0.99–1.15× its
peak memory and 0.958× its storage.

The condition is the claim. Parity holds when both sides read comparable
bytes, and every figure in this trial moves with what the reference is allowed
to declare:

| The reference declares | Facts + materialized backbone, against it |
| --- | --- |
| five typed paths **and a clustered index on exactly the five columns the queries touch** (arm A — the benchmark's entry) | 1.03–1.38× slower |
| five typed paths, no index (arm A0) | **0.72–1.09× — parity**, and faster on the two `did`-grouped queries |
| nothing — plain `JSON`, engine defaults (arm A00) | 0.28–0.69× — 1.4–3.5× faster, at 1.51× the storage |

Only A00 is a shape a store holding a mixture of document shapes could
actually have, and **the benchmark's own queries do not run against it** (§5).

**What this trial does not say.**

- **Not "the facts model is 5–16× slower than native JSON."** That is arm B,
  which measures *this trial's own read path* — open-coded lane arithmetic and
  a per-row path reconstruction — not the data model. Fixing how the query was
  written removed all of it (§5, and finding 5 in §7b).
- **Not "leeway is 1.4–3.5× faster than a JSON column."** That is against arm
  A00, whose queries needed 19 added casts to execute at all, and it is bought
  with 1.51× the storage.
- **Not a JSONBench result.** Mapping through leeway is schema-on-write, which
  violates the benchmark's stay-JSON rule. No arm here is leaderboard-eligible
  and none is comparable with published JSONBench numbers except arm A, which
  is the pinned DDL byte for byte.
- **Not reviewed, and not replicated.** One shared workstation, one run per
  configuration, one corpus, and the 10M tier as the only tier whose numbers
  mean anything. The operator was a confounder: four of the trial's own errors
  dominated its measurements, each found only because a reviewer pushed back
  (§5). A different operator would have produced different numbers from the
  same code.

**If you need a number**, take it from the `jsonbench` book — the committed
10M summary in
[`apps/sqlapplet/bookjsonbench/`](../../../apps/sqlapplet/bookjsonbench/),
whose pages hold raw seconds and bytes and compute ratios against the
reference arm you choose. The `runs/` directories hold the per-try evidence.
**No figure from this trial travels without the pair of arms it compares** —
that is the mis-citation this page was compacted to stop.

## 1 Question and scope

Two questions, equally weighted:

1. **The trial proper.** How well does the native toolbelt carry a recreation
   of JSONBench — which gaps and frictions surface, and how short and legible
   is the solution? Solution artifacts are committed and counted; every escape
   hatch is a finding, not a fix.
2. **The side-product, co-equal here.** What does holding the JSONBench
   Bluesky corpus in the `boxer.facts` data model cost against ClickHouse's
   native JSON type on the same hardware and engine — the **data-centricity
   tax**, defined as the deltas in size, latency and memory between a
   native-JSON reference and the facts arms, plus how much of the gap each
   indexing step closes.

Because a facts entry can never appear on the public leaderboard (§0), what is
reused is the dataset, the query intents and the measurement discipline; what
replaces the leaderboard is a local, same-hardware comparison against three
references (§3).

In scope: the mapping design space, the arm ladder, the tier ladder, results
as facts with a book applet over them. Out of scope: leaderboard eligibility,
the 1B tier, systems other than ClickHouse, and mutation workloads — the
upstream benchmark is read-only analytics.

This is a trial protocol (see the [directory convention](../README.md)); runs
append to the [logbook](./logbook.md).

## 2 The workload

Pinned upstream, verified against the commit in
[`upstream/PIN.md`](./upstream/PIN.md) rather than against the live site.

- **Dataset.** Bluesky Jetstream events, one JSON document per event. Tiers of
  1M / 10M / 100M / 1B; the full corpus is quoted upstream at 125 GB
  compressed, 425 GB raw.
- **Rules.** Analytics over documents that remain JSON — no relational
  schema-on-write; indexes explicitly allowed, clustered and secondary;
  otherwise default settings.
- **Metrics.** Data and index size after load; per-query cold and hot
  runtimes; memory; physical plans.
- **The five queries:**

  | # | Intent | Shape |
  | --- | --- | --- |
  | Q1 | event counts by collection | unfiltered GROUP BY `commit.collection` |
  | Q2 | counts + distinct users per collection | filter `kind='commit' ∧ operation='create'`, `uniqExact(did)` |
  | Q3 | hour-of-day histogram for post/repost/like | `toHour(fromUnixTimestamp64Micro(time_us))`, IN-list on collection |
  | Q4 | three earliest posters | GROUP BY `did`, `min(ts)`, LIMIT 3 |
  | Q5 | three longest activity spans | GROUP BY `did`, `date_diff(max,min)`, LIMIT 3 |

**Nothing upstream is vendored.** JSONBench is CC BY-NC-SA 4.0 and this
repository is MIT, so the protocol's original "vendor the DDL, queries and run
discipline" step is satisfied by a commit pin plus SHA-256 verification
([`upstream/fetch-pin.sh`](./upstream/fetch-pin.sh)) instead.

**Two portability facts, pre-registered and both confirmed.** `toHour()` is
server-timezone-dependent, so Q3's *result rows* do not line up with
upstream's published ones on a server that is not UTC — runtimes and cross-arm
comparisons are unaffected, since every arm runs on the same server. And the
queries use ClickHouse-idiom forms (`IN [..]` array literals, `::String`
casts) that the facts arms translate through the native query path (§4).

## 3 Arms — and what each one is allowed to declare

The arm's declarations are what its numbers mean, so they are stated here
rather than in a footnote. A ratio between two arms is only interpretable
alongside this table.

| Arm | Table | Declares | Read path |
| --- | --- | --- | --- |
| A | ClickHouse `JSON`, pinned upstream DDL | five typed paths, `max_dynamic_paths = 0`, **clustered on `kind, operation, collection, did, ts`** | typed subcolumns |
| A0 | the same DDL, `ORDER BY tuple()` | five typed paths | typed subcolumns |
| A00 | plain `JSON`, engine defaults, `ORDER BY tuple()` | nothing | engine-discovered subcolumns |
| B | `boxer.facts` DDL as the live store composes it, `ORDER BY ts` | nothing workload-specific | value-by-tag over lanes |
| C | arm B + data-skipping indices on kind / collection / subject | — | value-by-tag + a redundant `has()` guard |
| D | arm B + five `MATERIALIZED` backbone columns | the five paths, as columns | plain columns |
| E | arm D re-keyed on those columns in the reference's order | the five paths **and a clustered index on them** | plain columns |
| J | `mapping.LoadJsonMapping` — the *canonical* leeway JSON mapping, not facts | nothing | `value[indexOf(lmv, path)]` |

**A facts table cannot have arm A's key.** It holds a mixture of document
shapes, so most rows would carry none of those five paths; ordering by them is
available to a single-schema benchmark entry and not to a general store.
Comparing facts against arm A therefore charges the data model for the
benchmark's homogeneity — which is why A0 and A00 exist, and why every ratio
recorded before they did silently included the index advantage.

**Arm B is the live store's own shape.** The benchmark table comes from
`chstore.ComposeSetupSQL`, so arm B is provably the DDL the store declares
rather than an approximation. Its key is `ORDER BY ts` because latest-state
readback sorts by `ts`; Q1–Q5 group and filter by collection and user, not
time.

**Standing hypothesis (§4 of the original protocol), confirmed at both
tiers:** on the unmodified facts table the primary index prunes nothing —
every query reads every granule.

The benchmark database is a **separate ClickHouse database using the facts
DDL**, not the live store instance: "facts holding the data" means the model,
not the production tables.

## 4 Method

- **Environment.** Captured per run in `runs/<run>/environment.md` — CPU,
  memory, storage class, engine version, server timezone, and the settings the
  pinned DDL asks for. No hostnames or personal paths.
- **Tier ladder.** 1M as smoke, 10M as the development tier, 100M as the real
  run, each gated on the previous tier's disk and wall-clock actuals. 1B
  descoped outright; **100M descoped for arms A–E** (§8) and run only for arm
  J.
- **Per query, per arm:** cold and hot runtimes, memory, and
  `EXPLAIN indexes=1` pruning evidence. Cold is try 1 after a page-cache drop,
  hot is `min(try 2, try 3)` — the reduction upstream's own site applies. The
  1M run has **no cold column** (the cache drop needed a privilege that was
  not yet granted), so it is not comparable with later runs on that axis.
- **Per arm:** data size, index size, ingest wall clock and peak memory.
- **Idiom rule.** The facts arms' queries run through the native path —
  play/sqlapplet, grammar1 parse, canonicalize — exactly as any boxer workload
  would. Arm A may use `clickhouse-client` directly: it is the reference, not
  the idiom under test.
- **Process measurement.** The solution is a result: artifacts are committed,
  their size recorded, and manual interventions counted — an escape hatch is
  by definition a finding.
- **Reporting.** Domain results land as facts and are read back through the
  `jsonbench` book applet, so the benchmark dogfoods the reporting layer it is
  measuring. Every run appends a [logbook](./logbook.md) entry.
- **Correctness gate.** Every arm's five result sets are diffed against arm
  A's. All arms at all tiers returned byte-identical results, with Q3's
  timezone caveat applying equally to all of them.

**Teardown.** Each arm script drops its own database before rebuilding, so
re-running one arm needs no cleanup; nothing sweeps them all. To reclaim, list
first and drop second:

```sh
clickhouse-client -q "SELECT name FROM system.databases WHERE name LIKE 'jsonbench%' ORDER BY name"

clickhouse-client -q "SELECT name FROM system.databases WHERE name LIKE 'jsonbench%'" \
  | xargs -r -I{} clickhouse-client -q "DROP DATABASE IF EXISTS \`{}\`"
```

Dropping them loses nothing reproducible: the run directories are the numbers
of record, the arm scripts rebuild from the raw corpus, and the book carries
its own committed summary. **The pattern deliberately does not catch the
UDFs** — `chpack` and the `LW_*` read-back family install server-wide and are
a shipped repo capability ([ADR-0162](../../adr/0162-leeway-co-ragged-function-pack.md))
that other work may be using.

## 5 What the runs found

Numbers live as data, not prose: latency and memory in
[`jb-latency`](../../../apps/sqlapplet/bookjsonbench/jb-latency.md), sizes in
[`jb-sizes`](../../../apps/sqlapplet/bookjsonbench/jb-sizes.md), per-try
evidence under `runs/`. What follows is what the runs established, with the
arms and the tier named wherever a ratio appears.

**The data model is not what costs.** Every expensive thing this trial
measured turned out to be a capability that already existed in the repository
and that the trial failed to find. Arm B's figure moved four times under
review, and not once because the model was slow:

| What was wrong | What it cost |
| --- | --- |
| Open-coded lane arithmetic instead of the leeway query vocabulary | ~3× |
| Path resolved per row instead of a `MATERIALIZED` column | 7× time, 8× memory |
| Compared against a reference carrying schema knowledge facts cannot have | 1.02–1.73× latency, 9.8 % storage |
| Hand-rolled ragged read instead of `LW_LIST_BY_TAG_EQUAL` | up to 2.4× (Q2 −57.9 %) |

`chpack` had shipped and was not installed on the trial server.
`LW_LIST_BY_TAG_EQUAL` did precisely what the trial wrote by hand, and did it
more correctly — the hand-rolled form silently took the first element of a
returned slice. The materialized-column pattern was already in use in a
sibling experiment. **So the verdict on the toolbelt is not "slow" — it is
"undiscoverable from where a task-level consumer stands"** (§7b finding 6,
filed three times, and the reason [ADR-0171](../../adr/0171-leeway-sql-read-surface.md)
exists). It took four wrong headlines to surface it.

**Two levers, independent and additive, isolated over identical data at 10M.**
Materializing the five backbone paths is worth **3.8–13.8×** and costs +18.8 %
storage, four fifths of it `did`; it buys nothing structural — arm D still
reads every granule — it just replaces a per-row path reconstruction with a
column read. Re-keying on those columns is worth a further **1.07–1.76×** and
*saves* 9.3 % storage, because sorting low-cardinality columns compresses
better; it is the only lever here free in both directions. The gain lands
exactly where pruning lands — Q4/Q5, 227 granules of 2,389 against arm D's
2,393 of 2,393 — and is absent on unfiltered Q1, which is the attribution
working. Together they take arm B's 4.5–24.3× off the table and put facts at
**0.59–1.18× of the benchmark's own entry** (arm E against arm A), faster on
Q4 and Q5, at 0.954× its storage.

**Two things did not work.** `ORDER BY ts` prunes nothing — §3's hypothesis,
confirmed at 1M and 10M, every query reading every granule. And
**data-skipping indices recover nothing**: arm C prunes 2 granules of 2,394,
the same verdict at both tiers, because a bloom filter over a section's value
lane answers "does this granule contain the value anywhere" and at 8,192 rows
per granule the answer is almost always yes. Granule-level pruning cannot
serve membership set semantics unless the rows are clustered by the filtered
value — which is arm E.

**Storage is not a cost of the facts model at this scale; it is a saving.**
Arm B against arm A went from **1.44× at 1M to 0.886× at 10M** — the shred
carries ~4× the uncompressed bytes but compresses far harder, as its support
lanes (`*card`, `len`) and dictionary-encoded membership lanes amortise across
ten times the rows, while arm A's JSON shared-data overhead does not shrink
the same way. 1M was too small a tier for that to show. Both this inversion
and arm A's pruning advantage (which widens with scale) are **single-tier
results**; any claim about where they go next is extrapolation.

**The pinned reference DDL is a storage pessimisation on this corpus.** A00 is
the smallest table of any arm — 1,150,367,898 B against A0's 1,814,273,851 —
so `max_dynamic_paths = 0` costs 37 % over letting the engine discover and
type its own paths.

**The benchmark may be the wrong instrument for a heterogeneous store.** Its
own queries do not run against unhinted JSON: every `data.<path>` is
`Dynamic`, ClickHouse refuses `GROUP BY` on one (relaxable with
`allow_suspicious_types_in_group_by=1`) and refuses `IN [...]` outright with
no setting to relax, so Q3 cannot execute. Getting A00 numbers required 19
explicit casts, derived from the pinned file by
[`queries-native-dynamic.sh`](./queries-native-dynamic.sh) so the delta stays
auditable. That is a property of the workload, not of any system measured
here, and it bounds what the domain numbers can claim.

**Ingest.** At 10M, arm A loads in 41.2 s (~243k docs/s, server-side parse,
raw bytes shipped) against arm B's 330.6 s (30,252 docs/s in 384 MB of RSS,
121,205,987 attributes) — not like-for-like, and the facts side is a single Go
process doing JSON decode plus Arrow building. Both arms skip the same 6
malformed documents: the corpus contains JSON-lines records truncated
mid-string at exactly 65,536 bytes with the remainder on the next physical
line, and the facts ingester needed the same tolerance upstream documents
before the two arms held the same corpus at all. 10 JSON nulls were dropped —
facts has no `null` section (§7b finding 4). Sharding the ingest across 8
processes into one table (arm J, 100M) reached 297,619 docs/s aggregate, a
5.5× speed-up on 8 shards, which is what the 10M run named as the thing to
parallelise first.

**Solution size.** ~700 lines of Go and ~350 of harness for arms A–E, plus 599
hand-written lines for arm J over a generated builder. **Manual interventions:
one** — arm C's redundant `has()` conjunct, which is itself filed as the
reason arm C cannot work.

**What carried the workload without complaint.** `leeway-dml-codegen` took an
ingestion shape it was not designed for — arbitrary shredded JSON, 121.2M
attributes across five sections — with no generated-code changes and no escape
hatch. `chpack` installed cleanly on a live 26.7 server and, with the
read-back family, expressed all five queries with no escape hatch.

### 5a The canonical mapping (arm J), where facts is not the target

Arm J holds the same corpus under `mapping.LoadJsonMapping` — the canonical
leeway JSON mapping — rather than the facts schema. It is a **new arm, not a
re-run**: different schema, read vocabulary and sort key, so figures against
A/A0/A00/B are cross-tier from the 10M run (arm B was rebuilt for the
comparison and reproduced its recorded total to 0.26 %).

It is **1.5–2.0× faster than arm B** at 10M with no materialized columns, no
skipping indices, no re-keying and **no UDFs installed at all** — and that is
structural, not tuning. In facts a path resolution is
`LW_VALUE_BY_TAG_EQUAL` over `LW_RAGGED_PARENT_IDS`, plus a second cumulative
sum over `len` for the array-valued sections. Here every section is scalar and
one verbatim membership per attribute makes `lmv` co-index 1:1 with the value
lane, so `value[indexOf(lmv, '/commit/collection')]` is the entire vocabulary.
Findings 3, 4, 9 and 10 in §7b are structurally absent. Arm D still beats arm
J, which is expected rather than a counterpoint: materialization answers five
known paths, arm J answers all of them, and the same lever is available here
and was not pulled.

On storage at 10M, like-for-like: **0.915× the facts schema** and **1.061× of
unhinted native JSON** — with the row-identity column excluded, because the
A-family tables have no row identity at all (see the identity note below).
Materializing the backbone costs +18.1 % here against arm D's +18.8 %, and
settles a question the storage table invites: no, the mapping does not beat
native JSON on storage once the backbone is materialized (1.304× of A00 minus
identity, 1.582× as loaded).

**The addressing machinery is 2.3 % of the table.** At 100M — 1,200,650,881
attributes — every path, array coordinate and membership cardinality costs
~337 MiB of a 14.95 GB table; `mvhp` compresses 99–140× and `lmvcard`
216–283×. The recurring objection that a shredded representation is eaten by
its own addressing overhead does not survive measurement on this corpus. What
costs is the payload: `string:value` is 74.2 % of the table.

**Two measurement mistakes worth keeping, because both recurred.**

- **Row identity was charged against tables that have none.** The A-family is
  a single `data JSON` column with no row identity. Arm J's `id:blake3hash` is
  320,110,328 B at 10M — **20.3 % of the table** — and compresses at only 1.2×
  because a hash is incompressible by construction. It was also written at 32
  bytes where the facts arm writes 16, and the canonical type is unbounded, so
  the width is the writer's choice. **This was the second time the trial made
  this mistake**; the 1M diagnostics had already caught the facts arm's
  16-byte `id:naturalKey` at 8.6 % of arm B. Both figures are now reported
  as-loaded and minus-identity separately.
- **An omitted encoding hint is not a neutral default.** `LoadJsonMapping`
  declared no `AddColumnEncodingHints` on its `bool` / `int64` / `float64`
  value columns, so the generator emitted no `CODEC` clause at all and the
  column inherited server-default LZ4 while the *support* lane beside it in
  the same section got `T64, ZSTD(3)` from the membership machinery. Nothing
  in the mapping source made the asymmetry visible. On the real 10M `int64`
  lane, `DoubleDelta, ZSTD(3)` is **2.36× smaller** than the default (56.67 →
  23.98 MiB). Fixed, with the candidate table recorded in the mapping's doc
  comment. **The measurement contradicted the obvious argument, which is why
  it was worth making:** a leeway value lane is flattened across attributes,
  so consecutive elements are different fields — a microsecond timestamp
  beside an image height — which suggests delta encoding should be useless and
  a bit-plane transform should win. `T64` is in fact *worse* than plain
  `ZSTD(3)` here. The float hint remains **unvalidated**: this corpus is
  essentially float-free and `FPC(12)` / `Gorilla` / `ZSTD(3)` returned
  byte-identical sizes. One corpus is not a proof for `int64` either — these
  integers are dominated by a per-document timestamp.

**What the corpus itself turned out to be**, from the scenario queries the
benchmark's five cannot express (`queries-jsonmap-scenarios.sql`, at 100M):
3,197 distinct document shapes (1,216 at 10M — 10× the documents, 2.6× the
shapes), max 709 attributes, max depth 14; two genuinely polymorphic paths,
both under `skyfeedBuilder`, arriving as `string` in some documents and
`int64` in others; per-collection record schemas from 3 paths to 355, with two
fields at 0.0 % coverage — `/commit/record/type`, a typo for `$type`, and
`/commit/record/subject/quoteCount`. And **a path vocabulary that is not
finite**: the long tail holds paths like
`…/skeetsAppHistory/data/_/2024-11-21T16:31:48.241Z`, a client writing
timestamps as object keys, so the path space grows with the corpus. That is
the property a closed, registered path vocabulary cannot represent, and the
reason this mapping's memberships are verbatim rather than Ref-shaped (§7b
finding 3).

## 6 Where the path is in the data

A side-experiment, kept because the trial's own five queries cannot answer the
complementary question: **which queries are easier and cheaper on the leeway
shape than on a JSON column, and which are not.** JSONBench is posed for a
corpus whose schema you already know — every query names its paths — which is
the case a JSON column is built for and where §5 measures leeway as slower.

**The thesis, in one line: a JSON column is fast when the path is in the
query; a leeway table is fast when the path is in the data.** It is not a
claim that either representation dominates; §6b records where the JSON column
wins, on the same data in the same run.

### 6a The structural facts

Three properties, each verified against a live 26.7 server rather than read
off documentation.

**A JSON column cannot address a path known only at runtime.** ClickHouse can
*enumerate* paths — `JSONAllPaths`, `JSONAllPathsWithTypes`, and the
aggregates `distinctJSONPaths` / `distinctJSONPathsAndTypes` — so schema
discovery is expressible, and the naive claim that it is not would be wrong.
What is unavailable is using an enumerated path to read the column:

```text
SELECT data[JSONAllPaths(data)[1]] FROM bluesky
  → Code 43: First argument for function 'arrayElement' must be array, got 'JSON'

SELECT getSubcolumn(data, JSONAllPaths(data)[1]) FROM bluesky
  → Code 44: The second argument of function getSubcolumn should be a
             constant string with the name of a subcolumn
```

The only escape is `toString(data)` followed by `JSONExtract*` — re-serialising
each document back to JSON text and re-parsing it, per row, giving up columnar
storage entirely. Every query whose path set is data-dependent inherits that
cost. In a leeway table the path is an ordinary column value (`<section>:lmv`),
so a runtime path resolves with `indexOf` like any other array lookup and a
path *predicate* is just a predicate.

**A JSON column does not enumerate inside arrays.** `JSONAllPaths` stops at an
array — the array is a value, not a set of addresses. The leeway shred
addresses each element (`/commit/record/facets/_/features/_/$type`, with the
elided indices carried in `mvhp`), so element position is queryable without
naming the containing path. For a *known* path a JSON column indexes arrays
perfectly well and cheaply; the gap is only over paths not known in advance.
This also means the two systems do not enumerate the same path set, so path
*counts* are not comparable between them — runtimes are.

**The leeway lanes are narrow and the JSON column is not.** A path query on a
leeway table reads one dictionary-encoded `Array(LowCardinality(String))`
column; the equivalent on a JSON column materialises the object. The memory
column below shows it directly.

### 6b Measured, 10M tier, matched statement for statement

Both sides through the trial's own `measure.sh`, TRIES=3, cache dropped before
each query's tries. The leeway side is arm J (`ORDER BY tuple()`, fixed
codecs); the JSON side is **arm A00** — plain `JSON`, no hints, no index —
because that is the only variant a store holding a mixture of document shapes
could have, with its DDL taken verbatim from
[`runs/2026-08-06-m4-10m/arm-a00/ddl-as-applied.sql`](./runs/2026-08-06-m4-10m/arm-a00/ddl-as-applied.sql).
Neither table is indexed for the workload. Queries:
[`queries-usp-leeway.sql`](./queries-usp-leeway.sql) and
[`queries-usp-jsonv2.sql`](./queries-usp-jsonv2.sql). Evidence:
[`runs/2026-08-06-jsonmap-100m/usp/`](./runs/2026-08-06-jsonmap-100m/usp/).

| | Query | leeway (arm J) | plain `JSON` (arm A00) | ÷ leeway |
| --- | --- | --- | --- | --- |
| U1 | path census + per-path doc counts | **0.400 s** / 397 MB | 4.459 s / 4,198 MB | 11.1× |
| U2 | path × type census | **0.879 s** / 956 MB | 4.944 s / 4,208 MB | 5.6× |
| U3 | value anywhere, exact | **0.261 s** / 146 MB | 176.550 s / 4,395 MB | **676×** |
| U4 | subtree prefix census | **0.233 s** / 344 MB | 4.730 s / 4,192 MB | 20.3× |
| U5 | sum every integer, any path | **0.024 s** / 24 MB | *no expression exists* | — |
| U6 | leaf count per document † | **0.025 s** / 16 MB | 4.709 s / 4,182 MB | 188× |
| U7 | presence of one **constant** path | 0.041 s / 13 MB | **0.026 s** / 27 MB | 0.63× |
| U8 | numeric predicate over all int paths ‡ | **0.027 s** / 9 MB | 176.359 s / 4,405 MB | *not comparable* |
| U9 | array degree for every array path | **0.418 s** / 450 MB | *no expression exists* | — |

† Semantics differ: `JSONAllPaths` does not descend into arrays, so an array
counts once there and once per element here. Both answer "how wide is a
document" in their own vocabulary; only the runtimes are comparable.

‡ **Not the same question, and the JSON side is the easier one.** The leeway
form scans every integer-valued path; the JSON form names `time_us`, because
asking it over unknown paths would require running U2 first and emitting a
per-path disjunction. The 176 s is the cost of the text fallback on a *named*
path, so it understates the gap rather than inflating it.

**The gap tracks the mechanism, not the query.** U1/U2/U4/U6 are all "range
over paths", and all four cost the JSON column ~4.5–5 s and ~4.2 GB
regardless of what is asked, because each materialises the object to enumerate
it. The leeway side varies between 16 MB and 956 MB with the lane it actually
touches. The memory column is the clearer signal: flat at ~4.2 GB on one side,
spanning 60× on the other.

**U3 is structural, not a tuning case.** It is what the runtime-path
limitation costs when paid per row. And under stock server settings the query
does not merely take 176 s — it is **killed at 10 s** by `min_execution_speed`
(`Code 160: Query is executing too slow: 66158.668 rows/sec., minimum:
250000`) and returns nothing at all. The guard was relaxed to obtain a number.

**Two fairness controls, both found by getting a wrong answer first.**
`use_query_condition_cache=0`: with it on, the leeway value-anywhere query
reported 0.003 s / 170 KB on its second run — the cache replaying "no granule
matches", not work, and the honest number is 100× that. The JSON side could
not benefit, because its query never completed to populate a cache. And
`min_execution_speed=0`, above.

### 6c Where the JSON column wins

Three ways, none marginal.

**On the benchmark's own queries** — every path named in the query — the JSON
column is faster on all five even stripped of its hints and its index: arm J
is 1.14–2.53× of A00 at 10M. Add the hints and the clustered index back and
the gap widens to 5.0–15.9× of arm A. A typed subcolumn read beats an
`indexOf` over a path lane, and it should: the JSON column has already done at
write time the work leeway defers to read time.

**On storage**, at this tier, the JSON column is smaller — though by much less
than raw totals suggest, once row identity is excluded: arm J is **1.061× of
A00** minus identity against 1.339× as loaded, and it gets worse if the
backbone is materialized for the known-path workload (1.304× minus identity,
1.582× as loaded). So the honest figure is 6 % larger, not 37 % — but 6 %
larger is still larger, on every variant a heterogeneous store could actually
use. Of what remains, the addressing lanes are 2.3 % (§5a); the rest is
payload, where both sides store the same bytes near the entropy floor —
raising the string lane from `ZSTD(3)` to `ZSTD(12)` buys 4 % for a large CPU
cost, which is why it was left alone.

**On ingest**, at this tier, 77 s against 186 s per process. Not like-for-like
— the JSON side is ClickHouse's own multi-threaded server-side parser, the
leeway side a single Go process — and the leeway ingest parallelises across
files (297k docs/s on 8 shards at 100M). But as measured, per process, the
JSON column loads 2.4× faster.

**So the honest summary is a trade, not a win:** the leeway shape pays a
constant factor on known-path analytics and in storage, and buys the ability
to ask questions whose paths are not known when the query is written.

### 6d Threats to validity

- **One corpus.** Bluesky Jetstream is shallow-to-moderately-nested, heavily
  repetitive and string-dominated. A corpus with deep unique nesting, or one
  where most attributes are numeric, would move these numbers. Nothing here
  generalises to "JSON" as a category.
- **One tier, one machine, one run per configuration.** TRIES=3 controls
  short-term noise, not run-to-run drift on a shared workstation.
- **The leeway table's string/symbol split was sampled from one file** and
  pinned. A different split changes column widths and therefore timings.
- **The JSON side is A00, not A.** Against the benchmark's own hinted,
  clustered entry the leeway arm loses on the benchmark's own queries (§6c).
  A00 is the right control for *these* questions and the wrong one for those.
- **This is not a benchmark entry.** It is a pair of query sets chosen to
  probe a hypothesis formed before the measurements. The queries where the
  JSON column wins were added for the same reason and are reported in §6c
  rather than dropped.
- **The thesis rests on structural facts about one engine.** Whether it is
  about *leeway* or about *ClickHouse* is what
  [leeway on a second substrate](../leeway-second-substrate/README.md) exists
  to test.

## 7 Findings ledger

Friction encountered while executing this plan is filed as a finding rather
than silently worked around — competence slug × relation
(`missing` / `broken` / `pain`) × ISO 25010 characteristic, with severity and
evidence, per the classification scheme in the
[directory convention](../README.md). Competence slugs come from the corpus
vault ([ADR-0168](../../adr/0168-capmap-business-capability-corpus.md)); a
`missing` finding anchors at the nearest existing competence and proposes a
slug. Until the finding fact family lands, findings live in the
[logbook](./logbook.md) in the convention's line format, so later migration to
facts is mechanical.

### 7b The ledger, rolled up

One row per proximate obstacle, deduplicated across all runs, with the status
each carried when the trial closed on 2026-08-06. **Open / fixed was
re-checked against the tree at `b33bab3a`**, not carried over from the entry
that filed it.

| # | Finding | Class | Status at close |
| --- | --- | --- | --- |
| 1 | Nothing in-tree turns a JSON document into rows under `mapping.LoadJsonMapping`'s schema — every leeway ingestion here is hand-written per domain | `missing leeway → proposed:leeway-json-shredding` / functional-suitability.functional-completeness / S3 | **open** — `mapping/` still exports schema constructors only; needs a dialogue (new package) |
| 2 | The trial's shredder aborted the whole ingest on the first undecodable document, where the reference loader skips and continues | `broken leeway-dml-codegen → proposed:leeway-json-shredding` / reliability.fault-tolerance / S2 | fixed in the trial's own ingester; recurs in whatever (1) becomes |
| 3 | boxer.facts sections accept only Ref-shaped memberships; an open JSON corpus has no closed path vocabulary, so paths were demoted into the high-cardinality parameter channel | `broken leeway-dml-codegen` / functional-suitability.functional-appropriateness / S2 | **open** — facts schema change, Tier 1; needs a dialogue |
| 4 | boxer.facts has no `null` / `undefined` section, so JSON nulls cannot round-trip | `missing leeway → proposed:leeway-json-shredding` / functional-suitability.functional-completeness / S4 | **open** — verified: 21 tagged-value sections, none of them null. 10 nulls dropped at 10M |
| 5 | No tooling emits `MATERIALIZED` column definitions from a leeway schema, so a SQL consumer hand-writes them per path and keeps them in sync with physical column names | `missing leeway-ddl-codegen → proposed:leeway-sql-materialized-projections` / functional-suitability.functional-completeness / S3 | **open** — verified: no `MATERIALIZED` anywhere under `leeway/`. This is the lever worth 3.8–13.8× |
| 6 | Nothing a task-level consumer walks — the leeway skills, `mapping`, the generated DML/RA packages — points at the SQL query vocabulary | `pain leeway → proposed:leeway-query-vocabulary-discoverability` / usability.user-error-protection / S2 | **open** — verified: zero mentions of `chpack` or the `LW_*` family across all three leeway skills. **Filed three times** (chpack, the read-back family, ADR-0116 column handles) |
| 7 | The read-back UDF family carries no version marker, and every statement is `CREATE OR REPLACE`, so nothing detects or removes a retired function | `broken leeway-ddl-codegen → proposed:leeway-udf-provisioning-drift` / reliability.maturity / S3 | **open** — verified: `chpack` has `Version`/`LW_PACK_VERSION`, the read-back family has neither. Partially addressed 2026-08-07: `chpack.Install` now drops an append-only list of withdrawn names, so a *known* retired function no longer survives a rename. Detection is still absent (ADR-0171 §SD2) |
| 8 | A Ref-membership table cannot be read by anyone who does not already hold the registry — there is no server-side name→id lookup, so ids ride SQL pages as uint64 literals | `pain leeway-ddl-codegen → proposed:leeway-vocab-introspection` / usability.self-descriptiveness / S3 | **open** — verified: no vocabulary table is reachable from SQL |
| 9 | Resolving a value by path in SQL needs two independent cumulative-sum reconstructions, one over `lmrcard` and one over `len` | `pain leeway-read-access-codegen` / performance-efficiency.resource-utilisation / S3 | **halved on review** — only the `len` half is intrinsic to facts; the other half was this trial's own encoding choice |
| 10 | Getting that second reconstruction wrong fails *silently* on this corpus — every attribute has `len = 1`, so a naive co-index returns correct answers here and wrong ones on any multi-element array | `pain leeway-read-access-codegen` / functional-suitability.functional-correctness / S2 | **open as a hazard**; `LW_LIST_BY_TAG_EQUAL` is the form that does not have it |
| 11 | The facts DDL cannot be applied, or the table cloned, without `allow_suspicious_low_cardinality_types=1` | `pain leeway-ddl-codegen` / usability.operability / S4 | **open** — every client invocation in the harness carries the flag |
| 12 | `FROM {db:Identifier}.facts` fails grammar1 with *no viable alternative*, so an applet carrying it never mounts; ClickHouse itself accepts the form | `missing nanopass-pass-pipeline → proposed:grammar1-identifier-params` / functional-suitability.functional-completeness / S3 | **fixed 2026-08-06** — `paramSlot` was an alternative of `columnExpr` only; it is now also one of `tableIdentifier` / `databaseIdentifier` in grammar1 **and** grammar2 |
| 13 | A column handle bound by a `WITH <handle> AS alias` expression alias is not resolved — the pass visits a SELECT's own scope but not the WITH-expression clause | `missing nanopass-scope-resolution → proposed:resolve-column-names-with-aliases` / functional-suitability.functional-completeness / S3 | **fixed 2026-08-06** — the clause parses as the query-level `ctes` rule, a *sibling* of selectStmt; the pass now visits it against the first SELECT it precedes |
| 14 | Applet pages named `FROM facts` unqualified; hand-testing them with `--database=` hid it completely, and every page failed `UNKNOWN_TABLE` under a real applet | `pain — trial process` / S3 | fixed; a regression test now rejects an unqualified table reference in the book |
| 15 | The JSONBench query set does not port to unhinted JSON — `Dynamic` columns are refused by `GROUP BY` and by `IN`, and Q3 cannot execute without a cast | `note — workload, not toolbelt` / S3 | not actionable here; it bounds what the domain numbers can claim |
| 16 | `ResolveColumnNames` does not descend into a `WITH (SELECT …) AS alias` scalar subquery — the handle ships unexpanded and dies at the server, and `--strict` does not catch it because the pass never visits those nodes | `missing nanopass-scope-resolution → proposed:resolve-column-names-in-cte-subqueries` / functional-suitability.functional-completeness / S3 | **open** — adjacent to but distinct from row 13; worked around by repeating the sub-select |
| 17 | `mapping.LoadJsonMapping` declared no encoding hints on its `bool` / `int64` / `float64` value columns, and an omitted hint emits no `CODEC` clause at all rather than a neutral default | `broken leeway-ddl-codegen → proposed:leeway-encoding-hint-defaults` / functional-suitability.functional-correctness / S3 | **fixed** — 2.36× on the 10M `int64` lane; candidates recorded in the mapping's doc comment (§5a) |

Retracted, and kept visible because the retraction is the more useful record:

- **[missing leeway-read-access-codegen → proposed:leeway-sql-read-access /
  performance-efficiency.time-behaviour / S1]** — filed after the 1M run as
  "resolving a value by path in SQL has no accelerated form". The claim that
  nothing equivalent exists is **wrong**: a `MATERIALIZED` column resolving
  the path at merge time is exactly that, and it took arm B's Q1 from
  0.073 s / 194 MB to 0.008 s / 4.7 MB for 535 KiB of added column. Refiled
  narrower as row 5. This was the trial's largest single error and it inflated
  the headline for a full run.

Positive maturity — competences the runs leaned on successfully, which the
convention asks for in their own right:

- **`leeway-dml-codegen`** carried an ingestion shape it was not designed for
  (§5), with no generated-code changes and no escape hatch.
- **`leeway-ddl-codegen`** composed the benchmark table straight from
  `chstore.ComposeSetupSQL`, so arm B is provably the live store's own DDL.
- **ADR-0162 `chpack`** installed cleanly on a live 26.7 server and, with the
  read-back family, expressed all five queries with no escape hatch.
- **The leeway addressing machinery is 2.3 % of arm J's table** at 100M
  (§5a) — the overhead objection does not survive measurement on this corpus.

**Where the open rows go.** Rows 5–8 are one cluster — the SQL read surface is
unversioned, undiscoverable and ungenerated — and are carried by
[ADR-0171](../../adr/0171-leeway-sql-read-surface.md). Rows 12 and 13 were
localized defects that the ADR trigger list puts in neither tier, and both
were fixed on 2026-08-06, each with a regression test naming this ledger row.
Rows 1, 3 and 4 change the facts schema or add a package, so they need a
design dialogue before an ADR, and none is opened by this trial.

Row 12 cost more than filing it suggested, in three ways worth recording for
the next person who reads "a one-line grammar fix": it was not a missing rule
but a misplaced one (`paramSlot` existed and was reachable only from
`columnExpr`, which is why `{tier:String}` parsed and `{db:Identifier}` did
not); grammar2 needed the same alternative, because a slot has no canonical
form to be rewritten into; and three call sites were dereferencing
`tableIdentifier.Identifier()` with no nil check.

**Not done, deliberately.** The competence vault's `maturity` / `pain` fields
are still unset for every competence this trial exercised. The convention has
those flip editorially, citing findings, and ADR-0168 has no 0..5 rubric yet —
authoring the rubric is a prerequisite, not part of this trial. The recurring
`proposed:leeway-query-vocabulary-discoverability` slug has been filed three
times, which the directory convention calls the editorial signal to author a
corpus entry; that too is left to the vault's editor.

## 8 Milestones, closure, and re-running

M0 (pin + arm A) through M5 (results as facts + the book) are done; M6 ran as
arms D and E. **The trial closed on 2026-08-06 and is not retired.**

**100M was descoped for arms A–E**, and the gate passes on both counts (~77 GiB
against 262 GiB free, ~2 h wall clock, dominated by the single-process facts
ingest) — so this is a judgement about value, not feasibility. The primary
question was answered at 10M and arm E spent the last untried lever. What that
leaves unmeasured is the scaling direction of the two ratios that moved
between 1M and 10M: storage, which inverted, and arm A's pruning advantage,
which widens with scale. Arm J was later run at 100M for its own question
(§5a). Re-running A–E is a matter of raising `TIER`.

The protocol stays here rather than moving to
[`doc/adr-background-work/`](../../adr-background-work/) because re-running it
is the point: it is the quality practice's first task-level probe, and the
thing most worth measuring on a later build is whether §7b's open rows have
moved. ADR-0171 names a re-run **by an operator who has not read that ADR** as
the only honest check on its documentation-pointer gap.

**A re-run must read these first.**

- **The 1M tier is worthless for the domain question.** Every arm-A query
  finishes in tens of milliseconds, so timer granularity and scheduler noise
  are a material fraction of each measurement. It is a smoke test for the
  harness. 10M is the first tier whose numbers mean anything.
- **The facts arms' figures are not comparable across the two recorded runs.**
  The 1M run open-coded the lane arithmetic (~3× slower than the query
  vocabulary) and has no cold column. Arm A is comparable; the facts arms are
  not.
- **The numbers under `runs/` were produced by SQL naming functions the
  current build no longer installs.** The UDF roster moved to a single `LW_`
  namespace on 2026-08-07 (ADR-0162 Update). The protocol's `.sql` files are
  re-spelled; **nothing under `runs/` is**, deliberately — those files record
  which functions were installed on a server on a given day, including one
  retired in the repository and still present there, which is the observation
  ADR-0171 was written around. Re-spelling a recorded observation to a name
  that did not exist when it was made would destroy the evidence for the
  finding it produced. A rename was separately measured as
  performance-neutral (±12 %, workstation noise), since these UDFs are macros
  inlined at analysis time; re-running against a server provisioned from the
  current build is the check, and it has not been done.
- **The remaining open questions**, unanswered rather than closed: read
  discipline for the facts arms (workingset/argMax semantics as every other
  boxer read, or append-only raw reads on the grounds that events are
  immutable — whichever is chosen defines "the" facts number, and running both
  would itself measure the versioning overhead); and identity isolation, where
  the constraint is that the shared production store must not absorb benchmark
  subjects.

Continued by
[leeway on a second substrate](../leeway-second-substrate/README.md), which
takes this trial's canonical-mapping arm and its query set to a different
engine.

Related: [ADR-0066](../../adr/0066-leeway-dql-clickhouse-readback-generator.md),
[ADR-0089](../../adr/0089-rowdml-serialization-clickhouse-native-ingestion.md),
[ADR-0106](../../adr/0106-identity-fibonacci-tags-build-tag-retirement.md),
[ADR-0116](../../adr/0116-play-leeway-column-handle-resolution.md),
[ADR-0132](../../adr/0132-sqlapplet-sql-defined-applets.md),
[ADR-0148](../../adr/0148-app-workingsets.md),
[ADR-0162](../../adr/0162-leeway-co-ragged-function-pack.md),
[ADR-0171](../../adr/0171-leeway-sql-read-surface.md),
[pprof-profiles-as-data](../../adr-background-work/pprof-profiles-as-data.md).
