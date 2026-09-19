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

# chart heatmap sparse

Chart — a SPARSE grid: cells no row filled stay holes in the colormap's transparent BadColor rather than being drawn as a zero, which is a different claim

```sql
SELECT toHour(time)      AS x,
       toDayOfWeek(time) AS y,
       count()           AS z
FROM default.planes_mercator_sample100
WHERE ground_speed > 560
GROUP BY x, y
ORDER BY y, x
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2000}
{"do":"capture","text":"08_chart_heatmap_sparse","settleMs":600}
{"do":"hover","x":576,"y":790}
{"do":"capture","text":"08_chart_heatmap_hover","settleMs":600}
{"do":"hover","x":690,"y":820}
{"do":"capture","text":"08_chart_heatmap_hover_hole","settleMs":600}
```
