---
type: reference
audience: end-user
status: draft
title: Tables that share a schema
summary: "Leeway tables joined by the sections they have in common"
icon: "🕸"
endpoint: default
tabs: ["graphview", "network", table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Tables that share a schema

Two tables built from the same leeway schema carry the same sections, so the
count of sections a pair has in common is a usable affinity measure — and a
schema family comes out as a clique.

The **Graphview** tab is the reading that suits it: a force layout puts a
clique in a clump, where a ranked drawing spreads it along a rank. Switch
**auras by group** on and each database blobs separately. The **Network** tab
reads the same two CTEs ranked, and the Table tab is the pairs.

**This is affinity by name shape, not the pairwise verdict.** `equal`,
`subset`, `overlap` and `disjoint` come from the restoring classifier
comparing normalized `TableDesc`s, and live in
`boxer.tables_leeway_compatibility` (ADR-0170) after a refresh. What this draws
is the cheap, always-current approximation of the same picture: it knows two
tables share a section named `symbol`, not that they agree about what is in it.

`floor` keeps the graph readable. Every leeway table carries the backbone
sections, so at `floor = 1` everything connects to everything and the layout
has nothing left to say; raise it on a server with many tables, lower it to see
the weak ties.

The tuple comparison is what keeps each pair once — a cross join offers both
orders and the self-pair, and an undirected graph wants neither.

```sql
SET param_floor = 2;
WITH
  ts AS (
    SELECT database, table, groupUniqArray(section) AS secs
    FROM leeway.sections
    GROUP BY database, table
  ),
  vertices AS (
    SELECT
      concat(database, '.', table) AS id,
      table                        AS label,
      database                     AS `group`,
      length(secs)                 AS weight
    FROM ts
  ),
  edges AS (
    SELECT
      concat(a.database, '.', a.table)                 AS source,
      concat(b.database, '.', b.table)                 AS target,
      toString(length(arrayIntersect(a.secs, b.secs))) AS label,
      length(arrayIntersect(a.secs, b.secs))           AS weight
    FROM ts AS a
    CROSS JOIN ts AS b
    WHERE (a.database, a.table) < (b.database, b.table)
      AND length(arrayIntersect(a.secs, b.secs)) >= {floor:UInt32}
  )
SELECT source, target, weight AS shared_sections
FROM edges
ORDER BY weight DESC, source, target
```
