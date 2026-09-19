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
    BOXER_PLAY_FOCUS_PREVIEW: "1"
    BOXER_PLAY_PREVIEW_AS_SENT: "1"
  requires: ["clickhouse", "table:anchor.facts"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# preview as sent

Preview — 'as sent to server': the post-pass wire SQL, with the appended FORMAT ArrowStream and the statement selection

```sql
SELECT `id:id`, `symbol:value`
FROM anchor.facts
WHERE hasAny(`symbol:value`, ['DELIVERED'])
LIMIT 25
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"17_preview_as_sent"}
```
