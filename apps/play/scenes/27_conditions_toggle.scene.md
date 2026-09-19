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

# conditions toggle

Conditions — the ADR-0121 selection-conditions chrome, off by default and reachable only by clicking it on

```sql
SELECT icao, r, t, altitude FROM default.planes_mercator_sample100 WHERE altitude > 30000 AND ground_speed > 400 ORDER BY icao LIMIT 100
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"click","name":"Conditions","role":"check_box"}
{"do":"capture","text":"27_conditions_toggle","settleMs":600}
```
