---
type: reference
audience: end-user
status: draft
title: Coverage map
summary: "Map the package tree by coverage bracket and code size"
icon: "🗺"
endpoint: introspection
tabs: [treemap, table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Coverage map

The instrumented package tree as nested rectangles: each package's **area**
is its amount of code, each **colour** its coverage bracket. One glance
answers the question the overview's single percentage flattens — *where* the
covered code lives, and which big boxes are still dark.

The tree is the import-path hierarchy with the module prefix trimmed, so
the top level is the repository's own directories. Interior nodes are
directories; a directory that is also a package carries its own rectangle
inside its box (the ADR-0166 nodes contract), so `a/b`'s own code is not
silently redistributed into `a/b/c`.

**The knobs.** `size_by` picks the area: `stmts` is the package's code size
— the map of the codebase, coloured by coverage — and `uncovered` sizes by
*untested* statements, which turns the map into a work list: the biggest
rectangle is the biggest gap.

**Colour is the ratio itself, on a fixed 0–100 scale.** The tint is the
subtree's covered percentage, read off a sequential ramp whose ends the query
*declares* (`color_min` / `color_max`) rather than leaving to be surveyed from
this repository's own spread. That matters: surveyed, a tree spanning 12–68%
would paint 68% at the top of the ramp, which reads as fully covered — and two
runs could not be compared by colour at all. A package with no statements has
no ratio, so it is left uncoloured and falls through to the neutral depth
shading. A container's colour is its **subtree** ratio, so a directory can sit
mid-ramp while one child inside it is at 100.

```sql
SET param_size_by = 'stmts';

WITH
  pk AS (
    SELECT pkg_path, module_path, covered_stmts, total_stmts
    FROM keelson('coverage_pkgs')
  ),
  -- import paths relative to the module, so the tree starts at the
  -- repository's directories rather than three hostname levels; the module
  -- root package (if any) becomes '.'
  rel AS (
    SELECT multiIf(pkg_path = module_path, '.',
                   startsWith(pkg_path, concat(module_path, '/')),
                   substring(pkg_path, length(module_path) + 2),
                   pkg_path) AS rpath,
           covered_stmts, total_stmts
    FROM pk
  ),
  -- every prefix of every relative path is a node; each package contributes
  -- its statements to all of its prefixes, which is the subtree total
  -- without a join
  contrib AS (
    SELECT arrayJoin(arrayMap(i -> arrayStringConcat(arraySlice(splitByChar('/', rpath), 1, i), '/'),
                              range(1, length(splitByChar('/', rpath)) + 1))) AS id,
           covered_stmts, total_stmts
    FROM rel
  ),
  agg AS (
    SELECT id, sum(total_stmts) AS sub_total, sum(covered_stmts) AS sub_cov
    FROM contrib
    GROUP BY id
  ),
  own AS (
    SELECT rpath AS id, total_stmts AS own_total, covered_stmts AS own_cov
    FROM rel
  ),
  nodes AS (
    SELECT a.id AS id,
           if(position(a.id, '/') = 0,
              (SELECT any(module_path) FROM pk),
              arrayStringConcat(arraySlice(splitByChar('/', a.id), 1, length(splitByChar('/', a.id)) - 1), '/')) AS parent,
           arrayElement(splitByChar('/', a.id), -1) AS label,
           if({size_by:String} = 'uncovered',
              toFloat64(o.own_total - o.own_cov),
              toFloat64(o.own_total)) AS value,
           '' AS unit,
           a.sub_total AS subtree_stmts,
           a.sub_cov AS subtree_covered,
           round(100 * a.sub_cov / nullIf(a.sub_total, 0), 1) AS pct
    FROM agg AS a
    LEFT JOIN own AS o ON a.id = o.id
    UNION ALL
    SELECT any(module_path) AS id,
           '' AS parent,
           any(module_path) AS label,
           toFloat64(0) AS value,
           '' AS unit,
           sum(total_stmts) AS subtree_stmts,
           sum(covered_stmts) AS subtree_covered,
           round(100 * sum(covered_stmts) / nullIf(sum(total_stmts), 0), 1) AS pct
    FROM pk
  )
-- the colour channel: the subtree's covered ratio, on the scale a RATIO has
-- rather than the one this repository happens to span. `color` is the binding;
-- `pct` is the same number under the name the rest of the book calls it.
SELECT id, parent, label, value, unit,
       pct            AS color,
       toFloat64(0)   AS color_min,
       toFloat64(100) AS color_max,
       '%'            AS color_unit,
       subtree_stmts, subtree_covered, pct
FROM nodes
ORDER BY id
```

## Reading it honestly

- **Area is the package's own statements**, and directories have area only
  through their packages — a pure directory node is a frame, not a claim.
  Under `size_by = 'uncovered'` a fully covered package vanishes; that is
  the point of the work-list reading, and the `stmts` view is where it
  comes back.
- **The ratios are cumulative-session facts.** A package low on the ramp five
  minutes in may only mean its windows have not been opened yet; watch the
  map after driving the feature you care about.
- **Colour does not follow `size_by`.** Under `uncovered` the *area* becomes
  the gap while the tint stays the covered ratio, which is what makes that
  view read as a work list: a big rectangle still high on the ramp is a large
  package with a small proportional hole.
- **The scale is pinned, so the ramp ends are absolute.** Nothing here is
  normalised to the best or worst package present; the top of the bar is 100%
  whether or not anything reaches it. The Treemap tab's status line says
  `declared scale` when this is in force.
- **The Table tab carries `subtree_stmts`, `subtree_covered` and `pct`** —
  the exact numbers behind a rectangle you cannot judge by eye.
