---
type: reference
audience: package maintainer
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-24
---

# JSONBench-on-facts — logbook

Record of runs of the [jsonbench-on-facts](./README.md) trial, per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in its own `./runs/<YYYY-MM-DD-slug>/` directory.

**Compacted 2026-08-24.** These entries were narrative — the reasoning that
moved each headline, at length — and that narrative was being read as the
trial's result. Findings are now deduplicated once in
[README §7b](./README.md) and the numbers live in the `jsonbench` book and the
run directories; an entry here records what a run was, not what it argued. The
pre-compaction logbook is at
`45b9c1b8:doc/trials/jsonbench-on-facts/logbook.md`, and the corrections it
narrates are summarised in [README §5](./README.md).

Entry shape: date, milestone, one-line outcome; build under test; environment;
what was attempted; comparability with earlier entries; which §7b findings the
run produced; solution size; run directory.

## 2026-08-05 — M0–M3 at the 1M tier — the harness works; the tier is too small to price anything

- **Build:** boxer `5a065288`, ClickHouse 26.7.2.59, JSONBench pin `e6c7c98d`
  (2026-03-08)
- **Environment:** [`runs/2026-08-05-m0-m3-1m/environment.md`](./runs/2026-08-05-m0-m3-1m/environment.md)
  — 16C/32T workstation, 93 GiB, NVMe behind dm-crypt, server TZ
  Europe/Zurich. Three protocol deviations recorded there: nothing vendored
  (licence), **no cold column** (the cache drop needed a privilege not yet
  granted), and a non-UTC server timezone.
- **Attempted:** M0 (pin, run discipline, arm A), M1 (mapping + ingest), M2
  (arm B), M3 (arm C), all at 1M. M4–M6 not reached.
- **Outcome:** the M0 gate passes — arm A's relative query ordering
  (Q1 < Q3 < Q4 < Q5 < Q2) is identical to the published ordering and stable
  across two runs, with absolute hot numbers within roughly ±40 % of the
  published `m6i.8xlarge` figures on different hardware and a newer engine.
  All three arms byte-identical. §3's hypothesis confirmed: the unmodified
  facts table prunes nothing. Arm C's skipping indices are built and used by
  the planner but prune 1 granule of 245, and an on/off A/B on the same table
  shows no runtime difference.
  **This run's facts-arm ratios do not stand** and are not reproduced here:
  the queries open-coded the lane arithmetic (~3× slower than the query
  vocabulary), there was no cold column, and at 1M every arm-A query finishes
  in tens of milliseconds, so timer granularity and scheduler noise are a
  material fraction of each measurement. The tier is a smoke test for the
  harness. Arm A's numbers and the qualitative findings stand.
- **Findings produced:** §7b rows 1, 3, 4, 9, 10, 11, plus the retracted S1.
  Two same-day review passes moved them: a decomposition of the loaded tables
  relocated the cost from the storage model to the read path (the membership
  machinery is 8 % of arm B's disk footprint; 81.5 % is the shredded values;
  `id:naturalKey`, a hash arm A has no equivalent of, is 8.6 % on its own),
  and a prior-art review of a sibling experiment outside this repository
  downgraded two findings and retracted the S1 outright. That sibling also
  supplied the technique this run missed entirely — sorting the table by the
  extracted backbone expressions, which became arm E — and two harness
  improvements adopted at M4: `clickhouse format --oneline -n` to keep the
  query files readable, and a `log_comment` JSON tag making
  `system.query_log` the result store.
- **Solution size:** 693 lines of Go (`apps/jsonbench/`) + 351 of harness.
  **Manual interventions: one** — arm C's redundant `has()` conjunct.
- **Run dir:** [`./runs/2026-08-05-m0-m3-1m/`](./runs/2026-08-05-m0-m3-1m/)

## 2026-08-06 — M4 at the 10M tier, arms A/A0/A00/B/C/D/E with cold runs — the tier that answers the question

- **Build:** boxer `5a065288` plus this trial's uncommitted work, ClickHouse
  26.7.2.59, pin `e6c7c98d`
- **Environment:** [`runs/2026-08-06-m4-10m/environment.md`](./runs/2026-08-06-m4-10m/environment.md)
  — as the 1M run, with two differences that matter: **cold runs are
  measured** (a scoped `drop_caches` grant reproduces upstream's procedure
  exactly), and the facts arms' queries use the leeway query vocabulary rather
  than open-coded lane arithmetic. The four MergeTree serialization settings
  the pinned DDL asks for are already server defaults on 26.7.2.59, so the DDL
  is a no-op with respect to them.
- **Attempted:** the full ladder at 10M, plus the 100M gate extrapolation.
  Arms A0, A00 and E were all added *on review* during this run — A0 and A00
  because §3's arm table lacked any reference without workload-specific schema
  knowledge, E because the two levers turned out to be separable.
- **Comparability:** arm A is comparable with the 1M run. **The facts arms are
  not** — their queries changed form. Cold columns exist only from this run on.
- **Outcome:** the trial's numbers of record. All arms hold 9,999,994
  documents and return byte-identical results. Latency, memory and sizes are
  in the [`jsonbench` book](../../../apps/sqlapplet/bookjsonbench/); what they
  establish is [README §5](./README.md). In brief: facts with the backbone
  materialized is at parity with native JSON declaring the same paths;
  storage inverts from a 1.44× cost at 1M to 0.886× at 10M; arm C prunes 2
  granules of 2,394; the two levers are worth 3.8–13.8× and a further
  1.07–1.76×.
- **Findings produced:** §7b rows 2 (the shredder aborted on the corpus's
  malformed records, so until it was fixed the two arms did not hold the same
  corpus), 4 (now exercised — 10 nulls dropped), 6, 7, 8, 12, 13, 14, 15.
  Row 6 was filed three times in this run alone: `chpack` had shipped and was
  not installed; `LW_LIST_BY_TAG_EQUAL` did by design what the queries
  hand-rolled, and did it without the silent first-element truncation
  (arms B and C re-measured, results byte-identical, hot runtimes −5 % to
  −58 %, Q2 more than halving in time and memory); and ADR-0116's
  `ResolveColumnNames` existed for the physically-spelled column names the
  queries carried.
- **Also this run.** The UDF roster was aligned to UPPER_SNAKE (22 files,
  `chpack.Version` → 2) and arms B and C re-measured: ±12 %, workstation
  noise, which is what a pure rename should look like for macros inlined at
  analysis time. The rename surfaced that `CREATE OR REPLACE` cannot remove a
  renamed function — 16 stale camelCase functions had to be dropped by hand,
  reproducing row 7 deliberately. **The two UDF families are not
  alternatives:** `chpack` (ADR-0162) is the lane algebra and the `LEEWAY_*`
  read-back family (ADR-0066) is the leeway-schema-aware layer on top of it.
  What the upper layer adds, measured on this run's own table: correctness
  under non-uniform membership cardinality (`CO_LOOKUP` assumes 1:1
  co-indexing and silently returns the wrong value on a table where the kind
  tag carries zero memberships), and fusion —
  `LW_VALUE_BY_TAG_EQUAL(v,t,k,m)` is exactly `CO_LOOKUP(t, CO_GATHER(v, m), k)`
  but the composition materialises a broadcast lane per row where the fused
  body does one `indexOf` and two scalar `arrayElement`s: **0.122 s against
  3.147 s** on Q1 at 10M, far harder than the 1.5–2.4× ADR-0162 §SD4 records.
- **M5 done.** Both runs' timings and sizes load into a benchmark-local facts
  table via `jsonbench results` (235 facts) and the `jsonbench` book reads
  them back. The book **did not work when first committed, twice over**, and
  both were the trial's own doing (rows 14 and 12); only minting it through
  `mintBooks` caught the second, and running the SQL by hand never would have.
  Both fixes then left a worse property than either bug — pages that were
  correct but useless anywhere the data had not been loaded by hand — so the
  book was reshaped a third time to **carry its numbers** as a committed
  `values(...)` summary, referencing no stored table at all, which a test
  enforces. `jsonbench results` remains for the full per-try set.
- **Solution size:** +38 lines (`jsonbench_chpack.go`) and +12 in the
  ingester; `queries-facts.sql` reformatted from 2,000-character lines to
  readable multi-line SQL. **Manual interventions: one**, unchanged.
- **Run dir:** [`./runs/2026-08-06-m4-10m/`](./runs/2026-08-06-m4-10m/)

## 2026-08-06 — close-out — 100M descoped, the ledger rolled up, the read-surface cluster handed to ADR-0171

- **Build:** boxer `b33bab3a`. **This entry is not a run** — no measurements,
  no run directory. It records decisions and a verification pass.
- **100M descoped for arms A–E**, with the gate passing on both counts; the
  reasoning and what it leaves unmeasured are in [README §8](./README.md).
- **Findings re-verified at `b33bab3a`, not carried over.** Every durable
  finding was re-checked against the tree before roll-up, because several of
  the trial's own commits had changed the ground under them. Nine open, each
  confirmed by inspection rather than by memory of the entry that filed it;
  two fixed (`8ee2659e`, `86c762f3`). The re-check also sharpened row 12 from
  "a parse failure" to its precise shape: `paramSlot` exists and is simply
  unreachable from table position.
- **Ledger rolled up** into [README §7b](./README.md), deduplicated, with the
  retracted S1 kept visible and the positive-maturity lines kept beside the
  frictions. Showing the discoverability finding once with its three
  occurrences is what makes it read as the trial's central result rather than
  an incidental note.
- **Findings placed.** Rows 5–8 are one cluster, carried by
  [ADR-0171](../../adr/0171-leeway-sql-read-surface.md). Rows 12 and 13 sit in
  neither ADR tier and were fixed the same day, each with a regression test
  naming the ledger row; row 12 cost three things a "one-line grammar fix"
  does not suggest (see README §7b). Rows 1, 3 and 4 need a design dialogue
  first, and this trial does not open one.
- **Run dir:** none.

## 2026-08-06 — arm J: the canonical JSON mapping at 100M — a new arm, and two silent defects found

- **Build:** boxer `69db7c3e` plus this entry's uncommitted work, ClickHouse
  26.7.2.59, pin `e6c7c98d`
- **Environment:** [`runs/2026-08-06-jsonmap-100m/environment.md`](./runs/2026-08-06-jsonmap-100m/environment.md)
  — as the 10M run; its table states precisely how this arm differs from the
  facts arms.
- **Attempted:** load the descoped 100M tier under `mapping.LoadJsonMapping`
  — the canonical leeway JSON mapping rather than the facts schema — and find
  query scenarios the benchmark's five cannot express.
- **Comparability:** a **new arm, not a re-run**. Schema, read vocabulary and
  sort key all differ from arms B–E; figures against A/A0/A00/B are
  cross-tier from the 10M run, except arm B, which was rebuilt and reproduced
  its recorded total to 0.26 %.
- **Outcome:** 99,999,968 documents, 1,200,650,881 attributes, 336 s on 8
  shards (297,619 docs/s aggregate — a 5.5× speed-up on 8 shards, which is
  what the 10M entry named as the thing to parallelise first). The five
  benchmark results are byte-identical to the recorded arm A output at 10M; no
  reference arm was loaded at 100M, so the 100M outputs are recorded but
  unattested. What the arm establishes is [README §5a](./README.md) and the
  head-to-head in [§6](./README.md).
- **Findings produced:** §7b rows 16 and 17, plus the recurrence of the
  row-identity mistake — the second time the trial charged a leeway table for
  a column the A-family does not have at all. Positive maturity: the
  addressing machinery is 2.3 % of the table; the canonical mapping needs no
  UDFs at all; sharding the ingest works.
- **Solution size:** 599 hand-written lines of Go plus a 1,922-line generated
  builder; the shredder is reused unchanged from the facts arm, which is why
  both arms provably hold the same 121,205,987-attribute decomposition at 10M.
  **Manual interventions: one** — the repeated sub-select forced by row 16.
- **Run dir:** [`./runs/2026-08-06-jsonmap-100m/`](./runs/2026-08-06-jsonmap-100m/)

## 2026-08-07 — the query vocabulary moves under one `LW_` namespace — no re-measurement, and why

- **What changed.** Every leeway SQL UDF the trial calls was renamed onto a
  single namespace (ADR-0162 Update 2026-08-07): `CO_GATHER` →
  `LW_CO_GATHER`, `LEEWAY_VALUE_BY_TAG_EQUAL` → `LW_VALUE_BY_TAG_EQUAL`, and
  so on. Same bodies, same dependency order. `chpack.Version` 2 → 3.
- **What was touched.** The protocol's `.sql` files, because they are meant to
  be re-run and the old spellings do not resolve against a current server.
- **What was deliberately not touched: everything under `runs/`.** Those files
  record which functions were installed on a server on a given day —
  including one retired in the repository and still present there, which is
  the observation ADR-0171 was written around. Re-spelling a recorded
  observation to a name that did not exist when it was made would destroy the
  evidence for the finding it produced. The consequence, stated in
  [README §8](./README.md): the numbers under `runs/` were produced by SQL
  naming functions the current build no longer installs.
- **No re-measurement.** The 2026-08-06 rename was measured (±12 %) and that
  established a pure rename is performance-neutral for SQL UDFs; a second
  identical experiment would be ceremony.
- **Provisioning drift, one notch better.** `chpack.Install` now drops
  withdrawn names, so this rename does not add 23 more stale functions to
  whatever a server already carries. Detection is still absent — §7b row 7.
- **Run dir:** none.

## 2026-08-10 — a recovered side probe of per-column codecs — two of its six columns are too sparse to read

- **Provenance, and why to distrust this entry.** Not a run of the protocol.
  On 2026-08-06 a `codecprobe` database was built by hand through
  `clickhouse-client`, storing single columns of `jsonbench_j_10m.json` under
  19 codec choices. No run directory, no script, no environment record;
  reconstructed from `system.query_log` and `system.tables` because the
  database was being dropped and the numbers exist nowhere else. **It is not
  reproducible** — the source dataset has been deleted.
- **Protocol, as recovered.** Per variant, one
  `CREATE TABLE codecprobe.<name> (v <type> CODEC(<codec>)) ENGINE=MergeTree
  ORDER BY tuple()` then `INSERT … SELECT <column> FROM jsonbench_j_10m.json`.
  All 19 tables hold the same 9,999,994 rows.
- **Two columns carry almost no data, and their rows mean nothing.** `f64` and
  `bool` occupy 0.036 and 0.037 bytes/row uncompressed — those sections are
  all but empty in this dataset, which is why FPC(12), Gorilla and plain
  ZSTD(3) come out byte-identical at 56,898 on the float column: the
  specialised stage is not exercised and ZSTD is absorbing a near-constant
  column. **Read no codec preference from those two rows.**

  | column | B/row | codec | bytes | vs baseline |
  |---|---:|---|---:|---:|
  | `int64` | 5.94 | *(none)* | 59 433 386 | 1.000 |
  | | | T64, LZ4 | 40 055 704 | 0.674 |
  | | | T64, ZSTD(3) | 30 933 310 | 0.520 |
  | | | ZSTD(3) | 30 514 717 | 0.513 |
  | | | Delta, ZSTD(3) | 26 219 413 | 0.441 |
  | | | **DoubleDelta, ZSTD(3)** | **25 146 863** | **0.423** |
  | `blake3hash` | 40.01 | NONE | 400 071 808 | 1.000 |
  | | | LZ4 | 321 657 448 | 0.804 |
  | | | ZSTD(3) | 320 119 605 | 0.800 |
  | hash\[1..16] | 24.01 | NONE | 240 071 923 | 0.600 |
  | | | ZSTD(3) | 160 116 386 | 0.400 |
  | `string` | 115.61 | ZSTD(3) | 1 156 144 340 | 1.000 |
  | | | ZSTD(12) | 1 110 379 092 | 0.960 |
  | `float64` | 0.04 | — | *too sparse to read* | — |
  | `bool` | 0.04 | — | *too sparse to read* | — |

- **What the four readable columns support.** On integers the ordering is
  DoubleDelta < Delta < plain ZSTD(3) < T64 — and `T64`, whose name suggests
  it is *the* integer specialist, is the worst compressed variant here at both
  backings. On a blake3 hash no codec does much (0.80 at best), which is what
  incompressible-by-construction looks like; halving the identifier first
  saves more (0.60) than any codec on the full one, and the two compose
  (0.40). On strings, ZSTD(12) returns 4 % over ZSTD(3).
- **One live decision this touches.** ADR-0168 §SD4 assigns ZSTD(12) to
  `factsschema`'s `textArray` on the argument that prose has redundancy worth
  the heavier setting. The 4 % here is the only measurement of that split the
  trial holds, and it is against JSONBench's strings, not prose — so it
  neither confirms nor refutes the ADR's reasoning. Recorded so the next
  person weighing that codec knows a number exists and knows what it is not.
- **Run dir:** none. See the provenance caveat.
