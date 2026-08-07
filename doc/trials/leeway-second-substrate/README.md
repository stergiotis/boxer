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
> trial and its [pin](../jsonbench-on-facts/upstream/PIN.md); (c) **statements
> about DuckDB and DataFusion are from prior knowledge and are unverified** —
> neither binary is installed on the development box, no version is pinned, and
> M0 exists to install them and check every capability claimed in §3 against the
> actual builds. Where a §3 claim is load-bearing for the milestone cut it is
> marked *(c, unverified)*.

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
2. **The side-product, secondary here.** Whether the numbers travel — storage
   and latency for the same corpus under the same queries on a different
   engine. Unlike the sibling trial, where the domain numbers were co-equal,
   here they are a by-product: three engines on one workstation compare
   engines, not data models, and the trial is not posed to answer that.

In scope: the **canonical leeway JSON mapping** arm of the sibling trial
(its arm J), DuckDB as the primary target and DataFusion as a marginal-cost
third point, the 1M and 10M tiers, Q1–Q5 and U1–U9. Out of scope: the
`boxer.facts` arms (see §3, arm F — deliberately optional and last), the
100M tier, `play`/`sqlapplet` integration, and any claim that this trial
ranks engines.

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
[`queries-usp-leeway.sql`](../jsonbench-on-facts/queries-usp-leeway.sql).
Tiers: 1M as smoke, 10M as the reportable tier. Nothing about the workload is
re-derived here; if the sibling's pin moves, this trial's numbers move with it
and the logbook must say so.

## 3 System under test and arms

### 3a What is actually bound to ClickHouse *(tier a)*

Stage by stage, with the seam named:

| Stage | Binding | Port cost |
| --- | --- | --- |
| describe → IR → map | none — `common.TableDesc`, canonical types | free |
| DDL | `ddl/clickhouse` behind `common.TechnologySpecificGeneratorI` ([`lw_types.go:207`](../../../public/semistructured/leeway/common/lw_types.go)); `ddl/arrow` and `ddl/golang` already implement the same interface | a new backend, or none at all if the target reads Parquet |
| marshal / DML | the generated builders already emit Arrow record batches; `dml.WriteArrowRecords` already writes Parquet and Arrow IPC ([`lw_dml_arrow_utils.go`](../../../public/semistructured/leeway/dml/lw_dml_arrow_utils.go), used in [`dml/example/cli.go`](../../../public/semistructured/leeway/dml/example/cli.go)) | swap the sink |
| query vocabulary | `chpack`'s fifteen SQL UDFs (ADR-0162) and the `LEEWAY_*` readback family — but see H1 | none for the canonical mapping; unknown for facts |
| handle expansion | `jsonbench resolve` runs ADR-0116's pass through `nanopass`, a ClickHouse parser | ported query files carry physical column names, or something parser-free expands them |
| native query path | grammar1 / nanopass | out of scope, as arm A was in the sibling trial |
| `factsstore/chstore` | the live store's MergeTree DDL | avoided by scoping to the canonical mapping |

The DDL seam has three implementations already, so the interesting question is
not whether it holds but whether it is *needed*: an engine that reads Parquet
infers its own schema, and what that loses (encoding aspects — the
`LowCardinality` hint has no Parquet spelling) is a measurable, not an
argument.

### 3b Arms

| Arm | Engine | Data comes from | What it isolates |
| --- | --- | --- | --- |
| P | — | `FORMAT Parquet` out of the sibling trial's arm-J table | that the physical layout leaves ClickHouse at all |
| J-duck | DuckDB | arm P | whether the read vocabulary ports |
| J-df | DataFusion | arm P, same file | the marginal cost of a third engine once the file exists |
| W | DuckDB | Parquet written by `jsonbench jsonmap ingest` | whether the *writer* is neutral, not just the layout |
| N | DuckDB | the raw corpus via DuckDB's own JSON reader | a reference of the sibling trial's arm-A00 kind |
| F | DuckDB | the facts layout | *optional and last* — the `RAGGED_*` question (H2) |

Arm P before arm W is deliberate: the export is one statement and de-risks the
whole query translation before any Go is written. Arm W is what makes the
neutrality claim about leeway rather than about Parquet.

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
- **Idiom rule.** The ported queries run directly against each engine's CLI,
  as arm A did in the sibling trial. Handle expansion goes through a
  ClickHouse parser, so ported query files carry physical column names —
  which is a friction to file, not to fix here.
- **Process measurement.** The solution itself is a result: artifacts are
  committed, their size recorded (files, lines), and manual interventions
  counted — an escape hatch is by definition a finding.
- **Reporting.** Every milestone appends a logbook entry. Domain numbers are
  small enough and secondary enough here that a table inside the logbook entry
  is the expected form; the sibling trial's facts-and-applet path is
  deliberately not repeated, because the numbers are a by-product (§1) and
  do not warrant a second reporting surface.

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

- **M0 — acquire and pin.** Install DuckDB and `datafusion-cli`, record
  versions, and check every *(c, unverified)* claim in §3 against the actual
  builds — list indexing, list-position, `unnest`, macro definition, and
  DataFusion's list-lambda coverage in particular. Gate: both binaries execute
  a trivial list query. If DataFusion lacks the lambda forms U1–U9 need,
  descope arm J-df to Q1–Q5 and file the coverage finding rather than working
  around it.
- **M1 — export and translate.** Arm P at the 1M tier; translate Q1–Q5 and
  U1–U9 for both targets; compare results against the sibling trial's
  ClickHouse run. Gate: identical row counts, and identical values modulo H3's
  absent-path divergence — which, if it appears, is recorded before it is
  worked around.
- **M2 — measure at 10M.** Sizes, cold and hot latency, physical plans, for
  arms P / J-duck / J-df. This is the reportable run.
- **M3 — the writer.** A Parquet output path on `jsonbench jsonmap ingest`,
  reusing `dml.WriteArrowRecords`. Gate: the file it writes and the file arm P
  exported are equivalent — same schema, same row count, same query results.
  This is the milestone that turns "the layout ports" into "leeway writes it".
- **M4 — the native-JSON reference** *(optional, gated on M2)*. Arm N, for a
  reference of the arm-A00 kind. Descope-able because DuckDB's JSON type may
  be a weak enough reference that the comparison says more about DuckDB than
  about leeway (§7 Q3).
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
