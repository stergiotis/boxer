---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1920x1200
  stepSettleMs: 350
  env:
    BOXER_PLAY_WINDOW_SIZE: "1888x1100"
    BOXER_PLAY_AUTORUN: "1"
    BOXER_PLAY_FOCUS_GRAPH: "1"
  requires: ["clickhouse", "table:anchor.facts"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# graph

Graph — the reactive query graph (ADR-0097): every top-level CTE is a node, CTE references are data edges, unbound placeholders are signal edges

```sql
WITH
  recent AS (
    SELECT `id:id` AS id, `symbol:value`[1] AS kind, `timeRange:beginIncl`[1] AS t
    FROM anchor.facts WHERE length(`timeRange:beginIncl`) > 0
  ),
  by_kind AS (
    SELECT kind, count() AS n, min(t) AS first_seen FROM recent GROUP BY kind
  ),
  busiest AS (
    SELECT kind FROM by_kind ORDER BY n DESC LIMIT 3
  )
SELECT r.id, r.kind, r.t
FROM recent AS r
WHERE r.kind IN (SELECT kind FROM busiest)
ORDER BY r.t
LIMIT 100
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"11_graph"}
```
