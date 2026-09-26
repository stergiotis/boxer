---
type: reference
audience: end-user
status: draft
title: Query read flow
summary: "Trace read bytes from users through query kinds into tables"
icon: "🌊"
endpoint: default
tabs: [sankey, table]
keywords: [clickhouse, system tables, introspection, server, system.query_log, sankey, read_bytes, users, tables, workload]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Query read flow

Where the server's reads went, from `system.query_log`: bytes read by
finished queries, flowing from the **user** who ran them, through the
**query kind**, into the **tables** they read. The widest band into a table
is where scan work concentrates.

**The knobs.** `minutes` is how far back to read. `top_tables` keeps that
many tables by bytes read and folds the rest into `(other tables)`, so the
right-hand column stays readable.

**Reading the widths.** A query that reads several tables has its full
`read_bytes` credited to each of them, because the log does not split reads
per table — so the table column can sum to more than the user column, and
the panel reports the kind in the middle as unbalanced. Only queries that
finished and read something are counted; failed, running and write-only ones
are not.

```sql
SET param_minutes = 60;
SET param_top_tables = 12;
WITH
  q AS (
    SELECT user, query_kind, read_bytes, tables
    FROM system.query_log
    WHERE type = 'QueryFinish'
      AND read_bytes > 0
      AND event_time >= now() - toIntervalMinute({minutes:UInt32})),
  tb AS (
    SELECT query_kind, tbl, read_bytes FROM q ARRAY JOIN tables AS tbl),
  top AS (
    SELECT tbl FROM tb GROUP BY tbl ORDER BY sum(read_bytes) DESC LIMIT {top_tables:UInt32}),
  bucketed AS (
    SELECT tb.query_kind AS query_kind, if(top.tbl = '', '(other tables)', tb.tbl) AS bucket, tb.read_bytes AS read_bytes
    FROM tb LEFT JOIN top ON tb.tbl = top.tbl),
  flows AS (
    SELECT concat('u:', user) AS source, concat('k:', query_kind) AS target, sum(read_bytes) AS value
    FROM q GROUP BY source, target
    UNION ALL
    SELECT concat('k:', query_kind) AS source, concat('t:', bucket) AS target, sum(read_bytes) AS value
    FROM bucketed GROUP BY source, target),
  nodes AS (
    SELECT DISTINCT id, substring(id, 3) AS label,
           multiIf(startsWith(id, 'u:'), 0, startsWith(id, 'k:'), 1, 2) AS stage
    FROM (SELECT source AS id FROM flows UNION ALL SELECT target FROM flows))
SELECT source, target, formatReadableSize(value) AS bytes_read
FROM flows
ORDER BY value DESC
```
