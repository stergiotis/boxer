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

# appcenter — the Running tab

ADR-0348's Running tab, which the app center opens on: every open window
of the host, its frame time against the render loop, and the tasks it owns.
play opens beside it on a query that takes about forty seconds, so the tab
lists play's window with its query run among its tasks. The scene filters by
the task's title, which leaves play's window alone, so the task is
attributed to play's row; the app column is read rather than the task's, so
column widths a person stored cannot move the anchor off screen. Before
that it measures CPU (ADR-0351): five seconds of sampling, split by the
window each goroutine works for, summed in a line under the window count.
Then it closes what the filter shows: Close all asks first, naming the
window, and the tab lists the app center alone. Explore, last, opens play
on the measurement's profile, which the closed window's samples are still
part of.

```sql
SELECT number FROM numbers(80) WHERE sleepEachRow(0.5) = 0 SETTINGS max_block_size = 1
```

```jsonl trace
{"do":"wait","contains":"Refresh","role":"button","settleMs":500}
{"do":"read","valueContains":"window(s).","role":"label","pattern":"^\\d+ of (?P<windows>\\d+) window\\(s\\)\\.","settleMs":2000}
{"do":"expect","of":"windows","min":2}
{"do":"capture","text":"appcenter-running","settleMs":500}
{"do":"click","name":"\ue610 Measure CPU","role":"button"}
{"do":"wait","valueContains":"CPU over","role":"label","comment":"five seconds of sampling"}
{"do":"read","valueContains":"CPU over","role":"label","pattern":"^CPU over (?P<secs>\\d+) s, ended [\\d:]+: all (?P<all>[\\d.]+)%"}
{"do":"expect","of":"secs","min":4,"max":6}
{"do":"capture","text":"appcenter-running-cpu","settleMs":500}
{"do":"type","id":415954472043999163,"text":"run the query","comment":"the filter, by a task's title; play's window has a text box too, so by id"}
{"do":"read","valueContains":"window(s).","role":"label","pattern":"^(?P<shown>1) of \\d+ window\\(s\\)\\.","comment":"one window owns the query run"}
{"do":"wait","name":"SQL playground","role":"button","comment":"and it is play's: the app column"}
{"do":"click","name":"\ue4f6 Close 1 shown","role":"button"}
{"do":"read","valueContains":"SQL Playground?","role":"label","pattern":"^Close \\S* ?(?P<named>SQL Playground)\\?$","comment":"the confirmation names the window, so a wrong one cannot pass"}
{"do":"click","name":"\ue4f6 Close 1 window","role":"button"}
{"do":"read","valueContains":"window(s).","role":"label","pattern":"^(?P<left>0) of 1 window\\(s\\)\\.","comment":"play's window went through its closing edge"}
{"do":"capture","text":"appcenter-running-closed","settleMs":500}
{"do":"click","name":"\ue624 Explore","role":"button"}
{"do":"read","valueContains":"window(s).","role":"label","pattern":"^\\d+ of (?P<total>2) window\\(s\\)\\.","comment":"play opened on the profile"}
{"do":"capture","text":"appcenter-running-explore","settleMs":4000}
```
