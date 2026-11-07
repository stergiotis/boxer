---
type: reference
audience: contributor
status: draft
scene:
  launch: imzrt
  size: 1280x800
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# imzrt — freezing the sampler

The dashboard opens live, sampling the Go runtime of its own process every
second. The scene freezes the sampler, reads the interval the top bar
states and asserts it is the default, captures the frozen plots, lets the
sampler run again, and ends on F1 over the window, which opens imzrt's own
help. The values the plots show are this process's, so only the labels are
asserted.

```jsonl trace
{"do":"wait","name":"Freeze","role":"button","settleMs":1500}
{"do":"click","name":"Freeze","role":"button"}
{"do":"wait","name":"Go live","role":"button"}
{"do":"read","valueContains":"every ","role":"label","pattern":"^every (?P<interval>[0-9a-z.]+)$"}
{"do":"expect","of":"interval","is":"1s"}
{"do":"capture","text":"imzrt-frozen"}
{"do":"click","name":"Go live","role":"button"}
{"do":"wait","name":"Freeze","role":"button"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"Watching the Go runtime","role":"label","comment":"the reader shows imzrt's overview, not the index"}
{"do":"capture","text":"imzrt-help"}
```
