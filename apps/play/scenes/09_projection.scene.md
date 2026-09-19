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
    BOXER_PLAY_FOCUS_PROJECTION: "1"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# projection

Projection — the neighbour graph of a result's numeric columns (ADR-0230), laid out by the neighbour-embedding force model with clusters as auras, its nodes tied to the selection signal

```sql
SELECT icao, altitude, ground_speed, track_degrees, lat, lon, vertical_rate
FROM default.planes_mercator_sample100
WHERE altitude > 0 AND ground_speed > 0
LIMIT 1500
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":4000}
{"do":"click","name":"Compute projection","role":"button","settleMs":600}
{"do":"capture","text":"09_projection","settleMs":5000}
```
