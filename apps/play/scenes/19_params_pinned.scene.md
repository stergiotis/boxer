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

# params pinned

Parameters — a SET param_ line PINS the value into the buffer: a constant that shadows any signal of the same name, plus a folded range pair

```sql
SET param_kind = 'DELIVERED';
SET param_id_min = 10000;
SET param_id_max = 10030;
SELECT `id:id`, `symbol:value`, `text:text`
FROM anchor.facts
WHERE has(`symbol:value`, {kind:String})
  AND `id:id` BETWEEN {id_min:UInt64} AND {id_max:UInt64}
ORDER BY `id:id`
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"19_params_pinned"}
```
