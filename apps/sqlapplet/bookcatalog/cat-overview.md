---
type: reference
audience: end-user
status: draft
title: Data catalog overview
icon: "🗂"
endpoint: default
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Data catalog overview

What this ClickHouse instance holds, per database: how many tables, how many of
them the leeway naming grammar could rebuild a schema from, and how many of the
rest a play panel could render as they stand (ADR-0170).

**This is a snapshot, not a live view.** The four `boxer.tables_*` tables are
derived data, replaced whole by `boxer datacatalog refresh`; between runs they
describe the instance as it was. The last two columns say when that was — if
`discovered_at` is old, so is everything above it.

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
  shaped AS (
    SELECT database, count(DISTINCT name) AS n
    FROM boxer.tables_opaque_shapes
    GROUP BY database
  )
SELECT
  c.database                    AS database,
  count()                       AS tables,
  countIf(c.kind = 'leeway')    AS leeway,
  countIf(c.kind = 'opaque')    AS opaque,
  any(s.n)                      AS opaque_with_a_shape,
  sum(c.n_columns)              AS n_columns,
  (SELECT run_id FROM run)      AS run_id,
  (SELECT at FROM run)          AS discovered_at
FROM boxer.tables_catalog AS c
LEFT JOIN shaped AS s ON s.database = c.database
GROUP BY c.database
ORDER BY tables DESC, database
```
