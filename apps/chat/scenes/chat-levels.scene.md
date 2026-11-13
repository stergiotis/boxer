---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the person's table of levels, in host chrome

ADR-0323 §SD5 on the headless host: the queue's control opens a
list with Levels, a grid of the person's table — a row per kind, the cells
of its scale only, the current level filled. The person raises a finished
turn to highlight for every conversation, then resets it to the default.

The host keeps the table across its restarts, in the same store a person's
host on that machine reads. The trace therefore anchors on the turn row's own
cells by id rather than counting the grid's matches, so another row's level,
a default or one left behind by an interrupted run, cannot move it.

```jsonl trace
{"do":"note","text":"ADR-0323 SD5: the person's grid"}
{"do":"wait","contains":"New conversation","role":"button"}
{"do":"click","contains":"nothing waits on you","role":"button"}
{"do":"click","name":"Levels","role":"button"}
{"do":"wait","contains":"Levels","role":"window"}
{"do":"wait","name":"● critical","nth":2,"comment":"three rows default to critical: a consequential ask, a confirmation, a moderator question"}
{"do":"click","id":6060254586541539230,"comment":"the turn row's highlight cell"}
{"do":"read","id":6060254586541539230,"on":"name","pattern":"^(?P<turn>●) highlight$","comment":"the turn row is at highlight; a slow window's row is too, by default"}
{"do":"wait","id":6643140940500153969,"comment":"the turn row's Reset: an override, not the default"}
{"do":"capture","text":"chat-levels"}
{"do":"click","id":6643140940500153969}
{"do":"read","id":6060254586541539230,"on":"name","pattern":"^(?P<turn>○) highlight$","comment":"back at the default"}
```
