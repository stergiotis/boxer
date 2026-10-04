---
type: adr
status: accepted
date: 2026-09-25
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-25
---

# ADR-0259: jackstay — a guided, resumable sync of tables from one ClickHouse server to another

## Context

Boxer's stores are ClickHouse tables — `boxer.facts`, the generated record
stores, the lading snapshot store, watchbill, the data catalog — and moving
their contents to another server has no support in the tree. Nothing reads
`BACKUP`, `INTO OUTFILE` or `remote()`; the one migration recipe under
`doc/migration/` is ALTER-based and table-specific.

The stores are **ephemeral**: losing a copy costs a re-ingest, not a
business record. Source and target servers are assumed to run **close
ClickHouse versions**, which makes the `Native` format a safe wire between them.

What is wanted is not a dump tool but a **guided process** an operator steps
through, in a GUI or on the command line:

1. discover the databases reachable on a server with the given credentials;
2. discover their tables and propose how to bring the target's structure in
   line with the source;
3. describe the differences in *content*, by comparing the two sides' rows by
   primary key;
4. sync tables — in full, only what is missing, or a sample;
5. watch the sync progress, survive interruption, and keep an eye on the
   target's disk.

Two facts from the tree shape the answer:

- **Leeway tables are recognisable and restorable from their column names.**
  The data catalog ([ADR-0170](./0170-data-catalog-competence.md))
  classifies tables, restores their leeway description, and relates two shapes
  (`equal`, subset, …). A structure step can reuse that relation instead of
  inventing a column diff for leeway tables.
- **Membership ids are compile-time constants, not database rows**
  ([ADR-0171](./0171-leeway-sql-read-surface.md): "there is no server-side
  name→id lookup"). A byte-faithful row copy preserves them, so the sync does
  no id remapping. What it cannot check is that the binaries reading the target
  share the source's vocabulary. That remains the operator's concern.

## Decision

We will add **jackstay**: one engine package with two front ends, a CLI
(`boxer jackstay …`) and an imzero2 app. They share a
**plan** document. Each step of the process reads both servers and adds its
findings and proposals to the plan. The sync step executes the plan. Rows
move **relayed through the tool**: a `SELECT … FORMAT Native` HTTP stream
from the source is piped, undecoded, into an `INSERT … FORMAT Native` on the
target. Progress is kept in a **local journal beside the plan**, so the
servers see nothing but the synced tables.

### SD1 — The plan is the unit of work

Every step writes into one plan document (JSON, versioned): the two
endpoints (credentials by reference to the environment, never inline), the
chosen databases and tables, each table's structure verdict and DDL, the
content diff, and the chunk list with each chunk's sync mode. The GUI wizard and
the CLI subcommands (`discover`, `structure`, `apply-ddl`, `diff`, `sync`, `status`) are
two editors of the same document. The operator can read and edit a plan
before `sync` runs it, and an interrupted run resumes from its plan. A plan
names the source's state at planning time. `sync` re-reads the source's
per-chunk digests (SD4) before copying a chunk, and it re-plans any chunk
whose digest has moved rather than trusting a stale plan.

### SD2 — Discovery reads `system.*` only

Discovery reads `system.databases`, `system.tables` and `system.columns` on
both endpoints, plus `version()` and `uptime()`. System databases are
listed but not offered, and temporary tables are not discovered. A
difference in the servers' year.month release is shown as a note, not a
refusal: "close versions" is the operator's assumption to keep.

### SD3 — Structure: a verdict per table, DDL proposed and never run unseen

Each source table gets one verdict against the target. Columns are matched
by physical name, because rows are copied by name:

| Verdict | Condition | Proposed action |
| --- | --- | --- |
| `unsupported` | the source engine holds no rows a copy can move (views, dictionaries, integration engines, a materialized view's inner table), or the target name is taken by such a table | none; listed with the reason |
| `create` | table absent on target | the source's `create_table_query`, retargeted: database-renamed if mapped, `UUID` clause dropped, `IF NOT EXISTS` added |
| `extend` | the target lacks source columns; the target may also have columns of its own | `ALTER TABLE … ADD COLUMN … AFTER …` for each missing one, in source order |
| `narrower` | the target has every source column, and more | none; the target's own columns take their defaults |
| `identical` | same columns and types, same sorting key | none |
| `incompatible` | a shared column differs in type, or is stored on one side and computed (`ALIAS`, `EPHEMERAL`) on the other; the sorting keys differ; or the DDL cannot be derived | none; the table is excluded, with the reason |

Differences that do not stop a copy are **notes** on the verdict. These
include an engine or partition key that differs, a type that differs only in
`LowCardinality` wrapping (RowBinary bytes are equal, and a `Native` insert
converts), a `MATERIALIZED` column the target recomputes, and a replicated
engine whose DDL names a Keeper path.

Each verdict carries the **copy column list**: the explicit, ordered list
that SD4 hashes and SD5 selects and inserts. It holds the source's stored
columns that the target accepts in an INSERT. A `MATERIALIZED` source column
is selected explicitly when the target stores it as a plain column.

The ADR-0170 classifier's shape relation is recorded for tables that are
leeway on both sides. It cross-checks the physical verdict: two tables that
are `equal` as leeway shapes but spelled under different naming separators
share no column name, so they are `incompatible` for a copy by name. They
are not reported as unrelated.

The DDL is shown in full and runs only after an explicit confirmation step.
That step first **restates** the plan: it discovers both servers again and
judges every table afresh, and it refuses if any verdict or DDL has moved.
Progress made by the plan's own DDL is not staleness. After an interrupted
run, a table that went from `create` to `identical`, or from `extend` to
`narrower` or `identical`, or whose remaining statements are a subset of the
plan's, is accepted. Only the statements still pending are run. The DDL is
sent with the server's "suspicious DDL" guard settings enabled
(`allow_suspicious_low_cardinality_types` and its siblings):
`create_table_query` does not record the query-level settings the source's
DDL was accepted under, and leeway's facts shape needs the first of them.

### SD4 — Content diff: one scan of fine leaf digests per side

For a table whose two sides agree on the sorting key, the diff never moves
rows. Each side runs **one** `GROUP BY (chunk, leaf)` over the whole table.
A **leaf** is `cityHash64(tuple(<sorting key>)) % L`, where L is a power of
two sized so that a leaf holds about a thousand rows. Each leaf returns:

- `count()`;
- `sumWithOverflow(cityHash64(tuple(<sorting key>)))`, the **key digest**;
- `sumWithOverflow(cityHash64(formatRowNoNewline('RowBinary', <copied columns>)))`,
  the **row digest**, which separates "key missing" from "same key,
  different values".

A table with no sorting key (Log, Memory, `ORDER BY tuple()`) is keyed by
its row digest. Its rows can then only be missing or extra, never changed.

The column list inside `formatRowNoNewline` is **explicit, fixed in order,
and identical on both sides**: it is the copy column list of SD3. It is never
`*`, which expands to different lists under the `extend` and `narrower`
verdicts. The settings that shape RowBinary bytes are pinned in the query's
`SETTINGS`, so a server profile cannot change the digest. RowBinary makes a
Nullable column's null flag part of the bytes, and it leaves
`LowCardinality` wrapping out of them. So rows with NULLs are hashed, and a
column that is `LowCardinality` on one side only still compares equal.

A wrapping sum is additive and does not cancel on duplicate rows the way an
XOR would. Coarser levels, for the chunk, the table and anything in between,
are therefore sums of leaves that the tool computes itself. A multi-level
view costs no further scan, as with the one-pass hierarchy of pg_comparator.

Sums say *that* a leaf differs, not how. A differing leaf is therefore
reported with each side's row count and with which digest differs: the keys,
or only the rows. A leaf or chunk that one side lacks entirely is exact
without further work. For the remaining leaves, those under a row threshold
and within a per-table row budget, the tool reads per-row
`(key hash, row hash)` pairs from both sides in one more scan per side. It
then splits each leaf by key into rows missing on the target, extra on the
target, and changed. That is data-diff's threshold. Leaves above it are
reported as not compared row by row, and their rows count toward none of
the three totals.

The **chunk** is the unit of copying and verification. Leaves only locate
differences inside it. Its id is an expression both sides evaluate, so a
chunk means the same rows on both, whatever the target's own layout:

1. When the source table is partitioned, a chunk is one value of the
   source's partition key. Its id is the RowBinary bytes of the partition-key
   expressions, in hex; text would depend on the server's timezone.
2. When it is not partitioned and holds more than one chunk's worth of rows,
   a chunk is a range of the first sorting-key expression, provided that
   expression's type is orderable. Bounds are taken at equal ranks of a
   sorted reservoir sample of the source (`groupArraySample`). A sample
   works for any orderable type, where `quantiles` takes only numbers.
   DateTime bounds are written and cast in UTC, so a bound means one instant
   on either server.
3. Otherwise the whole table is one chunk, and leaves are the only
   subdivision.

The layout is stored in the plan by the first diff. Later plans of the same
table reuse it while its sorting and partition keys are unchanged, so digests
stay comparable from one run to the next.

Two kinds of column are handled in the plan rather than trusted:

- **`JSON` columns.** In native RowBinary, logically equal `JSON` values can
  serialize with their paths in different orders. With
  `output_format_binary_write_json_as_string` pinned, the paths come out in
  canonical order and equal documents give equal bytes. Both were observed
  on ClickHouse 26.8. `Dynamic` and `Variant` columns were not tested.
- **Merge-semantics engines.** On Replacing, Collapsing, Summing and
  AggregatingMergeTree, logically equal tables can hold different row
  multisets until both sides are fully merged. A diff can read such a side
  with `FINAL`, which is slower. Without it, the table's differences are
  marked as possibly spurious.

The prior art behind these choices, and the options not taken, are in
[jackstay-sync-prior-art](../adr-background-work/jackstay-sync-prior-art.md).

### SD5 — Sync: chunked, verified, resumable

A table is synced in one of three modes:

- `full` copies every chunk.
- `repair` makes the target's differing leaves equal to the source's. Those
  leaves are cleared on the target and then copied:
  `WHERE <chunk> AND cityHash64(<key>) % L IN (…)`. Clearing removes every
  target row in those leaves, including the ones that already matched. Repair
  acts only on what the plan's diff showed. Before it touches a chunk, it
  reads both sides afresh and checks two things: every leaf it would clear
  must be one the diff listed, and it must still hold the row count the diff
  recorded. A chunk that fails the check is reported **stale** and left
  alone. The dry run lists how many target rows each chunk would lose, so
  that no row is deleted that the operator did not see counted.
- `sample` copies the rows where `cityHash64('jackstay-sample', <key>) % N < k`.
  The salt keeps the sample independent of the leaf layout. A sample is
  deterministic, it keeps rows with related keys together, and it works on
  tables that declare no `SAMPLE BY`. `LIMIT` does neither.

A chunk is copied as one relayed stream: the source's `SELECT <copy columns>
… WHERE <chunk> FORMAT Native` body is the target's `INSERT … FORMAT Native`
body, undecoded. The insert runs with `insert_deduplicate = 0`. A chunk that
is cleared and copied again carries the same blocks, and a replicated target
would otherwise drop them as repeats. Range and partition chunks read the
source through a pruning predicate: bounds on the first sorting-key
expression, or `_partition_id`.

**Ownership** decides what a run may clear. When a run first reaches a table,
it counts the target's rows and records the answer in the journal:

- A full or sample sync into a non-empty target is refused unless the table
  is set to `append` or `replace`.
- The run owns the target's rows when the target was empty, or under
  `replace`.
- An owned chunk is cleared before it is copied whenever it holds rows. This
  covers `replace`, the leftovers of an interrupted attempt, and a chunk
  whose source moved after it was copied. Clearing uses `DROP PARTITION`
  where both tables partition by the same key, a lightweight `DELETE`
  otherwise. A table outside the MergeTree family can only be truncated
  whole.
- Under `append` the run owns nothing. It records an attempt before each
  copy, and a resumed run refuses a chunk it attempted, rather than risk
  inserting it twice.

**Verification** is arithmetic on digests. Before a copy the tool reads the
target chunk's leaf digests, and afterwards it reads them again. Because the
sums are additive, the chunk must hold exactly what it kept plus what the
source's digest says was copied. That holds under every mode, including a
sample and `append`. On a merge-semantics target, rows can collapse during
the copy. There, a mismatch is checked once more by comparing both sides
under `FINAL`, which is valid only when the target chunk held nothing
else. A chunk counts as done only when verified, and only then is it written
to the journal. An owned chunk that fails is cleared and copied again, up to
a bounded number of attempts. A chunk the run does not own is reported, and
the run moves on.

**The journal** is a local, append-only JSON-lines file beside the plan. Each
line is synced to disk before the chunk counts as done, and each carries
the run id the plan names. A resumed run skips a journaled chunk only while
the source chunk's digest still equals the one recorded when it was copied.
A new run ignores the lines of earlier ones. A later structure or diff step
begins a new run.

### SD6 — Monitoring: rows, rate, the wire, and the target's disk

**Rows.** Each relayed INSERT carries a query id, and the tool reads that
query's `written_rows` from the target's `system.processes` while it runs.
The count is the rows that have landed, and the relay parses nothing. HTTP
progress headers do not serve here: a streamed `SELECT` starts its body at
once, and no progress header follows it (observed on ClickHouse 26.8). A
chunk's expected row count is exact, from the source digest taken before the
copy. When verification fails, the count is wound back, so a retried chunk
is not counted twice. Rate and ETA come from the one estimator
([ADR-0247](./0247-one-progress-estimator.md)).

**The wire.** The relay asks the source for a compressed response (`zstd` by
default) and passes the compressed bytes to the target as they are,
declaring the encoding the source applied. Nothing is decompressed in
between. `Native` is uncompressed column data, and on one table it ran to
3.3 times the table's compressed size on disk. With `zstd` on the wire the
same table crossed in about a sixth of that. The monitor reports bytes as
they cross.

**The target's disk** is read from ClickHouse's system tables:

- `system.disks` gives each disk's `free_space`, `total_space` and
  `keep_free_space`;
- `system.storage_policies` and `system.tables` give the disks a table's
  storage policy writes to. A table outside the MergeTree family writes to
  the default disk;
- `system.parts` gives `sum(bytes_on_disk)` over each synced table's active
  parts.

**Pre-flight.** Before a sync starts, the tool groups the chosen tables by
the disks their policy writes to. For each group it compares an estimate
with free space less `keep_free_space`. The estimate adds two amounts, then
multiplies by a headroom factor:

- the source's on-disk bytes, scaled by the share the mode copies (the
  sample fraction, or the rows the diff listed for repair);
- the target bytes that `replace` or `repair` clear. A lightweight `DELETE`
  only masks rows, and the space returns when the parts merge. Observed on
  one table: 425 MB held against a 142 MB steady state, merged back within a
  minute.

The pre-flight warns and does not refuse. The estimate cannot know codec
differences between the servers or when merges run.

**The floor.** Before each chunk, the tool checks the table's disks against
a floor: the larger of an absolute size and a share of the disk, on top of
`keep_free_space`. Below the floor it waits, polling and saying why. An
operator frees space, or stops the run and resumes it later from the
journal. Merges move `bytes_on_disk` after a chunk lands, so the footprint
is shown as observed, never as a chunk's cost.

### SD7 — Front ends

Both front ends call one workflow layer in the engine, one function per step
(`PlanStructure`, `Recheck`, `ApplyDDLStep`, `DiffStep`, `PrepareSync`,
`BeginRun`, `SyncStep`). They are therefore two editors of one plan, not two
implementations of it.

- **CLI.** It has one subcommand per step: `discover`, `structure`,
  `apply-ddl`, `diff` and `sync`, each reading and writing the plan file.
  `status` renders the plan and its journal without contacting a server,
  and with `--disk` adds the target's disks and footprints. The sync's
  progress goes to `hmi/progressbar`.
- **GUI.** The app is a wizard, one page per step: Connect → Databases →
  Structure → Differences → Sync → Monitor.
  - **Background work.** Each step runs as a `bgjob` job on a copy of the
    plan and lands a new plan, which the frame takes, shows and saves when
    the plan has a file. The worker goroutines never call imzero2. The
    progress rows are `widgets/bgjobrow`; the sync reports rows landed
    against the rows expected, so rate and ETA come from the shared
    estimator.
  - **Plan files.** `widgets/filepicker` opens and saves plans. A seed
    variable names a plan to open at start and save to, which is how a
    headless scene reaches the steps that need a saved plan.
  - **Guards.** Applying DDL takes a second, explicit confirmation naming the
    target server. The sync refuses an unsaved plan, because its journal
    lives beside the file.
  - **Defaults.** With no target configured, the target is the source
    server. The Databases step then suggests renamed target databases, since
    a table cannot sync onto itself.
  - **Revisiting.** Revisiting a step rebuilds the plan sections it owns, and
    a later step's recheck refuses a plan the servers have moved away from.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `keelson/data/chclient` | added: `QueryStream`, `InsertStream` and `StreamOptions` (query id, response and body encodings): a query body returned undecoded, and an insert body streamed from an `io.Reader` | none (additive) |
| CLI registry (`public/app/main.go`) | added: `jackstay` command | none |
| app registry | added: the jackstay app manifest | the demo resolver's and capslock's side-effect import lists |
| env-var registry | added: `BOXER_JACKSTAY_PLAN`, the window's seed plan file | `doc/env-vars.md` |
| env-var registry ([ADR-0009](./0009-environment-variable-registry.md)) | added: `BOXER_JACKSTAY_TARGET_ENDPOINT` / `_USER` / `_PASSWORD` for the target, beside `CLICKHOUSE_*` for the source; the password has no CLI flag | `doc/env-vars.md` |
| plan / journal JSON | new, versioned | none outside the package |

## Alternatives

- **Server-side `remote()`.** The target would run `INSERT … SELECT FROM
  remote('src:9000', …)`. It is faster, and nothing passes through the tool,
  but it needs the target to reach the source's native port. It also moves
  credentials into SQL text, and it gives the tool no byte-level progress.
  It is deferred as an opt-in mode on the same chunk plan, not rejected.
- **`BACKUP` / `RESTORE`.** These copy whole tables or databases to a disk or
  S3 destination. They cannot express a sample, a leaf-level repair or a
  column-subset insert, and they need backup disks configured on both
  servers.
- **Decoding into Go and re-encoding (`chrows`, Arrow).** This costs a decode
  per row and adds a place where types can drift. The relay needs no
  knowledge of the rows, and `Native` between close versions is the most
  type-faithful wire available.
- **A journal table on the target.** It would be visible to everyone who
  queries the target, and it would outlive the operator's interest. The
  local journal keeps the servers clean, at the cost that a run resumes only
  from the machine that started it.
- **Diff by XOR of row hashes.** Duplicate rows cancel, and a table with
  repeated keys would read as identical. `sumWithOverflow` does not cancel.
- **Drilling down one level per query.** Each level would cost another
  scan of the differing range. Additive leaf digests from one scan give every
  coarser level without one.
- **Set reconciliation (IBLT, rateless IBLT).** A server-side IBLT is
  expressible with `ARRAY JOIN` and aggregates. What it saves is
  communication, and a leaf-digest exchange is already small; the cost that
  matters is server scans, where it is no cheaper. It also needs sizing for
  an unknown difference, handles duplicate rows poorly, and yields row hashes
  that take another scan to turn into rows. Revisit only if measurement
  finds large chunks with few, scattered differences that the leaf
  threshold does not resolve.
- **rsync's rolling search and byte-level content-defined chunking.** Both
  solve the boundary-shift problem of offset-defined blocks. Chunks defined
  by key values do not have it.
- **`cityHash64(<columns…>)` for the row digest.** The hash of a row with a
  NULL in it is NULL, and `sumWithOverflow` skips it. `cityHash64(tuple(…))`
  does not have that flaw, and an informal check found it cheaper than
  `formatRow`. It stays the fallback if a trial shows `formatRow`'s cost
  matters. `formatRow` was preferred because its input is a documented byte
  encoding rather than per-type hash-combining rules.
- **Part hashes (`system.parts.hash_of_all_files`) as a cross-server
  diff.** They describe physical layout, which differs between servers after
  any re-insert. They remain useful as a same-side change signal
  (see the deferrals under Status).

## Consequences

### Positive

- One plan document serves both front ends, review before execution, and
  resumption.
- The diff moves no rows. A matching chunk costs two aggregate scans, one per
  side, and nothing is transferred.
- Leeway tables get a structure verdict in leeway terms, through the data
  catalog's existing relation.

### Negative

- Every byte crosses the tool's machine twice, source → tool → target. For
  servers that sit next to each other and far from the operator, this is the
  slow path until the `remote()` mode lands.
- Digest equality is probabilistic: two different chunks can collide on a
  64-bit sum. The sync treats a match as proof, which is acceptable for
  ephemeral stores and would not be acceptable for records of value.
- Range chunks on an unpartitioned table use bounds sampled when the table
  was first diffed. A table that grows heavily afterwards gets uneven chunks,
  but they are still correct ones.
- The row-by-row stage costs a second scan per side, and its per-table row
  budget means a table with many scattered differences gets exact totals
  only for the leaves within the budget.
- `cityHash64` and RowBinary must agree across the two server versions.
  That holds between close versions; the tool does not check it.
- `formatRow` builds a string per row before hashing, which makes the row
  digest the dearest part of a diff scan.

### Neutral

- Materialized views, plain views, dictionaries and the identsql UDFs are
  not synced. The owning apps' `EnsureTable` paths and the identsql
  installer recreate them.
- A pack-file target (`export` / `import` of a tar holding a manifest and
  one `Native` stream per chunk) reuses the relay unchanged, with a file
  standing in for the target server. It is the last milestone, not a separate
  design.

## Migration — Tier 1

Nothing to migrate: every surface above is additive.

## Verification plan — Tier 1

- **Lane.** The integration lane (`//go:build integration`) runs two
  clickhouse-local-backed servers, or two server instances where the binary
  allows it. It seeds a leeway table and a plain table on the source, and
  runs discover → structure → diff → sync (`full`, `repair`, `sample`) →
  diff. The final diff must report every chunk identical. An interrupt test
  kills the relay mid-chunk and resumes; the row count must equal the
  source's, with no duplicates.
- **Default lane.** Plan building, verdict classification, chunk derivation
  and the SQL text of the digests are pure functions over `system.*` rows
  and are unit-tested without a server.
- **GUI.** A scene document ([ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md))
  steps the wizard against the integration fixtures.
- **Gap.** The tool does not check hash agreement across genuinely different
  server versions. Close versions is the stated premise.

## Status

Accepted 2026-09-25.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

### M1 — Engine: discovery, structure verdicts, plan document ✓

### M2 — Content diff (SD4) and the chunk derivation ✓

### M3 — Relayed sync, digest verification, journal and resume (SD5) ✓

### M4 — Monitoring: progress and disk (SD6); CLI complete ✓

### M5 — The jackstay app wizard and its scene ✓

### M6 — Pack-file target (`export` / `import`) — superseded by ADR-0271

Deferred:

- **Key-level repair.** Repair clears and copies whole leaves. For scattered
  differences, that moves far more rows than differ: a first live run
  cleared and re-copied about a third of a 1.4M-row table to mend under a
  thousand rows. The diff's row-by-row stage already knows the differing
  key hashes, so those keys alone could be cleared and copied.
- **Target-only chunks under `replace`.** Chunks the target holds and the
  source lacks are left as they are; repair clears them.
- **An archive of the last-synced leaf digests.** Unison keeps such an
  archive. It would let a later run skip re-digesting a partition whose
  active part set in `system.parts` (names and `hash_of_all_files`) is
  unchanged on both sides since that archive. Each side is compared only
  with its own past. It needs the chunk bounds SD4 already keeps.
- **Key-hash chunk boundaries** (`cityHash64(<key>) % M = 0`, the
  prolly-tree rule) for splitting a chunk that has outgrown its reused
  sampled bounds.
- The server-side `remote()` mode, database renaming beyond a flat map,
  and syncing views and UDFs.

## Updates

### 2026-09-26 — The wizard revised for the operator who uses it rarely

§SD7's GUI kept its structure — one page per step, one `bgjob` per step on a
copy of the plan, `widgets/filepicker` for plan files — and changed what each
page tells the operator, on the premise that nothing may need remembering
between uses:

- **The frame is a classic wizard's.** A breadcrumb trail names the steps,
  ticks the done ones and dims a step whose prerequisite is missing, with
  the reason on hover; a column beside each page carries the step's icon,
  what the step does, and what the earlier steps produced (the servers, the
  databases, the verdict count, the comparison, the run); and every page
  ends in the same right-aligned bar: Back, the page's own action, Next,
  the latter enabled once the step it leads to is unlocked. Structure is
  such a step: it unlocks when the plan exists, so Next on Databases does
  not lead to an empty page.
- **The plan file is proposed, not demanded.** A plan with no file gets one
  under `BOXER_JACKSTAY_PLAN_DIR` (by default the user's config directory)
  when the structure is first planned, named by date and servers, so the
  sync's refuse-unsaved guard still holds and is never met by accident.
  `Save as` remains. The plans the window used are kept under its persisted
  key `recent-plans` and listed on the first page; opening one lands on the
  furthest step that has a result.
- **Every write shows its consequence first.** The Structure page states
  what the pending DDL creates, grouped by database and table, before the
  two-step confirmation. The Sync page runs the pre-flight itself whenever
  the choices change and shows the rows, the bytes and the disks' fit
  before Start, which takes the same two-step confirmation as the DDL and
  names the target.
- **Defaults follow the plan.** The mode is proposed from the plan's state
  — repair after a comparison that found differences, full otherwise, with
  the reason stated — until the operator picks one. Compression and the
  restart choice sit under an Advanced header; restart appears only when
  the plan has a previous run.
- **The target side is shown.** The Databases page says whether each
  proposed target database exists and how many tables it holds, from the
  inventory discovery already took.
- **Monitor became Run.** One card leads: the sync in flight, or the last
  run's outcome, rows, bytes and duration, with the per-table report as a
  grid and the disks and chunk log beneath. Each page also shows the CLI
  invocation of its step, for the run someone automates afterwards.

The committed scene captures the first five pages; the sync itself was
verified with a scratch scene against one server, source and target, on a
1.5M-row database, followed by a comparison that found every table
identical.

### 2026-09-26 — Review fixes: retry, resume, replace, and one seam for diff and sync

A source-level review of the engine, the CLI and the wizard found the core of
§SD4 and §SD5 sound and its edges not. What changed:

- **Retry re-reads the source.** A copy is verified against the source digest
  it read from; a source that moved between the digest and the copy used to
  fail every attempt. Cleared rows are counted on the first attempt only.
- **Resume is checked against the journal's start entry**, which now records
  the mode and existing-rows policy the table was begun under; a run resumed
  under other settings is refused and told to restart.
- **Replace clears target-only chunks**, so the target ends up equal to the
  source; each is journaled at an empty source digest.
- **Repair reports the relay's error** rather than the digest mismatch that
  follows it, and counts rows only once they are verified.
- **A diff is not carried across a copy-column change**, since its digests
  hashed the earlier columns; `CarryOver` is one function with a `withDiffs`
  switch. §SD5's "a later structure or diff step begins a new run" is now
  only the structure step: a diff keeps the run, which is safe because a
  journaled chunk is skipped only while the source digest still matches.
- **One seam for every step.** `DiffStep` and `PrepareSyncStep` recheck the
  plan themselves, and `RunSync` owns the run id, the journal and the plan
  saves; the CLI and the wizard render and choose the file, as §SD7 meant.
- Partition chunks are sized per partition; Float keys keep NaN rows in
  chunk 0's predicate; a leaf whose pair stage returns nothing stays
  unresolved; `TableDiff.Final` records the request; the pre-flight charges
  each group's headroom to every disk it touches; the free-floor wait polls
  through a read error; the DDL client carries only the guard settings the
  target knows; the endpoint normaliser keeps a query string; `LoadPlan`
  validates what it read; extend DDL carries column comments; a retargeted
  CREATE whose body still names the source database is noted; nested
  `LowCardinality` compares as a note.
- A scripted fake client drives the retry, resume and replace branches in
  the default lane.

### 2026-09-28 — The wizard's plans live in its fs data area

The capslock gate (ADR-0026 §SD10) found the wizard writing plan files
itself, under the user's config directory, with no manifest subject to
justify it. The plans and their journals now live in the window's data area,
which the fs broker owns (ADR-0026, 2026-09-28): the window addresses a plan
by file name, and the manifest declares `fs.appdata.>`.

- `BOXER_JACKSTAY_PLAN_DIR` is gone; the broker's `BOXER_FS_APPDATA_DIR`
  sets the root. `BOXER_JACKSTAY_PLAN` names a plan in the data area rather
  than a path. The recent-plans list holds names, and rows written before
  this change, which held paths, are dropped.
- `Open…` and `Save as…` become **Import…** (copy a plan in through
  `fs.dialog.read`) and **Export…** (write a copy out through
  `fs.dialog.write`). The plan in the data area stays the window's plan, so
  the journal stays beside it. A plan a CLI run left elsewhere comes in
  without its journal, so a run begun from the CLI resumes from the CLI.
- The engine takes the file operations through `FilesI` (`SaveIn`,
  `LoadPlanIn`, `OpenJournalIn`, `RunSyncIn`); the CLI keeps the path forms
  over `OsFiles`. The journal appends one durable line per record as before.
- Saves run in the step's worker rather than on the frame.

### 2026-10-02 — Row filters, and M6 replaced by packs (ADR-0271)

[ADR-0271](./0271-jackstay-row-filters-and-packs.md) adds a per-table row
filter, which every step applies on both sides, and replaces the M6 sketch
under Status. M6's "tar holding a manifest and one `Native` stream per chunk"
becomes a directory pack. `export` writes it from the source, and a plan
names the pack as its source in place of a server. The sync's relay and
§SD5's verification are unchanged; the source side of every step now goes
through one interface. Under a filter, §SD5's clearing never drops a
partition or truncates a table.

### 2026-10-03 — The page's action leaves the wizard bar

The bar of the 2026-09-26 entry held Back, the page's own action and Next in
one row, so the button that writes to a server sat beside the ones that only
move between pages. The bar now holds Back at its left edge and Next at its
right. The page's action, with its confirmation, sits at the foot of the
page, above the bar, and stays there while the page scrolls.

### 2026-10-03 — The plan's phase in the bar

The plan's phase — discovered, planned, compared, synced and their running
and failed forms — is a state machine the window mirrors from its jobs and
the plan, drawn as a chip with a one-line summary and offered to agents as
`plan_state` and `plan_machine`. The chip takes the left edge of the bar, as
a status bar carries it; Back and Next move together to the right edge.

### 2026-10-03 — Review fixes: structure, chunking, the run, and the wizard's plan

A review of the package, the CLI and the wizard found these, now changed:

- **Structure (§SD3).** A `Replicated*` create whose Keeper path names no
  `{uuid}` is refused: retargeted as it stands, the target would join the
  source's replication group. A table that materialized views read from
  gets a note, since a sync's inserts fire them and its clears do not undo
  them. Merge engines whose parameters differ (`ReplacingMergeTree(ver)`
  against another version column) get a note, since FINAL then means
  different things on the two sides. A table whose columns vanished between
  discovery queries is refused. Two mappings on one server that make a table
  both a target and a source are refused. Applying the DDL keeps the diffs,
  choices and reports of the tables it did not alter, and the run.
- **Chunking (§SD4).** Range chunking is limited to key types whose bound
  text orders as the values do: integers, decimals, floats, dates and times,
  and byte strings. NaN is left out of the sample, strings are sampled as
  hex, and a layout whose bounds do not strictly ascend falls back to one
  chunk; otherwise a row could be assigned a chunk whose predicate selects
  nothing, and both sides would digest that chunk empty. A saved plan whose
  range layout cannot be ordered opens without that layout and the
  comparison over it, with a note; the next comparison derives it again.
  Chunk ids order by
  the layout's kind. A target engine without `_partition_id` reads its chunks
  through the expression form.
- **The run (§SD5).** Repair clears a whole chunk only when the differing
  leaves cover all of the target's and the source still holds nothing there.
  The row-binary settings the digests pin also close the relay's select and
  the clearing `DELETE`. A journal line cut short by a crash is truncated
  before the next append, and a sample run resumes only at its own fraction.
  A run takes a lock file beside the plan, refused while another process on
  any host holds it, and taken over only from a dead process on this host.
  The engine credits chunks an earlier call verified to the progress
  counter, and waits on the free-space floor itself.
- **The wizard (§SD7).** A sync that stops or is cancelled reloads the plan
  its worker saved, so the next start resumes the same run. One busy check
  gates every action that reads or writes the plan. Opening a plan resets
  what belonged to the previous one and seeds the databases, targets and
  filters from it, so planning it again keeps its slice. A step's result is
  dropped when another plan was opened meanwhile. A plan named at start that
  exists and cannot be read is not overwritten, as in the CLI. Planning
  against other servers starts a new plan file.

Deferred: the wizard's windows do not take the run lock. The fs broker that
holds their plans offers no exclusive create, so two windows, or a window and
the CLI on the window's data area, can still run one plan at once.

### 2026-10-03 — What the live server showed

Two of the fixes above rested on server behaviour, and a live server
contradicted one and exposed a second fault:

- **A DELETE does not take the settings it carries into its WHERE.** A
  lightweight `DELETE` and an `ALTER TABLE … DELETE` alike evaluate the
  predicate under the server's defaults. A JSON column's RowBinary depends on
  `output_format_binary_write_json_as_string`, so a keyless table's leaf
  predicate selected other rows in the repair's DELETE than in the diff. The
  row hash now reads a JSON column through `toJSONString`, whose bytes equal
  the pinned form, so digests already taken stay valid and no setting moves
  the predicate. The plan records those columns (`hashAsText`).
- **An alias could stand in for a column.** The digest queries alias their
  outputs (`chunk`, `pid`, `kh`, `rh`, …) in the SELECT that reads the table,
  and ClickHouse resolves a name to the alias first. A table with a column
  named `kh` was hashed without that column's values, so a difference in it
  went unseen by the diff and by the sync's verification. Every query the
  engine reads as JSON now sets `prefer_column_name_to_alias = 1`.

The Replicated refusal is not yet run against a server: the test lane's
server has no Keeper, and its test skips.

## References

- [ADR-0170](./0170-data-catalog-competence.md) — the data catalog: classification, restoration, shape relation.
- [ADR-0171](./0171-leeway-sql-read-surface.md) — membership ids have no server-side registry.
- [ADR-0247](./0247-one-progress-estimator.md) — the rate/ETA estimator.
- [ADR-0038](./0038-keelson-background-task-primitive.md) — background tasks.
- [ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md) — scene documents.
