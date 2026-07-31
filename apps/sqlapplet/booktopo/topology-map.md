---
type: reference
audience: end-user
status: draft
title: Topology map
icon: "🕸"
endpoint: introspection
tabs: [network, table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Topology map

The appliance topology (ADR-0126) as a node-link diagram: components as
boxes, their declared `needs` edges, and each component's observed
listening sockets as ellipses. Drift is colour — the `group` column is a
component's origin set, so a declared-but-absent component tints
differently from a declared-and-observed one.

The observed half exists only while a scraper publishes on the metric
plane; without one the graph shows the declared inventory alone. Until a
desired-state store exists, "declared" means "in the compiled-in
registry", not "must run on this box". Marks are cooperative identity,
not a security boundary. The canonical queries behind this view live in
doc/howto/topology-queries.md.

```sql
WITH
  comp AS (
    SELECT key,
           arrayStringConcat(arraySort(groupArray(DISTINCT origin)), '+') AS origins
    FROM keelson('topology_nodes')
    WHERE kind = 'component'
    GROUP BY key
  ),
  lst AS (
    SELECT c.dst_key AS source, l.dst_key AS target
    FROM keelson('topology_edges') AS l
    INNER JOIN keelson('topology_edges') AS c
      ON l.src_key = c.src_key AND l.host = c.host
    WHERE l.edge_kind = 'proc-listens'
      AND c.edge_kind = 'proc-in-component'
  ),
  vertices AS (
    SELECT key AS id, substring(key, 11) AS label, origins AS `group`, 'box' AS shape
    FROM comp
    UNION ALL
    SELECT DISTINCT target AS id, substring(target, 6) AS label, 'listener' AS `group`, 'ellipse' AS shape
    FROM lst
  ),
  edges AS (
    SELECT src_key AS source, dst_key AS target, 'needs' AS label
    FROM keelson('topology_edges')
    WHERE edge_kind = 'component-needs'
    UNION ALL
    SELECT source, target, 'listens' AS label
    FROM lst
  )
SELECT * FROM edges
```
