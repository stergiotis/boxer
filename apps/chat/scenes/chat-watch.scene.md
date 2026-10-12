---
type: reference
audience: contributor
status: draft
scene:
  launch: "subject_alias IN ('chat','widgets')"
  size: 1400x900
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-watch.script.jsonl"
    BOXER_CHAT_DRAFT: "Draw the wind above 15 m/s."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — a watched turn that ends while the person is elsewhere

ADR-0323 §SD4 and §SD7 (proposed) on the headless host, against the scripted
model in [chat-watch.script.jsonl](./chat-watch.script.jsonl), which takes
four seconds to answer. The person sends, turns on Watch for the
conversation, and moves to another window. The turn ends there unseen: the host's
control counts one highlight after what waits, and lists it apart. Open
raises the chat window, which marks what landed as new since the person
looked, and the highlight goes. The roster then shows the conversation in
its colour, with the steps it wrote per minute (ADR-0324 background §3.4,
§3.12).

```jsonl trace
{"do":"note","text":"ADR-0323 SD4: watch a conversation"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button","comment":"the seeded draft"}
{"do":"click","contains":"Watch","role":"button","comment":"tell me when this turn ends"}
{"do":"click","contains":"Widget gallery","role":"window","pointer":true,"comment":"look elsewhere while the model answers"}
{"do":"read","contains":"nothing waits on you","role":"button","on":"name","pattern":"(?P<highlights>\\d+)$","comment":"one highlight, after what waits"}
{"do":"expect","of":"highlights","eq":1}
{"do":"click","contains":"nothing waits on you","role":"button"}
{"do":"wait","valueContains":"the turn ended","role":"label"}
{"do":"capture","text":"chat-watch-highlight"}
{"do":"click","name":"Open","role":"button"}
{"do":"wait","valueContains":"new since you looked","role":"label","comment":"the chat marks what landed while the person was away"}
{"do":"read","contains":"nothing waits on you","role":"button","on":"name","pattern":"(?P<cleared>on you)$","comment":"looking cleared the highlight"}
{"do":"capture","text":"chat-watch-looked"}
{"do":"click","contains":"Conversations","role":"button","settleMs":4000,"comment":"the roster samples the runs every three seconds; two samples give a pace"}
{"do":"wait","valueContains":"/min","role":"label","comment":"the pace gauge on the open conversation"}
{"do":"capture","text":"chat-watch-roster"}
```
