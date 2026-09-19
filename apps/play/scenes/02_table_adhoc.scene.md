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
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# table adhoc

Table — an ordinary aggregate result (the non-leeway path): plain grid, column headers, row count in the status bar

```sql
SELECT t AS aircraft, count() AS positions, round(avg(altitude)) AS avg_altitude, max(ground_speed) AS top_speed
FROM default.planes_mercator_sample100
WHERE t != ''
GROUP BY t
ORDER BY positions DESC
LIMIT 40
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"02_table_adhoc"}
```
