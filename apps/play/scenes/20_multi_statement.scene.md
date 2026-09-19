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

# multi statement

Editor — a multi-statement buffer: the caret's statement is tinted, the gutter marks it, and Run ships just that one with its SET prelude

```sql
SET param_kind = 'PORT_SCAN';

SELECT count() FROM anchor.facts;

SELECT `id:id`, `symbol:value`, `text:text`
FROM anchor.facts
WHERE has(`symbol:value`, {kind:String});

SELECT 'a third statement, not run' AS note
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"20_multi_statement"}
```
