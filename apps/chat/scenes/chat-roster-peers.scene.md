---
type: reference
audience: contributor
status: draft
scene:
  launch: "subject_alias IN ('chat','launcher')"
  size: 1400x900
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-roster-peers.script.jsonl"
    BOXER_CHAT_DRAFT: "Draw the wind above 15 m/s."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the roster shows what the other chat windows' turns are doing

ADR-0324 background §3.1 on the headless host, against the scripted model
in [chat-roster-peers.script.jsonl](./chat-roster-peers.script.jsonl), which
takes six seconds to answer. The first chat window sends; the launcher opens
a second one, whose roster lists the first window's conversation with what
its turn is doing — the model at work, then answered — and a Raise button,
which brings the first window to the front. A chat window keeps its line
for the others in the host's process, so the roster reads it with no query
of its own.

```jsonl trace
{"do":"note","text":"ADR-0324 background 3.1: the other windows' turns in the roster"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button","comment":"the seeded draft, in the first chat window"}
{"do":"type","role":"text_input","nth":0,"text":"chat","settleMs":500,"comment":"the launcher's search field"}
{"do":"click","contains":"New window","role":"button","comment":"a second chat window"}
{"do":"wait","name":"Conversations","role":"button","nth":1}
{"do":"click","name":"Conversations","role":"button","nth":1,"comment":"the second window's roster"}
{"do":"read","valueContains":"chat window 1 ·","role":"label","pattern":"(?P<waiting>waiting for the answer)","comment":"the first window's turn waits on the model"}
{"do":"capture","text":"chat-roster-peers-running"}
{"do":"read","valueContains":"chat window 1 ·","role":"label","pattern":"(?P<done>· answered)","comment":"then it is answered"}
{"do":"click","contains":"Raise","role":"button","nth":1,"comment":"the launcher has a Raise too; the roster's comes after it"}
{"do":"capture","text":"chat-roster-peers-raised"}
```
