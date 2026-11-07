---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — idle without a model, and the help

A chat window opened on a host with no model configured: the bar's turn
chip reads `idle`, the transcript says there is no model to talk to and
names the two variables that would configure one, and F1 opens the
window's own help. The scene asserts the chip's state and the help's
heading; it needs no model, no grant and no ClickHouse.

```jsonl trace
{"do":"note","text":"--- the turn chip without a model ---"}
{"do":"wait","contains":"New conversation","role":"button","settleMs":1000}
{"do":"wait","value":"No model to talk to","role":"label"}
{"do":"read","value":"idle","role":"label","pattern":"^(?P<turn>\\w+)$","comment":"the turn chip in the bar"}
{"do":"expect","of":"turn","is":"idle"}
{"do":"capture","text":"chat-turn-idle"}

{"do":"note","text":"--- F1 opens the window's help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"The chat window","role":"label","comment":"the reader shows the chat's overview"}
{"do":"capture","text":"chat-turn-idle-help"}
```
