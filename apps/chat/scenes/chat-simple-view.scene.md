---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  env:
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_CHAT_ARTEFACT: "true"
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-simple-view.script.jsonl"
    BOXER_CHAT_DRAFT: "Write a short note."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — one rendering: the bar is one row, and the artefact comes forward

The headless host, against the scripted model in
[chat-simple-view.script.jsonl](./chat-simple-view.script.jsonl). This scene
held the Simple view until ADR-0404 §SD7 (proposed) retired it; the window it
showed is now the only one. The bar is one row: New conversation, the state
chip, and what the model may do in words; the rest is in the menu bar above.
The transcript has the window until the model's first write brings the
artefact forward beside it.
The running turn reads "working…" and counts steps. Settings, opened from the
bar's words, has no Simple view.

```jsonl trace
{"do":"note","text":"the chat's one rendering"}
{"do":"wait","contains":"Send","role":"button"}
{"do":"wait","contains":"Model:","role":"button","comment":"what the model may do, in words, in the bar"}
{"do":"capture","text":"chat-simple-view-full","sidecars":["svg","tree"]}
{"do":"click","contains":"Model:","role":"button","comment":"the words open Settings beside the transcript"}
{"do":"wait","valueContains":"What the model may do","role":"label"}
{"do":"click","name":"View","role":"button","comment":"the window's View menu"}
{"do":"click","pointer":true,"name":"Settings","role":"check_box","comment":"and it closes again"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"working…","role":"label","comment":"plain words for a running turn"}
{"do":"wait","valueContains":"step 2","role":"label"}
{"do":"wait","valueContains":"The scripted model wrote this note","role":"label","comment":"the artefact came forward: a view not shown is not laid out"}
{"do":"capture","text":"chat-simple-view-artefact","sidecars":["svg","tree"]}
{"do":"wait","valueContains":"The note is in the Artefact tab.","role":"label","settleMs":500,"comment":"the turn landed; the model's words are its own"}
```
