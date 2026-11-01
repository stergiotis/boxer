---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1400x900
  requires: [clickhouse]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# windowhost — pausing a window

ADR-0354 on the headless host. Pause, in the row the host draws above
play's body, replaces the body with the paused state: since when, that
play's background work continues because play does not stop it, and Resume.
Resume from the paused state brings play's body back.

```jsonl trace
{"do":"wait","name":"Run","role":"button"}
{"do":"click","name":" Pause","role":"button"}
{"do":"read","valueContains":"Paused for","role":"label","pattern":"^\\S+ Paused for (?P<secs>\\d+) s$"}
{"do":"wait","valueContains":"Its background work continues","role":"label","comment":"play does not implement app.PausableAppI"}
{"do":"capture","text":"windowhost-paused","settleMs":500}
{"do":"click","name":" Resume","role":"button","nth":1,"comment":"the paused state's, below the row's"}
{"do":"wait","name":"Run","role":"button","comment":"play's body is back"}
{"do":"wait","name":" Pause","role":"button"}
{"do":"capture","text":"windowhost-resumed","settleMs":500}
```
