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

# history two runs

History — more than one run in the list, which needs a second Run and so cannot be seeded at launch

```sql
SELECT t, count() AS n FROM default.planes_mercator_sample100 WHERE t != '' GROUP BY t ORDER BY n DESC LIMIT 20
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"click","name":"Run","comment":"a second run, so History has two entries"}
{"do":"click","name":"History","role":"button","comment":"the History tab of the editor leaf"}
{"do":"capture","text":"29_history_two_runs","settleMs":800}
```
