---
type: reference
audience: end-user
status: draft
title: Leeway tables
summary: "Which tables here carry leeway column names, and what they hold"
icon: "🧬"
endpoint: default
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Leeway tables

Every table on this server whose physical column names the leeway naming
convention composed, read straight out of `system.tables` and `system.columns`
with no snapshot in between (ADR-0226). Nothing has to be refreshed for this
to be current.

**`name_shape` counts column layouts; it is not a verdict.** `leeway` means
every column name decoded, `mixed` means some did and some did not, `foreign`
means none did — and none of those is the answer to "is this a leeway table".
That answer needs the restoring classifier, which rebuilds a `TableDesc` and is
written down in `boxer.tables_leeway` by a `boxer datacatalog refresh`
(ADR-0170); the [data catalog](../bookcatalog/cat-overview.md) book is its
front. What you have here is cheaper, always current, and claims less.

**`mixed` is usually correct rather than broken.** A leeway table with a
`MATERIALIZED` path column, or one hand-added column, reads as mixed — the
hand-added name is genuinely not a leeway name. Those tables sort to the top,
because a non-zero `n_foreign_columns` is the one column here worth a second
look; everything below the mixed rows decoded completely.

`sections` is the table's section vocabulary, which is what makes two tables
comparable at a glance — the [schema graph](lw-affinity.md) draws the same
fact.

```sql
SELECT
  database,
  name                  AS tbl,
  name_shape,
  n_foreign_columns,
  n_sections,
  n_columns,
  table_row_config,
  total_rows,
  data_compressed_bytes AS "stored@gloss/bytes",
  engine,
  sections
FROM leeway.tables
WHERE name_shape != 'foreign'
ORDER BY n_foreign_columns DESC, data_compressed_bytes DESC
```
