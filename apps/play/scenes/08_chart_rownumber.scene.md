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

# chart rownumber

Chart — the IMPLICIT abscissa (ADR-0172 §SD2): a result with no x column numbers its own rows 1, 2, 3 in the order the query returned them, so an ordinary top-N draws as the ranking curve it is

```sql
SELECT t       AS aircraft,
       count() AS positions
FROM default.planes_mercator_sample100
WHERE t != ''
GROUP BY aircraft
ORDER BY positions DESC
LIMIT 30
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2000}
{"do":"capture","text":"08_chart_rownumber","settleMs":600}
{"do":"click","name":"Bar"}
{"do":"capture","text":"08_chart_rownumber_bar","settleMs":600}
```
