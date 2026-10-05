---
type: reference
audience: contributor
status: draft
scene:
  launch: app-sessions
  size: 1400x900
  requires: [clickhouse]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# app-sessions — every app window, one lane per app

The starter book's App sessions applet (ADR-0260 §SD5): `keelson('app_runs')`
drawn on the Timeline tab, one lane per app, across the processes the host's
store holds. The scene waits for the applet to run and captures the drawing.
What it shows depends on the trail.

```jsonl trace
{"do":"read","valueContains":"rows ·","role":"label","pattern":"(?P<rows>\\d+) rows","settleMs":2000}
{"do":"capture","text":"app-sessions","settleMs":1000}
```
