---
type: reference
audience: end-user
status: draft
title: Table dependencies
summary: "Draw the tables, views and dictionaries feeding each other"
icon: "⛓"
endpoint: default
tabs: [network, graphview, table]
topics: [data]
keywords: [clickhouse, system tables, introspection, server, system.tables, graph, materialized view, view, dictionary, lineage, dependencies]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Table dependencies

How the tables on this server feed each other, read from `system.tables`: a
materialized view's source table **feeds** it, the view **writes to** its
target (an explicit `TO` table or its `.inner_id.` table), a dictionary
**loads** from its source table and a table whose defaults call `dictGet`
loads from the dictionary. Vertices are coloured by engine and sized by the
bytes they hold.

```md preamble
Edges labelled **reads (parsed)** come from a pattern match over plain views'
SQL, not from the server — see the caveat below.
```

**The knob.** `db` narrows to databases matching it (`LIKE`); `%` keeps all
of them except the system ones.

**What the server does not record.** A plain `VIEW` keeps no dependency list,
so its inputs are recovered by a regular expression over `as_select` that
takes the name after each `FROM` and `JOIN`. It misses table functions and
names written with quotes, and it draws a false edge for a name that is a CTE
of the view rather than a table. Treat those edges as a hint. Materialized
view, dictionary and `TO` edges come from the server and are exact.

```sql
SET param_db = '%';
WITH
  t AS (
    SELECT * FROM system.tables
    WHERE database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')
      AND database LIKE {db:String}),
  edges AS (
    SELECT concat(database, '.', name) AS source, concat(dd, '.', dt) AS target, 'feeds' AS label
    FROM t ARRAY JOIN dependencies_database AS dd, dependencies_table AS dt
    UNION ALL
    SELECT concat(database, '.', name), concat(target_database, '.', target_table), 'writes to'
    FROM t WHERE target_table != ''
    UNION ALL
    SELECT concat(dd, '.', dt), concat(database, '.', name), 'loads'
    FROM t ARRAY JOIN loading_dependencies_database AS dd, loading_dependencies_table AS dt
    UNION ALL
    SELECT if(position(ref, '.') > 0, ref, concat(database, '.', ref)), concat(database, '.', name), 'reads (parsed)'
    FROM t ARRAY JOIN extractAll(as_select, '(?i)\\b(?:FROM|JOIN)\\s+([\\w.]+)') AS ref
    WHERE engine = 'View'),
  vertices AS (
    SELECT concat(database, '.', name) AS id, name AS label, engine AS group,
           multiIf(engine LIKE '%View', 'ellipse', engine = 'Dictionary', 'circle', 'box') AS shape,
           total_bytes AS weight
    FROM t)
SELECT source, label, target FROM edges
```
