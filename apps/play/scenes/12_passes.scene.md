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
    BOXER_PLAY_FOCUS_PASSES: "1"
  requires: ["clickhouse", "table:anchor.facts"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# passes

Passes — the nanopass pre-execute sequence: each rewrite that ran on the buffer, in order, with the ones that were skipped marked

```sql
SELECT `id:id`, `symbol:*`
FROM anchor.facts
WHERE hasAny(`symbol:value`, ['DDOS', 'PORT_SCAN', 'SQL_INJECTION'])
ORDER BY `id:id`
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"12_passes"}
```
