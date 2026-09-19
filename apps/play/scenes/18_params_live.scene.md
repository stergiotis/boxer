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
  requires: ["clickhouse", "table:anchor.facts"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# params live

Parameters — an unbound {name:Type} placeholder is a LIVE signal: the widget above the editor writes the shared value

```sql
SELECT `id:id`, `symbol:value`[1] AS kind
FROM anchor.facts
ORDER BY `id:id`
LIMIT {lim:UInt64}
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"18_params_live"}
```
