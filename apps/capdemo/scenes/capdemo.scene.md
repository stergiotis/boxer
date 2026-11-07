---
type: reference
audience: contributor
status: draft
scene:
  launch: capdemo
  size: 900x820
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# capdemo — the four sections at rest

The capability broker demo as it opens: the four sections, each waiting
on a verb, nothing held. The scene reaches the file section's button,
reads the watch section's event counter and asserts it is zero, captures,
and ends with F1 on the demo's own help. No picker is opened: a dialog
waits on the person, which the driver is not.

```jsonl trace
{"do":"wait","name":"Pick a file…","role":"button"}
{"do":"wait","name":"Save","role":"button"}
{"do":"read","valueContains":"Events received:","role":"label","pattern":"Events received: (?P<events>\\d+)"}
{"do":"expect","of":"events","eq":0}
{"do":"capture","text":"capdemo"}

{"do":"note","text":"--- F1 over the window opens the demo's help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"The capability broker demo","role":"label","comment":"the reader shows the demo's overview, not the index"}
{"do":"capture","text":"capdemo-help"}
```
