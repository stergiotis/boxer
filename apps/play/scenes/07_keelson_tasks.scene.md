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

# keelson tasks

play's query run is a keelson task owned by its window (task.Run, ADR-0322): a statement that reads `keelson('tasks')` sees its own run in flight, joined to the window that started it on the instance key. A second run sees one row again, so the first run's task left the in-flight table when it ended.

```sql
SELECT t.kind AS kind, t.title AS title, t.state AS state, w.app_id AS app_id
FROM keelson('tasks') AS t INNER JOIN keelson('windows') AS w ON w.key = t.owner_instance_key
```

```jsonl trace
{"do":"wait","name":"Run","role":"button","comment":"the app has mounted"}
{"do":"read","valueContains":"play.query","role":"label","pattern":"^(?P<kind>play\\.query)$","comment":"the run's own task, joined to its window"}
{"do":"read","valueContains":"rows ·","role":"label","pattern":"^(?P<first>\\d+) rows"}
{"do":"expect","of":"first","eq":1}
{"do":"click","name":"Run","role":"button"}
{"do":"wait","name":"Run","role":"button","settleMs":1500}
{"do":"read","valueContains":"rows ·","role":"label","pattern":"^(?P<second>\\d+) rows"}
{"do":"expect","of":"second","eq":1,"comment":"the first run's task ended"}
{"do":"capture","text":"07_keelson_tasks"}
```
