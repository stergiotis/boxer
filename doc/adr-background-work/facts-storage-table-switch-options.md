---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-10-30 as background to
> one decision that [ADR-0296 §SD9](../adr/0296-audit-events-as-a-trail-component.md)
> binds and defers: whether `boxer.facts` becomes a Null table whose
> materialized views route rows to storage tables (**Option A, the
> switch**), or stays the one MergeTree table it is and meets the same reads
> with a skip index and per-read-path views (**Option B, one table**). Both
> are detailed with what each means for every writer and reader. Nothing
> here is a decision; §0 states the finding, §5 sets the two against
> criteria and §7 lists the forks an ADR has to settle. §2 says where the tree stood on the compile
> date and goes stale as it moves; the ADRs it cites are authoritative.
>
> **Provenance.** Every statement about this repository was read in the
> working tree on 2026-10-30, paths linked. Statements about ClickHouse
> server behaviour come from two places: those [ADR-0296](../adr/0296-audit-events-as-a-trail-component.md)
> records from its spike on 26.9 are cited to it; the rest are **recalled
> server behaviour, unverified in this tree**, marked *(recalled)*, and §8
> lists them as the questions a spike has to answer before an ADR relies on
> them. Inference from what was read is marked *(inference)*. No web was
> consulted. Sizes in §3 and §4 are estimates, and read as such.

# The `boxer.facts` storage-table switch — take it, or stay on one table

## 0 In short

- **Three items wait on the storage shape**: a correlation index (reads by
  conversation, task, subject or principal scan within the time range), an
  insertion-order cursor for the forwarder's backstop, and retention by
  class. The table `SetupTable` creates by default is one MergeTree sorted
  on `ts` alone — no partition, no TTL, no skip index (§1–§2).
- **Option A, the switch**, makes `boxer.facts` a Null table feeding storage
  tables through materialized views. It wins every criterion about what the
  stored shape can do — a `(subject, ts)` key, an insertion column,
  partition drops per class, a separable audit table — and loses every
  criterion about getting there: each of the twelve writers inherits
  multi-target commit semantics with no transaction; the two writers that
  re-send a held batch need deduplication settings that no insert path
  sends (§2.2); nine chstore readers, 17 views and three literal readers
  retarget; every deployed server needs a migration step (§3, §5).
- **Option B, one table**, adds a bloom-filter skip index on the shared
  array value columns and, only if that is measured and found wanting, one
  materialized view per read path. Nothing changes for writers, and the
  index is one `DROP INDEX` to undo. It forgoes the insertion column in its
  §SD9 form, cheap retention by class and the regulator's table; an ADR
  declining the switch should release four of §SD9's six constraints, or
  the trail keeps paying for an option it does not hold (§4, §5).
- **One measurement decides the read-pruning row, and neither option has
  it**: whether a `bloom_filter` index on the `Array(UInt64)` and
  `Array(String)` value columns prunes granules for `has()` at the trail's
  mix of kinds (ADR-0277 §SD10). It is the first milestone of both options
  (§4.1, §7).
- **Eight forks** for the ADR, the first being the main table's key under
  A, which §SD9 leaves unsaid (§7). Seven server behaviours this page
  relies on are *(recalled)* and listed as spikes (§8.2).

## 1 The question, and what §SD9 already binds

`boxer.facts` is one ClickHouse table that every keelson host, several
daemons and a handful of command-line ingestors write, each with its own
vocabulary, through two lanes — hand-written `chstore` verbs and generated
facts-bound record stores
([facts-bound record stores](../explanation/facts-bound-record-stores.md)).
Three items wait on how it is stored:

- a **correlation index** — reads by conversation, task, subject or
  principal scan the table narrowed only by time
  ([ADR-0277 §SD10](../adr/0277-one-audit-trail-for-model-calls-and-agent-work.md),
  [ADR-0296 §SD5](../adr/0296-audit-events-as-a-trail-component.md));
- an **insertion-order cursor** for the forwarder's backstop, which
  re-scans a time window because `ts` is the event's own time and a
  late-landing batch carries old timestamps
  ([ADR-0296 §SD6](../adr/0296-audit-events-as-a-trail-component.md),
  descoped again in
  [ADR-0330 §SD9](../adr/0330-a-shared-audit-recorder-below-the-runtime-one-sealed-writer-over-a-narrow-store-binding.md));
- **retention by class** — the audit component carries a `Retention`
  label precisely so a storage table can partition on it
  ([ADR-0296 §SD1](../adr/0296-audit-events-as-a-trail-component.md)).

ADR-0296 §SD9 records the owner's direction — `boxer.facts` *may* become a
Null table whose views route to a main table and an audit table
"partitioned by retention class and sorted by subject and time" — and
binds six constraints on every change to the trail until the switch is
decided, building only (d). Quoted by letter, compressed:

| | Constraint (ADR-0296 §SD9) | Built |
| --- | --- | --- |
| **(a)** | *Component-only routing.* A row is routed by a component's `Filter` "and by nothing else: never a natural-key prefix, never knowledge only Go has. The main table's view is the complement of every other route." | — |
| **(b)** | *Timeless ids.* Filters and derived-column expressions carry membership ids as literals from the runtime vocabulary and its committed golden; "an ordinal is never reused." | holds by construction |
| **(c)** | *Derived columns in the table.* Sort and partition columns are `MATERIALIZED` from scalar slots with built-in functions only; "the writer sets no extra column and `SELECT *` does not return them". An insertion-order column (`MATERIALIZED now64()`) comes with the switch. | — |
| **(d)** | *Reads and writes name different tables.* `WithReadTable` on the recorder and `ReadTable` on every generated store. chstore's readers, trailviews and keelsonddl "name the fixed facts table" and are deferred to the switch. | yes |
| **(e)** | *Duplicates on retry are suppressed.* Every view target carries a deduplication window or a Replicated engine; the recorder re-sends a held batch unchanged; the switch must test that the Arrow insert "sent with `insert_deduplicate` and `deduplicate_blocks_in_dependent_materialized_views`, lands once under a window"; a token, if sent, derives from row ids, never from the per-attempt batch id. | — |
| **(f)** | *One DDL path.* `SetupTable` and `keelsonddl` create storage tables, views and the `Merge` "in one step", with `allow_suspicious_low_cardinality_types` at query level for the `Merge`; views are recreated when a vocabulary change renames physical columns; "a test proves every component kind lands in exactly one table." | — |

And its closing sentence is the reason this page exists: "The switch is a
whole-table decision, not a trail one: once `boxer.facts` is Null, every
writer […] inherits materialized-view semantics: an insert fails when any
view's insert fails, and commits across targets are partial. The ADR that
makes the switch states that for each writer." §2 is the inventory that
sentence asks for; §3 states the consequence per writer.

Option B is not named in §SD9. It is the shape the three waiting items
would take if the switch is declined: the skip index §SD5 declined pending
a measurement, views of the kind `trailviews` already composes, and the
backstop's lagged re-scan as the permanent answer to the cursor.

## 2 The table as it stands (2026-10-30)

### 2.1 Engine, key, partitions, settings

The DDL is one generated statement,
[`factsddl.ComposeCreateTableSql`](../../public/keelson/runtime/factsschema/ddl/runtime_facts_ddl.out.go):
`CREATE DATABASE IF NOT EXISTS boxer; CREATE TABLE IF NOT EXISTS boxer.facts (…) ENGINE <clause> SETTINGS allow_suspicious_low_cardinality_types=1`.
The engine clause is the caller's;
[`chstore.defaultEngineClause`](../../public/keelson/runtime/factsstore/chstore/chstore.go)
is `MergeTree() ORDER BY "ts:ts:z64:47::0:"` — **time-ordered, no
`PARTITION BY`, no `TTL`, no skip index**, with the comment that retention
"is intentionally left to the operator's own engine clause […] since it
deletes data". `SetupTable` runs it as `IF NOT EXISTS` and does not
migrate: a deployed table keeps whatever engine clause and column names it
was created with. [`keelsonddl`](../../public/app/commands/keelsonddl/keelsonddl.go)
prints the same script (`--engine` overrides the clause) and, on request,
the trail views after it. [ADR-0184 §SD2](../adr/0184-sysmetrics-persistence-tee.md)
makes `chstore` the sole DDL author; every facts-bound store is generated
`ExternallyProvisioned` and carries no `EnsureTable`.

The column block: an envelope — `id` (UInt64), `naturalKey` (String), `ts`
(DateTime64(9)), `lc:expiresAt` (DateTime64(9), the lifecycle lane) — one
`foreignKey` section, and twenty data sections (`symbol`, `symbolArray`,
`stringArray`, `textArray`, `blobArray`, `bool`, the signed and unsigned
integer arrays, `u32Set`, `u64Set`, the float arrays, `u32Range`,
`timeArray`). Each section is a bundle of parallel arrays: the value
column, the membership columns `hr` / `lr` / `lmr` (the `lr` and `lmr`
lanes are `Array(LowCardinality(UInt64))`), `mrhp`, `len` for list shapes,
and the three cardinality columns. `symbol` and `symbolArray` values are
`Array(LowCardinality(String))`. There is no column that says what kind a
row is other than the membership ids inside `tv:symbol:lr`; every reader
narrows with `has(<lr column>, <membership id>)`.

Two envelope facts matter to the options. `id` is not one thing: `chstore`
mints it from a per-process counter, the trail hashes the natural key
([facts-bound record stores § keyed point lookup](../explanation/facts-bound-record-stores.md)).
And no column records *when a row was inserted*; `ts` is the recorded
thing's own time ([ADR-0277 §SD9](../adr/0277-one-audit-trail-for-model-calls-and-agent-work.md)).

### 2.2 Writers

Every lane below inserts Arrow IPC through one of two HTTP clients:
[`chclient.Client.InsertArrowSettings`](../../public/keelson/data/chclient/chclient.go)
(`INSERT INTO <table> FORMAT Arrow`, settings as URL parameters) reached
directly by `chstore`, or through
[`storeexec.Executor`](../../public/keelson/data/storeexec/storeexec.go) by
every generated store. The only per-insert settings the store layer can
carry are `async_insert` and `wait_for_async_insert`
([`recordstore.InsertSettings`](../../public/storage/recordstore/insert_settings.go));
**no path sends `insert_deduplicate`, `insert_deduplication_token` or
`deduplicate_blocks_in_dependent_materialized_views`** (grep over `public/`
and `apps/`, 2026-10-30). The one mention in code is
[`jackstay`](../../public/db/clickhouse/jackstay/digest.go), which sends
`insert_deduplicate = 0` on its own sync tables.

| Writer | Lane and path | Batch, cadence, retry | Names the fixed table? |
| --- | --- | --- | --- |
| **trail** — model calls, messages, agent actions, grants, fetches, captures, disclosures, adhoc datasets, lading publishes, coverage, audit events, seals | generated [`TrailStore`](../../public/keelson/runtime/trail/trail_store.out.go) under [`auditrec.Recorder`](../../public/storage/recordstore/evidence/audit/auditrec/recorder.go) via the trail's [binding](../../public/keelson/runtime/trail/binding.go); hostboot builds it over the persist executor ([`hostboot.go`](../../public/keelson/runtime/hostboot/hostboot.go)) | one batch per flush; the host flushes every second (`trail.WithFlushInterval`), write-ahead flushes on demand; a batch the server refuses is **held and re-sent unchanged with the same seal**, oldest first, under a 50 000-row backlog cap (`DefaultMaxBacklogRows`); past the cap the oldest are dropped and a gap row written; 5 s flush timeout | `TrailTableName = "boxer.facts"` baked; `ReadTable` option exists, no caller sets it (hostboot passes none) |
| **trail — ditch** | [`DitchBodiesSQL`](../../public/keelson/runtime/trail/ditch.go): `ALTER TABLE <facts> UPDATE … WHERE has(kind)` — a **mutation**, `mutations_sync = 2` ([ADR-0264 §SD6](../adr/0264-retained-model-conversations-on-facts.md)) | on demand | defaults to `TrailTableName` |
| **chstore — grant** | [`WriteGrant`](../../public/keelson/runtime/factsstore/chstore/chstore.go) from [`capbroker`](../../public/keelson/runtime/capbroker/broker.go) | one entity, one synchronous insert, no retry | `cfg.Database.Table`, `boxer.facts` by default |
| **chstore — log** | `WriteLogs` from [`logbridge`](../../public/keelson/runtime/logbridge/logbridge.go), selected by `BOXER_LOG_FACTS` in [`loghost`](../../public/keelson/runtime/loghost/loghost.go) | ring of 4096, flushed every 200 ms or at 512 rows as one multi-row insert; a failed flush is logged, rows are not re-sent | same |
| **chstore — runtime run** | `WriteRuntimeStart` from hostboot and the watchbill command | one row per process start | same |
| **chstore — heartbeat** | `WriteRuntimeHeartbeat` from [`heartbeat`](../../public/keelson/runtime/heartbeat/heartbeat.go) | one row every 30 s (`DefaultInterval`) per host | same |
| **chstore — app lifecycle, launch** | `WriteAppLifecycle`, `WriteLaunch` from [`windowhost`](../../public/keelson/runtime/windowhost/windowhost.go) and its [open service](../../public/keelson/runtime/windowhost/openservice.go) ([ADR-0135](../adr/0135-app-launch-requests.md)) | one row per window event | same |
| **chstore — fallback** | [`NewWithFallback`](../../public/keelson/runtime/factsstore/chstore/fallback.go): a server unreachable at boot gives an in-memory store | — | — |
| **sysmetrics** | [`sysmtee`](../../public/keelson/runtime/sysmtee/sysmtee.go) → generated [`SysmetricsStore`](../../public/keelson/runtime/sysmfacts/sysmetrics_store.out.go), run by [`sysmetricsd`](../../public/app/commands/sysmetricsd/sysmetricsd.go) ([ADR-0184](../adr/0184-sysmetrics-persistence-tee.md)) | flush every 10 s or at 256 rows; a failed flush keeps the batch pending and counts | `SysmetricsTableName = "boxer.facts"` baked |
| **watchbill presence** | [`watchbill.Presence`](../../public/keelson/runtime/watchbill/presence.go) → generated [`PresenceStore`](../../public/keelson/runtime/watchbill/watchbillpresence/presence_store.out.go) ([ADR-0237](../adr/0237-watchbill-worker-presence.md)) | one row and a flush per worker start and stop | baked |
| **queryrunsd** | a **refreshable materialized view** `boxer.mv_queryruns` — `REFRESH EVERY n SECOND APPEND TO boxer.facts AS SELECT * FROM url(<pull>) WHERE id NOT IN (SELECT id FROM boxer.facts WHERE ts ≥ watermark − overlap AND has(kind))` ([`queryrunfacts.ComposeMvSql`](../../public/keelson/runtime/queryrunfacts/mv.go), [`queryrunsvc`](../../public/keelson/runtime/queryrunsvc/queryrunsvc.go), [ADR-0115](../adr/0115-query-observability-data-plane-strategy.md)) | every 5 s by default; the dedup is the anti-join **over the destination table itself** | the qualified destination is a parameter; `boxer.facts` by default |
| **stevedore** | generated [`StevedoreStore`](../../public/streaming/stevedore/stevedorefacts/stevedore_store.out.go), opened by the [landing demo](../../public/app/commands/stevedoredemo/stevedoredemo_land.go) | per landing | baked |
| **markdown** | generated [`MddocStore`](../../public/semistructured/markdown/mddocfacts/ingest.go) from the [`markdown` command](../../public/app/commands/markdown/markdown_ingest.go) and mdedit's [send-to-play](../../apps/mdedit/mdedit_sendplay.go) ([ADR-0218](../adr/0218-markdown-ingestor-facts-model.md)) | one document and its items per flush | baked |
| **lading policy** | generated [`PolicyStore`](../../public/fs/lading/ladingpolicy/policy_store.out.go) via [`ladingingest.RecordPolicy`](../../public/fs/lading/ladingingest/record.go) ([ADR-0198](../adr/0198-fs-snapshot-store.md)) | one row per mount | callers pass `Table: layout.PolicyTable()` — the one store whose table is a runtime choice |
| **capmap** | hand-rolled `factsschema/dml` encoding, one `InsertArrow` of the whole corpus ([`capmapfacts.Ingest`](../../public/gov/capmapfacts/capmapfacts.go)) | one insert per load | the table is a parameter |
| **meshdemo**, **jsonbench** | a demo store ([`meshdemo.go`](../../public/keelson/runtime/factsschema/meshdemo/meshdemo.go)); a benchmark writing the facts shape to its own database ([`jsonbench_ddl.go`](../../apps/jsonbench/jsonbench_ddl.go)) | — | not production writers |

Not writers, for the record: app persist state left for `boxer.persiststate`
([ADR-0105](../adr/0105-keelson-adopts-generated-record-stores.md)); the
agent-run ledger has tables of its own; the bus-request `audit` kind is
retired onto the trail's `AuditEvent`
([ADR-0296 Updates, 2026-10-30](../adr/0296-audit-events-as-a-trail-component.md)).

### 2.3 Readers, and what each needs back

| Reader | Shape of the read | Table it names |
| --- | --- | --- |
| generated `Scan<Kind>` on every store | `SELECT * FROM <readTable> WHERE <baked Filter> [AND extra] ORDER BY ts, key [LIMIT]`; the decode is **positional over `SELECT *`**, and `VerifySchema` runs `DESCRIBE (SELECT * FROM <readTable>)` for that reason ([`trail_store.out.go`](../../public/keelson/runtime/trail/trail_store.out.go)) | `ReadTable` if set, else `Table`, else the baked name |
| auditrec reads — by subject, by principal, the backstop window, `VerifySeals` | a time-window predicate on `ts`, `has()` over the section's **value** column, an optional node `has()` on `symbol`; exact match after decode; duplicates by id folded in Go ([`auditrec/reads.go`](../../public/storage/recordstore/evidence/audit/auditrec/reads.go)) | through the trail's read store |
| `chstore` readers — `RecentLogs`, `ListRunEvents`, `LifecyclesByRun`, `LastHeartbeatForRun`, `LookupRunStart`, `AppRuns`, `AppLogTail`, `AppAuditSummary`, `AppLaunchStats` | hand-composed SQL with `has(symbol lr, kind id)` and `ORDER BY ts`, array arithmetic inline ([`runevents.go`](../../public/keelson/runtime/factsstore/chstore/runevents.go), [`recentlogs.go`](../../public/keelson/runtime/factsstore/chstore/recentlogs.go), [`runsessions.go`](../../public/keelson/runtime/factsstore/chstore/runsessions.go), [`apptrail.go`](../../public/keelson/runtime/factsstore/chstore/apptrail.go), [`launchstats.go`](../../public/keelson/runtime/factsstore/chstore/launchstats.go)) | `qualifiedTable()` — **the write table** |
| keelson relations — `app_runs`, `app_logs`, `app_audit`, `runtime_events` | over the chstore readers ([`providers/apptrail.go`](../../public/keelson/runtime/introspect/providers/apptrail.go), [`providers/runevents.go`](../../public/keelson/runtime/introspect/providers/runevents.go); [ADR-0191 §SD7](../adr/0191-runtime-instance-attribution.md)) | as above |
| `trailviews` — 17 `CREATE OR REPLACE VIEW`s: a data mart per kind, the timeline, three aggregates | `… FROM boxer.facts WHERE has(symbol lr, <kind id>)` with the read surface inlined ([golden](../../public/keelson/runtime/trail/trailviews/trailviews_clickhouse.out.sql)); installed with the surface where the fixed table exists ([`trailviews_surface.go`](../../public/keelson/runtime/trail/trailviews/trailviews_surface.go)), or by `keelsonddl --trail-views --database … --table …` | `Statements(db, table)` takes the name; the host install passes the `factsschema` constants |
| llm ration ledger | `ScanLlmCall` through the trail ([`ration_service.go`](../../public/keelson/runtime/llm/ration_service.go)) | the trail's |
| play | `runsHistoryFactsTable = "boxer.facts"` for captured runs ([`play_runs_history.go`](../../apps/play/play_runs_history.go)); a seed `SELECT * FROM boxer.facts` ([`app_register.go`](../../apps/play/app_register.go)); the Projection panel reads leeway-shaped `SELECT *` results | literal |
| `sysmreplay`, `loadstudy` | aggregate SQL over `sysmfacts.SysmetricsTableName` ([`decimate.go`](../../public/keelson/runtime/sysmreplay/decimate.go)); a literal `FROM boxer.facts` ([`loadstudy.go`](../../public/analytics/timeseries/loadstudy/loadstudy.go)) | baked / literal |
| the operator's TTL | `ALTER TABLE boxer.facts MODIFY TTL toDateTime(ts) + INTERVAL 30 DAY DELETE WHERE has(symbol lr, KindQueryRun)` — the documented form for bounding one kind ([query-observability](../explanation/query-observability.md)) | literal |

What the reads need and do not get: a key that prunes on *conversation*,
*task*, *subject* or *principal* (each is a value inside an array column,
reached by `has()` after the time range); a column that orders by
*insertion*; and a partition that lets a class of rows be dropped rather
than deleted. The
[read-surface explanation](../explanation/leeway-sql-read-surface.md) names
the standing gap behind the first: `MATERIALIZED` projections of leeway
slots are not generated, so each one is hand-written with physical names
inlined. The [jsonbench trial's §0](../trials/jsonbench-on-facts/README.md)
is the one measurement in the tree of what a materialized backbone buys on
this shape, and its claim is conditional — parity with a native `JSON`
column *when both declare the same typed paths* — so it says materialized
columns are the lever, not what any one of this page's reads would cost.

## 3 Option A — take the switch

### 3.1 The DDL shape

Four kinds of object replace the one table, all created by `SetupTable`
and printed by `keelsonddl` (f):

```
boxer.facts            ENGINE = Null                      -- every writer keeps inserting here
boxer.facts_main       MergeTree  ORDER BY (ts)           -- plus whatever the main table's key becomes
boxer.facts_audit      MergeTree  PARTITION BY (retention) ORDER BY (subject, ts)
                       with MATERIALIZED retention, subject, inserted_at (now64())
boxer.mv_facts_audit   MATERIALIZED VIEW TO boxer.facts_audit  AS SELECT * FROM boxer.facts WHERE <AuditEvent Filter>
boxer.mv_facts_main    MATERIALIZED VIEW TO boxer.facts_main   AS SELECT * FROM boxer.facts WHERE NOT (<AuditEvent Filter>)
boxer.facts_all        ENGINE = Merge(boxer, '^facts_(main|audit)$')   -- the read table for whoever wants both
```

The spike behind §SD9 showed, on 26.9 with the unchanged recorder: a
`Filter` and its complement put every row in exactly one table; a generated
store read each storage table and the `Merge` unchanged; the `MATERIALIZED`
derived columns agreed with the leeway UDFs and stayed out of `SELECT *`
([ADR-0296 Context](../adr/0296-audit-events-as-a-trail-component.md)). The
generator would emit the derived-column expressions beside each `Filter`
for every unit scalar slot (c), from the same baked ids (b); the
[`storegen`](../../public/keelson/runtime/factsschema/storegen/storegen.go)
seam that reconciles DTO tags with the vocabulary is where they would come
from. `ReadTable` (d) is the only switch-side piece built.

Two facts about ClickHouse the shape rests on and the tree does not record
*(recalled)*: a `Null` table accepts inserts and keeps nothing, and a
`MATERIALIZED` column is omitted from `SELECT *` unless
`asterisk_include_materialized_columns` is set — the spike relied on the
default. The `Merge` engine over tables with `LowCardinality(UInt64)`
arrays needs the suspicious-types setting at query time (f).

### 3.2 What every writer inherits

These follow from how materialized views are driven by an insert, and are
the semantics §SD9's closing sentence names. The first two are what the
spike observed; the rest are *(recalled)*:

1. **An insert fails when any view's insert fails.** The writer's one
   `INSERT` is the views' inserts; a refused target refuses the statement.
2. **Commits across targets are partial.** A view that committed before
   another failed stays committed; the writer sees one error.
3. **A plain retry duplicates** what already committed, unless the target
   has a deduplication window and the insert is marked to use it (e). On a
   non-replicated MergeTree the window is the
   `non_replicated_deduplication_window` table setting, off by default;
   `insert_deduplicate` governs whether an insert participates; and
   dependent views deduplicate only under
   `deduplicate_blocks_in_dependent_materialized_views`, off by default.
   A token (`insert_deduplication_token`) replaces the block hash where the
   retried block is not byte-identical.
4. **The order in which views commit is not deterministic**, so which
   target holds the partial result is not either.
5. **A mutation on the Null table does nothing.** `ALTER TABLE boxer.facts
   UPDATE` has no rows to touch; the trail's ditch and the operator's
   `MODIFY TTL` have to name a storage table.
6. **`SELECT` from the Null table is empty.** Every reader that names the
   write table reads nothing, silently.

### 3.3 Per-writer consequences

| Writer | Failure mode it gains | Retry it has | What the switch asks of it |
| --- | --- | --- | --- |
| trail / auditrec | a batch that lands in `facts_main` and fails into `facts_audit` is re-sent whole: duplicates in `facts_main` unless (e) holds | **yes — the re-send is unchanged and identical, so a dedup window can fold it** | the settings on the insert (an extension of `InsertSettings` and `storeexec`), the window on both targets, the test (e) names; `WithReadTable` set by hostboot |
| trail — ditch | the statement targets Null | — | `DitchBodiesSQL` takes the main table's name |
| chstore grant, run, heartbeat, lifecycle, launch | a refused view fails the verb; the caller already treats the write as best-effort | **no** — one attempt | read-table plumbing (§3.4); nothing on the write path |
| chstore log | a refused view loses the 512-row batch, as a refused insert does now | no | none |
| sysmetrics tee | a pending batch is re-sent after a failed flush → duplicates in the target that committed | yes (pending batch) | the same dedup settings as the trail, or acceptance of duplicates in a metrics plane |
| watchbill presence | a refused view fails `Started`/`Stopped` | no | none |
| queryrunsd | the MV's anti-join reads **the destination**; against a Null `boxer.facts` it finds nothing and re-appends every pulled row each tick | the refresh *is* the retry | the anti-join's `FROM` must name the storage table or the `Merge`; `ComposeMvSql` already takes the table as a parameter, so it is a wiring change plus a test |
| stevedore, markdown, lading policy, capmap | a refused view fails the ingest | no | none on the write path; lading's `PolicyTable()` layout may already be elsewhere |
| downstream consumers generating stores against `boxer.facts` | the same six items, with `ReadTable` already in their `<Store>StoreConfig` | their own | set `ReadTable`; nothing else, which is what §SD9 was written to guarantee |

### 3.4 What readers must change

- **chstore's readers** compose over `qualifiedTable()`, the write table.
  They need a read-table field on `chstore.Config` and every `compose*Sql`
  to take it — nine readers, one mechanical change (d).
- **trailviews** name the table they are given; the host install passes
  the `factsschema` constants, and `keelsonddl --trail-views` the CLI
  flags. Both have to pass the `Merge` (or the storage table a view wants),
  and the golden regenerates.
- **play, loadstudy, sysmreplay** carry the name as a literal or a baked
  constant; each is a one-line retarget, and each is a reader nobody will
  find until it returns empty.
- **`SELECT *` and derived columns.** Positional decode survives because
  `MATERIALIZED` columns are not in `*`; a reader that wants the derived
  column names it explicitly. A `Merge` adds a `_table` virtual column
  *(recalled)*, also outside `*`.
- **`FINAL` / dedup.** Nothing needs `FINAL`: the targets stay plain
  MergeTree and duplicates are prevented at insert (e) or folded in Go as
  the auditrec reads already do.

### 3.5 Migration of existing rows

Three shapes, none decided:

- **Copy.** `INSERT INTO boxer.facts_main SELECT * FROM <old>` and the same
  for the audit kind, then drop the old table and create the Null one. One
  statement per target; the copy is the one point where the row set is
  rewritten.
- **Leave.** Rename the existing table to `facts_main` (it already has the
  main table's engine clause), create `facts_audit` empty, and let the
  `Merge` span both. Audit rows written before the switch stay in the main
  table and never gain the audit partitioning — a known, dated exception.
- **Recreate.** Drop and start over, acceptable only where the trail is
  expendable, which `SetupTable`'s comment already says of the
  column-rename case.

`SetupTable` is `IF NOT EXISTS`, so an existing deployment does not take
the new shape on its own; the switch needs an explicit migration step and a
way to tell which shape a server has (the `Null` engine of `boxer.facts`
is a clean probe).

### 3.6 What it buys

- **An insertion-order cursor** for the backstop: `inserted_at MATERIALIZED
  now64()` on each storage table, the replacement for the lagged re-scan
  ([ADR-0330 §SD9](../adr/0330-a-shared-audit-recorder-below-the-runtime-one-sealed-writer-over-a-narrow-store-binding.md)).
  *(recalled)* a `MATERIALIZED` default is evaluated at insert, so a
  late-landing batch carries a late `inserted_at` and an old `ts` — exactly
  the pair the backstop needs.
- **Retention-class partitioning**, and with it a `TTL` or a
  `DROP PARTITION` per class on the audit table, instead of a row-level
  `DELETE WHERE` mutation across everything.
- **A `(subject, ts)` sort** on the audit table so reads by subject prune
  the primary key; the main table may take its own key — `(conversation,
  ts)` is the obvious candidate for the correlation reads, and is the first
  fork of §7.
- **An audit table separable for a regulator**: one table, one partition
  set, one grant.

### 3.7 Test lanes, and the spike's unverified claims

| Lane | Goes red when |
| --- | --- |
| a clickhouse-local test over the composed DDL (f) | a component kind lands in zero or two tables; a derived column disagrees with `LW_GET` on the same slot; `SELECT *` returns a derived column |
| the dedup test (e) | the recorder's re-sent batch lands twice in a target under a window with the settings sent; the settings do not reach the server through `storeexec` |
| the partial-failure test | a view that refuses leaves the other committed and the error is not surfaced as such |
| queryrunsd's live tests | the anti-join reads the Null table |
| the trailviews golden and `ViewsVersion` | views stop naming the read table |

Unverified by the spike and load-bearing: (e) end to end on the Arrow
insert; whether the dependent-views dedup setting folds a block the first
target already took when the second refused it; and the cost of the
derived-column expressions on insert at the trail's volume.

### 3.8 Risks and a rough size

Risks: a reader left on the write table reads nothing and nothing says so
(§3.2 item 6); duplicates under retry if (e) is wrong on any one setting;
the generator's derived-column emission is a new surface across every
facts-bound store; every deployed server needs the migration step.

Milestones *(estimate)*: **M1** generator emission of derived columns and
the complement filter, and the DDL composer for the storage tables, views
and `Merge` — the largest piece; **M2** the dedup seam in
`InsertSettings` / `storeexec` and the (e) test; **M3** read tables on
chstore, trailviews, keelsonddl, play, loadstudy, sysmreplay, hostboot's
`WithReadTable`, queryrunsd's anti-join; **M4** the migration step and the
probe; **M5** the cursor and the scheduled backstop. M1–M3 are each a
sitting of work in a package; M4 depends on how many servers there are.

## 4 Option B — stay on one table

### 4.1 The skip index of §SD5

The correlation reads narrow by `has(<lr column>, id)` for the kind and
`has(<value column>, v)` for the value. A `bloom_filter` skip index on an
`Array` column answers `has()` *(recalled)* — the one skip-index kind that
fits the shape. Two candidates:

- on `tv:u64Array:value` for subject reads, and `tv:stringArray:value` for
  conversation, task and principal reads — §SD5's objection stands: the
  column is shared by every u64 (or string) slot of every kind, so the
  filter indexes every value of every kind and its selectivity is that of
  the whole column;
- on the `lr` columns — indexing the *kind*, which is what every reader
  already filters on first, and which is low-cardinality enough that a
  bloom filter buys little over the time range.

Which one, at what granularity, and whether either prunes a granule at the
trail's row mix is the measurement
[ADR-0277 §SD10](../adr/0277-one-audit-trail-for-model-calls-and-agent-work.md)
asked for and nobody took. The tree has no path that adds an index to a
deployed table (§SD5); ClickHouse has one *(recalled)* — `ALTER TABLE … ADD
INDEX` then `MATERIALIZE INDEX` — so the missing piece is a `SetupTable`
or `keelsonddl` step, not a server capability.

### 4.2 Per-read-path materialized views

`trailviews` is a set of ordinary `VIEW`s: they rewrite the read, store
nothing, and prune nothing. A read path that needs its own key gets a
**materialized view with its own target table** — one per read path, keyed
for it (`(conversation, ts)`, `(subject, ts)`) — fed from the one
`boxer.facts` MergeTree. This is the same mechanism as Option A with the
roles inverted: the source table stores everything, the views store
copies. It inherits items 1–4 of §3.2 for the writers exactly as the
switch does, scaled by how many such views exist, and duplicates the rows
it indexes. It is the shape to reach for *after* §4.1 is measured and found
wanting, not beside it.

### 4.3 A cursor without an insertion column

The backstop stays the lagged re-scan: `ForwardWindow(from, to, page)` over
`ts`, with the prompt path's recent ids skipped, walked by `CatchUp`
([ADR-0330 §SD7](../adr/0330-a-shared-audit-recorder-below-the-runtime-one-sealed-writer-over-a-narrow-store-binding.md)).
Its cost is a scan of the window per pass and a window the caller must
choose wide enough to catch the latest-landing batch — the backlog cap
bounds how late that is in rows, not in time.

One table-side alternative the tree touches but does not use for this:
[`rowcas`](../../public/storage/recordstore/rowcas/rowcas.go) switches on
`enable_block_number_column` for the watchbill tables, which persists the
`_block_number` virtual column across merges *(recalled)*. A block number
is assigned at insert and grows with it, so it is an insertion-order
proxy without a column the writer or the generator knows about
*(inference)*. Whether it is monotonic enough for a cursor — across
merges, after mutations, and whether the ditch's mutation disturbs it —
is a spike, listed in §8.

### 4.4 What stays impossible on one table — precisely

- **Partitioning by retention class: not without recreating the table.**
  A MergeTree's partition key is fixed at `CREATE` *(recalled)*; the
  deployed table has none. Adding one is a copy into a new table — the
  same migration as Option A's, for less.
- **Changing the sort key: likewise.** `ORDER BY` can only be *extended*
  with newly added columns *(recalled)*; `(ts)` cannot become
  `(subject, ts)` in place.
- **TTL per kind or per class: possible.** A table holds one `TTL` clause,
  but the clause may carry several expressions, each with a `DELETE WHERE`
  — the form [query-observability](../explanation/query-observability.md)
  documents for query runs, and ADR-0296's retention label is a value
  `has()` can test. What it costs *(recalled)*: TTL deletion rewrites the
  parts that hold expiring rows, a merge-time mutation, where a partition
  drop frees files. At this table's volume that is an operational
  difference, not a semantic one.
- **A separable audit table: no.** One table is one grant.
- **An insertion-order column: only by `ALTER TABLE ADD COLUMN …
  MATERIALIZED now64()`** on the existing table — rows already present get
  the default evaluated at the `ALTER`, not their insertion time
  *(recalled)*; new rows get it right. This needs the same "alter a
  deployed table" path §4.1 needs and the tree lacks, and it contradicts
  the letter of (c), which ties the column to the switch — a fork for §7.

### 4.5 Per-writer consequences

None on the write path, for every writer in §2.2: the table they insert
into is the table that stores, with the semantics it has. A skip index
adds work per insert *(recalled)*, which is the trail's and the log lane's
to absorb. `ReadTable` stays empty everywhere. The ditch, the TTL and
queryrunsd's anti-join keep naming the table they name.

### 4.6 What it buys, what it forgoes

Buys: no change in failure semantics for twelve writers; no migration of
rows unless a partition or sort key is wanted after all; the `Merge`, the
complement filter and the derived-column generator do not exist; a
reversible first step (an index can be dropped). Forgoes: the insertion
cursor in its §SD9 form; cheap retention by class; a key that prunes on
subject or conversation; the regulator's table; and it leaves §SD9's six
constraints binding every trail change for a switch that is no longer
coming — an ADR declining the switch should release (a), (c), (e) and (f)
explicitly, or the trail keeps paying for an option it does not hold.

Milestones *(estimate)*: **M1** the measurement of ADR-0277 §SD10 on a
facts table at a realistic mix — the step both options need and neither
has; **M2** an `ADD INDEX` / `MATERIALIZE INDEX` path in `SetupTable` and
`keelsonddl` for a fresh and a deployed table; **M3** the TTL form as an
operator's how-to per class; **M4**, only if M1 says so, one per-read-path
materialized view; **M5**, optional, the `_block_number` spike for the
backstop. M1 and M2 are small; M4 is Option A's M1 for one path.

## 5 The options against criteria

`++` clearly better · `+` better · `−` worse · `−−` clearly worse. The
column is the option; the row is the criterion.

| Criterion | A — the switch | B — one table | Note |
| --- | --- | --- | --- |
| Correctness under partial failure | `−−` | `++` | A makes every writer's insert a multi-target commit with no transaction; B changes nothing. A's `−−` becomes `−` once (e) is verified and sent by every re-sending writer |
| Read pruning for correlation reads | `++` | `−` | A sorts a table for the read; B's bloom filter is on a column every kind shares, and prunes only what the measurement says it does |
| Insertion time available | `++` | `−` | A's `inserted_at` is in the design; B has a re-scan, or the `_block_number` spike |
| Retention per class | `++` | `+` | A drops partitions; B's `TTL … DELETE WHERE` per class works and costs merges |
| Cost to writers | `−` | `++` | A: nothing to code, everything to re-reason, plus dedup settings on the two re-senders and queryrunsd's anti-join; B: none |
| Cost to readers | `−−` | `++` | A retargets nine chstore readers, 17 views, play, loadstudy, sysmreplay and every downstream `ReadTable`; B: none |
| Migration risk | `−−` | `++` | A copies or renames every deployed table and needs a probe; B alters in place (index) or not at all |
| Reversibility | `−` | `++` | A's views can be dropped and the storage tables merged back, but every reader has moved; B's index is one `DROP INDEX` |
| Downstream impact | `+` | `+` | A was designed so a downstream store sets `ReadTable` and nothing else; B asks nothing. Both `+` because §SD9 already made the writer side invisible |
| Test lanes needed | `−−` | `−` | A: §3.7's five, two of them new kinds (dedup, partial failure); B: the measurement and an index-on-deployed-table lane |

Read as a whole: A wins every criterion about *what the stored shape can
do* and loses every criterion about *the cost of getting there*. The
weighting that decides it is the one the tree cannot supply — how much the
three waiting items are worth against the correctness budget of twelve
writers — and the one number that would move the read-pruning row (B's
measurement) does not exist.

## 6 What each option means for the items that wait on it

| Item | Under A | Under B |
| --- | --- | --- |
| **Correlation index** ([ADR-0277 §SD10](../adr/0277-one-audit-trail-for-model-calls-and-agent-work.md)), and the owner's "wait for the switch" | the main table's sort key answers it — if the key includes conversation or task, which §SD9 does not say (fork 1); the audit table's answers subject | the §SD5 bloom filter, after the measurement; the wait ends with a "no" and the index is built against the one table |
| **Backstop cursor** ([ADR-0330 §SD9](../adr/0330-a-shared-audit-recorder-below-the-runtime-one-sealed-writer-over-a-narrow-store-binding.md)) | `inserted_at` on the storage tables, the scheduled backstop persisting a position over it | stays the lagged re-scan, or the `_block_number` spike |
| **ADR-0296 §SD5's skip index** | moot for subject (the key), still open for principal | the item itself |
| **A viewer's reads** (play, the keelson relations, trailviews) | every one retargets to the `Merge` or a storage table, or reads empty | unchanged |
| **§SD9's six constraints** | (a)–(f) are built | an ADR has to release or re-scope them |
| **ADR-0296's deferred "integration test over the spike"** | becomes §3.7 | retired |

## 7 The forks an ADR has to settle

1. **The main table's key.** §SD9 fixes the audit table's `(subject, ts)`
   and says nothing of the main one. `(ts)` as now keeps the chstore
   readers' range reads and leaves correlation reads where they are;
   `(conversation, ts)` serves the trail and puts every row without a
   conversation at one key value.
2. **Two routes or more.** Audit and complement, or a route per family
   (model calls and messages are the volume; sysmetrics is a different
   plane). Each route is another target in every writer's commit (§3.2).
3. **Dedup: window, token, or accept duplicates.** Whether (e) is met by a
   window on each target plus the two settings, by a token derived from
   row ids, or by declaring the metrics plane duplicate-tolerant and the
   trail the only writer that must land once.
4. **Migration shape** (§3.5): copy, leave-and-rename, or recreate — and
   whether a pre-switch audit row that stays in the main table is
   acceptable.
5. **Read-table default.** Whether `ReadTable` empty should come to mean
   "the `Merge`" for every facts-bound store, or stay "the write table"
   and be set per caller; the former removes the silent-empty failure for
   generated stores and does nothing for the literal readers.
6. **Under B: release the constraints, or keep them.** Declining the
   switch while keeping (a), (c), (e), (f) binding preserves the option at
   the trail's expense; releasing them closes it.
7. **Under B: an insertion column without the switch.** Whether (c)'s
   `MATERIALIZED now64()` may be added to the one table by `ALTER`, with
   the deployed-table path that implies, or the re-scan is the answer.
8. **Who owns the measurement.** Both options' first milestone is the
   same measurement; whether it is taken before the ADR or as its M1.

## 8 Sources in the tree, and what the tree could not answer

### 8.1 Read for this page (2026-10-30)

Decisions:
[ADR-0296](../adr/0296-audit-events-as-a-trail-component.md),
[ADR-0277](../adr/0277-one-audit-trail-for-model-calls-and-agent-work.md),
[ADR-0330](../adr/0330-a-shared-audit-recorder-below-the-runtime-one-sealed-writer-over-a-narrow-store-binding.md),
[ADR-0327](../adr/0327-evidence-as-a-record-store-profile-chain-vault-marks-seals-and-anchors-any-store-opts-into.md),
[ADR-0100](../adr/0100-recordstore-generated-leeway-clickhouse-store.md),
[ADR-0115](../adr/0115-query-observability-data-plane-strategy.md),
[ADR-0191](../adr/0191-runtime-instance-attribution.md),
[ADR-0184](../adr/0184-sysmetrics-persistence-tee.md),
[ADR-0105](../adr/0105-keelson-adopts-generated-record-stores.md),
[ADR-0102](../adr/0102-leeway-clickhouse-table-clause-seam.md),
[ADR-0223](../adr/0223-watchbill-durable-work-on-facts.md),
[ADR-0237](../adr/0237-watchbill-worker-presence.md),
[ADR-0264](../adr/0264-retained-model-conversations-on-facts.md),
[ADR-0295](../adr/0295-recordstore-call-identity-observed-executors-and-write-observers.md),
[ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md),
[ADR-0135](../adr/0135-app-launch-requests.md),
[ADR-0198](../adr/0198-fs-snapshot-store.md),
[ADR-0218](../adr/0218-markdown-ingestor-facts-model.md),
[ADR-0259](../adr/0259-jackstay-guided-clickhouse-to-clickhouse-sync.md).

Explanations and trials:
[facts-bound record stores](../explanation/facts-bound-record-stores.md),
[leeway SQL read surface](../explanation/leeway-sql-read-surface.md),
[primary-key pruning through a SQL UDF](../explanation/clickhouse-udf-primary-key-pruning.md),
[query observability](../explanation/query-observability.md),
[watchbill consistency model](../explanation/watchbill-consistency-model.md),
[jsonbench on facts](../trials/jsonbench-on-facts/README.md) (§0 only, per
[citing a trial](../trials/README.md)).

Code — the DDL and its one path:
[`runtime_facts_ddl.out.go`](../../public/keelson/runtime/factsschema/ddl/runtime_facts_ddl.out.go),
[`chstore.go`](../../public/keelson/runtime/factsstore/chstore/chstore.go),
[`keelsonddl.go`](../../public/app/commands/keelsonddl/keelsonddl.go),
[`storegen.go`](../../public/keelson/runtime/factsschema/storegen/storegen.go),
[`gen.go`](../../public/storage/recordstore/gen/gen.go),
[`membership-assignments.golden`](../../public/keelson/runtime/vocab/testdata/membership-assignments.golden).
The insert path:
[`insert_settings.go`](../../public/storage/recordstore/insert_settings.go),
[`recordstore.go`](../../public/storage/recordstore/recordstore.go),
[`storeexec.go`](../../public/keelson/data/storeexec/storeexec.go),
[`chclient.go`](../../public/keelson/data/chclient/chclient.go),
[`jackstay/digest.go`](../../public/db/clickhouse/jackstay/digest.go).
Writers and readers: the files linked in §2.2 and §2.3, and
[`trailviews.go`](../../public/keelson/runtime/trail/trailviews/trailviews.go),
[`trail/reads.go`](../../public/keelson/runtime/trail/reads.go),
[`trail/facts_ddl_clickhouse.out.sql`](../../public/keelson/runtime/trail/facts_ddl_clickhouse.out.sql),
[`sysmfacts/facts_ddl_clickhouse.out.sql`](../../public/keelson/runtime/sysmfacts/facts_ddl_clickhouse.out.sql),
[`queryrunfacts/extract.go`](../../public/keelson/runtime/queryrunfacts/extract.go),
[`chstore/envvars.go`](../../public/keelson/runtime/factsstore/chstore/envvars.go).

### 8.2 Not answerable from the tree

Each is marked *(recalled)* above where it is used, and each is a spike
before an ADR leans on it:

1. Whether `insert_deduplicate` with
   `deduplicate_blocks_in_dependent_materialized_views` folds a re-sent
   Arrow block in a target that already took it when a sibling view
   refused — §SD9 (e), the spike's own gap.
2. The default and effect of `non_replicated_deduplication_window` on the
   server version deployed.
3. Whether a `bloom_filter` index on an `Array(UInt64)` or `Array(String)`
   value column prunes granules for `has()` at this table's mix of kinds —
   ADR-0277 §SD10's measurement.
4. Whether `_block_number` under `enable_block_number_column` is a usable
   insertion-order cursor across merges and after the ditch's mutation.
5. The cost of TTL `DELETE WHERE` rewrites against partition drops at the
   trail's volume.
6. What the partition key and sort key of the deployed tables actually are
   — `SetupTable` cannot say, and an operator may have passed an engine
   clause of their own.
7. The row volume per kind on a real host, which decides whether the
   switch's targets or the one table's index is the larger object.
