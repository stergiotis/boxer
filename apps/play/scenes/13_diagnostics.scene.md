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
    BOXER_PLAY_FOCUS_DIAGNOSTICS: "1"
  requires: ["clickhouse", "table:anchor.facts"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# diagnostics

Diagnostics — a handle that names no known section: flagged before Run, with suggestions, and the pre-execute rewrites that were skipped

```sql
SELECT `id:id`, `symbal:value`, `geoPoint:lattitude`
FROM anchor.facts
LIMIT 20
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"13_diagnostics"}
```
