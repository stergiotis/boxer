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
    BOXER_PLAY_FOCUS_NETWORK: "1"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# network

Network — the result as a layered node-link graph (ADR-0129): an edges channel, plus an optional vertices node carrying labels and groups

```sql
WITH
  top_ops AS (
    SELECT ownOp
    FROM default.planes_mercator_sample100
    WHERE ownOp != '' AND t != ''
    GROUP BY ownOp
    ORDER BY uniq(icao) DESC
    LIMIT 6
  ),
  fleets AS (
    SELECT ownOp, t
    FROM default.planes_mercator_sample100
    WHERE t != '' AND ownOp IN (SELECT ownOp FROM top_ops)
    GROUP BY ownOp, t
  ),
  vertices AS (
    SELECT DISTINCT ownOp AS id, ownOp AS label, 'operator' AS `group` FROM fleets
    UNION ALL
    SELECT DISTINCT t, t, 'aircraft type' FROM fleets
  ),
  edges AS (
    SELECT ownOp AS source, t AS target FROM fleets
  )
SELECT * FROM edges
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":3000}
{"do":"capture","text":"08_network"}
```
