---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# fs snapshot store M0 — logbook

Chronological, append-only record of runs of the
[M0 verification trial](./README.md), per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in its own `./runs/<YYYY-MM-DD-slug>/` directory.

## 2026-08-19 — M0, all eleven checks — the design holds; four generator refusals to route around, one of which needs a decision

- **Build under test:** boxer `deb4be09`; ClickHouse 26.7.3.19 (server
  timezone Europe/Zurich); rclone v1.74.3; Go 1.26.5;
  `lukechampine.com/blake3` v1.4.1. Corpus for check 9: this repository's
  `public/` tree at that commit — 3 712 files, 31.1 MiB.
- **Environment:** 32 logical cores (AMD Ryzen AI Max+ Pro 395), 93 GiB
  memory, ClickHouse data on the host's local filesystem. Host load average
  during check 8's repeats: ~28 — see the finding on it below.
- **Attempted:** the eleven checks of the
  [plan page](../../adr-background-work/iofs-clickhouse-snapshot-store-plan.md)
  §M0, plus the per-mount purge SD1 promises, so ADR-0198 SD11 can be
  decided on evidence rather than on assumption.

### What each check answered

| # | Check | Verdict |
| --- | --- | --- |
| 1 | one block = one compressed block at `index_granularity = 1` | **pass** — 200 blocks give 201 marks, 201 distinct compressed-file offsets, every mark at decompressed offset 0, at 1 MiB and at 256 KiB, in the facts `blobArray` value stream and in a plain `String` alike. A point read of one 1 MiB block costs exactly 1.00 MiB of `ReadCompressedBytes`. |
| 2 | `ttl_only_drop_parts = 1` and drop latency | **pass, with a constraint** — a whole-expired partition's part is replaced by an empty one; a *partially* expired part keeps its expired rows through background merges (1000 rows still there after 3 s; `ttl_only_drop_parts = 0` drops to 500) and only `OPTIMIZE … FINAL` removes them. Latency is bounded by `merge_with_ttl_timeout`, 14 400 s by default, ~0.2 s at 1. |
| 3 | `now()` in a `ROW POLICY`; `TTL` on a `DateTime64` plain | **pass** — both accepted, and the policy filters as expected. `TTL "lc:expiresAt:z64:4::0:"` needs no `toDateTime` wrapper beside `PARTITION BY toYYYYMMDD(…)` on the same plain. |
| 4 | `MATERIALIZED` tree columns by `ALTER` | **pass on four of five** — the `ALTER` takes on the facts-shaped table; `dir` may depend on `name`; the four are hidden from `SELECT *` (189 columns in the table, 185 in `SELECT *`), so the positional decode is untouched; the bloom filter over `dir` prunes 197 granules to 12. The fifth, `VerifySchema`, fails — see G4. |
| 5 | `recordstore/gen` over the facts `TableDesc` on a foreign table | **pass after three refusals** — G1–G3 below. Once past them the store generates, compiles, `EnsureTable`s the table with our clauses, takes 2 000 rows under one `(id, ts)` in 108 ms and reads them back through `Scan` with an `ExtraPredicate` over the physical columns, 0 mis-decoded. |
| 6 | the materialised view under a batched multi-mount insert | **pass** — one insert of 5 000 rows across 500 mounts, 500 of them root rows, puts exactly 500 rows in `fssnap`, one per mount. |
| 7 | `FULL OUTER JOIN` default filling | **pass** — `join_use_nulls = 0` fills the missing side with `''`, which is never a valid `io/fs` path, so the diff idiom classifies added / removed / same correctly. It is a *dependency* on that setting, not a property of the join. |
| 8 | insert throughput, facts-shaped against bespoke | **measured, loosely** — roughly 2× slower facts-shaped at 1 MiB blocks. See the finding on measurement noise. |
| 9 | compression on one representative mount | **pass** — the facts-shape overhead is 6.1 MiB uncompressed → **39 KiB compressed** (ratio 159) beside 31.3 MiB → 7.7 MiB of block data (ratio 4.0). Whole mount on disk: 8.45 MiB at 1 MiB/ZSTD(3), 9.03 at 1 MiB/ZSTD(1), 8.65 at 256 KiB/ZSTD(3), 9.06 at 256 KiB/ZSTD(1). Marks cost 178 B per block row facts-shaped against 12 B bespoke — 0.02 % of a 1 MiB block. |
| 10 | BLAKE3 subtree chaining values; `BLAKE3()` throughput | **pass, by two routes** — see B1 below. `blake3.Sum256` agrees with ClickHouse `BLAKE3()` (`d74981ef…` for `hello world`), so the audit query is sound; ClickHouse hashes at 2.5–2.8 GiB/s including the read and decompress, Go at ~20 GiB/s. |
| 11 | rclone contracts | **pass, with one wrinkle** — see R1. `rclone serve sftp --stdio` exists, honours `--exclude` on the serving side, exposes symlinks as `.rclonelink` under `--links`; `rclone serve s3` over the stdio SFTP remote lists buckets, lists a bucket recursively without a delimiter, and `cat`s the exact bytes. |

### Findings

- **[broken leeway-ddl-codegen → proposed:recordstore-foreign-tabledesc /
  functional-completeness / S3]** (G1) `recordstore/gen` refuses the
  `boxer.facts` `TableDesc` under any other table name — *"Input.TableName
  `fsmeta` and the TableDesc's own name `facts` disagree"*. The fix is one
  line at the call site (copy the `TableDesc`, set
  `DictionaryEntry.Name`), and the physical columns are unchanged by it, but
  nothing says so and the check reads as if the two were meant to be the
  same thing. (evidence: `runs/2026-08-19-m0/checks-05-08-09-10-go.txt`,
  `gen/generate.go.tmpl`)
- **[pain leeway-ddl-codegen / functional-completeness / S4]** (G2)
  `gen.Input.Database` must match `[a-z][a-z0-9]*`, so a scratch database
  cannot be called `boxer_m0`. Harmless for the real store (`boxer`), and
  the error names the rule. (evidence: same file)
- **[broken leeway-dml-codegen → proposed:recordstore-multi-row-key /
  functional-correctness / S2]** (G3) the emitted `Ingest<Kind>` verb cannot
  write this store's rows at all: it refuses two rows sharing a key
  (`ErrDuplicateIngestKey`), and the fs store writes one row per *path*
  under one mount id, thousands at a time. It also calls
  `Begin(id, ts, <empty envelope>)`, so a DTO's `naturalKey` and `expiresAt`
  are silently dropped even for a single row. `Begin`/`Add`/`Commit`
  directly carries both correctly. The plan page's M1 and M2 wording ("an
  entry row per node via `Ingest`") is wrong and is corrected in the ADR
  update. (evidence: `runs/2026-08-19-m0/checks-05-08-09-10-go.txt`,
  `gen/exercise.go.tmpl`)
- **[broken leeway-ddl-codegen → proposed:recordstore-verifyschema-materialized
  / functional-correctness / S2]** (G4) the emitted `VerifySchema` fails
  against a table carrying the design's `MATERIALIZED` tree columns: it
  compares `DESCRIBE TABLE`, which lists them, against the generated schema,
  which does not — *"table has 189 columns, the generated schema expects
  185"*. The decode it guards is not affected (it reads `SELECT *`, which
  hides them, and the same run decodes 2 000 rows with 0 errors), so the
  check is stricter than the contract it exists to protect. `DESCRIBE
  TABLE`'s `default_type` marks exactly the four, so filtering
  `MATERIALIZED` and `ALIAS` would align it — but that is a change to a
  shared generator, so it is a decision, not a trial fix. (evidence:
  `runs/2026-08-19-m0/checks-05-08-09-10-go.txt`,
  `runs/2026-08-19-m0/check-04-materialized.txt`)
- **[pain leeway-ddl-codegen / functional-correctness / S3]** (E1) the
  compact page's `ext` expression is wrong at both edges: it gives the root
  row `.` an extension of `.`, and reads `.gitignore` as being entirely an
  extension. `if(position(substring(name, 2), '.') = 0, '', concat('.',
  splitByChar('.', name)[-1]))` fixes both and gives `.hidden.txt` its
  `.txt`. (evidence: `runs/2026-08-19-m0/check-04-materialized.txt`)
- **[broken policy-repo-stats → proposed:clickhouse-projection-purge /
  functional-completeness / S3]** (P1) SD1's per-mount purge (a lightweight
  `DELETE`) and SD10's fleet-profile `by_path` projection are mutually
  exclusive at this server's defaults: with a projection present, `DELETE`
  throws under `lightweight_mutation_projection_mode = THROW`. Passing the
  setting on the statement does not help — `getSetting()` reads back
  `rebuild` and the guard still fires; the *table*-level `ALTER TABLE …
  MODIFY SETTING` is what it reads. Purge on 25 rows without a projection:
  58 ms. (evidence: `runs/2026-08-19-m0/extra-purge-projection.txt`)
- **[pain observability-continuous-profiling / performance-efficiency / S3]**
  (T1) check 8's ratio is not pinned by this run. Across six repeats the
  bespoke arm alone ranged 146–436 MiB/s at a host load average of ~28, and
  the facts/bespoke ratio ranged 1.3×–5.4×. Two earlier full-probe runs at
  lower load gave 1.70×/2.19× and 1.77×/2.94× (1 MiB / 256 KiB). What is
  stable in every run: facts-shaped is slower, by a factor near two, not by
  an order of magnitude — enough to decide SD11's `fsdata` shape, not enough
  to quote. A re-run on a quiet host is M0b. (evidence:
  `runs/2026-08-19-m0/check-08-throughput.txt`)
- **[missing similarity-ncd → proposed:blake3-subtree-chaining-values /
  functional-completeness / S4]** (B1) `lukechampine.com/blake3` exposes no
  subtree chaining value at a block offset: `Hasher` has none, and the
  primitives that compose one live in the `guts` subpackage, where
  `bao.compressGroup` — the ~25 lines that actually do it — is unexported.
  Reimplementing it over the exported `guts` primitives works. The supported
  alternative is better: `bao` takes a chunk-group exponent, so `group = 10`
  is 1 MiB groups, `EncodeBuf` produces a 200-byte outboard tree for 4 MiB
  of data whose root *is* `blake3.Sum256` of the file, and `VerifyChunk`
  accepts one block and rejects a corrupted one. (evidence:
  `runs/2026-08-19-m0/checks-05-08-09-10-go.txt`, `gen/blake3.go.tmpl`)
- **[pain config-env-var-registry / operability / S4]** (R1) rclone's `sftp`
  backend runs the `ssh=` command **twice**: once with `-s sftp`, and once
  with `echo ${ShellId}%ComSpec%` to probe the remote shell. Setting
  `shell_type=unix` on the remote suppresses the probe and leaves exactly
  one invocation. M5's head must either tolerate the probe or the documented
  spelling must carry `shell_type=unix`. (evidence:
  `runs/2026-08-19-m0/check-11-rclone.txt`)
- **positive maturity, `leeway-read-access-codegen`:** `SharedRA` bound to
  `factsschema/ra` under a foreign table name worked without a word of
  configuration beyond `Scaffold.Stylable`, and no read-access code was
  re-emitted. The generated `Scan<Kind>(ScanOpts{ExtraPredicate})` carried a
  key-range predicate over the physical columns and decoded 2 000 rows with
  no errors, first try.
- **positive maturity, `leeway-ddl-codegen`:** `gen.Input.DDL` (the ADR-0102
  seam) rendered every clause the design asks for — `PARTITION BY` on a
  lifecycle plain, a three-column `ORDER BY`, a bare `TTL`, three settings —
  and `EnsureTable` created the table from it against a live server.

### Decisions this run supports (ADR-0198 SD11)

Recorded in full in ADR-0198's `## Updates` entry for 2026-08-19; in short:
the block ordinal stays a `naturalKey` suffix; `fsdata` stays facts-shaped;
the tree columns are the hot attributes to materialise and no others yet;
the fleet profile's `by_path` projection is **not** default. Macro spellings
were not touched by any check and stay open.

- **Solution size:** the trial's own artefacts — 8 SQL/shell files (~330
  lines) under `sql/`, 7 Go templates (~700 lines) under `gen/`, `probe.sh`
  and `rclone-contracts.sh` (~180 lines). The generated store the probe
  builds and throws away is 18 046 lines, which is the number that decided
  it should not be committed.
- **Results:** none as facts — M0 measures the substrate, not a domain.
- **Run dir:** [`./runs/2026-08-19-m0/`](./runs/2026-08-19-m0/)
