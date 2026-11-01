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

# appcenter — pausing a slow window from its notice

ADR-0354 §SD4 on the headless host. The task demo's Slow Frame makes its
Frame take 12 ms; after ten seconds the host's attention control counts one
highlight (ADR-0351 §SD6). Its Pause pauses the window there and then: the
highlight goes, and the task demo's window shows the paused state.

```jsonl trace
{"do":"click","contains":"Slow Frame","role":"check_box"}
{"do":"sleep","settleMs":9000,"comment":"ten seconds above the line, frames drawn"}
{"do":"read","contains":"nothing waits on you","role":"button","on":"name","pattern":"(?P<highlights>\\d+)$"}
{"do":"expect","of":"highlights","eq":1}
{"do":"click","contains":"nothing waits on you","role":"button"}
{"do":"wait","valueContains":"Frame slow: p95","role":"label"}
{"do":"click","name":" Pause","role":"button","nth":1,"comment":"the notice's, after the window's own row"}
{"do":"wait","valueContains":"Paused for","role":"label","comment":"the task demo's window is paused"}
{"do":"read","contains":"nothing waits on you","role":"button","on":"name","pattern":"^(?P<control>\\S+ nothing waits on you)$","comment":"the highlight went with the pause"}
{"do":"capture","text":"appcenter-slow-window-pause","settleMs":500}
```
