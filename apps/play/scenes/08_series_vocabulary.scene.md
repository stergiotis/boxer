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
    BOXER_PLAY_FOCUS_TABLE: "1"
    BOXER_PLAY_OBSERVE: "scored"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# series vocabulary

The ts* client vocabulary (ADR-0163 M1): a CTE whose body is tsAnomalyScores runs IN PLAY, not on ClickHouse — observed in the result panels, with the Graph tab's engine badge and the honesty caption naming what was actually sent

```sql
WITH
  base AS (
    SELECT toDateTime64(toStartOfInterval(time, INTERVAL 5 MINUTE), 3) AS t,
           count()                                                     AS v
    FROM default.planes_mercator_sample100
    GROUP BY t
    ORDER BY t
  ),
  scored AS (SELECT tsAnomalyScores(t, v, 24) FROM base)
SELECT 1
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":3000}
{"do":"capture","text":"08_series_vocabulary"}
```
