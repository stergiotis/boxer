---
type: reference
audience: contributor
status: draft
scene:
  launch: taskdemo
  size: 1600x1000
  requires: [clickhouse]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# appcenter — a slow window's notice

ADR-0351 §SD6 on the headless host. The task demo's Slow Frame makes its
Frame take 12 ms, above the window host's line of 8 ms. After ten seconds
of it the host's attention control counts one highlight; its Open opens the
app center's Running tab filtered to the window's title, which leaves the
task demo's window alone, its p95 toned as slow. Pause 1 shown pauses it
(ADR-0354): its row says paused, and its window shows the paused state;
Resume 1 shown brings its body back.

```jsonl trace
{"do":"click","contains":"Slow Frame","role":"check_box"}
{"do":"sleep","settleMs":9000,"comment":"ten seconds above the line, frames drawn"}
{"do":"read","contains":"nothing waits on you","role":"button","on":"name","pattern":"(?P<highlights>\\d+)$","comment":"the star's count"}
{"do":"expect","of":"highlights","eq":1}
{"do":"click","contains":"nothing waits on you","role":"button"}
{"do":"wait","valueContains":"Frame slow: p95","role":"label","comment":"the notice's line"}
{"do":"capture","text":"appcenter-slow-window-notice","settleMs":500}
{"do":"click","name":"Open","role":"button"}
{"do":"read","valueContains":"window(s).","role":"label","pattern":"^(?P<shown>1) of 2 window\\(s\\)\\.","settleMs":2000,"comment":"the filter leaves the slow window alone"}
{"do":"wait","name":"Task primitive","role":"button","comment":"and it is the task demo's"}
{"do":"capture","text":"appcenter-slow-window-running","settleMs":500}
{"do":"click","name":"\ue39e Pause 1 shown","role":"button"}
{"do":"wait","contains":"Background task  (paused)","role":"button","comment":"the row says so; a grid cell is a button"}
{"do":"read","contains":"paused ","role":"button","on":"name","pattern":"^paused (?P<since>\\d+)s$","comment":"once a read agrees, the p50 cell says since when"}
{"do":"wait","valueContains":"Paused for","role":"label","comment":"the task demo's window shows the paused state"}
{"do":"capture","text":"appcenter-slow-window-paused","settleMs":500}
{"do":"click","name":"\ue3d0 Resume 1 shown","role":"button"}
{"do":"wait","contains":"Slow Frame","role":"check_box","comment":"the task demo's body is back"}
```
