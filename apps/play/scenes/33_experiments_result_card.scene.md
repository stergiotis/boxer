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
    BOXER_PLAY_FOCUS_EXPERIMENTS: "1"
  requires: ["clickhouse", "table:anchor.facts"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# experiments result card

Experiments — the card sink over the CURRENT result rather than the fixture, drawn beside the Detail tab that renders the same emitter: the pane owns its own CardDriver so the two do not share widget ids

```sql
SET param_selection = 0;
SELECT * FROM anchor.facts ORDER BY `id:id` LIMIT 1
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"click","name":"result","comment":"switch the source off the fixture","settleMs":800}
{"do":"capture","text":"33_experiments_result_card","settleMs":800}
```
