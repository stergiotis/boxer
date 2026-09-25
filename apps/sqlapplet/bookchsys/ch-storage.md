---
type: reference
audience: end-user
status: draft
title: Storage by column
summary: "Size every column on disk, coloured by compression ratio"
icon: "🧱"
endpoint: default
tabs: [treemap, icicle, table]
topics: [data]
keywords: [system.columns, disk, compression, bytes, size]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Storage by column

Every column's compressed bytes on disk, from `system.columns`, nested
database → table → column. In the Treemap tab a cell's **area** is the bytes
and its **colour** is the compression ratio (uncompressed over compressed),
so a large pale cell is a big column that compresses badly — usually the
first place to try a different codec or type. The colour scale is fixed at
1× to 20× and ratios above it draw as 20×: a constant column compresses by
three orders of magnitude, and a scale stretched to reach it would paint every
ordinary column the same dark. Clicking a database or table drills into it.

**The knob.** `db` narrows to databases matching it (`LIKE`).

**Reading it honestly.** The figures cover active parts of MergeTree-family
tables only; engines that do not report per-column sizes are absent rather
than zero. They are the server's accounting at the moment of the query, and
move as merges run.

```sql
SET param_db = '%';
SELECT [database, table, name] AS stack,
       data_compressed_bytes AS value,
       least(data_uncompressed_bytes / greatest(data_compressed_bytes, 1), 20) AS color,
       1 AS color_min,
       20 AS color_max,
       '×' AS color_unit
FROM system.columns
WHERE database LIKE {db:String} AND data_compressed_bytes > 0
```
