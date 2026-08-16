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

Each party is subdivided by **its own natural unit**, because they do not
share one:

- **third-party** by module — attribution there is exact, resolved against
  the module list the binary itself declares.
- **the standard library** by top-level directory (`net`, `crypto`,
  `runtime`, …), because no module owns it.
- **first-party** by its own directories, two segments deep. Grouping it by
  module would be technically consistent and useless: this repository is a
  single module, so the largest box on the map would have no interior.

Package-grained subdivision is deliberately *not* offered: package names are
derived from symbol names and over-split for generic code, so the map would
gain detail it cannot stand behind. Everything drawn here is exact.

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
  -- Each party's natural unit differs, so each gets its own grouping key.
  -- Using the module for all three would leave first-party as one
  -- undifferentiated rectangle, since this repository is a single module.
  g AS (
    SELECT party,
           multiIf(
             -- no module owns the standard library; its top directory stands in
             party = 'stdlib', arrayElement(splitByChar('/', pkg_path), 1),
             -- one main module, so subdivide it by its own directories
             party = 'first',
               arrayStringConcat(arraySlice(
                 splitByChar('/', if(pkg_path = module_path, '.',
                                     substring(pkg_path, length(module_path) + 2))),
                 1, 2), '/'),
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
  labelled AS (
    SELECT party, grp, bytes,
           -- A module path's last segment is its name, except when that
           -- segment is a bare major-version suffix: `…/arrow-go/v18` is
           -- named arrow-go, not v18.
           if(party = 'third',
              if(match(arrayElement(splitByChar('/', grp), -1), '^v[0-9]+$')
                 AND length(splitByChar('/', grp)) > 1,
                 arrayElement(splitByChar('/', grp), -2),
                 arrayElement(splitByChar('/', grp), -1)),
              grp) AS label
    FROM agg
  ),
  nodes AS (
    -- leaves: one module, one stdlib directory, or one first-party directory
    SELECT concat(party, '/', grp) AS id,   -- party-scoped so the three key spaces cannot collide
           party                   AS parent,
           label                   AS label,
           toFloat64(bytes)        AS value,
           'B'                     AS unit,
           party                   AS color,
           bytes                   AS bytes
    FROM labelled
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
    -- the root. Its colour is its own name rather than a fourth pseudo-party:
    -- the legend lists distinct colour values, so inventing one here would put
    -- a swatch in it for something that is not a party at all.
    SELECT 'binary'          AS id,
           ''                AS parent,
           'binary'          AS label,
           toFloat64(0)      AS value,
           'B'               AS unit,
           'binary'          AS color,
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
