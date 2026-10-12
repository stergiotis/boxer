---
type: reference
audience: contributor
status: draft
scene:
  launch: "subject_alias IN ('chat','widgets')"
  size: 1400x900
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-away.script.jsonl"
    BOXER_CHAT_DRAFT: "Draw the wind above 15 m/s."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — F8, next by weight

ADR-0324 on the headless host, against the scripted model of
[chat-away](./chat-away.scene.md), which takes five seconds to answer. The
person sends, turns on Watch and moves to another window; the watched turn
ends there unseen, and the host's control counts one highlight. F8, pressed
in the other window, raises the chat window that reported it and drops the
highlight: with nothing waiting, the newest highlight is the next thing by
weight.

```jsonl trace
{"do":"note","text":"ADR-0324: F8, next by weight"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button","comment":"the seeded draft"}
{"do":"click","contains":"Watch","role":"button","comment":"tell me when this turn ends"}
{"do":"click","contains":"Widget gallery","role":"window","pointer":true,"comment":"look elsewhere while the model answers"}
{"do":"read","contains":"nothing waits on you","role":"button","on":"name","pattern":"(?P<highlights>\\d+)$","comment":"the turn ended unseen"}
{"do":"expect","of":"highlights","eq":1}
{"do":"key","text":"F8","comment":"next by weight"}
{"do":"read","contains":"nothing waits on you","role":"button","on":"name","pattern":"(?P<cleared>on you)$","comment":"the highlight went"}
{"do":"wait","valueContains":"new since you looked","role":"label","comment":"the chat window was raised, and marks what landed"}
{"do":"capture","text":"chat-next-key"}
```
