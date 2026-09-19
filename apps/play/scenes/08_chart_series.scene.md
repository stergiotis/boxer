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

# chart series

Chart — the LONG idiom (ADR-0172 §SD1): a series column splits the rows into one drawn series each, over a temporal x that takes implot's time axis

```sql
SELECT toDateTime64(toStartOfInterval(time, INTERVAL 30 MINUTE), 3) AS x,
       t                                                             AS series,
       count()                                                       AS y
FROM default.planes_mercator_sample100
WHERE toDate(time, 'UTC') = (SELECT min(toDate(time, 'UTC')) FROM default.planes_mercator_sample100)
  AND t IN ('A320', 'B738', 'A21N')
GROUP BY x, series
ORDER BY x
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2000}
{"do":"capture","text":"08_chart_series"}
```
