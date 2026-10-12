---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-queue-quick-reply.script.jsonl"
    BOXER_CHAT_QUESTIONS: "true"
    BOXER_CHAT_DRAFT: "Draw the wind above 15 m/s."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — a question answered from the person's queue

ADR-0324 §SD5 (proposed) on the headless host, against the scripted model
in [chat-queue-quick-reply.script.jsonl](./chat-queue-quick-reply.script.jsonl):
the model asks one single-choice question. It shows in the chat's pending
bubble and waits in the host's queue; the person opens it from the queue's
control in the menu bar and answers in the card, and the same turn
continues with the answer.

```jsonl trace
{"do":"note","text":"ADR-0324 SD5: quick reply"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button","comment":"the seeded draft"}
{"do":"wait","valueContains":"Threshold on which speed?","role":"label"}
{"do":"wait","contains":"your turn · 1","comment":"the question waits in the host's queue"}
{"do":"click","contains":"your turn · 1"}
{"do":"capture","text":"chat-queue-menu"}
{"do":"click","name":"Open","role":"button"}
{"do":"wait","contains":"Your turn","role":"window","comment":"the card, non-modal, above the chat"}
{"do":"click","name":"gusts","role":"radio_button","nth":1,"comment":"gusts, in the card: the tree lists the chat window first, then the card"}
{"do":"capture","text":"chat-queue-card"}
{"do":"click","name":"Answer","role":"button","comment":"the card's Answer; the window's carries an icon"}
{"do":"wait","valueContains":"Drawing the layer","role":"label","comment":"the same turn, answered from the card"}
{"do":"wait","valueContains":"Speed: gusts","role":"label"}
{"do":"wait","contains":"nothing waits on you","comment":"the queue is empty"}
{"do":"capture","text":"chat-queue-answered"}
```
