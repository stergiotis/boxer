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

# chart heatmap

Chart — the GRID reading (ADR-0172 §SD1): the x, y and z columns pivoted into a heatmap over the distinct keys, with the colorscale legend bound to the same colormap

```sql
SELECT toHour(time)      AS x,
       toDayOfWeek(time) AS y,
       count()           AS z
FROM default.planes_mercator_sample100
GROUP BY x, y
ORDER BY y, x
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2000}
{"do":"capture","text":"08_chart_heatmap"}
```
