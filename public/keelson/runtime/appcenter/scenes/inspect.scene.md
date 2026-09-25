---
type: reference
audience: contributor
status: draft
scene:
  launch: launcher
  size: 1400x900
  requires: [clickhouse]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# inspect — from the launcher to the app center

ADR-0260 §SD1's route in: the launcher's detail pane carries an Inspect
action that opens the app center on the app the pane shows. The scene
selects the app-state manager in the launcher, presses Inspect, and waits
for the app center's page on that app.

```jsonl trace
{"do":"wait","value":"App state","role":"label","settleMs":2000}
{"do":"click","value":"App state","role":"label","pointer":true}
{"do":"wait","contains":"Inspect","role":"button","settleMs":500}
{"do":"click","contains":"Inspect","role":"button"}
{"do":"wait","name":"Capabilities","settleMs":3000}
{"do":"read","valueContains":"Cited under","role":"label","pattern":"Cited under (?P<dir>apps/appstate);","comment":"only the app center's page on App state says this"}
{"do":"capture","text":"inspect-appstate"}
```
