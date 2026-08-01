---
type: reference
audience: end-user
status: draft
title: CPU call graph
icon: "🕸"
endpoint: introspection
tabs: [network, table, detail]
datasets: [pprof_cpu]
datasets_hint: "Capture one from imzrt → Profiles → Capture CPU."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# CPU call graph

The most recent CPU capture as a directed graph: an edge `caller → callee`
weighted by the CPU time that flowed through that call, a vertex per
function labelled with its cumulative cost. The Network tab draws it; the
Table tab lists the same edges as caller/callee/ms rows, and a click's
Detail shows one edge.

**Needs a capture.** Open imzrt → Profiles → *Capture CPU*, before or after
opening this applet: with no `pprof_cpu` dataset yet it says so and keeps
looking, then binds and re-runs on its own once one appears. Re-captures
republish onto the same dataset — Run refreshes an open window.

**The knob.** `edge_cap` keeps the heaviest N edges. The vertex set follows
the kept edges, so lowering it prunes the picture rather than orphaning
nodes. Consecutive-frame pairs are deduplicated per stack
(`arrayDistinct`), so a recursive call contributes one edge per stack, not
one per frame.

```sql
SET param_edge_cap = 150;

WITH
  pairs AS (
    SELECT arrayJoin(arrayDistinct(arrayZip(arrayPopBack(stack), arrayPopFront(stack)))) AS p,
           value
    FROM keelson('pprof_cpu')),
  weights AS (
    SELECT p.1 AS caller, p.2 AS callee, sum(value) AS w
    FROM pairs
    GROUP BY caller, callee
    ORDER BY w DESC
    LIMIT {edge_cap:UInt64}),
  edges AS (
    SELECT caller AS source, callee AS target,
           concat(toString(round(w / 1e6, 1)), ' ms') AS label
    FROM weights),
  vertices AS (
    SELECT fn AS id,
           concat(fn, ' · ', toString(round(cum_ns / 1e6, 1)), ' ms') AS label
    FROM (
      SELECT arrayJoin(arrayDistinct(stack)) AS fn, sum(value) AS cum_ns
      FROM keelson('pprof_cpu')
      GROUP BY fn)
    WHERE fn IN (SELECT caller FROM weights)
       OR fn IN (SELECT callee FROM weights))
SELECT caller, callee, round(w / 1e6, 1) AS ms
FROM weights
ORDER BY ms DESC
```
