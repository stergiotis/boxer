---
type: reference
audience: end-user
status: draft
title: Go architecture
summary: "Draw the quotient graph of apps and public subsystems"
icon: "🏛"
endpoint: introspection
tabs: [table, network, detail]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Go architecture

One step back from packages: each `apps/<name>` and each `public/<area>` is a
**group**, and the graph of groups — the quotient — is small enough to draw
whole. Use it to see how the subsystems relate, whether sibling apps stay
independent, and where dependency cycles hide.

**Two things are coloured, not just grouped.** A forbidden `apps/a → apps/b`
edge draws in the **error** tone; an edge between two groups that depend on
each other draws in the **warning** tone. Group nodes inside a cycle carry the
warning tone too. Everything else is coloured by class.

**The knobs.** `group_depth` trades detail for overview (2 gives
`public/keelson`, 3 gives `public/keelson/runtime`). `show_external` folds the
third-party modules in. `app_prefix` is the sibling-independence rule's
subject — `apps/` here. `cycle_depth` bounds the cycle search.

The sibling rule keys on the **app directory**, not on the group, so sliding
`group_depth` coarser never hides a real violation: the `violations` column
and the error-toned edges are computed from package-level edges either way.
It flags **direct** app-to-app imports; a transitive coupling (`app A → some
library → app B`) is visible as a path through the quotient but is not
reddened.

```sql
SET param_group_depth = 2;
SET param_show_external = 0;
SET param_app_prefix = 'apps/';
SET param_cycle_depth = 6;
SET param_list_cap = 40;

WITH RECURSIVE
  root AS (SELECT root_module AS m FROM keelson('go_collection')),
  g AS (
    SELECT p.import_path AS path,
           p.class AS class,
           multiIf(
             p.class = 'stdlib', 'std',
             p.class = 'external', p.module_path,
             arrayStringConcat(
               arraySlice(
                 splitByChar('/', substring(p.import_path, length((SELECT m FROM root)) + 2)),
                 1, {group_depth:UInt8}), '/')) AS grp,
           if(p.class = 'internal'
                AND startsWith(substring(p.import_path, length((SELECT m FROM root)) + 2), {app_prefix:String}),
              arrayStringConcat(
                arraySlice(
                  splitByChar('/', substring(p.import_path, length((SELECT m FROM root)) + 2)),
                  1, 2), '/'),
              '') AS app
    FROM keelson('go_packages') AS p
    WHERE p.class = 'internal' OR ({show_external:UInt8} = 1 AND p.class = 'external')
  ),
  -- One row per package-level edge that crosses a group boundary, carrying
  -- the app-directory verdict the sibling rule needs.
  xe AS (
    SELECT gs.grp AS source, gd.grp AS target,
           (gs.app != '' AND gd.app != '' AND gs.app != gd.app) AS violating,
           gs.path AS src_path, gd.path AS dst_path
    FROM keelson('go_imports') AS i
    INNER JOIN g AS gs ON i.src_path = gs.path
    INNER JOIN g AS gd ON i.dst_path = gd.path
    WHERE gs.grp != gd.grp
  ),
  qe AS (
    SELECT source, target, count() AS weight, countIf(violating) AS violations
    FROM xe
    GROUP BY source, target
  ),
  -- Mutual reachability inside cycle_depth hops: a group that reaches itself
  -- is in a cycle. The bound is what makes this terminate at all — a
  -- ClickHouse recursive CTE has no DISTINCT across iterations, so a cyclic
  -- walk would otherwise spin. Measured on this repository, the answer stops
  -- changing at depth 6.
  qreach AS (
    SELECT source AS a, target AS b, 1 AS depth FROM qe
    UNION ALL
    SELECT DISTINCT r.a, q.target, r.depth + 1
    FROM qreach AS r
    INNER JOIN qe AS q ON r.b = q.source
    WHERE r.depth < {cycle_depth:UInt8}
  ),
  cyclic AS (SELECT DISTINCT a AS grp FROM qreach WHERE a = b),
  -- The offending edges themselves, so a flagged group can say which import
  -- broke the rule rather than only that one did.
  viol AS (
    SELECT source AS grp,
           arraySlice(groupUniqArray(concat(src_path, ' ▶ ', dst_path)), 1, {list_cap:UInt16}) AS pairs
    FROM xe
    WHERE violating
    GROUP BY grp
  ),
  groups AS (
    SELECT g.grp AS key,
           any(g.class) AS class,
           count() AS packages,
           (SELECT count() FROM qe WHERE qe.source = g.grp) AS out_deg,
           (SELECT count() FROM qe WHERE qe.target = g.grp) AS in_deg,
           (SELECT sum(violations) FROM qe WHERE qe.source = g.grp) AS violations,
           g.grp IN (SELECT grp FROM cyclic) AS in_cycle,
           arraySlice(groupUniqArray(g.path), 1, {list_cap:UInt16}) AS members,
           (SELECT pairs FROM viol WHERE viol.grp = g.grp) AS violation_edges
    FROM g
    GROUP BY key, g.grp
    ORDER BY packages DESC, key
  ),
  vertices AS (
    SELECT key AS id,
           concat(key, ' ·', toString(packages)) AS label,
           class AS `group`,
           multiIf(violations > 0, 'error', in_cycle, 'warning', '') AS tone
    FROM groups
  ),
  edges AS (
    SELECT qe.source AS source,
           qe.target AS target,
           toString(qe.weight) AS label,
           multiIf(qe.violations > 0, 'error',
                   (qe.target, qe.source) IN (SELECT source, target FROM qe), 'warning',
                   '') AS tone
    FROM qe
  )
SELECT * FROM groups
```

## Reading the table

- **packages** — how many packages the group folds.
- **out_deg / in_deg** — how many *groups* it depends on, and how many depend
  on it, at the current `group_depth`.
- **violations** — direct app-to-app import edges leaving this group. Nonzero
  is a broken invariant, not a smell.
- **in_cycle** — the group is part of a dependency cycle between groups. Go
  forbids *package* import cycles; groups can still cycle, and that mutual
  dependency is the strongest "intertangled" signal there is.
- **members / violation_edges** — the packages the group folds, and the
  offending `importer ▶ imported` pairs behind `violations`. Click a group and
  read them in the Detail tab; both are capped at `list_cap` entries while the
  counts beside them stay exact.

## Bounds, stated

The cycle search is bounded to `cycle_depth` hops. On this repository the set
of groups in cycles stops growing at depth 6 (36 groups at depth 4, 37 at
depth 6 and at depth 8), so the default is a safety rail rather than a
truncation — but on another module a longer cycle would go unreported. The
quotient itself is exact.
