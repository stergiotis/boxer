---
type: reference
audience: end-user
status: draft
title: Leeway schema hierarchy
summary: "Trace which leeway tables share a schema shape"
icon: "🪢"
endpoint: default
tabs: [sankey, table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Leeway schema hierarchy

Which leeway tables share a schema, drawn as flows: each left-hand node is a
*shape* — a set of attributes two or more tables have in common — and each
ribbon runs from a shape to a table that carries it. A table appearing under
several shapes is the picture of a hierarchy: it satisfies a small shared core
and a larger one at the same time.

**A shape is an intersection, not a declaration.** Nobody wrote these down. The
catalog relates every pair of leeway tables with the leeway containment test
(style- and order-insensitive, exact canonical types) and hashes the attributes
the pair has in common; shapes that coincide unify on one id. For an equal or
subset pair the intersection is the whole of the contained side, so its shape id
*is* that table's schema hash — which is why a table's own schema shows up as a
node here when something else contains it.

**Ribbon thickness is the attribute count**, not rows or bytes. A thick ribbon
means the two tables agree about a lot, and says nothing about how much data
either holds.

**The knobs.** `min_common` is the floor on shared attributes — raise it to drop
the incidental agreements (a couple of backbone columns every facts table has)
and leave the real families. `top_shapes` keeps the widest shapes and drops the
long tail; both exist because a complete diagram of a hundred tables is not a
diagram. Whatever they hide is still in `boxer.tables_leeway_compatibility` —
the Table tab shows what was drawn, and only that.

Disjoint pairs are stored but never drawn: a shape with no members is not a
shape.

```sql
SET param_min_common = 5;
SET param_top_shapes = 12;

WITH
  pairs AS (
    SELECT
      shape_id,
      n_common,
      concat(database_a, '.', name_a) AS a,
      concat(database_b, '.', name_b) AS b
    FROM boxer.tables_leeway_compatibility
    WHERE relation != 'disjoint' AND n_common >= {min_common:UInt32}
  ),
  members AS (
    SELECT shape_id, n_common, a AS tbl FROM pairs
    UNION ALL
    SELECT shape_id, n_common, b AS tbl FROM pairs
  ),
  edges AS (
    SELECT shape_id, tbl, max(n_common) AS n_common
    FROM members
    GROUP BY shape_id, tbl
  ),
  ranked AS (
    SELECT shape_id, count() AS n_tables, max(n_common) AS n_attrs
    FROM edges
    GROUP BY shape_id
    ORDER BY n_tables DESC, n_attrs DESC
    LIMIT {top_shapes:UInt32}
  ),
  kept AS (
    SELECT * FROM edges WHERE shape_id IN (SELECT shape_id FROM ranked)
  ),
  flows AS (
    SELECT
      concat('shape:', lower(hex(shape_id))) AS source,
      tbl                                    AS target,
      toFloat64(n_common)                    AS value
    FROM kept
  ),
  nodes AS (
    SELECT
      concat('shape:', lower(hex(r.shape_id)))                                    AS id,
      concat(toString(r.n_attrs), ' attrs, ', toString(r.n_tables), ' tables')    AS label,
      0                                                                           AS stage,
      'shape'                                                                     AS `group`
    FROM ranked AS r
    UNION ALL
    SELECT DISTINCT
      tbl                        AS id,
      tbl                        AS label,
      1                          AS stage,
      splitByChar('.', tbl)[1]   AS `group`
    FROM kept
  )
SELECT * FROM flows ORDER BY value DESC, source, target
```

## Reading a shape's attributes

The diagram names shapes by hash, which is deliberately opaque — a shape has no
name because nobody named it. To see what one *is*, intersect the attribute
lists of the tables under it:

```text
SELECT arrayIntersect(
         (SELECT attr_keys FROM boxer.tables_leeway WHERE database = 'a' AND name = 't'),
         (SELECT attr_keys FROM boxer.tables_leeway WHERE database = 'b' AND name = 'u'))
```

No third table materializes this: `attr_keys` is already an array, and the
intersection is one function call away whenever a reader wants it.
