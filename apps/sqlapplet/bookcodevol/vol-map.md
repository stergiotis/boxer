---
type: reference
audience: end-user
status: draft
title: Code volume map
icon: "🗺"
endpoint: introspection
tabs: [treemap, table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Code volume map

The binary as nested rectangles: **area** is contributed machine code,
**colour** is who wrote it. Three boxes at the top level — first-party,
third-party, standard library — each broken into the modules inside it. One
glance answers what the overview's percentages flatten: which specific
dependencies are the big ones.

The tree is deliberately **module-grained, not package-grained**. Module
attribution is exact (resolved against the module list the binary itself
declares), while package names are derived from symbol names and over-split
for generic code. This map shows only the part that is exact.

The standard library has no modules, so its box is broken down by top-level
directory (`net`, `crypto`, `runtime`, …) instead — the same shape at the
same grain.

**The knobs.** `size_by` picks the area: `text` is machine code, the honest
answer to "how big is this in the binary"; `data` sizes by data symbols
instead, which is a different and much lumpier picture — a few large tables
and buffers dominate it.

```sql
SET param_size_by = 'text';

WITH
  s AS (
    SELECT pkg_path, module_path, party,
           if({size_by:String} = 'data', data_bytes, text_bytes) AS bytes
    FROM keelson('go_symbols')
  ),
  -- The grouping key: the owning module, except for the standard library,
  -- which has none — there its top-level directory stands in.
  g AS (
    SELECT party,
           if(party = 'stdlib',
              arrayElement(splitByChar('/', pkg_path), 1),
              module_path) AS grp,
           bytes
    FROM s
  ),
  agg AS (
    SELECT party, grp, sum(bytes) AS bytes
    FROM g
    WHERE grp != ''
    GROUP BY party, grp
  ),
  parties AS (SELECT party, sum(bytes) AS bytes FROM agg GROUP BY party),
  nodes AS (
    -- leaves: one module (or one stdlib directory)
    SELECT grp                                        AS id,
           party                                      AS parent,
           arrayElement(splitByChar('/', grp), -1)    AS label,
           toFloat64(bytes)                           AS value,
           'B'                                        AS unit,
           party                                      AS color,
           bytes                                      AS bytes
    FROM agg
    UNION ALL
    -- the three party boxes
    SELECT party        AS id,
           'binary'     AS parent,
           party        AS label,
           toFloat64(0) AS value,
           'B'          AS unit,
           party        AS color,
           bytes        AS bytes
    FROM parties
    UNION ALL
    -- the root
    SELECT 'binary'          AS id,
           ''                AS parent,
           'binary'          AS label,
           toFloat64(0)      AS value,
           'B'               AS unit,
           'all'             AS color,
           sum(bytes)        AS bytes
    FROM agg
  ),
  total AS (SELECT sum(bytes) AS t FROM agg)
SELECT id, parent, label, value, unit, color,
       bytes,
       round(100 * bytes / nullIf((SELECT t FROM total), 0), 2) AS pct
FROM nodes
ORDER BY id
```

## Reading it honestly

- **Area is contributed machine code, after dead-code elimination.** A
  dependency that is large in source but barely called is small here. That
  is the point of this lens — and the reason it disagrees with a
  source-line count, which `keelson('go_packages')` provides when a
  toolchain is available.
- **Interior boxes have no area of their own.** A party box is a frame
  around its modules; its own `value` is zero and its `bytes` column carries
  the subtree total.
- **Colour is the party, not a scale** — three qualitative values on a
  qualitative palette, which is what the treemap panel renders honestly
  today.
- **Under `size_by = 'data'` the picture inverts.** The standard library
  dominates, because one FIPS buffer is tens of megabytes of zeroes. That is
  a real fact about the binary and a bad proxy for "amount of code", which is
  exactly why the two are separate knobs rather than one summed number.
