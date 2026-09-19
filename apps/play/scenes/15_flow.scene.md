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
    BOXER_PLAY_FOCUS_FLOW: "1"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# flow

Flow — clause-level dataflow inside one statement (ADR-0153), where the Graph tab's per-CTE boxes end

```sql
WITH busy AS (
  SELECT icao, count() AS pings, avg(altitude) AS avg_alt
  FROM default.planes_mercator_sample100
  WHERE altitude > 0
  GROUP BY icao
  HAVING pings > 20
)
SELECT b.icao, b.pings, round(b.avg_alt) AS avg_alt, p.t AS aircraft_type
FROM busy AS b
LEFT JOIN default.planes_mercator_sample100 AS p ON p.icao = b.icao
ORDER BY b.pings DESC
LIMIT 100
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"15_flow"}
```
