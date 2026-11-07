---
type: reference
audience: contributor
status: draft
scene:
  launch: watchbill
  size: 1400x900
  requires: [clickhouse]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# watchbill — Auto-refresh turns off

The management window's Auto-refresh check box, on at start, turned off by
a click: the two captures show it checked, then not. The tree does not
carry a check box's state, so the captures are the evidence; the scene
fails only if the box is gone. The box once wrote its value into a
variable that did not outlive the frame, and stayed checked.

Before the click the scene waits for the first list to land and reads the
status line under the filters, asserting the counts it carries are
numbers and that the shown rows do not exceed the held ones; after it, F1
opens the window's own help.

```jsonl trace
{"do":"wait","name":"Auto-refresh","role":"check_box"}
{"do":"read","valueContains":"job(s) shown, listed","role":"label","pattern":"(?P<shown>\\d+) of (?P<held>\\d+) job\\(s\\) shown","settleMs":6000}
{"do":"expect","of":"shown","min":0}
{"do":"expect","of":"held","minus":"shown","min":0,"comment":"the filter shows a subset of the held rows"}
{"do":"capture","text":"watchbill-auto-refresh-on"}
{"do":"click","name":"Auto-refresh","role":"check_box"}
{"do":"capture","text":"watchbill-auto-refresh-off","settleMs":800}

{"do":"note","text":"--- F1 over the window opens the window's help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"The Watchbill window","role":"label","comment":"the reader shows the overview, not the index"}
{"do":"capture","text":"watchbill-help"}
```
