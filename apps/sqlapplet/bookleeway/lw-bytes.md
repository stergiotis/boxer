---
type: reference
audience: end-user
status: draft
title: Where the bytes went
summary: "Every leeway column as area — database, table, section, lane kind"
icon: "🧊"
endpoint: default
tabs: ["icicle:nodes", "treemap:nodes", table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Where the bytes went

Every leeway column on the server, nested database → table → section → lane
kind → column and sized by what it stores. The Icicle and Treemap tabs read the
`nodes` CTE; the Table tab is the same rows ranked.

This is the picture the [section anatomy](lw-anatomy.md) chapter counts: the
`value` band against the `membership`, `membership-cardinality`, `length` and
`set-cardinality` bands beside it, at whatever proportion this server's data
actually produces.

**Colour is the compression ratio, and its scale is declared rather than
surveyed.** `color_min` / `color_max` pin the ramp to 0–1, so a column that
compresses badly reads as compressing badly instead of merely as the worst one
present — which is what a ratio needs and what surveying the result would get
wrong. Low is good: `0.02` is fifty-to-one.

Columns with nothing stored are dropped rather than drawn at zero area. An
empty table contributes no rectangle, not an invisible one.

```sql
WITH
  nodes AS (
    SELECT
      [database, table, section, lane_kind, leeway_column] AS stack,
      sum(data_compressed_bytes)                           AS value,
      'B'                                                  AS unit,
      sum(data_compressed_bytes) / sum(data_uncompressed_bytes) AS color,
      0.0     AS color_min,
      1.0     AS color_max,
      'ratio' AS color_unit
    FROM leeway.`columns`
    WHERE layout != 'foreign' AND data_compressed_bytes > 0 AND data_uncompressed_bytes > 0
    GROUP BY database, table, section, lane_kind, leeway_column
  )
SELECT
  stack[1]              AS database,
  stack[2]              AS tbl,
  stack[3]              AS section,
  stack[4]              AS lane_kind,
  stack[5]              AS leeway_column,
  value                 AS "stored@gloss/bytes",
  round(color, 3)       AS ratio
FROM nodes
ORDER BY value DESC
```
