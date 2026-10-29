---
type: reference
audience: contributor
status: draft
scene:
  launch: "subject_alias IN ('appcenter','play')"
  size: 1600x1000
  env:
    BOXER_PLAY_AUTORUN: "1"
  requires: [clickhouse]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# appcenter — the Running page

ADR-0348's Running page, what the app center shows while no app is
selected: every open window of the host, its frame time against the render
loop, and the tasks it owns. play opens beside it on a query that takes
about twenty seconds, so the page reads play's window with its query run as
its oldest task. The scene asserts that both windows are listed and that the
task is attributed to play's row.

```sql
SELECT number FROM numbers(40) WHERE sleepEachRow(0.5) = 0 SETTINGS max_block_size = 1
```

```jsonl trace
{"do":"wait","contains":"Refresh","role":"button","settleMs":500}
{"do":"read","valueContains":"window(s).","role":"label","pattern":"^(?P<windows>\\d+) window\\(s\\)\\.","settleMs":2000}
{"do":"expect","of":"windows","min":2}
{"do":"read","valueContains":"run the query, ","role":"label","pattern":"^(?P<oldest>run the query), ","comment":"play's query run, as its window's oldest task"}
{"do":"capture","text":"appcenter-running","settleMs":500}
```
