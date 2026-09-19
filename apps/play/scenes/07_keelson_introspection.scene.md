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
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# keelson introspection

A keelson() read routed to the in-process introspection plane (ADR-0094 / ADR-0141): the dispatch resolver moves a keelson-only statement off the pinned ClickHouse and answers it from this process, which the toolbar names

```sql
SELECT name, category FROM keelson('env') ORDER BY name LIMIT 10
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":4000}
{"do":"capture","text":"07_keelson_introspection"}
```
