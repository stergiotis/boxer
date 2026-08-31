---
type: reference
audience: end-user
status: draft
title: Data catalog overview
summary: "Count tables per database and how many leeway can rebuild"
icon: "🗂"
endpoint: default
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Data catalog overview

What this ClickHouse instance holds, per database: how many tables, how many of
them the leeway naming grammar could rebuild a schema from, and how many of the
rest a play panel could render as they stand (ADR-0170).

**This is a snapshot, and `dropped_since` / `created_since` say how stale.** The
four `boxer.tables_*` tables are derived data, replaced whole by
`boxer datacatalog refresh`; between runs they describe the instance as it was.
So this chapter checks itself: it re-reads `system.tables` live and counts, per
database, the tables the catalog lists that are **gone** and the ones on the
server it has **never seen**. Databases that have moved sort to the top. Two
zeroes down the column mean everything else here is current; anything else means
run a refresh before believing the row. `snapshot_age` is the same fact in
coarser form.

The other columns describe the catalog, not the server, and are therefore as old
as the run: a database dropped since the refresh still contributes its `tables`
and `leeway` counts. That is deliberate — the alternative is a chapter that
silently under-reports rather than one that says how out of date it is.

**"leeway" is a property of the column *names*, not of the data.** A table is
leeway iff its physical column names parse under the naming convention, which
is the same probe play runs on every result set before offering the Detail
card. Everything else is opaque, which is the expected answer for most tables
anyone writes by hand — it is not a defect and not a to-do.

**`opaque_with_a_shape` is the interesting number.** It counts opaque tables
that satisfy at least one known panel contract — a series, a set of flows, a
board of cards — and could therefore be drawn with a trivial query. The
[unmatched](cat-unmatched.md) chapter is the other side of it.

```sql
WITH
  run AS (
    SELECT any(run_id) AS run_id, max(discovered_at) AS at
    FROM boxer.tables_catalog
  ),
  live AS (
    SELECT database, name FROM system.tables
    WHERE database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')
  ),
  shaped AS (
    SELECT database, count(DISTINCT name) AS n
    FROM boxer.tables_opaque_shapes
    GROUP BY database
  ),
  merged AS (
    SELECT database, name, toString(kind) AS kind, n_columns,
           toUInt8(1) AS catalogued, toUInt8(0) AS still_there
    FROM boxer.tables_catalog
    UNION ALL
    SELECT database, name, '' AS kind, toUInt32(0) AS n_columns,
           toUInt8(0) AS catalogued, toUInt8(1) AS still_there
    FROM live
  ),
  per_table AS (
    SELECT database, name,
           max(catalogued)  AS catalogued,
           max(still_there) AS still_there,
           max(kind)        AS kind,
           max(n_columns)   AS n_columns
    FROM merged
    GROUP BY database, name
  )
SELECT
  t.database                                       AS database,
  countIf(t.catalogued = 1)                        AS tables,
  countIf(t.kind = 'leeway')                       AS leeway,
  countIf(t.kind = 'opaque')                       AS opaque,
  any(s.n)                                         AS opaque_with_a_shape,
  sum(t.n_columns)                                 AS n_columns,
  countIf(t.catalogued = 1 AND t.still_there = 0)  AS dropped_since,
  countIf(t.catalogued = 0 AND t.still_there = 1)  AS created_since,
  (SELECT run_id FROM run)                         AS run_id,
  formatReadableTimeDelta(now() - (SELECT at FROM run)) AS snapshot_age
FROM per_table AS t
LEFT JOIN shaped AS s ON s.database = t.database
GROUP BY t.database
ORDER BY dropped_since + created_since DESC, tables DESC, database
```
