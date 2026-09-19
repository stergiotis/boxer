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

# chart bars

Chart — the lanes reading (ADR-0172): a categorical x with two numeric columns, each labelled by its own column name, drawn as grouped bars and then as a line from the same claim

```sql
SELECT t                                      AS x,
       uniqExactIf(icao, altitude >= 10000)   AS above_10k,
       uniqExactIf(icao, altitude <  10000)   AS below_10k
FROM default.planes_mercator_sample100
WHERE t != ''
GROUP BY x
ORDER BY above_10k + below_10k DESC
LIMIT 12
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2000}
{"do":"capture","text":"08_chart_bars","settleMs":600}
{"do":"click","name":"Line"}
{"do":"capture","text":"08_chart_line","settleMs":600}
```
