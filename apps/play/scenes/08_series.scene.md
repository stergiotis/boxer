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
    BOXER_PLAY_FOCUS_SERIES: "1"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# series

Series — numbers against a time axis (ADR-0163 M0): the typed claim (first temporal column, every numeric column a lane), the Δt classification with the scaffold its finding offers, and modified-sinc smoothing with its extrapolated tail drawn faded

```sql
SELECT toDateTime64(toStartOfInterval(time, INTERVAL 5 MINUTE), 3) AS t,
       count()            AS positions,
       uniqExact(icao)    AS aircraft
FROM default.planes_mercator_sample100
WHERE toDate(time, 'UTC') = (SELECT min(toDate(time, 'UTC')) FROM default.planes_mercator_sample100)
  AND NOT (toHour(time, 'UTC') = 12 AND toMinute(time, 'UTC') < 5)
GROUP BY t
ORDER BY t
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2500}
{"do":"capture","text":"08_series","settleMs":600}
{"do":"click","name":"smooth"}
{"do":"capture","text":"08_series_smoothed","settleMs":600}
```
