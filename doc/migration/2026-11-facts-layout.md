---
type: how-to
audience: operator upgrading a deployment that already holds boxer.facts
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** The statements below have been run
> against clickhouse-local and against one ClickHouse server release, on a
> table of a few thousand rows (the `integration`-tagged tests of
> `factsstore/chstore`), not against a deployment.

# How to move boxer.facts into the monthly-partitioned layout

[ADR-0397](../adr/0397-the-facts-tables-partition-key-row-id-and-engine-clause.md)
creates a new `boxer.facts` partitioned by the month of `ts`
(`PARTITION BY toYYYYMM(ts) ORDER BY ts`), with the insert deduplication
window and a stamp. A table created before it is `ORDER BY ts` with no
partition key. ClickHouse changes no partition key in place, so moving an
existing table means creating one in the new layout and copying the rows.

contract: ddl/public/fs/lading/ladingpolicy/facts_ddl_clickhouse.out.sql
contract: ddl/public/keelson/runtime/sysmfacts/facts_ddl_clickhouse.out.sql
contract: ddl/public/keelson/runtime/trail/facts_ddl_clickhouse.out.sql
contract: ddl/public/keelson/runtime/watchbill/watchbillpresence/facts_ddl_clickhouse.out.sql
contract: ddl/public/semistructured/markdown/mddocfacts/facts_ddl_clickhouse.out.sql
contract: ddl/public/streaming/stevedore/stevedorefacts/facts_ddl_clickhouse.out.sql

## When to use this recipe

You hold a `boxer.facts` created before this change and want the new
layout: retention by dropping a month, and time-bounded reads that skip
months. Nothing requires it. The old table keeps working: every store reads
and writes either layout, and `CREATE TABLE IF NOT EXISTS` leaves it as it
is.

Skipping it shows as one line per start and in `boxer schema plan`:

```text
-- boxer.facts: ok — layout differs (informational)
--   note: layout differs (informational): the table is MergeTree, unpartitioned, order by `ts:ts:z64:47::0:`; this build creates MergeTree, partition by toYYYYMM("ts:ts:z64:47::0:"), order by "ts:ts:z64:47::0:". …
```

The plan still offers the deduplication window (`MODIFY SETTING`); it does
not stamp the table while the layout differs.

## The alternatives

- **Keep the old layout and quiet the note.** Set
  `BOXER_FACTS_ENGINE='MergeTree() ORDER BY "ts:ts:z64:47::0:"'` for every
  process. The check then expects the table's own layout, and the plan
  stamps it truthfully.
- **Drop the table** where the trail is expendable:
  `DROP TABLE IF EXISTS boxer.facts;` The next start creates it in the new
  layout.

## The statements

The authority for the new table is the DDL `chstore` composes,
which `boxer keelsonddl` prints; the facts-bound stores' goldens
(`public/keelson/runtime/trail/facts_ddl_clickhouse.out.sql` among them)
state the same engine clause.

1. **Stop every boxer process that writes the table** — hosts, workers,
   and anything with `BOXER_LOG_FACTS` set. A row written between the copy
   and the exchange would be left behind in the old table.

2. **Create the new table beside the old one:**

   ```sh
   boxer keelsonddl --table facts_next | clickhouse-client -mn
   ```

3. **Copy the rows.** The copy spans every month the table holds, so the
   per-insert partition limit is lifted for it, and block deduplication is
   off so that no two identical blocks are taken for one:

   ```sql
   INSERT INTO boxer.facts_next SELECT * FROM boxer.facts
   SETTINGS max_partitions_per_insert_block = 0, insert_deduplicate = 0;
   ```

4. **Check the counts agree:**

   ```sql
   SELECT (SELECT count() FROM boxer.facts) AS old, (SELECT count() FROM boxer.facts_next) AS new;
   ```

5. **Swap the tables** (an `Atomic` database, the default):

   ```sql
   EXCHANGE TABLES boxer.facts AND boxer.facts_next;
   ```

6. **Check, then drop the old table** (now named `facts_next`):

   ```sh
   boxer schema plan --table boxer.facts
   ```

   ```sql
   DROP TABLE boxer.facts_next;
   ```

Start the processes again. A host's query-run service recreates its
refreshable view over the table at start.

Leave `boxer.idlease` as it is throughout. It keeps the next unleased row
id of each stream and nothing else does. Setup recovers a lost one from
the largest id in the table, at the cost of reading the id column.

## Checking the result

`boxer schema plan --table boxer.facts` prints the table as `ok` with no
layout note and no statements. `system.parts` lists one partition id per
month that holds rows.

## What the existing rows read as

The same rows with the same values. Their ids are the old per-process
counters, small numbers that repeat across processes; rows written after the
upgrade carry leased ids (ADR-0397 §SD2), whose tag sits in high bits no
counter reached.
