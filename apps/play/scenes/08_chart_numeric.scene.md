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

# chart numeric

Chart — a CONTINUOUS x (ADR-0172 §SD2): bar slots sized from the smallest gap between distinct x values, a NULL ending a lane rather than being drawn as a zero, and the Scatter and log-y chips

```sql
SELECT intDiv(altitude, 5000) * 5                            AS x,
       nullIf(uniqExactIf(icao, ground_speed >= 400), 0)      AS fast,
       nullIf(uniqExactIf(icao, ground_speed <  400), 0)      AS slow
FROM default.planes_mercator_sample100
WHERE altitude > 0
GROUP BY x
ORDER BY x
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2000}
{"do":"capture","text":"08_chart_numeric","settleMs":600}
{"do":"click","name":"Scatter"}
{"do":"capture","text":"08_chart_scatter","settleMs":600}
{"do":"click","name":"log y"}
{"do":"capture","text":"08_chart_logy","settleMs":600}
{"do":"click","name":"log y"}
{"do":"capture","text":"08_chart_logy_off","settleMs":600}
```
