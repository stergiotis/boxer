---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** A measurement *plan*, not a result:
> every number below is either quoted from a cited source or marked "to
> measure". Do not cite as authoritative.

> **Provenance.** Compiled 2026-08-07, ahead of any measurement. Claims are
> three-tiered: (a) statements about this repository were checked against the
> working tree on the compile date, paths cited; (b) the workload is inherited
> wholesale from the [jsonbench-on-facts](../jsonbench-on-facts/README.md)
> trial and its [pin](../jsonbench-on-facts/upstream/PIN.md); (c) statements
> about DuckDB and DataFusion were compiled from prior knowledge and were
> unverified. **M0 ran on 2026-08-07 and checked them against pinned builds**
> — DuckDB v1.5.5 and datafusion-cli 54.1.0 — so a claim still marked
> *(c, unverified)* is one M0 did not reach. Evidence:
> [`runs/2026-08-07-m0/`](./runs/2026-08-07-m0/), regenerable with
> [`probe.sh`](./probe.sh); what it found is in the
> [logbook](./logbook.md).

# leeway on a second substrate — a toolbelt trial

## 1 Question and scope

leeway is meant to be reasonably technology-neutral: a data-mapping engine
whose six stages happen to have a ClickHouse realization, not a ClickHouse
engine. That claim has never been tested by moving anything. The sibling trial
built a real workload — a JSON corpus, five benchmark queries, nine
exploration queries, measured — and it runs on exactly one substrate.

1. **The trial proper.** What does it take to move the same data and the same
   queries onto a second columnar engine, and then onto a third, using the
   seams leeway already has? Which seams hold, which turn out to be
   ClickHouse-shaped, and how much code is written on the way? The artifacts
   are committed and counted; every escape hatch is a finding, not a fix.
2. **Does the USP verdict survive a change of engine?** The sibling trial's
   [USP experiments](../jsonbench-on-facts/leeway-usp-experiments.md) put the
   leeway shape head-to-head against ClickHouse's `JSON` type over nine
   queries and concluded: *a JSON column is fast when the path is in the
   query, a leeway table is fast when the path is in the data.* That thesis
   rests on three structural facts about ClickHouse's JSON type (its §2), and
   a structural fact about one engine is not a structural fact about JSON
   columns. Re-running U1–U9 on a second engine is the cheapest available
   test of which half of the thesis is about **leeway** and which half is
   about **ClickHouse**. This is why U1–U9 are first-class here rather than
   an appendix to Q1–Q5.
3. **The side-product, least weighted.** Whether the numbers travel — storage
   and latency for the same corpus under the same queries on a different
   engine. Unlike the sibling trial, where the domain numbers were co-equal,
   here they are a by-product: three engines on one workstation compare
   engines, not data models, and the trial is not posed to answer that.

In scope: the **canonical leeway JSON mapping** arm of the sibling trial
(its arm J), DuckDB as the primary target and DataFusion as a marginal-cost
third point, the 1M and 10M tiers, **both query sets — Q1–Q5 (the benchmark)
and U1–U9 (the USP set)** — and the JSON-side counterpart of the USP
head-to-head. Out of scope: the `boxer.facts` arms (see §3, arm F —
deliberately optional and last), the 100M tier, `play`/`sqlapplet`
integration, and any claim that this trial ranks engines.

**The two query sets ask different things and both are wanted.** Q1–Q5 is the
benchmark: every path is named in the query, and it is the case a JSON column
is built for. U1–U9 is the complement: the path is data, not syntax. The port
is expected to be easy for Q1–Q5 (H1) and is *interesting* for U1–U9, because
that is where the sibling trial's largest ratios live — 11× to 676×, and two
queries with no expression on the JSON side at all.

**Why the canonical mapping rather than facts.** The facts arm reaches
`chstore.ComposeSetupSQL` for its DDL, needs
`allow_suspicious_low_cardinality_types`, and carries Ref-shaped memberships
whose read path is the elaborate one (§7b rows 3 and 9 of the sibling
ledger). The canonical mapping is plain leeway with none of that baggage,
which makes it the honest measure of what porting leeway costs.

## 2 The workload

Inherited unchanged from
[jsonbench-on-facts](../jsonbench-on-facts/README.md): the Bluesky Jetstream
corpus at the pinned upstream commit, Q1–Q5 as translated for the canonical
mapping in
[`queries-jsonmap.sql`](../jsonbench-on-facts/queries-jsonmap.sql), and the
nine exploration queries in
[`queries-usp-leeway.sql`](../jsonbench-on-facts/queries-usp-leeway.sql),
matched statement for statement by its JSON-side counterpart
[`queries-usp-jsonv2.sql`](../jsonbench-on-facts/queries-usp-jsonv2.sql).
Tiers: 1M as smoke, 10M as the reportable tier. Nothing about the workload is
re-derived here; if the sibling's pin moves, this trial's numbers move with it
and the logbook must say so.

The USP set's design and its ClickHouse-side results are in
[leeway-usp-experiments.md](../jsonbench-on-facts/leeway-usp-experiments.md);
this trial re-runs the leeway half on other engines and re-poses the JSON half,
because the counterpart of ClickHouse's `JSON` type is a different object on
each target (§3b, arm N).

## 3 System under test and arms

### 3a What is actually bound to ClickHouse *(tier a)*

Stage by stage, with the seam named:

| Stage | Binding | Port cost |
| --- | --- | --- |
| describe → IR → map | none — `common.TableDesc`, canonical types | free |
| DDL | `ddl/clickhouse` behind `common.TechnologySpecificGeneratorI` ([`lw_types.go:207`](../../../public/semistructured/leeway/common/lw_types.go)); `ddl/arrow` and `ddl/golang` already implement the same interface | a new backend, or none at all if the target reads Parquet |
| marshal / DML | the generated builders already emit Arrow record batches; `dml.WriteArrowRecords` already writes Parquet and Arrow IPC ([`lw_dml_arrow_utils.go`](../../../public/semistructured/leeway/dml/lw_dml_arrow_utils.go), used in [`dml/example/cli.go`](../../../public/semistructured/leeway/dml/example/cli.go)) | swap the sink |
| query vocabulary | `chpack`'s fifteen SQL UDFs (ADR-0162) and the readback UDF family — but see H1 | none for the canonical mapping; unknown for facts |
| handle expansion | `jsonbench resolve` runs ADR-0116's pass through `nanopass`, a ClickHouse parser | ported query files carry physical column names, or something parser-free expands them |
| native query path | grammar1 / nanopass | out of scope, as arm A was in the sibling trial |
| `factsstore/chstore` | the live store's MergeTree DDL | avoided by scoping to the canonical mapping |

The DDL seam has three implementations already, so the interesting question is
not whether it holds but whether it is *needed*: an engine that reads Parquet
infers its own schema, and what that loses (encoding aspects — the
`LowCardinality` hint has no Parquet spelling) is a measurable, not an
argument.

### 3b Arms

| Arm | Engine | Data comes from | Query sets | What it isolates |
| --- | --- | --- | --- | --- |
| P | — | `FORMAT Parquet` out of the sibling trial's arm-J table | — | that the physical layout leaves ClickHouse at all |
| J-duck | DuckDB | arm P | Q1–Q5, U1–U9 | whether the read vocabulary ports |
| J-df | DataFusion | arm P, same file | Q1–Q5, U1–U9 | the marginal cost of a third engine once the file exists |
| W | DuckDB | Parquet written by `jsonbench jsonmap ingest` | Q1–Q5 | whether the *writer* is neutral, not just the layout |
| N-text | DuckDB | the raw corpus as `JSON` (text-backed) | Q1–Q5, U1–U9 | the USP counterpart: DuckDB's answer to ClickHouse's `JSON` type |
| N-struct | DuckDB | the raw corpus via `read_json` auto-inference | Q1–Q5, U1–U9 | a **third shape** ClickHouse's comparison had no equivalent of |
| X | ClickHouse | arm J, exploded with `ARRAY JOIN` | Q1–Q5, U1–U9 | the third rendering: one row per attribute, no array functions at all |
| F | DuckDB | the facts layout | Q1–Q5 | *optional and last* — the `RAGGED_*` question (H2) |

Arm P before arm W is deliberate: the export is one statement and de-risks the
whole query translation before any Go is written. Arm W is what makes the
neutrality claim about leeway rather than about Parquet.

**Arms X and Y are on the home engine on purpose.** They are the rendering M0
was forced into on DataFusion — explosion plus ordinary relational operators —
measured where the packed form is also available, so the representations can be
compared over identical data with the engine held constant. Both ran on
2026-08-07 and are the only arms with results; see the
[logbook](./logbook.md). Where they landed:

- **Exploded is at parity or better on four of the five benchmark queries**
  (Q1 0.18×, Q4 0.68×, Q5 0.72×, Q2 1.06×, Q3 2.19×) and far better on the
  path-oriented USP queries (U4 0.03×, U6 0.07×, U9 0.18×), because the path
  becomes a sort-key prefix instead of an array scan. It is also 0.70× on
  disk. **Memory is where it is consistently worse** — 2.7–13.2× on the
  reassembly queries, 69.8× on U8.
- **One table per section (arm Y) is not worth doing**: 0.2 % storage against
  the tagged-union form, query effects that cancel across the set, and it
  costs the reader the section roster.
- **A retraction sits in the logbook and is load-bearing for how this trial
  reports anything further.** Arm X's first numbers compared *my SQL*, not the
  representations — one formulation per arm, differing by 2–5× — and a second
  claim about re-keying inverted under the same correction. §4's formulation
  rule exists because of it.

**Arms N-text and N-struct are what make the USP question askable here**, and
they are not one arm. ClickHouse's `JSON` is a shredded columnar type; DuckDB
offers two different things and neither is that — a text-backed `JSON` type
with a function library, and schema-on-read inference into `STRUCT` columns
via `read_json`. The USP head-to-head therefore cannot be *transplanted*, only
re-posed, and reporting both keeps the comparison from being rigged in either
direction: N-text is the shape that tolerates a heterogeneous corpus,
N-struct is the shape that is fast when it does not have to.

### 3c Standing hypotheses, pre-registered

- **H1 — the canonical mapping needs no function pack.** Its entire read
  vocabulary is one form, `value[indexOf(lmv, path)]`, stated as such in
  [`queries-jsonmap.sql`](../jsonbench-on-facts/queries-jsonmap.sql) ("No UDF
  is installed to run this file"). Any engine with list indexing and a
  list-position builtin should carry it. If H1 holds, the port's cost is
  translation, not implementation.
- **H2 — the facts layout is the part that does not port, and structurally
  so.** Its read path needs two cumulative sums over descriptor lanes, and
  `arrayCumSum` is a ClickHouse builtin with no general SQL-array counterpart
  *(c, unverified for both targets)*. If H2 holds it is evidence that §7b row 3
  of the sibling ledger — facts sections accepting only Ref-shaped memberships
  — has a portability cost on top of the expressiveness cost already filed.
  Arm F is where this gets answered, and it is optional precisely because it is
  the arm that could turn a medium effort into a large one.
- **H3 — absent-path semantics diverge, and the byte-identical check breaks.**
  ClickHouse `indexOf` returns 0 and `arrayElement` at index 0 returns the
  type's default, which is why Q1 has an empty bucket. DuckDB's
  `list_position` returns NULL for an absent element *(c, unverified)*. Both
  prior runs passed a byte-identical-results check across arms; this one is
  expected not to, without an explicit coalesce that is itself a finding.
- **H4 — Q3 is timezone-incomparable by default.** Already pre-registered
  upstream for `toHour`; the target engines have their own answer, and the
  trial must pin both sides or drop Q3 from the comparison.
- **H5 — part of the USP thesis is a fact about ClickHouse, not about JSON
  columns.** The USP document's §2a rests on a hard limit: an enumerated path
  cannot be used to *read* a ClickHouse `JSON` column, so every data-dependent
  path query falls back to re-serialising and re-parsing per row. That is what
  U3's 676× and U8's 176 s are made of. If another engine's JSON type accepts a
  runtime path expression, those rows measure a ClickHouse limitation that
  leeway routes around, and the portable half of the thesis is the narrower
  claim about *lane width* (§2c) — which the memory column, flat at ~4.2 GB on
  the JSON side against a 60× spread on the leeway side, supports independently.
  U1/U2/U4/U6 and U9 are where that narrower claim gets retested.

  **M0 has already answered the DuckDB half, before any arm ran** — see the
  [logbook](./logbook.md) and `runs/2026-08-07-m0/`. It is stated here as a
  hypothesis anyway, because the DataFusion half is open, because a re-run
  follows this protocol rather than the logbook, and because the distinction
  between what was predicted and what was found is the thing these documents
  exist to keep.
- **Note, not a hypothesis.** DuckDB has neither `MATERIALIZED` columns nor
  data-skipping indices *(c, unverified)*, so the sibling trial's two largest
  levers — worth 3.8–13.8× and 1.07–1.76× there — have no direct counterpart.
  The materialization lever is still available, one stage earlier, as columns
  computed at shred time. Where in the pipeline a lever sits is itself worth
  recording.

## 4 Method

- **Environment.** ClickHouse at the repo-pinned version for the export;
  DuckDB and DataFusion versions recorded at M0 and pinned thereafter.
  Hardware, server timezone, and storage class recorded at run time — never
  hostnames or personal paths.
- **Run discipline.** Mirrors
  [`measure.sh`](../jsonbench-on-facts/measure.sh) so the two trials are
  comparable: `TRIES=3`, cache drop once before each query's tries, cold =
  try 1, hot = min(try 2, try 3). With cache-dropping unavailable the cold
  column is reported absent, not merely noisy.
- **Metrics, with their comparability caveats stated rather than assumed.**
  Latency is wall-clock per statement. Peak memory is **not** the same metric
  across engines — ClickHouse reports a query-scoped figure, the targets do
  not, and maxRSS of the process is the closest available substitute; runs
  record which was used. Storage is only comparable if the compression codec
  is pinned on both sides (§7 Q1); until it is, both figures are recorded and
  no ratio is quoted.
- **Fairness controls, which do not transfer.** The USP run needed two
  ClickHouse settings turned off — `use_query_condition_cache=0`, after the
  cache reported a 100×-too-fast number, and `min_execution_speed=0`, because
  the server otherwise *kills* the JSON-side value-anywhere query at 10 s. Both
  are ClickHouse-specific, so the targets need their own audit rather than the
  same flags: what caches a repeated predicate, and what aborts a slow query.
  A ported number obtained without that audit is not comparable to the
  ClickHouse one, and the run must say which controls it checked.
- **The row-identity trap, inherited.** The canonical mapping's
  `id:blake3hash` column is 20.3 % of the leeway table at 10M and has no
  counterpart in any JSON-side arm, so a size comparison including it charges
  leeway for something no query reads. The USP document reports sizes both ways
  and this trial must too; §7 Q1 is the same question in the codec dimension.
- **Idiom rule.** The ported queries run directly against each engine's CLI,
  as arm A did in the sibling trial. Handle expansion goes through a
  ClickHouse parser, so ported query files carry physical column names —
  which is a friction to file, not to fix here.
- **A ratio needs the best formulation on both sides — and one storage format
  across them.** No number comparing two representations may be quoted from one
  query formulation per side, nor across two storage formats: M4 quoted
  N-struct as 2.4× the leeway rendering on Q1 when the like-for-like figure is
  1.6×, the rest being DuckDB-native against Parquet. Where a
  layout admits more than one natural rendering — a join against a regroup, a
  higher-order form against an explosion — each side is measured at its best
  known form, and the alternatives are recorded alongside. This rule is here
  because the first arm X entry broke it and was wrong by 2–5×, and because
  the sibling trial's single largest error (its retracted S1) was the same
  mistake in a different costume. A representation comparison that has not
  searched formulations is measuring the author.
- **Process measurement.** The solution itself is a result: artifacts are
  committed, their size recorded (files, lines), and manual interventions
  counted — an escape hatch is by definition a finding.
- **Reporting.** Every milestone appends a logbook entry. Domain numbers are
  small enough and secondary enough here that a table inside the logbook entry
  is the expected form; the sibling trial's facts-and-applet path is
  deliberately not repeated, because the numbers are a by-product (§1) and
  do not warrant a second reporting surface.

## 4a Results — the verdict, 2026-08-07

M0–M4 are done. Every number below is 10M documents, one harness, hot =
min(try 2, try 3), **no cold column** (this box grants no passwordless sudo).
Detail and evidence in the [logbook](./logbook.md); the consolidated table is
`runs/2026-08-07-m4/m4-six-way.tsv`, which carries a `source` column because
the Q1–Q5 and USP figures do not all come from the same sitting — run-to-run
variance between them is 5–10 %, which is well inside every ratio quoted here
but is not zero. Two figures recur:

- **Q1–Q5** — the JSONBench set, where **every path is named in the query**.
- **U1,U2,U3,U4,U6,U8** — the USP set, where **the path is in the data**. The
  six ClickHouse's JSON type can express at all; leeway also answers U5 and U9,
  which it cannot.

### Is leeway reasonably technology-neutral? — **Yes, with a 2–3× band.**

The same mapping, the same corpus, four renderings across three engines:
Q1–Q5 spans **1.24 s (ClickHouse) → 3.31 s (DataFusion)**, a 2.7× band; the USP
set spans **1.50 → 2.92 s**, 1.9×. No configuration failed, no cliff, and
storage is 1.13–1.14 GiB everywhere except DuckDB's native format at 2.46 GiB —
which is a *format* choice, not a shape one. More important than the latency:
**all fourteen queries reproduce the ClickHouse oracle on every engine**, with
exactly two explained divergences — the absent-path bucket in exploded
renderings, and U5, whose disagreement is an Int64 overflow in the query rather
than anything about leeway.

### Is it unlocking the full potential of the underlying technology? — **No, not yet.**

The data model ports; the engineering around it lags, in four measured ways.
`ddl/arrow` types every bytes-lane `BLOB`, so `starts_with` and `contains` do
not bind and **two of fourteen queries fail on leeway's own writer output**
(M3). Encoding aspects do not cross the Parquet seam — the exploded table's
dense id is written `PLAIN` where `DELTA_BINARY_PACKED` applies, **8.5 %** (M2),
and leeway's writer passes no encoding properties either. `LowCardinality`
survives only because a writer happened to choose `RLE_DICTIONARY`, not because
leeway asked. And the lane algebra is written down only in its higher-order
form, while the rendering that ports everywhere — explosion plus relational
operators — is undocumented and is the *only* one DataFusion can run.

### Are other columnar technologies expressive enough? — **Yes, in three different spellings.**

DuckDB carries the higher-order rendering directly. DataFusion has **433
routines and not one higher-order array function**, yet reads the packed layout
exactly — `array_element` over `array_position`, `unnest` for the path census,
`array_max` standing in for one `arrayExists` — and matched the oracle on
**14 of 14**. The exploded rendering needs no array function at all. But
**nothing ports unchanged**: DuckDB needs `list_position`, a coalesce and
`lambda x:` (the `->` arrow is deprecated), DataFusion needs `array_position`, a
coalesce and an explicit `CAST` without which the composed idiom is a planning
error.

### Are native JSON types as expressive as leeway? — **Entirely engine-dependent.**

DuckDB's **is**: `json_extract(doc, p)` takes a runtime path and `json_tree`
walks into arrays reporting `fullkey`, so all fourteen are expressible.
ClickHouse's **is not** — U5 and U9 have no expression, and U3/U8 fall back to
re-serialising each document to text. DataFusion has **no JSON type at all**.
Schema-on-read is the least expressive of the three: **6 of 14**, and 1 of the
9 USP queries, because inference typed the backbone and left the varying tail
as `MAP(VARCHAR, JSON)`.

### Do they bring a disk-space advantage? — **No; marginal at best, and it reverses.**

ClickHouse's plain `JSON` is 1.07 GiB against leeway's 1.13 — **0.95×**, a 5 %
edge. Its *tuned* entry is 1.54 GiB, **larger** than leeway. DuckDB's JSON type
is 4.56 GiB against leeway's 2.46 — **1.9× larger**. There is no consistent
advantage in either direction.

### Do they bring a query-flexibility advantage? — **In ingest and evolution, not in query.**

The real advantage is upstream of the query: no mapping to declare, no
symbol-routing decision, no shredder, and new paths absorbed silently where
leeway's mapping is a DDL change. On the query side there is no advantage and
on ClickHouse a deficit. And the *fast* JSON configuration buys its speed by
giving flexibility back — arm A declares `max_dynamic_paths = 0`, so anything
outside its five named paths goes to shared data.

### Do they bring a query-performance advantage? — **Decisively yes for named paths, catastrophically no for discovered ones.**

Within ClickHouse, on identical documents:

| | Q1–Q5 (path named) | USP six (path discovered) |
| --- | --- | --- |
| arm A — typed hints + clustered index | **0.34 s** | — |
| arm A0 — typed hints, no index | **0.42 s** | — |
| arm A00 — plain `JSON` | 0.72 s | 234.72 s |
| leeway | 1.24 s | **1.50 s** |

**It is the typing, not the index**: removing the clustered index costs only
1.24× (0.34 → 0.42 s), so the JSON type's 3.0× advantage over leeway on named
paths survives a like-for-like unindexed comparison. Even undeclared it wins,
1.7×. Reverse the question and the same undeclared arm is **156× behind**
leeway (234.72 s against 1.50 s); DuckDB's JSON type is **106×** behind
leeway-native over the same six (237.55 s against 2.24 s), and 49–920× per
individual query.

**The one-line verdict.** leeway is neutral enough to move, at a 2–3× band and
without loss of correctness, and it is not yet extracting what the engines
offer. Against native JSON it trades a **3×** loss where the path is named for
a **50–900×** win where it is not — which is the sibling trial's thesis,
confirmed on two more engines, with the caveat that *which* of those regimes a
workload lives in decides everything, and JSONBench lives entirely in the first.

## 5 Findings ledger

Findings follow the classification scheme in the
[directory convention](../README.md): competence slug × relation
(`missing` / `broken` / `pain`) × ISO 25010 characteristic, severity S1–S4,
evidence in the run dir. Pre-registered candidates, so later readers can tell
hypotheses from surprises:

- `leeway-ddl-codegen` — no DDL backend exists for either target; whether
  Parquet schema inference is an adequate substitute, and what the encoding
  aspects lose in the round trip. Anchors a proposed slug if a backend turns
  out to be needed rather than merely absent.
- `leeway-dml-codegen` — the Parquet sink (arm W): how much code it takes
  given `WriteArrowRecords`, and whether anything in the generated builders
  assumes the ClickHouse-native sink.
- `leeway-read-access-codegen` — handle expansion bound to the ClickHouse
  parser; and, if arm F runs, the `arrayCumSum` dependency of H2.
- Query-semantics divergences (H3, H4) — these are `pain` against whichever
  competence the translation sits in, not defects in either engine.

## 6 Milestone cut (each descope-able)

- **M0 — acquire and pin.** ✓ **Done 2026-08-07.** DuckDB v1.5.5 and
  datafusion-cli 54.1.0 installed and pinned;
  [`probe.sh`](./probe.sh) turned §3's capability claims into evidence. Gate
  passed for both. **Its own descope instruction turned out to be wrong and is
  superseded**: it said to cut arm J-df to Q1–Q5 if DataFusion lacked lambdas.
  DataFusion lacks them entirely — 433 routines, no higher-order array function
  under any spelling — but the probe showed the lane algebra survives by
  explosion (`unnest` plus relational operators), so J-df keeps all of U1–U9 in
  a **second rendering** rather than losing four of them. That rendering is now
  part of M1. Details and findings in the [logbook](./logbook.md).
- **M1 — export and translate.** ✓ **Done 2026-08-07.** Arm P at the 1M tier,
  then four ported renderings against the tiebroken oracle: DuckDB packed
  (higher-order, `lambda x:`) and exploded, DataFusion packed
  (`array_element`/`array_position`) and exploded. **The gate passes** —
  every divergence is one of two explained classes, neither a translation
  error: Q1's absent-path bucket, which only the *exploded* renderings lack,
  and U5, where ClickHouse and DataFusion silently overflow Int64 and DuckDB
  does not. Q3 compares only with `session_timezone=UTC` pinned (M0 finding 4).
  Details in the [logbook](./logbook.md).
- **M2 — measure at 10M.** ✓ **Done 2026-08-07.** Nine configurations — three
  engines × {packed, exploded} plus exploded's join formulation on each — hot
  latency and storage, correctness re-verified. Headlines: the regroup/join
  formulation gap is **4.3–4.7× on DuckDB**, larger than the ClickHouse gap
  that prompted §4's rule; Parquet round-trips the packed layout at **parity**
  (1.005×) and the exploded one at **1.085×**, all of which is one column's
  lost delta encoding; and ClickHouse's default Parquet export adds **+31.5 %**
  of bloom filters the native table has no equivalent of. Cold runs remain
  unavailable, so the cold column is absent rather than noisy.
  Details in the [logbook](./logbook.md).
- **M3 — the writer.** ✓ **Done 2026-08-07, and the gate is split.** A
  `--parquet-out` flag on `jsonbench jsonmap ingest` sends the same Arrow
  record batches to `dml.WriteArrowRecords`, never contacting ClickHouse. Over
  the same source file: same row count, bytes within **0.45 %**, same
  encodings — but **not the same schema**. Every canonical-`y` column arrives
  as `BLOB` from leeway's writer and `VARCHAR` from ClickHouse's, so twelve of
  fourteen queries match byte-for-byte and the two that apply a string
  predicate to a path (U4, U9) fail to bind. Filed, not worked around.
  Details in the [logbook](./logbook.md).
- **M4 — the USP counterpart.** ✓ **Done 2026-08-07.** Arms N-text and
  N-struct, DuckDB only. **H5 holds**: DuckDB's JSON type takes a runtime path
  and `json_tree` walks into arrays, so both structural facts the USP thesis
  rests on are ClickHouse's rather than JSON's — N-text expresses all fourteen
  queries, including the two the sibling trial records as having no expression.
  **The thesis survives on cost anyway, with a wider margin**: 49–920× on the
  path-in-data queries against the sibling trial's 11–676×, at 15–81 GiB
  against 0.07–0.36. And schema-on-read (N-struct) **beats leeway on every
  benchmark query** — 1.6–2.6× like-for-like — while answering only 6 of 14,
  because inference typed the backbone and left `commit.record` as
  `MAP(VARCHAR, JSON)`. That win is exactly co-extensive with what inference
  typed: **one step into the tail and it inverts, leeway winning 2.6–3.4×**,
  because leeway's cost does not depend on where the path sits and N-struct's
  does. Details in the [logbook](./logbook.md).
- **M5 — the facts layout** *(optional, gated on M0–M3 having stayed cheap)*.
  Arm F, which answers H2 and would need a second `chpack` emitter or a
  hand-written macro set. This is the milestone most likely to be worth
  skipping; skipping it leaves H2 open, which is an acceptable outcome and
  should be said plainly in the logbook rather than left implied.
- **M6 — write-up.** Logbook entry per milestone, findings filed, a results
  section on this page, and a pointer to whatever the numbers end up informing.

## 7 Open questions

1. **Storage comparability.** ClickHouse `bytes_on_disk` and Parquet file
   bytes are not the same measurement under default codecs. Pin ZSTD on both
   sides, report both and compare nothing, or drop storage from the
   comparison — decide before M2, not after seeing the numbers.
2. **Whether a DDL backend is worth writing at all.** For engines that read
   files, Parquet schema inference may be the honest answer, and a
   `ddl/duckdb` backend would be ceremony. The counter-case is the encoding
   aspects, which inference cannot recover.
3. **Whether arm N is a fair reference.** DuckDB's JSON type is not shredded
   the way ClickHouse's is, so arm N may be a much weaker baseline than arm
   A00 was — in which case it flatters the leeway arm and should be reported
   as an engine observation, not a data-model one.
4. **Where the ported artifacts live.** This directory, or beside the
   sibling's, given the two share a pin, a corpus, and a query set.
5. **What a third engine would add.** DataFusion is included because the
   marginal cost is a second `SELECT` over a file that already exists. If that
   turns out to be false at M0, the trial is a two-engine trial and says so.

Related: [jsonbench-on-facts](../jsonbench-on-facts/README.md) — the sibling
trial this one continues;
[the leeway query algebra](../../explanation/leeway-query-algebra.md), which
states the algebra as substrate-independent and has had exactly one
realization;
[ADR-0162](../../adr/0162-leeway-co-ragged-function-pack.md) (the function
pack), [ADR-0116](../../adr/0116-play-leeway-column-handle-resolution.md)
(column handles),
[ADR-0171](../../adr/0171-leeway-sql-read-surface.md) (the read-surface
cluster this trial may add a portability row to).
