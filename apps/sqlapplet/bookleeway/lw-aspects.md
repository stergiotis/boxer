---
type: reference
audience: end-user
status: draft
title: What the vocabularies cost
summary: "Encoding hints, value semantics and uses against the bytes they sit on"
icon: "🏷"
endpoint: default
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# What the vocabularies cost

An aspect is declared per column and rides in the column's name; what it costs
is in `system.columns`. Joining the two is the question the aspect vocabulary
was made queryable for (ADR-0182 §SD4) — here as one table, grouped by aspect.

`v` picks the vocabulary: `enc` for encoding hints, `sem` for value semantics,
`use` for a section's declared uses.

**The rows overlap; they are not a partition.** A column declares several
aspects and is counted under each, so `stored` does not sum to the server's
total and two rows are not two disjoint populations. What the numbers support
is one aspect against another over whatever corpus this server happens to hold
— not "delta encoding saves X", which would need the same columns encoded both
ways.

`ratio` is stored over raw, so low is good. `tables` says how far an aspect's
evidence is spread: a ratio drawn from one table is that table's data, not the
aspect's.

Use aspects attach to sections rather than to columns, so under `use` every
column of a section carries its section's set — the counts are column counts
either way.

```sql
SET param_v = 'enc';
SELECT
  arrayJoin(multiIf({v:String} = 'sem', value_semantics,
                    {v:String} = 'use', use_aspects,
                    encoding_hints))         AS aspect,
  count()                                    AS columns_declaring,
  uniqExact((database, table))               AS tables,
  sum(data_uncompressed_bytes)               AS "raw@gloss/bytes",
  sum(data_compressed_bytes)                 AS "stored@gloss/bytes",
  round(sum(data_compressed_bytes) / sum(data_uncompressed_bytes), 4) AS ratio
FROM leeway.`columns`
WHERE layout != 'foreign' AND data_uncompressed_bytes > 0
GROUP BY aspect
ORDER BY sum(data_uncompressed_bytes) DESC
```
