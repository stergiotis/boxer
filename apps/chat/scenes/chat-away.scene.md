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

# chat — Away, and what happened while the person was away

ADR-0328 on the headless host, against the scripted model in
[chat-away.script.jsonl](./chat-away.script.jsonl), which takes five
seconds to answer. The person sends, turns on Watch, moves to another
window and sets Away in the host's control, which then names the status.
The watched turn ends while they are away; when they set Here again, the
host opens "While you were away" with the highlight, and Dismiss closes it.

```jsonl trace
{"do":"note","text":"ADR-0328: Away and the away log"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button","comment":"the seeded draft"}
{"do":"click","contains":"Watch","role":"button","comment":"tell me when this turn ends"}
{"do":"click","contains":"Widget gallery","role":"window","pointer":true,"comment":"look elsewhere while the model answers"}
{"do":"click","contains":"nothing waits on you","role":"button"}
{"do":"click","contains":"Away","comment":"the status selector in the list"}
{"do":"wait","contains":"Away · ","role":"button","comment":"the control names the status"}
{"do":"capture","text":"chat-away-set"}
{"do":"read","contains":"Away · ","role":"button","on":"name","pattern":"(?P<highlights>\\d+)$","comment":"the watched turn ended while away"}
{"do":"expect","of":"highlights","eq":1}
{"do":"click","contains":"Here","comment":"back"}
{"do":"click","name":"Close","role":"button","comment":"close the list, which lists the highlight too"}
{"do":"wait","contains":"While you were away","role":"window"}
{"do":"wait","value":"\ue46a highlight","role":"label","comment":"the turn's end is in the log"}
{"do":"capture","text":"chat-away-log"}
{"do":"click","name":"Dismiss","role":"button"}
{"do":"wait","contains":"nothing waits on you","role":"button","comment":"Here again: the control names no status"}
```
