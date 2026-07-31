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
boxes, their declared `needs` edges, each component's observed listening
sockets as ellipses — and, joined from `keelson('procs')`, the live
marked-process count per component. A component's label carries that
count (`imzero2-demo ·2`); its colour is the liveness verdict: `running`
(marked processes observed), `absent` (declared, none observed),
`undeclared` (a mark running that no registry entry declares).

Attribution is cooperative (ADR-0126): only processes that carry the
component mark — and are readable by the scraper — attach here. A daemon
running as another user or inside a container reads `·0` absent even
while its unit runs, and its listeners collect in the `unattributed`
ellipse instead, together with every listener of unmarked processes.
Drill into those with the Socket owners applet; `cgroup_unit` in
`keelson('procs')` corroborates systemd-managed processes the mark
cannot reach. Unit-state truth (`failed` vs absent) stays out of reach
until the deferred keelson.services collector lands (ADR-0126 §SD6).

The observed half exists only while a scraper publishes on the metric
plane — with a silent plane every component reads `absent`; check the
Plane staleness applet first. "Declared" means "in the compiled-in
registry", not "must run on this box". The canonical queries behind this
view live in doc/howto/topology-queries.md.

```sql
WITH
  comp AS (
    SELECT key,
           arraySort(groupArray(DISTINCT origin)) AS origins
    FROM keelson('topology_nodes')
    WHERE kind = 'component'
    GROUP BY key
  ),
  live AS (
    SELECT concat('component:', component) AS key, count() AS procs
    FROM keelson('procs')
    WHERE component != ''
    GROUP BY component
  ),
  lst AS (
    SELECT c.dst_key AS source, l.dst_key AS target
    FROM keelson('topology_edges') AS l
    INNER JOIN keelson('topology_edges') AS c
      ON l.src_key = c.src_key AND l.host = c.host
    WHERE l.edge_kind = 'proc-listens'
      AND c.edge_kind = 'proc-in-component'
  ),
  unattributed AS (
    SELECT count() AS n
    FROM keelson('sockets') AS s
    LEFT JOIN keelson('procs') AS p ON s.pid = p.pid AND s.host = p.host
    WHERE s.pid = 0 OR p.component = ''
  ),
  vertices AS (
    SELECT comp.key AS id,
           concat(substring(comp.key, 11), ' ·', toString(live.procs)) AS label,
           multiIf(live.procs = 0, 'absent',
                   has(comp.origins, 'declared'), 'running',
                   'undeclared') AS `group`,
           'box' AS shape
    FROM comp
    LEFT JOIN live ON comp.key = live.key
    UNION ALL
    SELECT DISTINCT target AS id, substring(target, 6) AS label,
           'listener' AS `group`, 'ellipse' AS shape
    FROM lst
    UNION ALL
    SELECT 'unattributed' AS id,
           concat('unattributed ·', toString(n)) AS label,
           'unattributed' AS `group`, 'ellipse' AS shape
    FROM unattributed
    WHERE n > 0
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
