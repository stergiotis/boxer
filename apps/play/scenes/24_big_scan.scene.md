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
  requires: ["clickhouse", "table:default.planes_mercator"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# big scan

Progress — a long scan: the live progress/ETA readout in the status bar, driven by the server's own elapsed and row counters

```sql
SELECT t AS aircraft_type, count() AS pings, round(avg(altitude)) AS avg_alt, round(max(ground_speed)) AS max_speed
FROM default.planes_mercator
GROUP BY t
ORDER BY pings DESC
LIMIT 50
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":3000}
{"do":"capture","text":"24_big_scan"}
```
