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
    BOXER_PLAY_FOCUS_SCHEMA: "1"
  requires: ["clickhouse", "table:anchor.facts"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# schema

Schema — the leeway schema inspector: sections, their value columns and the physical names a handle resolves to

```sql
SELECT * FROM anchor.facts LIMIT 50
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"10_schema"}
```
