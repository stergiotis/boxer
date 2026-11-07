---
type: reference
audience: contributor
status: draft
scene:
  launch: adhocdemo
  size: 1000x720
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# adhocdemo — the first publish lands

The ad-hoc dataset demo after its first publish: the bar names the bundle
and its revision, the Regenerate button is up, and the pane is the bundle
view over the series. The scene waits for Regenerate, which appears only
once the publish landed, reads the revision from the bar and asserts it is
at least 1, captures, and ends with F1 on the demo's own help. The tree
half is not pressed: it needs a ClickHouse server and opens a second
window.

```jsonl trace
{"do":"wait","name":"Regenerate","role":"button","settleMs":1500}
{"do":"read","valueContains":"bundle adhocdemo_items","role":"label","pattern":"bundle (?P<alias>\\S+) · rev (?P<rev>\\d+)"}
{"do":"expect","of":"rev","min":1}
{"do":"capture","text":"adhocdemo"}

{"do":"note","text":"--- F1 over the window opens the demo's help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"The ad-hoc dataset demo","role":"label","comment":"the reader shows the demo's overview, not the index"}
{"do":"capture","text":"adhocdemo-help"}
```
