---
type: reference
audience: end-user
status: draft
title: Section anatomy
summary: "What each leeway section stores, and how much is membership machinery"
icon: "🫀"
endpoint: default
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Section anatomy

A leeway section holds values of one kind, and beside the value column it
carries the lanes that say which logical attribute each value belongs to. This
chapter is that split, per section: how many columns hold data, how many are
the machinery, and what the machinery costs.

`support_lanes` is the number worth reading first. A section with one value
column and eight support lanes is not waste — those lanes are what let many
logical attributes share one physical column, which is the whole trade — but
they are where the bytes of a wide schema go, and `stored` prices it.

`db` and `tbl` are `LIKE` patterns, so the default `%` is every leeway table on
the server, ordered by what it stores. Narrow them to one table to read that
table alone.

`roles` and `lane_kinds` name the machinery. `val` is the data; `hr` / `lr` /
`lmr` and their `…card` companions are membership identity and cardinality;
`len` and `card` size arrays and sets. `use_aspects` are the section's declared
uses, which ride in the column names and are not stored per row.

```sql
SET param_db = '%';
SET param_tbl = '%';
SELECT
  database,
  table                             AS tbl,
  section,
  layout,
  n_value_columns,
  n_columns - n_value_columns       AS support_lanes,
  data_compressed_bytes             AS "stored@gloss/bytes",
  value_columns,
  canonical_types,
  roles,
  lane_kinds,
  use_aspects,
  streaming_group
FROM leeway.sections
WHERE database LIKE {db:String} AND table LIKE {tbl:String}
ORDER BY data_compressed_bytes DESC, database, tbl, section
```
