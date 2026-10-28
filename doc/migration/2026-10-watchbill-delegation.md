---
type: how-to
audience: operator upgrading a deployment that already holds boxer.watchbill
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to migrate boxer.watchbill for the job's delegation

[ADR-0344](../adr/0344-agent-caused-jobs-in-a-process-of-their-own-and-the-entry-frame-every-stack-carries.md)
§SD2 adds three sections to the job table — `jobTask`, `jobTaskEpoch` and
`jobCall` — so a job row records the agent call that enqueued it. This
recipe is the DDL step. Nothing on `boxer.facts` or on the event table
changes shape.

## When to use this recipe

You have a deployment whose `boxer.watchbill` table was created before this
change, and you are upgrading past it. Skip it if the table does not exist:
`EnsureTable` creates the new shape from scratch, and it is `CREATE TABLE IF
NOT EXISTS`, which leaves an existing table exactly as it is.

The symptom of skipping it is a statement that names columns the table does
not have, failing every time until the table matches the code. An enqueue
fails on the insert, and the worker's poll fails on the read of held jobs,
logged as a warning on every tick whether or not anything is queued:

```text
WRN …/watchbill/worker.go > watchbill: poll
  Error: read held jobs
  └── cause: chclient post: non-200: Code: 47. DB::Exception: Unknown expression
      or function identifier `tv:jobTask:lr:lr:u64:1247:::0::data`.
```

## The shorter alternative

Job rows expire after `KEELSON_WATCHBILL_KEEP` (default a week) and a
running row is a lease, not state worth keeping across an upgrade. Where
nothing is in flight, dropping the table is the whole migration:

```sql
DROP TABLE IF EXISTS boxer.watchbill;
```

The next start recreates it. Use the ALTER below where queued or scheduled
rows must survive.

## Why an ALTER works here at all

The three new sections are declared after every existing one, in the DTO
and in the schema builder, so their columns are the last thirty of the
generated layout. `ADD COLUMN` appends, which reproduces that layout
exactly; the generated store reads columns by name, so the order matters
only to the eye. This is the same property
[ADR-0191](../adr/0191-runtime-instance-attribution.md)'s persist-state
recipe relied on.

## The statements

Generated from the difference between the DDL golden before and after the
change (`public/keelson/runtime/watchbill/watchbillstore/watchbill_ddl_clickhouse.out.sql`).
`IF NOT EXISTS` makes the block idempotent. The `SET` is required: six of
the columns are `Array(LowCardinality(UInt64))`, which ClickHouse refuses to
create without `allow_suspicious_low_cardinality_types` — the same setting
the generated `CREATE TABLE` carries. Run the block as one session (for
example `clickhouse-client --multiquery`), so the `SET` reaches every
`ALTER`.

```sql
SET allow_suspicious_low_cardinality_types = 1;

ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTask:value:val:s:24:::0::data" Array(LowCardinality(String)) CODEC(ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTask:hr:hr:u64:47:::0::data" Array(UInt64) CODEC(Delta,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTask:lr:lr:u64:1247:::0::data" Array(LowCardinality(UInt64)) CODEC(Delta,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTask:lv:lv:y:124:::0::data" Array(LowCardinality(String)) CODEC(ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTask:lmr:lmr:u64:1247:::0::data" Array(LowCardinality(UInt64)) CODEC(Delta,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTask:mrhp:mrhp:y:4:::0::data" Array(String) CODEC(ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTask:hrcard:hrcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTask:lrcard:lrcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTask:lvcard:lvcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTask:lmrcard:lmrcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTaskEpoch:value:val:u64:4:::0::data" Array(UInt64) CODEC(ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTaskEpoch:hr:hr:u64:47:::0::data" Array(UInt64) CODEC(Delta,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTaskEpoch:lr:lr:u64:1247:::0::data" Array(LowCardinality(UInt64)) CODEC(Delta,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTaskEpoch:lv:lv:y:124:::0::data" Array(LowCardinality(String)) CODEC(ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTaskEpoch:lmr:lmr:u64:1247:::0::data" Array(LowCardinality(UInt64)) CODEC(Delta,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTaskEpoch:mrhp:mrhp:y:4:::0::data" Array(String) CODEC(ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTaskEpoch:hrcard:hrcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTaskEpoch:lrcard:lrcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTaskEpoch:lvcard:lvcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobTaskEpoch:lmrcard:lmrcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobCall:value:val:s:24:::0::data" Array(LowCardinality(String)) CODEC(ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobCall:hr:hr:u64:47:::0::data" Array(UInt64) CODEC(Delta,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobCall:lr:lr:u64:1247:::0::data" Array(LowCardinality(UInt64)) CODEC(Delta,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobCall:lv:lv:y:124:::0::data" Array(LowCardinality(String)) CODEC(ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobCall:lmr:lmr:u64:1247:::0::data" Array(LowCardinality(UInt64)) CODEC(Delta,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobCall:mrhp:mrhp:y:4:::0::data" Array(String) CODEC(ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobCall:hrcard:hrcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobCall:lrcard:lrcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobCall:lvcard:lvcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.watchbill ADD COLUMN IF NOT EXISTS "tv:jobCall:lmrcard:lmrcard:u64:4E:::0::data" Array(UInt64) CODEC(T64,ZSTD(3));
```

## Check

```sql
SELECT count() FROM system.columns
WHERE database = 'boxer' AND table = 'watchbill' AND name LIKE 'tv:job%';
```

The count equals the number of `"tv:job…"` columns in the DDL golden; if
your layout uses another database, substitute it in every statement.
