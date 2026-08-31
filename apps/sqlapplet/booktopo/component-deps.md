---
type: reference
audience: end-user
status: draft
title: Component dependencies
summary: "Draw the declared component needs graph and its closure"
icon: "⛓"
endpoint: introspection
tabs: [network, table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Component dependencies

The declared `needs` graph. The Network tab draws every component-needs
edge; the Table tab lists the transitive dependency closure of the
component named by the `root` parameter (a `component:<token>` node key,
walked WITH RECURSIVE over the same edges).

This is the declared side only — what the compiled-in registry says a
component needs, whether or not anything runs. For declared-vs-observed
disagreement see the Topology drift applet.

```sql
SET param_root = 'component:caddy';
WITH RECURSIVE closure AS (
    SELECT dst_key
    FROM keelson('topology_edges')
    WHERE edge_kind = 'component-needs' AND src_key = {root:String}
    UNION ALL
    SELECT e.dst_key
    FROM keelson('topology_edges') AS e
    INNER JOIN closure AS c ON e.src_key = c.dst_key
    WHERE e.edge_kind = 'component-needs'
  ),
  edges AS (
    SELECT src_key AS source, dst_key AS target
    FROM keelson('topology_edges')
    WHERE edge_kind = 'component-needs'
  )
SELECT DISTINCT dst_key AS depends_on FROM closure
```
