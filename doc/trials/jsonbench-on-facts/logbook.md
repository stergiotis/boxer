---
type: reference
audience: package maintainer
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-05
---

# JSONBench-on-facts — logbook

Chronological, append-only record of runs of the
[jsonbench-on-facts](./README.md) trial, per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in its own `./runs/<YYYY-MM-DD-slug>/` directory. Entry
template:

```markdown
## YYYY-MM-DD — <milestone / arm> — <one-line outcome>

- **Build under test:** boxer <commit>, ClickHouse <version>,
  JSONBench pin <commit>
- **Environment:** <CPU model, cores, memory, storage class, OS> — no
  hostnames or personal paths
- **Attempted:** <what this run set out to do>
- **Findings:** one line per proximate obstacle, per the trials README's
  *Finding classification*:
  **[<relation> <competence-slug> / <characteristic> / S#]** <statement>
  (evidence: <file in run dir>) — plus positive-maturity lines for
  competences the run leaned on successfully
- **Solution size:** <artifacts touched: files, lines — when applicable>
- **Results:** <facts run ids / applet pointer / "none this run">
- **Run dir:** <./runs/YYYY-MM-DD-slug/ — evidence backing this entry>
```

## 2026-08-05 — M0–M3 at the 1M tier — the tax is ~1.4× storage, 9–15× latency, 6–76× memory; neither index step recovers any of it

- **Build under test:** boxer `5a065288`, ClickHouse 26.7.2.59,
  JSONBench pin `e6c7c98d` (2026-03-08)
- **Environment:** AMD Ryzen AI MAX+ PRO 395, 16C/32T, 93 GiB RAM, NVMe
  behind dm-crypt, Fedora 44 / kernel 7.1.6, server TZ Europe/Zurich. Full
  capture and the three protocol deviations in the run dir's
  `environment.md`.
- **Attempted:** M0 (pin, run discipline, arm A), M1 (mapping + ingest),
  M2 (arm B), M3 (arm C) — all at the 1M tier. M4 (scale), M5 (results as
  facts + applet) and M6 (arm D) not attempted; see *Not done* below.
- **Findings:**
  - **[missing leeway → proposed:leeway-json-shredding / functional-suitability.functional-completeness / S3]**
    `mapping.LoadJsonMapping` defines the canonical JSON→leeway *schema*, but
    boxer ships no shredder that turns a JSON document into rows under it —
    every leeway ingestion in this tree is hand-written per domain, so the
    trial wrote its own walker (evidence: `apps/jsonbench/jsonbench_shred.go`, 130 lines;
    `public/semistructured/leeway/mapping/` exports only schema constructors).
    **Downgraded S2→S3 on review:** a shredder for exactly this mapping exists
    as a CLI outside this repository (`prior-art.md`), so the gap is boxer-side
    reach, not an absent capability.
  - **[broken leeway-dml-codegen / functional-suitability.functional-appropriateness / S2]**
    boxer.facts sections accept only Ref-shaped memberships
    (`LowCardRef`, `HighCardRef`, `MixedLowCardRefHighCardParameters`), all of
    which identify a membership by a uint64 from a vcs-registered vocabulary.
    The canonical JSON mapping needs *verbatim* memberships because an open
    JSON corpus has no closed path vocabulary. The path had to be demoted into
    the high-cardinality parameter channel — the detour is most of the 3.68×
    uncompressed-size inflation (evidence: `apps/jsonbench/jsonbench_vocab.go`;
    `runs/2026-08-05-m0-m3-1m/results.md` § Size).
  - **[pain leeway-read-access-codegen / performance-efficiency.resource-utilisation / S3]**
    Reading one value out of a facts row in SQL needs *two* independent
    cumulative-sum reconstructions — one over `lmrcard` to find the
    attribute's membership, one over `len` to find its value, because
    array-valued sections flatten their values across attributes (evidence:
    `queries-facts.sql` header).
    **Halved on review (`prior-art.md`):** only the `len` half is intrinsic.
    In the canonical leeway JSON mapping every attribute carries exactly one
    membership, so `lmv` co-indexes 1:1 with the value column and a plain
    `indexOf` resolves a path. This trial broke that co-indexing itself — the
    kind tag rides `lr`, and array indices ride a second `lmr` membership.
    Both were the ingester's choices. The `len` indirection does survive for
    facts, whose string and integer sections are array-valued where the
    canonical mapping's are scalar.
  - **[pain leeway-read-access-codegen / functional-suitability.functional-correctness / S2]**
    Getting that second reconstruction wrong fails *silently* on this corpus:
    every attribute here has `len = 1`, so naively co-indexing the value
    column with the membership columns returns correct answers on Bluesky and
    wrong ones on any document with a multi-element array. The first
    translation had exactly this bug and Q1/Q2 passed (evidence: run dir
    `arm-b/query-results.txt` matches arm A under both the wrong and the right
    form for Q1/Q2).
  - **[pain leeway-ddl-codegen / usability.operability / S4]**
    The facts DDL cannot be applied, or the table cloned, without
    `allow_suspicious_low_cardinality_types=1`; `CREATE TABLE … AS <facts>`
    fails with `SUSPICIOUS_TYPE_FOR_LOW_CARDINALITY` under default settings
    (evidence: `arm-c.sh`, which carries the flag on every client invocation).
  - **[missing leeway → proposed:leeway-json-shredding / functional-suitability.functional-completeness / S4]**
    boxer.facts has no `null` / `undefined` section, which the canonical JSON
    mapping does have, so JSON nulls cannot round-trip. **Not exercised by
    this corpus** — the Bluesky 1M tier contains zero JSON nulls
    (`nulls_dropped=0`, `arm-b/ingest-metrics.txt`), so this is a note, not a
    blocker, and it is filed because the next corpus will hit it.
  - **[missing leeway-read-access-codegen → proposed:leeway-sql-read-access / performance-efficiency.time-behaviour / S1]**
    Resolving a value by its membership path *in SQL* has no accelerated form:
    every query rebuilds a per-attribute path vector per row, materialising
    three whole array columns to evaluate one predicate. Measured on Q1
    against the same column — `arrayFirst` over the reconstructed path vector
    costs 0.083 s / 168 MB, a fixed index into that same column costs
    0.012 s / 21 MB (**7× time, 8× memory**), and arm A costs 0.005 s / 2.5 MB.
    Strip the reconstruction and arm B is within 2.4× of arm A instead of
    13.8× (evidence: `runs/2026-08-05-m0-m3-1m/diagnostics.md`). **This
    finding relocates most of the headline tax** away from the storage model
    and onto the read path.
    **Retracted as filed, and refiled narrower (`prior-art.md`):** the claim
    that "nothing equivalent exists for SQL consumers" is wrong. A ClickHouse
    `MATERIALIZED` column resolving the path at merge time is exactly that
    path, and a sibling experiment outside this repository already uses it.
    Measured here on arm B's own data, Q1 goes 0.073 s / 194 MB →
    **0.008 s / 4.7 MB** for 535 KiB of added column — 1.6× arm A's time and
    1.9× its memory, against 13.8× and 76× as this trial ran it. Refiled as:
    **[missing leeway-ddl-codegen → proposed:leeway-sql-materialized-projections
    / functional-suitability.functional-completeness / S3]** — boxer has no
    tooling that emits materialized-column definitions from a leeway schema,
    so a SQL consumer must hand-write them per path and keep them in sync with
    the physical column names.
  - Positive maturity, worth its own line: **`leeway-dml-codegen`** carried an
    ingestion shape it was not designed for — arbitrary shredded JSON, 12.0M
    attributes across five sections — at 37,420 docs/s and 376 MB peak RSS,
    with no generated-code changes and no escape hatch. The generated
    `InEntityFacts` builder and `chclient.InsertArrow` did the whole ingest
    path.
  - Positive maturity: **`leeway-ddl-codegen`** composed the benchmark-local
    table straight from `chstore.ComposeSetupSQL`, so arm B is provably the
    live store's own DDL rather than a hand-copied approximation
    (`apps/jsonbench/jsonbench_ddl.go`, 84 lines).
- **Protocol deviations** (properties of the machine and the licence, not
  toolbelt findings):
  1. **Nothing from upstream is vendored.** JSONBench is CC BY-NC-SA 4.0 and
     boxer is MIT; copying its files in would carry ShareAlike and
     NonCommercial obligations the repository does not have. M0's "vendor the
     DDL, queries, and run discipline" is instead satisfied by a pin plus
     SHA-256 verification (`upstream/PIN.md`, `upstream/fetch-pin.sh`). The
     protocol's M0 wording should be corrected to match.
  2. **No cold column.** The upstream cold procedure needs root for
     `/proc/sys/vm/drop_caches` and this workstation has no passwordless
     sudo. All numbers are hot; the cold column is *absent*, not noisy.
  3. **Server TZ is Europe/Zurich, not UTC**, so Q3's hour buckets do not line
     up with upstream's published result rows. Runtimes and cross-arm
     comparisons are unaffected — every arm runs on this server.
- **Solution size:** 693 lines of Go (`apps/jsonbench/`: shredder 130, ingest
  CLI 417, DDL command 84, vocab 62) + 351 lines of trial harness
  (`arm-a.sh` 48, `measure.sh` 100, `arm-c.sh` 45, `queries-facts.sql` 36,
  `queries-facts-skip.sql` 13, `upstream/fetch-pin.sh` 41, plus prose).
  **Manual interventions: one** — the redundant `has()` conjunct arm C's
  queries carry so a bloom filter has something to prune on. It is itself
  filed above as the reason arm C cannot work.
- **Results:** in the run directory, not yet as facts — see *Not done*.
  Headline: results byte-identical across all three arms; storage **1.44×**;
  hot latency **9.2×–15.3×**; peak memory **5.6×–76×**; the §4 hypothesis
  (primary index prunes nothing on the unmodified facts table) **confirmed**
  — `Condition: true`, 200/200 granules, all five queries; arm C's skipping
  indices are used but prune 1 granule of 245 and an on/off A/B on the same
  table shows no runtime difference, so **arm C closes none of the gap**.
- **Not done, and why:** M4 (10M/100M) — not reached this run; the 1M tier is
  a smoke tier and its ratios are soft at the second digit, so the 10M
  development tier is the next thing worth doing. M5 (results-as-facts + the
  book applet) — not started; results live as files in the run dir, which the
  convention allows only as an exception, so this is debt. M6 (arm D,
  re-keying) — the protocol gates it on "an unexplained B/C gap"; the gap is
  now *explained* (granule-level pruning cannot work on membership set
  semantics without clustering), which is precisely the argument **for**
  running arm D, so it is promoted from optional to the natural next
  experiment. The workingset/argMax pricing agreed for this run (§9 Q1) was
  not measured either — only the raw append-only headline number was.
- **Post-run correction (same day, same loaded tables):** a decomposition
  prompted by the headline looking worse than the model implies
  (`runs/2026-08-05-m0-m3-1m/diagnostics.md`) found that the membership
  machinery is only **8 %** of arm B's disk footprint — 81.5 % is the shredded
  values themselves — and that the latency and memory columns are dominated by
  the SQL path reconstruction, not the storage model. `results.md` § Size
  carries a superseded-attribution note. Two secondary measurements from the
  same pass: splitting the mixed `stringArray` column per JSON path buys only
  8 %, `ORDER BY did` buys 10 % (18 % together), and `id:naturalKey` — a
  16-byte blake3 this trial's ingester writes per row, which arm A has no
  equivalent of — is 8.6 % of arm B's total on its own.
- **Query-vocabulary correction (2026-08-06, prompted by review):** this run's
  queries open-coded the lane arithmetic. Two vocabularies for exactly that
  already existed and neither was used —
  [ADR-0162](../../adr/0162-leeway-co-ragged-function-pack.md)'s `chpack`
  (`CO_GATHER`, `RAGGED_STARTS`, …; shipped in
  `public/semistructured/leeway/chpack`, **not installed** on the trial
  server until now) and the older, already-installed
  `LEEWAY_VALUE_BY_TAG_EQUAL` / `LEEWAY_LU_MEMB_IDX_TO_VAL_IDX` UDFs.
  Rewriting Q1–Q5 in that vocabulary gives identical results and is
  **3× faster** — Q1 at 1M, best of 3: 0.068 s open-coded vs **0.023 s**.
  The gain is not the macro (ADR-0162 §SD1 is right that inlining is free);
  it is that the vocabulary resolves in *membership* space — `indexOf` on the
  flat tag lane, then membership-index → attribute-index — instead of
  materialising a per-attribute path vector per row. It also removes the
  zero-cardinality guard the open-coded form needed, because an attribute
  carrying no membership on that channel simply cannot be hit.
  Filed as: **[pain leeway → proposed:leeway-query-vocabulary-discoverability
  / usability.user-error-protection / S2]** — nothing on the path a task-level
  consumer walks (the leeway skills, `mapping`, the generated DML/RA packages)
  points at the query vocabulary; it was found only by review. The
  consequence here was not cosmetic: **every arm-B latency figure from the
  1M run is inflated ~3× by query form**, on top of the read-path effect
  already recorded above.
- **Prior-art review (same day):** a sibling experiment outside this
  repository had already recreated this benchmark on the *canonical leeway
  JSON mapping* rather than on boxer.facts
  (`runs/2026-08-05-m0-m3-1m/prior-art.md`). Reading it downgraded two
  findings and retracted the S1 above. It also supplies the technique this run
  missed outright — sorting the table by the extracted backbone expressions,
  which is arm D and reproduces the reference entry's clustered index — and
  two harness improvements: `clickhouse format --oneline -n` to keep the query
  files readable, and a `log_comment` JSON tag making `system.query_log` the
  result store, which is a shorter path to M5 than a fresh facts writer. The
  sibling's reference arm is *not* the pinned upstream DDL, so its ratios are
  internally consistent but not comparable with published JSONBench numbers;
  this run's arm A is, and passed the M0 ordering gate.
- **Run dir:** [`./runs/2026-08-05-m0-m3-1m/`](./runs/2026-08-05-m0-m3-1m/)

## 2026-08-06 — M4, 10M tier, arms A–D with cold runs — storage tax inverts to a saving; materialized backbone lands within 1.03–1.4× of native JSON

- **Build under test:** boxer `5a065288` + this trial's uncommitted work,
  ClickHouse 26.7.2.59, JSONBench pin `e6c7c98d`
- **Environment:** as the 1M run (`./runs/2026-08-06-m4-10m/environment.md`).
  Two differences that matter: **cold runs are measured** (the scoped
  `drop_caches` grant is in place, so `DROP_CACHES=1` reproduces upstream's
  procedure exactly), and the facts arms' queries use the leeway query
  vocabulary rather than open-coded lane arithmetic.
- **Attempted:** the full arm ladder A–D at the 10M development tier, plus the
  §9 Q6 100M-gate extrapolation.
- **Comparability with the 1M run:** arm A is comparable. **The facts arms are
  not** — their queries changed form, and the open-coded form the 1M run used
  is ~3× slower. Cold columns exist only from this run on.
- **Findings:**
  - **[broken leeway-dml-codegen → proposed:leeway-json-shredding /
    reliability.fault-tolerance / S2]** The trial's shredder aborted the whole
    ingest on the first undecodable document, where the reference loader skips
    and continues. The corpus contains malformed JSON-lines records — file 5
    line 91840 is truncated mid-string at exactly 65,536 bytes with its
    remainder on the next physical line — so the 10M tier could not be ingested
    at all until the same tolerance upstream documents was added. Until then
    the two arms did not hold the same corpus (evidence:
    `runs/2026-08-06-m4-10m/arm-b/ingest.time`, `apps/jsonbench/jsonbench.go`
    `undecodable`). Both arms now skip exactly the same 6 documents.
  - **[missing leeway → proposed:leeway-json-shredding /
    functional-suitability.functional-completeness / S3]** The absent `null`
    section is now exercised rather than hypothetical: 10 JSON nulls dropped
    at this tier (`nulls_dropped=10`). Still small; still real.
  - Positive maturity: **`leeway-ddl-codegen` / `leeway-dml-codegen`** carried
    121.2M attributes across five sections at 30,252 docs/s in 384 MB of RSS,
    unchanged, and the resulting table is **smaller on disk than ClickHouse's
    own JSON type** holding the same corpus.
  - Positive maturity: **ADR-0162 `chpack`** installed cleanly on a live 26.7
    server (`jsonbench chpack`, pack v1, 16 functions) and its vocabulary plus
    the older `LEEWAY_VALUE_BY_TAG_EQUAL` UDFs expressed all five queries with
    no escape hatch.
- **Solution size:** +38 lines (`apps/jsonbench/jsonbench_chpack.go`, a pack installer)
  and +12 in the ingester for error tolerance; `queries-facts.sql` went from
  five 2,000-character lines to readable multi-line SQL, and `measure.sh`
  normalises via `clickhouse format --oneline -n`. Arm D gained a committed
  `arm-d.sh`. **Manual interventions: one**, unchanged — arm C's redundant
  `has()` conjunct.
- **Results:** `runs/2026-08-06-m4-10m/results.md`. All four arms hold
  9,999,994 documents and return byte-identical results.
  **Storage: facts is 0.886× native JSON** — the 1M tier's 1.44× *inverts*,
  because the shred's support and membership lanes amortise across ten times
  the rows. **Latency with the backbone materialized (arm D): 1.03–1.4×;
  memory 0.8–3.1×.** Without it (arm B): 8.2–20.6× and *widening* with scale,
  because arm A's clustered index prunes to 115/1225 granules on Q4/Q5 while
  arm B reads every granule. **§4's hypothesis holds at 10M** (arm B
  2007/2007 on all five). **Arm C prunes 2 granules of 2394** — two tiers,
  same verdict: bloom filters over section value lanes cannot serve this
  workload.
- **100M gate (§9 Q6) — passes.** ~59 GiB for four arms plus ~18 GB of raw and
  staging against 262 GiB free; ~2 hours wall clock, dominated by the
  single-process facts ingest, which is the thing to parallelise first.
- **Arm A0 (added on review, same run dir):** arm A sorts on exactly the five
  paths the queries touch, and **a facts table cannot have that key** — it
  holds a mixture of document shapes, so most rows carry none of those paths.
  Comparing against arm A charges the data model for the benchmark's
  homogeneity. Arm A0 is the pinned DDL with `ORDER BY tuple()` and nothing
  else changed. Same 9,999,994 documents, byte-identical results, no
  `PrimaryKey` block in `EXPLAIN` at all.
  The clustered index was worth **1.02–1.73× latency** to arm A (most on Q4,
  which prunes to 115/1225 granules) and **9.8 % storage** (sorting low-
  cardinality columns compresses better). Against A0: **arm D is 0.72–1.09× on
  latency — faster on Q4 and Q5 — 0.99–1.15× on memory, and 0.958× on disk;
  arm B is 0.807× on disk and 8.0–12.8× on latency.**
  **On a like-for-like key the facts model with its backbone materialized is at
  parity with ClickHouse's native JSON type.** The remaining gap is entirely
  the read path. Filed as a protocol correction rather than a finding: §4's
  arm table lacked an unindexed reference, and every earlier ratio in this
  trial silently included the index advantage.
- **Arm A00 (added on review):** A0 still declares five typed paths and
  `max_dynamic_paths = 0`. That is the same class of workload knowledge as the
  sort key — for high-variability JSON there is no such five. A00 removes it:
  plain `JSON`, engine defaults.
  **The benchmark's queries do not run against it.** Every `data.<path>` is
  `Dynamic`; ClickHouse refuses `GROUP BY` on one (relaxable via
  `allow_suspicious_types_in_group_by`) and refuses `IN [...]` outright with no
  setting — Q3 cannot execute. 19 casts were needed, derived from the pin by
  `queries-native-dynamic.sh`. Filed as:
  **[note — workload, not toolbelt / S3]** the JSONBench query set is posed
  for a schema-hinted JSON column and does not port to unhinted JSON, which
  bounds what this trial's domain numbers can claim about heterogeneous
  corpora.
  A00 is also the **smallest table of any arm** — 1,150,367,898 bytes against
  A0's 1,814,273,851. The pinned DDL's `max_dynamic_paths = 0` is a 37 %
  storage pessimisation on this corpus; letting the engine discover paths and
  type them compresses far better.
  Against A00: **arm D is 0.28–0.69× on latency — 1.4–3.5× faster on every
  query — and 0.11–0.79× on memory, at 1.51× the storage; arm B is 2.2–6.4×
  on latency at 1.27× storage.** Three references are now reported (A, A0,
  A00) because the answer depends on which one a general fact store should be
  held to, and only A00 is a shape such a store could actually have.
- **M5 — results as facts + applet (2026-08-06):** both runs' timings and
  sizes are loaded into a benchmark-local facts table by `jsonbench results`
  (235 facts: 165 timings, 70 sizes) and read back by the **`jsonbench` book**
  (`apps/sqlapplet/bookjsonbench/`) — overview, latency by arm, and the tax
  ratios. §6's "the benchmark dogfoods the reporting layer it is measuring" is
  satisfied literally: the pages resolve their values with
  `LEEWAY_VALUE_BY_TAG_EQUAL` and re-align the ragged value lanes with
  `CO_GATHER`/`RAGGED_STARTS`, which is the read path the trial spent two runs
  measuring.
  One friction worth its line: the result facts tag their attributes with
  `LowCardRef` memberships, so a SQL page needs the **uint64 ids** —
  `6917529027641081861` and friends. There is no server-side name→id lookup,
  so the ids ride the pages as literals and `jsonbench vocab` exists only to
  print them. Filed as:
  **[pain leeway-ddl-codegen → proposed:leeway-vocab-introspection /
  usability.self-descriptiveness / S3]** — a Ref-membership table cannot be
  read by anyone who does not already hold the registry.
  The run directories keep the numbers as the provenance record; the facts
  table is the queryable copy, not the source of truth.
  **The book did not work when first committed, twice over**, and both were
  the trial's own doing:
  - **[pain — trial process / S3]** the pages named `FROM facts` unqualified.
    Hand-testing them with `clickhouse-client --database=jsonbench_results`
    hid it completely — the SQL was right and the deployment was wrong, and
    every page failed `UNKNOWN_TABLE` under a real applet, whose endpoint
    defaults elsewhere. Verifying a page means running it the way the applet
    does, not the way that makes it pass. A regression test now rejects an
    unqualified table reference in any page of this book.
  - **[missing nanopass-pass-pipeline → proposed:grammar1-identifier-params /
    functional-suitability.functional-completeness / S3]** the obvious fix —
    making the database a page parameter — does not parse.
    `FROM {db:Identifier}.facts` fails grammar1 with *no viable alternative at
    input '{'*, so an applet carrying it never mounts; `FROM {db:Identifier}`
    fails likewise. **Value** parameters (`{tier:String}`) parse fine and the
    pages use them. ClickHouse itself accepts the identifier form — verified
    against the live server — so this is a grammar1 gap, not a server
    limitation. The database is a literal until it closes.
  Only minting the book through `mintBooks` caught the second; running the SQL
  by hand never would have.
  **Both fixes left a worse property than either bug**, and the book was
  reshaped again to remove it: qualifying the table made the pages correct but
  still useless anywhere the benchmark data had not been loaded by hand. The
  pages now **carry their numbers** — a committed `values(...)` summary of the
  10M run — and reference no stored table at all, which a test enforces. Two
  pages (`jb-sizes`, `jb-latency`) replace the three that read the facts
  table.
  `jsonbench results` and the results-as-facts table remain for the full
  per-try set (165 timings, 70 sizes, queryable by run and arm), which is what
  §6 Reporting asks for; the book simply no longer depends on it. The run
  directories are still the provenance record, and the page summaries are
  generated from them rather than retyped.
- **UDF-family analysis (2026-08-06, on review):** the trial used both UDF
  families without examining how they relate. They are not alternatives:
  **`chpack` (ADR-0162) is the lane algebra; the `LEEWAY_*` readback family
  (ADR-0066,
  `public/semistructured/leeway/marshall/clickhouse/readback/`) is the
  leeway-schema-aware layer on top of it** — `HelperUDFsSQL()` emits the pack
  first, and `LEEWAY_LU_MEMB_IDX_TO_VAL_IDX` / `LEEWAY_UNFLATTEN` have already
  been retired onto `RAGGED_PARENT_IDS` / `RAGGED_NEST`.
  What the readback family adds, measured on `jsonbench_b_10m`:
  - **Correctness under non-uniform membership cardinality.** `CO_LOOKUP(keys,
    lane, k)` is `lane[indexOf(keys, k)]` and assumes the two lanes are 1:1
    co-indexed. On this table they are not — the kind tag carries zero `lmr`
    memberships — and `CO_LOOKUP` silently returns `blueskyEvent` for every
    row where `LEEWAY_VALUE_BY_TAG_EQUAL` returns the right collection. The
    readback form routes through `RAGGED_PARENT_IDS`, so zero- and
    multi-membership attributes resolve correctly.
  - **Fusion.** `LEEWAY_VALUE_BY_TAG_EQUAL(v,t,k,m)` is exactly
    `CO_LOOKUP(t, CO_GATHER(v, m), k)`, but the composition materialises a
    broadcast lane per row where the fused body does one `indexOf` and two
    scalar `arrayElement`s: **0.122 s vs 3.147 s**, a 26× difference on Q1 at
    10M. ADR-0162 §SD4's "fused bodies beat materializing per-instance lists"
    holds far harder here than the 1.5–2.4× it records.
  - **`LEEWAY_LIST_BY_TAG_EQUAL`** does tag→slice in one call, combining the
    membership indirection with the value lane's own raggedness. It is exactly
    what `queries-facts.sql` hand-rolls as
    `CO_GATHER(vals, RAGGED_STARTS(len))` + a tag lookup — and it is *more*
    correct, returning the whole slice where the hand-rolled form silently
    takes the first element only, the hazard that file's header documents.
  Filed as:
  **[pain leeway-read-access-codegen → proposed:leeway-query-vocabulary-discoverability
  / usability.self-descriptiveness / S3]** — the same discoverability finding
  as the chpack one above, one layer up: the purpose-built primitive for the
  trial's central operation existed and was found only by review.
  **[broken leeway-ddl-codegen → proposed:leeway-udf-provisioning-drift /
  reliability.maturity / S3]** — the trial server carried a *stale* readback
  family: `LEEWAY_LU_MEMB_IDX_TO_VAL_IDX`, retired in the repo, was still
  installed, while `LEEWAY_LIST_BY_TAG_EQUAL`, `LEEWAY_LU_ATTR_BY_TAG` and
  `LEEWAY_LU_MEMBS_OF_VAL_IDX` were absent. Every statement is
  `CREATE OR REPLACE`, so nothing removes a retired function, and unlike
  `chpack` the family carries no version marker to detect the skew.
  **Acted on 2026-08-06:** the facts queries now use
  `LEEWAY_LIST_BY_TAG_EQUAL` for the two array-valued sections instead of
  hand-rolling `CO_GATHER(vals, RAGGED_STARTS(len))`, and arms B and C were
  re-measured. Results stay byte-identical; hot runtimes drop **5–58 %**:

  | | Q1 | Q2 | Q3 | Q4 | Q5 |
  | --- | --- | --- | --- | --- | --- |
  | arm B | −17.6 % | **−57.9 %** | +1.2 % | −15.9 % | −14.7 % |
  | arm C | −5.0 % | **−55.6 %** | +1.5 % | −11.4 % | −16.3 % |

  Q2 more than halves, in time and in memory (1,130 → 650 MB), because it
  reads `/did` for every qualifying row: the hand-rolled form materialised the
  whole re-aligned string lane per row where the primitive slices one
  attribute. Q3 is unchanged — it touches only the small i64 lane, where the
  gather cost was never material. The spot-check that prompted this predicted
  15 % from Q4 alone and understated it by 4×.
  The correction also removes the silent truncation the file's own header
  warned about: `[1]` on a returned slice is an explicit choice where
  `CO_GATHER(vals, RAGGED_STARTS(len))` dropped everything past the first
  element without saying so.
  **Arm B against A00 is now 2.1–3.7×** (was 2.1–5.9×), and against A
  5.0–15.9×. Arm D is unaffected — its materialized columns never used the
  vocabulary. Tables above and the book's rows are synced.
- **UDF naming aligned to UPPER_SNAKE (2026-08-06), arms B and C
  re-measured.** The pack shipped camelCase (`coLookup`, `raggedStarts`)
  beside the read-back family's UPPER_SNAKE, and since the two became one
  stack a single expression routinely called both. The whole ADR-0162 roster
  is renamed — `CO_LOOKUP`, `RAGGED_PARENT_IDS`, `LEEWAY_PACK_VERSION` — with
  the `CO_`/`RAGGED_` prefixes kept so the algebra's two axes stay readable.
  22 files, the §SD6 roster rewritten, and a dated Update on the ADR recording
  it; `chpack.Version` is bumped to 2, which is what the marker is for.
  Two things the rename surfaced, both now called out in the ADR:
  `CREATE OR REPLACE` **cannot remove** a renamed function, so the 16 stale
  camelCase functions had to be dropped explicitly — the provisioning-drift
  finding above, reproduced deliberately. And this trial's queries still named
  the retired `LEEWAY_LU_MEMB_IDX_TO_VAL_IDX`, which was *still installed* on
  the server while absent from the repo; they now name `RAGGED_PARENT_IDS`
  and the retired function is dropped, so the re-measure proves nothing
  depended on it.
  **Arms B and C were re-measured** against the renamed pack. Results stay
  byte-identical to arm A, and hot runtimes move between −12 % and +12 % in
  both directions — run-to-run noise on a shared workstation, which is what a
  pure rename should look like. The tables above and the book's embedded rows
  are synced to the new evidence; the conclusions are unchanged.
- **Arm E — re-keying, the last unspent lever (2026-08-06).** Arm D
  materialises the backbone but keeps `ORDER BY ts` and prunes nothing. Arm E
  is arm D sorted on those columns in the reference's own order, derived from
  arm D's DDL so the two provably differ in the sort key alone (`arm-e.sh`).
  This is the protocol's §4 arm D; it is lettered E because the trial had
  already spent D on a read-path arm §4 did not anticipate.
  **It prunes**: 227 granules of 2,389 on Q4/Q5, the same ~10× reduction arm A
  gets, and the same fraction on Q2 and Q3.
  With the two levers isolated over identical data:
  **materialisation is worth 3.8–13.8×** at +18.8 % storage;
  **re-keying a further 1.07–1.76×** and it *saves* 9.3 % storage, since
  sorting low-cardinality columns compresses better. The gain lands exactly
  where pruning lands (Q4/Q5) and is absent on unfiltered Q1 — which is the
  attribution working. Together they take arm B's 4.5–24.3× off the table and
  put facts at **0.59–1.18× of the benchmark's own entry**, faster on Q4 and
  Q5, at 0.954× its storage.
- **Column handles (2026-08-06, on review).** The facts queries spelled every
  column physically (`tv:symbol:value:val:s:m:0:24:0::data`). ADR-0116's
  `ResolveColumnNames` exists for exactly this and the trial had not used it —
  the same discoverability finding, a third time. The queries now name
  `` `symbol:value` ``, `` `stringArray:len` `` and so on, and `jsonbench
  resolve` expands them against the target table before execution
  (`measure.sh` does this when `RESOLVE` is set). Support columns resolve
  alongside value columns, which is what makes the membership lanes reachable
  by name. Arms B and C were re-measured through the resolver; results stay
  byte-identical.
  One real limit found on the way, filed:
  **[missing nanopass-scope-resolution → proposed:resolve-column-names-with-aliases
  / functional-suitability.functional-completeness / S3]** — a handle bound by
  a `WITH <handle> AS alias` expression alias is **not** resolved; the pass
  visits column references in a SELECT's own scope but not the WITH-expression
  clause, so the handle ships unexpanded and the query fails
  `UNKNOWN_IDENTIFIER`. Verified against the live pass: the same handle used
  directly at its use site resolves. The trial's queries therefore inline the
  handles instead of binding them, which measured free (0.123 s vs 0.126 s on
  Q1) but costs the repetition the aliases existed to avoid.
- **Verdict:** [`./runs/2026-08-06-m4-10m/verdict.md`](./runs/2026-08-06-m4-10m/verdict.md)
- **Run dir:** [`./runs/2026-08-06-m4-10m/`](./runs/2026-08-06-m4-10m/)

## 2026-08-06 — close-out — 100M descoped, the ledger rolled up, the read-surface cluster handed to ADR-0171

- **Build under test:** boxer `b33bab3a`, ClickHouse 26.7.2.59, JSONBench pin
  `e6c7c98d`
- **Environment:** no measurements were taken. **This entry is not a run** — it
  records decisions and a verification pass over findings the two runs above
  produced, so it has no run directory and adds no numbers.
- **Attempted:** close the trial — settle the 100M tier, consolidate the
  findings ledger, and place the durable findings somewhere they can be acted
  on.
- **100M descoped.** The [§9 Q6](./README.md) gate passes on both counts
  (~77 GiB against 262 GiB free, ~2 h wall clock), so this is a judgement about
  value rather than feasibility, and it is recorded in the protocol at §6 and
  §8 M4 rather than left implied. The primary question was answered at 10M and
  arm E spent the last untried lever. What stays unmeasured: the scaling
  direction of the two ratios that moved between 1M and 10M — storage, which
  inverted from 1.44× to 0.807×, and arm A's pruning advantage, which widens
  with scale. Both are now single-tier results and the protocol says so.
- **Findings re-verified at `b33bab3a`, not carried over.** Every durable
  finding was re-checked against the tree before being rolled up, because
  several of the trial's own commits had changed the ground under them. Nine
  are open, and each was confirmed by inspection rather than by memory of the
  entry that filed it:
  - no shredder in-tree (`mapping/` exports schema constructors only);
  - facts has no `null` section (21 tagged-value sections, none of them null);
  - Ref-only memberships;
  - no `MATERIALIZED` emission anywhere under `public/semistructured/leeway/`;
  - the read-back family carries no version marker, where `chpack` has
    `Version` and `LEEWAY_PACK_VERSION`;
  - no vocabulary lookup reachable from SQL;
  - zero mentions of the SQL read vocabulary across all three leeway skills;
  - `paramSlot` is an alternative of `columnExpr` only — **this is the precise
    shape of the `{db:Identifier}` gap**, which the entry above recorded only
    as a parse failure: the rule exists and is simply not reachable from
    `tableExpr` / `tableIdentifier` / `databaseIdentifier`, which is why
    `{tier:String}` parses and the identifier form does not;
  - `ResolveColumnNames` unchanged since ADR-0116, so the WITH-alias gap
    stands.
  Two are fixed and recorded as such: the UDF naming split (`8ee2659e`) and the
  hand-rolled ragged read (`86c762f3`).
- **Ledger rolled up** into [README §7b](./README.md) — one row per proximate
  obstacle across both runs, deduplicated, with the retracted S1 kept visible
  because the retraction is the more useful record, and the positive-maturity
  lines kept beside the frictions. This logbook stays the per-run record; §7b
  is the cross-run view. The discoverability finding is shown once with its
  three occurrences rather than three times, which is what makes it read as the
  trial's central result rather than an incidental note.
- **Findings placed.** Ledger rows 5–8 — no materialized-column emission, the
  undiscoverable vocabulary, UDF provisioning drift, and vocabulary
  introspection — are one cluster and are carried by
  [ADR-0171](../../adr/0171-leeway-sql-read-surface.md) (proposed), which prices
  each against this trial's measurements. Rows 12 and 13 (grammar1 `paramSlot`
  placement, `ResolveColumnNames` WITH-aliases) sit in neither ADR tier per
  [CODINGSTANDARDS § What triggers an ADR](../../../CODINGSTANDARDS.md#what-triggers-an-adr)
  and are filed in §7b with the evidence a fix needs. Rows 1, 3 and 4 change
  the facts schema or add a package, so they need a design dialogue first, and
  this trial does not open one.
- **Rows 12 and 13 fixed, same day.** Both landed with a regression test naming
  the ledger row.
  Row 13 was the smaller one: `WITH <expr> AS name SELECT …` parses as the
  query-level `ctes` rule, which is a *sibling* of selectStmt, so a walk
  anchored at a scope's Node could never reach it — the selectStmt-level
  `withClause` was covered all along, which is why the gap looked arbitrary.
  The pass now visits each `ctes` node once, against the first SELECT it
  precedes.
  Row 12 cost more than filing it suggested, in three ways worth recording for
  the next person who reads "a one-line grammar fix": grammar2 needed the same
  alternative, because a slot has no canonical form to be rewritten into and
  `ValidateGrammar2` would otherwise reject any normalised query parameterised
  on its database; three call sites were dereferencing
  `tableIdentifier.Identifier()` with no nil check and would have panicked on
  the first parameterised table, so the change added
  `TableIdentifierName` / `DatabaseIdentifierName` and routed them through it;
  and the grammars needed regenerating with a hand-provisioned ANTLR jar, which
  the repo's `generate.sh` deliberately omits. A control regeneration of the
  *unchanged* grammar was run first and came back byte-identical, so the
  1,896-line generated diff is attributable to the change alone.
  Verified: the full `./public/db/clickhouse/...` suite (including the parse
  corpus, fuzz, round-trip-fidelity and semantic-equivalence lanes), plus play,
  leeway and keelson data — 52 packages, no failures.
- **Not done, deliberately:** the competence vault's `maturity` / `pain` fields
  are still `255` for every competence this trial exercised. The convention has
  those flip editorially, citing findings, and
  [ADR-0168](../../adr/0168-capmap-business-capability-corpus.md) has no 0..5
  rubric yet — authoring the rubric is a prerequisite, not part of this trial.
  The recurring `proposed:leeway-query-vocabulary-discoverability` slug has now
  been filed three times, which the [directory convention](../README.md) calls
  the editorial signal to author a corpus entry; that too is left to the vault's
  editor.
- **Solution size:** no code this entry. Documentation only — the protocol's
  §6/§7b/§8, this entry, and ADR-0171.
- **Results:** unchanged; `runs/2026-08-06-m4-10m/results.md` remains the
  numbers of record, and both it and the verdict remain `status: draft`
  pending human review.
- **Run dir:** none — see *Environment* above.
