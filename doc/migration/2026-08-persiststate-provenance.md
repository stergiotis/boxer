---
type: how-to
audience: operator upgrading a deployment that already holds boxer.persiststate
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to migrate boxer.persiststate for run and window provenance

[ADR-0191](../adr/0191-runtime-instance-attribution.md) §SD5 adds two sections
to the persist-state store — `stateRunId` and `stateInstanceKey` — so a
persist write records which process and which window made it. This recipe is
the DDL step. It is the only migration ADR-0191 needs; nothing on
`boxer.facts` changes shape.

## When to use this recipe

You have a deployment whose `boxer.persiststate` table was created before this
change, and you are upgrading past it. Skip it if the table does not exist —
`EnsureTable` creates the new shape from scratch.

The symptom of skipping it is not silence. `PersistStore.VerifySchema` runs at
open and compares the live column list to the generated one, positionally:

```
schema drift on boxer.persiststate: table has 33 columns, the generated schema
expects 53 — regenerated code against an old table (or vice versa); migrate or
regenerate
```

The host then falls back to the in-memory persist backend (`persist:mem` in
the status bar) and forgets app state at every restart. `EnsureTable` cannot
fix it: it is `CREATE TABLE IF NOT EXISTS`, which leaves an existing table
exactly as it is.

## Why an ALTER works here at all

The store decodes **positionally**, so a column in the wrong place is worse
than a missing one. Both new sections land at the *end* of the generated
column list — they are declared last in `loadPersistSchema` — so appending
them in order reproduces the generated layout exactly, and every existing
column keeps its position. That is what makes this an append rather than a
rebuild; it is a property of these two sections, not a general rule for this
table.

## Step 1 — apply the ALTERs

Twenty columns, in this order. `IF NOT EXISTS` makes the script re-runnable,
and `allow_suspicious_low_cardinality_types` is required because two of the
lanes are `Array(LowCardinality(UInt64))` — the same setting the generated
`CREATE TABLE` carries.

```sql
SET allow_suspicious_low_cardinality_types = 1;

ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateRunId:value:val:s:24:::0::data"           Array(LowCardinality(String)) CODEC(ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateRunId:hr:hr:u64:47:::0::data"             Array(UInt64) CODEC(Delta,ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateRunId:lr:lr:u64:1247:::0::data"           Array(LowCardinality(UInt64)) CODEC(Delta,ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateRunId:lv:lv:y:124:::0::data"              Array(LowCardinality(String)) CODEC(ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateRunId:lmr:lmr:u64:1247:::0::data"         Array(LowCardinality(UInt64)) CODEC(Delta,ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateRunId:mrhp:mrhp:y:4:::0::data"            Array(String) CODEC(ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateRunId:hrcard:hrcard:u64:4E:::0::data"     Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateRunId:lrcard:lrcard:u64:4E:::0::data"     Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateRunId:lvcard:lvcard:u64:4E:::0::data"     Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateRunId:lmrcard:lmrcard:u64:4E:::0::data"   Array(UInt64) CODEC(T64,ZSTD(3));

ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateInstanceKey:value:val:u64:4:::0::data"         Array(UInt64) CODEC(ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateInstanceKey:hr:hr:u64:47:::0::data"            Array(UInt64) CODEC(Delta,ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateInstanceKey:lr:lr:u64:1247:::0::data"          Array(LowCardinality(UInt64)) CODEC(Delta,ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateInstanceKey:lv:lv:y:124:::0::data"             Array(LowCardinality(String)) CODEC(ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateInstanceKey:lmr:lmr:u64:1247:::0::data"        Array(LowCardinality(UInt64)) CODEC(Delta,ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateInstanceKey:mrhp:mrhp:y:4:::0::data"           Array(String) CODEC(ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateInstanceKey:hrcard:hrcard:u64:4E:::0::data"    Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateInstanceKey:lrcard:lrcard:u64:4E:::0::data"    Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateInstanceKey:lvcard:lvcard:u64:4E:::0::data"    Array(UInt64) CODEC(T64,ZSTD(3));
ALTER TABLE boxer.persiststate ADD COLUMN IF NOT EXISTS "tv:stateInstanceKey:lmrcard:lmrcard:u64:4E:::0::data"  Array(UInt64) CODEC(T64,ZSTD(3));
```

The list above is transcribed from the generated DDL. If it and
`public/keelson/runtime/persist/persiststore/persiststate_ddl_clickhouse.out.sql`
ever disagree, the generated file is the authority — the checking step below
compares against it rather than against this page.

## Step 2 — check the result positionally

Column *order* is what the decode depends on, so a count check is not enough:

```sql
SELECT groupArray(name) = (
  -- paste the column list from the generated DDL, in file order
  ['id:id:s:4::0:', 'ts:ts:z64:47::0:', …]
) AS layout_ok
FROM (SELECT name FROM system.columns
      WHERE database = 'boxer' AND table = 'persiststate' ORDER BY position)
```

The cheaper check is to start the host: `VerifySchema` runs at open and is the
same comparison, so a clean boot with `persist:store` in the status bar is the
verdict.

## The alternative: drop and recreate

App persist state is UI state — pane sizes, last-selected rows, saved applet
definitions. If losing it is acceptable, this is shorter and cannot land the
columns in the wrong order:

```sql
DROP TABLE boxer.persiststate
```

`EnsureTable` recreates it at the next open. Runtime-saved SQL applets live
here too ([ADR-0132](../adr/0132-sqlapplet-sql-defined-applets.md) route 1), so
check whether any are worth exporting first — committed applets are unaffected.

## What the old rows say afterwards

Nothing, and that is by design. Rows written before the migration have empty
runs of the two new sections, which reads back as an absent membership rather
than as run `""` or window `0`. There is no backfill: the process and window
that wrote a historical value are not recoverable from anything else on the
row.
