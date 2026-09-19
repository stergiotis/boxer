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

# empty result

Empty result — the zero-row rendering across the panels, distinct from 'nothing has run yet'

```sql
SELECT * FROM anchor.facts WHERE 1 = 0
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"22_empty_result"}
```
