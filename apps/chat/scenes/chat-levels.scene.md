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
The host keeps the table across its restarts.

```jsonl trace
{"do":"note","text":"ADR-0323 SD5: the person's grid"}
{"do":"wait","contains":"New conversation","role":"button"}
{"do":"click","contains":"nothing waits on you","role":"button"}
{"do":"click","name":"Levels","role":"button"}
{"do":"wait","contains":"Levels","role":"window"}
{"do":"wait","name":"● critical","nth":2,"comment":"three rows default to critical: a consequential ask, a confirmation, a moderator question"}
{"do":"click","name":"○ highlight","nth":10,"comment":"the turn row's highlight: user, request, reply, intent by five effects, outcome and note come first"}
{"do":"wait","name":"● highlight","comment":"the only row at highlight"}
{"do":"wait","name":"Reset","role":"button","comment":"an override, not the default"}
{"do":"capture","text":"chat-levels"}
{"do":"click","name":"Reset","role":"button"}
{"do":"wait","name":"○ highlight","nth":10}
```
