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
    BOXER_PLAY_FOCUS_CHART: "1"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chart duplicate

Chart — the DATA-level reject (ADR-0172 §SD1): a repeated (x, y) cell rejects the WHOLE heatmap, naming the row and the cell, rather than letting the last row win

```sql
SELECT toHour(time)      AS x,
       toDayOfWeek(time) AS y,
       count()           AS z
FROM default.planes_mercator_sample100
GROUP BY x, y, t
ORDER BY y, x
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1500}
{"do":"capture","text":"08_chart_duplicate"}
```
