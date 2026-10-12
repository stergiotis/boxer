---
type: reference
audience: contributor
status: draft
scene:
  launch: watchbill
  size: 1400x900
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# watchbill — Auto-refresh turns off

The management window's Auto-refresh check box, on at start, turned off by
a click: the two captures show it checked, then not. The tree does not
carry a check box's state, so the captures are the evidence; the scene
fails only if the box is gone. The box once wrote its value into a
variable that did not outlive the frame, and stayed checked.

```jsonl trace
{"do":"wait","name":"Auto-refresh","role":"check_box"}
{"do":"capture","text":"watchbill-auto-refresh-on"}
{"do":"click","name":"Auto-refresh","role":"check_box"}
{"do":"capture","text":"watchbill-auto-refresh-off","settleMs":800}
```
