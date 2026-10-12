---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-queue-form.script.jsonl"
    BOXER_CHAT_QUESTIONS: "true"
    BOXER_CHAT_DRAFT: "Draw the wind above 15 m/s."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — a form of two questions answered in the host's card

ADR-0324 §SD5 and its update of 2026-10-12 on the headless host, against
the scripted model in [chat-queue-form.script.jsonl](./chat-queue-form.script.jsonl):
the model asks two questions, the second a multiple choice. F8 opens the
card; the person picks one option of the first and two of the second, and
Answer, disabled until every question has a choice, sends both answers to
the same turn.

```jsonl trace
{"do":"note","text":"ADR-0324 SD5: a form in the card"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"click","contains":"Send","role":"button","comment":"the seeded draft"}
{"do":"wait","contains":"your turn · 1","comment":"the form waits in the host's queue"}
{"do":"key","text":"F8","comment":"its card"}
{"do":"wait","contains":"Your turn","role":"window"}
{"do":"click","name":"gusts","role":"radio_button","nth":1,"comment":"in the card: the tree lists the chat window first, then the card"}
{"do":"click","name":"waves","role":"check_box","nth":1}
{"do":"click","name":"pressure","role":"check_box","nth":1}
{"do":"capture","text":"chat-queue-form-card"}
{"do":"click","name":"Answer","role":"button","comment":"the card's Answer; the window's carries an icon"}
{"do":"wait","valueContains":"Drawing the layer","role":"label","comment":"the same turn, answered from the card"}
{"do":"wait","valueContains":"Speed: gusts","role":"label"}
{"do":"wait","valueContains":"waves, pressure","role":"label"}
{"do":"wait","contains":"nothing waits on you","comment":"the queue is empty"}
{"do":"capture","text":"chat-queue-form-answered"}
```
