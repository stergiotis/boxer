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

ADR-0348's Running tab, which the app center opens on: every open window
of the host, its frame time against the render loop, and the tasks it owns. play opens beside it on a query that takes
about twenty seconds, so the page reads play's window with its query run as
its oldest task. The scene asserts that both windows are listed and that the
task is attributed to play's row. Then it closes play from its row: Close
asks first, naming the window, and the page lists the app center alone.

```sql
SELECT number FROM numbers(40) WHERE sleepEachRow(0.5) = 0 SETTINGS max_block_size = 1
```

```jsonl trace
{"do":"wait","contains":"Refresh","role":"button","settleMs":500}
{"do":"read","valueContains":"window(s).","role":"label","pattern":"^(?P<windows>\\d+) window\\(s\\)\\.","settleMs":2000}
{"do":"expect","of":"windows","min":2}
{"do":"read","valueContains":"run the query, ","role":"label","pattern":"^(?P<oldest>run the query), ","comment":"play's query run, as its window's oldest task"}
{"do":"capture","text":"appcenter-running","settleMs":500}
{"do":"click","name":"\ue4f6 Close","role":"button","nth":0,"comment":"play's row, the slowest; Close asks first"}
{"do":"click","contains":"SQL Playground","role":"button","comment":"the confirmation names the window, so a wrong row cannot pass"}
{"do":"read","valueContains":"window(s).","role":"label","pattern":"^(?P<left>1) window\\(s\\)\\.","comment":"play's window went through its closing edge"}
{"do":"capture","text":"appcenter-running-closed","settleMs":500}
```
